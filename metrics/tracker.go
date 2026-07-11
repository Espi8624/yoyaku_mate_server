package metrics

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"
)

var (
	tracker *ErrorTracker
	once    sync.Once
)

// - サーバー内で発生したエラーのカウントを一時的に集計し、詳細ログをメモリにバッファリングして非同期でバッチ保存するための構造체
type ErrorTracker struct {
	Count500 int64
	Count400 int64
	CountDB  int64
	CountSSE int64

	logBuffer []models.ErrorLog
	mu        sync.Mutex
}

// - ErrorTrackerのシングルトンインスタンスを返却し、初回呼び出し時に非同期バッチ転送用のバックグラウンドワーカーを実行する
func GetTracker() *ErrorTracker {
	once.Do(func() {
		tracker = &ErrorTracker{
			logBuffer: make([]models.ErrorLog, 0, 100),
		}
		go tracker.startBatchWorker()
	})
	return tracker
}

// - 発生したエラーオブジェクトを受け取り、メモリ内のエラーカウントをアトミックにインクリメントし、メモリオーバーフロー防止のため最大1,000件までバッファに追加する
func (t *ErrorTracker) RecordError(errLog models.ErrorLog) {
	// Increment counters
	switch errLog.ErrorType {
	case "500_INTERNAL_ERROR":
		atomic.AddInt64(&t.Count500, 1)
	case "400_BAD_REQUEST":
		atomic.AddInt64(&t.Count400, 1)
	case "DATABASE_ERROR":
		atomic.AddInt64(&t.CountDB, 1)
	case "SSE_DISCONNECT":
		atomic.AddInt64(&t.CountSSE, 1)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Rate limiting / Sampling (prevent memory overflow if spike happens)
	if len(t.logBuffer) < 1000 {
		t.logBuffer = append(t.logBuffer, errLog)
	}
}

// - 5秒周期でメモリバッファに蓄積されたエラーログをMongoDBへ一括保存（Flush）するバックグラウンドワーカー
func (t *ErrorTracker) startBatchWorker() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		t.flush()
	}
}

func (t *ErrorTracker) flush() {
	t.mu.Lock()
	if len(t.logBuffer) == 0 {
		t.mu.Unlock()
		return
	}
	
	logsToInsert := make([]interface{}, len(t.logBuffer))
	for i, logItem := range t.logBuffer {
		logsToInsert[i] = logItem
	}
	t.logBuffer = make([]models.ErrorLog, 0, 100)
	t.mu.Unlock()

	collection := db.GetCollection(db.DatabaseName, db.CollectionErrorLogs)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := collection.InsertMany(ctx, logsToInsert)
	if err != nil {
		log.Printf("Failed to bulk insert error logs: %v", err)
	}
}

func (t *ErrorTracker) GetMetrics() models.ErrorMetrics {
	return models.ErrorMetrics{
		Count500: atomic.LoadInt64(&t.Count500),
		Count400: atomic.LoadInt64(&t.Count400),
		CountDB:  atomic.LoadInt64(&t.CountDB),
		CountSSE: atomic.LoadInt64(&t.CountSSE),
	}
}
