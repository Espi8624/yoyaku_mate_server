package data

import (
	"fmt"
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"sort"

	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/net/context"
)

// Helper: 店舗の営業開始時間に基づいて、現在の「営業日」の開始時刻(Cutoff)を計算する
// 基本ロジック: その日の開店時間 - 1時間 を境界とする
// 例: 10:00開店 -> 09:00 Cutoff.
//
//	17:00開店 -> 16:00 Cutoff.
//
// 設定がない場合やエラー時はデフォルト(04:00 AM)を使用
func GetBusinessDayCutoff(storeID string, now time.Time) time.Time {
	// デフォルト値: 04:00 AM
	defaultCutoff := time.Date(now.Year(), now.Month(), now.Day(), 4, 0, 0, 0, now.Location())

	settings, err := GetStoreSettingsData(storeID)
	if err != nil {
		// 設定取得失敗時はデフォルトを使用 (時間帯判定のため前日チェックが必要)
		// 単純化のため、現在の時刻と比較して決定
		if now.Hour() < 4 {
			return defaultCutoff.AddDate(0, 0, -1)
		}
		return defaultCutoff
	}

	// 24時間営業フラグのチェック
	if settings.Settings.Is24Hours {
		// ResetTimeを使用 (例: "06:00")
		resetParts := strings.Split(settings.Settings.ResetTime, ":")
		if len(resetParts) != 2 {
			// ResetTimeが無効な場合はデフォルト(06:00)を使用
			resetParts = []string{"06", "00"}
		}

		resetHour, _ := strconv.Atoi(resetParts[0])
		resetMin, _ := strconv.Atoi(resetParts[1])

		// その日のResetTime
		cutoffTime := time.Date(now.Year(), now.Month(), now.Day(), resetHour, resetMin, 0, 0, now.Location())

		// もし現在時刻がその日のCutoffより前なら、まだ「前日の営業日」とみなす
		if now.Before(cutoffTime) {
			return cutoffTime.AddDate(0, 0, -1)
		}
		return cutoffTime
	}

	// 今日の曜日 (例: "Monday", "Tuesday"...)
	weekday := now.Weekday().String()

	// 営業時間を取得
	dayHours, ok := settings.Settings.OperatingHours[weekday]
	if !ok || dayHours.Start == "" {
		// 今日の設定がない場合、デフォルトを使用
		// Log logic check: if now < 4, treating as previous day?
		// Fallback to simple fixed 4am logic for safety
		if now.Hour() < 4 {
			return defaultCutoff.AddDate(0, 0, -1)
		}
		return defaultCutoff
	}

	// "10:00" -> 10, 0
	parts := strings.Split(dayHours.Start, ":")
	if len(parts) != 2 {
		if now.Hour() < 4 {
			return defaultCutoff.AddDate(0, 0, -1)
		}
		return defaultCutoff
	}
	startHour, _ := strconv.Atoi(parts[0])
	startMin, _ := strconv.Atoi(parts[1])

	// Cutoff = OpenTime - 1 hour
	cutoffHour := startHour - 1
	if cutoffHour < 0 {
		cutoffHour = 23 // 前日の23時 (稀なケース)
		// 日付計算が複雑になるため、0時未満になる場合は前日扱いにする必要があるが
		// Dateコンストラクタは負の値を正規化しないため注意。
		// ここでは単純に StartHour >= 1 前提、もしくは 0時開店なら 23時リセット。
	}

	cutoffTime := time.Date(now.Year(), now.Month(), now.Day(), cutoffHour, startMin, 0, 0, now.Location())

	// もし現在時刻がその日のCutoffより前なら、まだ「前日の営業日」とみなす
	if now.Before(cutoffTime) {
		return cutoffTime.AddDate(0, 0, -1)
	}

	// もし現在時刻がCutoffを過ぎていれば、今日の営業日
	return cutoffTime
}

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

	// 営業日ウィンドウの設定 (Dynamic Cutoff)
	windowStart := GetBusinessDayCutoff(storeID, now)
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

	// N+1 最適化: 店舗設定はループの外で1回だけ取得する
	settings, err := GetStoreSettingsData(storeID)
	minutesPerTeam := 10 // default
	if err == nil && settings.Settings.WaitingPolicy.EstimatedWaitTime > 0 {
		minutesPerTeam = settings.Settings.WaitingPolicy.EstimatedWaitTime
	}

	// 3. 全体リストを回しながら、アクティブなアイテムの場合、その順序に基づいて時間を計算
	for i := range waitingListData {
		if waitingListData[i].Status == "waiting" || waitingListData[i].Status == "notified" {
			// アクティブリスト内でのインデックスを探す
			// activeItemsはソートされているので、自身のQueueNumberより小さいものの数が待ち組数
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

	// 現在の営業日の開始時刻(Cutoff)を計算 (Dynamic)
	businessDayStart := GetBusinessDayCutoff(storeID, now)

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
	// 以前はここで waiting_id が必須だったが、今はサーバー側で生成するためチェックしない。
	// if item.WaitingID == "" {
	// 	return nil, fmt.Errorf("waiting_id is required")
	// }

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
	// WaitingIDをサーバー側で生成 (Collision防止: Milliseconds追加)
	// Clientからのwaiting_idは無視し、常にサーバー時間ベースで生成する
	now := time.Now()
	// Format: YYYYMMDD-HHmmss-SSS (e.g., 20240201-123456-789)
	item.WaitingID = now.Format("20060102-150405") + "-" + fmt.Sprintf("%03d", now.Nanosecond()/1e6)

	// UserID (firebase_uid) はAuth tokenから設定済み (Handler側)
	// StoreID はHandler側で設定済み

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
		"source":              item.Source,
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

// 特定店舗の次の利用可能なキュー番号を返却 (営業日ごとにリセット)
func GetNextQueueNumber(storeID string) (int, error) {
	// 営業日の開始日時を計算 (Dynamic Cutoff)
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)
	businessDayStart := GetBusinessDayCutoff(storeID, now)

	// Atomic Counterを使用
	return GetNextSequence(storeID, businessDayStart)
}

// 特定店舗の今日のウェイティングリストをキャンセル状態に更新
func ClearWaitingList(storeID string) error {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 日本時間帯設定
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)

	// 営業日ウィンドウの設定 (Dynamic)
	startOfDay := GetBusinessDayCutoff(storeID, now)
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

	// フィルター設定: storeIDとwaitingID、statusでフィルタリング
	// 日付フィルタ(registration_time)は削除：waiting_idが一意であるため、厳密な日付チェックは不要であり、
	// フォーマットやタイムゾーンの微妙な差異による404エラーを防ぐため。
	filter := bson.M{
		"store_id":   storeID,
		"waiting_id": waitingID,
		"status": bson.M{
			"$in": []string{"waiting", "notified"},
		},
	}

	log.Printf("[GetActiveWaitingList] Filter: %+v", filter)

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

	// N+1 最適化: 店舗設定はループの外で1回だけ取得する
	settings, err := GetStoreSettingsData(storeID)
	minutesPerTeam := 10 // default
	if err == nil && settings.Settings.WaitingPolicy.EstimatedWaitTime > 0 {
		minutesPerTeam = settings.Settings.WaitingPolicy.EstimatedWaitTime
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
// MongoDB Aggregationパイプラインで最近30日間の平均値のみをDB内で演算して取得
func GetAverageWaitingTime(storeID string) (int, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 最近 30日 (性能及び現実的な体感大気時間を反映するため)
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30).Format(time.RFC3339)

	pipeline := mongo.Pipeline{
		// 1. 条件に該当する要素をフィルタリング (最近30日以内、entry_timeが存在する要素)
		{{Key: "$match", Value: bson.M{
			"store_id":          storeID,
			"entry_time":        bson.M{"$ne": nil},
			"registration_time": bson.M{"$gte": thirtyDaysAgo},
		}}},
		// 2. 文字列の日付をDateオブジェクトに変換
		{{Key: "$addFields", Value: bson.M{
			"reg_date":   bson.M{"$toDate": "$registration_time"},
			"entry_date": bson.M{"$toDate": "$entry_time"},
		}}},
		// 3. グループ化して個数と待機時間(ミリ秒 -> 秒)の平均を計算
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
		log.Printf("Failed to aggregate average wait time: %v", err)
		return 0, err
	}
	defer cursor.Close(ctx)

	var result []struct {
		Count         int     `bson:"count"`
		AvgWaitMillis float64 `bson:"avgWaitMillis"`
	}

	if err = cursor.All(ctx, &result); err != nil {
		log.Printf("Failed to decode aggregation result: %v", err)
		return 0, err
	}

	if len(result) == 0 || result[0].Count < 40 {
		return -1, nil // 40件未満なら-1を返す
	}

	// ミリ秒を秒に変換
	averageSeconds := int(result[0].AvgWaitMillis / 1000)
	return averageSeconds, nil
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
