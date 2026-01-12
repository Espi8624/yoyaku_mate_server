package data

import (
	"fmt"
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"sort"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/net/context"
)

func GetWaitingListData(storeID string) ([]models.WaitingList, error) {
	// 期限切れデータの自動更新
	if err := AutoExpireWaitingItems(storeID); err != nil {
		log.Printf("Warning: Failed to auto-expire waiting items: %v", err)
	}

	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var waitingListData []models.WaitingList

	// 日本時間帯設定
	jst := time.FixedZone("Asia/Tokyo", 9*60*60) // UTC+9
	now := time.Now().In(jst)

	// 02:00を基準とした営業日ウィンドウの設定
	var windowStart time.Time
	if now.Hour() < 2 {
		windowStart = time.Date(now.Year(), now.Month(), now.Day()-1, 2, 0, 0, 0, jst)
	} else {
		windowStart = time.Date(now.Year(), now.Month(), now.Day(), 2, 0, 0, 0, jst)
	}
	windowEnd := windowStart.Add(24 * time.Hour)

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

	// 予想待ち時間の計算 (In-Memory)
	// 1. アクティブな待機アイテム(waiting, notified)だけを抽出
	var activeItems []models.WaitingList
	for _, item := range waitingListData {
		if item.Status == "waiting" || item.Status == "notified" {
			activeItems = append(activeItems, item)
		}
	}

	// 2. QueueNumber順にソート (念のため)
	sort.Slice(activeItems, func(i, j int) bool {
		return activeItems[i].QueueNumber < activeItems[j].QueueNumber
	})

	// 3. 全体リストを回しながら、アクティブなアイテムの場合、その順序に基づいて時間を計算
	for i := range waitingListData {
		if waitingListData[i].Status == "waiting" || waitingListData[i].Status == "notified" {
			// アクティブリスト内でのインデックスを探す
			// activeItemsはソートされているので、自身のQueueNumberより小さいものの数が待ち組数
			// 店舗設定から予想待ち時間を取得 (非効率だが一旦ループ内で取得、本来は外で一回取得すべき)
			// TODO: パフォーマンス最適化 (外で取得して渡す)
			settings, err := GetStoreSettingsData(storeID)
			minutesPerTeam := 10 // default
			if err == nil && settings.Settings.WaitingPolicy.EstimatedWaitTime > 0 {
				minutesPerTeam = settings.Settings.WaitingPolicy.EstimatedWaitTime
			}

			waitingCount := 0
			for idx, active := range activeItems {
				if active.QueueNumber == waitingListData[i].QueueNumber {
					waitingCount = idx
					break
				}
			}
			waitingListData[i].EstimatedWaitTime = CalculateEstimatedWaitTime(waitingCount, minutesPerTeam)
		} else {
			waitingListData[i].EstimatedWaitTime = 0
		}
	}

	if err := cursor.Err(); err != nil {
		log.Printf("Cursor error: %v", err)
		return nil, err
	}

	return waitingListData, nil
}

// 24時間（または営業日）が経過した待機データを 'no_show' に自動更新
func AutoExpireWaitingItems(storeID string) error {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)

	// 現在の営業日の開始時刻(02:00)を計算
	var businessDayStart time.Time
	if now.Hour() < 2 {
		businessDayStart = time.Date(now.Year(), now.Month(), now.Day()-1, 2, 0, 0, 0, jst)
	} else {
		businessDayStart = time.Date(now.Year(), now.Month(), now.Day(), 2, 0, 0, 0, jst)
	}

	cutoffStr := businessDayStart.Format("2006-01-02T15:04:05.000+09:00")

	// 現在の営業日より前に登録された 'waiting' または 'notified' 状態のデータを抽出
	filter := bson.M{
		"store_id": storeID,
		"status":   bson.M{"$in": []string{"waiting", "notified"}},
		"registration_time": bson.M{
			"$lt": cutoffStr,
		},
	}

	update := bson.M{
		"$set": bson.M{
			"status": "no_show",
		},
	}

	result, err := collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return err
	}

	if result.ModifiedCount > 0 {
		log.Printf("Auto-expired %d items for store %s", result.ModifiedCount, storeID)
	}

	return nil
}

