package models

import "time"

// - 個々のAPIリクエストに関する詳細ログ情報を定義するデータモデル
// - MongoDBのrequest_logsコレクションおよびAPI応答時に使用
type RequestLog struct {
	ID           string    `json:"id" bson:"_id,omitempty"`
	Timestamp    time.Time `json:"timestamp" bson:"timestamp"`
	Path         string    `json:"path" bson:"path"`
	Method       string    `json:"method" bson:"method"`
	StatusCode   int       `json:"status_code" bson:"status_code"`
	ResponseTime int64     `json:"response_time" bson:"response_time"` // 応答時間 (ミリ秒)
	ClientIP     string    `json:"client_ip" bson:"client_ip"`
}

// - 管理者リクエストダッシュボード上部カードに表示する要約統計データモデル
// - 24時間の要求件数、成功率、Peak TPS値を含む
type RequestMetrics struct {
	TotalRequests24h int64   `json:"total_requests_24h"`
	SuccessRate      float64 `json:"success_rate"`
	PeakTPS1h        int64   `json:"peak_tps_1h"`
}
