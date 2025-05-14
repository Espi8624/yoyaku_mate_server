package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// StoreCommentItem represents a comment on a store
type CommentInfoItem struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	StoreID     string             `bson:"store_id" json:"store_id"`
	Rating      float64            `bson:"rating" json:"rating"`
	CommentText string             `bson:"comment_text" json:"comment_text"`
	CommentID   string             `bson:"comment_id" json:"comment_id"`
	UserID      string             `bson:"user_id" json:"user_id"`
	Timestamp   time.Time          `bson:"timestamp" json:"timestamp"`

	StoreInfo StoreInfo `bson:"store_info" json:"store_info"`
	UserInfo  UserInfo  `bson:"user_info" json:"user_info"`
}

// StoreInfo represents the store details
type StoreInfo struct {
	StoreID   string `bson:"store_id" json:"store_id"`
	StoreName string `bson:"store_name" json:"store_name"`
}

// UserInfo represents the user details
type UserInfo struct {
	UserID      string `bson:"user_id" json:"user_id"`
	UserName    string `bson:"display_name" json:"user_name"`
	Email       string `bson:"email" json:"email"`
	PhoneNumber string `bson:"phone_number" json:"phone_number"`
}
