package models

import "go.mongodb.org/mongo-driver/bson/primitive"

// menu_list モデル
type MenuList struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	StoreID     string             `bson:"store_id" json:"store_id"`
	MenuID      string             `bson:"menu_id" json:"menu_id"`
	Title       string             `bson:"title" json:"title"`
	Description string             `bson:"description" json:"description"`
	Price       int                `bson:"price" json:"price"`
	Category    string             `bson:"category" json:"category"`
	ImageURL    string             `bson:"image_url" json:"image_url"`
	CreatedAt   string             `bson:"created_at" json:"created_at"`
	UpdatedAt   string             `bson:"updated_at" json:"updated_at"`
	MenuStatus  string             `bson:"menu_status" json:"menu_status"`
}
