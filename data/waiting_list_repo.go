package data

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const CollectionCounters = "counters"

// MongoWaitingListRepo 待機リストのMongoDBリポジトリ実装
type MongoWaitingListRepo struct{}

// Helper: 店舗の営業開始時間に基づいて、現在の「営業日」の開始時刻(Cutoff)を計算する
func (r *MongoWaitingListRepo) GetBusinessDayCutoff(storeID string, now time.Time) time.Time {
	defaultCutoff := time.Date(now.Year(), now.Month(), now.Day(), 4, 0, 0, 0, now.Location())
	storeRepo := &MongoStoreRepo{}
	settings, err := storeRepo.GetSettings(storeID)
	if err != nil {
		if now.Hour() < 4 {
			return defaultCutoff.AddDate(0, 0, -1)
		}
		return defaultCutoff
	}

	if settings.Settings.Is24Hours {
		resetParts := strings.Split(settings.Settings.ResetTime, ":")
		if len(resetParts) != 2 {
			resetParts = []string{"06", "00"}
		}
		resetHour, _ := strconv.Atoi(resetParts[0])
		resetMin, _ := strconv.Atoi(resetParts[1])
		cutoffTime := time.Date(now.Year(), now.Month(), now.Day(), resetHour, resetMin, 0, 0, now.Location())
		if now.Before(cutoffTime) {
			return cutoffTime.AddDate(0, 0, -1)
		}
		return cutoffTime
	}

	// now.Weekday().String()は"Monday"のように先頭大文字だが、DBのoperating_hoursキーは
	// "monday"のように小文字保存されているため、そのままでは常にマップ参照が失敗していた
	weekday := strings.ToLower(now.Weekday().String())
	dayHours, ok := settings.Settings.OperatingHours[weekday]
	if !ok || dayHours.Start == "" {
		if now.Hour() < 4 {
			return defaultCutoff.AddDate(0, 0, -1)
		}
		return defaultCutoff
	}

	parts := strings.Split(dayHours.Start, ":")
	if len(parts) != 2 {
		if now.Hour() < 4 {
			return defaultCutoff.AddDate(0, 0, -1)
		}
		return defaultCutoff
	}
	startHour, _ := strconv.Atoi(parts[0])
	startMin, _ := strconv.Atoi(parts[1])

	cutoffHour := startHour - 1
	if cutoffHour < 0 {
		cutoffHour = 23
	}

	cutoffTime := time.Date(now.Year(), now.Month(), now.Day(), cutoffHour, startMin, 0, 0, now.Location())
	if now.Before(cutoffTime) {
		return cutoffTime.AddDate(0, 0, -1)
	}
	return cutoffTime
}

// GetWaitingList 指定店舗の待機リストを全件取得する
func (r *MongoWaitingListRepo) GetWaitingList(storeID string) ([]models.WaitingList, error) {
	if err := r.AutoExpireWaitingItems(storeID); err != nil {
		log.Printf("Warning: Failed to auto-expire waiting items: %v", err)
	}

	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var waitingListData []models.WaitingList
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)

	windowStart := r.GetBusinessDayCutoff(storeID, now)
	windowEnd := windowStart.Add(24 * time.Hour)

	filter := bson.M{
		"store_id": storeID,
		"registration_time": bson.M{
			"$gte": windowStart.Format("2006-01-02T15:04:05.000+09:00"),
			"$lt":  windowEnd.Format("2006-01-02T15:04:05.000+09:00"),
		},
	}
	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		log.Printf("Failed to fetch waiting list data: %v", err)
		return nil, err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var waitingListItem models.WaitingList
		if err := cursor.Decode(&waitingListItem); err != nil {
			log.Printf("Failed to decode waiting list item: %v", err)
			continue
		}
		waitingListData = append(waitingListData, waitingListItem)
	}

	var activeItems []models.WaitingList
	for _, item := range waitingListData {
		if item.Status == "waiting" || item.Status == "notified" {
			activeItems = append(activeItems, item)
		}
	}

	sort.Slice(activeItems, func(i, j int) bool {
		return activeItems[i].QueueNumber < activeItems[j].QueueNumber
	})

	storeRepo := &MongoStoreRepo{}
	settings, err := storeRepo.GetSettings(storeID)
	minutesPerTeam := 10
	if err == nil && settings.Settings.WaitingPolicy.EstimatedWaitTime > 0 {
		minutesPerTeam = settings.Settings.WaitingPolicy.EstimatedWaitTime
	}

	for i := range waitingListData {
		if waitingListData[i].Status == "waiting" || waitingListData[i].Status == "notified" {
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
		return nil, err
	}

	return waitingListData, nil
}

// AutoExpireWaitingItems 期限切れデータの自動更新
func (r *MongoWaitingListRepo) AutoExpireWaitingItems(storeID string) error {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)
	businessDayStart := r.GetBusinessDayCutoff(storeID, now)
	cutoffStr := businessDayStart.Format("2006-01-02T15:04:05.000+09:00")

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

