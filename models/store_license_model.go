package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 認証ステータス定義
const (
	StatusNotSubmitted  = "NOT_SUBMITTED"  // 未提出
	StatusPending       = "PENDING"        // 審査中
	StatusPendingReview = "PENDING_REVIEW" // Upload後、審査中
	StatusApproved      = "APPROVED"       // 承認済み
	StatusRejected      = "REJECTED"       // 反則
)

// store_license モデル
type StoreLicense struct {
	ID                 primitive.ObjectID `bson:"_id,omitempty"       json:"id,omitempty"`
	StoreID            string             `bson:"store_id"            json:"store_id"`
	LicenseImageURL    string             `bson:"license_image_url,omitempty"   json:"license_image_url,omitempty"`
	VerificationStatus string             `bson:"verification_status" json:"verification_status"`
	AdminComment       string             `bson:"admin_comment,omitempty"     json:"admin_comment,omitempty"`
	CreatedAt          time.Time          `bson:"created_at"          json:"created_at"`
	UpdatedAt          time.Time          `bson:"updated_at"          json:"updated_at"`
	// LineLoginUrl       string             `bson:"line_login_url,omitempty"     json:"line_login_url,omitempty"`
	// LineAuthToken      string             `bson:"line_auth_token,omitempty" json:"line_auth_token,omitempty"`
	// LineUserID         string             `bson:"line_user_id,omitempty" json:"line_user_id,omitempty"`
}
