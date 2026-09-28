package handlers

import (
	"context"
	"log"
	"net/http"
	"time"

	"yoyaku_mate_server/auth"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// StatsUserRepository 統計データの閲覧権限を確認するため、ユーザー情報とアクセス権限を検証するインターフェース
type StatsUserRepository interface {
	GetByFirebaseUID(uid string) (*models.User, error)
	CheckStorePermission(userID primitive.ObjectID, storeID, role, permission string) (bool, error)
}

// StatsStoreRepository 統計の基準タイムゾーン設定を確認するため、店舗基本情報を取得するインターフェース
type StatsStoreRepository interface {
	GetStoreData(storeID string) (*models.Store, error)
}

type StatisticsHandler struct {
	userRepo  StatsUserRepository
	storeRepo StatsStoreRepository
}

func NewStatisticsHandler(userRepo StatsUserRepository, storeRepo StatsStoreRepository) *StatisticsHandler {
	return &StatisticsHandler{userRepo: userRepo, storeRepo: storeRepo}
}

// 曜日別集計のラベル（Mongoの $dayOfWeek は 1=日曜〜7=土曜）
var weekdayLabels = [7]string{"日", "月", "火", "水", "木", "金", "土"}

// HandleGet は店舗の統計情報（今日 or 今週固定）を取得するリクエストを処理します
func (h *StatisticsHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// 権限チェック
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}

	idToken := authHeader[len("Bearer "):]
	firebaseUID, err := auth.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	user, err := h.userRepo.GetByFirebaseUID(firebaseUID)
	if err != nil || user == nil {
		utils.RespondWithError(w, "User not found", http.StatusUnauthorized)
		return
	}

	hasPermission, err := h.userRepo.CheckStorePermission(user.ID, storeID, user.Role, "")
	if err != nil || !hasPermission {
		utils.RespondWithError(w, "Permission denied", http.StatusForbidden)
		return
	}

	period := r.URL.Query().Get("period")
	if period != "weekly" {
		period = "auto" // "今日" がデフォルト。今日/今週以外は受け付けない
	}

	stats, err := h.CalculateStatistics(storeID, period)
	if err != nil {
		log.Printf("Failed to calculate statistics for store %s: %v", storeID, err)
		utils.RespondWithError(w, "Failed to calculate statistics", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, stats, http.StatusOK)
}

// CalculateStatistics は period="auto"(今日を時間帯別) / "weekly"(今週を曜日別、常に直近の週固定)
// のいずれかで統計を集計する。過去の期間を指定して遡ることはできない。
func (h *StatisticsHandler) CalculateStatistics(storeID, period string) (*models.StatisticsResponse, error) {
	// 店舗情報の取得（タイムゾーン確認のため）
	store, err := h.storeRepo.GetStoreData(storeID)
	locationName := "Asia/Tokyo"
	if err == nil && store != nil && store.Timezone != "" {
		locationName = store.Timezone
	}

	loc, err := time.LoadLocation(locationName)
	if err != nil {
		log.Printf("Failed to load location '%s', defaulting to Asia/Tokyo: %v", locationName, err)
		loc = time.FixedZone("Asia/Tokyo", 9*60*60)
		locationName = "Asia/Tokyo"
	}

	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	var startCurrent, endCurrent, startPrev time.Time
	if period == "weekly" {
		// 直近の日曜日〜土曜日（今週）固定。前週も同じ幅で比較する
		sunday := today.AddDate(0, 0, -int(today.Weekday()))
		startCurrent = sunday
		endCurrent = sunday.AddDate(0, 0, 7)
		startPrev = sunday.AddDate(0, 0, -7)
	} else {
		startCurrent = today
		endCurrent = today.AddDate(0, 0, 1)
		startPrev = today.AddDate(0, 0, -1)
	}

	collection := db.GetCollection(db.DatabaseName, db.CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	layout := "2006-01-02T15:04:05.000"
	matchStage := bson.D{{Key: "$match", Value: bson.D{
		{Key: "store_id", Value: storeID},
		{Key: "registration_time", Value: bson.D{
			{Key: "$gte", Value: startPrev.Format(layout)},
			{Key: "$lt", Value: endCurrent.Format(layout)},
		}},
	}}}

	addFieldsStage := bson.D{{Key: "$addFields", Value: bson.D{
		{Key: "reg_date_obj", Value: bson.D{
			{Key: "$dateFromString", Value: bson.D{{Key: "dateString", Value: "$registration_time"}}},
		}},
		{Key: "entry_date_obj", Value: bson.D{
			{Key: "$cond", Value: bson.A{
				bson.D{{Key: "$ne", Value: bson.A{"$entry_time", nil}}},
				bson.D{{Key: "$dateFromString", Value: bson.D{{Key: "dateString", Value: "$entry_time"}}}},
				nil,
			}},
		}},
	}}}

	// バケットキー: autoは時間帯(0-23), weeklyは曜日(1=日〜7=土)
	var bucketExpr bson.D
	if period == "weekly" {
		bucketExpr = bson.D{{Key: "$dayOfWeek", Value: bson.D{{Key: "date", Value: "$reg_date_obj"}, {Key: "timezone", Value: locationName}}}}
	} else {
		bucketExpr = bson.D{{Key: "$hour", Value: bson.D{{Key: "date", Value: "$reg_date_obj"}, {Key: "timezone", Value: locationName}}}}
	}

	// ステータス別(current/prev)にバケット集計するfacetを組み立てるヘルパー
	bucketFacet := func(status string, from, to time.Time) bson.A {
		return bson.A{
			bson.D{{Key: "$match", Value: bson.D{
				{Key: "status", Value: status},
				{Key: "registration_time", Value: bson.D{
					{Key: "$gte", Value: from.Format(layout)},
					{Key: "$lt", Value: to.Format(layout)},
				}},
			}}},
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: bucketExpr},
				{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
			}}},
		}
	}

	facetStage := bson.D{{Key: "$facet", Value: bson.D{
		{Key: "visitor_current", Value: bucketFacet("completed", startCurrent, endCurrent)},
		{Key: "visitor_prev", Value: bucketFacet("completed", startPrev, startCurrent)},
		{Key: "cancelled_current", Value: bucketFacet("cancelled", startCurrent, endCurrent)},
		{Key: "cancelled_prev", Value: bucketFacet("cancelled", startPrev, startCurrent)},
		{Key: "no_show_current", Value: bucketFacet("no_show", startCurrent, endCurrent)},
		{Key: "no_show_prev", Value: bucketFacet("no_show", startPrev, startCurrent)},

		// ハイライト用の期間合計 (現在の期間)
		{Key: "totals_current", Value: bson.A{
			bson.D{{Key: "$match", Value: bson.D{
				{Key: "registration_time", Value: bson.D{
					{Key: "$gte", Value: startCurrent.Format(layout)},
					{Key: "$lt", Value: endCurrent.Format(layout)},
				}},
			}}},
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: nil},
				{Key: "total_visitors", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "completed"}}}, 1, 0}}}}}},
				{Key: "total_cancelled", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "cancelled"}}}, 1, 0}}}}}},
				{Key: "total_no_show", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "no_show"}}}, 1, 0}}}}}},
				{Key: "total_count", Value: bson.D{{Key: "$sum", Value: 1}}},
			}}},
		}},
		// 前期間(前日 or 前週)の来店者数のみ（成長率算出用）
		{Key: "totals_prev", Value: bson.A{
			bson.D{{Key: "$match", Value: bson.D{
				{Key: "registration_time", Value: bson.D{
					{Key: "$gte", Value: startPrev.Format(layout)},
					{Key: "$lt", Value: startCurrent.Format(layout)},
				}},
			}}},
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: nil},
				{Key: "total_visitors", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "completed"}}}, 1, 0}}}}}},
			}}},
		}},
		// 平均待ち時間 (現在の期間)
		{Key: "wait_times_current", Value: bson.A{
			bson.D{{Key: "$match", Value: bson.D{
				{Key: "registration_time", Value: bson.D{{Key: "$gte", Value: startCurrent.Format(layout)}, {Key: "$lt", Value: endCurrent.Format(layout)}}},
				{Key: "status", Value: "completed"},
				{Key: "entry_date_obj", Value: bson.D{{Key: "$ne", Value: nil}}},
			}}},
			bson.D{{Key: "$project", Value: bson.D{
				{Key: "wait_duration", Value: bson.D{{Key: "$divide", Value: bson.A{bson.D{{Key: "$subtract", Value: bson.A{"$entry_date_obj", "$reg_date_obj"}}}, 1000}}}},
			}}},
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: nil},
				{Key: "avg_wait", Value: bson.D{{Key: "$avg", Value: "$wait_duration"}}},
			}}},
		}},
	}}}

	cursor, err := collection.Aggregate(ctx, mongo.Pipeline{matchStage, addFieldsStage, facetStage})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	result := bson.M{}
	if len(results) > 0 {
		result = results[0]
	}

	response := &models.StatisticsResponse{Period: period}

	// バケット数とラベル生成（autoは24時間、weeklyは7曜日）
	bucketCount := 24
	if period == "weekly" {
		bucketCount = 7
	}
	labelFor := func(bucketIndex int) string {
		if period == "weekly" {
			return weekdayLabels[bucketIndex]
		}
		return time.Date(0, 1, 1, bucketIndex, 0, 0, 0, time.UTC).Format("15")
	}
	// Mongoの $dayOfWeek は1始まり(1=日)なので、weeklyの場合は-1して0始まりに揃える
	toBucketMap := func(facetName string) map[int]int {
		m := make(map[int]int)
		if arr, ok := result[facetName].(bson.A); ok {
			for _, item := range arr {
				im := item.(bson.M)
				id := int(im["_id"].(int32))
				if period == "weekly" {
					id -= 1
				}
				m[id] = int(im["count"].(int32))
			}
		}
		return m
	}
	buildChart := func(currentFacet, prevFacet string) []models.ChartData {
		curMap := toBucketMap(currentFacet)
		prevMap := toBucketMap(prevFacet)
		chart := make([]models.ChartData, 0, bucketCount)
		for i := 0; i < bucketCount; i++ {
			chart = append(chart, models.ChartData{
				Label:     labelFor(i),
				Value:     curMap[i],
				PrevValue: prevMap[i],
			})
		}
		return chart
	}

	response.VisitorChart = buildChart("visitor_current", "visitor_prev")
	response.CancelledChart = buildChart("cancelled_current", "cancelled_prev")
	response.NoShowChart = buildChart("no_show_current", "no_show_prev")

	// --- ハイライト集計 ---
	visitorTotal, cancelledTotal, noShowTotal, totalCount := 0, 0, 0, 0
	if arr, ok := result["totals_current"].(bson.A); ok && len(arr) > 0 {
		m := arr[0].(bson.M)
		visitorTotal = int(m["total_visitors"].(int32))
		cancelledTotal = int(m["total_cancelled"].(int32))
		noShowTotal = int(m["total_no_show"].(int32))
		totalCount = int(m["total_count"].(int32))
	}
	prevVisitorTotal := 0
	if arr, ok := result["totals_prev"].(bson.A); ok && len(arr) > 0 {
		m := arr[0].(bson.M)
		prevVisitorTotal = int(m["total_visitors"].(int32))
	}

	response.VisitorTotal = visitorTotal
	response.CancelledTotal = cancelledTotal
	response.NoShowTotal = noShowTotal
	response.VisitorGrowthRate = CalculateGrowthRate(visitorTotal, prevVisitorTotal)
	if totalCount > 0 {
		response.NoShowRate = (float64(cancelledTotal+noShowTotal) / float64(totalCount)) * 100
	}

	response.AverageWaitTime = "--分"
	if arr, ok := result["wait_times_current"].(bson.A); ok && len(arr) > 0 {
		m := arr[0].(bson.M)
		if avg, ok := m["avg_wait"].(float64); ok {
			response.WaitTimeSeconds = int(avg)
			response.AverageWaitTime = utils.FormatDuration(int(avg))
		}
	}

	return response, nil
}

func CalculateGrowthRate(current, previous int) float64 {
	if previous == 0 {
		if current > 0 {
			return 100.0 // 100% 成長 (技術的には無限大ですが、新規トラフィックを示すため100%とします)
		}
		return 0.0
	}
	return (float64(current-previous) / float64(previous)) * 100
}
