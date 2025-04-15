package models

// ユーザー情報データ構造体
type UserInfoItem struct {
	ID          int    `json:"id"`
	UserName    string `json:"user_name"`
	Email       string `json:"email"`
	PhoneNumber string `json:"phone_number"`
}

// よく訪問する店データ構造体
type FrequentPlaceItem struct {
	StoreName string `json:"store_name"`
	TimeStamp string `json:"time_stamp"`
}

// タイムラインデータ構造体
type TimeLineItem struct {
	StoreName string `json:"store_name"`
	TimeStamp string `json:"time_stamp"`
}

// 予約カレンダーデータ構造体
type ReservationItem struct {
	ID        int    `json:"id"`
	Details   string `json:"details"`
	TimeStamp string `json:"time_stamp"`
}

// お知らせデータ構造体
type NotificationItem struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
	Time    string `json:"time"`
	Type    string `json:"type"`
}

// レビューデータ構造体
type ReviewItem struct {
	ID        int     `json:"id"`
	StoreName string  `json:"store_name"`
	Comments  string  `json:"comments"`
	Rating    float64 `json:"rating"`
	TimeStamp string  `json:"time_stamp"`
}
