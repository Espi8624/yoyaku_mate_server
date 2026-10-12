package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/shift"

	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// ShiftTableRepository 店舗の週単位シフト表の操作を抽象化するインターフェース
type ShiftTableRepository interface {
	GetShiftTable(storeID, weekStartDate string) (*models.ShiftTable, error)
	CreateShiftTable(table models.ShiftTable) error
	AddShift(shiftTableID string, shift models.Shift) error
	UpdateShift(shiftTableID, shiftID string, shift models.Shift) error
	DeleteShift(shiftTableID, shiftID string) error
	ReplaceShifts(shiftTableID string, shifts []models.Shift) error
	PublishShifts(shiftTableID string, shifts []models.Shift, publishedAt time.Time) error
	GetStaffShiftCounts(storeID, beforeWeekStartDate string, lookbackWeeks int) (map[primitive.ObjectID]int, error)
	GetStaffPairCounts(storeID, beforeWeekStartDate string, lookbackWeeks int) (map[string]int, error)
}

// ShiftChangeRequestRepository 週間シフト表に対する修正依頼の操作を抽象化するインターフェース
type ShiftChangeRequestRepository interface {
	CreateRequest(req models.ShiftChangeRequest) error
	GetRequestsForWeek(storeID, weekStartDate string) ([]models.ShiftChangeRequest, error)
	ResolveAppliedForWeek(storeID, weekStartDate string) ([]models.ShiftChangeRequest, error)
	RevertAppliedForWeek(storeID, weekStartDate string) (int, error)
	MarkRequestApplied(requestID string) error
	DeleteRequest(requestID string) error
}

// ShiftTableHandler シフト表関連のHTTPリクエストを処理するハンドラ
type ShiftTableHandler struct {
	shiftTableRepo    ShiftTableRepository
	staffRepo         StaffRepository
	userRepo          UserRepository
	authSvc           AuthService
	settingsRepo      StoreSettingsRepository
	changeRequestRepo ShiftChangeRequestRepository
}

func NewShiftTableHandler(shiftTableRepo ShiftTableRepository, staffRepo StaffRepository, userRepo UserRepository, authSvc AuthService, settingsRepo StoreSettingsRepository, changeRequestRepo ShiftChangeRequestRepository) *ShiftTableHandler {
	return &ShiftTableHandler{
		shiftTableRepo:    shiftTableRepo,
		staffRepo:         staffRepo,
		userRepo:          userRepo,
		authSvc:           authSvc,
		settingsRepo:      settingsRepo,
		changeRequestRepo: changeRequestRepo,
	}
}

// authenticate トークンを検証し、リクエストユーザーを取得する共通処理
func (h *ShiftTableHandler) authenticate(r *http.Request) (*models.User, int, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, http.StatusUnauthorized, fmt.Errorf("Authorization header is required")
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	firebaseUID, err := h.authSvc.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		return nil, http.StatusUnauthorized, fmt.Errorf("Invalid or expired token")
	}

	user, err := h.userRepo.GetByFirebaseUID(firebaseUID)
	if err != nil || user == nil {
		return nil, http.StatusUnauthorized, fmt.Errorf("User not found")
	}
	return user, http.StatusOK, nil
}

// hasViewAccess マネージャー、または承認済みスタッフかどうかを確認 (閲覧権限)
func (h *ShiftTableHandler) hasViewAccess(userID primitive.ObjectID, storeID string) (bool, error) {
	isManager, err := h.userRepo.CheckStorePermission(userID, storeID, "manager", "")
	if err != nil {
		return false, err
	}
	if isManager {
		return true, nil
	}
	return h.userRepo.CheckStorePermission(userID, storeID, "staff", "")
}

// hasManageAccess マネージャーかどうかを確認 (シフト表の作成・編集権限)
func (h *ShiftTableHandler) hasManageAccess(userID primitive.ObjectID, storeID string) (bool, error) {
	return h.userRepo.CheckStorePermission(userID, storeID, "manager", "")
}

