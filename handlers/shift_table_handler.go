package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

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
	GetStaffShiftCounts(storeID, beforeWeekStartDate string, lookbackWeeks int) (map[primitive.ObjectID]int, error)
}

// ShiftChangeRequestRepository 週間シフト表に対する修正依頼の操作を抽象化するインターフェース
type ShiftChangeRequestRepository interface {
	CreateRequest(req models.ShiftChangeRequest) error
	GetRequestsForWeek(storeID, weekStartDate string) ([]models.ShiftChangeRequest, error)
	ResolvePendingForWeek(storeID, weekStartDate string) ([]models.ShiftChangeRequest, error)
}

// shiftFairnessLookbackWeeks 自動配置の公平配分で過去実績を遡って参照する週数。
// これが無いと「同数なら常にID順」で毎回・毎週同じ人が優先されてしまうため、
// 直近の実績を加味することで長期的な偏りを抑える
const shiftFairnessLookbackWeeks = 12

// validShiftDays シフトの曜日として許容される値 (スタッフの勤務可能日と同じキー体系)
var validShiftDays = map[string]bool{
	"monday":    true,
	"tuesday":   true,
	"wednesday": true,
	"thursday":  true,
	"friday":    true,
	"saturday":  true,
	"sunday":    true,
}

// timeFormatPattern "HH:MM" (00:00〜23:59) 形式の検証用正規表現
var timeFormatPattern = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)$`)

// isValidWeekStartDate 週開始日が "YYYY-MM-DD" 形式かつ実際の月曜日かどうかを検証
func isValidWeekStartDate(weekStartDate string) bool {
	parsed, err := time.Parse("2006-01-02", weekStartDate)
	if err != nil {
		return false
	}
	return parsed.Weekday() == time.Monday
}

// validateShiftRequest シフトの曜日・開始/終了時刻の妥当性を検証
func validateShiftRequest(day, startTime, endTime string) bool {
	if !validShiftDays[day] {
		return false
	}
	if !timeFormatPattern.MatchString(startTime) || !timeFormatPattern.MatchString(endTime) {
		return false
	}
	return startTime < endTime
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
		utils.RespondWithError(w, "store_id and week_start_date are required", http.StatusBadRequest)
		return
	}
	if !isValidWeekStartDate(weekStartDate) {
		utils.RespondWithError(w, "week_start_date must be a valid Monday date (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		utils.RespondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasViewAccess(user.ID, storeID)
	if err != nil {
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		utils.RespondWithError(w, "You do not have permission to view the shift table for this store", http.StatusForbidden)
		return
	}

	table, err := h.shiftTableRepo.GetShiftTable(storeID, weekStartDate)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// この週にはまだシフト表が作成されていない
			utils.RespondWithError(w, "Shift table not found for this week", http.StatusNotFound)
			return
		}
		utils.RespondWithError(w, "Failed to fetch shift table", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, table, http.StatusOK)
}

// CreateShiftTableHandler 指定週の空のシフト表を作成 (マネージャー専用)
func (h *ShiftTableHandler) CreateShiftTableHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	if storeID == "" {
		utils.RespondWithError(w, "store_id is required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		utils.RespondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		utils.RespondWithError(w, "You do not have permission to create a shift table for this store", http.StatusForbidden)
		return
	}

	var req struct {
		WeekStartDate string `json:"week_start_date"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if !isValidWeekStartDate(req.WeekStartDate) {
		utils.RespondWithError(w, "week_start_date must be a valid Monday date (YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	// 既に同じ週のシフト表が存在するか確認
	_, err = h.shiftTableRepo.GetShiftTable(storeID, req.WeekStartDate)
	if err == nil {
		utils.RespondWithError(w, "Shift table already exists for this week", http.StatusConflict)
		return
	}
	if err != mongo.ErrNoDocuments {
		utils.RespondWithError(w, "Failed to check existing shift table", http.StatusInternalServerError)
		return
	}

	newTable := models.ShiftTable{
		ID:            primitive.NewObjectID(),
		StoreID:       storeID,
		WeekStartDate: req.WeekStartDate,
	}
	if err := h.shiftTableRepo.CreateShiftTable(newTable); err != nil {
		utils.RespondWithError(w, "Failed to create shift table", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, newTable, http.StatusCreated)
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
	if !validateShiftRequest(req.Day, req.StartTime, req.EndTime) {
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
		utils.RespondWithError(w, "store_id and week_start_date are required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		utils.RespondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		utils.RespondWithError(w, "You do not have permission to edit the shift table for this store", http.StatusForbidden)
		return
	}

	table, err := h.shiftTableRepo.GetShiftTable(storeID, weekStartDate)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			utils.RespondWithError(w, "Shift table not found for this week. Create it first.", http.StatusNotFound)
			return
		}
		utils.RespondWithError(w, "Failed to fetch shift table", http.StatusInternalServerError)
		return
	}

	shift, statusCode, err := h.validateAndBuildShift(r, storeID)
	if err != nil {
		utils.RespondWithError(w, err.Error(), statusCode)
		return
	}

	if err := h.shiftTableRepo.AddShift(table.ID.Hex(), shift); err != nil {
		utils.RespondWithError(w, "Failed to add shift", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, shift, http.StatusCreated)
}

// UpdateShiftHandler 指定週のシフト表内のシフトを1件更新 (マネージャー専用)
func (h *ShiftTableHandler) UpdateShiftHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]
	shiftID := vars["shiftId"]
	if storeID == "" || weekStartDate == "" || shiftID == "" {
		utils.RespondWithError(w, "store_id, week_start_date and shift_id are required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		utils.RespondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		utils.RespondWithError(w, "You do not have permission to edit the shift table for this store", http.StatusForbidden)
		return
	}

	table, err := h.shiftTableRepo.GetShiftTable(storeID, weekStartDate)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			utils.RespondWithError(w, "Shift table not found for this week", http.StatusNotFound)
			return
		}
		utils.RespondWithError(w, "Failed to fetch shift table", http.StatusInternalServerError)
		return
	}

	shift, statusCode, err := h.validateAndBuildShift(r, storeID)
	if err != nil {
		utils.RespondWithError(w, err.Error(), statusCode)
		return
	}

	if err := h.shiftTableRepo.UpdateShift(table.ID.Hex(), shiftID, shift); err != nil {
		utils.RespondWithError(w, "Failed to update shift", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]string{"message": "Shift updated successfully"}, http.StatusOK)
}

