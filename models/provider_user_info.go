package models

import "go.mongodb.org/mongo-driver/bson/primitive"

type User struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"` // MongoDB 기본 ID
	FirebaseUID string             `bson:"firebase_uid"`  // Firebase UID
	Username    string             `bson:"username"`      // 사용자 이름
	Email       string             `bson:"email"`         // 이메일 주소
	Phone       string             `bson:"phone"`         // 전화번호
	Role        string             `bson:"role"`          // 권한 (admin, manager 등)
}
