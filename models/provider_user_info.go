package models

import "go.mongodb.org/mongo-driver/bson/primitive"

type User struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"_id"`         // MongoDB 기본 ID
	FirebaseUID string             `bson:"firebase_uid" json:"firebase_uid"` // Firebase UID
	Username    string             `bson:"user_name" json:"user_name"`       // 사용자 이름
	Email       string             `bson:"email" json:"email"`               // 이메일 주소
	Phone       string             `bson:"phone" json:"phone"`               // 전화번호
	Role        string             `bson:"role" json:"role"`                 // 권한 (admin, manager 등)
}
