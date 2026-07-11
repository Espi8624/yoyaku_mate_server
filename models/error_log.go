package models

import "time"

// - 個別のエラーログの詳細情報を表すデータモデル
// - MongoDBのerror_logsコレクションへの保存およびJSON APIのレスポンス時に使用する
type ErrorLog struct {
	ID         string    `json:"id" bson:"_id,omitempty"`
	Timestamp  time.Time `json:"timestamp" bson:"timestamp"`
	ErrorType  string    `json:"error_type" bson:"error_type"`
	Message    string    `json:"message" bson:"message"`
	Path       string    `json:"path" bson:"path"`
	Method     string    `json:"method" bson:"method"`
	ClientIP   string    `json:"client_ip" bson:"client_ip"`
	StackTrace string    `json:"stack_trace,omitempty" bson:"stack_trace,omitempty"`
}

// - 管理者エラーダッシュボード上部カードに表示する累積エラー統計のデータモデル
// - 各HTTPレスポンスステータスおよび接続切断タイプ別の集計値を保持する
type ErrorMetrics struct {
	Count500 int64 `json:"count_500"`
	Count400 int64 `json:"count_400"`
	CountDB  int64 `json:"count_db"`
	CountSSE int64 `json:"count_sse"`
}
