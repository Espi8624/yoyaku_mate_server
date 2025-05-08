package handlers

import (
	"net/http"
	"strconv"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"
)

// 基本ハンドラー
func ProviderHomeHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello, This is Yoyaku Mate Server."))
}

// 店情報返却
func StoreInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// クエリパラメータからstore_idを取得
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id", http.StatusBadRequest)
		return
	}

	// store_idをint32に変換
	storeIDInt64, err := strconv.ParseInt(storeID, 10, 32)
	if err != nil {
		utils.RespondWithError(w, "Invalid store_id", http.StatusBadRequest)
		return
	}
	storeIDInt := int32(storeIDInt64) // int32に変換

	// MongoDBから店情報を取得
	storeInfo, err := data.GetStoreInfoData(storeIDInt)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch store info", http.StatusInternalServerError)
		return
	}

	// JSON 形式でレスポンスを返す
	utils.RespondWithJSON(w, storeInfo, http.StatusOK)
}

// 店メニュー情報返却
func StoreMenuHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// クエリパラメータからstore_idを取得
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id", http.StatusBadRequest)
		return
	}

	// store_idをint32に変換
	storeIDInt64, err := strconv.ParseInt(storeID, 10, 32)
	if err != nil {
		utils.RespondWithError(w, "Invalid store_id", http.StatusBadRequest)
		return
	}
	storeIDInt := int32(storeIDInt64) // int32に変換

	// MongoDBから店メニュー情報を取得
	storeMenuData, err := data.GetStoreMenuData(storeIDInt)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch store menu", http.StatusInternalServerError)
		return
	}

	// JSON 形式でレスポンスを返す
	utils.RespondWithJSON(w, storeMenuData, http.StatusOK)
}

// 店メニュー情報返却
func StoreCommentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// クエリパラメータからstore_idを取得
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id", http.StatusBadRequest)
		return
	}

	// store_idをint32に変換
	storeIDInt64, err := strconv.ParseInt(storeID, 10, 32)
	if err != nil {
		utils.RespondWithError(w, "Invalid store_id", http.StatusBadRequest)
		return
	}
	storeIDInt := int32(storeIDInt64) // int32に変換

	// MongoDBから店メニュー情報を取得
	storeCommentData, err := data.GetStoreCommentData(storeIDInt)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch store menu", http.StatusInternalServerError)
		return
	}

	// JSON 形式でレスポンスを返す
	utils.RespondWithJSON(w, storeCommentData, http.StatusOK)
}

// 店予約情報返却
func StoreReservationsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var reservationsData []models.StoreReservationItem
	reservationsData = data.GetAllStoreReservationsData()
	utils.RespondWithJSON(w, reservationsData, http.StatusOK)
}
