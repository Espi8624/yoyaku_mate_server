package handlers

import (
	"errors"
	"log"
	"math"
	"net/http"
	"time"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/events"
	"yoyaku_mate_server/metrics"
	"yoyaku_mate_server/models"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// AdminMetricsRepository 管理者ダッシュボード向けメトリクスの読み取りを抽象化するインターフェース
type AdminMetricsRepository interface {
	CountErrorsByType() (models.ErrorMetrics, error)
	ListErrorLogs(errorType string, limit int64) ([]models.ErrorLog, error)
	CountRequestsSince(since time.Time, successOnly bool) (int64, error)
	PeakTPSSince(since time.Time) (int64, error)
	ListRequestLogs(limit int64) ([]models.RequestLog, error)
	CountDailyActiveUsers(date string) (int64, error)
	CountMonthlyActiveUsers(since time.Time) (int64, error)
	ResponseTimeStats(since time.Time) (models.ResponseTimeMetrics, error)
	ListAuditLogs(limit int64) ([]models.AuditLog, error)
	DBStats() (models.DBMetrics, error)
}

// AdminMetricsHandler 管理者ダッシュボードのメトリクスAPIを扱うハンドラ
type AdminMetricsHandler struct {
	repo AdminMetricsRepository
}

func NewAdminMetricsHandler(repo AdminMetricsRepository) *AdminMetricsHandler {
	return &AdminMetricsHandler{repo: repo}
}

// respondMetricsError DB未接続とそれ以外の失敗を区別してエラー応答を返す
func respondMetricsError(w http.ResponseWriter, err error, message string) {
	if errors.Is(err, data.ErrDatabaseUnavailable) {
		respondWithError(w, "Database connection error", http.StatusInternalServerError)
		return
	}
	log.Printf("%s: %v", message, err)
	respondWithError(w, message, http.StatusInternalServerError)
}

// GetErrorMetrics
// - MongoDBからエラー発生タイプ別の合計数をリアルタイムでカウントする
// - 管理者ダッシュボード上部サマリーカードの統計データとして返却する
func (h *AdminMetricsHandler) GetErrorMetrics(w http.ResponseWriter, r *http.Request) {
	stats, err := h.repo.CountErrorsByType()
	if err != nil {
		respondMetricsError(w, err, "Failed to count errors")
		return
	}
	respondWithJSON(w, stats, http.StatusOK)
}

// GetErrorLogs
// - MongoDBに保存された詳細なエラーログを最新順にソートして最大50件取得する
// - 管理者ダッシュボード下部のテーブルデータとして返却する
// - ?error_type= を付ければその種別だけを返す (既定では SSE_DISCONNECT を除外。理由はリポジトリ側)
func (h *AdminMetricsHandler) GetErrorLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := h.repo.ListErrorLogs(r.URL.Query().Get("error_type"), 50)
	if err != nil {
		respondMetricsError(w, err, "Failed to fetch error logs")
		return
	}
	respondWithJSON(w, logs, http.StatusOK)
}

// GetRequestMetrics
// - 直近24時間における累積要求件数、成功率、および直近1時間以内のPeak TPS統計を演算して返却する
func (h *AdminMetricsHandler) GetRequestMetrics(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)
	oneHourAgo := now.Add(-1 * time.Hour)

	// - 24時間以内の総リクエスト数を集計
	total24h, err := h.repo.CountRequestsSince(yesterday, false)
	if err != nil {
		respondMetricsError(w, err, "Failed to calculate total requests")
		return
	}

	// - 24時間以内の成功したリクエスト数を集計 (status_code < 400)
	success24h, err := h.repo.CountRequestsSince(yesterday, true)
	if err != nil {
		respondMetricsError(w, err, "Failed to calculate success requests")
		return
	}

	successRate := 100.0
	if total24h > 0 {
		successRate = float64(success24h) / float64(total24h) * 100.0
	}

	// - 直近1時間における秒単位の要求数集計によるPeak TPSの算出
	peakTPS, err := h.repo.PeakTPSSince(oneHourAgo)
	if err != nil {
		// - 集計エラー時は0として処理を続行
		log.Printf("Peak TPS aggregation error: %v", err)
		peakTPS = 0
	}

	respondWithJSON(w, models.RequestMetrics{
		TotalRequests24h: total24h,
		SuccessRate:      successRate,
		PeakTPS1h:        peakTPS,
	}, http.StatusOK)
}

// GetRequestLogs
// - MongoDBに保存された詳細なリクエストログを最新順にソートして最大50件取得する
func (h *AdminMetricsHandler) GetRequestLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := h.repo.ListRequestLogs(50)
	if err != nil {
		respondMetricsError(w, err, "Failed to fetch request logs")
		return
	}
	respondWithJSON(w, logs, http.StatusOK)
}

