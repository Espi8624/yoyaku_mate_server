package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"yoyaku_mate_server/auth"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// メール認証を完了しないまま放置されたFirebaseアカウントを、
// 同じメールアドレスでの再登録時に削除してよいとみなす猶予期間。
// 猶予期間内は、別端末・別タブで認証待ちの最中である可能性を考慮し削除しない
const unverifiedAccountGracePeriod = 30 * time.Minute

var (
	signUpEmailPattern = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	signUpPhonePattern = regexp.MustCompile(`^\d{2,3}-\d{3,4}-\d{4}$`)
)

// SignUpRepository 会員登録・店舗追加・重複チェックに必要な永続化操作を抽象化するインターフェース
type SignUpRepository interface {
	RegisterUser(user models.User, store *models.Store, settings *models.StoreSetting, joinStoreID string) (*models.User, error)
	AddStoreForManager(firebaseUID string, store *models.Store, settings *models.StoreSetting) (*models.User, error)
	StoreExists(storeID string) (bool, error)
	IsEmailInUse(email string) (bool, error)
	IsPhoneInUse(phone string) (bool, error)
}

// SignUpHandler 会員登録と、その前後(重複チェック・ログイン直後の店舗追加)を扱うハンドラ
type SignUpHandler struct {
	repo SignUpRepository
}

func NewSignUpHandler(repo SignUpRepository) *SignUpHandler {
	return &SignUpHandler{repo: repo}
}

// SignUp 会員加入処理
// /api/auth/signup [post]
func (h *SignUpHandler) SignUp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Firebase IDで、トークン検証
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		respondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	firebaseUIDFromToken, emailVerified, err := auth.VerifyIDTokenWithEmailVerified(r.Context(), idToken)
	if err != nil {
		respondWithError(w, "Invalid or expired token: "+err.Error(), http.StatusUnauthorized)
		return
	}
	// メール未認証のアカウントは登録不可
	if !emailVerified {
		respondWithError(w, "Email verification required before signup", http.StatusForbidden)
		return
	}

	var req models.SignUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// トークンで検証されたUIDと、要請本文のUIDが一致しているか確認
	if firebaseUIDFromToken != req.FirebaseUID {
		respondWithError(w, "Firebase UID mismatch between token and request body", http.StatusBadRequest)
		return
	}

	// 必須フィールド検証
	if req.FirebaseUID == "" || req.Email == "" || req.Name == "" || req.Role == "" {
		respondWithError(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// 本人情報(生年月日・住所)の必須フィールド検証 (建物名のみ任意)
	if req.Birthdate == "" || req.ZipCode == "" || req.Prefecture == "" || req.City == "" || req.Address == "" {
		respondWithError(w, "Missing required personal info fields (birthdate/address)", http.StatusBadRequest)
		return
	}

	// 生年月日の形式検証 + 満年齢が労働基準法上の最低就労年齢(満15歳)以上か確認
	birthdate, err := time.Parse("2006-01-02", req.Birthdate)
	if err != nil {
		respondWithError(w, "Invalid birthdate format (expected YYYY-MM-DD)", http.StatusBadRequest)
		return
	}
	if birthdate.After(time.Now()) {
		respondWithError(w, "Birthdate cannot be in the future", http.StatusBadRequest)
		return
	}
	if calculateAge(birthdate) < 15 {
		respondWithError(w, "Applicant must be at least 15 years old", http.StatusBadRequest)
		return
	}

	// 権限検証
	if req.Role != "manager" && req.Role != "staff" {
		respondWithError(w, "Invalid role: must be 'manager' or 'staff'", http.StatusBadRequest)
		return
	}

	// メールアドレス形式検証
	if !signUpEmailPattern.MatchString(req.Email) {
		respondWithError(w, "Invalid email format", http.StatusBadRequest)
		return
	}

	// 個人電話番号形式検証
	if !signUpPhonePattern.MatchString(req.PhoneNumber) {
		respondWithError(w, "Invalid personal phone number format (e.g., 010-1234-5678)", http.StatusBadRequest)
		return
	}

	var newStore *models.Store
	var newSettings *models.StoreSetting
	joinStoreID := ""
	switch req.Role {
	case "manager":
		// 店舗情報がある場合のみ作成 (Refactor: 任意項目化)
		if req.StoreName != nil && *req.StoreName != "" {
			if err := validateNewStoreRequest(&req); err != nil {
				respondWithError(w, err.Error(), http.StatusBadRequest)
				return
			}
			newStore, newSettings = buildNewStore(&req)
		}
	case "staff":
		// 店舗IDがある場合のみ参加 (Refactor: 任意項目化)
		joinStoreID = req.StoreID
	}

	newUser := models.User{
		ID:               primitive.NewObjectID(),
		FirebaseUID:      req.FirebaseUID,
		UserName:         req.Name,
		UserNameFurigana: req.NameFurigana,
		Email:            req.Email,
		Phone:            req.PhoneNumber,
		Role:             req.Role,
		Birthdate:        req.Birthdate,
		ZipCode:          req.ZipCode,
		Prefecture:       req.Prefecture,
		City:             req.City,
		Address:          req.Address,
		Building:         req.Building,
		TermsAgreed:      req.TermsAgreed,
		TermsAgreedAt:    parseAgreedAt(req.TermsAgreedAt),
		PrivacyAgreed:    req.PrivacyAgreed,
		PrivacyAgreedAt:  parseAgreedAt(req.PrivacyAgreedAt),
	}

	createdUser, err := h.repo.RegisterUser(newUser, newStore, newSettings, joinStoreID)
	if err != nil {
		respondSignUpError(w, err)
		return
	}

	responseData := map[string]interface{}{"user": createdUser}
	if req.Role == "manager" {
		responseData["store"] = newStore
	}
	respondWithJSON(w, responseData, http.StatusCreated)
}

// calculateAge 生年月日から満年齢を計算する(誕生日を迎えているかどうかを考慮)
func calculateAge(birthdate time.Time) int {
	now := time.Now()
	age := now.Year() - birthdate.Year()
	// 今年の誕生日がまだ来ていない場合は1歳引く
	if now.Month() < birthdate.Month() ||
		(now.Month() == birthdate.Month() && now.Day() < birthdate.Day()) {
		age--
	}
	return age
}

// parseAgreedAt 規約同意日時(RFC3339)をパースする。空・不正な値はゼロ値として扱う
func parseAgreedAt(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// StoreExists 店舗IDの存在確認
func (h *SignUpHandler) StoreExists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		respondWithError(w, "Store ID is required", http.StatusBadRequest)
		return
	}
	exists, err := h.repo.StoreExists(storeID)
	if err != nil {
		respondWithError(w, "Database error", http.StatusInternalServerError)
		return
	}
	respondWithJSON(w, map[string]bool{"exists": exists}, http.StatusOK)
}

