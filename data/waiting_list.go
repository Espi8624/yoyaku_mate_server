package data

import (
	"fmt"
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/net/context"
)

func GetWaitingListData(storeID string) ([]models.WaitingListItem, error) {
	collection := db.GetCollection("yoyaku_mate_provider_db", "waiting_list")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var waitingListData []models.WaitingListItem

	// 일본 시간대 설정
	// 日本時間帯設定
	jst := time.FixedZone("Asia/Tokyo", 9*60*60) // UTC+9
	now := time.Now().In(jst)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jst)
	endOfDay := startOfDay.Add(24 * time.Hour)
	// 필터 설정: storeID와 당일 등록 시간으로 필터링
	// フィルター設定: storeIDと当日の登録時間でフィルタリング
	filter := bson.M{
		"store_id": storeID,
		"registration_time": bson.M{
			"$gte": startOfDay.Format("2006-01-02T15:04:05.000+09:00"),
			"$lt":  endOfDay.Format("2006-01-02T15:04:05.000+09:00"),
		},
		"status": "waiting",
	}
	// MongoDB 쿼리 실행
	// MongoDBクエリ実行
	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		log.Printf("Failed to fetch waiting list data: %v", err)
		return nil, err
	}
	defer cursor.Close(ctx)

	// 결과를 디코딩하여 waitingListData 에 추가
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

// CreateWaitingListItem은 데이터베이스에 새로운 웨이팅 리스트 항목을 생성합니다
// 新しいウェイティングリスト項目作成
func CreateWaitingListItem(item models.WaitingListItem) error {
	collection := db.GetCollection("yoyaku_mate_provider_db", "waiting_list")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Ensure required fields are set
	if item.StoreID == "" {
		return fmt.Errorf("store_id is required")
	}

	// Set default values if not provided
	if item.Status == "" {
		item.Status = "waiting"
	}

	// Set registration time if not provided
	if item.RegistrationTime == "" {
		jst := time.FixedZone("Asia/Tokyo", 9*60*60)
		now := time.Now().In(jst)
		item.RegistrationTime = now.Format("2006-01-02T15:04:05.000+09:00")
	}

	// Explicitly set called_time and entry_time to null if not set
	doc := bson.M{
		"store_id":          item.StoreID,
		"waiting_id":        item.WaitingID,
		"queue_number":      item.QueueNumber,
		"customer_name":     item.CustomerName,
		"party_size":        item.PartySize,
		"registration_time": item.RegistrationTime,
		"contact":           item.Contact,
		"status":            item.Status,
		"nationality":       item.Nationality,
		"called_time":       nil,
		"entry_time":        nil,
		"notes":             item.Notes,
	}

	// Insert the new item
	_, err := collection.InsertOne(ctx, doc)
	if err != nil {
		log.Printf("Failed to insert waiting list item: %v", err)
		return err
	}

	return nil
}

// GetNextQueueNumber returns the next available queue number for a store
// 特定店舗の次の利用可能なキュー番号を返却
func GetNextQueueNumber(storeID string) (int, error) {
	collection := db.GetCollection("yoyaku_mate_provider_db", "waiting_list")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Find the highest queue number for the store
	opts := options.FindOne().SetSort(bson.D{{Key: "queue_number", Value: -1}})
	filter := bson.D{{Key: "store_id", Value: storeID}}

	var lastItem models.WaitingListItem
	err := collection.FindOne(ctx, filter, opts).Decode(&lastItem)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// If no documents exist, start with queue number 1
			return 1, nil
		}
		return 0, err
	}

	// Return the next queue number
	return lastItem.QueueNumber + 1, nil
}

// 웨이팅 리스트를 비우고 상태를 'cancelled'로 업데이트
// 特定店舗の今日のウェイティングリストをキャンセル状態に更新
func ClearWaitingList(storeID string) error {
	collection := db.GetCollection("yoyaku_mate_provider_db", "waiting_list")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 일본 시간대 설정
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jst)
	endOfDay := startOfDay.Add(24 * time.Hour)

	// Filter for today's waiting items
	filter := bson.M{
		"store_id": storeID,
		"registration_time": bson.M{
			"$gte": startOfDay.Format("2006-01-02T15:04:05.000+09:00"),
			"$lt":  endOfDay.Format("2006-01-02T15:04:05.000+09:00"),
		},
		"status": "waiting",
	}

	// Update to set status to cancelled
	update := bson.M{
		"$set": bson.M{
			"status": "cancelled",
		},
	}

	// Update all matching documents
	result, err := collection.UpdateMany(ctx, filter, update)
	if err != nil {
		log.Printf("Failed to clear waiting list: %v", err)
		return err
	}

	log.Printf("Successfully cleared %d waiting list items", result.ModifiedCount)
	return nil
}

// GetUserWaitingListItem은 특정 매장의 특정 사용자에 대한 웨이팅 리스트 항목을 조회합니다
// 特定店舗の特定ユーザーのウェイティングリスト項目を取得
func GetUserWaitingListItem(storeID, waitingID string) (*models.WaitingListItem, error) {
	collection := db.GetCollection("yoyaku_mate_provider_db", "waiting_list")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 일본 시간대 설정
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jst)
	endOfDay := startOfDay.Add(24 * time.Hour)

	// filter 설정: storeID, waitingID, 당일 등록 시간으로 필터링
	// フィルター設定: storeID、waitingID、当日の登録時間でフィルタリング
	filter := bson.M{
		"store_id":   storeID,
		"waiting_id": waitingID,
		"registration_time": bson.M{
			"$gte": startOfDay.Format("2006-01-02T15:04:05.000+09:00"),
			"$lt":  endOfDay.Format("2006-01-02T15:04:05.000+09:00"),
		},
		"status": "waiting",
	}

	var waitingListItem models.WaitingListItem
	err := collection.FindOne(ctx, filter).Decode(&waitingListItem)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // 결과가 없는 경우
		}
		return nil, err
	}

	return &waitingListItem, nil
}
