package handlers

import (
	"encoding/json"
	"net/http"
	"yoyaku_mate_server/models"
)

// LicenseCallStoreRepository 特定の店舗の営業許可証(ライセンス)情報を単一取得するためのインターフェース
type LicenseCallStoreRepository interface {
	GetLicense(storeID string) (*models.StoreLicense, error)
}

type StoreLicenseCallHandler struct {
	repo LicenseCallStoreRepository
}

func NewStoreLicenseCallHandler(repo LicenseCallStoreRepository) *StoreLicenseCallHandler {
	return &StoreLicenseCallHandler{repo: repo}
}

// 店舗認証情報返却
func (h *StoreLicenseCallHandler) GetStoreLicenseHandler(w http.ResponseWriter, r *http.Request) {
	// URL Queryパラメーターから値取得
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		http.Error(w, "store_id is required", http.StatusBadRequest)
		return
	}

	// DB照会呼出
	license, err := h.repo.GetLicense(storeID)
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
