package models

// ユーザー情報データ構造体
type UserInfoItem struct {
	ID          int    `json:"id"`
	UserName    string `json:"user_name"`
	Email       string `json:"email"`
	PhoneNumber string `json:"phone_number"`
}

// 予約データ構造体
type ReservationInfoItem struct {
	ReservationID int    `json:"reservation_id"`
	UserName      string `json:"user_name"`
	StoreID       int    `json:"store_id"`
	StoreName     string `json:"store_name"`
	Details       string `json:"details"`
	ReservedDate  string `json:"reserved_date"`
	ReservedTime  string `json:"reserved_time"`
	TimeStamp     string `json:"time_stamp"`
}

// よく訪問する店データ構造体
type FrequentPlaceItem struct {
	StoreID     int    `json:"store_id"`
	StoreName   string `json:"store_name"`
	LastVisited string `json:"last_visited"`
	VisitCount  int    `json:"visit_count"`
}

// タイムラインデータ構造体
type TimeLineItem struct {
	ReservationID int    `json:"reservation_id"`
	StoreName     string `json:"store_name"`
	ReservedDate  string `json:"reserved_date"`
	ReservedTime  string `json:"reserved_time"`
}

// お知らせデータ構造体
type NotificationItem struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
	Time    string `json:"time"`
	Type    string `json:"type"`
}
