package models

import "go.mongodb.org/mongo-driver/bson/primitive"

// store_settings モデル
type StoreSetting struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"_id,omitempty"`
	StoreID   string             `bson:"store_id" json:"store_id"`
	ManagerID string             `bson:"manager_id" json:"manager_id"`
	// ManagerName マネージャーの表示名。DBには保存せず、GET応答時に user_info から解決して埋める
	ManagerName string   `bson:"-" json:"manager_name,omitempty"`
	Settings    Settings `bson:"settings" json:"settings"`
	UpdatedAt   string   `bson:"updated_at" json:"updated_at"`
}

type Settings struct {
	OperatingHours     map[string]StoreDayHours       `bson:"operating_hours" json:"operating_hours"`
	ClosedDays         ClosedDays                     `bson:"closed_days" json:"closed_days"`
	WaitingPolicy      WaitingPolicy                  `bson:"waiting_policy" json:"waiting_policy"`
	Is24Hours          bool                           `bson:"is_24_hours" json:"is_24_hours"`
	ResetTime          string                         `bson:"reset_time" json:"reset_time"`                                         // HH:MM format
	AIAdditionalInfo   string                         `bson:"ai_additional_info" json:"ai_additional_info"`                         // AIへの追加情報
	RequiredStaffCount map[string]DayStaffRequirement `bson:"required_staff_count,omitempty" json:"required_staff_count,omitempty"` // 曜日別の必要人数・1人あたりのシフト時間数
}

type StoreDayHours struct {
	Start string `bson:"start" json:"start"`
	End   string `bson:"end" json:"end"`
}

// DayStaffRequirement 1曜日分の、各時間帯に同時に必要な人員数(Count)と、
// その曜日の営業時間中に交代(引き継ぎ)が何回発生するか(ShiftChangeCount、交代回数)。
// 交代がN回発生する場合、営業時間はN+1個の時間帯に区切られる(柵の杭と区間の関係と同じ)。
// シフトの開始時刻は指定せず、自動配置時に営業時間をShiftChangeCount+1個の
// 等しい長さのブロックに均等分割し、各ブロックにCount名を配置する
// (例: Count=3, ShiftChangeCount=2, 営業時間9:00〜22:00(13h) → 9:00〜13:20/13:20〜17:40/17:40〜22:00の3ブロックに分割、各ブロックに3名)
type DayStaffRequirement struct {
	Count            int `bson:"count" json:"count"`
	ShiftChangeCount int `bson:"shift_change_count" json:"shift_change_count"`
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
