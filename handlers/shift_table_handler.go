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
	GetStaffPairCounts(storeID, beforeWeekStartDate string, lookbackWeeks int) (map[string]int, error)
}

// ShiftChangeRequestRepository 週間シフト表に対する修正依頼の操作を抽象化するインターフェース
type ShiftChangeRequestRepository interface {
	CreateRequest(req models.ShiftChangeRequest) error
	GetRequestsForWeek(storeID, weekStartDate string) ([]models.ShiftChangeRequest, error)
	ResolvePendingForWeek(storeID, weekStartDate string) ([]models.ShiftChangeRequest, error)
	ResolveRequest(requestID string) error
	DeleteRequest(requestID string) error
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
	if settings.Settings.ExcludeManagerFromShiftTable {
		candidates = removeManagerCandidates(candidates)
	} else {
		candidates = appendManagerCandidateIfMissing(candidates, settings.ManagerID)
	}

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

	// 同様に、直近の実績から「誰と誰が一緒に組んだか」も取得し、自動配置が
	// 同じペアばかり繰り返し組ませないようにする(取得に失敗しても自動配置自体は継続する)
	historicalPairCounts, err := h.shiftTableRepo.GetStaffPairCounts(
		storeID, weekStartDate, shiftFairnessLookbackWeeks)
	if err != nil {
		log.Printf("Failed to fetch historical pair counts for store_id=%s, continuing without them: %v", storeID, err)
		historicalPairCounts = nil
	}

	newShifts := autoAssignShifts(baseShifts, &settings.Settings, candidates, historicalCounts, historicalPairCounts)

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

// DeleteShiftChangeRequestHandler 修正依頼を1件削除する (マネージャー専用)。
// 一括適用(ApplyShiftChangeRequestsHandler)で衝突により見送られ、対応不要になった
// 依頼を手動で一覧から消すために使う
func (h *ShiftTableHandler) DeleteShiftChangeRequestHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	storeID := vars["storeId"]
	weekStartDate := vars["weekStartDate"]
	requestID := vars["requestId"]
	if storeID == "" || weekStartDate == "" || requestID == "" {
		utils.RespondWithError(w, "store_id, week_start_date and request_id are required", http.StatusBadRequest)
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

	if err := h.changeRequestRepo.DeleteRequest(requestID); err != nil {
		utils.RespondWithError(w, "Failed to delete shift change request", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]string{"message": "Shift change request deleted successfully"}, http.StatusOK)
}

// --- 修正依頼の一括適用 ---
// 「適用してみる」ボタン1回では終わらない場合がある(衝突が見つかるたび、マネージャーの
// 判断を1件ずつ挟む必要があるため)。クライアントは、これまでの判断(resolutions)を
// 毎回全部載せて何度も同じエンドポイントを呼び直すウィザード方式にする。各呼び出しは
// (DB状態 + resolutions)だけで完結するステートレスな処理: 判断済みの依頼はそのまま
// 反映し、まだ判断のない衝突に当たった時点で処理を止めてその1件をクライアントに返す。
// 衝突に当たる前に反映できた分は、その呼び出しの中で確定コミットする(ウィザードを
// 途中でやめても、既に答えた分は失われない)

// 衝突1件に対してマネージャーが下す判断
const (
	changeRequestActionPrioritize     = "prioritize"      // 衝突(依頼同士)で、この依頼を優先する
	changeRequestActionSwap           = "swap"            // 衝突(定員超過)で、指定した相手と自分の枠を入れ替える
	changeRequestActionConfirmOverlap = "confirm_overlap" // 衝突(本人の時間重複)を承知の上で適用する
	changeRequestActionSkip           = "skip"            // この依頼は今回見送る(保留のまま残す)
)

// 衝突の種類
const (
	changeRequestConflictTypeRequestConflict = "request_conflict"      // 複数の依頼が同じ枠を希望
	changeRequestConflictTypeCapacity        = "capacity_conflict"     // 希望先の枠が既に定員一杯
	changeRequestConflictTypeSelfOverlap     = "self_overlap_conflict" // 依頼者本人の他のシフトと重複
)

// changeRequestResolution クライアントが衝突1件に対して下した判断
type changeRequestResolution struct {
	RequestID     string `json:"request_id"`
	Action        string `json:"action"`
	TargetStaffID string `json:"target_staff_id,omitempty"`
}

// changeRequestCandidate 衝突ダイアログに並べる選択肢1件(名前ボタン)
type changeRequestCandidate struct {
	StaffID   string `json:"staff_id"`
	StaffName string `json:"staff_name"`
	// RequestID 依頼同士の衝突(request_conflict)で、この候補自身の依頼IDを表す。
	// 定員超過(capacity)の候補(単なる既存シフトの担当者)には無い
	RequestID string `json:"request_id,omitempty"`
}

// changeRequestConflict マネージャーの判断待ちの衝突1件
type changeRequestConflict struct {
	Type        string                   `json:"type"`
	RequestID   string                   `json:"request_id"`
	StaffName   string                   `json:"staff_name"`
	ToDay       string                   `json:"to_day"`
	ToStartTime string                   `json:"to_start_time"`
	ToEndTime   string                   `json:"to_end_time"`
	Candidates  []changeRequestCandidate `json:"candidates,omitempty"`
}

// changeRequestApplyResult ApplyShiftChangeRequestsHandler のレスポンス。
// conflict が nil なら今回の呼び出しで全て処理完了(done=true)
type changeRequestApplyResult struct {
	AppliedCount      int                    `json:"applied_count"`
	SkippedStaleCount int                    `json:"skipped_stale_count"`
	Done              bool                   `json:"done"`
	Conflict          *changeRequestConflict `json:"conflict,omitempty"`
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

	var req struct {
		Resolutions []changeRequestResolution `json:"resolutions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	resolutionByRequestID := make(map[string]changeRequestResolution, len(req.Resolutions))
	for _, res := range req.Resolutions {
		resolutionByRequestID[res.RequestID] = res
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

	allRequests, err := h.changeRequestRepo.GetRequestsForWeek(storeID, weekStartDate)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch shift change requests", http.StatusInternalServerError)
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
		utils.RespondWithError(w, "Failed to fetch store settings", http.StatusInternalServerError)
		return
	}

	staffList, err := h.staffRepo.GetStoreStaffByStoreID(storeID)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch staff list", http.StatusInternalServerError)
		return
	}
	managerName := ""
	if managerObjID, err := primitive.ObjectIDFromHex(settings.ManagerID); err == nil {
		if manager, err := h.userRepo.GetUserData(managerObjID); err == nil && manager != nil {
			managerName = manager.UserName
		}
	}
	staffNames := resolveStaffNames(staffList, settings.ManagerID, managerName)

	shifts := append([]models.Shift{}, table.Shifts...)
	appliedRequestIDs := map[string]bool{}
	skippedThisRun := map[string]bool{}
	staleCount := 0
	var conflict *changeRequestConflict

requestLoop:
	for _, cr := range pending {
		reqIDHex := cr.ID.Hex()
		if skippedThisRun[reqIDHex] || appliedRequestIDs[reqIDHex] {
			continue
		}

		// 依頼元のシフトがまだそのまま存在するか (スタッフ・曜日・時刻が完全一致するか)。
		// target_shift_id はあくまで参考情報のため一致判定には使わない
		originalIdx := findShiftIndex(shifts, cr.StaffID, cr.FromDay, cr.FromStartTime, cr.FromEndTime)
		if originalIdx == -1 {
			staleCount++
			continue
		}

		resolution, hasResolution := resolutionByRequestID[reqIDHex]
		if hasResolution && resolution.Action == changeRequestActionSkip {
			continue
		}

		// --- 衝突①: 他のまだ未処理の依頼と希望先が重なっていないか ---
		competitors := findCompetingRequests(pending, cr, skippedThisRun, appliedRequestIDs)
		if len(competitors) > 0 {
			if !hasResolution || resolution.Action != changeRequestActionPrioritize {
				candidates := []changeRequestCandidate{
					{StaffID: cr.StaffID.Hex(), StaffName: cr.StaffName, RequestID: reqIDHex},
				}
				for _, c := range competitors {
					candidates = append(candidates, changeRequestCandidate{
						StaffID: c.StaffID.Hex(), StaffName: c.StaffName, RequestID: c.ID.Hex(),
					})
				}
				conflict = &changeRequestConflict{
					Type: changeRequestConflictTypeRequestConflict, RequestID: reqIDHex, StaffName: cr.StaffName,
					ToDay: cr.ToDay, ToStartTime: cr.ToStartTime, ToEndTime: cr.ToEndTime,
					Candidates: candidates,
				}
				break requestLoop
			}
			// 優先する人を決定 -> 選ばれなかった全員(自分自身も含めうる)は今回見送る
			for _, c := range append([]models.ShiftChangeRequest{cr}, competitors...) {
				if c.StaffID.Hex() != resolution.TargetStaffID {
					skippedThisRun[c.ID.Hex()] = true
				}
			}
			if cr.StaffID.Hex() != resolution.TargetStaffID {
				// この依頼は敗れた側 -> 次の依頼へ(勝者は別途この後の周回で処理される)
				continue
			}
		}

		// --- 衝突②: 希望先の枠が既に必要人数分埋まっていないか ---
		swapWithIdx := -1
		required := settings.Settings.RequiredStaffCount[cr.ToDay].Count
		if required > 0 {
			occupants := findOccupants(shifts, cr.ToDay, cr.ToStartTime, cr.ToEndTime, cr.StaffID)
			if len(occupants) >= required {
				if hasResolution && resolution.Action == changeRequestActionSwap {
					swapWithIdx = findShiftIndexByStaff(shifts, resolution.TargetStaffID, cr.ToDay, cr.ToStartTime, cr.ToEndTime)
					if swapWithIdx == -1 {
						// スワップ相手が(別の変更で)既にいなくなっている -> 保留のまま次へ
						continue
					}
				} else {
					candidates := make([]changeRequestCandidate, 0, len(occupants))
					for _, occ := range occupants {
						candidates = append(candidates, changeRequestCandidate{
							StaffID: occ.StaffID.Hex(), StaffName: staffNameOf(staffNames, occ.StaffID),
						})
					}
					conflict = &changeRequestConflict{
						Type: changeRequestConflictTypeCapacity, RequestID: reqIDHex, StaffName: cr.StaffName,
						ToDay: cr.ToDay, ToStartTime: cr.ToStartTime, ToEndTime: cr.ToEndTime,
						Candidates: candidates,
					}
					break requestLoop
				}
			}
		}

		// --- 衝突③: 依頼者本人の他のシフトと時間が重ならないか ---
		if hasOtherOverlappingShift(shifts, cr.StaffID, originalIdx, cr.ToDay, cr.ToStartTime, cr.ToEndTime) {
			if !hasResolution || resolution.Action != changeRequestActionConfirmOverlap {
				conflict = &changeRequestConflict{
					Type: changeRequestConflictTypeSelfOverlap, RequestID: reqIDHex, StaffName: cr.StaffName,
					ToDay: cr.ToDay, ToStartTime: cr.ToStartTime, ToEndTime: cr.ToEndTime,
				}
				break requestLoop
			}
		}

		// ここまで来たら適用確定
		shifts[originalIdx].Day = cr.ToDay
		shifts[originalIdx].StartTime = cr.ToStartTime
		shifts[originalIdx].EndTime = cr.ToEndTime
		if swapWithIdx != -1 {
			shifts[swapWithIdx].Day = cr.FromDay
			shifts[swapWithIdx].StartTime = cr.FromStartTime
			shifts[swapWithIdx].EndTime = cr.FromEndTime
		}
		appliedRequestIDs[reqIDHex] = true
	}

	// 衝突で止まった場合でも、そこまでに確定した分は必ずコミットする
	// (ウィザードを途中でやめても、既に答えた分が失われないようにするため)
	if len(appliedRequestIDs) > 0 {
		if err := h.shiftTableRepo.ReplaceShifts(table.ID.Hex(), shifts); err != nil {
			utils.RespondWithError(w, "Failed to apply shift changes", http.StatusInternalServerError)
			return
		}
		for id := range appliedRequestIDs {
			if err := h.changeRequestRepo.ResolveRequest(id); err != nil {
				log.Printf("Failed to resolve shift change request %s after applying: %v", id, err)
			}
		}
	}

	utils.RespondWithJSON(w, changeRequestApplyResult{
		AppliedCount:      len(appliedRequestIDs),
		SkippedStaleCount: staleCount,
		Done:              conflict == nil,
		Conflict:          conflict,
	}, http.StatusOK)
}

// findShiftIndex スタッフ・曜日・開始/終了時刻が完全一致するシフトを探す
// (修正依頼の元になったシフトが、まだそのまま存在するかの確認に使う)
func findShiftIndex(shifts []models.Shift, staffID primitive.ObjectID, day, startTime, endTime string) int {
	for i, s := range shifts {
		if s.StaffID == staffID && s.Day == day && s.StartTime == startTime && s.EndTime == endTime {
			return i
		}
	}
	return -1
}

// findShiftIndexByStaff 指定スタッフの、指定曜日・時間帯に重なるシフトを探す
// (定員超過の衝突で、スワップ相手として指定された人の現在のシフトを特定するのに使う)
func findShiftIndexByStaff(shifts []models.Shift, staffIDHex, day, startTime, endTime string) int {
	startMin, ok1 := parseTimeToMinutes(startTime)
	endMin, ok2 := parseTimeToMinutes(endTime)
	if !ok1 || !ok2 {
		return -1
	}
	for i, s := range shifts {
		if s.Day != day || s.StaffID.Hex() != staffIDHex {
			continue
		}
		if shiftOverlapsRange(s, startMin, endMin) {
			return i
		}
	}
	return -1
}

// findOccupants 指定曜日・時間帯に重なる、excludeStaffID以外のシフトを全て返す
// (定員超過かどうかの判定、およびスワップ候補一覧の表示に使う)
func findOccupants(shifts []models.Shift, day, startTime, endTime string, excludeStaffID primitive.ObjectID) []models.Shift {
	startMin, ok1 := parseTimeToMinutes(startTime)
	endMin, ok2 := parseTimeToMinutes(endTime)
	if !ok1 || !ok2 {
		return nil
	}
	var occupants []models.Shift
	for _, s := range shifts {
		if s.Day != day || s.StaffID == excludeStaffID {
			continue
		}
		if shiftOverlapsRange(s, startMin, endMin) {
			occupants = append(occupants, s)
		}
	}
	return occupants
}

// hasOtherOverlappingShift staffIDが、excludeIdx以外のシフトで指定曜日・時間帯と
// 重なるものを持っているかどうか(依頼者本人の二重予約チェックに使う)
func hasOtherOverlappingShift(shifts []models.Shift, staffID primitive.ObjectID, excludeIdx int, day, startTime, endTime string) bool {
	startMin, ok1 := parseTimeToMinutes(startTime)
	endMin, ok2 := parseTimeToMinutes(endTime)
	if !ok1 || !ok2 {
		return false
	}
	for i, s := range shifts {
		if i == excludeIdx || s.StaffID != staffID || s.Day != day {
			continue
		}
		if shiftOverlapsRange(s, startMin, endMin) {
			return true
		}
	}
	return false
}

// findCompetingRequests current と希望先(曜日・時間帯)が重なる、他のまだ未処理の
// pending依頼を全て探す(依頼同士の衝突判定に使う)
func findCompetingRequests(pending []models.ShiftChangeRequest, current models.ShiftChangeRequest,
	skippedThisRun, appliedThisRun map[string]bool) []models.ShiftChangeRequest {
	var competitors []models.ShiftChangeRequest
	for _, other := range pending {
		if other.ID == current.ID {
			continue
		}
		otherIDHex := other.ID.Hex()
		if skippedThisRun[otherIDHex] || appliedThisRun[otherIDHex] {
			continue
		}
		if other.ToDay != current.ToDay {
			continue
		}
		if timeRangesOverlap(current.ToStartTime, current.ToEndTime, other.ToStartTime, other.ToEndTime) {
			competitors = append(competitors, other)
		}
	}
	return competitors
}

// timeRangesOverlap 2つの [start, end) 時刻区間("HH:MM")が重なるかどうか
func timeRangesOverlap(aStart, aEnd, bStart, bEnd string) bool {
	aStartMin, ok1 := parseTimeToMinutes(aStart)
	aEndMin, ok2 := parseTimeToMinutes(aEnd)
	bStartMin, ok3 := parseTimeToMinutes(bStart)
	bEndMin, ok4 := parseTimeToMinutes(bEnd)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return false
	}
	return aStartMin < bEndMin && bStartMin < aEndMin
}

// resolveStaffNames store_staff_info一覧から、シフトのstaffId(store_staff_infoの_id)を
// 表示名に変換するマップを作る。マネージャーがstore_staff_infoを持たない(通常ケース)場合は
// managerID/managerNameをフォールバックとして補う(shift_table_view.dartのクライアント側
// ロジックと同じ考え方)
func resolveStaffNames(staffList []map[string]interface{}, managerID, managerName string) map[string]string {
	names := make(map[string]string, len(staffList)+1)
	for _, entry := range staffList {
		id, ok := entry["_id"].(primitive.ObjectID)
		if !ok {
			continue
		}
		name, _ := entry["user_name"].(string)
		if name == "" {
			name = "不明"
		}
		names[id.Hex()] = name
	}
	if managerID != "" {
		if _, exists := names[managerID]; !exists {
			if managerName != "" {
				names[managerID] = managerName
			} else {
				names[managerID] = "マネージャー"
			}
		}
	}
	return names
}

// staffNameOf names から staffID の表示名を引く(無ければ「不明」)
func staffNameOf(names map[string]string, staffID primitive.ObjectID) string {
	if name, ok := names[staffID.Hex()]; ok {
		return name
	}
	return "不明"
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

// removeManagerCandidates 設定でマネージャーがシフト自動配置から除外されている場合に、
// 候補一覧からマネージャー分(buildAutoAssignCandidatesがIsManagerを立てた既存項目も含む)を取り除く
func removeManagerCandidates(candidates []autoAssignCandidate) []autoAssignCandidate {
	filtered := make([]autoAssignCandidate, 0, len(candidates))
	for _, c := range candidates {
		if c.IsManager {
			continue
		}
		filtered = append(filtered, c)
	}
	return filtered
}

// autoAssignShifts 必要人員設定・営業時間・定休日・スタッフの勤務可能時間をもとに、
// baseShifts に不足分のシフトを自動的に追加した新しいシフト一覧を返す。
// 各曜日は営業開始〜終了時刻を (ShiftChangeCount+1)個の等しい長さのブロックに
// 均等分割し(端数は出ない)、各ブロックごとに同時に必要な人数(Count)を満たすよう配置される。
// baseShifts が空(nilまたは長さ0)の場合は最初から全て自動生成する。
// historicalCounts は直近数週間分の実績(スタッフごとの担当シフト数)。これを公平配分の
// 初期値として使うことで、その週だけで完結しない、複数週にまたがった公平配分になる
// (nilの場合は0から、つまり従来通りその週だけで公平配分する)。
// historicalPairCounts は同様に直近数週間分の「誰と誰が一緒に組んだか」の実績で、
// ペア反復ペナルティの初期値として使う(nilの場合は0から)。
//
// 各ブロックの人員選定は、単純に担当数が少ない順に選ぶのではなく、
// 必要人数分の組み合わせを全て作って点数を付け、最も点数が低い組み合わせを選ぶ
// 2段階方式(候補生成→スコアリング)にしている。点数が低いほど良い組み合わせ:
//
//	score = 担当数合計*10 + ペア反復合計*5 + 連続勤務件数*5
//
// これにより、個人ごとの公平配分(担当数)だけでなく、同じ2人ばかりが
// 繰り返し組まされること・同じ日に隙間なく連続して働かされることも抑えられる
func autoAssignShifts(baseShifts []models.Shift, settings *models.Settings, candidates []autoAssignCandidate, historicalCounts map[primitive.ObjectID]int, historicalPairCounts map[string]int) []models.Shift {
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

	// ペア反復ペナルティのため、直近実績(historicalPairCounts)を初期値に、
	// baseShifts に既に含まれるペア(同じ曜日で時間帯が重なる2人)も反映しておく
	pairAssignedCount := make(map[string]int, len(historicalPairCounts))
	for key, count := range historicalPairCounts {
		pairAssignedCount[key] = count
	}
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i].Day != result[j].Day || result[i].StaffID == result[j].StaffID {
				continue
			}
			if shiftsOverlap(result[i], result[j]) {
				pairAssignedCount[pairKey(result[i].StaffID, result[j].StaffID)]++
			}
		}
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

			if need > len(eligible) {
				// 勤務可能な候補が必要人数に満たない場合は、いる分だけ配置する
				need = len(eligible)
			}
			if need <= 0 {
				continue
			}

			startTime := formatMinutesToTime(blockStart)
			endTime := formatMinutesToTime(blockEnd)

			// 必要人数分の組み合わせを全て作り、点数が最も低い(=良い)組み合わせを選ぶ。
			// 組み合わせ数が極端に多くなる(スタッフが非常に多い等)場合の安全策として、
			// その場合だけ従来通り「担当数が少ない順」の単純な方式にフォールバックする
			var chosen []autoAssignCandidate
			if combinationCountExceeds(len(eligible), need, maxAutoAssignCombinations) {
				sorted := make([]autoAssignCandidate, len(eligible))
				copy(sorted, eligible)
				sort.SliceStable(sorted, func(i, j int) bool {
					ci, cj := sorted[i], sorted[j]
					if assignedCount[ci.ID] != assignedCount[cj.ID] {
						return assignedCount[ci.ID] < assignedCount[cj.ID]
					}
					return ci.ID.Hex() < cj.ID.Hex()
				})
				chosen = sorted[:need]
			} else {
				var bestScore int
				var bestKey string
				for idx, combo := range generateCombinations(eligible, need) {
					score := scoreCombination(combo, day, blockStart, blockEnd, assignedCount, pairAssignedCount, result)
					key := comboKey(combo)
					if idx == 0 || score < bestScore || (score == bestScore && key < bestKey) {
						chosen = combo
						bestScore = score
						bestKey = key
					}
				}
			}

			for i := 0; i < len(chosen); i++ {
				for j := i + 1; j < len(chosen); j++ {
					pairAssignedCount[pairKey(chosen[i].ID, chosen[j].ID)]++
				}
			}

			for _, candidate := range chosen {
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

// maxAutoAssignCombinations 1ブロックあたりに全生成してよい組み合わせ数の上限。
// スタッフ数が非常に多い店舗での組み合わせ爆発を防ぐための安全策で、これを超える
// 場合だけ組み合わせ生成をやめ、以前と同じ「担当数が少ない順」の単純な方式にフォールバックする。
// 実際の店舗規模(スタッフ数十名程度まで)では通常発動しない
const maxAutoAssignCombinations = 20000

// combinationCountExceeds nCk(nからkを選ぶ組み合わせの数)が cap を超えるかどうかを、
// 大きな数への桁あふれを避けながら判定する(計算途中でcapを超えた時点で打ち切る)
func combinationCountExceeds(n, k, cap int) bool {
	if k <= 0 || k > n {
		return false
	}
	count := 1
	for i := 0; i < k; i++ {
		count = count * (n - i) / (i + 1)
		if count > cap {
			return true
		}
	}
	return false
}

// generateCombinations items から大きさ k の組み合わせを全て生成する(辞書順)
func generateCombinations(items []autoAssignCandidate, k int) [][]autoAssignCandidate {
	n := len(items)
	if k <= 0 || k > n {
		return nil
	}

	indices := make([]int, k)
	for i := range indices {
		indices[i] = i
	}

	var combos [][]autoAssignCandidate
	for {
		combo := make([]autoAssignCandidate, k)
		for i, idx := range indices {
			combo[i] = items[idx]
		}
		combos = append(combos, combo)

		// 次の組み合わせのインデックス列へ進める。末尾から、まだ動かせる
		// (n-k個の余地がある)桁を探して+1し、その右側を詰め直す
		i := k - 1
		for i >= 0 && indices[i] == i+n-k {
			i--
		}
		if i < 0 {
			break
		}
		indices[i]++
		for j := i + 1; j < k; j++ {
			indices[j] = indices[j-1] + 1
		}
	}
	return combos
}

// scoreCombination 組み合わせ1件分の点数を計算する。低いほど良い組み合わせ:
// 担当数合計(個人の公平配分) + ペア反復合計(同じ2人の組み合わせを避ける) +
// 連続勤務件数(同じ日に隙間なく連続して働く負担を避ける)の加重合計
func scoreCombination(combo []autoAssignCandidate, day string, blockStart, blockEnd int,
	assignedCount map[primitive.ObjectID]int, pairAssignedCount map[string]int, result []models.Shift) int {
	workloadImbalance := 0
	for _, c := range combo {
		workloadImbalance += assignedCount[c.ID]
	}

	pairRepeat := 0
	for i := 0; i < len(combo); i++ {
		for j := i + 1; j < len(combo); j++ {
			pairRepeat += pairAssignedCount[pairKey(combo[i].ID, combo[j].ID)]
		}
	}

	consecutiveBurden := 0
	for _, c := range combo {
		if hasAdjacentShift(result, c.ID, day, blockStart, blockEnd) {
			consecutiveBurden++
		}
	}

	return workloadImbalance*10 + pairRepeat*5 + consecutiveBurden*5
}

// hasAdjacentShift staffID が同じ曜日に、このブロックと隙間なく隣接するシフト
// (終了時刻がこのブロックの開始時刻と一致、または開始時刻がこのブロックの終了時刻と一致)
// を既に持っているかどうか。連続勤務の負担を表す簡易指標として使う
func hasAdjacentShift(shifts []models.Shift, staffID primitive.ObjectID, day string, blockStart, blockEnd int) bool {
	for _, s := range shifts {
		if s.StaffID != staffID || s.Day != day {
			continue
		}
		sMin, ok1 := parseTimeToMinutes(s.StartTime)
		eMin, ok2 := parseTimeToMinutes(s.EndTime)
		if !ok1 || !ok2 {
			continue
		}
		if eMin == blockStart || sMin == blockEnd {
			return true
		}
	}
	return false
}

// shiftsOverlap 2つのシフトの [start_time, end_time) が重なるかどうか
func shiftsOverlap(a, b models.Shift) bool {
	aStart, ok1 := parseTimeToMinutes(a.StartTime)
	aEnd, ok2 := parseTimeToMinutes(a.EndTime)
	bStart, ok3 := parseTimeToMinutes(b.StartTime)
	bEnd, ok4 := parseTimeToMinutes(b.EndTime)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return false
	}
	return aStart < bEnd && bStart < aEnd
}

// pairKey 2人分のIDから、順序に依存しない一意なマップキーを作る("aHex|bHex"、必ず
// 小さい方が先)。data/shift_table_repo.go の同名関数と全く同じフォーマットにする必要がある
// (過去実績からの初期値と、この関数内で使う実行中の累積値のキーを一致させるため)
func pairKey(a, b primitive.ObjectID) string {
	aHex, bHex := a.Hex(), b.Hex()
	if aHex > bHex {
		aHex, bHex = bHex, aHex
	}
	return aHex + "|" + bHex
}

// comboKey 組み合わせの同点タイブレーク用に、メンバーIDを昇順に並べて連結した
// 文字列を作る(実行のたびに同じ入力なら同じ結果になる決定論的な選択にするため)
func comboKey(combo []autoAssignCandidate) string {
	ids := make([]string, len(combo))
	for i, c := range combo {
		ids[i] = c.ID.Hex()
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}
