package db

import (
	"context"
	"log"
	"net/url"
	"sync"
	"time"
	"yoyaku_mate_server/config"
	"yoyaku_mate_server/utils"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	DatabaseName               = "project_rusui"
	CollectionWaitingList      = "waiting_list"
	CollectionErrorLogs        = "error_logs"
	CollectionRequestLogs      = "request_logs"
	CollectionDailyActiveUsers = "daily_active_users"
	CollectionAuditLogs        = "audit_logs"
	CollectionSessions         = "sessions"
)

// mongoClient は確立済みのMongoDBクライアント。未接続の間はnil。
//
//   - 以前は公開変数 MongoClient だったが、再接続ワーカー(StartReconnectWatcher)が
//     書き込み、全ハンドラが読むようになったためデータ競合になる。
//     アクセスは必ず Client() / setClient() を通すこと
var (
	mongoClient *mongo.Client
	clientMu    sync.RWMutex

	// connectURI は再接続ワーカーが使う接続文字列。InitMongoDBが記録する
	connectURI string
)

// Client は現在のMongoDBクライアントを返す。未接続の場合はnil。
// 呼び出し側は IsReady() での確認、またはミドルウェアによる遮断を前提とすること
func Client() *mongo.Client {
	clientMu.RLock()
	defer clientMu.RUnlock()
	return mongoClient
}

// IsReady はMongoDBクライアントが確立済みかどうかを返す。
//
//   - ここで見るのは「クライアントが存在するか」だけで、疎通そのものではない。
//     疎通が一時的に切れてもドライバが自動で復旧するため、pingの失敗で
//     IsReadyをfalseにすると健全なリクエストまで巻き添えで503にしてしまう
//   - 実際の疎通確認は /health (handlers.HealthHandler) の役割として分けている
func IsReady() bool {
	return Client() != nil
}

func setClient(c *mongo.Client) {
	clientMu.Lock()
	defer clientMu.Unlock()
	mongoClient = c
}

// maskMongoURI は接続文字列から認証情報を伏せた、ログ出力用の文字列を返す。
//
// - 以前はURIをそのままログへ出しており、fly.ioのログにDBのユーザー名と
//   パスワードが平文で残り続けていた。ログは保管され、アプリの閲覧権限が
//   あれば誰でも読めるため、認証情報の置き場所としては最悪に近い
// - 解析できなかった場合はURIを一切出さない。原文へフォールバックすると
//   「伏せ字にしたつもりが出ている」という最も危険な状態になる
func maskMongoURI(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "(URIの解析に失敗したため非表示)"
	}

	masked := *parsed
	if parsed.User != nil {
		// - "***" のような記号はパーセントエンコードされて "%2A%2A%2A" になり
		//   ログが読みづらくなるため、エンコード対象にならない英字を使う
		masked.User = url.User("REDACTED")
	}
	// - 現状のクエリはauthSource等だが、将来認証情報が混ざっても漏れないよう落とす。
	//   接続先の特定にはスキーム+ホスト+DB名があれば足りる
	masked.RawQuery = ""

	return masked.String()
}

// reconnectInterval は起動時の接続に失敗した場合、再接続ワーカーが次の試行までに空ける間隔
const reconnectInterval = 30 * time.Second

// Initialize MongoDB connection
func InitMongoDB(uri string) error {
	connectURI = uri
	return connectOnce(5)
}

