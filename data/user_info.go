package data

import "yoyaku_mate_server/models"

// ユーザーデータ
var userInfo = models.UserInfoItem{
	ID: 1, UserName: "Kumamoto Tarou", Email: "example@email.com", PhoneNumber: "070-1234-5678",
}

// ユーザデータ取得
func GetUserInfoData() models.UserInfoItem {
	return userInfo
}
