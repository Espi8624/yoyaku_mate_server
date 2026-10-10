package handlers

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"yoyaku_mate_server/auth"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson/primitive"
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

// StatsWaitingListRepository 統計の元になる待機データを集計するインターフェース
type StatsWaitingListRepository interface {
	AggregateStatistics(storeID, timezone string, byWeekday bool, startPrev, startCurrent, endCurrent time.Time) (*data.WaitingStatistics, error)
}

type StatisticsHandler struct {
	userRepo    StatsUserRepository
	storeRepo   StatsStoreRepository
	waitingRepo StatsWaitingListRepository
}

func NewStatisticsHandler(userRepo StatsUserRepository, storeRepo StatsStoreRepository, waitingRepo StatsWaitingListRepository) *StatisticsHandler {
	return &StatisticsHandler{userRepo: userRepo, storeRepo: storeRepo, waitingRepo: waitingRepo}
}

// 曜日別集計のラベル（0=日曜〜6=土曜）
var weekdayLabels = [7]string{"日", "月", "火", "水", "木", "金", "土"}

// HandleGet は店舗の統計情報（今日 or 今週固定）を取得するリクエストを処理します
func (h *StatisticsHandler) HandleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		respondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// 権限チェック
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		respondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}

	idToken := authHeader[len("Bearer "):]
	firebaseUID, err := auth.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		respondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	user, err := h.userRepo.GetByFirebaseUID(firebaseUID)
	if err != nil || user == nil {
		respondWithError(w, "User not found", http.StatusUnauthorized)
		return
	}

	hasPermission, err := h.userRepo.CheckStorePermission(user.ID, storeID, user.Role, "")
	if err != nil || !hasPermission {
		respondWithError(w, "Permission denied", http.StatusForbidden)
		return
	}

	period := r.URL.Query().Get("period")
	if period != "weekly" {
		period = "auto" // "今日" がデフォルト。今日/今週以外は受け付けない
	}

	stats, err := h.CalculateStatistics(storeID, period)
	if err != nil {
		log.Printf("Failed to calculate statistics for store %s: %v", storeID, err)
		respondWithError(w, "Failed to calculate statistics", http.StatusInternalServerError)
		return
	}

	respondWithJSON(w, stats, http.StatusOK)
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

	agg, err := h.waitingRepo.AggregateStatistics(storeID, locationName, period == "weekly", startPrev, startCurrent, endCurrent)
	if err != nil {
		return nil, err
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
	buildChart := func(curMap, prevMap map[int]int) []models.ChartData {
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

	response.VisitorChart = buildChart(agg.VisitorCurrent, agg.VisitorPrev)
	response.CancelledChart = buildChart(agg.CancelledCurrent, agg.CancelledPrev)
	response.NoShowChart = buildChart(agg.NoShowCurrent, agg.NoShowPrev)

	// --- ハイライト集計 ---
	response.VisitorTotal = agg.VisitorTotal
	response.CancelledTotal = agg.CancelledTotal
	response.NoShowTotal = agg.NoShowTotal
	response.VisitorGrowthRate = CalculateGrowthRate(agg.VisitorTotal, agg.PrevVisitorTotal)
	if agg.TotalCount > 0 {
		response.NoShowRate = (float64(agg.CancelledTotal+agg.NoShowTotal) / float64(agg.TotalCount)) * 100
	}

	response.AverageWaitTime = "--分"
	if agg.AvgWaitSeconds != nil {
		response.WaitTimeSeconds = int(*agg.AvgWaitSeconds)
		response.AverageWaitTime = formatDuration(int(*agg.AvgWaitSeconds))
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

// formatDuration は秒数を "X分Y秒" または "X秒" 形式にフォーマットします
func formatDuration(seconds int) string {
	if seconds < 0 {
		return "--分"
	}
	min := seconds / 60
	sec := seconds % 60
	if min > 0 {
		return fmt.Sprintf("%d分%d秒", min, sec)
	}
	return fmt.Sprintf("%d秒", sec)
}
