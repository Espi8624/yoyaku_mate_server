package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/metrics"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

	"github.com/gorilla/mux"
)

// body parsing
type updateStatusPayload struct {
	Status  string `json:"status"`
	Comment string `json:"comment"`
}

func GetStoresHandler(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")

	stores, err := data.GetStoresByStatus(status)
	if err != nil {
		utils.RespondWithError(w, "Failed to retrieve stores", http.StatusInternalServerError)
		return
	}

	if stores == nil {
		stores = []models.StoreWithLicense{}
	}

	utils.RespondWithJSON(w, stores, http.StatusOK)
}

func UpdateStoreStatusHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeId := vars["storeId"]

	var payload updateStatusPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	action := fmt.Sprintf("STORE_%s", payload.Status)

	err := data.UpdateLicenseStatus(storeId, payload.Status, payload.Comment)
	if err != nil {
		metrics.SetAuditContext(r, action, fmt.Sprintf("Store ID: %s", storeId), payload.Comment)
		utils.RespondWithError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// - 監査イベントコンテキストの注入
	metrics.SetAuditContext(r, action, fmt.Sprintf("Store ID: %s", storeId), payload.Comment)

	utils.RespondWithJSON(w, map[string]string{"message": "Store status updated successfully"}, http.StatusOK)
}

// 仮URLを返却
func (h *UploadHandler) GetLicenseImageURLHandler(w http.ResponseWriter, r *http.Request) {
	// ストレージクライアントの初期化確認
	if h.Minio == nil {
		utils.RespondWithError(w, "Storage service is not configured", http.StatusServiceUnavailable)
		return
	}

	imageKey := r.URL.Query().Get("key")
	if imageKey == "" {
		utils.RespondWithError(w, "Image key is required", http.StatusBadRequest)
		return
	}

	signedURL, err := h.Minio.GetPresignedURL(h.BizBucketName, imageKey)
	if err != nil {
		log.Printf("Error generating presigned URL: %v", err)
		utils.RespondWithError(w, "Could not generate image URL", http.StatusInternalServerError)
		return
	}

	response := map[string]string{"url": signedURL}
	utils.RespondWithJSON(w, response, http.StatusOK)
}
