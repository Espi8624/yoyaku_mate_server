package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"
)

// WaitingList の主要な操作を処理するハンドラ
func WaitingListHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("action") == "average_waiting_time" {
			handleGetAverageWaitingTime(w, r)
			return
		}
		// if r.URL.Query().Get("waiting_id") != "" {
		// 	handleGetUserWaitingList(w, r)
		// 	return
		// }
		handleGetWaitingList(w, r)
	case http.MethodPost:
		if r.URL.Query().Get("action") == "clear" {
			handleClearWaitingList(w, r)
			return
		}
		handleCreateWaitingList(w, r)
	case http.MethodPatch:
		if r.URL.Query().Get("action") == "status" {
			handleUpdateWaitingStatus(w, r)
			return
		}
		fallthrough
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// WaitingList アップデートのためのポーリングリクエストを処理
func HandleWaitingListPolling(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	waitingList, err := data.GetWaitingListData(storeID)
	if err != nil {
		log.Printf("Error fetching waiting list data: %v", err)
		utils.RespondWithError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, waitingList, http.StatusOK)
	// log.Printf("Polling request handled successfully for store_id: %s", storeID)
}

// WaitingList の取得を処理する GET リクエストを処理
func handleGetWaitingList(w http.ResponseWriter, r *http.Request) {
	// クエリパラメータから storeID を取得
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		http.Error(w, "Missing storeId parameter", http.StatusBadRequest)
		return
	}

	// データ取得
	waitingListData, err := data.GetWaitingListData(storeID)
	if err != nil {
		log.Printf("Failed to fetch waiting list: %v", err)
		utils.RespondWithError(w, "Failed to fetch waiting list", http.StatusInternalServerError)
		return
	}

	// JSON 応答
	utils.RespondWithJSON(w, waitingListData, http.StatusOK)
}

// 新しいウェイティングリスト作成処理 (POSTリクエスト処理)
func handleCreateWaitingList(w http.ResponseWriter, r *http.Request) {
	var newWaiting models.WaitingList
	if err := json.NewDecoder(r.Body).Decode(&newWaiting); err != nil {
		log.Printf("Error decoding request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 必須フィールド検証
	if newWaiting.StoreID == "" {
		log.Printf("Missing required field: store_id")
		http.Error(w, "Missing required field: store_id", http.StatusBadRequest)
		return
	}
	if newWaiting.PartySize <= 0 {
		log.Printf("Invalid party_size: %d", newWaiting.PartySize)
		http.Error(w, "Invalid party_size: must be greater than 0", http.StatusBadRequest)
		return
	}

	createdItem, err := data.CreateWaitingListItem(newWaiting)
	if err != nil {
		log.Printf("Failed to create waiting list item: %v", err)
		// data階層で送られたエラーメッセージをそのまま使用
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, createdItem, http.StatusCreated)
}

// WaitingList をクリアするリクエストを処理
func handleClearWaitingList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// クエリパラメータから storeID を取得
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// waiting list をクリアする
	err := data.ClearWaitingList(storeID)
	if err != nil {
		log.Printf("Failed to clear waiting list: %v", err)
		utils.RespondWithError(w, "Failed to clear waiting list", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]string{"message": "Waiting list cleared successfully"}, http.StatusOK)
}

// 特定のユーザーのウェイティングリスト項目を取得する GET リクエストを処理
func handleGetUserWaitingList(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")

	if storeID == "" {
		utils.RespondWithError(w, "Missing required parameters: store_id and waiting_id", http.StatusBadRequest)
		return
	}

	// データ取得
	waitingListItem, err := data.GetUserWaitingListItem(storeID)
	if err != nil {
		log.Printf("Failed to fetch user waiting list item: %v", err)
		utils.RespondWithError(w, "Failed to fetch waiting list item", http.StatusInternalServerError)
		return
	}

	if waitingListItem == nil {
		utils.RespondWithJSON(w, map[string]interface{}{"message": "No waiting list item found"}, http.StatusNotFound)
		return
	}

	// JSON 応答
	utils.RespondWithJSON(w, waitingListItem, http.StatusOK)
}

// 待機目録のステータスをアップデートする PATCH 要請を処理
func handleUpdateWaitingStatus(w http.ResponseWriter, r *http.Request) {
	var updateRequest struct {
		StoreID   string `json:"store_id"`
		WaitingID string `json:"waiting_id"`
		Status    string `json:"status"`
	}

	if err := json.NewDecoder(r.Body).Decode(&updateRequest); err != nil {
		log.Printf("Error decoding request body: %v", err)
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 必須フィールド検証
	if updateRequest.StoreID == "" || updateRequest.WaitingID == "" || updateRequest.Status == "" {
		utils.RespondWithError(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Status 有効性検証
	validStatuses := map[string]bool{
		"waiting":   true,
		"notified":  true,
		"completed": true,
		"cancelled": true,
		"no_show":   true,
	}
	if !validStatuses[updateRequest.Status] {
		utils.RespondWithError(w, "Invalid status value", http.StatusBadRequest)
		return
	}

	// Status アップデート
	err := data.UpdateWaitingItemStatus(updateRequest.StoreID, updateRequest.WaitingID, updateRequest.Status)
	if err != nil {
		log.Printf("Failed to update waiting status: %v", err)
		http.Error(w, "Failed to update waiting status", http.StatusInternalServerError)
		return
	}

	// 成功応答
	utils.RespondWithJSON(w, map[string]string{
		"message": "Status updated successfully",
		"status":  updateRequest.Status,
	}, http.StatusOK)
}

// 平均待機時間を返すハンドラ
func handleGetAverageWaitingTime(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}
	avgSec, err := data.GetAverageWaitingTime(storeID)
	if err != nil {
		utils.RespondWithError(w, "Failed to calculate average waiting time", http.StatusInternalServerError)
		return
	}
	avgText := "--分"
	if avgSec > 0 {
		min := avgSec / 60
		sec := avgSec % 60
		if min > 0 {
			avgText = fmt.Sprintf("%d分%d秒", min, sec)
		} else {
			avgText = fmt.Sprintf("%d秒", sec)
		}
	}
	resp := models.AverageWaitingTimeResponse{
		AverageSeconds: avgSec,
		AverageText:    avgText,
	}
	utils.RespondWithJSON(w, resp, http.StatusOK)
}

// 待機ユーザー用データ確認メソッド
func WaitingListUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	storeID := r.URL.Query().Get("store_id")
	waitingID := r.URL.Query().Get("waiting_id")

	if storeID == "" || waitingID == "" {
		utils.RespondWithError(w, "Missing required parameters: store_id and waiting_id", http.StatusBadRequest)
		return
	}

	waitingListItem, err := data.GetActiveWaitingList(storeID, waitingID)
	if err != nil {
		log.Printf("Failed to fetch user waiting list item: %v", err)
		utils.RespondWithError(w, "Failed to fetch waiting list item", http.StatusInternalServerError)
		return
	}

	if waitingListItem == nil {
		utils.RespondWithJSON(w, map[string]interface{}{"message": "No waiting list item found for the given IDs"}, http.StatusNotFound)
		return
	}

	utils.RespondWithJSON(w, waitingListItem, http.StatusOK)
}