// EmailCheck メールアドレスが新規登録に使えるかの確認
func (h *SignUpHandler) EmailCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req models.EmailCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Email == "" {
		respondWithError(w, "Email is required", http.StatusBadRequest)
		return
	}
	if !signUpEmailPattern.MatchString(req.Email) {
		respondWithError(w, "Invalid email format", http.StatusBadRequest)
		return
	}
	// 1. MongoDB check
	inUse, err := h.repo.IsEmailInUse(req.Email)
	if err != nil {
		respondWithError(w, "Database error", http.StatusInternalServerError)
		return
	}

	// 2. Firebase Auth check
	// MongoDBに存在しない場合でもFirebaseに存在する可能性があるため、チェックする
	// ただし、メール未認証のアカウントは再登録を許可する
	// (退会済みアカウントのFirebaseユーザーは退会時に既に削除済みのため、ここには残らない)
	firebaseUserRecord, firebaseErr := auth.GetUserByEmail(r.Context(), req.Email)
	firebaseExists := (firebaseErr == nil)

	// 未認証のFirebaseアカウントは再登録可能（中断された登録フローへの対応）
	if firebaseExists && firebaseUserRecord != nil && !firebaseUserRecord.EmailVerified {
		firebaseExists = false

		// 猶予期間を過ぎた未認証アカウントは実際に削除し、メールアドレスを解放する。
		// (削除しないとクライアント側のFirebase Auth createUserWithEmailAndPassword が
		//  依然として email-already-in-use で失敗し、再登録が完了しないため)
		// 猶予期間内は、別端末・別タブで認証待ちの最中である可能性を考慮して削除しない
		createdAt := time.UnixMilli(firebaseUserRecord.UserMetadata.CreationTimestamp)
		if time.Since(createdAt) > unverifiedAccountGracePeriod {
			if err := auth.DeleteUser(r.Context(), firebaseUserRecord.UID); err != nil {
				log.Printf("放置された未認証Firebaseアカウントの削除に失敗しました (uid=%s): %v", firebaseUserRecord.UID, err)
			}
		}
	}

	// DBまたはFirebaseに存在する場合、利用できない
	isUnavailable := inUse || firebaseExists

	respondWithJSON(w, map[string]bool{"available": !isUnavailable}, http.StatusOK)
}

// PhoneCheck 電話番号が新規登録に使えるかの確認
func (h *SignUpHandler) PhoneCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req models.PhoneCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.PhoneNumber == "" {
		respondWithError(w, "Phone number is required", http.StatusBadRequest)
		return
	}
	if !signUpPhonePattern.MatchString(req.PhoneNumber) {
		respondWithError(w, "Invalid phone number format (e.g., 010-1234-5678)", http.StatusBadRequest)
		return
	}
	inUse, err := h.repo.IsPhoneInUse(req.PhoneNumber)
	if err != nil {
		respondWithError(w, "Database error", http.StatusInternalServerError)
		return
	}
	respondWithJSON(w, map[string]bool{"available": !inUse}, http.StatusOK)
}

