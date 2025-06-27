package handlers

import (
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"
)

// GET /api/provider_store?store_id=xxx
func ProviderStoreHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		storeID := r.URL.Query().Get("store_id")
		if storeID == "" {
			utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
			return
		}
		store, err := data.GetProviderStoreData(storeID)
		if err != nil {
			utils.RespondWithError(w, "Provider store not found", http.StatusNotFound)
			return
		}
		utils.RespondWithJSON(w, store, http.StatusOK)
	default:
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
