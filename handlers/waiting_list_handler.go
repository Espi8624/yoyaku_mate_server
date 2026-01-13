package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
	"yoyaku_mate_server/auth"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/events"
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
		if r.URL.Query().Get("action") == "qr_token" {
			handleGetQRToken(w, r)
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

// HandleWaitingListPolling WaitingList アップデートのためのポーリングリクエストを処理
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

// handleGetWaitingList WaitingList の取得を処理する GET リクエストを処理
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

// handleCreateWaitingList 新しいウェイティングリスト作成処理 (POSTリクエスト処理)
func handleCreateWaitingList(w http.ResponseWriter, r *http.Request) {
	var newWaiting models.WaitingList
	if err := json.NewDecoder(r.Body).Decode(&newWaiting); err != nil {
		log.Printf("Error decoding request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 今日のQRトークンを取得
	vToken := r.URL.Query().Get("v_token")
	if vToken == "" {
		// v_tokenがクエリパラメータに含まれていない場合、JSONボディに含まれているかチェックする
		// まずクエリパラメータをチェックする
		log.Printf("Missing v_token for store %s", newWaiting.StoreID)
		http.Error(w, "QRコードが正しくないか、期限切れです。再度スキャンして下さい。", http.StatusForbidden)
		return
	}

	// Date string in JST (YYYYMMDD)
	jst := time.FixedZone("JST", 9*60*60)
	dateStr := time.Now().In(jst).Format("20060102")

	if !utils.VerifyHMACDateToken(newWaiting.StoreID, dateStr, vToken) {
		log.Printf("Invalid v_token for store %s: %s (Expected for %s)", newWaiting.StoreID, vToken, dateStr)
		http.Error(w, "QRコードが正しくないか、期限切れです。再度スキャンして下さい。", http.StatusForbidden)
		return
	}

	// 必須フィールド検証
	if newWaiting.StoreID == "" {
		log.Printf("Missing required field: store_id")
		http.Error(w, "店舗IDが正しくありません。", http.StatusBadRequest)
		return
	}
	if newWaiting.PartySize <= 0 {
		log.Printf("Invalid party_size: %d", newWaiting.PartySize)
		http.Error(w, "人数が正しくありません。", http.StatusBadRequest)
		return
	}

	// 最大受付可能人数チェック
	settings, err := data.GetStoreSettingsData(newWaiting.StoreID)
	if err == nil {
		maxCount := settings.Settings.WaitingPolicy.MaxWaitingCount
		if maxCount > 0 && newWaiting.PartySize > maxCount {
			log.Printf("Party size %d exceeds max waiting count %d", newWaiting.PartySize, maxCount)
			http.Error(w, fmt.Sprintf("最大受付可能人数(%d人)を超えました", maxCount), http.StatusBadRequest)
			return
		}
	}

	// Authorizationヘッダーがある場合（スタッフ/マネージャー）、権限チェック
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		idToken := authHeader[len("Bearer "):]
		firebaseUID, err := auth.VerifyIDToken(r.Context(), idToken)
		if err != nil {
			utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}

		// ユーザー情報取得
		user, err := data.GetUserByFirebaseUID(firebaseUID)
		if err != nil || user == nil {
			log.Printf("Failed to get user by Firebase UID: %v", err)
			utils.RespondWithError(w, "User not found", http.StatusUnauthorized)
			return
		}

		// 権限チェック（マネージャーまたはAPPROVED状態のスタッフのみ）
		hasPermission, err := data.CheckUserStorePermission(user.ID, newWaiting.StoreID, user.Role, "")
		if err != nil {
			log.Printf("Failed to check user permission: %v", err)
			utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
			return
		}
		if !hasPermission {
			log.Printf("User %s does not have permission for store %s", user.ID.Hex(), newWaiting.StoreID)
			utils.RespondWithError(w, "この店舗の待機リストを管理する権限がありません。スタッフとして承認されていることを確認してください。", http.StatusForbidden)
			return
		}
	}

	// ライセンス取得
	license, err := data.GetStoreLicenseByStoreID(newWaiting.StoreID)
	if err != nil {
		// ライセンス情報が見つからない場合 (店舗が存在しないか、データが不整合な場合)
		log.Printf("Failed to get license info for store %s: %v", newWaiting.StoreID, err)
		http.Error(w, "この店舗の認証情報が見つかりません。", http.StatusForbidden)
		return
	}
	// ライセンス情報がAPPROVEDであることを確認
	if license.VerificationStatus != models.StatusApproved { // "APPROVED"
		log.Printf("Store %s is not approved. Status: %s", newWaiting.StoreID, license.VerificationStatus)
		http.Error(w, "現在、この店舗は待機受付を行っておりません。", http.StatusForbidden)
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

	// DBの一貫性を確保するために少し待機してから通知
	// 直後のFetchでデータが見えない場合があるため
	go func() {
		time.Sleep(100 * time.Millisecond)
		notifyStore(newWaiting.StoreID)
	}()
}

// handleClearWaitingList WaitingList をクリアするリクエストを処理
func handleClearWaitingList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Firebase認証チェック
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}

	idToken := authHeader[len("Bearer "):]
	firebaseUID, err := auth.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	// クエリパラメータから storeID を取得
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// ユーザー情報取得
	user, err := data.GetUserByFirebaseUID(firebaseUID)
	if err != nil || user == nil {
		log.Printf("Failed to get user by Firebase UID: %v", err)
		utils.RespondWithError(w, "User not found", http.StatusUnauthorized)
		return
	}

	// 権限チェック（マネージャーまたはAPPROVED状態のスタッフのみ）
	hasPermission, err := data.CheckUserStorePermission(user.ID, storeID, user.Role, "")
	if err != nil {
		log.Printf("Failed to check user permission: %v", err)
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasPermission {
		log.Printf("User %s does not have permission for store %s", user.ID.Hex(), storeID)
		utils.RespondWithError(w, "この店舗の待機リストを管理する権限がありません。", http.StatusForbidden)
		return
	}

	// waiting list をクリアする
	err = data.ClearWaitingList(storeID)
	if err != nil {
		log.Printf("Failed to clear waiting list: %v", err)
		utils.RespondWithError(w, "Failed to clear waiting list", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]string{"message": "Waiting list cleared successfully"}, http.StatusOK)
	notifyStore(storeID)
}

// 特定のユーザーのウェイティングリスト項目を取得する GET リクエストを処理
// func handleGetUserWaitingList(w http.ResponseWriter, r *http.Request) {
// 	storeID := r.URL.Query().Get("store_id")

// 	if storeID == "" {
// 		utils.RespondWithError(w, "Missing required parameters: store_id and waiting_id", http.StatusBadRequest)
// 		return
// 	}

// 	// データ取得
// 	waitingListItem, err := data.GetUserWaitingListItem(storeID)
// 	if err != nil {
// 		log.Printf("Failed to fetch user waiting list item: %v", err)
// 		utils.RespondWithError(w, "Failed to fetch waiting list item", http.StatusInternalServerError)
// 		return
// 	}

// 	if waitingListItem == nil {
// 		utils.RespondWithJSON(w, map[string]interface{}{"message": "No waiting list item found"}, http.StatusNotFound)
// 		return
// 	}

// 	// JSON 応答
// 	utils.RespondWithJSON(w, waitingListItem, http.StatusOK)
// }

// handleUpdateWaitingStatus 待機目録のステータスをアップデートする PATCH 要請を処理
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

	// Firebase認証チェック
	authHeader := r.Header.Get("Authorization")

	// Authorizationヘッダーがない場合
	if authHeader == "" {
		// キャンセル以外の操作は許可しない
		if updateRequest.Status != "cancelled" {
			utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
			return
		}
		// status == "cancelled" の場合は認証スキップ (ゲストによるキャンセル)
	} else {
		// Authorizationヘッダーがある場合 (スタッフ/マネージャーによる操作)
		idToken := authHeader[len("Bearer "):]
		firebaseUID, err := auth.VerifyIDToken(r.Context(), idToken)
		if err != nil {
			utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}

		// ユーザー情報取得
		user, err := data.GetUserByFirebaseUID(firebaseUID)
		if err != nil || user == nil {
			log.Printf("Failed to get user by Firebase UID: %v", err)
			utils.RespondWithError(w, "User not found", http.StatusUnauthorized)
			return
		}

		// 権限チェック（マネージャーまたはAPPROVED状態のスタッフのみ）
		hasPermission, err := data.CheckUserStorePermission(user.ID, updateRequest.StoreID, user.Role, "")
		if err != nil {
			log.Printf("Failed to check user permission: %v", err)
			utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
			return
		}
		if !hasPermission {
			log.Printf("User %s does not have permission for store %s", user.ID.Hex(), updateRequest.StoreID)
			utils.RespondWithError(w, "この店舗の待機リストを管理する権限がありません。", http.StatusForbidden)
			return
		}
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
	notifyStore(updateRequest.StoreID)
}

// handleGetAverageWaitingTime 平均待機時間を返すハンドラ
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

// WaitingListUserHandler 待機ユーザー用データ確認メソッド
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

// handleGetQRToken QRトークンを取得する
func handleGetQRToken(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// Firebase認証チェック
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}

	idToken := authHeader[len("Bearer "):]
	firebaseUID, err := auth.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	user, err := data.GetUserByFirebaseUID(firebaseUID)
	if err != nil || user == nil {
		utils.RespondWithError(w, "User not found", http.StatusUnauthorized)
		return
	}

	hasPermission, err := data.CheckUserStorePermission(user.ID, storeID, user.Role, "")
	if err != nil || !hasPermission {
		utils.RespondWithError(w, "Permission denied", http.StatusForbidden)
		return
	}

	// JST基準の今日の日付
	jst := time.FixedZone("JST", 9*60*60)
	dateStr := time.Now().In(jst).Format("20060102")

	token := utils.GenerateHMACDateToken(storeID, dateStr)

	utils.RespondWithJSON(w, map[string]string{
		"v_token": token,
		"date":    dateStr,
	}, http.StatusOK)
}

// HandleWaitingListStream はリアルタイムの待機リスト更新のためのServer-Sent Eventsを処理します
func HandleWaitingListStream(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		http.Error(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// SSE用のヘッダーを設定
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// このクライアント用のチャンネルを作成
	clientChan := make(chan string, 10)

	// クライアントを登録
	broker := events.GetBroker()
	broker.AddClient(storeID, clientChan)

	// 接続が閉じられたときにクライアントを削除
	defer broker.RemoveClient(storeID, clientChan)

	// 接続終了を監視
	notify := r.Context().Done()

	// 初期データ送信 (オプションだがUX向上のため)
	go func() {
		waitingList, err := data.GetWaitingListData(storeID)
		if err == nil {
			jsonData, _ := json.Marshal(waitingList)
			broker.Broadcast(storeID, string(jsonData))
		}
	}()

	for {
		select {
		case <-notify:
			return
		case msg := <-clientChan:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			w.(http.Flusher).Flush()
		}
	}
}

// notifyStore は最新データを取得し、すべてのサブスクライバーにブロードキャストします
func notifyStore(storeID string) {
	// 最新データを取得
	waitingList, err := data.GetWaitingListData(storeID)
	if err != nil {
		log.Printf("Error fetching waiting list for broadcast: %v", err)
		return
	}

	// JSONにマーシャル
	jsonData, err := json.Marshal(waitingList)
	if err != nil {
		log.Printf("Error marshaling waiting list for broadcast: %v", err)
		return
	}

	// ブロードキャスト
	events.GetBroker().Broadcast(storeID, string(jsonData))
}
