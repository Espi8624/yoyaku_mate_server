package handlers

import (
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"

	"encoding/json"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// GET /api/provider_user?user_id=xxx
func ProviderUserHandler(w http.ResponseWriter, r *http.Request) {
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

		user, err := data.GetProviderUserData(objectID)
		if err != nil {
			utils.RespondWithError(w, "Provider user not found", http.StatusNotFound)
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
		if err := data.UpdateProviderUserData(objectID, update); err != nil {
			utils.RespondWithError(w, "Failed to update provider user info", http.StatusInternalServerError)
			return
		}
		utils.RespondWithJSON(w, map[string]bool{"success": true}, http.StatusOK)
	default:
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /api/provider_user/firebase_uid?uid=xxxx
func ProviderUserByFirebaseUIDHandler(w http.ResponseWriter, r *http.Request) {
	uid := r.URL.Query().Get("uid")
	if uid == "" {
		utils.RespondWithError(w, "Missing uid parameter", http.StatusBadRequest)
		return
	}
	user, err := data.GetProviderUserDataByFirebaseUID(uid)
	if err != nil {
		utils.RespondWithError(w, "User not found", http.StatusNotFound)
		return
	}
	utils.RespondWithJSON(w, user, http.StatusOK)
}
