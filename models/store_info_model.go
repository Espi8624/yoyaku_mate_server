package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// store_info モデル
type Store struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	StoreName     string             `bson:"store_name" json:"store_name"`
	Address       string             `bson:"address" json:"address"`
	Phone         string             `bson:"phone" json:"phone"`
	UserID        primitive.ObjectID `bson:"user_id" json:"user_id"`
	StoreID       string             `bson:"store_id" json:"store_id"`
	StoreImageURL string             `bson:"store_image_url,omitempty" json:"store_image_url,omitempty"`
}

type StoreWithLicense struct {
	StoreID            string    `bson:"store_id"            json:"store_id"`
	StoreName          string    `bson:"store_name"          json:"store_name"`
	Address            string    `bson:"address"             json:"address"`
	Phone              string    `bson:"phone"               json:"phone"`
	LicenseImageURL    string    `bson:"license_image_url"   json:"license_image_url"`
	VerificationStatus string    `bson:"verification_status" json:"verification_status"`
	CreatedAt          time.Time `bson:"created_at"          json:"created_at"`
}