// DeleteShiftHandler 指定週のシフト表内のシフトを1件削除 (マネージャー専用)
func (h *ShiftTableHandler) DeleteShiftHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]
	shiftID := vars["shiftId"]
	if storeID == "" || weekStartDate == "" || shiftID == "" {
		utils.RespondWithError(w, "store_id, week_start_date and shift_id are required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		utils.RespondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		utils.RespondWithError(w, "You do not have permission to edit the shift table for this store", http.StatusForbidden)
		return
	}

	table, err := h.shiftTableRepo.GetShiftTable(storeID, weekStartDate)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			utils.RespondWithError(w, "Shift table not found for this week", http.StatusNotFound)
			return
		}
		utils.RespondWithError(w, "Failed to fetch shift table", http.StatusInternalServerError)
		return
	}

	if err := h.shiftTableRepo.DeleteShift(table.ID.Hex(), shiftID); err != nil {
		utils.RespondWithError(w, "Failed to delete shift", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]string{"message": "Shift deleted successfully"}, http.StatusOK)
}

// AutoGenerateShiftsHandler 必要人員設定とスタッフの勤務可能時間を突き合わせ、
// シフトを自動的に割り当てる (マネージャー専用)
func (h *ShiftTableHandler) AutoGenerateShiftsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]
	if storeID == "" || weekStartDate == "" {
		utils.RespondWithError(w, "store_id and week_start_date are required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		utils.RespondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		utils.RespondWithError(w, "You do not have permission to edit the shift table for this store", http.StatusForbidden)
		return
	}

	table, err := h.shiftTableRepo.GetShiftTable(storeID, weekStartDate)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			utils.RespondWithError(w, "Shift table not found for this week. Create it first.", http.StatusNotFound)
			return
		}
		utils.RespondWithError(w, "Failed to fetch shift table", http.StatusInternalServerError)
		return
	}

	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Mode != "fill_gaps" && req.Mode != "replace_all" {
		utils.RespondWithError(w, "mode must be 'fill_gaps' or 'replace_all'", http.StatusBadRequest)
		return
	}

	settings, err := h.settingsRepo.GetSettings(storeID)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch store settings", http.StatusInternalServerError)
		return
	}

	staffList, err := h.staffRepo.GetStoreStaffByStoreID(storeID)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch staff list", http.StatusInternalServerError)
		return
	}
	candidates := buildAutoAssignCandidates(staffList, settings.ManagerID)
	candidates = appendManagerCandidateIfMissing(candidates, settings.ManagerID)

	baseShifts := table.Shifts
	if req.Mode == "replace_all" {
		baseShifts = nil
	}

	// 直近 shiftFairnessLookbackWeeks 週分の実績を公平配分の基準に加えることで、
	// 「毎回同じ人ばかり優先される」偏りを抑える(取得に失敗しても自動配置自体は継続する)
	historicalCounts, err := h.shiftTableRepo.GetStaffShiftCounts(
		storeID, weekStartDate, shiftFairnessLookbackWeeks)
	if err != nil {
		log.Printf("Failed to fetch historical shift counts for store_id=%s, continuing without them: %v", storeID, err)
		historicalCounts = nil
	}

	newShifts := autoAssignShifts(baseShifts, &settings.Settings, candidates, historicalCounts)

	if err := h.shiftTableRepo.ReplaceShifts(table.ID.Hex(), newShifts); err != nil {
		utils.RespondWithError(w, "Failed to auto-generate shifts", http.StatusInternalServerError)
		return
	}

	table.Shifts = newShifts
	utils.RespondWithJSON(w, table, http.StatusOK)
}

