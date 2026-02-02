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
	dateStr := r.URL.Query().Get("date")
	startDateStr := r.URL.Query().Get("start_date")
	endDateStr := r.URL.Query().Get("end_date")

	stats, err := CalculateStatistics(storeID, period, dateStr, startDateStr, endDateStr)
	if err != nil {
		log.Printf("Failed to calculate statistics for store %s: %v", storeID, err)
		utils.RespondWithError(w, "Failed to calculate statistics", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, stats, http.StatusOK)
}

func CalculateStatistics(storeID, period, dateStr, startDateStr, endDateStr string) (*models.StatisticsResponse, error) {
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

	var now time.Time
	if dateStr != "" {
		parsed, err := time.ParseInLocation("2006-01-02", dateStr, loc)
		if err != nil {
			log.Printf("Invalid date format: %s. Defaulting to now.", dateStr)
			now = time.Now().In(loc)
		} else {
			now = parsed
		}
	} else {
		now = time.Now().In(loc)
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	// 日付範囲設定
	var startDate time.Time
	var endDate time.Time
	var prevStartDate time.Time
	var dateFormat string

	// Explicit Date Range Check
	isExplicitRange := false
	if startDateStr != "" && endDateStr != "" {
		s, err1 := time.ParseInLocation("2006-01-02", startDateStr, loc)
		e, err2 := time.ParseInLocation("2006-01-02", endDateStr, loc)
		if err1 == nil && err2 == nil {
			startDate = s
			// End date from frontend is typically inclusive (e.g. 2024-01-27 to 2024-02-02).
			// Backend logic treats endDate as exclusive upper bound ($lt).
			// So we need to add 1 day to the parsed end date.
			// Example: "2026-02-02" -> Parse -> 00:00:00. AddDate(0,0,1) -> Feb 3 00:00:00.
			// Range: [Feb 2 00:00, Feb 3 00:00). Correct.
			endDate = e.AddDate(0, 0, 1)

			// Calculate duration for previous period
			// duration := endDate.Sub(startDate) // This includes the +1 day adjustment
			// Wait, let's look at logic.
			// Current: [Start, End).
			// Prev: [Start - duration, Start).
			// Example: Weekly. Start=Jan 27, End=Feb 3 (inclusive logic). Duration = 7 days.
			// Prev Start = Jan 27 - 7 days = Jan 20.
			// Range: [Jan 20, Jan 27). Correct.
			durationDays := int(endDate.Sub(startDate).Hours() / 24)
			prevStartDate = startDate.AddDate(0, 0, -durationDays)

			isExplicitRange = true

			// Date Format determination based on period
			if period == "yearly" {
				dateFormat = "%Y-%m"
			} else {
				dateFormat = "%Y-%m-%d"
			}
		}
	}

	if !isExplicitRange {
		switch period {
		case "weekly":
			// 過去7日間 (今日含む)
			startDate = today.AddDate(0, 0, -6)
			endDate = today.AddDate(0, 0, 1)
			prevStartDate = startDate.AddDate(0, 0, -7)
			dateFormat = "%Y-%m-%d"
		case "monthly":
			startDate = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
			endDate = startDate.AddDate(0, 1, 0)
			prevStartDate = startDate.AddDate(0, -1, 0)
			dateFormat = "%Y-%m-%d"
		case "yearly":
			startDate = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
			endDate = startDate.AddDate(1, 0, 0)
			prevStartDate = startDate.AddDate(-1, 0, 0)
			dateFormat = "%Y-%m"
		default: // "auto"
			// デフォルト（今日）
			startDate = today
			endDate = today.AddDate(0, 0, 1)
			// 1日前と比較
			prevStartDate = today.AddDate(0, 0, -1)
			dateFormat = "%Y-%m-%d"
		}
	}

	// 1. 期間全体の統計データ (チャートデータ + 合計数)
	// フィルタ開始日を「前期間の開始日」に設定して、両方の期間のデータを取得する
	// フィルタ終了日を「現在の期間の終了日」に設定する
	startFilter := prevStartDate.Format("2006-01-02T15:04:05.000")
	endFilter := endDate.Format("2006-01-02T15:04:05.000")

	matchStage := bson.D{{Key: "$match", Value: bson.D{
		{Key: "store_id", Value: storeID},
		{Key: "registration_time", Value: bson.D{
			{Key: "$gte", Value: startFilter},
			{Key: "$lt", Value: endFilter},
		}},
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

	// 詳細データ集計パイプライン
	facetStage := bson.D{{Key: "$facet", Value: bson.D{
		// A. チャート用データ（日別/月別グルーピング）
		{Key: "chart_data", Value: bson.A{
			bson.D{{Key: "$project", Value: bson.D{
				{Key: "group_key", Value: bson.D{{Key: "$dateToString", Value: bson.D{{Key: "format", Value: dateFormat}, {Key: "date", Value: "$reg_date_obj"}, {Key: "timezone", Value: locationName}}}}},
				{Key: "status", Value: 1},
			}}},
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: "$group_key"},
				{Key: "count", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "completed"}}}, 1, 0}}}}}},
			}}},
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
		}},

		// B. ハイライト用集計 (期間全体)
		// 今回（現在の期間）の統計: >= startDate AND < endDate
		{Key: "stats_current", Value: bson.A{
			bson.D{{Key: "$match", Value: bson.D{
				{Key: "registration_time", Value: bson.D{
					{Key: "$gte", Value: startDate.Format("2006-01-02T15:04:05.000")},
					{Key: "$lt", Value: endDate.Format("2006-01-02T15:04:05.000")},
				}},
			}}},
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: nil},
				{Key: "total_visitors", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "completed"}}}, 1, 0}}}}}},
				{Key: "total_count", Value: bson.D{{Key: "$sum", Value: 1}}},
				{Key: "no_show_cancel_count", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$in", Value: bson.A{"$status", bson.A{"no_show", "cancelled"}}}}, 1, 0}}}}}},
			}}},
		}},
		// 前回（前の期間）の統計: >= prevStartDate AND < startDate
		{Key: "stats_prev", Value: bson.A{
			bson.D{{Key: "$match", Value: bson.D{
				{Key: "registration_time", Value: bson.D{
					{Key: "$gte", Value: prevStartDate.Format("2006-01-02T15:04:05.000")},
					{Key: "$lt", Value: startDate.Format("2006-01-02T15:04:05.000")},
				}},
			}}},
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: nil},
				{Key: "total_visitors", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "completed"}}}, 1, 0}}}}}},
			}}},
		}},

		// C. 平均待ち時間 (現在の期間)
		{Key: "wait_times_current", Value: bson.A{
			bson.D{{Key: "$match", Value: bson.D{
				{Key: "registration_time", Value: bson.D{{Key: "$gte", Value: startDate.Format("2006-01-02T15:04:05.000")}}},
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

		// D. 時間帯別データ (現在 vs 前回の集計)
		// 時間ごとの傾向を見るために、期間内の全データの時間を集計する
		{Key: "hourly_current", Value: bson.A{
			bson.D{{Key: "$match", Value: bson.D{
				{Key: "registration_time", Value: bson.D{{Key: "$gte", Value: startDate.Format("2006-01-02T15:04:05.000")}}},
			}}},
			bson.D{{Key: "$project", Value: bson.D{
				{Key: "hour", Value: bson.D{{Key: "$hour", Value: bson.D{{Key: "date", Value: "$reg_date_obj"}, {Key: "timezone", Value: locationName}}}}},
				{Key: "status", Value: 1},
			}}},
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: "$hour"},
				{Key: "count", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "completed"}}}, 1, 0}}}}}},
			}}},
		}},
		{Key: "hourly_prev", Value: bson.A{
			bson.D{{Key: "$match", Value: bson.D{
				{Key: "registration_time", Value: bson.D{
					{Key: "$gte", Value: prevStartDate.Format("2006-01-02T15:04:05.000")},
					{Key: "$lt", Value: startDate.Format("2006-01-02T15:04:05.000")},
				}},
			}}},
			bson.D{{Key: "$project", Value: bson.D{
				{Key: "hour", Value: bson.D{{Key: "$hour", Value: bson.D{{Key: "date", Value: "$reg_date_obj"}, {Key: "timezone", Value: locationName}}}}},
				{Key: "status", Value: 1},
			}}},
			bson.D{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: "$hour"},
				{Key: "count", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{bson.D{{Key: "$eq", Value: bson.A{"$status", "completed"}}}, 1, 0}}}}}},
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

	response := &models.StatisticsResponse{}

	// --- 1. 訪問者統計の集計 ---
	var currentTotal, prevTotal int

	// 初期値は0
	currentTotal = 0
	prevTotal = 0

	// 'stats_current' のパース
	if statsCur, ok := result["stats_current"].(bson.A); ok && len(statsCur) > 0 {
		sMap := statsCur[0].(bson.M)
		if val, ok := sMap["total_visitors"].(int32); ok {
			currentTotal = int(val)
		}
	}
	// 'stats_prev' のパース
	if statsPrev, ok := result["stats_prev"].(bson.A); ok && len(statsPrev) > 0 {
		sMap := statsPrev[0].(bson.M)
		if val, ok := sMap["total_visitors"].(int32); ok {
			prevTotal = int(val)
		}
	}

	// 日次（auto）の場合、昨日や先週の同曜日の具体的なカウントも必要ですが、
	// UIモデル（VisitorStats struct）は少し固定されています。
	// フロントエンドは日次ビューの比較用に「昨日」と「先週同曜日」を具体的に使用します。
	// 月次/週次の場合、「昨日」は通常「前期間」にマッピングされます。
	// マッピング:
	// Today -> 今回の期間の合計
	// Yesterday -> 前回の期間の合計
	// LastWeekSameDay -> 0 (集計ビューでは計算負荷削減のため計算しない、または前期間にマッピング)

	// 注意: periodが 'auto' (日次) の場合、上記のロジックで 'stats_current' は今日の分、
	// 'stats_prev' は昨日の分として計算されるため、問題ありません。

	response.VisitorStats = models.VisitorStats{
		Today:           currentTotal,
		Yesterday:       prevTotal,
		LastWeekSameDay: 0, // Not calculated for aggregate view to save query complexity
		WowGrowthRate:   0, // Not calculated
		DodGrowthRate:   CalculateGrowthRate(currentTotal, prevTotal),
	}

	// --- 2. チャートデータ ---
	chartMap := make(map[string]int)
	if charts, ok := result["chart_data"].(bson.A); ok {
		for _, c := range charts {
			cMap := c.(bson.M)
			key := cMap["_id"].(string)
			count := int(cMap["count"].(int32))
			chartMap[key] = count
		}
	}
	noShowChartMap := make(map[string]int)
	if nsCharts, ok := result["no_show_chart_data"].(bson.A); ok {
		for _, c := range nsCharts {
			cMap := c.(bson.M)
			key := cMap["_id"].(string)
			count := int(cMap["count"].(int32))
			noShowChartMap[key] = count
		}
	}
	cancelledChartMap := make(map[string]int)
	if cancelCharts, ok := result["cancelled_chart_data"].(bson.A); ok {
		for _, c := range cancelCharts {
			cMap := c.(bson.M)
			key := cMap["_id"].(string)
			count := int(cMap["count"].(int32))
			cancelledChartMap[key] = count
		}
	}

	// 日付の欠落を埋める
	response.ChartData = make([]models.ChartData, 0)
	response.NoShowChartData = make([]models.ChartData, 0)
	response.CancelledChartData = make([]models.ChartData, 0)

	// Initialize Totals
	totalCancelled := 0
	totalNoShow := 0

	// ループ処理 (週間/月間/年間/日次)
	// 日付範囲に基づいてループ回数を決定する
	daysDiff := int(endDate.Sub(startDate).Hours() / 24)
	if daysDiff <= 0 {
		daysDiff = 1 // 最低1日
	}

	if period == "yearly" {
		// 年次は月ごとのループ (最大12ヶ月)
		// Explicit range (year) will span 12 months usually.
		for i := 0; i < 12; i++ {
			// Monthly iteration
			// Note: d calculation needs careful handling if startDate is not Jan 1.
			// But typically yearly view starts at Jan 1.
			// If explicit range is used, startDate might be strictly defined.
			d := startDate.AddDate(0, i, 0) // Add months
			key := d.Format("2006-01")

			valNoShow := noShowChartMap[key]
			valCancelled := cancelledChartMap[key]
			totalNoShow += valNoShow
			totalCancelled += valCancelled

			// Previous Period Logic for Year:
			// Compare with Same Month Last Year.
			prevD := d.AddDate(-1, 0, 0)
			prevKey := prevD.Format("2006-01")
			label := d.Format("1") + "月"

			response.ChartData = append(response.ChartData, models.ChartData{
				Label: label, Value: chartMap[key], PrevValue: chartMap[prevKey],
			})
			response.NoShowChartData = append(response.NoShowChartData, models.ChartData{
				Label: label, Value: valNoShow, PrevValue: noShowChartMap[prevKey],
			})
			response.CancelledChartData = append(response.CancelledChartData, models.ChartData{
				Label: label, Value: valCancelled, PrevValue: cancelledChartMap[prevKey],
			})
		}
	} else {
		// Weekly, Monthly, Auto -> Daily iteration
		for i := 0; i < daysDiff; i++ {
			d := startDate.AddDate(0, 0, i) // Add days
			key := d.Format("2006-01-02")

			valNoShow := noShowChartMap[key]
			valCancelled := cancelledChartMap[key]
			totalNoShow += valNoShow
			totalCancelled += valCancelled

			// Previous Period Logic:
			// "Previous" depends on context.
			// If Weekly -> prev is 7 days ago? Or user defined prevStartDate?
			// Since we calculated chartMap and prevChartMap based on prevStartDate...
			// BUT chart logic specifically generates `prevKey` to lookup in `chartMap`.
			// The `chartMap` contains BOTH current and previous data?
			// NO. `chartMap` comes from `facet -> chart_data` which ONLY looks at `prevStartDate`... wait.
			// Let's re-read the pipeline logic.

			// Pipeline 'chart_data' facet:
			// $project -> dateToString. matchStage (outer) filters >= prevStartDate.
			// So `chartMap` contains keys for BOTH [prevStart, Start) AND [Start, End).

			// So `PrevValue` lookup needs to find the key corresponding to "Previous Equivalent Day".
			// Rule:
			// If Weekly/Daily/Auto: Previous = d - 7 days? Or d - duration?
			// Standard practice: Weekly -> -7 days. Monthly -> -1 Month?
			// Auto -> -1 day?

			var prevD time.Time
			if period == "monthly" {
				prevD = d.AddDate(0, -1, 0)
			} else if period == "weekly" {
				prevD = d.AddDate(0, 0, -7)
			} else {
				// Auto or Custom
				// Default to -1 day if strict "Daily" comparison,
				// BUT commonly users want "Same day last week" for trends?
				// Let's stick to simple logic:
				// If Weekly -> -7. If Monthly -> -1 Month.
				// Else (Auto) -> -1 Day (Yesterday).
				prevD = d.AddDate(0, 0, -1)
			}
			prevKey := prevD.Format("2006-01-02")

			label := d.Format("2") // 日

			response.ChartData = append(response.ChartData, models.ChartData{
				Label: label, Value: chartMap[key], PrevValue: chartMap[prevKey],
			})
			response.NoShowChartData = append(response.NoShowChartData, models.ChartData{
				Label: label, Value: valNoShow, PrevValue: noShowChartMap[prevKey],
			})
			response.CancelledChartData = append(response.CancelledChartData, models.ChartData{
				Label: label, Value: valCancelled, PrevValue: cancelledChartMap[prevKey],
			})
		}
	}

	// Assign calculated totals
	response.TotalCancelled = totalCancelled
	response.TotalNoShow = totalNoShow

	// --- 3. 時間帯別データ (期間合計) ---
	var hourlyData []models.HourlyData
	hourlyMap := make(map[int]int)
	hourlyPrevMap := make(map[int]int)

	if hourly, ok := result["hourly_current"].(bson.A); ok {
		for _, h := range hourly {
			hMap := h.(bson.M)
			hour := int(hMap["_id"].(int32))
			count := int(hMap["count"].(int32))
			hourlyMap[hour] = count
		}
	}
	if hourlyPrev, ok := result["hourly_prev"].(bson.A); ok {
		for _, h := range hourlyPrev {
			hMap := h.(bson.M)
			hour := int(hMap["_id"].(int32))
			count := int(hMap["count"].(int32))
			hourlyPrevMap[hour] = count
		}
	}

	for i := 0; i < 24; i++ {
		hourlyData = append(hourlyData, models.HourlyData{
			Hour:      i,
			Count:     hourlyMap[i],
			PrevCount: hourlyPrevMap[i],
		})
	}
	response.HourlyCongestion = hourlyData

	// --- 4. 待ち時間 & 5. No Show率 (集計) ---
	if waitTimes, ok := result["wait_times_current"].(bson.A); ok && len(waitTimes) > 0 {
		wMap := waitTimes[0].(bson.M)
		if avg, ok := wMap["avg_wait"].(float64); ok {
			response.WaitTimeSeconds = int(avg)
			response.AverageWaitTime = utils.FormatDuration(int(avg))
		}
	} else {
		response.AverageWaitTime = "--分"
	}

	if statusStats, ok := result["stats_current"].(bson.A); ok && len(statusStats) > 0 { // stats_current にはステータスカウントが含まれる
		sMap := statusStats[0].(bson.M)
		total := 0
		if t, ok := sMap["total_count"].(int32); ok {
			total = int(t)
		}

		noShowCancel := 0
		if n, ok := sMap["no_show_cancel_count"].(int32); ok {
			noShowCancel = int(n)
		}

		if total > 0 {
			response.NoShowRate = (float64(noShowCancel) / float64(total)) * 100
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
