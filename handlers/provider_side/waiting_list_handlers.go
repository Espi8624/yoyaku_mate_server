package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"
)

// WaitingListHandler는 웨이팅 리스트의 주요 작업을 처리하는 핸들러입니다
// Waiting list の 主要な操作を処理するハンドラ
func WaitingListHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("action") == "average_waiting_time" {
			handleGetAverageWaitingTime(w, r)
			return
		}
		if r.URL.Query().Get("waiting_id") != "" {
			handleGetUserWaitingList(w, r)
			return
		}
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

// HandleWaitingListPolling은 웨이팅 리스트 업데이트를 위한 폴링 요청을 처리합니다
// Waiting list アップデートのためのポーリングリクエストを処理
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

// handleGetWaitingList는 웨이팅 리스트 조회를 위한 GET 요청을 처리합니다
// Waiting list の取得を処理する GET リクエストを処理
func handleGetWaitingList(w http.ResponseWriter, r *http.Request) {
	// 쿼리 파라미터에서 storeID 가져오기
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

// handleCreateWaitingList handles POST requests for creating new waiting list items
// 新しいウェイティングリスト作成処理 (POSTリクエスト処理)
func handleCreateWaitingList(w http.ResponseWriter, r *http.Request) {
	var newWaiting models.WaitingListItem
	if err := json.NewDecoder(r.Body).Decode(&newWaiting); err != nil {
		log.Printf("Error decoding request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 필수 필드 검증
	if newWaiting.StoreID == "" {
		log.Printf("Missing required field: store_id")
		http.Error(w, "Missing required field: store_id", http.StatusBadRequest)
		return
	}
	if newWaiting.CustomerName == "" {
		log.Printf("Missing required field: customer_name")
		http.Error(w, "Missing required field: customer_name", http.StatusBadRequest)
		return
	}
	if newWaiting.PartySize <= 0 {
		log.Printf("Invalid party_size: %d", newWaiting.PartySize)
		http.Error(w, "Invalid party_size: must be greater than 0", http.StatusBadRequest)
		return
	}

	// Set default values
	newWaiting.Status = "waiting"
	// Set registration time to current JST time
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)
	newWaiting.RegistrationTime = now.Format(time.RFC3339)

	// Validate or generate WaitingID
	if newWaiting.WaitingID == "" {
		log.Printf("Warning: WaitingID not provided by client, generating one")
		newWaiting.WaitingID = now.Format("20060102-150405")
	} else if !utils.IsValidWaitingID(newWaiting.WaitingID) {
		log.Printf("Invalid WaitingID format: %s", newWaiting.WaitingID)
		http.Error(w, "Invalid waiting_id format", http.StatusBadRequest)
		return
	}

	// Get the next queue number
	nextQueueNumber, err := data.GetNextQueueNumber(newWaiting.StoreID)
	if err != nil {
		log.Printf("Failed to get next queue number: %v", err)
		http.Error(w, "Failed to create waiting list item", http.StatusInternalServerError)
		return
	}
	newWaiting.QueueNumber = nextQueueNumber

	// Create the waiting list item
	err = data.CreateWaitingListItem(newWaiting)
	if err != nil {
		log.Printf("Failed to create waiting list item: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return the created item
	utils.RespondWithJSON(w, newWaiting, http.StatusCreated)
}

// handleClearWaitingList handles requests to clear the waiting list
// Waiting list をクリアするリクエストを処理
func handleClearWaitingList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get storeID from query parameters
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// Clear the waiting list
	err := data.ClearWaitingList(storeID)
	if err != nil {
		log.Printf("Failed to clear waiting list: %v", err)
		utils.RespondWithError(w, "Failed to clear waiting list", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]string{"message": "Waiting list cleared successfully"}, http.StatusOK)
}

// handleGetUserWaitingList는 특정 사용자의 웨이팅 리스트 항목을 조회하는 GET 요청을 처리합니다
// 特定のユーザーのウェイティングリスト項目を取得する GET リクエストを処理
func handleGetUserWaitingList(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	userID := r.URL.Query().Get("waiting_id")

	if storeID == "" || userID == "" {
		utils.RespondWithError(w, "Missing required parameters: store_id and waiting_id", http.StatusBadRequest)
		return
	}

	// 데이터 조회
	// データ取得
	waitingListItem, err := data.GetUserWaitingListItem(storeID, userID)
	if err != nil {
		log.Printf("Failed to fetch user waiting list item: %v", err)
		utils.RespondWithError(w, "Failed to fetch waiting list item", http.StatusInternalServerError)
		return
	}

	if waitingListItem == nil {
		utils.RespondWithJSON(w, map[string]interface{}{"message": "No waiting list item found"}, http.StatusNotFound)
		return
	}

	// JSON 응답
	// JSON 応答
	utils.RespondWithJSON(w, waitingListItem, http.StatusOK)
}

// handleUpdateWaitingStatus는 웨이팅 항목의 상태를 업데이트하는 PATCH 요청을 처리합니다
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

	// 필수 필드 검증
	if updateRequest.StoreID == "" || updateRequest.WaitingID == "" || updateRequest.Status == "" {
		utils.RespondWithError(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Status 유효성 검증
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

	// 상태 업데이트
	err := data.UpdateWaitingItemStatus(updateRequest.StoreID, updateRequest.WaitingID, updateRequest.Status)
	if err != nil {
		log.Printf("Failed to update waiting status: %v", err)
		http.Error(w, "Failed to update waiting status", http.StatusInternalServerError)
		return
	}

	// 성공 응답
	utils.RespondWithJSON(w, map[string]string{
		"message": "Status updated successfully",
		"status":  updateRequest.Status,
	}, http.StatusOK)
}

// 평균 대기시간 반환 핸들러
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
