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
type ShiftTable struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	StoreID       string             `bson:"store_id" json:"store_id"`
	WeekStartDate string             `bson:"week_start_date" json:"week_start_date"` // その週の月曜日 "YYYY-MM-DD"
	Shifts        []Shift            `bson:"shifts,omitempty" json:"shifts,omitempty"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at" json:"updated_at"`
}
