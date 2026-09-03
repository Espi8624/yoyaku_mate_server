package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// StaffRepository スタッフ管理の操作を抽象化するインターフェース
type StaffRepository interface {
	CheckStoreStaffExists(userID primitive.ObjectID, storeID string) (bool, error)
	CreateStoreStaffInfo(staffInfo models.StoreStaffInfo) error
	GetStoreStaffByStoreID(storeID string) ([]map[string]interface{}, error)
	GetStoreStaffByUserAndStore(userID primitive.ObjectID, storeID string) (*models.StoreStaffInfo, error)
	GetStoreStaffByID(staffID string) (*models.StoreStaffInfo, error)
	UpdateStoreStaffStatus(staffID, status string) error
	UpdateStoreStaffPermissions(staffID string, permissions []string) error
	UpdateStoreStaffAvailability(staffID string, availability models.Availability) error
}

// validateAvailability Availability内の各勤務不可時間帯が妥当かどうかを検証。
// 終日指定(AllDay=true)なら時刻は不要、時間帯指定なら "HH:MM" 形式かつ開始<終了を要求する
// (timeFormatPatternは shift_table_handler.go で定義)
func validateAvailability(a models.Availability) bool {
	days := [][]models.UnavailableRange{
		a.Monday, a.Tuesday, a.Wednesday, a.Thursday, a.Friday, a.Saturday, a.Sunday,
	}
	for _, ranges := range days {
		for _, r := range ranges {
			if r.AllDay {
				continue
			}
			if !timeFormatPattern.MatchString(r.StartTime) || !timeFormatPattern.MatchString(r.EndTime) {
				return false
			}
			if r.StartTime >= r.EndTime {
				return false
			}
		}
	}
	return true
}

// StoreStaffHandler スタッフ管理関連のHTTPリクエストを処理するハンドラ
type StoreStaffHandler struct {
	staffRepo StaffRepository
	userRepo  UserRepository      // UserRepository for getting user and checking permissions
	storeRepo StoreInfoRepository // StoreRepository for checking store existence
	authSvc   AuthService
}

func NewStoreStaffHandler(staffRepo StaffRepository, userRepo UserRepository, storeRepo StoreInfoRepository, authSvc AuthService) *StoreStaffHandler {
	return &StoreStaffHandler{
		staffRepo: staffRepo,
		userRepo:  userRepo,
		storeRepo: storeRepo,
		authSvc:   authSvc,
	}
}

