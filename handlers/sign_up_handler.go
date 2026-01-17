package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"yoyaku_mate_server/auth"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	DatabaseName            = "saboten_provider"
	UsersCollection         = "user_info"
	StoresCollection        = "store_info"
	StoreSettingsCollection = "store_settings"
	StoreLicenseCollection  = "store_license"
)

// 会員加入処理
// /api/auth/signup [post]
func SignUpHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Firebase IDで、トークン検証
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	firebaseUIDFromToken, err := auth.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Invalid or expired token: "+err.Error(), http.StatusUnauthorized)
		return
	}

	var req models.SignUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// トークンで検証されたUIDと、要請本文のUIDが一致しているか確認
	if firebaseUIDFromToken != req.FirebaseUID {
		utils.RespondWithError(w, "Firebase UID mismatch between token and request body", http.StatusBadRequest)
		return
	}

	// 必須フィールド検証
	if req.FirebaseUID == "" || req.Email == "" || req.Name == "" || req.Role == "" {
		utils.RespondWithError(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// 権限検証
	if req.Role != "manager" && req.Role != "staff" {
		utils.RespondWithError(w, "Invalid role: must be 'manager' or 'staff'", http.StatusBadRequest)
		return
	}

	// メールアドレス形式検証
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(req.Email) {
		utils.RespondWithError(w, "Invalid email format", http.StatusBadRequest)
		return
	}

	// 個人電話番号形式検証
	phoneRegex := regexp.MustCompile(`^\d{2,3}-\d{3,4}-\d{4}$`)
	if !phoneRegex.MatchString(req.PhoneNumber) {
		utils.RespondWithError(w, "Invalid personal phone number format (e.g., 010-1234-5678)", http.StatusBadRequest)
		return
	}

	// MongoDB Transaction スタート
	session, err := db.MongoClient.StartSession()
	if err != nil {
		utils.RespondWithError(w, "Failed to start database session", http.StatusInternalServerError)
		return
	}
	defer session.EndSession(r.Context())

	// Transaction実行
	result, err := session.WithTransaction(r.Context(), func(sessCtx mongo.SessionContext) (interface{}, error) {
		userCollection := db.GetCollection(DatabaseName, UsersCollection)
		storeCollection := db.GetCollection(DatabaseName, StoresCollection)

		// ユーザー中腹確認 (FirebaseUID, メールアドレス, 個人電話番号)
		var existingUser models.User
		err := userCollection.FindOne(sessCtx, bson.M{
			"$or": []bson.M{
				{"firebase_uid": req.FirebaseUID},
				{"email": req.Email},
				{"phone": req.PhoneNumber},
			},
		}).Decode(&existingUser)

		if err == nil {
			// 既に存在するユーザーである為、Transaction Callback
			return nil, fmt.Errorf("user with this email or phone number already exists")
		} else if err != mongo.ErrNoDocuments {
			return nil, fmt.Errorf("database error during user check")
		}

		var storeIdForUser string
		// var lineLoginUrl string
		newUserID := primitive.NewObjectID()
		var newStore *models.Store

		switch req.Role {
		case "manager":
			if req.StoreName == nil || *req.StoreName == "" {
				return nil, fmt.Errorf("store name is required for manager role")
			}
			if req.StoreTelNumber == nil || !phoneRegex.MatchString(*req.StoreTelNumber) {
				return nil, fmt.Errorf("invalid store phone number format (e.g., 02-123-4567)")
			}

			// 店舗電話番号重複検査
			count, err := storeCollection.CountDocuments(sessCtx, bson.M{"phone": *req.StoreTelNumber})
			if err != nil {
				return nil, fmt.Errorf("database error during store phone check: %w", err)
			}
			if count > 0 {
				return nil, fmt.Errorf("a store with this phone number already exists")
			}

			createdStore := models.Store{
				ID:        primitive.NewObjectID(),
				StoreName: *req.StoreName,
				Address:   *req.StoreAddress,
				Phone:     *req.StoreTelNumber,
				StoreID:   primitive.NewObjectID().Hex(),
				UserID:    newUserID,
			}

			_, err = storeCollection.InsertOne(sessCtx, createdStore)
			if err != nil {
				return nil, fmt.Errorf("failed to create store: %w", err)
			}
			newStore = &createdStore
			storeIdForUser = newStore.StoreID

			// LINE認証Token生成
			// lineToken, err := utils.GenerateSecureToken(32)
			// if err != nil {
			// 	return nil, fmt.Errorf("failed to generate security token: %w", err)
			// }

			licenseCollection := db.GetCollection(DatabaseName, StoreLicenseCollection)
			initialLicenseInfo := models.StoreLicense{
				ID:                 primitive.NewObjectID(),
				StoreID:            newStore.StoreID,
				VerificationStatus: models.StatusNotSubmitted,
				CreatedAt:          time.Now(),
				UpdatedAt:          time.Now(),
				// LineAuthToken:      lineToken,
			}
			_, err = licenseCollection.InsertOne(sessCtx, initialLicenseInfo)
			if err != nil {
				return nil, fmt.Errorf("failed to create license info: %w", err)
			}

			// 店舗設定データ生成
			storeSettingsCollection := db.GetCollection(DatabaseName, StoreSettingsCollection)
			defaultSettings := models.StoreSetting{
				ID:        primitive.NewObjectID(),
				StoreID:   newStore.StoreID,
				ManagerID: newUserID.Hex(),
				Settings: models.Settings{
					OperatingHours: map[string]models.StoreDayHours{
						"monday": {Start: "09:00", End: "18:00"}, "tuesday": {Start: "09:00", End: "18:00"},
						"wednesday": {Start: "09:00", End: "18:00"}, "thursday": {Start: "09:00", End: "18:00"},
						"friday": {Start: "09:00", End: "18:00"}, "saturday": {Start: "09:00", End: "18:00"},
						"sunday": {Start: "09:00", End: "18:00"},
					},
					ClosedDays: models.ClosedDays{
						SpecificDates: []string{}, RegularWeekly: []string{}, RegularMonthly: []string{}, HolidayClosure: true,
					},
					WaitingPolicy: models.WaitingPolicy{
						MaxWaitingCount:     utils.GetIntPointerValue(req.MaxWaitingCount, 10),
						EstimatedWaitTime:   utils.GetIntPointerValue(req.EstimatedWaitTime, 10),
						EnableMenuSelection: utils.GetBoolPointerValue(req.EnableMenuSelection, false),
					},
				},
				UpdatedAt: time.Now().UTC().Format(time.RFC3339),
			}
			_, err = storeSettingsCollection.InsertOne(sessCtx, defaultSettings)
			if err != nil {
				return nil, fmt.Errorf("failed to create default store settings: %w", err)
			}

			// LINEログインURL生成
			// lineChannelID := os.Getenv("LINE_LOGIN_CHANNEL_ID")
			// lineCallbackURL := os.Getenv("LINE_CALLBACK_URL")

			// baseURL := "https://access.line.me/oauth2/v2.1/authorize"
			// params := url.Values{}
			// params.Add("response_type", "code")
			// params.Add("client_id", lineChannelID)
			// params.Add("redirect_uri", lineCallbackURL)
			// params.Add("state", lineToken) // state価で生成したtokenを使用
			// params.Add("scope", "openid profile")
			// params.Add("bot_prompt", "aggressive")

			// lineLoginUrl = baseURL + "?" + params.Encode() // URL完成

		case "staff":
			if req.StoreID == "" {
				return nil, fmt.Errorf("store ID is required for staff role")
			}

			var existingStore models.Store
			err := storeCollection.FindOne(sessCtx, bson.M{"store_id": req.StoreID}).Decode(&existingStore)
			if err == mongo.ErrNoDocuments {
				return nil, fmt.Errorf("store with the provided ID not found")
			} else if err != nil {
				return nil, fmt.Errorf("database error while verifying store: %w", err)
			}
			storeIdForUser = existingStore.StoreID

		default:
			return nil, fmt.Errorf("invalid or unsupported user role: %s", req.Role)
		}

		// Parse TermsAgreedAt
		var termsAgreedAt time.Time
		if req.TermsAgreedAt != "" {
			parsedTime, err := time.Parse(time.RFC3339, req.TermsAgreedAt)
			if err == nil {
				termsAgreedAt = parsedTime
			}
		}

		// Parse PrivacyAgreedAt
		var privacyAgreedAt time.Time
		if req.PrivacyAgreedAt != "" {
			parsedTime, err := time.Parse(time.RFC3339, req.PrivacyAgreedAt)
			if err == nil {
				privacyAgreedAt = parsedTime
			}
		}

		newUser := models.User{
			ID:               newUserID,
			FirebaseUID:      req.FirebaseUID,
			UserName:         req.Name,
			UserNameFurigana: req.NameFurigana,
			Email:            req.Email,
			Phone:            req.PhoneNumber,
			Role:             req.Role,
			StoreID:          storeIdForUser,
			TermsAgreed:      req.TermsAgreed,
			TermsAgreedAt:    termsAgreedAt,
			PrivacyAgreed:    req.PrivacyAgreed,
			PrivacyAgreedAt:  privacyAgreedAt,
		}

		// ユーザー生成
		_, err = userCollection.InsertOne(sessCtx, newUser)
		if err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}

		// Staffの場合、store_staff_infoにも追加
		if req.Role == "staff" {
			storeStaffCollection := db.GetCollection(DatabaseName, "store_staff_info")
			newStaffInfo := models.StoreStaffInfo{
				ID:        primitive.NewObjectID(),
				UserID:    newUserID,
				Role:      "staff",
				StoreID:   storeIdForUser,
				Status:    models.StaffStatusPending, // 初期状態は承認待ち
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			_, err = storeStaffCollection.InsertOne(sessCtx, newStaffInfo)
			if err != nil {
				return nil, fmt.Errorf("failed to create store staff info: %w", err)
			}
		}

		response_data := map[string]interface{}{}
		response_data["user"] = newUser

		if req.Role == "manager" {
			response_data["store"] = newStore
			// response_data["line_login_url"] = lineLoginUrl
		}

		return response_data, nil
	})
	// Transaction終了

	if err != nil {
		// Trasaction Callbackされた時の処理
		if strings.Contains(err.Error(), "already exists") {
			utils.RespondWithError(w, err.Error(), http.StatusConflict)
		} else if strings.Contains(err.Error(), "not found") {
			utils.RespondWithError(w, err.Error(), http.StatusNotFound)
		} else if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "Invalid") {
			utils.RespondWithError(w, err.Error(), http.StatusBadRequest)
		} else {
			utils.RespondWithError(w, "Transaction failed: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Transaction成功
	utils.RespondWithJSON(w, result, http.StatusCreated)
}

func StoreExistsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Store ID is required", http.StatusBadRequest)
		return
	}
	storeCollection := db.GetCollection(DatabaseName, StoresCollection)
	count, err := storeCollection.CountDocuments(r.Context(), bson.M{"store_id": storeID})
	if err != nil {
		utils.RespondWithError(w, "Database error", http.StatusInternalServerError)
		return
	}
	exists := count > 0
	utils.RespondWithJSON(w, map[string]bool{"exists": exists}, http.StatusOK)
}

func EmailCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req models.EmailCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Email == "" {
		utils.RespondWithError(w, "Email is required", http.StatusBadRequest)
		return
	}
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(req.Email) {
		utils.RespondWithError(w, "Invalid email format", http.StatusBadRequest)
		return
	}
	// 1. MongoDB check
	userCollection := db.GetCollection(DatabaseName, UsersCollection)
	count, err := userCollection.CountDocuments(r.Context(), bson.M{"email": req.Email})
	if err != nil {
		utils.RespondWithError(w, "Database error", http.StatusInternalServerError)
		return
	}

	// 2. Firebase Auth check
	// MongoDBに存在しない場合でもFirebaseに存在する可能性があるため、チェックする
	// 最終加入段階で、エラー400防止のため、チェックする
	_, err = auth.GetUserByEmail(r.Context(), req.Email)
	firebaseExists := (err == nil)

	// DBまたはFirebaseに存在する場合、利用できない
	isUnavailable := (count > 0) || firebaseExists

	utils.RespondWithJSON(w, map[string]bool{"available": !isUnavailable}, http.StatusOK)
}

func PhoneCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req models.PhoneCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.PhoneNumber == "" {
		utils.RespondWithError(w, "Phone number is required", http.StatusBadRequest)
		return
	}
	phoneRegex := regexp.MustCompile(`^\d{2,3}-\d{3,4}-\d{4}$`)
	if !phoneRegex.MatchString(req.PhoneNumber) {
		utils.RespondWithError(w, "Invalid phone number format (e.g., 010-1234-5678)", http.StatusBadRequest)
		return
	}
	userCollection := db.GetCollection(DatabaseName, UsersCollection)
	count, err := userCollection.CountDocuments(r.Context(), bson.M{"phone": req.PhoneNumber})
	if err != nil {
		utils.RespondWithError(w, "Database error", http.StatusInternalServerError)
		return
	}
	utils.RespondWithJSON(w, map[string]bool{"available": count == 0}, http.StatusOK)
}

// ログインされているユーザーの店舗登録プロセス
func AddNewStoreHandler(w http.ResponseWriter, r *http.Request) {
	// 使用者認証
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	if idToken == authHeader {
		utils.RespondWithError(w, "Invalid Authorization header format", http.StatusUnauthorized)
		return
	}
	firebaseUID, err := auth.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	// 新しい店舗情報をリクエストボディから取得
	var req models.SignUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	session, err := db.MongoClient.StartSession()
	if err != nil {
		utils.RespondWithError(w, "Failed to start database session", http.StatusInternalServerError)
		return
	}
	defer session.EndSession(r.Context())

	result, err := session.WithTransaction(r.Context(), func(sessCtx mongo.SessionContext) (interface{}, error) {

		userCollection := db.GetCollection(DatabaseName, UsersCollection)
		storeCollection := db.GetCollection(DatabaseName, StoresCollection)

		// firebaseUIDで既存ユーザーを検索し、user.ID（MongoDBの_id）を取得
		var existingUser models.User
		err := userCollection.FindOne(sessCtx, bson.M{"firebase_uid": firebaseUID}).Decode(&existingUser)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				return nil, fmt.Errorf("user not found")
			}
			return nil, fmt.Errorf("database error while finding user: %w", err)
		}

		// 'manager'でない場合、新しい店舗登録を防ぐ
		if existingUser.Role != "manager" {
			return nil, fmt.Errorf("only managers can add new stores")
		}

		// 店舗情報有効性検証
		if req.StoreName == nil || *req.StoreName == "" {
			return nil, fmt.Errorf("store name is required")
		}
		phoneRegex := regexp.MustCompile(`^\d{2,3}-\d{3,4}-\d{4}$`)
		if req.StoreTelNumber == nil || !phoneRegex.MatchString(*req.StoreTelNumber) {
			return nil, fmt.Errorf("invalid store phone number format")
		}
		count, err := storeCollection.CountDocuments(sessCtx, bson.M{"phone": *req.StoreTelNumber})
		if err != nil {
			return nil, fmt.Errorf("database error during store phone check: %w", err)
		}
		if count > 0 {
			return nil, fmt.Errorf("a store with this phone number already exists")
		}

		// 新しい店舗データ生成
		newStore := models.Store{
			ID:        primitive.NewObjectID(),
			StoreName: *req.StoreName,
			Address:   *req.StoreAddress,
			Phone:     *req.StoreTelNumber,
			StoreID:   primitive.NewObjectID().Hex(),
			UserID:    existingUser.ID,
		}
		_, err = storeCollection.InsertOne(sessCtx, newStore)
		if err != nil {
			return nil, fmt.Errorf("failed to create store: %w", err)
		}

		// store_license及びstore_settings生成
		// lineToken, err := utils.GenerateSecureToken(32)
		// if err != nil {
		// 	return nil, fmt.Errorf("failed to generate security token: %w", err)
		// }

		licenseCollection := db.GetCollection(DatabaseName, StoreLicenseCollection)
		initialLicenseInfo := models.StoreLicense{
			ID:                 primitive.NewObjectID(),
			StoreID:            newStore.StoreID,
			VerificationStatus: models.StatusNotSubmitted,
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
			// LineAuthToken:      lineToken,
		}
		_, err = licenseCollection.InsertOne(sessCtx, initialLicenseInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to create initial license info: %w", err)
		}

		storeSettingsCollection := db.GetCollection(DatabaseName, StoreSettingsCollection)
		defaultSettings := models.StoreSetting{
			ID:        primitive.NewObjectID(),
			StoreID:   newStore.StoreID,
			ManagerID: existingUser.ID.Hex(),
			Settings: models.Settings{
				OperatingHours: map[string]models.StoreDayHours{
					"monday": {Start: "09:00", End: "18:00"}, "tuesday": {Start: "09:00", End: "18:00"},
					"wednesday": {Start: "09:00", End: "18:00"}, "thursday": {Start: "09:00", End: "18:00"},
					"friday": {Start: "09:00", End: "18:00"}, "saturday": {Start: "09:00", End: "18:00"},
					"sunday": {Start: "09:00", End: "18:00"},
				},
				ClosedDays: models.ClosedDays{
					SpecificDates: []string{}, RegularWeekly: []string{}, RegularMonthly: []string{}, HolidayClosure: true,
				},
				WaitingPolicy: models.WaitingPolicy{
					MaxWaitingCount:     utils.GetIntPointerValue(req.MaxWaitingCount, 10),
					EstimatedWaitTime:   utils.GetIntPointerValue(req.EstimatedWaitTime, 10),
					EnableMenuSelection: utils.GetBoolPointerValue(req.EnableMenuSelection, false),
				},
			},
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		_, err = storeSettingsCollection.InsertOne(sessCtx, defaultSettings)
		if err != nil {
			return nil, fmt.Errorf("failed to create default store settings: %w", err)
		}

		// var lineLoginUrl string

		// LINEログインURL生成
		// lineChannelID := os.Getenv("LINE_LOGIN_CHANNEL_ID")
		// lineCallbackURL := os.Getenv("LINE_CALLBACK_URL")

		// baseURL := "https://access.line.me/oauth2/v2.1/authorize"
		// params := url.Values{}
		// params.Add("response_type", "code")
		// params.Add("client_id", lineChannelID)
		// params.Add("redirect_uri", lineCallbackURL)
		// params.Add("state", lineToken) // state価で生成したtokenを使用
		// params.Add("scope", "openid profile")
		// params.Add("bot_prompt", "aggressive")

		// lineLoginUrl = baseURL + "?" + params.Encode() // URL完成

		response_data := map[string]interface{}{}
		response_data["user"] = existingUser
		response_data["store"] = newStore
		// response_data["line_login_url"] = lineLoginUrl

		return response_data, nil
	})

	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			utils.RespondWithError(w, err.Error(), http.StatusNotFound)
		} else if strings.Contains(err.Error(), "already exists") {
			utils.RespondWithError(w, err.Error(), http.StatusConflict)
		} else {
			utils.RespondWithError(w, "Transaction failed: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	utils.RespondWithJSON(w, result, http.StatusCreated)
}
