package handlers

import (
	"context"
	"log"
	"net/http"
	"time"
	"yoyaku_mate_server/db"
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