// 新しいウェイティングリスト項目作成
func CreateWaitingListItem(item models.WaitingList) (*models.WaitingList, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 必須フィールドの検証
	if item.StoreID == "" {
		return nil, fmt.Errorf("store_id is required")
	}
	if item.PartySize <= 0 {
		return nil, fmt.Errorf("party_size must be greater than 0")
	}
	if item.WaitingID == "" {
		return nil, fmt.Errorf("waiting_id is required")
	}

	// 提供されていない場合はデフォルト値を設定
	if item.Status == "" {
		item.Status = "waiting"
	}

	// 次のQueueを獲得
	nextQueueNumber, err := GetNextQueueNumber(item.StoreID)
	if err != nil {
		return nil, fmt.Errorf("failed to get next queue number: %v", err)
	}
	item.QueueNumber = nextQueueNumber

	// ハンドラから移してきた基本値設定
	if item.Status == "" {
		item.Status = "waiting"
	}
	if item.RegistrationTime == "" {
		jst := time.FixedZone("Asia/Tokyo", 9*60*60)
		now := time.Now().In(jst)
		item.RegistrationTime = now.Format("2006-01-02T15:04:05.000+09:00")
	}
	if item.WaitingID == "" {
		jst := time.FixedZone("Asia/Tokyo", 9*60*60)
		now := time.Now().In(jst)
		item.WaitingID = now.Format("20060102-150405")
	}

	// 予想待ち時間の計算
	// 現在の待機組数(waiting, notified)を取得
	countFilter := bson.M{
		"store_id": item.StoreID,
		"status":   bson.M{"$in": []string{"waiting", "notified"}},
		// 営業日の判定は必要か？ -> 既にClearWaitingList等で古いのは消えてる/cancelされてる前提なら不要だが、念のため日付入れてもいい
		// 単純化のため、statusだけでカウントする (古いのが残ってたらそれも待ち時間に含めるのが安全)
	}
	activeCount, err := collection.CountDocuments(ctx, countFilter)
	if err != nil {
		log.Printf("Failed to count active waiting items: %v", err)
		item.EstimatedWaitTime = 0 // エラー時は0
	} else {
		// 店舗設定から予想待ち時間を取得
		settings, err := GetStoreSettingsData(item.StoreID)
		minutesPerTeam := 10 // default
		if err == nil && settings.Settings.WaitingPolicy.EstimatedWaitTime > 0 {
			minutesPerTeam = settings.Settings.WaitingPolicy.EstimatedWaitTime
		}
		item.EstimatedWaitTime = CalculateEstimatedWaitTime(int(activeCount), minutesPerTeam)
	}

	// BSONドキュメントを作成
	doc := bson.M{
		"store_id":            item.StoreID,
		"waiting_id":          item.WaitingID,
		"queue_number":        item.QueueNumber,
		"party_size":          item.PartySize,
		"registration_time":   item.RegistrationTime,
		"contact":             item.Contact,
		"status":              item.Status,
		"nationality":         item.Nationality,
		"called_time":         nil,
		"entry_time":          nil,
		"notes":               item.Notes,
		"estimated_wait_time": item.EstimatedWaitTime,
		"menu_items":          item.MenuItems,
	}

	// 新規項目を挿入
	result, err := collection.InsertOne(ctx, doc)
	if err != nil {
		log.Printf("Failed to insert waiting list item: %v\nDocument: %+v", err, doc)
		return nil, fmt.Errorf("failed to insert waiting list item: %v", err)
	}

	// 挿入されたIDを設定して返却 (再取得を避ける)
	item.ID = result.InsertedID.(primitive.ObjectID)

	return &item, nil
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

	// 02:00を基準とした営業日ウィンドウの設定
	var startOfDay time.Time
	if now.Hour() < 2 {
		startOfDay = time.Date(now.Year(), now.Month(), now.Day()-1, 2, 0, 0, 0, jst)
	} else {
		startOfDay = time.Date(now.Year(), now.Month(), now.Day(), 2, 0, 0, 0, jst)
	}
	endOfDay := startOfDay.Add(24 * time.Hour)

	// 今日の待機項目をフィルタリング
	filter := bson.M{
		"store_id": storeID,
		"registration_time": bson.M{
			"$gte": startOfDay.Format("2006-01-02T15:04:05.000+09:00"),
			"$lt":  endOfDay.Format("2006-01-02T15:04:05.000+09:00"),
		},
		"status": "waiting",
	}

	// ステータスをcancelledに更新
	update := bson.M{
		"$set": bson.M{
			"status": "cancelled",
		},
	}

	// 一致するすべてのドキュメントを更新
	result, err := collection.UpdateMany(ctx, filter, update)
	if err != nil {
		log.Printf("Failed to clear waiting list: %v", err)
		return err
	}

	log.Printf("Successfully cleared %d waiting list items", result.ModifiedCount)
	return nil
}

