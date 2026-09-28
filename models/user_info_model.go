package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ユーザーステータス定義
//   - Statusが空文字("")の既存ドキュメントもACTIVE扱いにすること
//     (フィールド追加前に作られたドキュメントとの後方互換性のため)
const (
	UserStatusActive    = "ACTIVE"
	UserStatusWithdrawn = "WITHDRAWN" // 退会済み。連絡先は保持したまま、ログインのみ不可にする
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
	// Birthdateは"YYYY-MM-DD"形式の文字列で保持する。プロフィール編集は
	// 汎用の$setエンドポイント(UpdateUserData)を経由するため、time.Timeにすると
	// クライアントから送られた生JSON文字列がそのまま$setされた際にBSONデコードで
	// 失敗する恐れがある
	Birthdate       string    `bson:"birthdate,omitempty" json:"birthdate,omitempty"`
	ZipCode         string    `bson:"zip_code,omitempty" json:"zip_code,omitempty"`
	Prefecture      string    `bson:"prefecture,omitempty" json:"prefecture,omitempty"`
	City            string    `bson:"city,omitempty" json:"city,omitempty"`
	Address         string    `bson:"address,omitempty" json:"address,omitempty"`
	Building        string    `bson:"building,omitempty" json:"building,omitempty"`
	StoreID         string    `bson:"store_id,omitempty" json:"store_id,omitempty"`
	UserImageURL    string    `bson:"user_image_url,omitempty" json:"user_image_url,omitempty"`
	TermsAgreed     bool      `bson:"terms_agreed" json:"terms_agreed"`
	TermsAgreedAt   time.Time `bson:"terms_agreed_at" json:"terms_agreed_at"`
	PrivacyAgreed   bool      `bson:"privacy_agreed" json:"privacy_agreed"`
	PrivacyAgreedAt time.Time `bson:"privacy_agreed_at" json:"privacy_agreed_at"`
	// Status/WithdrawnAtは会員退会(ソフトデリート)用。連絡先(電話番号等)は
	// 削除せず保持し、ログインのみ不可にする(handlers.RequireAuthMiddlewareで拒否)
	Status      string    `bson:"status,omitempty" json:"status,omitempty"`
	WithdrawnAt time.Time `bson:"withdrawn_at,omitempty" json:"withdrawn_at,omitempty"`
}

// IsWithdrawn 退会済みかどうかを返す(Status未設定の既存ドキュメントはACTIVE扱い)
func (u *User) IsWithdrawn() bool {
	return u != nil && u.Status == UserStatusWithdrawn
}
