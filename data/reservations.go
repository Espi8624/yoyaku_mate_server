package data

import "yoyaku_mate_server/models"

// 予約状況データ目録
var reservationsData = []models.ReservationItem{
	{ID: 1, Details: "日の丸美容室　予約", TimeStamp: "2025-03-20 13:00"},
	{ID: 2, Details: "川崎食堂　予約", TimeStamp: "2025-03-23 11:00"},
	{ID: 3, Details: "品川食堂　予約", TimeStamp: "2025-03-23 17:00"},
	{ID: 4, Details: "日本橋皮膚科　予約", TimeStamp: "2025-03-24 17:00"},
	{ID: 5, Details: "日本橋整形外科　予約", TimeStamp: "2025-03-24 19:00"},
	{ID: 6, Details: "川崎食堂　予約", TimeStamp: "2025-03-25 12:00"},
	{ID: 7, Details: "上野写真館　予約", TimeStamp: "2025-04-04 12:00"},
}

// 全ての予約状況データ取得
func GetAllReservations() []models.ReservationItem {
	return reservationsData
}