// GetShiftTableHandler 指定週のシフト表を取得
func (h *ShiftTableHandler) GetShiftTableHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]

	if storeID == "" || weekStartDate == "" {
		respondWithError(w, "store_id and week_start_date are required", http.StatusBadRequest)
		return
	}
	if !shift.IsValidWeekStartDate(weekStartDate) {
		respondWithError(w, "week_start_date must be a valid Monday date (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		respondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasViewAccess(user.ID, storeID)
	if err != nil {
		respondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		respondWithError(w, "You do not have permission to view the shift table for this store", http.StatusForbidden)
		return
	}

	isManager, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		respondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}

	table, err := h.shiftTableRepo.GetShiftTable(storeID, weekStartDate)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// この週にはまだシフト表が作成されていない
			respondWithError(w, "Shift table not found for this week", http.StatusNotFound)
			return
		}
		respondWithError(w, "Failed to fetch shift table", http.StatusInternalServerError)
		return
	}

	if !isManager {
		// スタッフには確定版だけを見せる。一度も確定していない週は「まだ作成されていない」のと
		// 区別する必要がないため、404を返して既存の空状態表示に寄せる
		if table.PublishedAt == nil {
			respondWithError(w, "Shift table not found for this week", http.StatusNotFound)
			return
		}
		// クライアントが下書き/確定版を意識せず描画できるよう、確定版を Shifts に載せ替えて返す
		table.Shifts = table.PublishedShifts
		table.HasUnpublishedChanges = false
		respondWithJSON(w, table, http.StatusOK)
		return
	}

	// マネージャーには下書きをそのまま返し、確定ボタンの出し分けに使う未確定フラグを添える
	table.HasUnpublishedChanges, err = h.hasUnpublishedChanges(table)
	if err != nil {
		respondWithError(w, "Failed to check unpublished changes", http.StatusInternalServerError)
		return
	}

	respondWithJSON(w, table, http.StatusOK)
}

// hasUnpublishedChanges 下書きに未確定の変更が残っているかを判定する。
// シフトの内容差分だけでなく「下書きへ反映済み・未確定(applied)」の修正依頼も見る。
// 出し直し(superseded)や陳腐化(stale)の依頼はシフト表を変えずに状態だけが変わるため、
// 内容比較だけだと確定ボタンが出ず、applied のまま永久に残ってしまう
func (h *ShiftTableHandler) hasUnpublishedChanges(table *models.ShiftTable) (bool, error) {
	if table.PublishedAt == nil {
		return true, nil
	}
	if !shift.ContentEqual(table.Shifts, table.PublishedShifts) {
		return true, nil
	}

	requests, err := h.changeRequestRepo.GetRequestsForWeek(table.StoreID, table.WeekStartDate)
	if err != nil {
		return false, err
	}
	for _, cr := range requests {
		if cr.Status == models.ShiftChangeRequestStatusApplied {
			return true, nil
		}
	}
	return false, nil
}

// PublishShiftTableHandler 下書きを確定し、スタッフに公開する (マネージャー専用)。
// シフト表がスタッフから見えるようになる唯一の経路で、同時に下書きへ反映済みの
// 修正依頼(applied)も処理済み(resolved)へ進める
func (h *ShiftTableHandler) PublishShiftTableHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]

	if storeID == "" || weekStartDate == "" {
		respondWithError(w, "store_id and week_start_date are required", http.StatusBadRequest)
		return
	}
	if !shift.IsValidWeekStartDate(weekStartDate) {
		respondWithError(w, "week_start_date must be a valid Monday date (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		respondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		respondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		respondWithError(w, "You do not have permission to publish the shift table for this store", http.StatusForbidden)
		return
	}

	table, err := h.shiftTableRepo.GetShiftTable(storeID, weekStartDate)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			respondWithError(w, "Shift table not found for this week", http.StatusNotFound)
			return
		}
		respondWithError(w, "Failed to fetch shift table", http.StatusInternalServerError)
		return
	}

	publishedAt := time.Now()
	if err := h.shiftTableRepo.PublishShifts(table.ID.Hex(), table.Shifts, publishedAt); err != nil {
		respondWithError(w, "Failed to publish shift table", http.StatusInternalServerError)
		return
	}

	// 確定版の公開に成功した後で依頼を処理済みにする。ここが失敗しても
	// シフト表自体は公開済みなので、依頼状態のズレはログに残して処理は続行する
	resolved, err := h.changeRequestRepo.ResolveAppliedForWeek(storeID, weekStartDate)
	if err != nil {
		log.Printf("Failed to resolve applied change requests after publishing %s/%s: %v", storeID, weekStartDate, err)
	}

	table.PublishedShifts = table.Shifts
	table.PublishedAt = &publishedAt
	table.UpdatedAt = publishedAt
	table.HasUnpublishedChanges = false

	respondWithJSON(w, map[string]interface{}{
		"shift_table":            table,
		"resolved_request_count": len(resolved),
	}, http.StatusOK)
}

