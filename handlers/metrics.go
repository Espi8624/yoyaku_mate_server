package handlers

import (
	"context"
	"net/http"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

	"go.mongodb.org/mongo-driver/bson"
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
