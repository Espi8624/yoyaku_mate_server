package data

import "yoyaku_mate_server/models"

// 타임라인 데이터 목록
var userInfo = models.UserInfoItem{
	ID: 1, UserName: "Kumamoto Tarou", Email: "example@email.com", PhoneNumber: "070-1234-5678",
}

// 모든 알림 반환
func GetUserInfoData() models.UserInfoItem {
	return userInfo
}
