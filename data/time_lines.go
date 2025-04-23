package data

import "yoyaku_mate_server/models"

// タイムラインデータ目録
var timeLinesData = []models.TimeLineItem{
	{ReservationID: 1, StoreName: "日の丸美容室", TimeStamp: "2025-03-20 13:00"},
	{ReservationID: 2, StoreName: "川崎食堂", TimeStamp: "2025-03-23 11:00"},
	{ReservationID: 3, StoreName: "品川食堂", TimeStamp: "2025-03-23 17:00"},
	{ReservationID: 4, StoreName: "日本橋皮膚科", TimeStamp: "2025-03-24 17:00"},
	{ReservationID: 5, StoreName: "日本橋整形外科", TimeStamp: "2025-03-25 12:00"},
}

// 全てのタイムラインデータ取得
func GetAllTimeLines() []models.TimeLineItem {
	return timeLinesData
}
