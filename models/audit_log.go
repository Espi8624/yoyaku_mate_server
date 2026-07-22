package models

import "time"

// - 管理者操作の監査ログデータモデル
// - MongoDBの audit_logs コレクションへの保存およびJSON APIレスポンスに使用
type AuditLog struct {
	ID string `json:"id" bson:"_id,omitempty"`
	// - 発生日時
	Timestamp time.Time `json:"timestamp" bson:"timestamp"`
	// - 実行されたアクション種別（例: "STORE_APPROVED", "STORE_REJECTED"）
	Action string `json:"action" bson:"action"`
	// - アクションの対象説明（例: "Store ID: 1042"）
	Target string `json:"target" bson:"target"`
	// - 処理結果: "SUCCESS" | "FAILED"
	Status string `json:"status" bson:"status"`
	// - 追加の変更詳細（任意）
	Details string `json:"details,omitempty" bson:"details,omitempty"`
}