// CreateItem 新しい待機リストアイテムを作成する
func (r *MongoWaitingListRepo) CreateItem(item models.WaitingList) (*models.WaitingList, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if item.StoreID == "" {
		return nil, fmt.Errorf("store_id is required")
	}
	if item.PartySize <= 0 {
		return nil, fmt.Errorf("party_size must be greater than 0")
	}

	if item.WaitingID != "" {
		var existingItem models.WaitingList
		dupFilter := bson.M{
			"store_id":   item.StoreID,
			"waiting_id": item.WaitingID,
		}
		err := collection.FindOne(ctx, dupFilter).Decode(&existingItem)
		if err == nil {
			log.Printf("Already existing waiting registration (idempotent). store_id: %s, waiting_id: %s", item.StoreID, item.WaitingID)
			return &existingItem, nil
		} else if err != mongo.ErrNoDocuments {
			return nil, fmt.Errorf("failed to check duplicate waiting item: %v", err)
		}
	}

	if item.Status == "" {
		item.Status = "waiting"
	}

	nextQueueNumber, err := r.GetNextQueueNumber(item.StoreID)
	if err != nil {
		return nil, fmt.Errorf("failed to get next queue number: %v", err)
	}
	item.QueueNumber = nextQueueNumber

	if item.RegistrationTime == "" {
		jst := time.FixedZone("Asia/Tokyo", 9*60*60)
		now := time.Now().In(jst)
		item.RegistrationTime = now.Format("2006-01-02T15:04:05.000+09:00")
	}

	if item.WaitingID == "" {
		now := time.Now()
		item.WaitingID = now.Format("20060102-150405") + "-" + fmt.Sprintf("%03d", now.Nanosecond()/1e6)
	}

	countFilter := bson.M{
		"store_id": item.StoreID,
		"status":   bson.M{"$in": []string{"waiting", "notified"}},
	}
	activeCount, err := collection.CountDocuments(ctx, countFilter)
	if err != nil {
		item.EstimatedWaitTime = 0
	} else {
		storeRepo := &MongoStoreRepo{}
		settings, err := storeRepo.GetSettings(item.StoreID)
		minutesPerTeam := 10
		if err == nil && settings.Settings.WaitingPolicy.EstimatedWaitTime > 0 {
			minutesPerTeam = settings.Settings.WaitingPolicy.EstimatedWaitTime
		}
		item.EstimatedWaitTime = CalculateEstimatedWaitTime(int(activeCount), minutesPerTeam)
	}

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
		"source":              item.Source,
	}

	result, err := collection.InsertOne(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("failed to insert waiting list item: %v", err)
	}
	item.ID = result.InsertedID.(primitive.ObjectID)

	return &item, nil
}

func (r *MongoWaitingListRepo) GetNextQueueNumber(storeID string) (int, error) {
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)
	businessDayStart := r.GetBusinessDayCutoff(storeID, now)
	return r.GetNextSequence(storeID, businessDayStart)
}

func (r *MongoWaitingListRepo) GetNextSequence(storeID string, businessDate time.Time) (int, error) {
	collection := db.GetCollection(DatabaseName, CollectionCounters)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dateStr := businessDate.Format("20060102")
	filter := bson.M{
		"_id": models.CounterID{
			StoreID: storeID,
			Date:    dateStr,
		},
	}
	update := bson.M{"$inc": bson.M{"seq": 1}}

	var updatedCounter models.Counter
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	err := collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedCounter)
	if err == nil {
		return updatedCounter.Seq, nil
	}
	if err != mongo.ErrNoDocuments {
		return 0, fmt.Errorf("failed to get next sequence: %w", err)
	}

	currentMax, err := r.getMaxQueueNumberInternal(storeID, businessDate)
	if err != nil {
		return 0, fmt.Errorf("failed to get max queue number during initialization: %w", err)
	}
	newSeq := currentMax + 1
	newCounter := models.Counter{
		ID: models.CounterID{
			StoreID: storeID,
			Date:    dateStr,
		},
		Seq: newSeq,
	}
	_, err = collection.InsertOne(ctx, newCounter)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return r.GetNextSequence(storeID, businessDate)
		}
		return 0, fmt.Errorf("failed to insert new counter: %w", err)
	}
	return newSeq, nil
}

func (r *MongoWaitingListRepo) getMaxQueueNumberInternal(storeID string, businessDate time.Time) (int, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	startStr := businessDate.Format("2006-01-02T15:04:05.000+09:00")
	opts := options.FindOne().SetSort(bson.D{{Key: "queue_number", Value: -1}})
	filter := bson.M{
		"store_id": storeID,
		"registration_time": bson.M{
			"$gte": startStr,
		},
	}

	var lastItem models.WaitingList
	err := collection.FindOne(ctx, filter, opts).Decode(&lastItem)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return 0, nil
		}
		return 0, err
	}
	return lastItem.QueueNumber, nil
}

