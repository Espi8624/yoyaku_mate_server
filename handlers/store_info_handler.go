package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"

	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// 店舗情報に対する GET および PUT リクエストを処理
// GET /api/provider_store?store_id=xxx または ?user_id=xxx
// PUT /api/provider_store?store_id=xxx
func StoreHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleGetStore(w, r)
	case http.MethodPut:
		handleUpdateStore(w, r)
	default:
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// 店舗情報の取得(GET)を処理
func handleGetStore(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	userID := r.URL.Query().Get("user_id")

	if storeID != "" {
		// store_id で照会
		store, err := data.GetStoreData(storeID)
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

		store, err := data.GetStoreDataByUserID(objectID)
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
func handleUpdateStore(w http.ResponseWriter, r *http.Request) {
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

	if err := data.UpdateStoreData(storeID, update); err != nil {
		utils.RespondWithError(w, "Failed to update store info", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]bool{"success": true}, http.StatusOK)
}

func (h *UploadHandler) UploadStoreImage(w http.ResponseWriter, r *http.Request) {
	// storeId取得
	vars := mux.Vars(r)
	storeId := vars["storeId"]

	// 'logoImage'ファイルをマルチパートフォームから取得
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB limit
		utils.RespondWithError(w, "Could not parse multipart form", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("storeImage")
	if err != nil {
		utils.RespondWithError(w, "Could not get uploaded file named 'storeImage'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	assetsPublicDomain := os.Getenv("R2_ASSETS_PUBLIC_DOMAIN")

	// MinIOにアップロード
	fileURL, err := h.Minio.UploadFile("saboten-assets", assetsPublicDomain, file, header)
	if err != nil {
		log.Printf("Error uploading logo to Minio: %v", err)
		utils.RespondWithError(w, "Could not upload file", http.StatusInternalServerError)
		return
	}

	// DBの店舗情報をアップデート
	updatedStore, err := data.UpdateStoreImageURL(storeId, fileURL)
	if err != nil {
		log.Printf("Error updating store image URL in DB: %v", err)
		utils.RespondWithError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, updatedStore, http.StatusOK)
}
