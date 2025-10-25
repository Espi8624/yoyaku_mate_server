package main

import (
	"log"
	"net/http"
	"yoyaku_mate_server/config"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/db"
	handlers "yoyaku_mate_server/handlers"

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
	// mux := http.NewServeMux()
	r := mux.NewRouter()
	r.Use(mux.CORSMethodMiddleware(r))

	// Register routes
	handlers.RegisterRoutes(r, uploadHandler)

	// Configure CORS
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})

	handler := c.Handler(r)

	// Start server
	log.Printf("Server starting on %s...", cfg.Server.Port)
	if err := http.ListenAndServe(cfg.Server.Port, handler); err != nil {
		log.Fatal("Server failed to start: ", err)
	}
}