// --- シフト修正依頼 ---
// スタッフが自分の割当ブロックをタップし、「現在の割当(From)→希望する割当(To)」を送る
// (スタッフ→マネージャー)。マネージャーは依頼を見ながら通常のシフト編集機能でシフト表を
// 直接修正し、対応が済んだら ResolveShiftChangeRequestsHandler でまとめて処理済みにする

// CreateShiftChangeRequestHandler シフトブロックに対する修正依頼を1件作成する (承認済みスタッフ用)
func (h *ShiftTableHandler) CreateShiftChangeRequestHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]
	if storeID == "" || weekStartDate == "" {
		utils.RespondWithError(w, "store_id and week_start_date are required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		utils.RespondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasViewAccess(user.ID, storeID)
	if err != nil {
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		utils.RespondWithError(w, "You do not have permission to view the shift table for this store", http.StatusForbidden)
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
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	// From/To とも、シフト追加/編集(AddShift/UpdateShift)と同じ day/start_time/end_time
	// 形式なので、同じバリデーション関数(validateShiftRequest)で検証する
	if !validateShiftRequest(req.FromDay, req.FromStartTime, req.FromEndTime) {
		utils.RespondWithError(w, "Invalid from day/start_time/end_time", http.StatusBadRequest)
		return
	}
	if !validateShiftRequest(req.ToDay, req.ToStartTime, req.ToEndTime) {
		utils.RespondWithError(w, "Invalid to day/start_time/end_time", http.StatusBadRequest)
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
		utils.RespondWithError(w, "Failed to create shift change request", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, changeRequest, http.StatusCreated)
}

// GetShiftChangeRequestsHandler 週間シフト表に対する修正依頼一覧を取得する
// (承認済みスタッフ/マネージャー共通。自分の依頼だけに絞る/全件見るかはクライアント側で判断する)
func (h *ShiftTableHandler) GetShiftChangeRequestsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]
	if storeID == "" || weekStartDate == "" {
		utils.RespondWithError(w, "store_id and week_start_date are required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		utils.RespondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasViewAccess(user.ID, storeID)
	if err != nil {
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		utils.RespondWithError(w, "You do not have permission to view the shift table for this store", http.StatusForbidden)
		return
	}

	requests, err := h.changeRequestRepo.GetRequestsForWeek(storeID, weekStartDate)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch shift change requests", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, requests, http.StatusOK)
}

// ResolveShiftChangeRequestsHandler その週の未処理(pending)な修正依頼を全てまとめて
// 処理済み(resolved)にする (マネージャー専用。編集のたびではなく、対応が一段落した時に押す想定)
func (h *ShiftTableHandler) ResolveShiftChangeRequestsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]
	if storeID == "" || weekStartDate == "" {
		utils.RespondWithError(w, "store_id and week_start_date are required", http.StatusBadRequest)
		return
	}

	user, statusCode, err := h.authenticate(r)
	if err != nil {
		utils.RespondWithError(w, err.Error(), statusCode)
		return
	}

	hasAccess, err := h.hasManageAccess(user.ID, storeID)
	if err != nil {
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		utils.RespondWithError(w, "You do not have permission to edit the shift table for this store", http.StatusForbidden)
		return
	}

	resolved, err := h.changeRequestRepo.ResolvePendingForWeek(storeID, weekStartDate)
	if err != nil {
		utils.RespondWithError(w, "Failed to resolve shift change requests", http.StatusInternalServerError)
		return
	}

	// TODO: プッシュ通知インフラ整備後、ここで resolved の各 StaffID 宛に
	// 「シフト表が更新されました」通知を送る (現時点はアプリ内表示のみ)

	utils.RespondWithJSON(w, resolved, http.StatusOK)
}

// --- 自動配置アルゴリズム ---

// weekdayOrder / weekdayJapaneseLabels Weekday定数の順序と、休業日判定に使う日本語ラベルの対応
var weekdayOrder = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
var weekdayJapaneseLabels = []string{"月", "火", "水", "木", "金", "土", "日"}

// weekdayJapaneseLabelOf 曜日キー(英語小文字)から休業日判定用の日本語ラベルを取得
func weekdayJapaneseLabelOf(day string) string {
	for i, d := range weekdayOrder {
		if d == day {
			return weekdayJapaneseLabels[i]
		}
	}
	return ""
}

// isClosedDay 曜日が定休日(regular_weekly)に含まれているかどうか
func isClosedDay(day string, closedDays models.ClosedDays) bool {
	label := weekdayJapaneseLabelOf(day)
	if label == "" {
		return false
	}
	for _, d := range closedDays.RegularWeekly {
		if d == label {
			return true
		}
	}
	return false
}

// parseTimeToMinutes "HH:MM" を 0〜1439 の分に変換
func parseTimeToMinutes(t string) (int, bool) {
	parts := strings.Split(t, ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, errH := strconv.Atoi(parts[0])
	m, errM := strconv.Atoi(parts[1])
	if errH != nil || errM != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// formatMinutesToTime 分を "HH:MM" 形式に変換
func formatMinutesToTime(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// dayStartMinutes 指定曜日の営業開始時刻(分)を算出する。24時間営業なら0時、
// 営業時間データが無い/不正な場合は 9:00 をデフォルトとする
// (自動配置で必要人員のシフト開始時刻の起点として使う)
func dayStartMinutes(day string, settings *models.Settings) int {
	if settings.Is24Hours {
		return 0
	}
	hours, exists := settings.OperatingHours[day]
	if !exists {
		return 9 * 60
	}
	startMin, ok := parseTimeToMinutes(hours.Start)
	if !ok {
		return 9 * 60
	}
	return startMin
}

// maxValidShiftMinutes シフトの終了時刻として許容される最大値 (23:59)。
// timeFormatPattern が "24:00" を許容しないため、24時間営業/日またぎの
// 閉店時刻はこの値に丸める
const maxValidShiftMinutes = 23*60 + 59

// dayEndMinutes 指定曜日の営業終了時刻(分)を算出する。自動配置で1日の
// 営業時間をShiftCount個のブロックに均等分割するための終端として使う。
// 24時間営業なら23:59(24:00はシフト時刻として表現できないため)、
// 営業時間データが無い場合は dayStartMinutes の9:00デフォルトと対になる18:00とする。
// 閉店時刻が開始時刻以前(データ不正、または日をまたぐ営業時間)の場合は、
// シフトが日をまたぐことに未対応で均等分割の基準にできないため、
// 開始時刻そのものを返して呼び出し側に当該曜日の自動配置を諦めさせる
func dayEndMinutes(day string, settings *models.Settings, startMin int) int {
	if settings.Is24Hours {
		return maxValidShiftMinutes
	}
	hours, exists := settings.OperatingHours[day]
	if !exists {
		return 18 * 60
	}
	endMin, ok := parseTimeToMinutes(hours.End)
	if !ok || endMin <= startMin {
		return startMin
	}
	return endMin
}

// shiftOverlapsRange 既存シフトが指定の分単位の時間範囲と重なっているかどうか
func shiftOverlapsRange(shift models.Shift, startMin, endMin int) bool {
	sMin, ok1 := parseTimeToMinutes(shift.StartTime)
	eMin, ok2 := parseTimeToMinutes(shift.EndTime)
	if !ok1 || !ok2 {
		return false
	}
	return sMin < endMin && eMin > startMin
}

// autoAssignCandidate 自動配置の対象となる1人分の情報 (承認済みスタッフ、またはマネージャー)
type autoAssignCandidate struct {
	ID           primitive.ObjectID
	Availability map[string][]models.UnavailableRange // 曜日 -> 勤務不可時間帯のリスト
	// マネージャーは store_staff_info を持たず勤務可能時間を設定する画面自体が無いため、
	// 曜日・時間帯を問わず常に配置対象にする
	IsManager bool
}

// isAvailable 指定曜日の [startMin, endMin) 区間が、勤務不可時間帯と重ならないかどうか
func (c autoAssignCandidate) isAvailable(day string, startMin, endMin int) bool {
	if c.IsManager {
		return true
	}
	for _, r := range c.Availability[day] {
		if r.AllDay {
			return false
		}
		rStart, ok1 := parseTimeToMinutes(r.StartTime)
		rEnd, ok2 := parseTimeToMinutes(r.EndTime)
		if !ok1 || !ok2 {
			continue
		}
		if rStart < endMin && rEnd > startMin {
			return false
		}
	}
	return true
}

// parseAvailability GetStoreStaffByStoreID が返す集計結果内の availability フィールド
// (map[string]interface{} 経由でデコードされた BSON サブドキュメント) を安全に解析する。
// ネストしたサブドキュメント/配列は primitive.M/primitive.A ではなく素の
// map[string]interface{}/[]interface{} としてデコードされるため、両方を許容する
func parseAvailability(raw interface{}) map[string][]models.UnavailableRange {
	result := map[string][]models.UnavailableRange{}

	var availMap map[string]interface{}
	switch v := raw.(type) {
	case primitive.M:
		availMap = v
	case map[string]interface{}:
		availMap = v
	default:
		return result
	}

	for day, v := range availMap {
		var arr []interface{}
		switch a := v.(type) {
		case primitive.A:
			arr = a
		case []interface{}:
			arr = a
		default:
			continue
		}
		ranges := make([]models.UnavailableRange, 0, len(arr))
		for _, item := range arr {
			var rangeMap map[string]interface{}
			switch r := item.(type) {
			case primitive.M:
				rangeMap = r
			case map[string]interface{}:
				rangeMap = r
			default:
				continue
			}
			allDay, _ := rangeMap["all_day"].(bool)
			startTime, _ := rangeMap["start_time"].(string)
			endTime, _ := rangeMap["end_time"].(string)
			ranges = append(ranges, models.UnavailableRange{
				AllDay:    allDay,
				StartTime: startTime,
				EndTime:   endTime,
			})
		}
		result[day] = ranges
	}
	return result
}

// buildAutoAssignCandidates GetStoreStaffByStoreID の結果から、承認済み(APPROVED)スタッフを抽出する。
// 項目の user_id が managerID と一致する場合(マネージャーが過去にスタッフとして参加した際の
// store_staff_info が残っているケース)は、その項目の IsManager フラグを立てて
// 勤務可能時間のチェックを不要にする (別IDで新規候補を作らないため表示名/色が一致し続ける)
func buildAutoAssignCandidates(staffList []map[string]interface{}, managerID string) []autoAssignCandidate {
	candidates := make([]autoAssignCandidate, 0, len(staffList))
	for _, entry := range staffList {
		status, _ := entry["status"].(string)
		if status != models.StaffStatusApproved {
			continue
		}
		id, ok := entry["_id"].(primitive.ObjectID)
		if !ok {
			continue
		}
		// マネージャーがスタッフとして参加した後にマネージャーになった等の経緯で
		// store_staff_info が残っているケースでは、そのまま同じ _id/氏名を使い回して
		// マネージャー扱い(勤務可能時間のチェックを不要に)する。別IDで新規候補を
		// 作ってしまうと、同一人物のシフトなのに表示名/色が一致しなくなるため避ける
		isManager := false
		if userID, ok := entry["user_id"].(primitive.ObjectID); ok && userID.Hex() == managerID {
			isManager = true
		}
		candidates = append(candidates, autoAssignCandidate{
			ID:           id,
			Availability: parseAvailability(entry["availability"]),
			IsManager:    isManager,
		})
	}
	return candidates
}

// appendManagerCandidateIfMissing 店舗のマネージャーがまだ候補に含まれていない場合のみ追加する。
// 通常マネージャーは store_staff_info を持たず GetStoreStaffByStoreID に含まれないため、
// その場合の最終手段として使う (勤務可能時間帯の設定画面が無いため常に配置可能として扱う)
func appendManagerCandidateIfMissing(candidates []autoAssignCandidate, managerID string) []autoAssignCandidate {
	for _, c := range candidates {
		if c.IsManager {
			return candidates
		}
	}
	objID, err := primitive.ObjectIDFromHex(managerID)
	if err != nil {
		return candidates
	}
	return append(candidates, autoAssignCandidate{ID: objID, IsManager: true})
}

// autoAssignShifts 必要人員設定・営業時間・定休日・スタッフの勤務可能時間をもとに、
// baseShifts に不足分のシフトを自動的に追加した新しいシフト一覧を返す。
// 各曜日は営業開始〜終了時刻を (ShiftChangeCount+1)個の等しい長さのブロックに
// 均等分割し(端数は出ない)、各ブロックごとに同時に必要な人数(Count)を満たすよう配置される。
// baseShifts が空(nilまたは長さ0)の場合は最初から全て自動生成する。
// historicalCounts は直近数週間分の実績(スタッフごとの担当シフト数)。これを公平配分の
// 初期値として使うことで、その週だけで完結しない、複数週にまたがった公平配分になる
// (nilの場合は0から、つまり従来通りその週だけで公平配分する)
func autoAssignShifts(baseShifts []models.Shift, settings *models.Settings, candidates []autoAssignCandidate, historicalCounts map[primitive.ObjectID]int) []models.Shift {
	result := make([]models.Shift, len(baseShifts))
	copy(result, baseShifts)

	// 公平な割り当てのため、直近実績(historicalCounts)を初期値に、
	// 現時点での担当シフト数をスタッフごとに集計する
	assignedCount := make(map[primitive.ObjectID]int, len(historicalCounts))
	for id, count := range historicalCounts {
		assignedCount[id] = count
	}
	for _, s := range result {
		assignedCount[s.StaffID]++
	}

	for _, day := range weekdayOrder {
		if isClosedDay(day, settings.ClosedDays) {
			continue
		}

		requirement := settings.RequiredStaffCount[day]
		required := requirement.Count
		// 交代がN回発生する場合、営業時間はN+1個のブロックに区切られる
		// (柵の杭と区間の関係と同じ。交代0回=1ブロックも有効な設定)
		blockCount := requirement.ShiftChangeCount + 1
		if required <= 0 || requirement.ShiftChangeCount < 0 {
			continue
		}

		// シフトの開始時刻は指定されないため、その曜日の営業時間全体を
		// blockCount 個のブロックに均等分割する(整数分割のため端数は出ない)
		dayStart := dayStartMinutes(day, settings)
		dayEnd := dayEndMinutes(day, settings, dayStart)
		if dayEnd <= dayStart {
			// 閉店時刻が不明/不正、または日をまたぐ営業時間(シフトの日またぎは未対応)の
			// 場合は均等分割の基準にできないため、当該曜日の自動配置はスキップする
			continue
		}
		totalMinutes := dayEnd - dayStart

		for i := 0; i < blockCount; i++ {
			// (dayStart起点で totalMinutes*i/blockCount ずつ進める形にすることで、
			// 端数が最後のブロックにまとまり、ブロック同士が隙間なく接続される)
			blockStart := dayStart + totalMinutes*i/blockCount
			blockEnd := dayStart + totalMinutes*(i+1)/blockCount
			if blockEnd <= blockStart {
				// blockCountが営業時間(分)より大きい極端な設定値に対する安全策
				continue
			}

			// 既にこの曜日・時間帯に重なっているスタッフを集計 (重複割り当て防止)
			alreadyAssigned := map[primitive.ObjectID]bool{}
			current := 0
			for _, s := range result {
				if s.Day != day {
					continue
				}
				if shiftOverlapsRange(s, blockStart, blockEnd) {
					alreadyAssigned[s.StaffID] = true
					current++
				}
			}

			need := required - current
			if need <= 0 {
				continue
			}

			// 対象曜日・時間帯に勤務可能(不可時間帯と重ならない)で、
			// まだ割り当てられていない候補を抽出
			eligible := make([]autoAssignCandidate, 0, len(candidates))
			for _, c := range candidates {
				if alreadyAssigned[c.ID] {
					continue
				}
				if c.isAvailable(day, blockStart, blockEnd) {
					eligible = append(eligible, c)
				}
			}

			// 現在の総担当シフト数が少ない人を優先(公平分配)、同数ならID順で決定論的に
			sort.SliceStable(eligible, func(i, j int) bool {
				ci, cj := eligible[i], eligible[j]
				if assignedCount[ci.ID] != assignedCount[cj.ID] {
					return assignedCount[ci.ID] < assignedCount[cj.ID]
				}
				return ci.ID.Hex() < cj.ID.Hex()
			})

			startTime := formatMinutesToTime(blockStart)
			endTime := formatMinutesToTime(blockEnd)

			for i := 0; i < need && i < len(eligible); i++ {
				candidate := eligible[i]
				result = append(result, models.Shift{
					ID:        primitive.NewObjectID(),
					StaffID:   candidate.ID,
					Day:       day,
					StartTime: startTime,
					EndTime:   endTime,
				})
				assignedCount[candidate.ID]++
			}
		}
	}

	return result
}
