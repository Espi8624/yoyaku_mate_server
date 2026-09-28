package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 認証ステータス定義
const (
	StaffStatusPending   = "PENDING"   // 審査中
	StaffStatusApproved  = "APPROVED"  // 承認済み
	StaffStatusRejected  = "REJECTED"  // 反則
	StaffStatusWithdrawn = "WITHDRAWN" // 会員退会済み(店舗の所属記録としては残す)
)

// UnavailableRange 1件の勤務不可時間帯。終日不可の場合は AllDay=true とし、
// StartTime/EndTime は空にする
type UnavailableRange struct {
	AllDay    bool   `bson:"all_day" json:"all_day"`
	StartTime string `bson:"start_time,omitempty" json:"start_time,omitempty"` // "HH:MM"
	EndTime   string `bson:"end_time,omitempty" json:"end_time,omitempty"`     // "HH:MM"
}

// Availability 曜日ごとの勤務不可時間帯リスト (1曜日に複数件持てる)
// 各曜日のリストが空の場合、その曜日は「終日勤務可能」を意味する
type Availability struct {
	Monday    []UnavailableRange `bson:"monday,omitempty" json:"monday,omitempty"`
	Tuesday   []UnavailableRange `bson:"tuesday,omitempty" json:"tuesday,omitempty"`
	Wednesday []UnavailableRange `bson:"wednesday,omitempty" json:"wednesday,omitempty"`
	Thursday  []UnavailableRange `bson:"thursday,omitempty" json:"thursday,omitempty"`
	Friday    []UnavailableRange `bson:"friday,omitempty" json:"friday,omitempty"`
	Saturday  []UnavailableRange `bson:"saturday,omitempty" json:"saturday,omitempty"`
	Sunday    []UnavailableRange `bson:"sunday,omitempty" json:"sunday,omitempty"`
}

// user_info モデル
type StoreStaffInfo struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	UserID      primitive.ObjectID `bson:"user_id,omitempty" json:"user_id,omitempty"`
	Role        string             `bson:"role" json:"role"`
	StoreID     string             `bson:"store_id,omitempty" json:"store_id,omitempty"`
	Status      string             `bson:"status" json:"status"`
	Permissions []string           `bson:"permissions,omitempty" json:"permissions,omitempty"`
	// 一度でも承認(APPROVED)されたことがあるかどうか。REJECTEDは「申請却下」
	// (一度も承認されていない)と「承認取り消し」(過去に承認されていた)の
	// 両方で共有される状態のため、この2つをクライアント側で区別するために保持する
	HasBeenApproved bool         `bson:"has_been_approved,omitempty" json:"has_been_approved,omitempty"`
	Availability    Availability `bson:"availability,omitempty" json:"availability,omitempty"`
	CreatedAt       time.Time    `bson:"created_at" json:"created_at"`
	UpdatedAt       time.Time    `bson:"updated_at" json:"updated_at"`
}
