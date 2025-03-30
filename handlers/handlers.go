package handlers

import (
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"
)

// 기본 핸들러
func HomeHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello, This is Yoyaku Mate Server."))
}

// 유저 정보 반환
func UserInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var userInfoData models.UserInfoItem
	userInfoData = data.GetUserInfoData()
	utils.RespondWithJSON(w, userInfoData, http.StatusOK)
}

// 자주 방문하는 장소 목록 반환
func FrequentPlacesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var frequentPlacesData []models.FrequentPlaceItem
	frequentPlacesData = data.GetAllFrequentPlaces()
	utils.RespondWithJSON(w, frequentPlacesData, http.StatusOK)
}

// 타임라인 데이터 반환
func TimeLineHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var timeLinesData []models.TimeLineItem
	timeLinesData = data.GetAllTimeLines()
	utils.RespondWithJSON(w, timeLinesData, http.StatusOK)
}

// 예약 데이터 반환
func ReservationsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reservationsData []models.ReservationItem
	reservationsData = data.GetAllReservations()
	utils.RespondWithJSON(w, reservationsData, http.StatusOK)
}

func NotificationsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 쿼리 파라미터로 타입 필터링 (예: ?type=store)
	notificationType := r.URL.Query().Get("type")
	var notifications []models.NotificationItem

	if notificationType == "" || notificationType == "all" {
		notifications = data.GetAllNotifications()
	} else {
		notifications = data.GetNotificationsByType(notificationType)
	}

	utils.RespondWithJSON(w, notifications, http.StatusOK)
}

// 리뷰 데이터 반환
func ReviewsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reviews []models.ReviewItem
	reviews = data.GetAllReviews()
	utils.RespondWithJSON(w, reviews, http.StatusOK)
}