// AddNewStore ログインされているユーザーの店舗登録プロセス
func (h *SignUpHandler) AddNewStore(w http.ResponseWriter, r *http.Request) {
	// 使用者認証
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		respondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	if idToken == authHeader {
		respondWithError(w, "Invalid Authorization header format", http.StatusUnauthorized)
		return
	}
	firebaseUID, err := auth.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		respondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	// 新しい店舗情報をリクエストボディから取得
	var req models.SignUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 店舗情報有効性検証
	if req.StoreName == nil || *req.StoreName == "" {
		respondWithError(w, "store name is required", http.StatusBadRequest)
		return
	}
	if err := validateNewStoreRequest(&req); err != nil {
		respondWithError(w, err.Error(), http.StatusBadRequest)
		return
	}

	newStore, newSettings := buildNewStore(&req)
	owner, err := h.repo.AddStoreForManager(firebaseUID, newStore, newSettings)
	if err != nil {
		respondSignUpError(w, err)
		return
	}

	respondWithJSON(w, map[string]interface{}{
		"user":  owner,
		"store": newStore,
	}, http.StatusCreated)
}

// validateNewStoreRequest 店舗の新規作成に必要な項目を検証する (会員登録時・店舗追加時で共通)
//   - 以前はトランザクション内で検証しており、エラー文字列の部分一致でステータスを決めていたため、
//     店舗電話番号の形式エラー("invalid ..." が "Invalid" に一致しない)が500になっていた。
//     入力値の検証はDBに触れる前にここで済ませ、確実に400で返す
//   - StoreAddress はポインタのまま参照すると未指定時にnil参照でpanicするため、必須として検証する
func validateNewStoreRequest(req *models.SignUpRequest) error {
	if req.StoreTelNumber == nil || !signUpPhonePattern.MatchString(*req.StoreTelNumber) {
		return errors.New("invalid store phone number format (e.g., 02-123-4567)")
	}
	if req.StoreAddress == nil || *req.StoreAddress == "" {
		return errors.New("store address is required")
	}
	// 業種タグ必須検証
	if req.StoreCategory == nil || *req.StoreCategory == "" {
		return errors.New("business category is required")
	}
	if !models.IsValidStoreCategory(*req.StoreCategory) {
		return errors.New("invalid business category")
	}
	return nil
}

// buildNewStore リクエストから新規店舗と初期店舗設定を組み立てる (所有者の設定はリポジトリ側で行う)
// - 呼び出し前に validateNewStoreRequest で検証済みであること
func buildNewStore(req *models.SignUpRequest) (*models.Store, *models.StoreSetting) {
	store := &models.Store{
		ID:               primitive.NewObjectID(),
		StoreName:        *req.StoreName,
		BusinessCategory: *req.StoreCategory,
		Address:          *req.StoreAddress,
		Building:         valueOr(req.StoreBuilding, ""),
		ZipCode:          valueOr(req.StoreZipCode, ""),
		Prefecture:       valueOr(req.StorePrefecture, ""),
		City:             valueOr(req.StoreCity, ""),
		Phone:            *req.StoreTelNumber,
		StoreID:          primitive.NewObjectID().Hex(),
	}

	operatingHours := req.OperatingHours
	if len(operatingHours) == 0 {
		operatingHours = map[string]models.StoreDayHours{
			"monday":    {Start: "09:00", End: "18:00"},
			"tuesday":   {Start: "09:00", End: "18:00"},
			"wednesday": {Start: "09:00", End: "18:00"},
			"thursday":  {Start: "09:00", End: "18:00"},
			"friday":    {Start: "09:00", End: "18:00"},
			"saturday":  {Start: "09:00", End: "18:00"},
			"sunday":    {Start: "09:00", End: "18:00"},
		}
	}

	settings := &models.StoreSetting{
		ID: primitive.NewObjectID(),
		Settings: models.Settings{
			OperatingHours: operatingHours,
			ClosedDays: models.ClosedDays{
				SpecificDates: []string{}, RegularWeekly: []string{}, RegularMonthly: []string{}, HolidayClosure: true,
			},
			WaitingPolicy: models.WaitingPolicy{
				MaxWaitingCount:         valueOr(req.MaxWaitingCount, 10),
				EstimatedWaitTime:       valueOr(req.EstimatedWaitTime, 10),
				EnableMenuSelection:     valueOr(req.EnableMenuSelection, false),
				RequireOneMenuPerPerson: valueOr(req.RequireOneMenuPerPerson, false),
			},
			Is24Hours: valueOr(req.Is24Hours, false),
			ResetTime: valueOr(req.ResetTime, ""),
		},
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	return store, settings
}

// respondSignUpError リポジトリのエラーをHTTPステータスに変換して返す
func respondSignUpError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, data.ErrUserAlreadyExists), errors.Is(err, data.ErrStorePhoneExists):
		respondWithError(w, err.Error(), http.StatusConflict)
	case errors.Is(err, data.ErrUserNotFound), errors.Is(err, data.ErrStoreNotFound):
		respondWithError(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, data.ErrNotManager):
		respondWithError(w, err.Error(), http.StatusForbidden)
	default:
		respondWithError(w, "Transaction failed: "+err.Error(), http.StatusInternalServerError)
	}
}
