package handlers

import (
	"encoding/json"
	"net/http"
	"yoyaku_mate_server/data"
)

// 店舗認証情報返却
func GetStoreLicenseHandler(w http.ResponseWriter, r *http.Request) {
	// URL Queryパラメーターから値取得
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		http.Error(w, "store_id is required", http.StatusBadRequest)
		return
	}

	// DB照会呼出
	license, err := data.GetStoreLicenseByStoreID(storeID)
	if err != nil {
		if err.Error() == "mongo: no documents in result" {
			http.Error(w, "Store license not found", http.StatusNotFound)
		} else {
			http.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	// 成功応答をJSONに変換
	// {"data": {...}}
	response := map[string]interface{}{
		"data": license,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
