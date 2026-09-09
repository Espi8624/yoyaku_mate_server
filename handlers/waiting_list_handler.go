package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
	"yoyaku_mate_server/events"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ==========================================================
// インターフェース定義 (依存性の抽象化)
// ==========================================================

// WaitingListRepository 待機リストのCRUD操作を抽象化するインターフェース
type WaitingListRepository interface {
	GetWaitingList(storeID string) ([]models.WaitingList, error)
	CreateItem(item models.WaitingList) (*models.WaitingList, error)
	UpdateItemStatus(storeID, waitingID, status string) error
	ClearList(storeID string) error
	GetAverageWaitingTime(storeID string) (int, error)
	GetBusinessDayCutoff(storeID string, now time.Time) time.Time
}

// StoreRepository 店舗設定とライセンス情報の取得を抽象化するインターフェース
type StoreRepository interface {
	GetSettings(storeID string) (*models.StoreSetting, error)
	GetLicense(storeID string) (*models.StoreLicense, error)
}

// UserRepository ユーザー情報の取得と権限チェックを抽象化するインターフェース
type UserRepository interface {
	GetByFirebaseUID(uid string) (*models.User, error)
	GetUserData(userID primitive.ObjectID) (*models.User, error)
	CheckStorePermission(userID primitive.ObjectID, storeID, role, permission string) (bool, error)
}

// AuthService Firebase認証を抽象化するインターフェース
type AuthService interface {
	VerifyIDToken(ctx context.Context, idToken string) (string, error)
}

// ErrorTracker エラーログ記録を抽象化するインターフェース
type ErrorTracker interface {
	RecordError(log models.ErrorLog)
}

// ==========================================================
// WaitingListHandler 構造体 & コンストラクタ
// ==========================================================

// WaitingListHandler 待機リスト関連のHTTPリクエストを処理するハンドラ
type WaitingListHandler struct {
	waitingRepo WaitingListRepository
	storeRepo   StoreRepository
	userRepo    UserRepository
	authSvc     AuthService
	// - ゲストと直員が同一エンドポイントを共有しており認証要否がリクエスト内容で変わるため、
	//   このハンドラだけはセッション検証をミドルウェアに任せられず、自前で呼び出す
	sessionRepo MiddlewareSessionRepository
	broker      *events.Broker
	userBroker  *events.WaitingUserBroker
	tracker     ErrorTracker
}

// NewWaitingListHandler WaitingListHandlerのコンストラクタ
func NewWaitingListHandler(
	waitingRepo WaitingListRepository,
	storeRepo StoreRepository,
	userRepo UserRepository,
	authSvc AuthService,
	sessionRepo MiddlewareSessionRepository,
	broker *events.Broker,
	userBroker *events.WaitingUserBroker,
	tracker ErrorTracker,
) *WaitingListHandler {
	return &WaitingListHandler{
		waitingRepo: waitingRepo,
		storeRepo:   storeRepo,
		userRepo:    userRepo,
		authSvc:     authSvc,
		sessionRepo: sessionRepo,
		broker:      broker,
		userBroker:  userBroker,
		tracker:     tracker,
	}
}

// ==========================================================
// 公開ハンドラメソッド
// ==========================================================