// JoinStoreHandler スタッフが店舗に参加するためのリクエストを処理
func (h *StoreStaffHandler) JoinStoreHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. トークンの検証
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	firebaseUID, err := h.authSvc.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Invalid or expired token: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// 2. リクエストボディのパース
	var req struct {
		StoreID string `json:"store_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.StoreID == "" {
		utils.RespondWithError(w, "store_id is required", http.StatusBadRequest)
		return
	}

	// 3. FirebaseUIDによるユーザー検索
	user, err := h.userRepo.GetByFirebaseUID(firebaseUID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			utils.RespondWithError(w, "User not found", http.StatusNotFound)
		} else {
			utils.RespondWithError(w, "Database error finding user", http.StatusInternalServerError)
		}
		return
	}

	// 4. 店舗が存在するか確認
	_, err = h.storeRepo.GetStoreData(req.StoreID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			utils.RespondWithError(w, "Store not found", http.StatusNotFound)
		} else {
			utils.RespondWithError(w, "Database error checking store", http.StatusInternalServerError)
		}
		return
	}

	// 5. 既に参加済みまたは申請中か確認
	exists, err := h.staffRepo.CheckStoreStaffExists(user.ID, req.StoreID)
	if err != nil {
		utils.RespondWithError(w, "Database error checking staff info", http.StatusInternalServerError)
		return
	}
	if exists {
		utils.RespondWithError(w, "User is already a staff member or pending approval for this store", http.StatusConflict)
		return
	}

	// 6. 店舗スタッフ情報の作成
	newStaffInfo := models.StoreStaffInfo{
		ID:        primitive.NewObjectID(),
		UserID:    user.ID,
		Role:      "staff", // Default role for joining
		StoreID:   req.StoreID,
		Status:    models.StaffStatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := h.staffRepo.CreateStoreStaffInfo(newStaffInfo); err != nil {
		utils.RespondWithError(w, "Failed to create staff info", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]string{"message": "Join request sent successfully"}, http.StatusCreated)
}

// hasStaffManagementAccess マネージャー、または承認済みスタッフかどうかを確認
// (スタッフ一覧の閲覧はマネージャーと同等の権限で運用する)
func (h *StoreStaffHandler) hasStaffManagementAccess(userID primitive.ObjectID, storeID string) (bool, error) {
	isManager, err := h.userRepo.CheckStorePermission(userID, storeID, "manager", "")
	if err != nil {
		return false, err
	}
	if isManager {
		return true, nil
	}
	return h.userRepo.CheckStorePermission(userID, storeID, "staff", "")
}

// hasAccessToStaffTarget マネージャー、または「対象が本人自身」の承認済みスタッフかどうかを確認
// (スタッフはステータス/権限/勤務可能時間を自分自身に対してのみ操作可能。他人の情報は変更不可)
func (h *StoreStaffHandler) hasAccessToStaffTarget(userID primitive.ObjectID, storeID, targetStaffID string) (bool, error) {
	isManager, err := h.userRepo.CheckStorePermission(userID, storeID, "manager", "")
	if err != nil {
		return false, err
	}
	if isManager {
		return true, nil
	}

	ownStaffInfo, err := h.staffRepo.GetStoreStaffByUserAndStore(userID, storeID)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return false, nil
		}
		return false, err
	}
	if ownStaffInfo.Status != models.StaffStatusApproved {
		return false, nil
	}
	return ownStaffInfo.ID.Hex() == targetStaffID, nil
}

// GetStoreStaffHandler 店舗の全スタッフを取得するリクエストを処理
func (h *StoreStaffHandler) GetStoreStaffHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]

	if storeID == "" {
		utils.RespondWithError(w, "store_id is required", http.StatusBadRequest)
		return
	}

	// 1. トークンと権限の検証 (マネージャーのみ)
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	firebaseUID, err := h.authSvc.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	user, err := h.userRepo.GetByFirebaseUID(firebaseUID)
	if err != nil || user == nil {
		utils.RespondWithError(w, "User not found", http.StatusUnauthorized)
		return
	}

	// ユーザーがこの店舗のマネージャーまたは承認済みスタッフかどうか確認
	hasPermission, err := h.hasStaffManagementAccess(user.ID, storeID)
	if err != nil {
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasPermission {
		utils.RespondWithError(w, "You do not have permission to view staff for this store", http.StatusForbidden)
		return
	}

	// 2. スタッフリストの取得
	staffList, err := h.staffRepo.GetStoreStaffByStoreID(storeID)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch staff list", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, staffList, http.StatusOK)
}

// UpdateStoreStaffStatusHandler スタッフのステータスを更新するリクエストを処理
func (h *StoreStaffHandler) UpdateStoreStaffStatusHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	staffID := vars["staffId"]

	if storeID == "" || staffID == "" {
		utils.RespondWithError(w, "store_id and staff_id are required", http.StatusBadRequest)
		return
	}

	// 1. トークンと権限の検証 (マネージャーのみ)
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	firebaseUID, err := h.authSvc.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	user, err := h.userRepo.GetByFirebaseUID(firebaseUID)
	if err != nil || user == nil {
		utils.RespondWithError(w, "User not found", http.StatusUnauthorized)
		return
	}

	// ユーザーがこの店舗のマネージャー、または対象が本人自身であるスタッフかどうか確認
	hasPermission, err := h.hasAccessToStaffTarget(user.ID, storeID, staffID)
	if err != nil {
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasPermission {
		utils.RespondWithError(w, "You do not have permission to manage staff for this store", http.StatusForbidden)
		return
	}

	// 2. リクエストボディのパース
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// ステータスの検証
	validStatuses := map[string]bool{
		models.StaffStatusApproved: true,
		models.StaffStatusRejected: true,
		models.StaffStatusPending:  true,
	}
	if !validStatuses[req.Status] {
		utils.RespondWithError(w, "Invalid status", http.StatusBadRequest)
		return
	}

	// 3. ステータスの更新
	if err := h.staffRepo.UpdateStoreStaffStatus(staffID, req.Status); err != nil {
		utils.RespondWithError(w, "Failed to update staff status", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]string{"message": "Staff status updated successfully"}, http.StatusOK)
}

// UpdateStoreStaffPermissionsHandler スタッフの権限を更新するリクエストを処理
func (h *StoreStaffHandler) UpdateStoreStaffPermissionsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	staffID := vars["staffId"]

	if storeID == "" || staffID == "" {
		utils.RespondWithError(w, "store_id and staff_id are required", http.StatusBadRequest)
		return
	}

	// 1. トークンと権限の検証 (マネージャーのみ)
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	firebaseUID, err := h.authSvc.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	user, err := h.userRepo.GetByFirebaseUID(firebaseUID)
	if err != nil || user == nil {
		utils.RespondWithError(w, "User not found", http.StatusUnauthorized)
		return
	}

	// ユーザーがこの店舗のマネージャー、または対象が本人自身であるスタッフかどうか確認
	hasPermission, err := h.hasAccessToStaffTarget(user.ID, storeID, staffID)
	if err != nil {
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasPermission {
		utils.RespondWithError(w, "You do not have permission to manage staff permissions for this store", http.StatusForbidden)
		return
	}

	// 2. リクエストボディのパース
	var req struct {
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 3. 権限の更新
	if err := h.staffRepo.UpdateStoreStaffPermissions(staffID, req.Permissions); err != nil {
		utils.RespondWithError(w, "Failed to update staff permissions", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]string{"message": "Staff permissions updated successfully"}, http.StatusOK)
}

// UpdateStoreStaffAvailabilityHandler マネージャーが特定スタッフの勤務可能な曜日・時間帯を更新
func (h *StoreStaffHandler) UpdateStoreStaffAvailabilityHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	staffID := vars["staffId"]

	if storeID == "" || staffID == "" {
		utils.RespondWithError(w, "store_id and staff_id are required", http.StatusBadRequest)
		return
	}

	// 1. トークンと権限の検証 (マネージャーのみ)
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	firebaseUID, err := h.authSvc.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	user, err := h.userRepo.GetByFirebaseUID(firebaseUID)
	if err != nil || user == nil {
		utils.RespondWithError(w, "User not found", http.StatusUnauthorized)
		return
	}

	// ユーザーがこの店舗のマネージャー、または対象が本人自身であるスタッフかどうか確認
	hasPermission, err := h.hasAccessToStaffTarget(user.ID, storeID, staffID)
	if err != nil {
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasPermission {
		utils.RespondWithError(w, "You do not have permission to manage staff availability for this store", http.StatusForbidden)
		return
	}

	// 2. リクエストボディのパース
	var req struct {
		Availability models.Availability `json:"availability"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if !validateAvailability(req.Availability) {
		utils.RespondWithError(w, "Invalid time block value", http.StatusBadRequest)
		return
	}

	// 3. 勤務可能日の更新
	if err := h.staffRepo.UpdateStoreStaffAvailability(staffID, req.Availability); err != nil {
		utils.RespondWithError(w, "Failed to update availability", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]string{"message": "Staff availability updated successfully"}, http.StatusOK)
}
