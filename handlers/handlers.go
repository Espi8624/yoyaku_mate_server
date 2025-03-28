package handlers

import (
	"encoding/json"
	"net/http"
)

// 자주 방문하는 장소 목록
var FrequentPlaces = []string{
	"日の丸美容室",
	"川崎食堂",
	"品川食堂",
	"日本橋皮膚科",
	"日本橋整形外科",
}

// 타임라인 데이터 구조체
type TimeLineItem struct {
	PlaceName string `json:"placeName"`
	TimeStamp string `json:"timeStamp"`
}

// 타임라인 데이터 목록
var TimeLineData = []TimeLineItem{
	{PlaceName: "日の丸美容室", TimeStamp: "2025-03-20 13:00"},
	{PlaceName: "川崎食堂", TimeStamp: "2025-03-23 11:00"},
	{PlaceName: "品川食堂", TimeStamp: "2025-03-23 17:00"},
	{PlaceName: "日本橋皮膚科", TimeStamp: "2025-03-24 17:00"},
	{PlaceName: "日本橋整形外科", TimeStamp: "2025-03-25 12:00"},
}

// 예약캘린더 데이터 구조체
type ReservationsItem struct {
	ID        int    `json:"id"`
	Details   string `json:"details"`
	TimeStamp string `json:"timeStamp"`
}

var ReservationsData = []ReservationsItem{
	{ID: 1, Details: "日の丸美容室　予約", TimeStamp: "2025-03-20 13:00"},
	{ID: 2, Details: "川崎食堂　予約", TimeStamp: "2025-03-23 11:00"},
	{ID: 3, Details: "品川食堂　予約", TimeStamp: "2025-03-23 17:00"},
	{ID: 4, Details: "日本橋皮膚科　予約", TimeStamp: "2025-03-24 17:00"},
	{ID: 5, Details: "日本橋整形外科　予約", TimeStamp: "2025-03-24 19:00"},
	{ID: 6, Details: "川崎食堂　予約", TimeStamp: "2025-03-25 12:00"},
	{ID: 7, Details: "上野写真館　予約", TimeStamp: "2025-04-04 12:00"},
}

// 기본 핸들러
func HomeHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello, This is Yoyaku Mate Server."))
}

// 자주 방문하는 장소 목록 반환
func FrequentPlacesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(FrequentPlaces)
	if err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// 타임라인 데이터를 반환
func TimeLineHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(TimeLineData)
	if err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// 예약약 데이터를 반환
func ReservationsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(ReservationsData)
	if err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}
