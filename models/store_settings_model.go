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
	OperatingHours   map[string]StoreDayHours `bson:"operating_hours" json:"operating_hours"`
	ClosedDays       ClosedDays               `bson:"closed_days" json:"closed_days"`
	WaitingPolicy    WaitingPolicy            `bson:"waiting_policy" json:"waiting_policy"`
	Is24Hours        bool                     `bson:"is_24_hours" json:"is_24_hours"`
	ResetTime        string                   `bson:"reset_time" json:"reset_time"`                 // HH:MM format
	AIAdditionalInfo string                   `bson:"ai_additional_info" json:"ai_additional_info"` // AIへの追加情報
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
	MaxWaitingCount         int  `bson:"max_waiting_count" json:"max_waiting_count"`
	EstimatedWaitTime       int  `bson:"estimated_wait_time" json:"estimated_wait_time"`
	EnableMenuSelection     bool `bson:"enable_menu_selection" json:"enable_menu_selection"`
	RequireOneMenuPerPerson bool `bson:"require_one_menu_per_person" json:"require_one_menu_per_person"`
}
