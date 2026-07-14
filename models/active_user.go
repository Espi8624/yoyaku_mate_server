package models

import "time"

// - 日別アクティブユーザー情報を定義するデータモデル
// - MongoDBのdaily_active_usersコレクションで使用
type ActiveUserLog struct {
	ID        string    `json:"id" bson:"_id,omitempty"`
	Date      string    `json:"date" bson:"date"`
	ClientIP  string    `json:"client_ip" bson:"client_ip"`
	Timestamp time.Time `json:"timestamp" bson:"timestamp"`
}

// - アクティブユーザーダッシュボード上部サマリーカードに表示する統計データモデル
type ActiveUserMetrics struct {
	CurrentActiveUsers int64 `json:"current_active_users"`
	DailyActiveUsers   int64 `json:"daily_active_users"`
	MonthlyActiveUsers int64 `json:"monthly_active_users"`
}
