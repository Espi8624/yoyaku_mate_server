package main

import (
	"log"
	"net/http"
	"yoyaku_mate_server/handlers"

	"github.com/rs/cors"
)

func main() {
	mux := http.NewServeMux()

	// client handler 接続
	mux.HandleFunc("/home", handlers.ClientHomeHandler)
	mux.HandleFunc("/user-info", handlers.UserInfoHandler)
	mux.HandleFunc("/frequent-places", handlers.FrequentPlacesHandler)
	mux.HandleFunc("/timeline", handlers.TimeLineHandler)
	mux.HandleFunc("/reservations", handlers.ReservationsHandler)
	mux.HandleFunc("/notifications", handlers.NotificationsHandler)
	mux.HandleFunc("/reviews", handlers.ReviewsHandler)

	// provider handler 接続
	mux.HandleFunc("/provider/home", handlers.ProviderHomeHandler)
	mux.HandleFunc("/provider/store-info", handlers.StoreInfoHandler)
	mux.HandleFunc("/provider/store-menus", handlers.StoreMenuHandler)
	mux.HandleFunc("/provider/store-reservations", handlers.StoreReservationsHandler)
	mux.HandleFunc("/provider/store-reviews", handlers.StoreReviewsHandler)

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
