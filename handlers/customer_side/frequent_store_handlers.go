package handlers

import (
	"net/http"
	"yoyaku_mate_server/services"
	"yoyaku_mate_server/utils"
)

// よく訪問する店目録返却
func FrequentStoreHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 쿼리 파라미터에서 user_id 가져오기
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		utils.RespondWithError(w, "Missing user_id", http.StatusBadRequest)
		return
	}

	// 서비스 레이어에서 자주 방문한 장소 데이터 가져오기
	frequentPlaces, err := services.GetFrequentStores(userID)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch frequent stores", http.StatusInternalServerError)
		return
	}

	// JSON 형식으로 응답 반환
	utils.RespondWithJSON(w, frequentPlaces, http.StatusOK)
}
