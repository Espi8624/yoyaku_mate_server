package main

import (
	"fmt"
	"log"
	"net/http"
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

	rateLimitedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		activeLmt := lmt
		if realtimeQueuePaths[r.URL.Path] {
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

	// Start server
	log.Printf("Server starting on %s...", cfg.Server.Port)
	if err := http.ListenAndServe(cfg.Server.Port, rateLimitedHandler); err != nil {
		log.Fatal("Server failed to start: ", err)
	}
}
