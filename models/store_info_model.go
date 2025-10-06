package models

import "go.mongodb.org/mongo-driver/bson/primitive"

// store_info モデル
type Store struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	StoreName string             `bson:"store_name" json:"store_name"`
	Address   string             `bson:"address" json:"address"`
	Phone     string             `bson:"phone" json:"phone"`
	UserID    primitive.ObjectID `bson:"user_id" json:"user_id"`
	StoreID   string             `bson:"store_id" json:"store_id"`
}