// GetActiveUserMetrics
// - リアルタイム同時接続者数およびDAU/MAU統計を集計して返却する
func (h *AdminMetricsHandler) GetActiveUserMetrics(w http.ResponseWriter, r *http.Request) {
	// 1. リアルタイム同時接続者数 (インメモリから即座に取得)
	currentActive := metrics.GetRequestTracker().GetActiveUsersCount()

	// 2. 1日アクティブユーザー数 (DAU) - 本日の日付文字列を基準にユニークIP数を集計
	dauCount, err := h.repo.CountDailyActiveUsers(time.Now().Format("2006-01-02"))
	if errors.Is(err, data.ErrDatabaseUnavailable) {
		// DB接続失敗時もエラーを返却せずダッシュボードのクラッシュを防ぐためにフォールバック値を提供
		log.Println("DB connection failure in GetActiveUserMetrics")
		respondWithJSON(w, models.ActiveUserMetrics{
			CurrentActiveUsers: currentActive,
			DailyActiveUsers:   currentActive,
			MonthlyActiveUsers: currentActive,
		}, http.StatusOK)
		return
	}
	if err != nil {
		log.Printf("Failed to count DAU: %v", err)
		dauCount = currentActive // Fallback
	}

	// 3. 月間アクティブユーザー数 (MAU) - 直近30日以内のユニークIP数を集計
	mauCount, err := h.repo.CountMonthlyActiveUsers(time.Now().Add(-30 * 24 * time.Hour))
	if err != nil {
		log.Printf("Failed to count MAU: %v", err)
		mauCount = dauCount // Fallback to DAU
	}

	// もしMAUやDAUがリアルタイム同時接続者数より少ない場合は補正処理 (即時フォールバック)
	if dauCount < currentActive {
		dauCount = currentActive
	}
	if mauCount < dauCount {
		mauCount = dauCount
	}

	respondWithJSON(w, models.ActiveUserMetrics{
		CurrentActiveUsers: currentActive,
		DailyActiveUsers:   dauCount,
		MonthlyActiveUsers: mauCount,
	}, http.StatusOK)
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

	respondWithJSON(w, result, http.StatusOK)
}

// GetResponseTimeMetrics
// - クエリパラメータ ?range=5m|1h|24h を受け取り、指定期間内の
// - 全体サマリー(avg/p95/p99/error_rate)と遅いエンドポイント上位10件を集計して返す
func (h *AdminMetricsHandler) GetResponseTimeMetrics(w http.ResponseWriter, r *http.Request) {
	// - クエリパラメータからtime range取得 (デフォルト: 1h)
	var since time.Time
	switch r.URL.Query().Get("range") {
	case "5m":
		since = time.Now().UTC().Add(-5 * time.Minute)
	case "24h":
		since = time.Now().UTC().Add(-24 * time.Hour)
	default: // "1h" or anything else
		since = time.Now().UTC().Add(-1 * time.Hour)
	}

	result, err := h.repo.ResponseTimeStats(since)
	if err != nil {
		respondMetricsError(w, err, "Failed to aggregate response time metrics")
		return
	}
	respondWithJSON(w, result, http.StatusOK)
}

// GetAuditLogs
// - MongoDBの audit_logs コレクションから最新順で最大100件の監査ログを取得して返却
func (h *AdminMetricsHandler) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := h.repo.ListAuditLogs(100)
	if err != nil {
		respondMetricsError(w, err, "Failed to fetch audit logs")
		return
	}
	respondWithJSON(w, logs, http.StatusOK)
}

// GetSystemMetricsHandler はリアルタイムのハードウェアリソース使用状況（CPU、Memory、Disk）を取得します
func GetSystemMetricsHandler(w http.ResponseWriter, r *http.Request) {
	// 1. CPU Usage
	cpuPercents, err := cpu.Percent(0, false) // 0 means do not block/wait
	var cpuUsage float64
	if err == nil && len(cpuPercents) > 0 {
		cpuUsage = math.Round(cpuPercents[0]*10) / 10
	}

	// 2. Memory Usage
	var memUsage float64
	vMem, err := mem.VirtualMemory()
	if err == nil {
		memUsage = math.Round(vMem.UsedPercent*10) / 10
	}

	// 3. Disk Space Usage
	var diskUsage float64
	dUsage, err := disk.Usage("/")
	if err == nil {
		diskUsage = math.Round(dUsage.UsedPercent*10) / 10
	}

	metricsData := models.SystemMetrics{
		CPUUsage:    cpuUsage,
		MemoryUsage: memUsage,
		DiskSpace:   diskUsage,
	}

	respondWithJSON(w, metricsData, http.StatusOK)
}

// GetDBMetrics MongoDBのリアルタイム統計を取得します（接続数、DBサイズ、スロークエリ）
func (h *AdminMetricsHandler) GetDBMetrics(w http.ResponseWriter, r *http.Request) {
	metricsData, err := h.repo.DBStats()
	if err != nil {
		if errors.Is(err, data.ErrDatabaseUnavailable) {
			respondWithError(w, "Database client is not initialized", http.StatusInternalServerError)
			return
		}
		respondMetricsError(w, err, "Failed to fetch DB metrics")
		return
	}
	respondWithJSON(w, metricsData, http.StatusOK)
}