// DiscardShiftTableDraftHandler 下書きを破棄し、確定版の内容へ戻す (マネージャー専用)。
// 「確定してからの編集をなかったことにする」操作で、確定版そのものは一切変更しない。
// スタッフに見えているシフト表は元から確定版なので、この操作でスタッフ側の表示は変わらない
func (h *ShiftTableHandler) DiscardShiftTableDraftHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]

	if storeID == "" || weekStartDate == "" {
		respondWithError(w, "store_id and week_start_date are required", http.StatusBadRequest)
		return
	}
	if !shift.IsValidWeekStartDate(weekStartDate) {
		respondWithError(w, "week_start_date must be a valid Monday date (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		respondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		respondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		respondWithError(w, "You do not have permission to edit the shift table for this store", http.StatusForbidden)
		return
	}

	table, err := h.shiftTableRepo.GetShiftTable(storeID, weekStartDate)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			respondWithError(w, "Shift table not found for this week", http.StatusNotFound)
			return
		}
		respondWithError(w, "Failed to fetch shift table", http.StatusInternalServerError)
		return
	}

	// 一度も確定していない週は戻す先が「空のシフト表」になる。
	// ここを拒否すると、シフト表を削除しただけの状態から自動配置をやり直す手段が
	// 無くなる (下部ボタンは確定に切り替わっている) ため、空へ戻すことを許可する
	published := table.PublishedShifts
	if published == nil {
		published = []models.Shift{}
	}
	if err := h.shiftTableRepo.ReplaceShifts(table.ID.Hex(), published); err != nil {
		respondWithError(w, "Failed to discard draft", http.StatusInternalServerError)
		return
	}

	// 下書きを捨てた以上、その下書きへの反映だけを根拠にした applied も未対応へ戻す。
	// ここが失敗しても下書きは既に戻っているため、ログに残して処理は続行する
	revertedCount, err := h.changeRequestRepo.RevertAppliedForWeek(storeID, weekStartDate)
	if err != nil {
		log.Printf("Failed to revert applied change requests after discarding draft %s/%s: %v", storeID, weekStartDate, err)
	}

	table.Shifts = published
	table.HasUnpublishedChanges = false

	respondWithJSON(w, map[string]interface{}{
		"shift_table":            table,
		"reverted_request_count": revertedCount,
	}, http.StatusOK)
}