// connectOnce は指定回数まで接続とPingを試み、成功したらクライアントを確定させる。
// attempts回すべて失敗した場合は最後のエラーを返す
func connectOnce(attempts int) error {
	log.Printf("Try mongoDB connect: %s", maskMongoURI(connectURI))

	clientOptions := options.Client().
		ApplyURI(connectURI).
		SetConnectTimeout(config.GetMongoTimeout()). // 30秒(production.jsonから設定)
		SetServerSelectionTimeout(30 * time.Second).
		SetSocketTimeout(45 * time.Second).
		SetMaxPoolSize(10).
		SetRetryWrites(true).
		SetRetryReads(true).
		SetMaxConnecting(5).
		SetServerAPIOptions(options.ServerAPI(options.ServerAPIVersion1))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var err error
	for i := 0; i < attempts; i++ {
		var client *mongo.Client
		client, err = mongo.Connect(ctx, clientOptions)
		if err != nil {
			log.Printf("MongoDB connect failed (%d/%d): %v", i+1, attempts, err)
			time.Sleep(5 * time.Second)
			continue
		}

		// connect test (Ping)
		err = client.Ping(ctx, nil)
		if err != nil {
			log.Printf("MongoDB Ping failed (%d/%d): %v", i+1, attempts, err)
			// - Ping失敗したクライアントは破棄する。Disconnectしないとコネクションプールが
			//   残り、再試行のたびにソケットが積み上がっていく
			_ = client.Disconnect(ctx)
			time.Sleep(5 * time.Second)
			continue
		}

		// - Pingが通ってから初めて公開する。途中で差し込むと、疎通していないクライアントを
		//   IsReady()がtrueと判定する時間帯ができてしまう
		setClient(client)
		log.Println("MongoDB connect success")

		// - インデックス作成はリクエスト処理をブロックしないようバックグラウンドで実行
		//   (Fly.ioのコールドスタート時、起動からリスニング開始までの時間を最短化するため。
		//    以前は同期実行しており、10個超のインデックスを順次作成する間ProxyがTimeoutしていた)
		utils.Go("mongo_ensure_indexes", func() {
			if err := EnsureIndexes(); err != nil {
				log.Printf("Failed to create indexes: %v", err)
				// Index creation failure should not stop server startup, but warn loudly
			}
		})

		return nil
	}

	log.Printf("MongoDB connect failed after %d attempts", attempts)
	return err
}

// StartReconnectWatcher は起動時の接続に失敗した場合に、バックグラウンドで再接続を試み続ける。
//
//   - 以前は起動時に失敗するとクライアントがnilのまま二度と復旧せず、Atlasが復旧しても
//     再デプロイするまでサービスが戻らなかった
//   - プロセスを落とす(log.Fatal)選択はしない。マシン1台構成では、Atlasが戻るまで
//     クラッシュループに入り、再起動のたびに起動シーケンス(シークレット取得を含む)を
//     やり直すことになる。503を返しながら待ち、復旧した瞬間に自力で戻る方が早い
//   - 接続確立後の一時的な切断はドライバ自身がトポロジを監視して復旧するため、
//     ここでは扱わない。このワーカーが面倒を見るのは「一度も繋がっていない」状態だけ
func StartReconnectWatcher() {
	utils.GoForever("mongo_reconnect_watcher", func() {
		for {
			if IsReady() {
				return
			}
			time.Sleep(reconnectInterval)
			if IsReady() {
				return
			}
			log.Println("MongoDB未接続のため再接続を試みます")
			if err := connectOnce(1); err != nil {
				log.Printf("MongoDB再接続に失敗しました。%v後に再試行します: %v", reconnectInterval, err)
			}
		}
	})
}

// // MongoDB collection 取得
func GetCollection(database, collection string) *mongo.Collection {
	client := Client()
	if client == nil {
		log.Println("MongoDB 클라이언트가 초기화되지 않음")
		return nil
	}
	return client.Database(database).Collection(collection)
}

