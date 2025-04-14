package handlers

import (
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"
)

// 유저 정보 반환
func StoreInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var userInfoData models.StoreInfoItem
	userInfoData = data.GetStoreInfoData()
	utils.RespondWithJSON(w, userInfoData, http.StatusOK)
}