// CreateShiftTableHandler 指定週の空のシフト表を作成 (マネージャー専用)
func (h *ShiftTableHandler) CreateShiftTableHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	if storeID == "" {
		respondWithError(w, "store_id is required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		respondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		respondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		respondWithError(w, "You do not have permission to create a shift table for this store", http.StatusForbidden)
		return
	}

	var req struct {
		WeekStartDate string `json:"week_start_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if !shift.IsValidWeekStartDate(req.WeekStartDate) {
		respondWithError(w, "week_start_date must be a valid Monday date (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	// 既に同じ週のシフト表が存在するか確認
	_, err = h.shiftTableRepo.GetShiftTable(storeID, req.WeekStartDate)
	if err == nil {
		respondWithError(w, "Shift table already exists for this week", http.StatusConflict)
		return
	}
	if err != mongo.ErrNoDocuments {
		respondWithError(w, "Failed to check existing shift table", http.StatusInternalServerError)
		return
	}

	newTable := models.ShiftTable{
		ID:            primitive.NewObjectID(),
		StoreID:       storeID,
		WeekStartDate: req.WeekStartDate,
	}
	if err := h.shiftTableRepo.CreateShiftTable(newTable); err != nil {
		respondWithError(w, "Failed to create shift table", http.StatusInternalServerError)
		return
	}

	respondWithJSON(w, newTable, http.StatusCreated)
}

// validateAndBuildShift リクエストボディを検証し、承認済みスタッフに紐づくShiftを構築する共通処理
func (h *ShiftTableHandler) validateAndBuildShift(r *http.Request, storeID string) (models.Shift, int, error) {
	var req struct {
		StaffID   string `json:"staff_id"`
		Day       string `json:"day"`
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return models.Shift{}, http.StatusBadRequest, fmt.Errorf("Invalid request body")
	}
	if !shift.IsValidSlot(req.Day, req.StartTime, req.EndTime) {
		return models.Shift{}, http.StatusBadRequest, fmt.Errorf("Invalid day or time range")
	}

	staffObjID, err := primitive.ObjectIDFromHex(req.StaffID)
	if err != nil {
		return models.Shift{}, http.StatusBadRequest, fmt.Errorf("Invalid staff_id")
	}

	// マネージャーは store_staff_info を持たないことが多いため、まず店舗のマネージャー
	// 本人かどうかを確認する。マネージャーであれば承認済みスタッフと同様に許可する
	// (シフト表画面の「マネージャー」選択肢はこのIDを使う)
	settings, err := h.settingsRepo.GetSettings(storeID)
	if err != nil {
		return models.Shift{}, http.StatusInternalServerError, fmt.Errorf("Failed to verify store settings")
	}
	if req.StaffID != settings.ManagerID {
		staffInfo, err := h.staffRepo.GetStoreStaffByID(req.StaffID)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				return models.Shift{}, http.StatusNotFound, fmt.Errorf("Staff not found")
			}
			return models.Shift{}, http.StatusInternalServerError, fmt.Errorf("Failed to verify staff")
		}
		if staffInfo.StoreID != storeID || staffInfo.Status != models.StaffStatusApproved {
			return models.Shift{}, http.StatusBadRequest, fmt.Errorf("Staff is not an approved member of this store")
		}
	}

	return models.Shift{
		ID:        primitive.NewObjectID(),
		StaffID:   staffObjID,
		Day:       req.Day,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}, http.StatusOK, nil
}

// AddShiftHandler 指定週のシフト表にシフトを1件追加 (マネージャー専用)
func (h *ShiftTableHandler) AddShiftHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]
	if storeID == "" || weekStartDate == "" {
		respondWithError(w, "store_id and week_start_date are required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		respondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		respondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		respondWithError(w, "You do not have permission to edit the shift table for this store", http.StatusForbidden)
		return
	}

	table, err := h.shiftTableRepo.GetShiftTable(storeID, weekStartDate)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			respondWithError(w, "Shift table not found for this week. Create it first.", http.StatusNotFound)
			return
		}
		respondWithError(w, "Failed to fetch shift table", http.StatusInternalServerError)
		return
	}

	newShift, statusCode, err := h.validateAndBuildShift(r, storeID)
	if err != nil {
		respondWithError(w, err.Error(), statusCode)
		return
	}

	if err := h.shiftTableRepo.AddShift(table.ID.Hex(), newShift); err != nil {
		respondWithError(w, "Failed to add shift", http.StatusInternalServerError)
		return
	}

	respondWithJSON(w, newShift, http.StatusCreated)
}

// UpdateShiftHandler 指定週のシフト表内のシフトを1件更新 (マネージャー専用)
func (h *ShiftTableHandler) UpdateShiftHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]
	shiftID := vars["shiftId"]
	if storeID == "" || weekStartDate == "" || shiftID == "" {
		respondWithError(w, "store_id, week_start_date and shift_id are required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		respondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		respondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		respondWithError(w, "You do not have permission to edit the shift table for this store", http.StatusForbidden)
		return
	}

	table, err := h.shiftTableRepo.GetShiftTable(storeID, weekStartDate)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			respondWithError(w, "Shift table not found for this week", http.StatusNotFound)
			return
		}
		respondWithError(w, "Failed to fetch shift table", http.StatusInternalServerError)
		return
	}

	updatedShift, statusCode, err := h.validateAndBuildShift(r, storeID)
	if err != nil {
		respondWithError(w, err.Error(), statusCode)
		return
	}

	if err := h.shiftTableRepo.UpdateShift(table.ID.Hex(), shiftID, updatedShift); err != nil {
		respondWithError(w, "Failed to update shift", http.StatusInternalServerError)
		return
	}

	respondWithJSON(w, map[string]string{"message": "Shift updated successfully"}, http.StatusOK)
}

// DeleteShiftHandler 指定週のシフト表内のシフトを1件削除 (マネージャー専用)
func (h *ShiftTableHandler) DeleteShiftHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]
	shiftID := vars["shiftId"]
	if storeID == "" || weekStartDate == "" || shiftID == "" {
		respondWithError(w, "store_id, week_start_date and shift_id are required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		respondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		respondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		respondWithError(w, "You do not have permission to edit the shift table for this store", http.StatusForbidden)
		return
	}

	table, err := h.shiftTableRepo.GetShiftTable(storeID, weekStartDate)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			respondWithError(w, "Shift table not found for this week", http.StatusNotFound)
			return
		}
		respondWithError(w, "Failed to fetch shift table", http.StatusInternalServerError)
		return
	}

	if err := h.shiftTableRepo.DeleteShift(table.ID.Hex(), shiftID); err != nil {
		respondWithError(w, "Failed to delete shift", http.StatusInternalServerError)
		return
	}

	respondWithJSON(w, map[string]string{"message": "Shift deleted successfully"}, http.StatusOK)
}

// AutoGenerateShiftsHandler 必要人員設定とスタッフの勤務可能時間を突き合わせ、
// シフトを自動的に割り当てる (マネージャー専用)
func (h *ShiftTableHandler) AutoGenerateShiftsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]
	if storeID == "" || weekStartDate == "" {
		respondWithError(w, "store_id and week_start_date are required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		respondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		respondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		respondWithError(w, "You do not have permission to edit the shift table for this store", http.StatusForbidden)
		return
	}

	table, err := h.shiftTableRepo.GetShiftTable(storeID, weekStartDate)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			respondWithError(w, "Shift table not found for this week. Create it first.", http.StatusNotFound)
			return
		}
		respondWithError(w, "Failed to fetch shift table", http.StatusInternalServerError)
		return
	}

	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Mode != "fill_gaps" && req.Mode != "replace_all" {
		respondWithError(w, "mode must be 'fill_gaps' or 'replace_all'", http.StatusBadRequest)
		return
	}

	settings, err := h.settingsRepo.GetSettings(storeID)
	if err != nil {
		respondWithError(w, "Failed to fetch store settings", http.StatusInternalServerError)
		return
	}

	staffList, err := h.staffRepo.GetStoreStaffByStoreID(storeID)
	if err != nil {
		respondWithError(w, "Failed to fetch staff list", http.StatusInternalServerError)
		return
	}
	candidates := shift.BuildCandidates(staffList, settings.ManagerID, settings.Settings.ExcludeManagerFromShiftTable)

	baseShifts := table.Shifts
	if req.Mode == "replace_all" {
		baseShifts = nil
	}

	// 直近 shift.FairnessLookbackWeeks 週分の実績を公平配分の基準に加えることで、
	// 「毎回同じ人ばかり優先される」偏りを抑える(取得に失敗しても自動配置自体は継続する)
	historicalCounts, err := h.shiftTableRepo.GetStaffShiftCounts(
		storeID, weekStartDate, shift.FairnessLookbackWeeks)
	if err != nil {
		log.Printf("Failed to fetch historical shift counts for store_id=%s, continuing without them: %v", storeID, err)
		historicalCounts = nil
	}

	// 同様に、直近の実績から「誰と誰が一緒に組んだか」も取得し、自動配置が
	// 同じペアばかり繰り返し組ませないようにする(取得に失敗しても自動配置自体は継続する)
	historicalPairCounts, err := h.shiftTableRepo.GetStaffPairCounts(
		storeID, weekStartDate, shift.FairnessLookbackWeeks)
	if err != nil {
		log.Printf("Failed to fetch historical pair counts for store_id=%s, continuing without them: %v", storeID, err)
		historicalPairCounts = nil
	}

	newShifts := shift.AutoAssign(baseShifts, &settings.Settings, candidates, historicalCounts, historicalPairCounts)

	if err := h.shiftTableRepo.ReplaceShifts(table.ID.Hex(), newShifts); err != nil {
		respondWithError(w, "Failed to auto-generate shifts", http.StatusInternalServerError)
		return
	}

	table.Shifts = newShifts
	// 配置しきれなかったブロック(勤務可能な候補が必要人数に満たなかった等)を
	// マネージャーに伝えるため、配置結果に対して不足分を算出して添える
	table.ShiftShortages = shift.ComputeShortages(newShifts, &settings.Settings)
	respondWithJSON(w, table, http.StatusOK)
}

// --- シフト修正依頼 ---
// スタッフが自分の割当ブロックをタップし、「現在の割当(From)→希望する割当(To)」を送る
// (スタッフ→マネージャー)。マネージャーは依頼を見ながら通常のシフト編集機能でシフト表を
// 直接修正するか、ApplyShiftChangeRequestsHandler で下書きへ一括反映する。
// どちらも下書き止まりで、確定(PublishShiftTableHandler)で初めてスタッフに反映される

// CreateShiftChangeRequestHandler シフトブロックに対する修正依頼を1件作成する (承認済みスタッフ用)
func (h *ShiftTableHandler) CreateShiftChangeRequestHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]
	if storeID == "" || weekStartDate == "" {
		respondWithError(w, "store_id and week_start_date are required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		respondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasViewAccess(user.ID, storeID)
	if err != nil {
		respondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		respondWithError(w, "You do not have permission to view the shift table for this store", http.StatusForbidden)
		return
	}

	var req struct {
		TargetShiftID string `json:"target_shift_id"`
		FromDay       string `json:"from_day"`
		FromStartTime string `json:"from_start_time"`
		FromEndTime   string `json:"from_end_time"`
		ToDay         string `json:"to_day"`
		ToStartTime   string `json:"to_start_time"`
		ToEndTime     string `json:"to_end_time"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	// From/To とも、シフト追加/編集(AddShift/UpdateShift)と同じ day/start_time/end_time
	// 形式なので、同じバリデーション関数(shift.IsValidSlot)で検証する
	if !shift.IsValidSlot(req.FromDay, req.FromStartTime, req.FromEndTime) {
		respondWithError(w, "Invalid from day/start_time/end_time", http.StatusBadRequest)
		return
	}
	if !shift.IsValidSlot(req.ToDay, req.ToStartTime, req.ToEndTime) {
		respondWithError(w, "Invalid to day/start_time/end_time", http.StatusBadRequest)
		return
	}

	changeRequest := models.ShiftChangeRequest{
		StoreID:       storeID,
		WeekStartDate: weekStartDate,
		StaffID:       user.ID,
		StaffName:     user.UserName,
		TargetShiftID: strings.TrimSpace(req.TargetShiftID),
		FromDay:       req.FromDay,
		FromStartTime: req.FromStartTime,
		FromEndTime:   req.FromEndTime,
		ToDay:         req.ToDay,
		ToStartTime:   req.ToStartTime,
		ToEndTime:     req.ToEndTime,
	}
	if err := h.changeRequestRepo.CreateRequest(changeRequest); err != nil {
		respondWithError(w, "Failed to create shift change request", http.StatusInternalServerError)
		return
	}

	respondWithJSON(w, changeRequest, http.StatusCreated)
}

// GetShiftChangeRequestsHandler 週間シフト表に対する修正依頼一覧を取得する
// (承認済みスタッフ/マネージャー共通。自分の依頼だけに絞る/全件見るかはクライアント側で判断する)
func (h *ShiftTableHandler) GetShiftChangeRequestsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]
	if storeID == "" || weekStartDate == "" {
		respondWithError(w, "store_id and week_start_date are required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		respondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasViewAccess(user.ID, storeID)
	if err != nil {
		respondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		respondWithError(w, "You do not have permission to view the shift table for this store", http.StatusForbidden)
		return
	}

	isManager, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		respondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}

	requests, err := h.changeRequestRepo.GetRequestsForWeek(storeID, weekStartDate)
	if err != nil {
		respondWithError(w, "Failed to fetch shift change requests", http.StatusInternalServerError)
		return
	}

	if !isManager {
		// applied はマネージャーが下書きに反映しただけの中間状態。スタッフのシフト表は
		// まだ変わっていないため、「対応済み」と見せると実態と食い違う。未対応に伏せて返す
		requests = shift.MaskAppliedAsPending(requests)
	}

	respondWithJSON(w, requests, http.StatusOK)
}

