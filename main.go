package main

import (
	"log"
	"net/http"
	"yoyaku_mate_server/db"
	CustomerSidehandlers "yoyaku_mate_server/handlers/customer_side"
	ProviderSidehandlers "yoyaku_mate_server/handlers/provider_side"

	"github.com/rs/cors"
)

func main() {
	// MongoDB初期化
	mongoURI := "mongodb://localhost:27017"
	db.InitMongoDB(mongoURI)

	// HTTP Mux 初期化
	mux := http.NewServeMux()

	// ルーティング設定
	CustomerSidehandlers.RegisterRoutes(mux)
	ProviderSidehandlers.RegisterRoutes(mux)

	// CORS 設定
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "https://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})

	handler := c.Handler(mux)

	// サーバー起動
	log.Println("Server starting on :8080...")
	err := http.ListenAndServe(":8080", handler)
	if err != nil {
		log.Fatal("Server failed to start: ", err)
	}
}
