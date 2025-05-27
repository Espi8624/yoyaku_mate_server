package main

import (
	"log"
	"net/http"
	"os"
	"yoyaku_mate_server/config"
	"yoyaku_mate_server/db"
	CustomerSidehandlers "yoyaku_mate_server/handlers/customer_side"
	ProviderSidehandlers "yoyaku_mate_server/handlers/provider_side"

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

	// Initialize HTTP mux
	mux := http.NewServeMux()

	// Register routes
	CustomerSidehandlers.RegisterRoutes(mux)
	ProviderSidehandlers.RegisterRoutes(mux)

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
