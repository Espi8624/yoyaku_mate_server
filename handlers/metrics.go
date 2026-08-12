package handlers

import (
	"context"
	"log"
	"math"
	"net/http"
	"time"

	"yoyaku_mate_server/events"
	"yoyaku_mate_server/metrics"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

type AdminMetricsRepository interface {
	GetErrorMetrics(ctx context.Context) (models.ErrorMetrics, error)
	GetErrorLogs(ctx context.Context, limit int64) ([]models.ErrorLog, error)
	GetRequestMetrics(ctx context.Context) (models.RequestMetrics, error)
	GetRequestLogs(ctx context.Context, limit int64) ([]models.RequestLog, error)
	GetDAUMAU(ctx context.Context) (int64, int64, error)
	GetResponseTimeMetrics(ctx context.Context, since time.Time) ([]models.EndpointLatency, models.ResponseTimeSummary, error)
	GetAuditLogs(ctx context.Context, limit int64) ([]models.AuditLog, error)
	GetDBMetrics(ctx context.Context) (models.DBMetrics, error)
}

type AdminMetricsHandler struct {
	repo AdminMetricsRepository
}

func NewAdminMetricsHandler(repo AdminMetricsRepository) *AdminMetricsHandler {
	return &AdminMetricsHandler{repo: repo}
}

// - MongoDBからエラー発生タイプ別の合計数をリアルタイムでカウントする
// - 管理者ダッシュボード上部サマリーカードの統計データとして返却する
func (h *AdminMetricsHandler) GetErrorMetricsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stats, err := h.repo.GetErrorMetrics(ctx)
	if err != nil {
		utils.RespondWithError(w, "Database error", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, stats, http.StatusOK)
}

// - MongoDBに保存された詳細なエラーログを最新順にソートして最大50件取得する
// - 管理者ダッシュボード下部のテーブルデータとして返却する
func (h *AdminMetricsHandler) GetErrorLogsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logs, err := h.repo.GetErrorLogs(ctx, 50)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch error logs", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, logs, http.StatusOK)
}

// - 直近24時間における累積要求件数、成功率、および直近1時間以内のPeak TPS統計を演算して返却するハンドラー
func (h *AdminMetricsHandler) GetRequestMetricsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stats, err := h.repo.GetRequestMetrics(ctx)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch request metrics", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, stats, http.StatusOK)
}

// - MongoDBに保存された詳細なリクエストログを最新順にソートして最大50件取得するハンドラー
func (h *AdminMetricsHandler) GetRequestLogsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logs, err := h.repo.GetRequestLogs(ctx, 50)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch request logs", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, logs, http.StatusOK)
}

// - リアルタイム同時接続者数およびDAU/MAU統計を集計して返却するハンドラー
func (h *AdminMetricsHandler) GetActiveUserMetricsHandler(w http.ResponseWriter, r *http.Request) {
	// 1. リアルタイム同時接続者数 (インメモリから即座に取得)
	currentActive := metrics.GetRequestTracker().GetActiveUsersCount()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dauCount, mauCount, err := h.repo.GetDAUMAU(ctx)
	if err != nil {
		log.Printf("DB connection failure or error in GetActiveUserMetricsHandler: %v", err)
		// エラーが発生した場合でも現在の接続者数で代替して返します
		dauCount = currentActive
		mauCount = currentActive
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
func (h *AdminMetricsHandler) GetSSEMetricsHandler(w http.ResponseWriter, r *http.Request) {
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
func (h *AdminMetricsHandler) GetResponseTimeMetricsHandler(w http.ResponseWriter, r *http.Request) {
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

	endpoints, summary, err := h.repo.GetResponseTimeMetrics(ctx, since)
	if err != nil {
		utils.RespondWithError(w, "Failed to aggregate endpoint latency", http.StatusInternalServerError)
		return
	}

	result := models.ResponseTimeMetrics{
		Summary:   summary,
		Endpoints: endpoints,
	}

	utils.RespondWithJSON(w, result, http.StatusOK)
}

// - MongoDBの audit_logs コレクションから最新順で最大100件の監査ログを取得して返却
func (h *AdminMetricsHandler) GetAuditLogsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logs, err := h.repo.GetAuditLogs(ctx, 100)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch audit logs", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, logs, http.StatusOK)
}

// GetSystemMetricsHandler はリアルタイムのハードウェアリソース使用状況（CPU、Memory、Disk）を取得します
func (h *AdminMetricsHandler) GetSystemMetricsHandler(w http.ResponseWriter, r *http.Request) {
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

	utils.RespondWithJSON(w, metricsData, http.StatusOK)
}

// GetDBMetricsHandler は MongoDBのリアルタイム統計を取得します（接続数、DBサイズ、スロークエリ）
func (h *AdminMetricsHandler) GetDBMetricsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	metricsData, err := h.repo.GetDBMetrics(ctx)
	if err != nil {
		utils.RespondWithError(w, "Failed to get db metrics", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, metricsData, http.StatusOK)
}

