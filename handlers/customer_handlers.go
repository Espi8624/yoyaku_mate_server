package handlers

import (
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"
)

// 基本ハンドラー
func CustomerHomeHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello, This is Yoyaku Mate Server."))
}

// ユーザー情報返却
func UserInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var userInfoData models.UserInfoItem
	userInfoData = data.GetUserInfoData()
	utils.RespondWithJSON(w, userInfoData, http.StatusOK)
}

// よく訪問する店目録返却
func FrequentPlacesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var frequentPlacesData []models.FrequentPlaceItem
	frequentPlacesData = data.GetAllFrequentPlaces()
	utils.RespondWithJSON(w, frequentPlacesData, http.StatusOK)
}

// タイムラインデータ返却
func TimeLineHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var timeLinesData []models.TimeLineItem
	timeLinesData = data.GetAllTimeLines()
	utils.RespondWithJSON(w, timeLinesData, http.StatusOK)
}

// 予約データ返却
func ReservationsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reservationsData []models.ReservationInfoItem
	reservationsData = data.GetAllReservationInfo()
	utils.RespondWithJSON(w, reservationsData, http.StatusOK)
}

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

// レヴューデータ返却
func ReviewsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reviews []models.ReviewItem
	reviews = data.GetAllReviews()
	utils.RespondWithJSON(w, reviews, http.StatusOK)
}
