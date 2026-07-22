package metrics

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	tracker *ErrorTracker
	once    sync.Once
)

// - サーバー内で発生したエラーのカウントを一時的に集計し、詳細ログをメモリにバッファリングして非同期でバッチ保存するための構造体
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

var (
	requestTracker *RequestTracker
	reqOnce        sync.Once
)

// - サーバー内部で発生した全HTTPリクエスト情報をバッファリングし、非同期でバッチ保存するための構造体
type RequestTracker struct {
	logBuffer   []models.RequestLog
	activeUsers map[string]time.Time // key: ClientIP, value: last request time
	mu          sync.Mutex
}

// - RequestTrackerのシングルトンインスタンスを返却し、初回呼び出し時にバックグラウンドバッチ保存ワーカーを実行
func GetRequestTracker() *RequestTracker {
	reqOnce.Do(func() {
		requestTracker = &RequestTracker{
			logBuffer:   make([]models.RequestLog, 0, 100),
			activeUsers: make(map[string]time.Time),
		}
		go requestTracker.startBatchWorker()
	})
	return requestTracker
}

// - 5分スライディングウィンドウの外部からリアルタイムの同時接続者数を安全に取得するメソッド
func (t *RequestTracker) GetActiveUsersCount() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	var count int64
	for _, lastActive := range t.activeUsers {
		if now.Sub(lastActive) <= 5*time.Minute {
			count++
		}
	}
	return count
}

// - 古い(5分超過)セッションデータをメモリからクリア (ロックを獲得した状態で呼び出すこと)
func (t *RequestTracker) cleanupActiveUsers() {
	now := time.Now()
	for ip, lastActive := range t.activeUsers {
		if now.Sub(lastActive) > 5*time.Minute {
			delete(t.activeUsers, ip)
		}
	}
}

// - リクエストログオブジェクトをバッファに追加し、メモリオーバーフロー防止のため最大1,000件までバッファリング
func (t *RequestTracker) RecordRequest(reqLog models.RequestLog) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.logBuffer) < 1000 {
		t.logBuffer = append(t.logBuffer, reqLog)
	}

	// リアルタイム接続情報の更新
	if reqLog.ClientIP != "" {
		t.activeUsers[reqLog.ClientIP] = time.Now()
	}
}

// - 5秒周期でメモリバッファに蓄積されたリクエストログをMongoDBへ一括保存(Flush)するバックグラウンドワーカー
func (t *RequestTracker) startBatchWorker() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		t.flush()
	}
}

// - メモリバッファの内容をMongoDBへバルクインサートし、完了後にバッファを初期化
func (t *RequestTracker) flush() {
	t.mu.Lock()
	if len(t.logBuffer) == 0 {
		t.cleanupActiveUsers()
		t.mu.Unlock()
		return
	}

	// バルクインサート用のログコピーおよび重複のない日別接続IPの抽出
	type dailyKey struct {
		Date     string
		ClientIP string
	}
	uniqueActive := make(map[dailyKey]time.Time)
	logsToInsert := make([]interface{}, len(t.logBuffer))
	for i, logItem := range t.logBuffer {
		logsToInsert[i] = logItem
		if logItem.ClientIP != "" && logItem.ClientIP != "127.0.0.1" {
			dateStr := logItem.Timestamp.Format("2006-01-02")
			uniqueActive[dailyKey{Date: dateStr, ClientIP: logItem.ClientIP}] = logItem.Timestamp
		}
	}
	t.logBuffer = make([]models.RequestLog, 0, 100)
	t.cleanupActiveUsers()
	t.mu.Unlock()

	collection := db.GetCollection(db.DatabaseName, db.CollectionRequestLogs)
	if collection == nil {
		log.Println("Failed to get MongoDB request_logs collection")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. 全てのリクエストログをバルクインサート
	_, err := collection.InsertMany(ctx, logsToInsert)
	if err != nil {
		log.Printf("Failed to bulk insert request logs: %v", err)
	}

	// 2. ユニークな接続ユーザー情報をdaily_active_usersコレクションにバルクアップサート
	if len(uniqueActive) > 0 {
		var writes []mongo.WriteModel
		for key, timestamp := range uniqueActive {
			filter := bson.M{"date": key.Date, "client_ip": key.ClientIP}
			update := bson.M{
				"$set": bson.M{
					"timestamp": timestamp,
				},
			}
			model := mongo.NewUpdateOneModel().
				SetFilter(filter).
				SetUpdate(update).
				SetUpsert(true)
			writes = append(writes, model)
		}

		dauCollection := db.GetCollection(db.DatabaseName, db.CollectionDailyActiveUsers)
		if dauCollection != nil {
			_, err := dauCollection.BulkWrite(ctx, writes)
			if err != nil {
				log.Printf("Failed to bulk write daily active users: %v", err)
			}
		}
	}
}


// =====================================================================
// AuditTracker — 管理者操作の監査ログバッファおよび非同期保存
// =====================================================================

var (
	auditTracker *AuditTracker
	auditOnce    sync.Once
)

// - 管理者操作の監査ログをメモリ内にバッファリングし、非同期でMongoDBに一括保存する構造体
type AuditTracker struct {
	logBuffer []models.AuditLog
	mu        sync.Mutex
}

// - AuditTracker のシングルトンインスタンスを返し、初回呼び出し時に5秒周期のバッチワーカーを開始
func GetAuditTracker() *AuditTracker {
	auditOnce.Do(func() {
		auditTracker = &AuditTracker{
			logBuffer: make([]models.AuditLog, 0, 100),
		}
		go auditTracker.startBatchWorker()
	})
	return auditTracker
}

// - 監査ログオブジェクトをメモリバッファに追加（メモリオーバーフロー防止のため最大1,000件制限）
func (t *AuditTracker) RecordAudit(auditLog models.AuditLog) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.logBuffer) < 1000 {
		t.logBuffer = append(t.logBuffer, auditLog)
	}
}

// - 5秒周期でメモリバッファに蓄積された監査ログをMongoDBに一括保存するバッチワーカー
func (t *AuditTracker) startBatchWorker() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		t.flush()
	}
}

// - メモリバッファの内容をMongoDBの audit_logs コレクションに BulkInsert 後、バッファを初期化
func (t *AuditTracker) flush() {
	t.mu.Lock()
	if len(t.logBuffer) == 0 {
		t.mu.Unlock()
		return
	}

	logsToInsert := make([]interface{}, len(t.logBuffer))
	for i, logItem := range t.logBuffer {
		logsToInsert[i] = logItem
	}
	t.logBuffer = make([]models.AuditLog, 0, 100)
	t.mu.Unlock()

	collection := db.GetCollection(db.DatabaseName, db.CollectionAuditLogs)
	if collection == nil {
		log.Println("Failed to get MongoDB audit_logs collection")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := collection.InsertMany(ctx, logsToInsert); err != nil {
		log.Printf("Failed to bulk insert audit logs: %v", err)
	}
}
