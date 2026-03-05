package handlers

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"
	"yoyaku_mate_server/auth"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"

	"encoding/json"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// GET /api/provider_user?user_id=xxx
func UserHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		userID := r.URL.Query().Get("user_id")
		if userID == "" {
			utils.RespondWithError(w, "Missing user_id parameter", http.StatusBadRequest)
			return
		}

		// 文字列を ObjectId に変換
		objectID, err := primitive.ObjectIDFromHex(userID)
		if err != nil {
			utils.RespondWithError(w, "Invalid user_id format", http.StatusBadRequest)
			return
		}

		user, err := data.GetUserData(objectID)
		if err != nil {
			utils.RespondWithError(w, "User not found", http.StatusNotFound)
			return
		}
		utils.RespondWithJSON(w, user, http.StatusOK)
	case http.MethodPut:
		userID := r.URL.Query().Get("user_id")
		if userID == "" {
			utils.RespondWithError(w, "Missing user_id parameter", http.StatusBadRequest)
			return
		}
		objectID, err := primitive.ObjectIDFromHex(userID)
		if err != nil {
			utils.RespondWithError(w, "Invalid user_id format", http.StatusBadRequest)
			return
		}
		var update map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		updatedUser, err := data.UpdateUserData(objectID, update)
		if err != nil {
			utils.RespondWithError(w, "Failed to update user info", http.StatusInternalServerError)
			return
		}
		// REST 標準: PUT レスポンスに更新後のリソースを返却
		utils.RespondWithJSON(w, updatedUser, http.StatusOK)
	default:
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *UploadHandler) UploadUserImage(w http.ResponseWriter, r *http.Request) {
	// 認証情報取得と検証
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Unauthorized: Authorization header not found", http.StatusUnauthorized)
		return
	}

	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	if idToken == authHeader {
		utils.RespondWithError(w, "Unauthorized: Invalid token format", http.StatusUnauthorized)
		return
	}

	firebaseUID, err := auth.VerifyIDToken(context.Background(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Unauthorized: Invalid ID token", http.StatusUnauthorized)
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		utils.RespondWithError(w, "Could not parse multipart form", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("userImage")
	if err != nil {
		utils.RespondWithError(w, "Could not get uploaded file named 'userImage'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// MinIOにアップロード
	fileURL, err := h.Minio.UploadFile(h.AssetsBucketName, h.AssetsPublicDomain, file, header)
	if err != nil {
		log.Printf("Error uploading user to Minio: %v", err)
		utils.RespondWithError(w, "Could not upload file", http.StatusInternalServerError)
		return
	}

	// DBアップデート
	updatedUser, err := data.UpdateUserImageURL(firebaseUID, fileURL)
	if err != nil {
		log.Printf("Error updating user image URL in DB: %v", err)
		utils.RespondWithError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, updatedUser, http.StatusOK)
}

// GET /api/provider_user/firebase_uid?uid=xxxx
// This acts as a "Secondary Login" or "Session Start" endpoint.
func UserByFirebaseUIDHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Verify Authentication
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	firebaseUID, err := auth.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	// 2. Validate Request UID
	uid := r.URL.Query().Get("uid")
	if uid == "" {
		utils.RespondWithError(w, "Missing uid parameter", http.StatusBadRequest)
		return
	}
	if firebaseUID != uid {
		utils.RespondWithError(w, "Token UID does not match request UID", http.StatusForbidden)
		return
	}

	// 3. Check Intent (Regenerate Token?)
	regenerateToken := r.URL.Query().Get("regenerate_token") == "true"

	// 4. Get User Data First
	user, err := data.GetUserDataByFirebaseUID(uid)
	if err != nil {
		utils.RespondWithError(w, "User not found", http.StatusNotFound)
		return
	}

	if regenerateToken {
		// Generate New Login Token (Session ID)
		newLoginToken := utils.GenerateRandomString(32)

		// Update User in DB with new token
		_, err = data.UpdateUserData(user.ID, map[string]interface{}{
			"login_token": newLoginToken,
			"updated_at":  time.Now(),
		})
		if err != nil {
			utils.RespondWithError(w, "Failed to update login session", http.StatusInternalServerError)
			return
		}
		user.LoginToken = newLoginToken
	}

	utils.RespondWithJSON(w, user, http.StatusOK)
}
