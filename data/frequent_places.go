package data

import "yoyaku_mate_server/models"

var frequentPlacesData = []models.FrequentPlaceItem{
	{StoreName: "日の丸美容室", TimeStamp: "2025-03-20 19:00", LastVisited: "2025-03-20", VisitCount: 1},
	{StoreName: "川崎食堂", TimeStamp: "2025-03-23 17:00", LastVisited: "2025-03-23", VisitCount: 2},
	{StoreName: "川崎食堂", TimeStamp: "2025-03-23 17:00", LastVisited: "2025-03-23", VisitCount: 2},
	{StoreName: "品川食堂", TimeStamp: "2025-03-23 21:00", LastVisited: "2025-03-23", VisitCount: 3},
	{StoreName: "日本橋皮膚科", TimeStamp: "2025-03-24 20:00", LastVisited: "2025-03-24", VisitCount: 4},
	{StoreName: "日本橋整形外科", TimeStamp: "2025-03-25 18:00", LastVisited: "2025-03-25", VisitCount: 5},
}

func GetAllFrequentPlaces() []models.FrequentPlaceItem {
	return frequentPlacesData
}
