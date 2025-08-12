package models

// 会員加入リクエストデータ構造体
type SignUpRequest struct {
	FirebaseUID    string  `json:"firebase_uid"`
	Name           string  `json:"name"`
	Email          string  `json:"email"`
	PhoneNumber    string  `json:"phone_number"`
	Role           string  `json:"role"`
	StoreID        *string `json:"store_id,omitempty"`
	StoreName      *string `json:"store_name,omitempty"`
	StoreAddress   *string `json:"store_address,omitempty"`
	StoreTelNumber *string `json:"store_tel_number,omitempty"`
	StoreEmail     *string `json:"store_email,omitempty"`
	BizNumber      *string `json:"biz_number,omitempty"`
	Description    *string `json:"description,omitempty"`
}

// メールアドレス重複確認リクエスト構造体
type EmailCheckRequest struct {
	Email string `json:"email"`
}

// 電話番号重複確認リクエスト構造体
type PhoneCheckRequest struct {
	PhoneNumber string `json:"phone_number"`
}
