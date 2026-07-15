package models

// SSEBrokerStats は、個別ブローカーの統計応答構造を定義します
type SSEBrokerStats struct {
	// 購読中の店舗またはユーザーキーの数
	ActiveKeys int `json:"active_keys"`
	// 全体の有効な接続（チャネル）数
	TotalConnections int `json:"total_connections"`
	// 平均接続維持時間（秒）
	AvgUptimeSeconds float64 `json:"avg_uptime_seconds"`
}

// SSEMetrics は、SSEの全体ステータスの応答モデルを定義します
type SSEMetrics struct {
	// 店舗待ちリストブローカーの統計
	StoreBroker SSEBrokerStats `json:"store_broker"`
	// 個別待ち顧客ブローカーの統計
	WaitingUserBroker SSEBrokerStats `json:"waiting_user_broker"`
	// 両ブローカーの合算接続数
	TotalConnections int `json:"total_connections"`
	// 接続の健全性状態: "HEALTHY" | "IDLE"
	Health string `json:"health"`
}
