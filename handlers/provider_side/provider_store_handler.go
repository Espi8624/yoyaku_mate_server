package handlers

import (
	"encoding/json"
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// 店舗情報に対する GET および PUT リクエスト を処理
// GET /api/provider_store?store_id=xxx 또는 ?user_id=xxx
// PUT /api/provider_store?store_id=xxx
func ProviderStoreHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleGetProviderStore(w, r)
	case http.MethodPut:
		handleUpdateProviderStore(w, r)
	default:
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// 店舗情報の取得(GET)を処理
func handleGetProviderStore(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	userID := r.URL.Query().Get("user_id")

	if storeID != "" {
		// store_id で照会
		store, err := data.GetProviderStoreData(storeID)
		if err != nil {
			// data 関数が mongo.ErrNoDocuments を返却したら 404, その他は 500
			if err == mongo.ErrNoDocuments {
				utils.RespondWithError(w, "Store not found by store_id", http.StatusNotFound)
			} else {
				utils.RespondWithError(w, "Internal server error", http.StatusInternalServerError)
			}
			return
		}
		// 成功時、Status 200 ＆ データ を 'data' キーでラップして返却
		utils.RespondWithJSON(w, store, http.StatusOK)
		return
	}

	if userID != "" {
		// user_id で照会
		objectID, err := primitive.ObjectIDFromHex(userID)
		if err != nil {
			utils.RespondWithError(w, "Invalid user_id format", http.StatusBadRequest)
			return
		}

		store, err := data.GetProviderStoreDataByUserID(objectID)
		if err != nil {
			// data 関数が mongo.ErrNoDocuments を返却したら 404, その他は 500
			if err == mongo.ErrNoDocuments {
				utils.RespondWithError(w, "Store not found for the given user_id", http.StatusNotFound)
			} else {
				utils.RespondWithError(w, "Internal server error", http.StatusInternalServerError)
			}
			return
		}

		// 成功時、Status 200 ＆ データ を 'data' キーでラップして返却
		utils.RespondWithJSON(w, store, http.StatusOK)
		return
	}

	utils.RespondWithError(w, "Missing required query parameter: store_id or user_id", http.StatusBadRequest)
}

// 店舗情報修正(PUT) ロジックを処理
// PUT /api/provider_store?store_id=xxx
func handleUpdateProviderStore(w http.ResponseWriter, r *http.Request) {
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
}
