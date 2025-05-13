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

	// クエリパラメータからstore_idを取得
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		utils.RespondWithError(w, "Missing user_id", http.StatusBadRequest)
		return
	}

	// MongoDBから店情報を取得
	storeInfo, err := data.GetReservationsInfoData(userID)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch store info", http.StatusInternalServerError)
		return
	}

	// JSON 形式でレスポンスを返す
	utils.RespondWithJSON(w, storeInfo, http.StatusOK)
}