// DeleteShiftChangeRequestHandler 修正依頼を1件削除する (マネージャー専用)。
// 一括適用(ApplyShiftChangeRequestsHandler)で衝突により見送られ、対応不要になった
// 依頼を手動で一覧から消すために使う
func (h *ShiftTableHandler) DeleteShiftChangeRequestHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]
	requestID := vars["requestId"]
	if storeID == "" || weekStartDate == "" || requestID == "" {
		respondWithError(w, "store_id, week_start_date and request_id are required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		respondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		respondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		respondWithError(w, "You do not have permission to edit the shift table for this store", http.StatusForbidden)
		return
	}

	if err := h.changeRequestRepo.DeleteRequest(requestID); err != nil {
		respondWithError(w, "Failed to delete shift change request", http.StatusInternalServerError)
		return
	}

	respondWithJSON(w, map[string]string{"message": "Shift change request deleted successfully"}, http.StatusOK)
}

// --- 修正依頼の一括適用 ---
// 「適用してみる」ボタン1回では終わらない場合がある(衝突が見つかるたび、マネージャーの
// 判断を1件ずつ挟む必要があるため)。クライアントは、これまでの判断(resolutions)を
// 毎回全部載せて何度も同じエンドポイントを呼び直すウィザード方式にする。各呼び出しは
// (DB状態 + resolutions)だけで完結するステートレスな処理: 判断済みの依頼はそのまま
// 反映し、まだ判断のない衝突に当たった時点で処理を止めてその1件をクライアントに返す。
// 衝突に当たる前に反映できた分は、その呼び出しの中で確定コミットする(ウィザードを
// 途中でやめても、既に答えた分は失われない)

