package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"yoyaku_mate_server/db"

	"github.com/shirou/gopsutil/v3/cpu"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// - AlertPage(管理者画面)に表示されている3つの閾値をそのまま実装する
const (
	alertCheckInterval    = 30 * time.Second
	alertWindow           = 5 * time.Minute
	alertCooldown         = 15 * time.Minute // - 同じ種類のアラートを再送するまでの最小間隔(スパム防止)
	errorRateThresholdPct = 1.0              // エラー率 > 1% (5分平均)
	latencyThresholdMs    = 500.0            // APIレイテンシ > 500ms (5分平均)
	cpuThresholdPct       = 90.0             // CPU使用率 > 90% (1分連続)
	cpuSustainedChecks    = 2                // 30秒間隔で2回連続 ≒ 1分間持続
)

// AlertWorker は一定間隔でエラー率/応答時間/CPU使用率を監視し、
// 閾値超過時にSlack Incoming Webhookへ通知するバックグラウンドワーカー。
type AlertWorker struct {
	webhookURL string

	mu         sync.Mutex
	lastSentAt map[string]time.Time // アラート種別ごとのクールダウン管理
	cpuStreak  int                  // CPU閾値超過が何回連続したか
}

// StartAlertWorker はワーカーを生成し、webhookURLが設定されている場合のみ監視ループを開始する。
// - 未設定時は機能を無効化するだけで、サーバー起動自体は妨げない(安全側デフォルト)
func StartAlertWorker(webhookURL string) *AlertWorker {
	w := &AlertWorker{
		webhookURL: webhookURL,
		lastSentAt: make(map[string]time.Time),
	}
	if webhookURL == "" {
		log.Println("SLACK_WEBHOOK_URL is not set. Alert notifications are disabled.")
		return w
	}
	go w.loop()
	return w
}

func (w *AlertWorker) loop() {
	ticker := time.NewTicker(alertCheckInterval)
	defer ticker.Stop()
	for range ticker.C {
		w.checkErrorRate()
		w.checkLatency()
		w.checkCPU()
	}
}

// - 直近5分間のリクエストログからエラー率(status_code >= 400)を集計する
func (w *AlertWorker) checkErrorRate() {
	collection := db.GetCollection(db.DatabaseName, db.CollectionRequestLogs)
	if collection == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	since := time.Now().UTC().Add(-alertWindow)
	total, err := collection.CountDocuments(ctx, bson.M{"timestamp": bson.M{"$gte": since}})
	if err != nil || total == 0 {
		return
	}
	errCount, err := collection.CountDocuments(ctx, bson.M{
		"timestamp":   bson.M{"$gte": since},
		"status_code": bson.M{"$gte": 400},
	})
	if err != nil {
		return
	}

	rate := float64(errCount) / float64(total) * 100
	if rate > errorRateThresholdPct {
		w.notify("error_rate", fmt.Sprintf(
			":rotating_light: エラー率が閾値を超えました: %.2f%% (直近5分, %d/%d件, 閾値 %.0f%%)",
			rate, errCount, total, errorRateThresholdPct,
		))
	}
}

// - 直近5分間のリクエストログから平均応答時間を集計する
func (w *AlertWorker) checkLatency() {
	collection := db.GetCollection(db.DatabaseName, db.CollectionRequestLogs)
	if collection == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	since := time.Now().UTC().Add(-alertWindow)
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{{Key: "timestamp", Value: bson.D{{Key: "$gte", Value: since}}}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "avg_ms", Value: bson.D{{Key: "$avg", Value: "$response_time"}}},
		}}},
	}

	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		return
	}
	defer cursor.Close(ctx)

	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil || len(results) == 0 {
		return
	}

	avgMs := toFloat64(results[0]["avg_ms"])
	if avgMs > latencyThresholdMs {
		w.notify("latency", fmt.Sprintf(
			":rotating_light: 平均応答時間が閾値を超えました: %.0fms (直近5分, 閾値 %.0fms)",
			avgMs, latencyThresholdMs,
		))
	}
}

// - サーバープロセス自身のCPU使用率を確認し、閾値超過が連続した場合のみ通知する
func (w *AlertWorker) checkCPU() {
	percents, err := cpu.Percent(0, false)
	if err != nil || len(percents) == 0 {
		return
	}
	current := percents[0]

	w.mu.Lock()
	if current > cpuThresholdPct {
		w.cpuStreak++
	} else {
		w.cpuStreak = 0
	}
	streak := w.cpuStreak
	w.mu.Unlock()

	if streak >= cpuSustainedChecks {
		sustainedSeconds := int(alertCheckInterval.Seconds()) * cpuSustainedChecks
		w.notify("cpu", fmt.Sprintf(
			":rotating_light: CPU使用率が%d秒以上%.0f%%を超えています: %.1f%%",
			sustainedSeconds, cpuThresholdPct, current,
		))
	}
}

// - クールダウン期間内は同じ種類のアラートを再送しない(閾値監視のような継続的な状態向け)
func (w *AlertWorker) notify(alertKey, message string) {
	w.mu.Lock()
	if last, ok := w.lastSentAt[alertKey]; ok && time.Since(last) < alertCooldown {
		w.mu.Unlock()
		return
	}
	w.lastSentAt[alertKey] = time.Now()
	w.mu.Unlock()

	SendSlackMessage(w.webhookURL, message)
}

// SendSlackMessage は指定したSlack Incoming Webhook URLへメッセージを送信する。
// - 店舗の許可証申請通知のような単発イベント通知からも直接呼び出せるよう、
//   クールダウンを伴わない形でAlertWorkerから切り出している
// - webhookURLが空の場合は何もしない(機能未設定時の安全側デフォルト)
func SendSlackMessage(webhookURL, message string) {
	if webhookURL == "" {
		return
	}

	payload, err := json.Marshal(map[string]string{"text": message})
	if err != nil {
		log.Printf("Failed to marshal Slack alert payload: %v", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		log.Printf("Failed to build Slack alert request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to send Slack alert: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		log.Printf("Slack alert webhook returned unexpected status: %d", resp.StatusCode)
	}
}

// - interface{}からfloat64へ安全に型変換するヘルパー(handlers.toFloat64と同等だが、
//   パッケージを跨いだ依存を避けるためここに複製する)
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
