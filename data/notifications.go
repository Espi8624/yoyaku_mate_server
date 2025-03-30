package data

import "yoyaku_mate_server/models"

var notificationsData = []models.NotificationItem{
	{ID: 1, Message: "가게 A에서 새 메뉴가 추가되었습니다.", Time: "2시간 전", Type: "store"},
	{ID: 2, Message: "가게 B 주문이 준비되었습니다.", Time: "4시간 전", Type: "store"},
	{ID: 3, Message: "50% 할인 쿠폰이 발급되었습니다.", Time: "1일 전", Type: "coupon"},
	{ID: 4, Message: "주말 특가 쿠폰이 도착했습니다.", Time: "2일 전", Type: "coupon"},
	{ID: 5, Message: "시스템 점검이 3월 28일 예정입니다.", Time: "3시간 전", Type: "system"},
	{ID: 6, Message: "앱이 업데이트 되었습니다.", Time: "5시간 전", Type: "system"},
}

// 모든 알림 반환
func GetAllNotifications() []models.NotificationItem {
	return notificationsData
}

// 타입별 알림 반환
func GetNotificationsByType(notificationType string) []models.NotificationItem {
	var result []models.NotificationItem
	for _, n := range notificationsData {
		if n.Type == notificationType {
			result = append(result, n)
		}
	}
	return result
}