// changeRequestApplyResult ApplyShiftChangeRequestsHandler のレスポンス。
// conflict が nil なら今回の呼び出しで全て処理完了(done=true)
type changeRequestApplyResult struct {
	AppliedCount int `json:"applied_count"`
	// SupersededCount 本人が同じ枠へ出し直したため、古い方を解決済みにした件数
	SupersededCount int `json:"superseded_count"`
	// SkippedStaleCount 依頼元のシフトが既に存在せず適用できなかったため、解決済みにした件数
	// (「既に処理済み」とは限らない。マネージャーがそのシフトを削除・変更した場合も含む)
	SkippedStaleCount int             `json:"skipped_stale_count"`
	Done              bool            `json:"done"`
	Conflict          *shift.Conflict `json:"conflict,omitempty"`
}

// ApplyShiftChangeRequestsHandler その週の未処理(pending)な修正依頼を、作成日時が
// 古い順に実際のシフト表へ反映していく (マネージャー専用)。衝突(上記3種類)に当たると
// 途中で止まり、その1件をレスポンスで返す。クライアントはマネージャーの判断を
// resolutions に足して同じエンドポイントを呼び直す
func (h *ShiftTableHandler) ApplyShiftChangeRequestsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]
	if storeID == "" || weekStartDate == "" {
		respondWithError(w, "store_id and week_start_date are required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		respondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		respondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		respondWithError(w, "You do not have permission to edit the shift table for this store", http.StatusForbidden)
		return
	}

	var req struct {
		Resolutions []shift.Resolution `json:"resolutions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	table, err := h.shiftTableRepo.GetShiftTable(storeID, weekStartDate)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			respondWithError(w, "Shift table not found for this week", http.StatusNotFound)
			return
		}
		respondWithError(w, "Failed to fetch shift table", http.StatusInternalServerError)
		return
	}

	allRequests, err := h.changeRequestRepo.GetRequestsForWeek(storeID, weekStartDate)
	if err != nil {
		respondWithError(w, "Failed to fetch shift change requests", http.StatusInternalServerError)
		return
	}
	pending := make([]models.ShiftChangeRequest, 0, len(allRequests))
	for _, cr := range allRequests {
		if cr.Status == models.ShiftChangeRequestStatusPending {
			pending = append(pending, cr)
		}
	}
	// GetRequestsForWeek は新しい順なので、古い順(先着順)に処理するために反転する
	sort.SliceStable(pending, func(i, j int) bool {
		return pending[i].CreatedAt.Before(pending[j].CreatedAt)
	})

	settings, err := h.settingsRepo.GetSettings(storeID)
	if err != nil {
		respondWithError(w, "Failed to fetch store settings", http.StatusInternalServerError)
		return
	}

	staffList, err := h.staffRepo.GetStoreStaffByStoreID(storeID)
	if err != nil {
		respondWithError(w, "Failed to fetch staff list", http.StatusInternalServerError)
		return
	}
	managerName := ""
	if managerObjID, err := primitive.ObjectIDFromHex(settings.ManagerID); err == nil {
		if manager, err := h.userRepo.GetUserData(managerObjID); err == nil && manager != nil {
			managerName = manager.UserName
		}
	}
	result := shift.ApplyChangeRequests(table.Shifts, pending, req.Resolutions, &settings.Settings, staffList, settings.ManagerID, managerName)

	// 衝突で止まった場合でも、そこまでに確定した分は必ずコミットする
	// (ウィザードを途中でやめても、既に答えた分が失われないようにするため)
	// ただし反映先はあくまで下書き。スタッフに見える確定版へは「確定」を押すまで反映されない
	if len(result.Applied) > 0 {
		if err := h.shiftTableRepo.ReplaceShifts(table.ID.Hex(), result.Shifts); err != nil {
			respondWithError(w, "Failed to apply shift changes", http.StatusInternalServerError)
			return
		}
		for id := range result.Applied {
			if err := h.changeRequestRepo.MarkRequestApplied(id); err != nil {
				log.Printf("Failed to mark shift change request %s as applied: %v", id, err)
			}
		}
	}

	// 適用されなかったが、そのままにしておくと永久に未対応として残り続ける依頼を片付ける
	// - superseded: 本人が出し直したため、古い方はもう本人の意思ではない
	// - stale: 依頼元のシフトが既に無く、この先も適用されることはない
	// 衝突で中断した場合、まだ判定されていない依頼が残っているため stale は確定扱いにしない
	// これらもマネージャーの判断なので applied 止まりにし、確定と同時にスタッフへ見せる
	resolvedAsObsolete := 0
	for id := range result.Superseded {
		if err := h.changeRequestRepo.MarkRequestApplied(id); err != nil {
			log.Printf("Failed to mark superseded shift change request %s as applied: %v", id, err)
			continue
		}
		resolvedAsObsolete++
	}
	staleResolved := 0
	if result.Conflict == nil {
		for id := range result.Stale {
			if err := h.changeRequestRepo.MarkRequestApplied(id); err != nil {
				log.Printf("Failed to mark stale shift change request %s as applied: %v", id, err)
				continue
			}
			staleResolved++
		}
	}

	respondWithJSON(w, changeRequestApplyResult{
		AppliedCount:      len(result.Applied),
		SupersededCount:   resolvedAsObsolete,
		SkippedStaleCount: staleResolved,
		Done:              result.Conflict == nil,
		Conflict:          result.Conflict,
	}, http.StatusOK)
}
