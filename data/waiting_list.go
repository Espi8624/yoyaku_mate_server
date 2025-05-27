package data

import (
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"golang.org/x/net/context"
)

func GetWaitingListData(storeID string) ([]models.WaitingListItem, error) {
	collection := db.GetCollection("yoyaku_mate_provider_db", "waiting_list")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var waitingListData []models.WaitingListItem

	// 現在の日時を取得し、当日の開始と終了の時間を計算
	now := time.Now().UTC()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfDay := startOfDay.Add(24 * time.Hour)

	// filter 設定：storeID と当日の登録時間でフィルタリング
	filter := bson.M{
		"store_id":          storeID,
		"registration_time": bson.M{"$gte": startOfDay, "$lt": endOfDay},
	}

	// MongoDB Query 実行
	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		log.Printf("Failed to fetch waiting list data: %v", err)
		return nil, err
	}
	defer cursor.Close(ctx)

	// 結果をデコードして waitingListData に追加
	for cursor.Next(ctx) {
		var waitingListItem models.WaitingListItem
		if err := cursor.Decode(&waitingListItem); err != nil {
			log.Printf("Failed to decode waiting list item: %v", err)
			continue
		}
		waitingListData = append(waitingListData, waitingListItem)
	}

	if err := cursor.Err(); err != nil {
		log.Printf("Cursor error: %v", err)
		return nil, err
	}

	return waitingListData, nil
}
