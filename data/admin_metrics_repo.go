package data

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoAdminMetricsRepo 管理者ダッシュボード向けメトリクスの読み取り専用リポジトリ実装
// - 書き込み側(バッファリング・バッチ保存)は metrics パッケージが担う
type MongoAdminMetricsRepo struct{}

// metricsCollection メトリクス系コレクションを取得する。未接続なら ErrDatabaseUnavailable
func metricsCollection(name string) (*mongo.Collection, error) {
	collection := db.GetCollection(db.DatabaseName, name)
	if collection == nil {
		return nil, ErrDatabaseUnavailable
	}
	return collection, nil
}

// CountErrorsByType エラー発生タイプ別の合計数を数える (管理者ダッシュボード上部のサマリーカード用)
// - 種別ごとのカウント失敗は0件として扱う (1種別の失敗でサマリー全体を落とさないため)
func (r *MongoAdminMetricsRepo) CountErrorsByType() (models.ErrorMetrics, error) {
	collection, err := metricsCollection(db.CollectionErrorLogs)
	if err != nil {
		return models.ErrorMetrics{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count500, _ := collection.CountDocuments(ctx, bson.M{"error_type": "500_INTERNAL_ERROR"})
	count400, _ := collection.CountDocuments(ctx, bson.M{"error_type": "400_BAD_REQUEST"})
	countDB, _ := collection.CountDocuments(ctx, bson.M{"error_type": "DATABASE_ERROR"})
	countSSE, _ := collection.CountDocuments(ctx, bson.M{"error_type": "SSE_DISCONNECT"})

	return models.ErrorMetrics{
		Count500: count500,
		Count400: count400,
		CountDB:  countDB,
		CountSSE: countSSE,
	}, nil
}

// ListErrorLogs 詳細なエラーログを最新順に最大 limit 件取得する
//   - errorType が空なら SSE_DISCONNECT を除外する。
//     正常な切断 (客が画面を閉じた) は接続1本につき1件記録されるため件数が圧倒的に多く、
//     最新50件がこれで埋まって本当の500がウィンドウの外へ押し出される。
//     障害時に最初に開く画面がノイズで埋まっているのが一番困る
//   - 件数自体はサマリ (CountErrorsByType の CountSSE) で見えるため、
//     ここで省いても情報は失われない
//   - errorType を指定すればその種別だけを返す。SSE_DISCONNECT を明示指定すれば
//     切断だけを追うこともできる (error_type には idx_error_type がある)
func (r *MongoAdminMetricsRepo) ListErrorLogs(errorType string, limit int64) ([]models.ErrorLog, error) {
	collection, err := metricsCollection(db.CollectionErrorLogs)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"error_type": bson.M{"$ne": "SSE_DISCONNECT"}}
	if errorType != "" {
		filter = bson.M{"error_type": errorType}
	}

	logs := []models.ErrorLog{}
	if err := findLatest(ctx, collection, filter, limit, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

// CountRequestsSince since 以降のリクエスト数を数える。successOnly なら status_code < 400 のみ
func (r *MongoAdminMetricsRepo) CountRequestsSince(since time.Time, successOnly bool) (int64, error) {
	collection, err := metricsCollection(db.CollectionRequestLogs)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"timestamp": bson.M{"$gte": since}}
	if successOnly {
		filter["status_code"] = bson.M{"$lt": 400}
	}
	return collection.CountDocuments(ctx, filter)
}

// PeakTPSSince since 以降の秒単位リクエスト数の最大値 (Peak TPS) を集計する
func (r *MongoAdminMetricsRepo) PeakTPSSince(since time.Time) (int64, error) {
	collection, err := metricsCollection(db.CollectionRequestLogs)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{{Key: "timestamp", Value: bson.D{{Key: "$gte", Value: since}}}}}},
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

	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, nil
	}
	return toInt64(results[0]["peakTPS"]), nil
}