// Handle WaitingListの主要な操作を処理するメインハンドラ
func (h *WaitingListHandler) Handle(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("action") == "average_waiting_time" {
			h.handleGetAverageWaitingTime(w, r)
			return
		}
		if r.URL.Query().Get("action") == "qr_token" {
			h.handleGetQRToken(w, r)
			return
		}
		h.handleGetWaitingList(w, r)
	case http.MethodPost:
		if r.URL.Query().Get("action") == "clear" {
			h.handleClearWaitingList(w, r)
			return
		}
		h.handleCreateWaitingList(w, r)
	case http.MethodPatch:
		if r.URL.Query().Get("action") == "status" {
			h.handleUpdateWaitingStatus(w, r)
			return
		}
		fallthrough
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandlePolling WaitingListアップデートのためのポーリングリクエストを処理
func (h *WaitingListHandler) HandlePolling(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	waitingList, err := h.waitingRepo.GetWaitingList(storeID)
	if err != nil {
		log.Printf("Error fetching waiting list data: %v", err)
		utils.RespondWithError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, waitingList, http.StatusOK)
}

// HandleStream リアルタイム待機リスト更新のためのServer-Sent Eventsを処理
func (h *WaitingListHandler) HandleStream(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		http.Error(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// - SSE用ヘッダーを設定
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// - クライアントチャンネルを作成してブローカーに登録
	clientChan := make(chan string, 10)
	h.broker.AddClient(storeID, clientChan)
	defer h.broker.RemoveClient(storeID, clientChan)

	// - 接続終了を監視
	notify := r.Context().Done()

	// - 接続時に初期データを送信
	go func() {
		waitingList, err := h.waitingRepo.GetWaitingList(storeID)
		if err == nil {
			jsonData, _ := json.Marshal(waitingList)
			h.broker.Broadcast(storeID, string(jsonData))
		}
	}()

	for {
		select {
		case <-notify:
			// - SSE接続切断をエラートラッカーに記録
			h.tracker.RecordError(models.ErrorLog{
				Timestamp: time.Now().UTC(),
				ErrorType: "SSE_DISCONNECT",
				Message:   "SSE Client Disconnected",
				Path:      r.URL.Path,
				Method:    r.Method,
				ClientIP:  r.RemoteAddr,
			})
			return
		case msg := <-clientChan:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			w.(http.Flusher).Flush()
		}
	}
}

// HandleWaitingItemStream 個別の待機顧客のリアルタイムステータス変化を監視するSSEを処理
func (h *WaitingListHandler) HandleWaitingItemStream(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	waitingID := r.URL.Query().Get("waiting_id")
	if storeID == "" || waitingID == "" {
		http.Error(w, "Missing store_id or waiting_id parameter", http.StatusBadRequest)
		return
	}

	// - SSE用ヘッダーを設定
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// - クライアントチャンネルを作成してブローカーに登録
	clientChan := make(chan string, 10)
	key := storeID + ":" + waitingID
	h.userBroker.AddClient(key, clientChan)
	defer h.userBroker.RemoveClient(key, clientChan)

	// - 接続終了を監視
	notify := r.Context().Done()

	// - 接続時に初期データを送信
	go func() {
		res, err := h.getWaitingUserResponse(storeID, waitingID)
		if err == nil {
			jsonData, _ := json.Marshal(res)
			h.userBroker.Broadcast(key, string(jsonData))
		}
	}()

	for {
		select {
		case <-notify:
			// - 個別待機顧客のSSE接続切断をエラートラッカーに記録
			h.tracker.RecordError(models.ErrorLog{
				Timestamp: time.Now().UTC(),
				ErrorType: "SSE_DISCONNECT",
				Message:   "SSE User Disconnected",
				Path:      r.URL.Path,
				Method:    r.Method,
				ClientIP:  r.RemoteAddr,
			})
			return
		case msg := <-clientChan:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			w.(http.Flusher).Flush()
		}
	}
}

// ==========================================================
// プライベートハンドラメソッド
// ==========================================================

// handleGetWaitingList 待機リストの取得(GET)を処理
func (h *WaitingListHandler) handleGetWaitingList(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		http.Error(w, "Missing storeId parameter", http.StatusBadRequest)
		return
	}

	waitingListData, err := h.waitingRepo.GetWaitingList(storeID)
	if err != nil {
		log.Printf("Failed to fetch waiting list: %v", err)
		utils.RespondWithError(w, "Failed to fetch waiting list", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, waitingListData, http.StatusOK)
}

// isStoreOpenNow 現在時刻が指定店舗の営業時間内かどうかを判定する。
// - 定休日(closed_days.regular_weekly、shift_table_handler.goのisClosedDayと共通ロジック)なら false
// - 24時間営業なら true
// - 当日の営業時間データが無い/不正な場合は、既存店舗の設定不備で誤って受付停止にしないよう true (制限しない)
// - 閉店時刻が開始時刻以下の場合は日をまたぐ営業(例: 18:00〜翌2:00)とみなして判定する
func isStoreOpenNow(settings *models.Settings, now time.Time) bool {
	weekday := strings.ToLower(now.Weekday().String())

	if isClosedDay(weekday, settings.ClosedDays) {
		return false
	}

	if settings.Is24Hours {
		return true
	}

	hours, ok := settings.OperatingHours[weekday]
	if !ok || hours.Start == "" || hours.End == "" {
		return true
	}

	startMin, okStart := parseTimeToMinutes(hours.Start)
	endMin, okEnd := parseTimeToMinutes(hours.End)
	if !okStart || !okEnd {
		return true
	}

	nowMin := now.Hour()*60 + now.Minute()

	if endMin <= startMin {
		return nowMin >= startMin || nowMin < endMin
	}
	return nowMin >= startMin && nowMin < endMin
}

// handleCreateWaitingList 新しい待機リストアイテムの作成(POST)を処理
func (h *WaitingListHandler) handleCreateWaitingList(w http.ResponseWriter, r *http.Request) {
	// - QRトークンの検証
	vToken := r.URL.Query().Get("v_token")
	if vToken == "" {
		log.Printf("Missing v_token in query parameter")
		http.Error(w, "QRコードが正しくないか、期限切れです。再度スキャンして下さい。", http.StatusForbidden)
		return
	}

	var newWaiting models.WaitingList
	if err := json.NewDecoder(r.Body).Decode(&newWaiting); err != nil {
		log.Printf("Error decoding request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// - JST基準の営業日を計算
	jst := time.FixedZone("JST", 9*60*60)
	now := time.Now().In(jst)
	businessDate := h.waitingRepo.GetBusinessDayCutoff(newWaiting.StoreID, now)
	dateStr := businessDate.Format("20060102")

	// - デフォルトのソースはウェブ(QRコード)
	newWaiting.Source = "web"

	// - HMACトークンの検証
	if !utils.VerifyHMACDateToken(newWaiting.StoreID, dateStr, vToken) {
		log.Printf("Invalid v_token for store %s: %s (Expected for %s)", newWaiting.StoreID, vToken, dateStr)
		http.Error(w, "QRコードが正しくないか、期限切れです。再度スキャンして下さい。", http.StatusForbidden)
		return
	}

	// - 必須フィールド検証
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

	// - 最大受付可能人数チェックとメニュー選択必須チェック
	settings, err := h.storeRepo.GetSettings(newWaiting.StoreID)
	if err == nil {
		maxCount := settings.Settings.WaitingPolicy.MaxWaitingCount
		if maxCount > 0 && newWaiting.PartySize > maxCount {
			log.Printf("Party size %d exceeds max waiting count %d", newWaiting.PartySize, maxCount)
			http.Error(w, fmt.Sprintf("最大受付可能人数(%d人)を超えました", maxCount), http.StatusBadRequest)
			return
		}

		// - 事前メニュー選択が有効な場合、メニュー項目が必須かチェック
		if settings.Settings.WaitingPolicy.EnableMenuSelection {
			if len(newWaiting.MenuItems) == 0 {
				log.Printf("Menu selection required but missing for store %s", newWaiting.StoreID)
				http.Error(w, "メニューの選択は必須です。", http.StatusBadRequest)
				return
			}
			// - 1人1メニュー制限チェック
			if settings.Settings.WaitingPolicy.RequireOneMenuPerPerson {
				totalQuantity := 0
				for _, item := range newWaiting.MenuItems {
					totalQuantity += item.Quantity
				}
				if totalQuantity < newWaiting.PartySize {
					log.Printf("One menu per person check failed: total %d, party %d", totalQuantity, newWaiting.PartySize)
					http.Error(w, "お一人様につき少なくとも1つのメニューを注文してください。", http.StatusBadRequest)
					return
				}
			}
		}
	}

	// - Authorizationヘッダーがある場合(スタッフ/マネージャー)、権限チェック
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		idToken := authHeader[len("Bearer "):]
		firebaseUID, err := h.authSvc.VerifyIDToken(r.Context(), idToken)
		if err != nil {
			utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}

		// - ユーザー情報取得
		user, err := h.userRepo.GetByFirebaseUID(firebaseUID)
		if err != nil || user == nil {
			log.Printf("Failed to get user by Firebase UID: %v", err)
			utils.RespondWithError(w, "User not found", http.StatusUnauthorized)
			return
		}

		// - 端末セッションチェック (ミドルウェアを適用できないため自前で呼び出す)
		if !VerifySessionForUser(w, r, h.sessionRepo, user) {
			return
		}

		// - 権限チェック
		hasPermission, err := h.userRepo.CheckStorePermission(user.ID, newWaiting.StoreID, user.Role, "")
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
		// - スタッフ/マネージャーによるAppからの登録
		newWaiting.Source = "app"
	}

	// - 営業時間外の受付拒否 (QRからの顧客登録のみ対象。マネージャー/スタッフによる
	//   Appからの手動登録(source=="app")は、閉店直前のウォークイン客対応のため対象外)
	if newWaiting.Source == "web" && settings != nil {
		if !isStoreOpenNow(&settings.Settings, now) {
			log.Printf("Store %s is currently closed (outside operating hours), rejecting web waiting registration", newWaiting.StoreID)
			http.Error(w, "現在、営業時間外のため、待機受付を行っておりません。", http.StatusForbidden)
			return
		}
	}

	// - ライセンス確認
	license, err := h.storeRepo.GetLicense(newWaiting.StoreID)
	if err != nil {
		log.Printf("Failed to get license info for store %s: %v", newWaiting.StoreID, err)
		http.Error(w, "この店舗の認証情報が見つかりません。", http.StatusForbidden)
		return
	}
	if license.VerificationStatus != models.StatusApproved {
		log.Printf("Store %s is not approved. Status: %s", newWaiting.StoreID, license.VerificationStatus)
		http.Error(w, "現在、この店舗は待機受付を行っておりません。", http.StatusForbidden)
		return
	}

	// - 待機リストアイテムを作成
	createdItem, err := h.waitingRepo.CreateItem(newWaiting)
	if err != nil {
		log.Printf("Failed to create waiting list item: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, createdItem, http.StatusCreated)

	// - DBの一貫性確保のため少し待機してから通知
	go func() {
		time.Sleep(100 * time.Millisecond)
		h.notifyStore(newWaiting.StoreID)
	}()
}

// handleClearWaitingList 待機リストをクリアするリクエストを処理
func (h *WaitingListHandler) handleClearWaitingList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// - Firebase認証チェック
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}

	idToken := authHeader[len("Bearer "):]
	firebaseUID, err := h.authSvc.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// - ユーザー情報取得
	user, err := h.userRepo.GetByFirebaseUID(firebaseUID)
	if err != nil || user == nil {
		log.Printf("Failed to get user by Firebase UID: %v", err)
		utils.RespondWithError(w, "User not found", http.StatusUnauthorized)
		return
	}

	// - 端末セッションチェック (ミドルウェアを適用できないため自前で呼び出す)
	if !VerifySessionForUser(w, r, h.sessionRepo, user) {
		return
	}

	// - 権限チェック
	hasPermission, err := h.userRepo.CheckStorePermission(user.ID, storeID, user.Role, "")
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

	// - 待機リストをクリア
	if err := h.waitingRepo.ClearList(storeID); err != nil {
		log.Printf("Failed to clear waiting list: %v", err)
		utils.RespondWithError(w, "Failed to clear waiting list", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]string{"message": "Waiting list cleared successfully"}, http.StatusOK)
	h.notifyStore(storeID)
}

// handleUpdateWaitingStatus 待機リストのステータスをアップデートするPATCHリクエストを処理
func (h *WaitingListHandler) handleUpdateWaitingStatus(w http.ResponseWriter, r *http.Request) {
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

	// - Firebase認証チェック
	authHeader := r.Header.Get("Authorization")

	if authHeader == "" {
		// - キャンセル以外の操作は認証が必要
		if updateRequest.Status != "cancelled" {
			utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
			return
		}
		// - status == "cancelled" の場合は認証スキップ (ゲストによるキャンセル)
	} else {
		// - スタッフ/マネージャーによる操作
		idToken := authHeader[len("Bearer "):]
		firebaseUID, err := h.authSvc.VerifyIDToken(r.Context(), idToken)
		if err != nil {
			utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}

		// - ユーザー情報取得
		user, err := h.userRepo.GetByFirebaseUID(firebaseUID)
		if err != nil || user == nil {
			log.Printf("Failed to get user by Firebase UID: %v", err)
			utils.RespondWithError(w, "User not found", http.StatusUnauthorized)
			return
		}

		// - 端末セッションチェック (ミドルウェアを適用できないため自前で呼び出す)
		if !VerifySessionForUser(w, r, h.sessionRepo, user) {
			return
		}

		// - 権限チェック
		hasPermission, err := h.userRepo.CheckStorePermission(user.ID, updateRequest.StoreID, user.Role, "")
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

	// - ステータス有効性検証
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

	// - ステータスアップデート
	if err := h.waitingRepo.UpdateItemStatus(updateRequest.StoreID, updateRequest.WaitingID, updateRequest.Status); err != nil {
		log.Printf("Failed to update waiting status: %v", err)
		http.Error(w, "Failed to update waiting status", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]string{
		"message": "Status updated successfully",
		"status":  updateRequest.Status,
	}, http.StatusOK)
	h.notifyStore(updateRequest.StoreID)
}

// handleGetAverageWaitingTime 平均待機時間を返すハンドラ
func (h *WaitingListHandler) handleGetAverageWaitingTime(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	avgSec, err := h.waitingRepo.GetAverageWaitingTime(storeID)
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

	utils.RespondWithJSON(w, models.AverageWaitingTimeResponse{
		AverageSeconds: avgSec,
		AverageText:    avgText,
	}, http.StatusOK)
}

// handleGetQRToken QRトークンを生成して返すハンドラ
func (h *WaitingListHandler) handleGetQRToken(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// - JST基準の日付 (Dynamic Cutoff)
	jst := time.FixedZone("JST", 9*60*60)
	now := time.Now().In(jst)
	businessDate := h.waitingRepo.GetBusinessDayCutoff(storeID, now)
	dateStr := businessDate.Format("20060102")

	token := utils.GenerateHMACDateToken(storeID, dateStr)

	utils.RespondWithJSON(w, map[string]string{
		"v_token": token,
		"date":    dateStr,
	}, http.StatusOK)
}

// ==========================================================
// プライベートヘルパーメソッド
// ==========================================================

// WaitingUserResponse 個別の待機顧客用SSEの応答データ構造
type WaitingUserResponse struct {
	models.WaitingList
	WaitingCount         int    `json:"waiting_count"`
	EstimatedWaitingTime string `json:"estimated_waiting_time"`
}

// notifyStore 最新データを取得し、全サブスクライバーにブロードキャストする
func (h *WaitingListHandler) notifyStore(storeID string) {
	waitingList, err := h.waitingRepo.GetWaitingList(storeID)
	if err != nil {
		log.Printf("Error fetching waiting list for broadcast: %v", err)
		return
	}

	jsonData, err := json.Marshal(waitingList)
	if err != nil {
		log.Printf("Error marshaling waiting list for broadcast: %v", err)
		return
	}

	h.broker.Broadcast(storeID, string(jsonData))

	// - 個別待機ユーザー用SSEにもアップデートを送信
	h.notifyWaitingUsers(storeID)
}

// notifyWaitingUsers 指定店舗の全アクティブな待機顧客にアップデートを送信する
func (h *WaitingListHandler) notifyWaitingUsers(storeID string) {
	waitingList, err := h.waitingRepo.GetWaitingList(storeID)
	if err != nil {
		log.Printf("Error fetching waiting list for notifying users: %v", err)
		return
	}

	for _, item := range waitingList {
		key := storeID + ":" + item.WaitingID

		h.userBroker.Mutex.RLock()
		clients, exists := h.userBroker.Clients[key]
		clientsExist := exists && len(clients) > 0
		h.userBroker.Mutex.RUnlock()

		if clientsExist {
			res, err := h.getWaitingUserResponse(storeID, item.WaitingID)
			if err != nil {
				log.Printf("Error generating response for active client %s: %v", key, err)
				continue
			}

			jsonData, err := json.Marshal(res)
			if err != nil {
				log.Printf("Error marshaling response: %v", err)
				continue
			}

			h.userBroker.Broadcast(key, string(jsonData))
		}
	}
}

// getWaitingUserResponse 特定の待機アイテムの詳細応答データを構築する
func (h *WaitingListHandler) getWaitingUserResponse(storeID string, waitingID string) (*WaitingUserResponse, error) {
	waitingList, err := h.waitingRepo.GetWaitingList(storeID)
	if err != nil {
		return nil, err
	}

	var details *models.WaitingList
	for i := range waitingList {
		if waitingList[i].WaitingID == waitingID {
			details = &waitingList[i]
			break
		}
	}
	if details == nil {
		return nil, fmt.Errorf("waiting item not found")
	}

	// - アクティブな待機アイテム(waiting, notified)のみを抽出
	var activeItems []models.WaitingList
	for _, item := range waitingList {
		if item.Status == "waiting" || item.Status == "notified" {
			activeItems = append(activeItems, item)
		}
	}

	// - QueueNumber順にソート
	sort.Slice(activeItems, func(i, j int) bool {
		return activeItems[i].QueueNumber < activeItems[j].QueueNumber
	})

	// - 自分より前の人数をカウント
	waitingCount := 0
	for i, item := range activeItems {
		if item.WaitingID == waitingID {
			waitingCount = i
			break
		}
	}

	// - 店舗設定からチームあたりの時間を取得
	minutesPerTeam := 10
	if settings, err := h.storeRepo.GetSettings(storeID); err == nil && settings.Settings.WaitingPolicy.EstimatedWaitTime > 0 {
		minutesPerTeam = settings.Settings.WaitingPolicy.EstimatedWaitTime
	}

	return &WaitingUserResponse{
		WaitingList:          *details,
		WaitingCount:         len(activeItems),
		EstimatedWaitingTime: fmt.Sprintf("%d mins", waitingCount*minutesPerTeam),
	}, nil
}
