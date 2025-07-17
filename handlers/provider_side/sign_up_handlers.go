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

// SignUpRequest는 회원가입 요청 데이터 구조체입니다.
type SignUpRequest struct {
	FirebaseUID    string  `json:"firebase_uid"` // Firebase에서 받은 UID
	Name           string  `json:"name"`         // 사용자 이름
	Email          string  `json:"email"`        // 이메일
	PhoneNumber    string  `json:"phone_number"` // 전화번호
	Role           string  `json:"role"`         // "manager" or "staff"
	StoreName      *string `json:"store_name,omitempty"`
	StoreAddress   *string `json:"store_address,omitempty"`
	StoreTelNumber *string `json:"store_tel_number,omitempty"`
	StoreEmail     *string `json:"store_email,omitempty"`
	BizNumber      *string `json:"biz_number,omitempty"`
	Description    *string `json:"description,omitempty"`
}

// EmailCheckRequest는 이메일 중복 확인 요청 구조체입니다.
type EmailCheckRequest struct {
	Email string `json:"email"`
}

// PhoneCheckRequest는 전화번호 중복 확인 요청 구조체입니다.
type PhoneCheckRequest struct {
	PhoneNumber string `json:"phone_number"`
}

// SignUpHandler handles the user registration process
// @Summary Register a new user
// @Description Register a new user with the provided information
// @Tags auth
// @Accept json
// @Produce json
// @Param user body SignUpRequest true "User registration information"
// @Success 201 {object} models.UserInfo
// @Failure 400 {object} utils.ErrorResponse
// @Failure 409 {object} utils.ErrorResponse
// @Router /api/auth/signup [post]
func SignUpHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SignUpRequest
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

	// Validate phone number format
	phoneRegex := regexp.MustCompile(`^\d{2,3}-\d{3,4}-\d{4}$`)
	if !phoneRegex.MatchString(req.PhoneNumber) {
		utils.RespondWithError(w, "Invalid phone number format (e.g., 010-1234-5678)", http.StatusBadRequest)
		return
	}

	// Get MongoDB collection
	userCollection := db.GetCollection(DatabaseName, UsersCollection)

	// Check if user already exists (by FirebaseUID, email, or phone)
	var existingUser models.User
	err := userCollection.FindOne(r.Context(), bson.M{
		"$or": []bson.M{
			{"firebase_uid": req.FirebaseUID},
			{"email": req.Email},
			{"phone": req.PhoneNumber},
		},
	}).Decode(&existingUser)

	if err == nil {
		utils.RespondWithError(w, "User already exists", http.StatusConflict)
		return
	} else if err != mongo.ErrNoDocuments {
		utils.RespondWithError(w, "Database error", http.StatusInternalServerError)
		return
	}

	newUser := models.User{
		ID:          primitive.NewObjectID(),
		FirebaseUID: req.FirebaseUID,
		Username:    req.Name,
		Email:       req.Email,
		Phone:       req.PhoneNumber,
		Role:        req.Role,
	}

	if req.Role == "manager" && req.StoreName != nil {
		// Create store for manager
		storeCollection := db.GetCollection(DatabaseName, StoresCollection)
		bizNumber := ""
		if req.BizNumber != nil {
			bizNumber = *req.BizNumber
		}

		storeDoc := models.Store{
			ID:        primitive.NewObjectID(),
			StoreName: *req.StoreName,
			Address:   *req.StoreAddress,
			Phone:     *req.StoreTelNumber,
			BizNumber: bizNumber,
			StoreID:   primitive.NewObjectID().Hex(), // Generate custom store ID
			UserID:    newUser.ID,                    // Link to the user's MongoDB ID
		}

		_, err = storeCollection.InsertOne(r.Context(), storeDoc)
		if err != nil {
			utils.RespondWithError(w, "Failed to create store", http.StatusInternalServerError)
			return
		}

		// Create default store settings for the new store
		storeSettingsCollection := db.GetCollection(DatabaseName, StoreSettingsCollection)
		defaultSettings := models.StoreSettings{
			ID:        primitive.NewObjectID(),
			StoreID:   storeDoc.StoreID,
			ManagerID: newUser.ID.Hex(),
			Settings: models.Settings{
				OperatingHours: map[string]models.StoreDayHours{
					"monday":    {Start: "09:00", End: "18:00"},
					"tuesday":   {Start: "09:00", End: "18:00"},
					"wednesday": {Start: "09:00", End: "18:00"},
					"thursday":  {Start: "09:00", End: "18:00"},
					"friday":    {Start: "09:00", End: "18:00"},
					"saturday":  {Start: "09:00", End: "18:00"},
					"sunday":    {Start: "09:00", End: "18:00"},
				},
				ClosedDays: models.ClosedDays{
					SpecificDates:  []string{},
					RegularWeekly:  []string{},
					RegularMonthly: []string{},
					HolidayClosure: true,
				},
				WaitingPolicy: models.WaitingPolicy{
					MaxWaitingCount: 100,
				},
			},
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		_, err = storeSettingsCollection.InsertOne(r.Context(), defaultSettings)
		if err != nil {
			utils.RespondWithError(w, "Failed to create default store settings", http.StatusInternalServerError)
			return
		}
	}

	// Create user
	_, err = userCollection.InsertOne(r.Context(), newUser)
	if err != nil {
		utils.RespondWithError(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, newUser, http.StatusCreated)
}

// EmailCheckHandler handles email duplication check
// @Summary Check email availability
// @Description Check if an email is already registered
// @Tags auth
// @Accept json
// @Produce json
// @Param email body EmailCheckRequest true "Email to check"
// @Success 200 {object} map[string]bool
// @Router /api/auth/check-email [post]
func EmailCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req EmailCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Email == "" {
		utils.RespondWithError(w, "Email is required", http.StatusBadRequest)
		return
	}

	// Validate email format
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

// PhoneCheckHandler handles phone number duplication check
// @Summary Check phone number availability
// @Description Check if a phone number is already registered
// @Tags auth
// @Accept json
// @Produce json
// @Param phone body PhoneCheckRequest true "Phone number to check"
// @Success 200 {object} map[string]bool
// @Router /api/auth/check-phone [post]
func PhoneCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PhoneCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.PhoneNumber == "" {
		utils.RespondWithError(w, "Phone number is required", http.StatusBadRequest)
		return
	}

	// Validate phone number format
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
