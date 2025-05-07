package models

import "go.mongodb.org/mongo-driver/bson/primitive"

// 店情報データ構造体
type StoreInfoItem struct {
	ID                   primitive.ObjectID `bson:"_id,omitempty" json:"id"` // MongoDB의 ObjectID
	StoreID              int32              `bson:"store_id" json:"store_id"`
	StoreName            string             `bson:"store_name" json:"store_name"`
	StoreAddress         string             `bson:"store_address" json:"store_address"`
	StoreTelNumber       string             `bson:"store_tel_number" json:"store_tel_number"`
	StoreEmail           string             `bson:"store_email" json:"store_email"`
	StoreOfficialWebSite string             `bson:"store_official_web_site" json:"store_official_web_site"`
	StoreDescription     string             `bson:"store_description" json:"store_description"`
	BusinessHours        BusinessHours      `bson:"business_hours" json:"business_hours"`
}

// 営業時間データ構造体
type BusinessHours struct {
	Monday    DayHours `bson:"Monday" json:"monday"`
	Tuesday   DayHours `bson:"Tuesday" json:"tuesday"`
	Wednesday DayHours `bson:"Wednesday" json:"wednesday"`
	Thursday  DayHours `bson:"Thursday" json:"thursday"`
	Friday    DayHours `bson:"Friday" json:"friday"`
	Saturday  DayHours `bson:"Saturday" json:"saturday"`
	Sunday    DayHours `bson:"Sunday" json:"sunday"`
}

// 一日営業時間データ構造体
type DayHours struct {
	Open  string `bson:"open" json:"open"`
	Close string `bson:"close" json:"close"`
}

// 店メニューデータ構造体
type StoreMenuItem struct {
	StoreID            int     `json:"store_id"`
	StoreName          string  `json:"store_name"`
	MenuNumber         int     `json:"menu_number"`
	MenuName           string  `json:"menu_name"`
	MenuPrice          float64 `json:"menu_price"`
	MenuDescription    string  `json:"menu_description"`
	MenuImage          string  `json:"menu_image"`
	MenuActivationFlag bool    `json:"menu_activation_flag"`
}

// 店予約状況データ構造体
type StoreReservationItem struct {
	StoreID      int    `json:"store_id"`
	StoreName    string `json:"store_name"`
	CustomerName string `json:"customer_name"`
	Details      string `json:"details"`
	ReservedDate string `json:"reserved_date"`
	ReservedTime string `json:"reserved_time"`
	TimeStamp    string `json:"time_stamp"`
}
