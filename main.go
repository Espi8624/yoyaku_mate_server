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

	// Initialize MongoDB
	if err := db.InitMongoDB(cfg.MongoDB.URI); err != nil {
		log.Printf("Failed to initialize MongoDB: %v", err)
		return
	}

	// Start monitoring waiting list collection
	collection := db.GetCollection(cfg.MongoDB.Database, "waiting_list")
	go db.MonitorWaitingList(collection)

	// MinIO 클라이언트 초기화 (이전과 동일)
	minioClient, err := data.NewMinioClient(
		"http://localhost:9000",
		"minioadmin",
		"minioadmin",
		"yoyaku-mate-biz", // MinIO에서 만든 버킷 이름
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
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
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
