package handlers

import (
	"encoding/json"
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

	"github.com/gorilla/mux"
)

// for request body parsing
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

	err := data.UpdateLicenseStatus(storeId, payload.Status, payload.Comment)
	if err != nil {
		utils.RespondWithError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]string{"message": "Store status updated successfully"}, http.StatusOK)
}
