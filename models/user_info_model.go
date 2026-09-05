package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// user_info モデル
type User struct {
	ID               primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
	FirebaseUID      string             `bson:"firebase_uid" json:"firebase_uid"`
	UserName         string             `bson:"user_name" json:"user_name"`
	UserNameFurigana string             `bson:"user_name_furigana" json:"user_name_furigana"`
	Email            string             `bson:"email" json:"email"`
	Phone            string             `bson:"phone" json:"phone"`
	Role             string             `bson:"role" json:"role"`
	StoreID          string             `bson:"store_id,omitempty" json:"store_id,omitempty"`
	UserImageURL     string             `bson:"user_image_url,omitempty" json:"user_image_url,omitempty"`
	TermsAgreed      bool               `bson:"terms_agreed" json:"terms_agreed"`
	TermsAgreedAt    time.Time          `bson:"terms_agreed_at" json:"terms_agreed_at"`
	PrivacyAgreed    bool               `bson:"privacy_agreed" json:"privacy_agreed"`
	PrivacyAgreedAt  time.Time          `bson:"privacy_agreed_at" json:"privacy_agreed_at"`
}
