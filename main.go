package main

import (
	"log"
	"net/http"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/handlers"

	"github.com/rs/cors"
)

func main() {
	// MongoDB初期化
	mongoURI := "mongodb://localhost:27017"
	db.InitMongoDB(mongoURI)

	mux := http.NewServeMux()

	// customer side handler 接続
	mux.HandleFunc("/home", handlers.CustomerHomeHandler)
	mux.HandleFunc("/user-info", handlers.UserInfoHandler)
	mux.HandleFunc("/frequent-places", handlers.FrequentPlacesHandler)
	mux.HandleFunc("/timeline", handlers.TimeLineHandler)
	mux.HandleFunc("/reservations", handlers.ReservationsHandler)
	mux.HandleFunc("/notifications", handlers.NotificationsHandler)

	// provider side handler 接続
	mux.HandleFunc("/provider/home", handlers.ProviderHomeHandler)
	mux.HandleFunc("/provider/store-info", handlers.StoreInfoHandler)
	mux.HandleFunc("/provider/store-menus", handlers.StoreMenuHandler)
	mux.HandleFunc("/provider/store-comments", handlers.StoreCommentHandler)
	mux.HandleFunc("/provider/store-reservations", handlers.StoreReservationsHandler)

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
