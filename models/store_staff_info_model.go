package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 認証ステータス定義
const (
	StaffStatusPending  = "PENDING"  // 審査中
	StaffStatusApproved = "APPROVED" // 承認済み
	StaffStatusRejected = "REJECTED" // 反則
)

// user_info モデル
type StoreStaffInfo struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	UserID      primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
	Role        string             `bson:"role" json:"role"`
	StoreID     string             `bson:"store_id,omitempty" json:"store_id,omitempty"`
	Status      string             `bson:"status" json:"status"`
	Permissions []string           `bson:"permissions,omitempty" json:"permissions,omitempty"`
	CreatedAt   time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time          `bson:"updated_at" json:"updated_at"`
}
