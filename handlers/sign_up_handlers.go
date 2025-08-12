package handlers

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	DatabaseName            = "yoyaku_mate_provider"
	UsersCollection         = "user_info"
	StoresCollection        = "store_info"
	StoreSettingsCollection = "store_settings"
)

// SignUpHandler handles the user registration process
// @Summary Register a new user
// @Description Register a new user with the provided information
// @Tags auth
// @Accept json
// @Produce json
// @Param user body SignUpRequest true "User registration information"
// @Success 201 {object} models.User
// @Failure 400 {object} utils.ErrorResponse
// @Failure 409 {object} utils.ErrorResponse
// @Router /api/auth/signup [post]
func SignUpHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req models.SignUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.FirebaseUID == "" || req.Email == "" || req.Name == "" || req.Role == "" {
		utils.RespondWithError(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Validate role
	if req.Role != "manager" && req.Role != "staff" {
		utils.RespondWithError(w, "Invalid role: must be 'manager' or 'staff'", http.StatusBadRequest)
		return
	}

	// Validate email format
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(req.Email) {
		utils.RespondWithError(w, "Invalid email format", http.StatusBadRequest)
		return
	}

	// Validate personal phone number format
	phoneRegex := regexp.MustCompile(`^\d{2,3}-\d{3,4}-\d{4}$`)
	if !phoneRegex.MatchString(req.PhoneNumber) {
		utils.RespondWithError(w, "Invalid personal phone number format (e.g., 010-1234-5678)", http.StatusBadRequest)
		return
	}

	userCollection := db.GetCollection(DatabaseName, UsersCollection)
	storeCollection := db.GetCollection(DatabaseName, StoresCollection)

	// Check if user already exists (by FirebaseUID, email, or personal phone)
	var existingUser models.User
	err := userCollection.FindOne(r.Context(), bson.M{
		"$or": []bson.M{
			{"firebase_uid": req.FirebaseUID},
			{"email": req.Email},
			{"phone": req.PhoneNumber},
		},
	}).Decode(&existingUser)

	if err == nil {
		utils.RespondWithError(w, "User with this email or phone number already exists", http.StatusConflict)
		return
	} else if err != mongo.ErrNoDocuments {
		utils.RespondWithError(w, "Database error during user check", http.StatusInternalServerError)
		return
	}

	var storeIdForUser string
	newUserID := primitive.NewObjectID()

	switch req.Role {
	case "manager":
		// 新しい店舗生成
		if req.StoreName == nil || *req.StoreName == "" {
			utils.RespondWithError(w, "Store name is required for manager role", http.StatusBadRequest)
			return
		}
		if req.StoreTelNumber == nil || !phoneRegex.MatchString(*req.StoreTelNumber) {
			utils.RespondWithError(w, "Invalid store phone number format (e.g., 02-123-4567)", http.StatusBadRequest)
			return
		}

		// 店舗電話番号重複検査
		count, err := storeCollection.CountDocuments(r.Context(), bson.M{"phone": *req.StoreTelNumber})
		if err != nil {
			utils.RespondWithError(w, "Database error during store phone check", http.StatusInternalServerError)
			return
		}
		if count > 0 {
			utils.RespondWithError(w, "A store with this phone number already exists", http.StatusConflict)
			return
		}

		bizNumber := ""
		if req.BizNumber != nil {
			bizNumber = *req.BizNumber
		}

		newStore := models.Store{
			ID:        primitive.NewObjectID(),
			StoreName: *req.StoreName,
			Address:   *req.StoreAddress,
			Phone:     *req.StoreTelNumber,
			BizNumber: bizNumber,
			StoreID:   primitive.NewObjectID().Hex(),
			UserID:    newUserID,
		}

		_, err = storeCollection.InsertOne(r.Context(), newStore)
		if err != nil {
			utils.RespondWithError(w, "Failed to create store", http.StatusInternalServerError)
			return
		}

		storeIdForUser = newStore.StoreID

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
				WaitingPolicy: models.WaitingPolicy{MaxWaitingCount: 100},
			},
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		_, err = storeSettingsCollection.InsertOne(r.Context(), defaultSettings)
		if err != nil {
			storeCollection.DeleteOne(r.Context(), bson.M{"_id": newStore.ID})
			utils.RespondWithError(w, "Failed to create default store settings", http.StatusInternalServerError)
			return
		}

	case "staff":
		if req.StoreID == nil || *req.StoreID == "" {
			utils.RespondWithError(w, "Store ID is required for staff role", http.StatusBadRequest)
			return
		}

		var existingStore models.Store
		err := storeCollection.FindOne(r.Context(), bson.M{"store_id": *req.StoreID}).Decode(&existingStore)
		if err == mongo.ErrNoDocuments {
			utils.RespondWithError(w, "Store with the provided ID not found", http.StatusNotFound)
			return
		} else if err != nil {
			utils.RespondWithError(w, "Database error while verifying store", http.StatusInternalServerError)
			return
		}

		storeIdForUser = existingStore.StoreID
	}

	// User モデル生成
	newUser := models.User{
		ID:          newUserID,
		FirebaseUID: req.FirebaseUID,
		Username:    req.Name,
		Email:       req.Email,
		Phone:       req.PhoneNumber,
		Role:        req.Role,
		StoreID:     storeIdForUser,
	}

	// ユーザー生成
	_, err = userCollection.InsertOne(r.Context(), newUser)
	if err != nil {
		if req.Role == "manager" {
			storeCollection.DeleteMany(r.Context(), bson.M{"user_id": newUserID})
			db.GetCollection(DatabaseName, StoreSettingsCollection).DeleteMany(r.Context(), bson.M{"manager_id": newUserID.Hex()})
		}
		utils.RespondWithError(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, newUser, http.StatusCreated)
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
	userCollection := db.GetCollection(DatabaseName, UsersCollection)
	count, err := userCollection.CountDocuments(r.Context(), bson.M{"email": req.Email})
	if err != nil {
		utils.RespondWithError(w, "Database error", http.StatusInternalServerError)
		return
	}
	utils.RespondWithJSON(w, map[string]bool{"available": count == 0}, http.StatusOK)
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
