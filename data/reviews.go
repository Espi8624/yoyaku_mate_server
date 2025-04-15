package data

import "yoyaku_mate_server/models"

// レヴューデータ目録
var reviewsData = []models.ReviewItem{
	{ID: 1, StoreName: "日の丸美容室", Comments: "잘 깎아요.", Rating: 3.1, TimeStamp: "2025-03-20 19:00"},
	{ID: 2, StoreName: "川崎食堂", Comments: "음식이 맛있어요.", Rating: 3.7, TimeStamp: "2025-03-23 17:00"},
	{ID: 3, StoreName: "品川食堂", Comments: "직원이 친절해요.", Rating: 4.1, TimeStamp: "2025-03-23 21:00"},
	{ID: 4, StoreName: "日本橋皮膚科", Comments: "의사가 실력있어요.", Rating: 4.7, TimeStamp: "2025-03-24 20:00"},
	{ID: 5, StoreName: "日本橋整形外科", Comments: "의사가 형편없어요.", Rating: 1.7, TimeStamp: "2025-03-25 18:00"},
}

// 全てのレヴューデータ取得
func GetAllReviews() []models.ReviewItem {
	return reviewsData
}
