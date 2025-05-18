package handlers

import (
	"net/http"
	"yoyaku_mate_server/services"
	"yoyaku_mate_server/utils"
)

// タイムラインデータ返却
func UserTimelineHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		utils.RespondWithError(w, "Missing user_id", http.StatusBadRequest)
		return
	}

	timelineData, err := services.GetUserTimeline(userID)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch timeline data", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, timelineData, http.StatusOK)
}
