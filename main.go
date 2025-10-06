package main

import (
	"log"
	"net/http"
	"os"
	"yoyaku_mate_server/config"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/db"
	handlers "yoyaku_mate_server/handlers"

	"github.com/rs/cors"
)

func main() {
	// Load configuration
	env := os.Getenv("GO_ENV")
	cfg := config.Load(env)

	// LINE関連
	// err := godotenv.Load()
	// if err != nil {
	// 	log.Println("No .env file found, using default environment variables")
	// }

	// .envで読み込んだ値を変数に保存
	// channelSecret := os.Getenv("LINE_MESSAGING_CHANNEL_SECRET")
	// channelToken := os.Getenv("LINE_MESSAGING_CHANNEL_ACCESS_TOKEN")

	// 両方価が存在するか確認
	// if channelSecret == "" || channelToken == "" {
	// 	log.Fatal("エラー: LINE関連環境変数が設定されていません。")
	// }

	// handlersパッケージの初期化関数を直接呼出
	// if err := handlers.InitLineBot(channelSecret, channelToken); err != nil {
	// 	log.Fatalf("LINE Bot クライアント生成失敗: %v", err)
	// }
	// LINE関連

	// Initialize MongoDB
	if err := db.InitMongoDB(cfg.MongoDB.URI); err != nil {
		log.Printf("Failed to initialize MongoDB: %v", err)
		return
	}

	// Start monitoring waiting list collection
	collection := db.GetCollection(cfg.MongoDB.Database, "waiting_list")
	go db.MonitorWaitingList(collection)

	// MinIO クライアント初期化
	minioClient, err := data.NewMinioClient(
		"http://localhost:9000",
		"minioadmin",
		"minioadmin",
		"yoyaku-mate-biz", // MinIO バケット名
	)
	if err != nil {
		log.Fatalf("Could not initialize Minio client: %v", err)
	}

	uploadHandler := handlers.NewUploadHandler(minioClient)

	// Initialize HTTP mux
	mux := http.NewServeMux()

	// Register routes
	handlers.RegisterRoutes(mux, uploadHandler)

	// Configure CORS
	c := cors.New(cors.Options{
		AllowedOrigins:   cfg.Server.AllowOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})

	handler := c.Handler(mux)

	// Start server
	log.Printf("Server starting on %s...", cfg.Server.Port)
	if err := http.ListenAndServe(cfg.Server.Port, handler); err != nil {
		log.Fatal("Server failed to start: ", err)
	}
}