// ListRequestLogs 詳細なリクエストログを最新順に最大 limit 件取得する
func (r *MongoAdminMetricsRepo) ListRequestLogs(limit int64) ([]models.RequestLog, error) {
	collection, err := metricsCollection(db.CollectionRequestLogs)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logs := []models.RequestLog{}
	if err := findLatest(ctx, collection, bson.M{}, limit, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

// CountDailyActiveUsers 指定日(YYYY-MM-DD)のユニークIP数 (DAU) を数える
func (r *MongoAdminMetricsRepo) CountDailyActiveUsers(date string) (int64, error) {
	collection, err := metricsCollection(db.CollectionDailyActiveUsers)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return collection.CountDocuments(ctx, bson.M{"date": date})
}

// CountMonthlyActiveUsers since 以降の重複なくユニークな client_ip の数 (MAU) を数える
func (r *MongoAdminMetricsRepo) CountMonthlyActiveUsers(since time.Time) (int64, error) {
	collection, err := metricsCollection(db.CollectionDailyActiveUsers)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	distinctIPs, err := collection.Distinct(ctx, "client_ip", bson.M{
		"timestamp": bson.M{"$gte": since},
	})
	if err != nil {
		return 0, err
	}
	return int64(len(distinctIPs)), nil
}

// ResponseTimeStats since 以降の全体サマリー(avg/p95/p99/error_rate)と、
// 平均応答時間の遅いエンドポイント上位10件を集計する
// - MongoDB 7.0以上の $percentile 演算子を使用
func (r *MongoAdminMetricsRepo) ResponseTimeStats(since time.Time) (models.ResponseTimeMetrics, error) {
	collection, err := metricsCollection(db.CollectionRequestLogs)
	if err != nil {
		return models.ResponseTimeMetrics{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	matchStage := bson.D{{Key: "$match", Value: bson.D{
		{Key: "timestamp", Value: bson.D{{Key: "$gte", Value: since}}},
	}}}
	percentile := func(p float64) bson.D {
		return bson.D{{Key: "$percentile", Value: bson.D{
			{Key: "input", Value: "$response_time"},
			{Key: "p", Value: bson.A{p}},
			{Key: "method", Value: "approximate"},
		}}}
	}
	errorCount := bson.D{{Key: "$sum", Value: bson.D{
		{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$gte", Value: bson.A{"$status_code", 400}}},
			1, 0,
		}},
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
			{Key: "p95_ms", Value: percentile(0.95)},
			{Key: "p99_ms", Value: percentile(0.99)},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "error_count", Value: errorCount},
		}}},
		// - avg_msの降順で上位10件に絞り込む
		{{Key: "$sort", Value: bson.D{{Key: "avg_ms", Value: -1}}}},
		{{Key: "$limit", Value: 10}},
	}

	var rawEndpoints []bson.M
	if err := aggregateAll(ctx, collection, endpointPipeline, &rawEndpoints); err != nil {
		return models.ResponseTimeMetrics{}, fmt.Errorf("endpoint latency aggregation: %w", err)
	}

	// - bson.Mから型安全なEndpointLatencyスライスへ変換
	endpoints := make([]models.EndpointLatency, 0, len(rawEndpoints))
	for _, raw := range rawEndpoints {
		ep := models.EndpointLatency{}
		if id, ok := raw["_id"].(bson.M); ok {
			ep.Path, _ = id["path"].(string)
			ep.Method, _ = id["method"].(string)
		}
		ep.AvgMs = roundTo1(toFloat64(raw["avg_ms"]))
		ep.P95Ms = roundTo1(firstPercentile(raw["p95_ms"]))
		ep.P99Ms = roundTo1(firstPercentile(raw["p99_ms"]))
		ep.Count = toInt64(raw["count"])
		if ep.Count > 0 {
			ep.ErrorPct = roundTo1(float64(toInt64(raw["error_count"])) / float64(ep.Count) * 100)
		}
		endpoints = append(endpoints, ep)
	}

	// --- 2. 全体サマリー集計 ---
	summaryPipeline := mongo.Pipeline{
		matchStage,
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "avg_ms", Value: bson.D{{Key: "$avg", Value: "$response_time"}}},
			{Key: "p95_ms", Value: percentile(0.95)},
			{Key: "p99_ms", Value: percentile(0.99)},
			{Key: "total", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "error_count", Value: errorCount},
		}}},
	}

	var summaryRaw []bson.M
	if err := aggregateAll(ctx, collection, summaryPipeline, &summaryRaw); err != nil {
		return models.ResponseTimeMetrics{}, fmt.Errorf("summary latency aggregation: %w", err)
	}

	summary := models.ResponseTimeSummary{}
	if len(summaryRaw) > 0 {
		raw := summaryRaw[0]
		summary.AvgMs = roundTo1(toFloat64(raw["avg_ms"]))
		summary.P95Ms = roundTo1(firstPercentile(raw["p95_ms"]))
		summary.P99Ms = roundTo1(firstPercentile(raw["p99_ms"]))
		if total := toInt64(raw["total"]); total > 0 {
			summary.ErrorRatePct = roundTo1(float64(toInt64(raw["error_count"])) / float64(total) * 100)
		}
	}

	return models.ResponseTimeMetrics{
		Summary:   summary,
		Endpoints: endpoints,
	}, nil
}