// コレクションのインデックスを作成
func EnsureIndexes() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	collection := GetCollection(DatabaseName, CollectionWaitingList)
	if collection == nil {
		return nil
	}

	// 統計用複合インデックス: store_id + registration_time (降順)
	// これにより、store_idによるフィルタリングとregistration_timeによる範囲クエリ（今日の統計など）が最適化されます
	indexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "store_id", Value: 1},
			{Key: "registration_time", Value: -1},
		},
		Options: options.Index().SetName("idx_store_reg_time"),
	}

	_, err := collection.Indexes().CreateOne(ctx, indexModel)
	if err != nil {
		return err
	}
	log.Println("Created compound index: idx_store_reg_time on waiting_list")

	// 個別待機アイテムの照会用複合インデックス: store_id + waiting_id
	// - CreateItemの重複登録チェック、UpdateItemStatus、UpdateWaitingStatusが
	//   全てこの組み合わせでフィルタしており、待機通知のたびに呼ばれるホットパスのため必須
	waitingIDIndexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "store_id", Value: 1},
			{Key: "waiting_id", Value: 1},
		},
		Options: options.Index().SetName("idx_store_waiting_id"),
	}
	if _, err := collection.Indexes().CreateOne(ctx, waitingIDIndexModel); err != nil {
		log.Printf("Failed to create idx_store_waiting_id index: %v", err)
	} else {
		log.Println("Created compound index: idx_store_waiting_id on waiting_list")
	}

	errorLogsCollection := GetCollection(DatabaseName, CollectionErrorLogs)
	if errorLogsCollection != nil {
		ttlIndexModel := mongo.IndexModel{
			Keys: bson.D{{Key: "timestamp", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(604800).SetName("idx_error_logs_ttl"),
		}
		_, err := errorLogsCollection.Indexes().CreateOne(ctx, ttlIndexModel)
		if err != nil {
			log.Printf("Failed to create TTL index for error_logs: %v", err)
		} else {
			log.Println("Created TTL index: idx_error_logs_ttl on error_logs (7 days)")
		}

		typeIndexModel := mongo.IndexModel{
			Keys: bson.D{{Key: "error_type", Value: 1}},
			Options: options.Index().SetName("idx_error_type"),
		}
		_, err = errorLogsCollection.Indexes().CreateOne(ctx, typeIndexModel)
		if err != nil {
			log.Printf("Failed to create error_type index: %v", err)
		} else {
			log.Println("Created index: idx_error_type on error_logs")
		}
	}

	requestLogsCollection := GetCollection(DatabaseName, CollectionRequestLogs)
	if requestLogsCollection != nil {
		// - ディスク容量不足の防止およびMongoDB Atlasストレージコスト最小化のため、3日間(259,200秒)のTTLを適用
		// - 3日以上経過した古いリクエストログデータはバックグラウンドで自動的に永久削除される
		ttlIndexModel := mongo.IndexModel{
			Keys: bson.D{{Key: "timestamp", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(259200).SetName("idx_request_logs_ttl"), // 3日
		}
		_, err := requestLogsCollection.Indexes().CreateOne(ctx, ttlIndexModel)
		if err != nil {
			log.Printf("Failed to create TTL index for request_logs: %v", err)
		} else {
			log.Println("Created TTL index: idx_request_logs_ttl on request_logs (3 days)")
		}

		// - 最近のリクエストログ照会および24時間統計/Aggregationクエリの性能最適化のためのtimestamp降順インデックス
		timeIndexModel := mongo.IndexModel{
			Keys: bson.D{{Key: "timestamp", Value: -1}},
			Options: options.Index().SetName("idx_request_logs_timestamp"),
		}
		_, err = requestLogsCollection.Indexes().CreateOne(ctx, timeIndexModel)
		if err != nil {
			log.Printf("Failed to create timestamp index for request_logs: %v", err)
		} else {
			log.Println("Created index: idx_request_logs_timestamp on request_logs")
		}
	}

	dauCollection := GetCollection(DatabaseName, CollectionDailyActiveUsers)
	if dauCollection != nil {
		// 31日 TTL インデックス (2,678,400秒)
		ttlIndexModel := mongo.IndexModel{
			Keys: bson.D{{Key: "timestamp", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(2678400).SetName("idx_dau_ttl"),
		}
		_, err := dauCollection.Indexes().CreateOne(ctx, ttlIndexModel)
		if err != nil {
			log.Printf("Failed to create TTL index for daily_active_users: %v", err)
		} else {
			log.Println("Created TTL index: idx_dau_ttl on daily_active_users (31 days)")
		}

		// date + client_ip 複合ユニークインデックス
		compoundIndexModel := mongo.IndexModel{
			Keys: bson.D{
				{Key: "date", Value: 1},
				{Key: "client_ip", Value: 1},
			},
			Options: options.Index().SetUnique(true).SetName("idx_date_ip"),
		}
		_, err = dauCollection.Indexes().CreateOne(ctx, compoundIndexModel)
		if err != nil {
			log.Printf("Failed to create compound unique index for daily_active_users: %v", err)
		} else {
			log.Println("Created unique index: idx_date_ip on daily_active_users")
		}
	}

	sessionsCollection := GetCollection(DatabaseName, CollectionSessions)
	if sessionsCollection != nil {
		// - セッショントークンによる検索は認証が必要な全リクエストで走るため、ユニークインデックスを張る
		sessionIDIndexModel := mongo.IndexModel{
			Keys:    bson.D{{Key: "session_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("idx_sessions_session_id"),
		}
		_, err := sessionsCollection.Indexes().CreateOne(ctx, sessionIDIndexModel)
		if err != nil {
			log.Printf("Failed to create session_id index for sessions: %v", err)
		} else {
			log.Println("Created unique index: idx_sessions_session_id on sessions")
		}

		// - 同一端末の有効セッション検索、および他端末の一括無効化で使う複合インデックス
		sessionUserIndexModel := mongo.IndexModel{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "device_id", Value: 1},
				{Key: "revoked_at", Value: 1},
			},
			Options: options.Index().SetName("idx_sessions_user_device"),
		}
		_, err = sessionsCollection.Indexes().CreateOne(ctx, sessionUserIndexModel)
		if err != nil {
			log.Printf("Failed to create user/device index for sessions: %v", err)
		} else {
			log.Println("Created index: idx_sessions_user_device on sessions")
		}

		// - 90日間アクセスの無いセッションは自動削除する (無効化済みの古いレコードが無限に溜まるのを防ぐ)
		sessionTTLIndexModel := mongo.IndexModel{
			Keys:    bson.D{{Key: "last_seen_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(7776000).SetName("idx_sessions_ttl"),
		}
		_, err = sessionsCollection.Indexes().CreateOne(ctx, sessionTTLIndexModel)
		if err != nil {
			log.Printf("Failed to create TTL index for sessions: %v", err)
		} else {
			log.Println("Created TTL index: idx_sessions_ttl on sessions (90 days)")
		}
	}

	auditLogsCollection := GetCollection(DatabaseName, CollectionAuditLogs)
	if auditLogsCollection != nil {
		// - ディスク容量節約のため90日間（7,776,000秒）TTLインデックスを適用
		auditTTLIndexModel := mongo.IndexModel{
			Keys:    bson.D{{Key: "timestamp", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(7776000).SetName("idx_audit_logs_ttl"),
		}
		_, err := auditLogsCollection.Indexes().CreateOne(ctx, auditTTLIndexModel)
		if err != nil {
			log.Printf("Failed to create TTL index for audit_logs: %v", err)
		} else {
			log.Println("Created TTL index: idx_audit_logs_ttl on audit_logs (90 days)")
		}

		// - 最新順の取得検索最適化インデックス
		timeIndexModel := mongo.IndexModel{
			Keys:    bson.D{{Key: "timestamp", Value: -1}},
			Options: options.Index().SetName("idx_audit_logs_timestamp"),
		}
		_, err = auditLogsCollection.Indexes().CreateOne(ctx, timeIndexModel)
		if err != nil {
			log.Printf("Failed to create timestamp index for audit_logs: %v", err)
		} else {
			log.Println("Created index: idx_audit_logs_timestamp on audit_logs")
		}
	}

	return nil
}

