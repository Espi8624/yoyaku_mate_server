package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"
)

// WaitingListHandler handles the main waiting list operations
func WaitingListHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleGetWaitingList(w, r)
	case http.MethodPost:
		handleCreateWaitingList(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetWaitingList handles GET requests for waiting list
func handleGetWaitingList(w http.ResponseWriter, r *http.Request) {
	// storeID をクエリパラメータから取得
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		http.Error(w, "Missing storeId parameter", http.StatusBadRequest)
		return
	}

	// データ取得
	waitingListData, err := data.GetWaitingListData(storeID)
	if err != nil {
		log.Printf("Failed to fetch waiting list: %v", err)
		http.Error(w, "Failed to fetch waiting list", http.StatusInternalServerError)
		return
	}

	// JSON 応答
	utils.RespondWithJSON(w, waitingListData, http.StatusOK)
}

// handleCreateWaitingList handles POST requests for creating new waiting list items
func handleCreateWaitingList(w http.ResponseWriter, r *http.Request) {
	var newWaiting models.WaitingListItem
	if err := json.NewDecoder(r.Body).Decode(&newWaiting); err != nil {
		log.Printf("Error decoding request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Set default values
	newWaiting.Status = "waiting"

	// Set registration time to current JST time
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	now := time.Now().In(jst)
	newWaiting.RegistrationTime = now

	// Generate WaitingID using timestamp
	newWaiting.WaitingID = now.Format("20060102-150405")

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
		http.Error(w, "Failed to create waiting list item", http.StatusInternalServerError)
		return
	}

	// Return the created item
	utils.RespondWithJSON(w, newWaiting, http.StatusCreated)
}

// HandleWaitingListPolling handles polling requests for waiting list updates
func HandleWaitingListPolling(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		http.Error(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// Get waiting list data
	waitingList, err := data.GetWaitingListData(storeID)
	if err != nil {
		log.Printf("Error fetching waiting list data: %v", err)
		utils.RespondWithError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, waitingList, http.StatusOK)
	log.Printf("Polling request handled successfully for store_id: %s", storeID)
}