// ListAuditLogs 監査ログを最新順に最大 limit 件取得する
func (r *MongoAdminMetricsRepo) ListAuditLogs(limit int64) ([]models.AuditLog, error) {
	collection, err := metricsCollection(db.CollectionAuditLogs)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logs := []models.AuditLog{}
	if err := findLatest(ctx, collection, bson.M{}, limit, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

// DBStats MongoDBのリアルタイム統計 (DBサイズ・接続数・直近24時間のスロークエリ数) を取得する
// - 各項目の取得失敗はログに残して0のまま返す (権限不足などで一部だけ取れないことがあるため)
func (r *MongoAdminMetricsRepo) DBStats() (models.DBMetrics, error) {
	client := db.Client()
	if client == nil {
		return models.DBMetrics{}, ErrDatabaseUnavailable
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var metricsData models.DBMetrics

	// 1. Database Size (dbStats)
	var dbStats bson.M
	if err := client.Database(db.DatabaseName).RunCommand(ctx, bson.D{{Key: "dbStats", Value: 1}}).Decode(&dbStats); err == nil {
		if dataSize, ok := dbStats["dataSize"]; ok {
			// Convert bytes to MB and round to 2 decimal places
			metricsData.DatabaseSizeMB = math.Round((toFloat64(dataSize)/(1024*1024))*100) / 100
		}
	} else {
		log.Printf("Failed to get dbStats: %v", err)
	}

	// 2. Active Connections (serverStatus) - 権限がない場合があるため、失敗時は静かにスキップ
	var serverStatus bson.M
	if err := client.Database("admin").RunCommand(ctx, bson.D{{Key: "serverStatus", Value: 1}}).Decode(&serverStatus); err == nil {
		if conns, ok := serverStatus["connections"].(bson.M); ok {
			if current, ok := conns["current"]; ok {
				metricsData.ActiveConnections = toInt64(current)
			}
		}
	} else {
		log.Printf("Failed to get serverStatus (may require clusterMonitor role): %v", err)
	}

	// 3. Slow Queries (24H) - RequestLogsコレクションで応答時間200ms以上をカウント（DB負荷を減らすためのApplication Log基盤）
	if reqLogCollection := db.GetCollection(db.DatabaseName, db.CollectionRequestLogs); reqLogCollection != nil {
		yesterday := time.Now().UTC().Add(-24 * time.Hour)
		slowCount, err := reqLogCollection.CountDocuments(ctx, bson.M{
			"timestamp":     bson.M{"$gte": yesterday},
			"response_time": bson.M{"$gte": 200},
		})
		if err == nil {
			metricsData.SlowQueries24h = slowCount
		} else {
			log.Printf("Failed to count slow queries: %v", err)
		}
	}

	return metricsData, nil
}

// findLatest filter に一致するドキュメントを timestamp の降順で最大 limit 件 out へデコードする
func findLatest(ctx context.Context, collection *mongo.Collection, filter bson.M, limit int64, out interface{}) error {
	findOptions := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(limit)

	cursor, err := collection.Find(ctx, filter, findOptions)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	return cursor.All(ctx, out)
}

// aggregateAll pipeline を実行し、結果を全件 out へデコードする
func aggregateAll(ctx context.Context, collection *mongo.Collection, pipeline mongo.Pipeline, out interface{}) error {
	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	return cursor.All(ctx, out)
}

// firstPercentile $percentile はスライスで返すため、最初の要素を数値として取り出す
func firstPercentile(v interface{}) float64 {
	if arr, ok := v.(bson.A); ok && len(arr) > 0 {
		return toFloat64(arr[0])
	}
	return 0
}

// roundTo1 小数第1位で丸める
func roundTo1(v float64) float64 {
	return math.Round(v*10) / 10
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
