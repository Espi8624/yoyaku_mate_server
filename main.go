package main

import (
	"fmt"
	"log"
	"net/http"
	"yoyaku_mate_server/config"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/db"
	handlers "yoyaku_mate_server/handlers"

	"github.com/didip/tollbooth/v7"
	"github.com/gorilla/mux"
	"github.com/rs/cors"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize MongoDB
	if err := db.InitMongoDB(cfg.MongoDB.URI); err != nil {
		log.Printf("MongoDB初期化失敗: %v", err)
	} else {
		// Start monitoring waiting list collection
		collection := db.GetCollection(cfg.MongoDB.Database, "waiting_list")
		go db.MonitorWaitingList(collection)
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

	if cfg.R2.AccountID == "" {
		log.Fatal("Fatal: R2_ACCOUNT_ID is not set.")
	}
	r2Endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.R2.AccountID)

	storageClient, err := data.NewMinioClient(
		r2Endpoint,
		cfg.R2.AccessKey,
		cfg.R2.SecretKey,
		"",
	)
	if err != nil {
		log.Fatalf("Could not initialize R2 client: %v", err)
	}
	uploadHandler := handlers.NewUploadHandler(
		storageClient,
		cfg.R2.AssetsBucketName,
		cfg.R2.AssetsPublicDomain,
		cfg.R2.BizBucketName,
	)

	// Initialize HTTP mux
	// mux := http.NewServeMux()
	r := mux.NewRouter()
	r.Use(mux.CORSMethodMiddleware(r))

	// Register routes
	handlers.RegisterRoutes(r, uploadHandler)

	// Configure CORS
	c := cors.New(cors.Options{
		AllowedOrigins:   cfg.Server.AllowOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})

	handler := c.Handler(r)

	// Rate Limiting Middleware (5 requests per second per IP)
	// Burst of 10 to allow parallel requests (like images/css or multiple API calls)
	lmt := tollbooth.NewLimiter(5, nil)
	lmt.SetBurst(10)

	// Create a custom handler for rejection to return JSON
	lmt.SetMessage(`{"status": "error", "message": "Too Many Requests"}`)
	lmt.SetStatusCode(http.StatusTooManyRequests)
	lmt.SetOnLimitReached(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
	})

	rateLimitedHandler := tollbooth.LimitHandler(lmt, handler)

	// Start server
	log.Printf("Server starting on %s...", cfg.Server.Port)
	if err := http.ListenAndServe(cfg.Server.Port, rateLimitedHandler); err != nil {
		log.Fatal("Server failed to start: ", err)
	}
}
