package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ユーザー情報データ構造体
type UserInfoItem struct {
	ID          primitive.ObjectID `json:"id" bson:"_id"`
	UserID      string             `json:"user_id" bson:"user_id"`
	UserName    string             `json:"user_name" bson:"user_name"`
	DisplayName string             `json:"display_name" bson:"display_name"`
	Email       string             `json:"email" bson:"email"`
	PhoneNumber string             `json:"phone_number" bson:"phone_number"`
	FirstName   string             `json:"first_name" bson:"first_name"`
	LastName    string             `json:"last_name" bson:"last_name"`
	CreatedAt   time.Time          `json:"created_at" bson:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at" bson:"updated_at"`
	IsActive    bool               `json:"is_active" bson:"is_active"`
	Role        string             `json:"role" bson:"role"`
	LastLogin   time.Time          `json:"last_login,omitempty" bson:"last_login,omitempty"`
}
