package handlers

import (
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"

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

		// 문자열을 ObjectId로 변환
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
	default:
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
