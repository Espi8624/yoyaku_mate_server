package handlers

import (
	"context"
	"log"
	"net/http"
	"time"

	"yoyaku_mate_server/auth"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// StatisticsHandler は店舗の統計情報を取得するリクエストを処理します
func StatisticsHandler(w http.ResponseWriter, r *http.Request) {
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

	user, err := data.GetUserByFirebaseUID(firebaseUID)
	if err != nil || user == nil {
		utils.RespondWithError(w, "User not found", http.StatusUnauthorized)
		return
	}

	hasPermission, err := data.CheckUserStorePermission(user.ID, storeID, user.Role, "")
	if err != nil || !hasPermission {
		utils.RespondWithError(w, "Permission denied", http.StatusForbidden)
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "auto"
	}

	// 統計情報の計算
	stats, err := CalculateStatistics(storeID, period)
	if err != nil {
		log.Printf("Failed to calculate statistics for store %s: %v", storeID, err)
		utils.RespondWithError(w, "Failed to calculate statistics", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, stats, http.StatusOK)
}

func CalculateStatistics(storeID, period string) (*models.StatisticsResponse, error) {
	// 店舗情報の取得（タイムゾーン確認のため）
	store, err := data.GetStoreData(storeID)
	locationName := "Asia/Tokyo"
	if err == nil && store != nil && store.Timezone != "" {
		locationName = store.Timezone
	}

	// タイムゾーンのロード
	loc, err := time.LoadLocation(locationName)
	if err != nil {
		log.Printf("Failed to load location '%s', defaulting to Asia/Tokyo: %v", locationName, err)
		loc = time.FixedZone("Asia/Tokyo", 9*60*60)
		locationName = "Asia/Tokyo"
	}

	collection := db.GetCollection(db.DatabaseName, db.CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	// 日付範囲設定
	var startDate time.Time
	var prevStartDate time.Time
	var dateFormat string

	switch period {
	case "weekly":
		// 過去7日間 (今日含む)
		startDate = today.AddDate(0, 0, -6)
		// 前期間: さらに7日前
		prevStartDate = startDate.AddDate(0, 0, -7)
		dateFormat = "%Y-%m-%d"
	case "monthly":
		// 今月初めから
		startDate = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		// 前期間: 先月初めから
		prevStartDate = startDate.AddDate(0, -1, 0)
		dateFormat = "%Y-%m-%d"
	case "yearly":
		// 今年初めから
		startDate = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
		// 前期間: 去年初めから
		prevStartDate = startDate.AddDate(-1, 0, 0)
		dateFormat = "%Y-%m" // 月単位集計
	default: // "auto"
		// 既存ロジック (先週の同曜日比較用)
		startDate = today.AddDate(0, 0, -7)
		// autoの場合はチャート比較はしない
		prevStartDate = startDate
		dateFormat = "%Y-%m-%d"
	}

	// フィルタ開始日を「前期間の開始日」に設定して、両方の期間のデータを取得する
	// MongoDBの保存時間はUTCかISO文字列だが、フィルタは文字列比較で行っているため
	// フォーマットには注意が必要。ここでは単純な文字列比較のためオフセット付きでフォーマットする。
	startFilter := prevStartDate.Format("2006-01-02T15:04:05.000") // タイムゾーンオフセットはデータ依存

	matchStage := bson.D{{Key: "$match", Value: bson.D{
		{Key: "store_id", Value: storeID},
		{Key: "registration_time", Value: bson.D{{Key: "$gte", Value: startFilter}}},
	}}}

	addFieldsStage := bson.D{{Key: "$addFields", Value: bson.D{
		{Key: "reg_date_obj", Value: bson.D{
			{Key: "$dateFromString", Value: bson.D{
				{Key: "dateString", Value: "$registration_time"},
			}},
		}},
		{Key: "entry_date_obj", Value: bson.D{
			{Key: "$cond", Value: bson.A{
				bson.D{{Key: "$ne", Value: bson.A{"$entry_time", nil}}},
				bson.D{{Key: "$dateFromString", Value: bson.D{
					{Key: "dateString", Value: "$entry_time"},
				}}},
				nil,
			}},
		}},
	}}}

	// 注意: MongoDBのAggregationで複数の独立した集計を行うには、
	// 通常 $facet を使いますが、メモリ制限を回避するためにここでは
	// 「期間データ」と「今日のデータ」に分けてクエリを実行します。

	// Query A: 期間全体の統計 (Visitor, Charts)
	// $facetは便利ですが、16MB/100MB制限があるため、ここでは
	// 期間が長い場合のリスクを減らすため、$project で必要なフィールドだけを残してから $facet します。
	// 入力ドキュメントを最小化します。

	projectMinimalStage := bson.D{{Key: "$project", Value: bson.D{
		{Key: "reg_date_obj", Value: 1},
		{Key: "status", Value: 1},
	}}}

	periodFacetStage := bson.D{{Key: "$facet", Value: bson.D{
		{Key: "visitor_counts", Value: bson.A{
			bson.D{{Key: "$project", Value: bson.D{
				{Key: "status", Value: 1},
				{Key: "day_str", Value: bson.D{{Key: "$dateToString", Value: bson.D{{Key: "format", Value: "%Y-%m-%d"}, {Key: "date", Value: "$reg_date_obj"}, {Key: "timezone", Value: locationName}}}}},
			}}},
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: "$day_str"},
				{Key: "completed_count", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "completed"}}}, 1, 0}}}}}},
			}}},
		}},
		{Key: "chart_data", Value: bson.A{
			bson.D{{Key: "$project", Value: bson.D{
				{Key: "group_key", Value: bson.D{{Key: "$dateToString", Value: bson.D{{Key: "format", Value: dateFormat}, {Key: "date", Value: "$reg_date_obj"}, {Key: "timezone", Value: locationName}}}}},
				{Key: "status", Value: 1},
			}}},
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: "$group_key"},
				{Key: "count", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "completed"}}}, 1, 0}}}}}},
			}}},
			bson.D{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
		}},
		{Key: "no_show_chart_data", Value: bson.A{
			bson.D{{Key: "$project", Value: bson.D{
				{Key: "group_key", Value: bson.D{{Key: "$dateToString", Value: bson.D{{Key: "format", Value: dateFormat}, {Key: "date", Value: "$reg_date_obj"}, {Key: "timezone", Value: locationName}}}}},
				{Key: "status", Value: 1},
			}}},
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: "$group_key"},
				{Key: "count", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "no_show"}}}, 1, 0}}}}}},
			}}},
			bson.D{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
		}},
		{Key: "cancelled_chart_data", Value: bson.A{
			bson.D{{Key: "$project", Value: bson.D{
				{Key: "group_key", Value: bson.D{{Key: "$dateToString", Value: bson.D{{Key: "format", Value: dateFormat}, {Key: "date", Value: "$reg_date_obj"}, {Key: "timezone", Value: locationName}}}}},
				{Key: "status", Value: 1},
			}}},
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: "$group_key"},
				{Key: "count", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "cancelled"}}}, 1, 0}}}}}},
			}}},
			bson.D{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
		}},
	}}}

	cursorPeriod, err := collection.Aggregate(ctx, mongo.Pipeline{matchStage, addFieldsStage, projectMinimalStage, periodFacetStage})
	if err != nil {
		return nil, err
	}
	defer cursorPeriod.Close(ctx)

	var periodResults []bson.M
	if err = cursorPeriod.All(ctx, &periodResults); err != nil {
		return nil, err
	}

	// Query B: 今日の詳細データ (Hourly, WaitTime, Stats)
	// Matchステージを今日のみに絞ることで高速化＆メモリ節約
	todayMatchStage := bson.D{{Key: "$match", Value: bson.D{
		{Key: "store_id", Value: storeID},
		{Key: "registration_time", Value: bson.D{{Key: "$gte", Value: today.Format("2006-01-02T15:04:05.000+09:00")}}},
	}}}

	todayFacetStage := bson.D{{Key: "$facet", Value: bson.D{
		{Key: "hourly_today", Value: bson.A{
			bson.D{{Key: "$project", Value: bson.D{
				{Key: "hour", Value: bson.D{{Key: "$hour", Value: bson.D{{Key: "date", Value: "$reg_date_obj"}, {Key: "timezone", Value: locationName}}}}},
			}}},
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: "$hour"},
				{Key: "count", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "completed"}}}, 1, 0}}}}}},
			}}},
			bson.D{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
		}},
		{Key: "wait_times_today", Value: bson.A{
			bson.D{{Key: "$match", Value: bson.D{
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
		{Key: "status_stats_today", Value: bson.A{
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: nil},
				{Key: "total", Value: bson.D{{Key: "$sum", Value: 1}}},
				{Key: "no_show_cancel", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$in", Value: bson.A{"$status", bson.A{"no_show", "cancelled"}}}}, 1, 0}}}}}},
			}}},
		}},
	}}}

	cursorToday, err := collection.Aggregate(ctx, mongo.Pipeline{todayMatchStage, addFieldsStage, todayFacetStage})
	if err != nil {
		return nil, err
	}
	defer cursorToday.Close(ctx)

	var todayResults []bson.M
	if err = cursorToday.All(ctx, &todayResults); err != nil {
		return nil, err
	}

	// Combine Results
	result := bson.M{}
	if len(periodResults) > 0 {
		for k, v := range periodResults[0] {
			result[k] = v
		}
	}
	if len(todayResults) > 0 {
		for k, v := range todayResults[0] {
			result[k] = v
		}
	}

	response := &models.StatisticsResponse{}

	// --- 1. 訪問者統計のパース (既存ロジック) ---
	todayStr := today.Format("2006-01-02")
	yesterday := today.AddDate(0, 0, -1)
	yesterdayStr := yesterday.Format("2006-01-02")
	lastWeek := today.AddDate(0, 0, -7)
	lastWeekStr := lastWeek.Format("2006-01-02")

	var todayCount, yesterdayCount, lastWeekCount int

	if visitors, ok := result["visitor_counts"].(bson.A); ok {
		for _, v := range visitors {
			vMap := v.(bson.M)
			date := vMap["_id"].(string)
			count := int(vMap["completed_count"].(int32))

			if date == todayStr {
				todayCount = count
			} else if date == yesterdayStr {
				yesterdayCount = count
			} else if date == lastWeekStr {
				lastWeekCount = count
			}
		}
	}
	response.VisitorStats = models.VisitorStats{
		Today:           todayCount,
		Yesterday:       yesterdayCount,
		LastWeekSameDay: lastWeekCount,
		WowGrowthRate:   CalculateGrowthRate(todayCount, lastWeekCount),
		DodGrowthRate:   CalculateGrowthRate(todayCount, yesterdayCount),
	}

	// --- 2. チャートデータ統計のパース (新規) ---
	response.ChartData = make([]models.ChartData, 0)
	chartMap := make(map[string]int)

	if charts, ok := result["chart_data"].(bson.A); ok {
		for _, c := range charts {
			cMap := c.(bson.M)
			key := cMap["_id"].(string)
			count := int(cMap["count"].(int32))
			chartMap[key] = count
		}
	}

	// --- 2.5 No-Show チャートデータ統計のパース ---
	response.NoShowChartData = make([]models.ChartData, 0)
	noShowChartMap := make(map[string]int)

	if nsCharts, ok := result["no_show_chart_data"].(bson.A); ok {
		for _, c := range nsCharts {
			cMap := c.(bson.M)
			key := cMap["_id"].(string)
			count := int(cMap["count"].(int32))
			noShowChartMap[key] = count
		}
	}

	// --- 2.6 Cancelled チャートデータ統計のパース ---
	response.CancelledChartData = make([]models.ChartData, 0)
	cancelledChartMap := make(map[string]int)

	if cancelCharts, ok := result["cancelled_chart_data"].(bson.A); ok {
		for _, c := range cancelCharts {
			cMap := c.(bson.M)
			key := cMap["_id"].(string)
			count := int(cMap["count"].(int32))
			cancelledChartMap[key] = count
		}
	}

	// 日付/月の欠落部分を0で埋め、前期間をマッピングする
	if period == "weekly" {
		for i := 0; i < 7; i++ {
			d := startDate.AddDate(0, 0, i)
			key := d.Format("2006-01-02")

			// 前期間: 7日前
			prevD := d.AddDate(0, 0, -7)
			prevKey := prevD.Format("2006-01-02")

			label := d.Format("1/2")

			// 訪問者チャート
			val := chartMap[key]
			prevVal := chartMap[prevKey]
			response.ChartData = append(response.ChartData, models.ChartData{
				Label:     label,
				Value:     val,
				PrevValue: prevVal,
			})

			// No-Show チャート
			nsVal := noShowChartMap[key]
			nsPrevVal := noShowChartMap[prevKey]
			response.NoShowChartData = append(response.NoShowChartData, models.ChartData{
				Label:     label,
				Value:     nsVal,
				PrevValue: nsPrevVal,
			})

			// Cancelled チャート
			cVal := cancelledChartMap[key]
			cPrevVal := cancelledChartMap[prevKey]
			response.CancelledChartData = append(response.CancelledChartData, models.ChartData{
				Label:     label,
				Value:     cVal,
				PrevValue: cPrevVal,
			})
		}
	} else if period == "monthly" {
		daysInMonth := now.Day()
		if daysInMonth < 1 {
			daysInMonth = 1
		}
		for i := 0; i < daysInMonth; i++ {
			d := startDate.AddDate(0, 0, i)
			key := d.Format("2006-01-02")

			// 前期間: 1ヶ月前
			prevD := d.AddDate(0, -1, 0)
			prevKey := prevD.Format("2006-01-02")

			label := d.Format("1/2")

			// 訪問者チャート
			val := chartMap[key]
			prevVal := chartMap[prevKey]
			response.ChartData = append(response.ChartData, models.ChartData{
				Label:     label,
				Value:     val,
				PrevValue: prevVal,
			})

			// No-Show チャート
			nsVal := noShowChartMap[key]
			nsPrevVal := noShowChartMap[prevKey]
			response.NoShowChartData = append(response.NoShowChartData, models.ChartData{
				Label:     label,
				Value:     nsVal,
				PrevValue: nsPrevVal,
			})

			// Cancelled チャート
			cVal := cancelledChartMap[key]
			cPrevVal := cancelledChartMap[prevKey]
			response.CancelledChartData = append(response.CancelledChartData, models.ChartData{
				Label:     label,
				Value:     cVal,
				PrevValue: cPrevVal,
			})
		}
	} else if period == "yearly" {
		currentMonth := int(now.Month())
		for i := 1; i <= currentMonth; i++ {
			d := time.Date(now.Year(), time.Month(i), 1, 0, 0, 0, 0, loc)
			key := d.Format("2006-01") // 月単位フォーマット

			// 前期間: 1年前
			prevD := d.AddDate(-1, 0, 0)
			prevKey := prevD.Format("2006-01")

			label := d.Format("1") + "月"

			// 訪問者チャート
			val := chartMap[key]
			prevVal := chartMap[prevKey]
			response.ChartData = append(response.ChartData, models.ChartData{
				Label:     label,
				Value:     val,
				PrevValue: prevVal,
			})

			// No-Show チャート
			nsVal := noShowChartMap[key]
			nsPrevVal := noShowChartMap[prevKey]
			response.NoShowChartData = append(response.NoShowChartData, models.ChartData{
				Label:     label,
				Value:     nsVal,
				PrevValue: nsPrevVal,
			})

			// Cancelled チャート
			cVal := cancelledChartMap[key]
			cPrevVal := cancelledChartMap[prevKey]
			response.CancelledChartData = append(response.CancelledChartData, models.ChartData{
				Label:     label,
				Value:     cVal,
				PrevValue: cPrevVal,
			})
		}
	} else {
		// Auto/Default -> 今日の時間別データのみ
	}

	// --- 3. 時間別データ (既存) ---
	var hourlyData []models.HourlyData
	hourlyMap := make(map[int]int)
	if hourly, ok := result["hourly_today"].(bson.A); ok {
		for _, h := range hourly {
			hMap := h.(bson.M)
			hour := int(hMap["_id"].(int32))
			count := int(hMap["count"].(int32))
			hourlyMap[hour] = count
		}
	}
	for i := 0; i < 24; i++ {
		hourlyData = append(hourlyData, models.HourlyData{Hour: i, Count: hourlyMap[i]})
	}
	response.HourlyCongestion = hourlyData

	// --- 4. 待ち時間 & 5. No Show率 (既存) ---
	if waitTimes, ok := result["wait_times_today"].(bson.A); ok && len(waitTimes) > 0 {
		wMap := waitTimes[0].(bson.M)
		if avg, ok := wMap["avg_wait"].(float64); ok {
			response.WaitTimeSeconds = int(avg)
			response.AverageWaitTime = utils.FormatDuration(int(avg))
		}
	} else {
		response.AverageWaitTime = "--分"
	}

	if statusStats, ok := result["status_stats_today"].(bson.A); ok && len(statusStats) > 0 {
		sMap := statusStats[0].(bson.M)
		total := int(sMap["total"].(int32))
		noShowCancel := int(sMap["no_show_cancel"].(int32))
		if total > 0 {
			response.NoShowRate = (float64(noShowCancel) / float64(total)) * 100
		}
	}

	log.Printf("[Statistics] Store: %s, Period: %s, Visitor: %d, ChartDataLen: %d", storeID, period, todayCount, len(response.ChartData))

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
