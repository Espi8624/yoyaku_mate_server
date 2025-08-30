package handlers

import (
	"encoding/json"
	"net/http"
	"yoyaku_mate_server/data"
)

// GetStoreLicenseHandler는 store_id를 기반으로 가게의 인증 정보를 조회하여 반환합니다.
// Flutter 앱에서 이 API를 호출(call)할 것입니다.
func GetStoreLicenseHandler(w http.ResponseWriter, r *http.Request) {
	// 1. URL 쿼리 파라미터에서 'store_id'를 가져옵니다.
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		// utils.RespondWithError와 같은 헬퍼 함수가 있다면 사용해도 좋습니다.
		http.Error(w, "store_id is required", http.StatusBadRequest)
		return
	}

	// 2. data 패키지의 DB 조회 함수를 호출(call)합니다.
	license, err := data.GetStoreLicenseByStoreID(storeID)
	if err != nil {
		if err.Error() == "mongo: no documents in result" {
			http.Error(w, "Store license not found", http.StatusNotFound)
		} else {
			http.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	// 3. 성공 응답을 JSON 형식으로 반환합니다.
	//    Flutter ViewModel이 {"data": {...}} 형식을 기대하고 있으므로 맵으로 감싸줍니다.
	response := map[string]interface{}{
		"data": license,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
