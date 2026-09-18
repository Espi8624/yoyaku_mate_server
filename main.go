package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"yoyaku_mate_server/auth"
	"yoyaku_mate_server/config"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/events"
	handlers "yoyaku_mate_server/handlers"
	"yoyaku_mate_server/metrics"

	"github.com/didip/tollbooth/v7"
	"github.com/gorilla/mux"
	"github.com/rs/cors"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize Firebase Auth
	// - 認証情報が無ければ全ての認証付きAPIが機能しないため、ここで即座に停止させる
	if err := auth.InitFirebase(); err != nil {
		log.Fatalf("Firebase初期化エラー: %v", err)
	}

	// Initialize MongoDB
	if err := db.InitMongoDB(cfg.MongoDB.URI); err != nil {
		log.Printf("MongoDB初期化失敗: %v", err)
	}

	// - エラー率/応答時間/CPU使用率の閾値超過をSlackへ通知するバックグラウンドワーカーを起動
	//   (SLACK_WEBHOOK_URL未設定時は内部で無効化される)
	metrics.StartAlertWorker(cfg.SlackWebhookURL)

	// MinIO クライアント初期化
	// minioClient, err := data.NewMinioClient(
	// 	"http://localhost:9000",
	// 	"minioadmin",
	// 	"minioadmin",
	// 	"yoyaku-mate-biz", // MinIO バケット名
	// )
	// if err != nil {
	// 	log.Fatalf("Could not initialize Minio client: %v", err)
	// }
	// uploadHandler := handlers.NewUploadHandler(minioClient)

	var storageClient *data.MinioClient
	if cfg.R2.AccountID == "" {
		// R2_ACCOUNT_IDが設定されていない場合、ローカルストレージを使用するため空のインスタンスを生成
		log.Println("Warning: R2_ACCOUNT_ID is not set. Using local file storage.")
		storageClient = &data.MinioClient{}
	} else {
		r2Endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.R2.AccountID)
		var r2Err error
		storageClient, r2Err = data.NewMinioClient(
			r2Endpoint,
			cfg.R2.AccessKey,
			cfg.R2.SecretKey,
			"",
		)
		if r2Err != nil {
			// クライアント初期化失敗時は警告を出力して続行
			log.Printf("Warning: Could not initialize R2 client: %v", r2Err)
		}
	}
	menuRepo := &data.MongoMenuRepo{}
	userRepo := &data.MongoUserRepo{}
	storeRepo := &data.MongoStoreRepo{}
	staffRepo := &data.MongoStaffRepo{}
	shiftTableRepo := &data.MongoShiftTableRepo{}
	shiftChangeRequestRepo := &data.MongoShiftChangeRequestRepo{}
	authSvc := &auth.FirebaseAuthService{}

	uploadHandler := handlers.NewUploadHandler(
		storageClient,
		menuRepo,
		userRepo,
		storeRepo,
		authSvc,
		cfg.R2.AssetsBucketName,
		cfg.R2.AssetsPublicDomain,
		cfg.R2.BizBucketName,
		cfg.SlackWebhookURL,
	)

	// Initialize HTTP mux
	// mux := http.NewServeMux()
	r := mux.NewRouter()
	r.Use(mux.CORSMethodMiddleware(r))

	// Initialize DI handlers
	sessionRepo := &data.MongoSessionRepo{}

	waitingHandler := handlers.NewWaitingListHandler(
		&data.MongoWaitingListRepo{},
		storeRepo,
		userRepo,
		authSvc,
		sessionRepo,
		events.GetBroker(),
		events.GetWaitingUserBroker(),
		metrics.GetTracker(),
	)

	menuHandler := handlers.NewMenuListHandler(
		menuRepo,
		userRepo,
		authSvc,
	)

	sessionHandler := handlers.NewSessionHandler(sessionRepo)
	userInfoHandler := handlers.NewUserInfoHandler(userRepo, storeRepo, staffRepo, authSvc)
	storeInfoHandler := handlers.NewStoreInfoHandler(storeRepo, userRepo)
	storeSettingsHandler := handlers.NewStoreSettingsHandler(storeRepo, userRepo)
	storeStaffHandler := handlers.NewStoreStaffHandler(staffRepo, userRepo, storeRepo, authSvc)
	shiftTableHandler := handlers.NewShiftTableHandler(shiftTableRepo, staffRepo, userRepo, authSvc, storeRepo, shiftChangeRequestRepo)
	storeListHandler := handlers.NewStoreListHandler(storeRepo, authSvc)
	storeAiContextHandler := handlers.NewStoreAIContextHandler(storeRepo, &data.MongoWaitingListRepo{}, menuRepo)
	adminHandler := handlers.NewStoreInfoAdminHandler(storeRepo)
	storeLicenseCallHandler := handlers.NewStoreLicenseCallHandler(storeRepo)
	statisticsHandler := handlers.NewStatisticsHandler(userRepo, storeRepo)

	// Register routes
	handlers.RegisterRoutes(
		r,
		userRepo,
		sessionRepo,
		sessionHandler,
		uploadHandler,
		waitingHandler,
		menuHandler,
		userInfoHandler,
		storeInfoHandler,
		storeSettingsHandler,
		storeStaffHandler,
		shiftTableHandler,
		storeListHandler,
		storeAiContextHandler,
		adminHandler,
		storeLicenseCallHandler,
		statisticsHandler,
	)

	// Configure CORS
	c := cors.New(cors.Options{
		AllowedOrigins:   cfg.Server.AllowOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})

	handler := metrics.MetricsMiddleware(metrics.GetRequestTracker(), metrics.GetTracker())(c.Handler(r))

	// Rate Limiting Middleware (5 requests per second per IP)
	// Burst of 10 to allow parallel requests (like images/css or multiple API calls)
	// - tollboothはデフォルトで (IP, パス) の組ごとにバケットを分けるため、他エンドポイントとは
	//   独立してカウントされる(エンドポイント間で予算を共有するわけではない)
	lmt := tollbooth.NewLimiter(5, nil)
	lmt.SetBurst(10)

	// Create a custom handler for rejection to return JSON
	lmt.SetMessage(`{"status": "error", "message": "Too Many Requests"}`)
	lmt.SetStatusCode(http.StatusTooManyRequests)
	lmt.SetOnLimitReached(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
	})

	// - リアルタイム待機列エンドポイント(SSE購読・ポーリング)専用の緩いレートリミッター
	//   同一店舗Wi-Fi/NAT配下では複数客が同一IPとして扱われ、同じパスのバケットを取り合うことになる。
	//   これら3エンドポイントは書き込みを伴わない読み取り専用のため、上限を引き上げて
	//   通常利用で429→再接続ループ(体感の「固まり」)が起きるのを防ぐ
	realtimeLmt := tollbooth.NewLimiter(30, nil)
	realtimeLmt.SetBurst(60)
	realtimeLmt.SetMessage(`{"status": "error", "message": "Too Many Requests"}`)
	realtimeLmt.SetStatusCode(http.StatusTooManyRequests)
	realtimeLmt.SetOnLimitReached(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
	})

	realtimeQueuePaths := map[string]bool{
		"/api/waiting-list/stream":      true,
		"/api/waiting-list/stream-user": true,
		"/api/waiting-list/poll":        true,
	}

	// - 管理者ログインは共有パスワード方式のため、総当たり攻撃を緩和する目的で
	//   デフォルトより厳しいレートリミットを別途適用する
	loginLmt := tollbooth.NewLimiter(1, nil)
	loginLmt.SetBurst(3)
	loginLmt.SetMessage(`{"status": "error", "message": "Too Many Requests"}`)
	loginLmt.SetStatusCode(http.StatusTooManyRequests)
	loginLmt.SetOnLimitReached(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
	})

	rateLimitedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		activeLmt := lmt
		switch {
		case r.URL.Path == "/api/admin/auth/login":
			activeLmt = loginLmt
		case realtimeQueuePaths[r.URL.Path]:
			activeLmt = realtimeLmt
		}
		if httpErr := tollbooth.LimitByRequest(activeLmt, w, r); httpErr != nil {
			activeLmt.ExecOnLimitReached(w, r)
			w.WriteHeader(httpErr.StatusCode)
			_, _ = w.Write([]byte(httpErr.Message))
			return
		}
		handler.ServeHTTP(w, r)
	})

	// - タイムアウトを設定するためhttp.Serverを明示的に組み立てる。
	//   ListenAndServeの既定値は「無制限」で、ヘッダを少しずつ送り続けるだけで
	//   接続を占有できてしまう(Slowloris)。shared-cpu-1xでは特に効きやすい
	srv := &http.Server{
		Addr:    cfg.Server.Port,
		Handler: rateLimitedHandler,

		// - ヘッダ受信の制限。Slowlorisを止めるのはこの一点で足りる
		ReadHeaderTimeout: 10 * time.Second,

		// - keep-aliveで待機中の接続を回収する。リクエストとリクエストの「間」にのみ
		//   適用され、ハンドラ実行中には関与しないためSSEは切れない
		IdleTimeout: 120 * time.Second,

		// - ReadTimeout と WriteTimeout は意図的に設定しない。
		//   特にWriteTimeoutを入れると、その時間ごとに全てのSSE接続が
		//   応答の途中で強制切断される
	}

	// - SIGTERMを受けてから終了するまでの猶予。fly.ioは停止時にSIGTERMを送る
	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("Server starting on %s...", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		log.Fatal("Server failed to start: ", err)
	case <-shutdownCtx.Done():
		log.Println("シャットダウン信号を受信しました。処理中のリクエストを待機します...")
	}

	// - 猶予は短くする。SSE接続は自分から終了しないため Shutdown は必ず猶予を
	//   使い切る一方、fly.ioはSIGTERMの5秒後(kill_timeoutの既定値)にSIGKILLを送る。
	//   猶予をそれより長く取ると毎回SIGKILLが先に届き、下のFlushAllに到達しない。
	//   通常のRESTリクエストは3秒あれば捌き切れる
	graceCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := srv.Shutdown(graceCtx); err != nil {
		// - 期限切れ = SSE接続が残っている状態。クライアントは再接続で復帰するため
		//   ここで打ち切ってよい。閉じずに抜けるとFlushAllが遅れる
		log.Printf("猶予内に接続を閉じ切れませんでした。残りを強制的に閉じます: %v", err)
		if closeErr := srv.Close(); closeErr != nil {
			log.Printf("接続の強制クローズに失敗: %v", closeErr)
		}
	}

	// - メモリ上に溜まっているメトリクスをMongoへ書き出す。
	//   バッチワーカーは5秒周期のため、ここで流さないと最大5秒分が失われる
	metrics.FlushAll()

	log.Println("シャットダウン完了")
}
