package models

// 유저 데이터 구조체
type UserInfoItem struct {
	ID          int    `json:"id"`
	UserName    string `json:"user_name"`
	Email       string `json:"email"`
	PhoneNumber string `json:"phone_number"`
}

// 자주 방문하는 장소 데이터 구조체
type FrequentPlaceItem struct {
	StoreName string `json:"store_name"`
	TimeStamp string `json:"time_stamp"`
}

// 타임라인 데이터 구조체
type TimeLineItem struct {
	StoreName string `json:"store_name"`
	TimeStamp string `json:"time_stamp"`
}

// 예약 캘린더 데이터 구조체
type ReservationItem struct {
	ID        int    `json:"id"`
	Details   string `json:"details"`
	TimeStamp string `json:"time_stamp"`
}

// 알림 데이터 구조체
type NotificationItem struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
	Time    string `json:"time"`
	Type    string `json:"type"`
}

// 리뷰 데이터 구조체
type ReviewItem struct {
	ID        int     `json:"id"`
	StoreName string  `json:"store_name"`
	Comments  string  `json:"comments"`
	Rating    float64 `json:"rating"`
	TimeStamp string  `json:"time_stamp"`
}