// UpdateItemStatus 指定アイテムのステータスを更新する
func (r *MongoWaitingListRepo) UpdateItemStatus(storeID, waitingID, status string) error {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)
	currentTime := now.Format("2006-01-02T15:04:05.000+09:00")

	setFields := bson.M{"status": status}
	switch status {
	case "completed":
		setFields["entry_time"] = currentTime
	case "notified":
		setFields["called_time"] = currentTime
	}

	update := bson.M{"$set": setFields}
	filter := bson.M{"store_id": storeID, "waiting_id": waitingID}
	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("no waiting item found with waiting_id: %s", waitingID)
	}
	return nil
}

// ClearList 指定店舗の待機リストをクリアする
func (r *MongoWaitingListRepo) ClearList(storeID string) error {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)

	startOfDay := r.GetBusinessDayCutoff(storeID, now)
	endOfDay := startOfDay.Add(24 * time.Hour)

	filter := bson.M{
		"store_id": storeID,
		"registration_time": bson.M{
			"$gte": startOfDay.Format("2006-01-02T15:04:05.000+09:00"),
			"$lt":  endOfDay.Format("2006-01-02T15:04:05.000+09:00"),
		},
		"status": "waiting",
	}
	update := bson.M{"$set": bson.M{"status": "cancelled"}}
	_, err := collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return err
	}
	return nil
}

// GetAverageWaitingTime 指定店舗の平均待機時間（秒）を取得する
func (r *MongoWaitingListRepo) GetAverageWaitingTime(storeID string) (int, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	thirtyDaysAgo := time.Now().AddDate(0, 0, -30).Format(time.RFC3339)
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"store_id":          storeID,
			"entry_time":        bson.M{"$ne": nil},
			"registration_time": bson.M{"$gte": thirtyDaysAgo},
		}}},
		{{Key: "$addFields", Value: bson.M{
			"reg_date":   bson.M{"$toDate": "$registration_time"},
			"entry_date": bson.M{"$toDate": "$entry_time"},
		}}},
		{{Key: "$group", Value: bson.M{
			"_id":   nil,
			"count": bson.M{"$sum": 1},
			"avgWaitMillis": bson.M{
				"$avg": bson.M{
					"$subtract": []interface{}{"$entry_date", "$reg_date"},
				},
			},
		}}},
	}

	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	var result []struct {
		Count         int     `bson:"count"`
		AvgWaitMillis float64 `bson:"avgWaitMillis"`
	}
	if err = cursor.All(ctx, &result); err != nil {
		return 0, err
	}
	if len(result) == 0 || result[0].Count < 40 {
		return -1, nil
	}
	averageSeconds := int(result[0].AvgWaitMillis / 1000)
	return averageSeconds, nil
}

// GetUserWaitingListItem 特定店舗の特定ユーザーのウェイティングリスト項目を取得
func (r *MongoWaitingListRepo) GetUserWaitingListItem(storeID string) (*models.WaitingList, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	filter := bson.M{"store_id": storeID}

	var waitingListItem models.WaitingList
	err := collection.FindOne(ctx, filter).Decode(&waitingListItem)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &waitingListItem, nil
}

// GetActiveWaitingList 特定店舗の特定ユーザーのアクティブなウェイティングリストを取得
func (r *MongoWaitingListRepo) GetActiveWaitingList(storeID string, waitingID string) ([]models.WaitingList, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var waitingListData []models.WaitingList
	filter := bson.M{
		"store_id":   storeID,
		"waiting_id": waitingID,
		"status": bson.M{
			"$in": []string{"waiting", "notified"},
		},
	}
	opts := options.Find().SetSort(bson.M{"registration_time": 1})
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var waitingListItem models.WaitingList
		if err := cursor.Decode(&waitingListItem); err != nil {
			continue
		}
		waitingListData = append(waitingListData, waitingListItem)
	}

	if err := cursor.Err(); err != nil {
		return nil, err
	}

	storeRepo := &MongoStoreRepo{}
	settings, err := storeRepo.GetSettings(storeID)
	minutesPerTeam := 10
	if err == nil && settings.Settings.WaitingPolicy.EstimatedWaitTime > 0 {
		minutesPerTeam = settings.Settings.WaitingPolicy.EstimatedWaitTime
	}

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
				waitingListData[i].EstimatedWaitTime = 0
			} else {
				waitingListData[i].EstimatedWaitTime = CalculateEstimatedWaitTime(int(aheadCount), minutesPerTeam)
			}
		} else {
			waitingListData[i].EstimatedWaitTime = 0
		}
	}
	return waitingListData, nil
}

// UpdateWaitingStatus 特定のウェイティング項目のステータスを更新 (Return full object)
func (r *MongoWaitingListRepo) UpdateWaitingStatus(storeID, waitingID, status string) (*models.WaitingList, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)

	update := bson.M{"$set": bson.M{"status": status}}
	if status == "notified" {
		update["$set"].(bson.M)["called_time"] = now.Format(time.RFC3339)
	}

	filter := bson.M{"store_id": storeID, "waiting_id": waitingID}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
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

func CalculateEstimatedWaitTime(waitingCount int, minutesPerTeam int) int {
	if minutesPerTeam <= 0 {
		minutesPerTeam = 10
	}
	return waitingCount * minutesPerTeam
}
