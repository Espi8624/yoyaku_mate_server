package handlers

import (
	"encoding/json"
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// GET /api/provider_store?store_id=xxx
func ProviderStoreHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		storeID := r.URL.Query().Get("store_id")
		// GET /api/provider_store?user_id=xxx
		userID := r.URL.Query().Get("user_id")

		if storeID != "" {
			// store_id로 조회
			store, err := data.GetProviderStoreData(storeID)
			if err != nil {
				utils.RespondWithError(w, "Provider store not found", http.StatusNotFound)
				return
			}
			utils.RespondWithJSON(w, store, http.StatusOK)
			return
		} else if userID != "" {
			// user_id로 조회
			objectID, err := primitive.ObjectIDFromHex(userID)
			if err != nil {
				utils.RespondWithError(w, "Invalid user_id format", http.StatusBadRequest)
				return
			}
			store, err := data.GetProviderStoreDataByUserID(objectID)
			if err != nil {
				utils.RespondWithError(w, "Provider store not found", http.StatusNotFound)
				return
			}
			utils.RespondWithJSON(w, store, http.StatusOK)
			return
		} else {
			utils.RespondWithError(w, "Missing store_id or user_id parameter", http.StatusBadRequest)
			return
		}
	case http.MethodPut:
		storeID := r.URL.Query().Get("store_id")
		if storeID == "" {
			utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
			return
		}
		var update map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if err := data.UpdateProviderStoreData(storeID, update); err != nil {
			utils.RespondWithError(w, "Failed to update provider store info", http.StatusInternalServerError)
			return
		}
		utils.RespondWithJSON(w, map[string]bool{"success": true}, http.StatusOK)
	default:
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
