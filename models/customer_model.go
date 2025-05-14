package models

// // ユーザー情報データ構造体
// type UserInfoItem struct {
// 	ID          int    `json:"id"`
// 	UserName    string `json:"user_name"`
// 	Email       string `json:"email"`
// 	PhoneNumber string `json:"phone_number"`
// }

// お知らせデータ構造体
type NotificationItem struct {
	ID      int    `json:"id"`
	Message string `json:"message"`
	Time    string `json:"time"`
	Type    string `json:"type"`
}
