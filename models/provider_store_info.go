package models

import "go.mongodb.org/mongo-driver/bson/primitive"

type Store struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"_id"`     // MongoDB 기본 ID
	StoreName string             `bson:"store_name" json:"store_name"` // 점포 이름
	Address   string             `bson:"address" json:"address"`       // 주소
	Phone     string             `bson:"phone" json:"phone"`           // 전화번호
	BizNumber string             `bson:"biz_number" json:"biz_number"` // 사업자등록번호
	UserID    primitive.ObjectID `bson:"user_id" json:"user_id"`       // 점포 관리자 (User 참조)
	StoreID   string             `bson:"store_id" json:"store_id"`     // 커스텀 점포 ID (선택사항)
}
