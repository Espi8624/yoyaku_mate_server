package models

// 会員加入リクエストデータ構造体
type SignUpRequest struct {
	FirebaseUID             string                   `json:"firebase_uid"`
	Name                    string                   `json:"name"`
	NameFurigana            string                   `json:"name_furigana"`
	Email                   string                   `json:"email"`
	PhoneNumber             string                   `json:"phone_number"`
	Role                    string                   `json:"role"`
	StoreID                 string                   `bson:"store_id,omitempty" json:"store_id,omitempty"` // *stringからstringへ変更、bsonタグを追加
	StoreName               *string                  `json:"store_name,omitempty"`
	StoreZipCode            *string                  `json:"store_zip_code,omitempty"`
	StorePrefecture         *string                  `json:"store_prefecture,omitempty"`
	StoreCity               *string                  `json:"store_city,omitempty"`
	StoreAddress            *string                  `json:"store_address,omitempty"`
	StoreBuilding           *string                  `json:"store_building,omitempty"`
	StoreTelNumber          *string                  `json:"store_tel_number,omitempty"`
	StoreEmail              *string                  `json:"store_email,omitempty"`
	BizNumber               *string                  `json:"biz_number,omitempty"`
	Description             *string                  `json:"description,omitempty"`
	TermsAgreed             bool                     `json:"terms_agreed"`
	TermsAgreedAt           string                   `json:"terms_agreed_at"`
	PrivacyAgreed           bool                     `json:"privacy_agreed"`
	PrivacyAgreedAt         string                   `json:"privacy_agreed_at"`
	EstimatedWaitTime       *int                     `json:"estimated_wait_time,omitempty"`
	MaxWaitingCount         *int                     `json:"max_waiting_count,omitempty"`
	EnableMenuSelection     *bool                    `json:"enable_menu_selection,omitempty"`
	RequireOneMenuPerPerson *bool                    `json:"require_one_menu_per_person,omitempty"` // New field
	OperatingHours          map[string]StoreDayHours `json:"operating_hours,omitempty"`
	Is24Hours               *bool                    `json:"is_24_hours,omitempty"`
	ResetTime               *string                  `json:"reset_time,omitempty"`
}

// メールアドレス重複確認リクエスト構造体
type EmailCheckRequest struct {
	Email string `json:"email"`
}

// 電話番号重複確認リクエスト構造体
type PhoneCheckRequest struct {
	PhoneNumber string `json:"phone_number"`
}