// 特定店舗の特定ユーザーのウェイティングリスト項目を取得
func GetUserWaitingListItem(storeID string) (*models.WaitingList, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	defer cancel()
	filter := bson.M{
		"store_id": storeID,
	}

	var waitingListItem models.WaitingList
	err := collection.FindOne(ctx, filter).Decode(&waitingListItem)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // 結果がない場合
		}
		return nil, err
	}

	return &waitingListItem, nil
}

func GetActiveWaitingList(storeID string, waitingID string) ([]models.WaitingList, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var waitingListData []models.WaitingList

	// 日本時間帯設定
	jst := time.FixedZone("Asia/Tokyo", 9*60*60) // UTC+9
	now := time.Now().In(jst)
	// 02:00を基準とした営業日ウィンドウの設定
	var windowStart time.Time
	if now.Hour() < 2 {
		windowStart = time.Date(now.Year(), now.Month(), now.Day()-1, 2, 0, 0, 0, jst)
	} else {
		windowStart = time.Date(now.Year(), now.Month(), now.Day(), 2, 0, 0, 0, jst)
	}
	windowEnd := windowStart.Add(24 * time.Hour)

	// フィルター設定: storeIDとwindow範囲の登録時間でフィルタリング
	filter := bson.M{
		"store_id":   storeID,
		"waiting_id": waitingID,
		"status": bson.M{
			"$in": []string{"waiting", "notified"},
		},
		"registration_time": bson.M{
			"$gte": windowStart.Format("2006-01-02T15:04:05.000+09:00"),
			"$lt":  windowEnd.Format("2006-01-02T15:04:05.000+09:00"),
		},
	}

	// 결과를 registration_time 순서로 정렬하여 항상 같은 순서를 보장합니다.
	opts := options.Find().SetSort(bson.M{"registration_time": 1})

	// MongoDBクエリ実行
	cursor, err := collection.Find(ctx, filter, opts)
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

	// 各アイテムについて、自分より前の待機数をカウントして時間を計算
	for i := range waitingListData {
		if waitingListData[i].Status == "waiting" || waitingListData[i].Status == "notified" {
			countFilter := bson.M{
				"store_id": storeID,
				"status":   bson.M{"$in": []string{"waiting", "notified"}},
				"queue_number": bson.M{
					"$lt": waitingListData[i].QueueNumber,
				},
			}
			aheadCount, err := collection.CountDocuments(ctx, countFilter)
			if err != nil {
				log.Printf("Failed to count items ahead: %v", err)
				waitingListData[i].EstimatedWaitTime = 0
			} else {
				// 店舗設定から予想待ち時間を取得
				settings, err := GetStoreSettingsData(storeID)
				minutesPerTeam := 10 // default
				if err == nil && settings.Settings.WaitingPolicy.EstimatedWaitTime > 0 {
					minutesPerTeam = settings.Settings.WaitingPolicy.EstimatedWaitTime
				}
				waitingListData[i].EstimatedWaitTime = CalculateEstimatedWaitTime(int(aheadCount), minutesPerTeam)
			}
		} else {
			waitingListData[i].EstimatedWaitTime = 0
		}
	}

	return waitingListData, nil
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

// 予想待機時間を計算する共通ロジック
// 将来的にAIロジックなどに置き換える場合はここを修正する
func CalculateEstimatedWaitTime(waitingCount int, minutesPerTeam int) int {
	// 単純な計算: 1組あたり minutesPerTeam 分
	if minutesPerTeam <= 0 {
		minutesPerTeam = 10
	}
	return waitingCount * minutesPerTeam
}
