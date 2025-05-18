package handlers

import (
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"
)

// お知らせデータ返却
func NotificationsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Query　パラメータでタイプフィルタリング (例: ?type=store)
	notificationType := r.URL.Query().Get("type")
	var notifications []models.NotificationItem

	if notificationType == "" || notificationType == "all" {
		notifications = data.GetAllNotifications()
	} else {
		notifications = data.GetNotificationsByType(notificationType)
	}

	utils.RespondWithJSON(w, notifications, http.StatusOK)
}
