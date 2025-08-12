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

func GetWaitingListData(storeID string) ([]models.WaitingList, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var waitingListData []models.WaitingList

	// 日本時間帯設定
	jst := time.FixedZone("Asia/Tokyo", 9*60*60) // UTC+9
	now := time.Now().In(jst)
	// window: 先日 23時 ~ 明日 1時
	windowStart := time.Date(now.Year(), now.Month(), now.Day()-1, 23, 0, 0, 0, jst)
	windowEnd := time.Date(now.Year(), now.Month(), now.Day()+1, 1, 0, 0, 0, jst)
	// フィルター設定: storeIDとwindow範囲の登録時間でフィルタリング
	filter := bson.M{
		"store_id": storeID,
		"registration_time": bson.M{
			"$gte": windowStart.Format("2006-01-02T15:04:05.000+09:00"),
			"$lt":  windowEnd.Format("2006-01-02T15:04:05.000+09:00"),
		},
	}
	// MongoDBクエリ実行
	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		log.Printf("Failed to fetch waiting list data: %v", err)
		return nil, err
	}
	defer cursor.Close(ctx)

	// 結果をデコードして waitingListData に追加
	for cursor.Next(ctx) {
		var waitingListItem models.WaitingList
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

// 新しいウェイティングリスト項目作成
func CreateWaitingListItem(item models.WaitingList) (*models.WaitingList, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Validate required fields
	if item.StoreID == "" {
		return nil, fmt.Errorf("store_id is required")
	}
	if item.CustomerName == "" {
		return nil, fmt.Errorf("customer_name is required")
	}
	if item.PartySize <= 0 {
		return nil, fmt.Errorf("party_size must be greater than 0")
	}
	if item.WaitingID == "" {
		return nil, fmt.Errorf("waiting_id is required")
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

	// // Check for duplicate waiting_id
	// 中腹処理必要時、DBに直接追加必要
	// filter := bson.M{
	// 	"store_id":   item.StoreID,
	// 	"waiting_id": item.WaitingID,
	// }

	// var existingItem models.WaitingListItem
	// err := collection.FindOne(ctx, filter).Decode(&existingItem)
	// if err == nil {
	// 	return nil, fmt.Errorf("waiting_id %s already exists for store %s", item.WaitingID, item.StoreID)
	// } else if err != mongo.ErrNoDocuments {
	// 	log.Printf("Error checking for duplicate waiting_id: %v", err)
	// 	return nil, fmt.Errorf("failed to check for duplicate waiting_id: %v", err)
	// }

	// Create BSON document
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
	result, err := collection.InsertOne(ctx, doc)
	if err != nil {
		log.Printf("Failed to insert waiting list item: %v\nDocument: %+v", err, doc)
		return nil, fmt.Errorf("failed to insert waiting list item: %v", err)
	}

	log.Printf("Successfully inserted waiting list item with ID: %v", result.InsertedID)
	// 挿入された document を照会し、返却
	var createdItem models.WaitingList
	filter := bson.M{"_id": result.InsertedID}
	err = collection.FindOne(ctx, filter).Decode(&createdItem)
	if err != nil {
		log.Printf("Failed to fetch newly created waiting list item: %v", err)
		return nil, fmt.Errorf("failed to fetch newly created item: %v", err)
	}

	return &createdItem, nil
}

// 特定店舗の次の利用可能なキュー番号を返却
func GetNextQueueNumber(storeID string) (int, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 特定店舗の最大キュー番号を取得
	opts := options.FindOne().SetSort(bson.D{{Key: "queue_number", Value: -1}})
	filter := bson.D{{Key: "store_id", Value: storeID}}

	var lastItem models.WaitingList
	err := collection.FindOne(ctx, filter, opts).Decode(&lastItem)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// もしドキュメントが存在しない場合、キュー番号1から開始
			return 1, nil
		}
		return 0, err
	}

	// 次のキュー番号を返却
	return lastItem.QueueNumber + 1, nil
}

// 特定店舗の今日のウェイティングリストをキャンセル状態に更新
func ClearWaitingList(storeID string) error {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 日本時間帯設定
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

// 特定店舗の特定ユーザーのウェイティングリスト項目を取得
func GetUserWaitingListItem(storeID, waitingID string) (*models.WaitingList, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 日本時間帯設定
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, jst)
	endOfDay := startOfDay.Add(24 * time.Hour)

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

	var waitingListItem models.WaitingList
	err := collection.FindOne(ctx, filter).Decode(&waitingListItem)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // 결과가 없는 경우
		}
		return nil, err
	}

	return &waitingListItem, nil
}

// 特定のウェイティング項目のステータスを更新
func UpdateWaitingStatus(storeID, waitingID, status string) (*models.WaitingList, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 日本時間帯設定
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)

	// 状態に応じて追加フィールドを更新
	update := bson.M{
		"$set": bson.M{
			"status": status,
		},
	}

	// status が "notified" の場合、called_time を追加
	if status == "notified" {
		update["$set"].(bson.M)["called_time"] = now.Format(time.RFC3339)
	}

	// アップデート遂行
	filter := bson.M{
		"store_id":   storeID,
		"waiting_id": waitingID,
	}

	// UpdateOne 後、アップデートされたドキュメントを取得
	after := options.After
	opts := options.FindOneAndUpdate().SetReturnDocument(after)

	var updatedItem models.WaitingList
	err := collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedItem)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("waiting item not found")
		}
		return nil, err
	}

	return &updatedItem, nil
}

// ウェイティングリストの項目のステータスを更新し、必要に応じて時間フィールドを設定
func UpdateWaitingItemStatus(storeID string, waitingID string, status string) error {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// 日本時間帯設定
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)
	currentTime := now.Format("2006-01-02T15:04:05.000+09:00")

	// ステータスに応じて時間フィールドを設定
	setFields := bson.M{
		"status": status,
	}

	// ステータスに応じて時間フィールドを設定
	switch status {
	case "completed":
		setFields["entry_time"] = currentTime
		log.Printf("Setting entry_time to %s for waiting_id %s", currentTime, waitingID)
	case "notified":
		setFields["called_time"] = currentTime
	}

	update := bson.M{
		"$set": setFields,
	}

	filter := bson.M{
		"store_id":   storeID,
		"waiting_id": waitingID,
	}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		log.Printf("Failed to update waiting status: %v", err)
		return err
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("no waiting item found with waiting_id: %s", waitingID)
	}

	return nil
}

// 平均待機時間（秒）を返す
// 担当者：紙谷
func GetAverageWaitingTime(storeID string) (int, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// フィルター設定
	filter := bson.M{
		"store_id":   storeID,
		"entry_time": bson.M{"$ne": nil},
	}

	// entry_timeがnilでないドキュメントを取得
	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	// 平均待機時間を計算
	totalSeconds := int64(0)
	count := int64(0)

	// 各ドキュメントをループして待機時間を計算
	for cursor.Next(ctx) {
		var item models.WaitingList
		if err := cursor.Decode(&item); err != nil {
			continue
		}
		// 登録時間と入店時間をパース
		reg, err1 := time.Parse(time.RFC3339, item.RegistrationTime)
		var ent time.Time
		var err2 error
		if item.EntryTime != nil {
			ent, err2 = time.Parse(time.RFC3339, *item.EntryTime)
		} else {
			err2 = fmt.Errorf("entry_time is nil")
		}
		if err1 == nil && err2 == nil {
			totalSeconds += int64(ent.Sub(reg).Seconds())
			count++
		}
	}
	if count < 40 {
		return -1, nil // 40件未満なら-1
	}
	// 平均待機時間を計算
	return int(totalSeconds / count), nil
}
