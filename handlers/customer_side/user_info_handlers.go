package handlers

import (
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"
)

// ユーザー情報返却
func UserInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// クエリパラメータからuser_idを取得
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		utils.RespondWithError(w, "Missing user_id", http.StatusBadRequest)
		return
	}

	// MongoDBから店メニュー情報を取得
	userInfoData, err := data.GetUserInfoData(userID)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch user info", http.StatusInternalServerError)
		return
	}

	// JSON 形式でレスポンスを返す
	utils.RespondWithJSON(w, userInfoData, http.StatusOK)
}

// 店メニュー情報返却
func UserCommentsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// クエリパラメータからuser_idを取得
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		utils.RespondWithError(w, "Missing store_id", http.StatusBadRequest)
		return
	}

	// MongoDBから店メニュー情報を取得
	userCommentData, err := data.GetUserCommentData(userID)
	if err != nil {
		utils.RespondWithError(w, "Failed to fetch store menu", http.StatusInternalServerError)
		return
	}

	// JSON 形式でレスポンスを返す
	utils.RespondWithJSON(w, userCommentData, http.StatusOK)
}
