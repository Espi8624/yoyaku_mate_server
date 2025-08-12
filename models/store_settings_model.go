package models

import "go.mongodb.org/mongo-driver/bson/primitive"

// store_settings モデル
type StoreSetting struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"_id,omitempty"`
	StoreID   string             `bson:"store_id" json:"store_id"`
	ManagerID string             `bson:"manager_id" json:"manager_id"`
	Settings  Settings           `bson:"settings" json:"settings"`
	UpdatedAt string             `bson:"updated_at" json:"updated_at"`
}

type Settings struct {
	OperatingHours map[string]StoreDayHours `bson:"operating_hours" json:"operating_hours"`
	ClosedDays     ClosedDays               `bson:"closed_days" json:"closed_days"`
	WaitingPolicy  WaitingPolicy            `bson:"waiting_policy" json:"waiting_policy"`
}

type StoreDayHours struct {
	Start string `bson:"start" json:"start"`
	End   string `bson:"end" json:"end"`
}

type ClosedDays struct {
	SpecificDates  []string `bson:"specific_dates" json:"specific_dates"`
	RegularWeekly  []string `bson:"regular_weekly" json:"regular_weekly"`
	RegularMonthly []string `bson:"regular_monthly" json:"regular_monthly"`
	HolidayClosure bool     `bson:"holiday_closure" json:"holiday_closure"`
}

type WaitingPolicy struct {
	MaxWaitingCount       int `bson:"max_waiting_count" json:"max_waiting_count"`
	EstimatedWaitingCount int `bson:"estimated_waiting_count" json:"estimated_waiting_count"`
}
