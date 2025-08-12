package handlers

import (
	"encoding/json"
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"
)

// GET /api/store_settings?store_id=xxx
func StoreSettingsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		storeID := r.URL.Query().Get("store_id")
		if storeID == "" {
			utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
			return
		}
		settings, err := data.GetStoreSettingsData(storeID)
		if err != nil {
			utils.RespondWithError(w, "Store settings not found", http.StatusNotFound)
			return
		}
		utils.RespondWithJSON(w, settings, http.StatusOK)
	case http.MethodPut:
		storeID := r.URL.Query().Get("store_id")
		if storeID == "" {
			utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
			return
		}
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if err := data.UpsertStoreSettings(storeID, reqBody); err != nil {
			utils.RespondWithError(w, "Failed to update store settings", http.StatusInternalServerError)
			return
		}
		utils.RespondWithJSON(w, map[string]bool{"success": true}, http.StatusOK)
	default:
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
