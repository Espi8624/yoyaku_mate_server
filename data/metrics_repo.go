package data

import (
	"context"
	"log"
	"math"
	"time"

	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoMetricsRepo struct{}

// GetErrorMetrics MongoDBからエラー発生タイプ別の合計数を取得します。
func (r *MongoMetricsRepo) GetErrorMetrics(ctx context.Context) (models.ErrorMetrics, error) {
	collection := db.GetCollection(db.DatabaseName, db.CollectionErrorLogs)
	if collection == nil {
		return models.ErrorMetrics{}, mongo.ErrClientDisconnected
	}

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

// GetErrorLogs 最新のエラーログデータを指定された件数分取得します。
func (r *MongoMetricsRepo) GetErrorLogs(ctx context.Context, limit int64) ([]models.ErrorLog, error) {
	collection := db.GetCollection(db.DatabaseName, db.CollectionErrorLogs)
	if collection == nil {
		return nil, mongo.ErrClientDisconnected
	}

	findOptions := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(limit)

	cursor, err := collection.Find(ctx, bson.M{}, findOptions)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var logs []models.ErrorLog
	if err = cursor.All(ctx, &logs); err != nil {
		return nil, err
	}
	if logs == nil {
		logs = []models.ErrorLog{}
	}
	return logs, nil
}

// GetRequestMetrics 直近24時間の累積リクエスト件数、成功率、および1時間以内のPeak TPSを集計します。
func (r *MongoMetricsRepo) GetRequestMetrics(ctx context.Context) (models.RequestMetrics, error) {
	collection := db.GetCollection(db.DatabaseName, db.CollectionRequestLogs)
	if collection == nil {
		return models.RequestMetrics{}, mongo.ErrClientDisconnected
	}

	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)
	oneHourAgo := now.Add(-1 * time.Hour)

	total24h, err := collection.CountDocuments(ctx, bson.M{
		"timestamp": bson.M{"$gte": yesterday},
	})
	if err != nil {
		return models.RequestMetrics{}, err
	}

	success24h, err := collection.CountDocuments(ctx, bson.M{
		"timestamp":   bson.M{"$gte": yesterday},
		"status_code": bson.M{"$lt": 400},
	})
	if err != nil {
		return models.RequestMetrics{}, err
	}

	successRate := 100.0
	if total24h > 0 {
		successRate = float64(success24h) / float64(total24h) * 100.0
	}

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
	if err == nil {
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

	return models.RequestMetrics{
		TotalRequests24h: total24h,
		SuccessRate:      successRate,
		PeakTPS1h:        peakTPS,
	}, nil
}

// GetRequestLogs 最新のAPIリクエストログデータを指定された件数分取得します。
func (r *MongoMetricsRepo) GetRequestLogs(ctx context.Context, limit int64) ([]models.RequestLog, error) {
	collection := db.GetCollection(db.DatabaseName, db.CollectionRequestLogs)
	if collection == nil {
		return nil, mongo.ErrClientDisconnected
	}

	findOptions := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(limit)

	cursor, err := collection.Find(ctx, bson.M{}, findOptions)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var logs []models.RequestLog
	if err = cursor.All(ctx, &logs); err != nil {
		return nil, err
	}
	if logs == nil {
		logs = []models.RequestLog{}
	}
	return logs, nil
}

// GetDAUMAU 本日基準のDAUおよび直近30日基準のMAUを集計します。
func (r *MongoMetricsRepo) GetDAUMAU(ctx context.Context) (int64, int64, error) {
	dauCollection := db.GetCollection(db.DatabaseName, db.CollectionDailyActiveUsers)
	if dauCollection == nil {
		return 0, 0, mongo.ErrClientDisconnected
	}

	todayStr := time.Now().Format("2006-01-02")
	dauCount, err := dauCollection.CountDocuments(ctx, bson.M{"date": todayStr})
	if err != nil {
		return 0, 0, err
	}

	thirtyDaysAgo := time.Now().Add(-30 * 24 * time.Hour)
	distinctIPs, err := dauCollection.Distinct(ctx, "client_ip", bson.M{
		"timestamp": bson.M{"$gte": thirtyDaysAgo},
	})
	
	var mauCount int64
	if err == nil {
		mauCount = int64(len(distinctIPs))
	} else {
		mauCount = dauCount // fallback
	}

	return dauCount, mauCount, nil
}

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

// GetResponseTimeMetrics 指定された時点以降のエンドポイント別の遅延時間(Latency)と全体の応答時間サマリーを返します。
func (r *MongoMetricsRepo) GetResponseTimeMetrics(ctx context.Context, since time.Time) ([]models.EndpointLatency, models.ResponseTimeSummary, error) {
	collection := db.GetCollection(db.DatabaseName, db.CollectionRequestLogs)
	if collection == nil {
		return nil, models.ResponseTimeSummary{}, mongo.ErrClientDisconnected
	}

	matchStage := bson.D{{Key: "$match", Value: bson.D{
		{Key: "timestamp", Value: bson.D{{Key: "$gte", Value: since}}},
	}}}

	endpointPipeline := mongo.Pipeline{
		matchStage,
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
		{{Key: "$sort", Value: bson.D{{Key: "avg_ms", Value: -1}}}},
		{{Key: "$limit", Value: 10}},
	}

	endpointCursor, err := collection.Aggregate(ctx, endpointPipeline)
	if err != nil {
		return nil, models.ResponseTimeSummary{}, err
	}
	defer endpointCursor.Close(ctx)

	var rawEndpoints []bson.M
	if err := endpointCursor.All(ctx, &rawEndpoints); err != nil {
		return nil, models.ResponseTimeSummary{}, err
	}

	endpoints := make([]models.EndpointLatency, 0, len(rawEndpoints))
	for _, raw := range rawEndpoints {
		ep := models.EndpointLatency{}
		if id, ok := raw["_id"].(bson.M); ok {
			ep.Path, _ = id["path"].(string)
			ep.Method, _ = id["method"].(string)
		}
		ep.AvgMs = math.Round(toFloat64(raw["avg_ms"])*10) / 10
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
		return nil, models.ResponseTimeSummary{}, err
	}
	defer summaryCursor.Close(ctx)

	var summaryRaw []bson.M
	if err := summaryCursor.All(ctx, &summaryRaw); err != nil {
		return nil, models.ResponseTimeSummary{}, err
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

	return endpoints, summary, nil
}

// GetAuditLogs 管理者の作業記録(Audit Logs)を最新順に取得します。
func (r *MongoMetricsRepo) GetAuditLogs(ctx context.Context, limit int64) ([]models.AuditLog, error) {
	collection := db.GetCollection(db.DatabaseName, db.CollectionAuditLogs)
	if collection == nil {
		return nil, mongo.ErrClientDisconnected
	}

	findOptions := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(limit)

	cursor, err := collection.Find(ctx, bson.M{}, findOptions)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var logs []models.AuditLog
	if err = cursor.All(ctx, &logs); err != nil {
		return nil, err
	}
	if logs == nil {
		logs = []models.AuditLog{}
	}
	return logs, nil
}

// GetDBMetrics MongoDBの現在のアクティブな接続数、データベースのサイズ、スロークエリ発生件数を取得します。
func (r *MongoMetricsRepo) GetDBMetrics(ctx context.Context) (models.DBMetrics, error) {
	if db.MongoClient == nil {
		return models.DBMetrics{}, mongo.ErrClientDisconnected
	}

	var metricsData models.DBMetrics

	var dbStats bson.M
	if err := db.MongoClient.Database(db.DatabaseName).RunCommand(ctx, bson.D{{Key: "dbStats", Value: 1}}).Decode(&dbStats); err == nil {
		if dataSize, ok := dbStats["dataSize"]; ok {
			sizeBytes := toFloat64(dataSize)
			metricsData.DatabaseSizeMB = math.Round((sizeBytes/(1024*1024))*100) / 100
		}
	} else {
		log.Printf("Failed to get dbStats: %v", err)
	}

	var serverStatus bson.M
	if err := db.MongoClient.Database("admin").RunCommand(ctx, bson.D{{Key: "serverStatus", Value: 1}}).Decode(&serverStatus); err == nil {
		if conns, ok := serverStatus["connections"].(bson.M); ok {
			if current, ok := conns["current"]; ok {
				metricsData.ActiveConnections = toInt64(current)
			}
		}
	} else {
		log.Printf("Failed to get serverStatus: %v", err)
	}

	reqLogCollection := db.GetCollection(db.DatabaseName, db.CollectionRequestLogs)
	if reqLogCollection != nil {
		yesterday := time.Now().UTC().Add(-24 * time.Hour)
		slowCount, err := reqLogCollection.CountDocuments(ctx, bson.M{
			"timestamp":     bson.M{"$gte": yesterday},
			"response_time": bson.M{"$gte": 200},
		})
		if err == nil {
			metricsData.SlowQueries24h = slowCount
		}
	}

	return metricsData, nil
}
