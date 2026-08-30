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

// 勤務可能時間帯定義
const (
	TimeBlockMorning   = "MORNING"   // 午前
	TimeBlockAfternoon = "AFTERNOON" // 午後
	TimeBlockEvening   = "EVENING"   // 夜間
)

// Availability 曜日ごとの勤務可能時間帯リスト
// 各曜日のリストが空の場合、その曜日は「勤務不可」を意味する
type Availability struct {
	Monday    []string `bson:"monday,omitempty" json:"monday,omitempty"`
	Tuesday   []string `bson:"tuesday,omitempty" json:"tuesday,omitempty"`
	Wednesday []string `bson:"wednesday,omitempty" json:"wednesday,omitempty"`
	Thursday  []string `bson:"thursday,omitempty" json:"thursday,omitempty"`
	Friday    []string `bson:"friday,omitempty" json:"friday,omitempty"`
	Saturday  []string `bson:"saturday,omitempty" json:"saturday,omitempty"`
	Sunday    []string `bson:"sunday,omitempty" json:"sunday,omitempty"`
}

// user_info モデル
type StoreStaffInfo struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	UserID       primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
	Role         string             `bson:"role" json:"role"`
	StoreID      string             `bson:"store_id,omitempty" json:"store_id,omitempty"`
	Status       string             `bson:"status" json:"status"`
	Permissions  []string           `bson:"permissions,omitempty" json:"permissions,omitempty"`
	Availability Availability       `bson:"availability,omitempty" json:"availability,omitempty"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updated_at"`
}
