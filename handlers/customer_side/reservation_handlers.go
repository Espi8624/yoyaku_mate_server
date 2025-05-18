package handlers

import (
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"
)

// 店情報返却
func ReservationInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// クエリパラメータからuser_idを取得
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		utils.RespondWithError(w, "Missing user_id", http.StatusBadRequest)
		return
	}

	// MongoDBから予約情報を取得
	reservationInfo, err := data.GetReservationsInfoData(userID)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch reservation info", http.StatusInternalServerError)
		return
	}

	// JSON 形式でレスポンスを返す
	utils.RespondWithJSON(w, reservationInfo, http.StatusOK)
}

func ReservationDetailsByIDHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// クエリパラメータからreservation_idを取得
	reservation_id := r.URL.Query().Get("reservation_id")
	if reservation_id == "" {
		utils.RespondWithError(w, "Missing reservation_id", http.StatusBadRequest)
		return
	}

	// MongoDBから予約情報を取得
	reservationInfo, err := data.GetReservationDetailsByID(reservation_id)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch reservation info", http.StatusInternalServerError)
		return
	}

	// JSON 形式でレスポンスを返す
	utils.RespondWithJSON(w, reservationInfo, http.StatusOK)
}
