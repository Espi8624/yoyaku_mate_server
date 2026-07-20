package handlers

import (
	"context"
	"log"
	"math"
	"net/http"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/events"
	"yoyaku_mate_server/metrics"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// - MongoDBからエラー発生タイプ別の合計数をリアルタイムでカウントする
// - 管理者ダッシュボード上部サマリーカードの統計データとして返却する
func GetErrorMetricsHandler(w http.ResponseWriter, r *http.Request) {
	collection := db.GetCollection(db.DatabaseName, db.CollectionErrorLogs)
	if collection == nil {
		utils.RespondWithError(w, "Database error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count500, _ := collection.CountDocuments(ctx, bson.M{"error_type": "500_INTERNAL_ERROR"})
	count400, _ := collection.CountDocuments(ctx, bson.M{"error_type": "400_BAD_REQUEST"})
	countDB, _ := collection.CountDocuments(ctx, bson.M{"error_type": "DATABASE_ERROR"})
	countSSE, _ := collection.CountDocuments(ctx, bson.M{"error_type": "SSE_DISCONNECT"})

	stats := models.ErrorMetrics{
		Count500: count500,
		Count400: count400,
		CountDB:  countDB,
		CountSSE: countSSE,
	}

	utils.RespondWithJSON(w, stats, http.StatusOK)
}

// - MongoDBに保存された詳細なエラーログを最新順にソートして最大50件取得する
// - 管理者ダッシュボード下部のテーブルデータとして返却する
func GetErrorLogsHandler(w http.ResponseWriter, r *http.Request) {
	collection := db.GetCollection(db.DatabaseName, db.CollectionErrorLogs)
	if collection == nil {
		utils.RespondWithError(w, "Database error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	findOptions := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(50) // fetch latest 50 logs

	cursor, err := collection.Find(ctx, bson.M{}, findOptions)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch error logs", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var logs []models.ErrorLog
	if err = cursor.All(ctx, &logs); err != nil {
		utils.RespondWithError(w, "Failed to decode error logs", http.StatusInternalServerError)
		return
	}

	if logs == nil {
		logs = []models.ErrorLog{}
	}

	utils.RespondWithJSON(w, logs, http.StatusOK)
}

// - 直近24時間における累積要求件数、成功率、および直近1時間以内のPeak TPS統計を演算して返却するハンドラー
func GetRequestMetricsHandler(w http.ResponseWriter, r *http.Request) {
	collection := db.GetCollection(db.DatabaseName, db.CollectionRequestLogs)
	if collection == nil {
		utils.RespondWithError(w, "Database connection error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)
	oneHourAgo := now.Add(-1 * time.Hour)

	// - 24時間以内の総リクエスト数を集計
	total24h, err := collection.CountDocuments(ctx, bson.M{
		"timestamp": bson.M{"$gte": yesterday},
	})
	if err != nil {
		log.Printf("Failed to count total requests: %v", err)
		utils.RespondWithError(w, "Failed to calculate total requests", http.StatusInternalServerError)
		return
	}

	// - 24時間以内の成功したリクエスト数を集計 (status_code < 400)
	success24h, err := collection.CountDocuments(ctx, bson.M{
		"timestamp":   bson.M{"$gte": yesterday},
		"status_code": bson.M{"$lt": 400},
	})
	if err != nil {
		log.Printf("Failed to count success requests: %v", err)
		utils.RespondWithError(w, "Failed to calculate success requests", http.StatusInternalServerError)
		return
	}

	successRate := 100.0
	if total24h > 0 {
		successRate = float64(success24h) / float64(total24h) * 100.0
	}

	// - 直近1時間における秒単位の要求数集計によるPeak TPSの算出 (Aggregationを使用)
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{{Key: "timestamp", Value: bson.D{{Key: "$gte", Value: oneHourAgo}}}}}},
		{{Key: "$project", Value: bson.D{
			{Key: "second", Value: bson.D{
				{Key: "$dateToString", Value: bson.D{
					{Key: "format", Value: "%Y-%m-%d %H:%M:%S"},
					{Key: "date", Value: "$timestamp"},
				}},
			}},
		}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$second"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "peakTPS", Value: bson.D{{Key: "$max", Value: "$count"}}},
		}}},
	}

	var peakTPS int64 = 0
	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		log.Printf("Peak TPS aggregation error: %v", err)
		// - 集計エラー時は0として処理を続行
	} else {
		defer cursor.Close(ctx)
		var results []bson.M
		if err := cursor.All(ctx, &results); err == nil && len(results) > 0 {
			if maxVal, ok := results[0]["peakTPS"]; ok {
				switch v := maxVal.(type) {
				case int32:
					peakTPS = int64(v)
				case int64:
					peakTPS = v
				}
			}
		}
	}

	stats := models.RequestMetrics{
		TotalRequests24h: total24h,
		SuccessRate:      successRate,
		PeakTPS1h:        peakTPS,
	}

	utils.RespondWithJSON(w, stats, http.StatusOK)
}

// - MongoDBに保存された詳細なリクエストログを最新順にソートして最大50件取得するハンドラー
func GetRequestLogsHandler(w http.ResponseWriter, r *http.Request) {
	collection := db.GetCollection(db.DatabaseName, db.CollectionRequestLogs)
	if collection == nil {
		utils.RespondWithError(w, "Database connection error", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	findOptions := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(50)

	cursor, err := collection.Find(ctx, bson.M{}, findOptions)
	if err != nil {
		log.Printf("Failed to fetch request logs: %v", err)
		utils.RespondWithError(w, "Failed to fetch request logs", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var logs []models.RequestLog
	if err = cursor.All(ctx, &logs); err != nil {
		log.Printf("Failed to decode request logs: %v", err)
		utils.RespondWithError(w, "Failed to decode request logs", http.StatusInternalServerError)
		return
	}

	if logs == nil {
		logs = []models.RequestLog{}
	}

	utils.RespondWithJSON(w, logs, http.StatusOK)
}

// - リアルタイム同時接続者数およびDAU/MAU統計を集計して返却するハンドラー
func GetActiveUserMetricsHandler(w http.ResponseWriter, r *http.Request) {
	// 1. リアルタイム同時接続者数 (インメモリから即座に取得)
	currentActive := metrics.GetRequestTracker().GetActiveUsersCount()

	dauCollection := db.GetCollection(db.DatabaseName, db.CollectionDailyActiveUsers)
	if dauCollection == nil {
		// DB接続失敗時もエラーを返却せずダッシュボードのクラッシュを防ぐためにフォールバック値を提供
		log.Println("DB connection failure in GetActiveUserMetricsHandler")
		utils.RespondWithJSON(w, models.ActiveUserMetrics{
			CurrentActiveUsers: currentActive,
			DailyActiveUsers:   currentActive,
			MonthlyActiveUsers: currentActive,
		}, http.StatusOK)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 2. 1日アクティブユーザー数 (DAU) - 本日の日付文字列を基準にユニークIP数を集計
	todayStr := time.Now().Format("2006-01-02")
	dauCount, err := dauCollection.CountDocuments(ctx, bson.M{"date": todayStr})
	if err != nil {
		log.Printf("Failed to count DAU: %v", err)
		dauCount = currentActive // Fallback
	}

	// 3. 月間アクティブユーザー数 (MAU) - 直近30日以内のユニークIP数を集計
	thirtyDaysAgo := time.Now().Add(-30 * 24 * time.Hour)
	
	// 重複なくユニークなclient_ipの数をカウント
	distinctIPs, err := dauCollection.Distinct(ctx, "client_ip", bson.M{
		"timestamp": bson.M{"$gte": thirtyDaysAgo},
	})
	
	var mauCount int64
	if err != nil {
		log.Printf("Failed to count MAU: %v", err)
		mauCount = dauCount // Fallback to DAU
	} else {
		mauCount = int64(len(distinctIPs))
	}

	// もしMAUやDAUがリアルタイム同時接続者数より少ない場合は補正処理 (即時フォールバック)
	if dauCount < currentActive {
		dauCount = currentActive
	}
	if mauCount < dauCount {
		mauCount = dauCount
	}

	metricsData := models.ActiveUserMetrics{
		CurrentActiveUsers: currentActive,
		DailyActiveUsers:   dauCount,
		MonthlyActiveUsers: mauCount,
	}

	utils.RespondWithJSON(w, metricsData, http.StatusOK)
}

// GetSSEMetricsHandler は2つのSSEブローカーのリアルタイム接続状況を取得して返します
// DBへのアクセスを伴わずインメモリ参照のみ行うため、応答速度が非常に高速です
func GetSSEMetricsHandler(w http.ResponseWriter, r *http.Request) {
	// 店舗待ちリストブローカーの統計取得
	storeStats := events.GetBroker().GetStats()
	// 個別待ち顧客ブローカーの統計取得
	userStats := events.GetWaitingUserBroker().GetStats()

	totalConnections := storeStats.TotalConnections + userStats.TotalConnections

	// 接続数に基づくヘルス状態の判定
	health := "IDLE"
	if totalConnections > 0 {
		health = "HEALTHY"
	}

	result := models.SSEMetrics{
		StoreBroker: models.SSEBrokerStats{
			ActiveKeys:       storeStats.ActiveKeys,
			TotalConnections: storeStats.TotalConnections,
			AvgUptimeSeconds: storeStats.AvgUptimeSeconds,
		},
		WaitingUserBroker: models.SSEBrokerStats{
			ActiveKeys:       userStats.ActiveKeys,
			TotalConnections: userStats.TotalConnections,
			AvgUptimeSeconds: userStats.AvgUptimeSeconds,
		},
		TotalConnections: totalConnections,
		Health:           health,
	}

	utils.RespondWithJSON(w, result, http.StatusOK)
}

// - クエリパラメータ ?range=5m|1h|24h を受け取り、指定期間内の
// - 全体サマリー(avg/p95/p99/error_rate)と遅いエンドポイント上位10件を集計して返すハンドラー
// - MongoDB 7.0以上の $percentile 演算子を使用
func GetResponseTimeMetricsHandler(w http.ResponseWriter, r *http.Request) {
	collection := db.GetCollection(db.DatabaseName, db.CollectionRequestLogs)
	if collection == nil {
		utils.RespondWithError(w, "Database connection error", http.StatusInternalServerError)
		return
	}

	// - クエリパラメータからtime range取得 (デフォルト: 1h)
	rangeParam := r.URL.Query().Get("range")
	var since time.Time
	switch rangeParam {
	case "5m":
		since = time.Now().UTC().Add(-5 * time.Minute)
	case "24h":
		since = time.Now().UTC().Add(-24 * time.Hour)
	default: // "1h" or anything else
		since = time.Now().UTC().Add(-1 * time.Hour)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	matchStage := bson.D{{Key: "$match", Value: bson.D{
		{Key: "timestamp", Value: bson.D{{Key: "$gte", Value: since}}},
	}}}

	// --- 1. エンドポイント別集計 (上位10件) ---
	endpointPipeline := mongo.Pipeline{
		matchStage,
		// - path+methodごとにグループ化し、avg/p95/p99/件数/エラー件数を集計
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{
				{Key: "path", Value: "$path"},
				{Key: "method", Value: "$method"},
			}},
			{Key: "avg_ms", Value: bson.D{{Key: "$avg", Value: "$response_time"}}},
			{Key: "p95_ms", Value: bson.D{
				{Key: "$percentile", Value: bson.D{
					{Key: "input", Value: "$response_time"},
					{Key: "p", Value: bson.A{0.95}},
					{Key: "method", Value: "approximate"},
				}},
			}},
			{Key: "p99_ms", Value: bson.D{
				{Key: "$percentile", Value: bson.D{
					{Key: "input", Value: "$response_time"},
					{Key: "p", Value: bson.A{0.99}},
					{Key: "method", Value: "approximate"},
				}},
			}},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "error_count", Value: bson.D{
				{Key: "$sum", Value: bson.D{
					{Key: "$cond", Value: bson.A{
						bson.D{{Key: "$gte", Value: bson.A{"$status_code", 400}}},
						1, 0,
					}},
				}},
			}},
		}}},
		// - avg_msの降順で上位10件に絞り込む
		{{Key: "$sort", Value: bson.D{{Key: "avg_ms", Value: -1}}}},
		{{Key: "$limit", Value: 10}},
	}

	endpointCursor, err := collection.Aggregate(ctx, endpointPipeline)
	if err != nil {
		log.Printf("Endpoint latency aggregation error: %v", err)
		utils.RespondWithError(w, "Failed to aggregate endpoint latency", http.StatusInternalServerError)
		return
	}
	defer endpointCursor.Close(ctx)

	var rawEndpoints []bson.M
	if err := endpointCursor.All(ctx, &rawEndpoints); err != nil {
		log.Printf("Endpoint cursor decode error: %v", err)
		utils.RespondWithError(w, "Failed to decode endpoint latency", http.StatusInternalServerError)
		return
	}

	// - bson.Mから型安全なEndpointLatencyスライスへ変換
	endpoints := make([]models.EndpointLatency, 0, len(rawEndpoints))
	for _, raw := range rawEndpoints {
		ep := models.EndpointLatency{}
		if id, ok := raw["_id"].(bson.M); ok {
			ep.Path, _ = id["path"].(string)
			ep.Method, _ = id["method"].(string)
		}
		ep.AvgMs = math.Round(toFloat64(raw["avg_ms"])*10) / 10
		// - $percentileはスライスで返すため最初の要素を取得
		if arr, ok := raw["p95_ms"].(bson.A); ok && len(arr) > 0 {
			ep.P95Ms = math.Round(toFloat64(arr[0])*10) / 10
		}
		if arr, ok := raw["p99_ms"].(bson.A); ok && len(arr) > 0 {
			ep.P99Ms = math.Round(toFloat64(arr[0])*10) / 10
		}
		ep.Count = toInt64(raw["count"])
		errorCount := toInt64(raw["error_count"])
		if ep.Count > 0 {
			ep.ErrorPct = math.Round(float64(errorCount)/float64(ep.Count)*100*10) / 10
		}
		endpoints = append(endpoints, ep)
	}

	// --- 2. 全体サマリー集計 ---
	summaryPipeline := mongo.Pipeline{
		matchStage,
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "avg_ms", Value: bson.D{{Key: "$avg", Value: "$response_time"}}},
			{Key: "p95_ms", Value: bson.D{
				{Key: "$percentile", Value: bson.D{
					{Key: "input", Value: "$response_time"},
					{Key: "p", Value: bson.A{0.95}},
					{Key: "method", Value: "approximate"},
				}},
			}},
			{Key: "p99_ms", Value: bson.D{
				{Key: "$percentile", Value: bson.D{
					{Key: "input", Value: "$response_time"},
					{Key: "p", Value: bson.A{0.99}},
					{Key: "method", Value: "approximate"},
				}},
			}},
			{Key: "total", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "error_count", Value: bson.D{
				{Key: "$sum", Value: bson.D{
					{Key: "$cond", Value: bson.A{
						bson.D{{Key: "$gte", Value: bson.A{"$status_code", 400}}},
						1, 0,
					}},
				}},
			}},
		}}},
	}

	summaryCursor, err := collection.Aggregate(ctx, summaryPipeline)
	if err != nil {
		log.Printf("Summary latency aggregation error: %v", err)
		utils.RespondWithError(w, "Failed to aggregate summary", http.StatusInternalServerError)
		return
	}
	defer summaryCursor.Close(ctx)

	var summaryRaw []bson.M
	if err := summaryCursor.All(ctx, &summaryRaw); err != nil {
		log.Printf("Summary cursor decode error: %v", err)
		utils.RespondWithError(w, "Failed to decode summary", http.StatusInternalServerError)
		return
	}

	summary := models.ResponseTimeSummary{}
	if len(summaryRaw) > 0 {
		raw := summaryRaw[0]
		summary.AvgMs = math.Round(toFloat64(raw["avg_ms"])*10) / 10
		if arr, ok := raw["p95_ms"].(bson.A); ok && len(arr) > 0 {
			summary.P95Ms = math.Round(toFloat64(arr[0])*10) / 10
		}
		if arr, ok := raw["p99_ms"].(bson.A); ok && len(arr) > 0 {
			summary.P99Ms = math.Round(toFloat64(arr[0])*10) / 10
		}
		total := toInt64(raw["total"])
		errorCount := toInt64(raw["error_count"])
		if total > 0 {
			summary.ErrorRatePct = math.Round(float64(errorCount)/float64(total)*100*10) / 10
		}
	}

	result := models.ResponseTimeMetrics{
		Summary:   summary,
		Endpoints: endpoints,
	}

	utils.RespondWithJSON(w, result, http.StatusOK)
}

// - interface{}から float64へ安全に型変換するヘルパー
func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	}
	return 0
}

// - interface{}から int64へ安全に型変換するヘルパー
func toInt64(v interface{}) int64 {
	switch val := v.(type) {
	case int32:
		return int64(val)
	case int64:
		return val
	case float64:
		return int64(val)
	}
	return 0
}
