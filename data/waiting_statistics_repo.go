package data

import (
	"context"
	"time"
	"yoyaku_mate_server/db"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// WaitingStatistics 店舗統計画面向けに待機データを集計した結果
// - バケットのキーは、時間帯別なら 0〜23(時)、曜日別なら 0〜6(0=日曜)
type WaitingStatistics struct {
	VisitorCurrent   map[int]int
	VisitorPrev      map[int]int
	CancelledCurrent map[int]int
	CancelledPrev    map[int]int
	NoShowCurrent    map[int]int
	NoShowPrev       map[int]int

	// 現在の期間の合計 (ハイライト用)
	VisitorTotal   int
	CancelledTotal int
	NoShowTotal    int
	TotalCount     int
	// 前期間(前日 or 前週)の来店者数 (成長率算出用)
	PrevVisitorTotal int
	// AvgWaitSeconds 現在の期間に来店完了した客の平均待ち時間(秒)。該当データが無ければ nil
	AvgWaitSeconds *float64
}

// AggregateStatistics 店舗の待機データを [startPrev, endCurrent) の範囲で集計する
//   - [startPrev, startCurrent) を前期間、[startCurrent, endCurrent) を現在の期間として比較する
//   - byWeekday なら曜日別、そうでなければ時間帯別にバケット集計する
//   - timezone はバケット(時・曜日)の判定に使うタイムゾーン名 (例: "Asia/Tokyo")
func (r *MongoWaitingListRepo) AggregateStatistics(storeID, timezone string, byWeekday bool, startPrev, startCurrent, endCurrent time.Time) (*WaitingStatistics, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	if collection == nil {
		return nil, ErrDatabaseUnavailable
	}
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

	// バケットキー: 時間帯別は時(0-23), 曜日別は曜日(1=日〜7=土)
	var bucketExpr bson.D
	if byWeekday {
		bucketExpr = bson.D{{Key: "$dayOfWeek", Value: bson.D{{Key: "date", Value: "$reg_date_obj"}, {Key: "timezone", Value: timezone}}}}
	} else {
		bucketExpr = bson.D{{Key: "$hour", Value: bson.D{{Key: "date", Value: "$reg_date_obj"}, {Key: "timezone", Value: timezone}}}}
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

	// Mongoの $dayOfWeek は1始まり(1=日)なので、曜日別の場合は-1して0始まりに揃える
	toBucketMap := func(facetName string) map[int]int {
		m := make(map[int]int)
		if arr, ok := result[facetName].(bson.A); ok {
			for _, item := range arr {
				im := item.(bson.M)
				id := int(im["_id"].(int32))
				if byWeekday {
					id -= 1
				}
				m[id] = int(im["count"].(int32))
			}
		}
		return m
	}

	stats := &WaitingStatistics{
		VisitorCurrent:   toBucketMap("visitor_current"),
		VisitorPrev:      toBucketMap("visitor_prev"),
		CancelledCurrent: toBucketMap("cancelled_current"),
		CancelledPrev:    toBucketMap("cancelled_prev"),
		NoShowCurrent:    toBucketMap("no_show_current"),
		NoShowPrev:       toBucketMap("no_show_prev"),
	}

	if arr, ok := result["totals_current"].(bson.A); ok && len(arr) > 0 {
		m := arr[0].(bson.M)
		stats.VisitorTotal = int(m["total_visitors"].(int32))
		stats.CancelledTotal = int(m["total_cancelled"].(int32))
		stats.NoShowTotal = int(m["total_no_show"].(int32))
		stats.TotalCount = int(m["total_count"].(int32))
	}
	if arr, ok := result["totals_prev"].(bson.A); ok && len(arr) > 0 {
		m := arr[0].(bson.M)
		stats.PrevVisitorTotal = int(m["total_visitors"].(int32))
	}
	if arr, ok := result["wait_times_current"].(bson.A); ok && len(arr) > 0 {
		m := arr[0].(bson.M)
		if avg, ok := m["avg_wait"].(float64); ok {
			stats.AvgWaitSeconds = &avg
		}
	}

	return stats, nil
}
