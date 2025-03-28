package main

import (
	"log"
	"net/http"
	"yoyaku_mate_server/handlers"

	"github.com/rs/cors"
)

func main() {
	mux := http.NewServeMux()

	// 핸들러 연결
	mux.HandleFunc("/", handlers.HomeHandler)
	mux.HandleFunc("/frequent-places", handlers.FrequentPlacesHandler)
	mux.HandleFunc("/timeline", handlers.TimeLineHandler)
	mux.HandleFunc("/reservations", handlers.ReservationsHandler)

	// CORS 설정
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "https://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})

	handler := c.Handler(mux)

	// 서버 시작
	log.Println("Server starting on :8080...")
	err := http.ListenAndServe(":8080", handler)
	if err != nil {
		log.Fatal("Server failed to start: ", err)
	}
}
