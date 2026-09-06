package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Shift 週内の1件の勤務シフト
type Shift struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	StaffID   primitive.ObjectID `bson:"staff_id" json:"staff_id"`
	Day       string             `bson:"day" json:"day"`               // Weekday定数 (monday..sunday)
	StartTime string             `bson:"start_time" json:"start_time"` // "HH:MM"
	EndTime   string             `bson:"end_time" json:"end_time"`     // "HH:MM"
}

// ShiftTable 店舗の週単位シフト表
// マネージャーが明示的に作成するまで、その週のドキュメントは存在しない
//
// 下書き(Shifts)と確定版(PublishedShifts)の2本立てで持つ。
// マネージャーの編集・自動配置・修正依頼の適用は全て下書きにだけ効き、
// 「確定」(PublishShiftTableHandler)を押した時点で初めて確定版へコピーされる。
// スタッフに見せるのは常に確定版のみで、作りかけのシフト表は一切見えない
type ShiftTable struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	StoreID       string             `bson:"store_id" json:"store_id"`
	WeekStartDate string             `bson:"week_start_date" json:"week_start_date"` // その週の月曜日 "YYYY-MM-DD"

	// Shifts マネージャーが編集中の下書き。GET時、マネージャーにはこちらを返す
	Shifts []Shift `bson:"shifts,omitempty" json:"shifts,omitempty"`

	// PublishedShifts 確定済みでスタッフに公開されている版。
	// GET時、スタッフにはこちらを Shifts として返す (クライアントは区別せず描画できる)
	PublishedShifts []Shift `bson:"published_shifts,omitempty" json:"-"`

	// PublishedAt 最後に確定した日時。nil は「一度も確定していない」= スタッフには未公開
	PublishedAt *time.Time `bson:"published_at,omitempty" json:"published_at,omitempty"`

	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`

	// HasUnpublishedChanges 下書きに未確定の変更が残っているか (DBには保存せず、GET時に算出)。
	// マネージャー向けのレスポンスにだけ含める
	HasUnpublishedChanges bool `bson:"-" json:"has_unpublished_changes"`
}
