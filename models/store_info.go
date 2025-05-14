package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 店情報データ構造体
type StoreInfoItem struct {
	ID                   primitive.ObjectID `bson:"_id,omitempty" json:"id"` // MongoDB의 ObjectID
	StoreID              string             `bson:"store_id" json:"store_id"`
	StoreName            string             `bson:"store_name" json:"store_name"`
	StoreAddress         string             `bson:"store_address" json:"store_address"`
	StoreTelNumber       string             `bson:"store_tel_number" json:"store_tel_number"`
	StoreEmail           string             `bson:"store_email" json:"store_email"`
	StoreOfficialWebSite string             `bson:"store_official_web_site" json:"store_official_web_site"`
	StoreDescription     string             `bson:"store_description" json:"store_description"`
	BusinessHours        BusinessHours      `bson:"business_hours" json:"business_hours"`
	StoreCategory        string             `bson:"store_category" json:"store_category"`
	StoreRating          float64            `bson:"store_rating" json:"store_rating"`
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
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	MenuID      string             `bson:"menu_id" json:"menu_id"`
	StoreID     string             `bson:"store_id" json:"store_id"`
	MenuName    string             `bson:"menu_name" json:"menu_name"`
	Price       float64            `bson:"price" json:"price"`
	Category    string             `bson:"category" json:"category"`
	Description string             `bson:"description" json:"description"`
	Ingredients []string           `bson:"ingredients" json:"ingredients"`
	Available   bool               `bson:"available" json:"available"`
}

// コメントデータ構造体
type StoreCommentItem struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	StoreID     string             `bson:"store_id" json:"store_id"`
	Rating      float64            `bson:"rating" json:"rating"`
	CommentText string             `bson:"comment_text" json:"comment_text"`
	CommentID   string             `bson:"comment_id" json:"comment_id"`
	UserID      string             `bson:"user_id" json:"user_id"`
	Timestamp   time.Time          `bson:"timestamp" json:"timestamp"`
}
