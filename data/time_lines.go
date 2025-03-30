package data

import "yoyaku_mate_server/models"

// 타임라인 데이터 목록
var timeLinesData = []models.TimeLineItem{
	{StoreName: "日の丸美容室", TimeStamp: "2025-03-20 13:00"},
	{StoreName: "川崎食堂", TimeStamp: "2025-03-23 11:00"},
	{StoreName: "品川食堂", TimeStamp: "2025-03-23 17:00"},
	{StoreName: "日本橋皮膚科", TimeStamp: "2025-03-24 17:00"},
	{StoreName: "日本橋整形外科", TimeStamp: "2025-03-25 12:00"},
}

// 모든 알림 반환
func GetAllTimeLines() []models.TimeLineItem {
	return timeLinesData
}
