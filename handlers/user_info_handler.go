package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// UserInfoRepository ユーザー情報の取得と更新を抽象化するインターフェース
type UserInfoRepository interface {
	GetUserData(userID primitive.ObjectID) (*models.User, error)
	UpdateUserData(userID primitive.ObjectID, update map[string]interface{}) (*models.User, error)
	GetUserDataByFirebaseUID(uid string) (*models.User, error)
}

// UserInfoHandler ユーザー情報関連のHTTPリクエストを処理するハンドラ
type UserInfoHandler struct {
	userRepo UserInfoRepository
	authSvc  AuthService
}

func NewUserInfoHandler(userRepo UserInfoRepository, authSvc AuthService) *UserInfoHandler {
	return &UserInfoHandler{
		userRepo: userRepo,
		authSvc:  authSvc,
	}
}

// HandleUser GET /api/provider_user?user_id=xxx
// HandleUser PUT /api/provider_user?user_id=xxx
// - RequireAuthMiddlewareを通過後に呼び出される
func (h *UserInfoHandler) HandleUser(w http.ResponseWriter, r *http.Request) {
	// - ミドルウェアで格納された認証済みユーザーを取得
	authUser, ok := GetUserFromContext(r.Context())
	if !ok {
		utils.RespondWithError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		userID := r.URL.Query().Get("user_id")
		if userID == "" {
			utils.RespondWithError(w, "Missing user_id parameter", http.StatusBadRequest)
			return
		}

		// - 文字列をObjectIdに変換
		objectID, err := primitive.ObjectIDFromHex(userID)
		if err != nil {
			utils.RespondWithError(w, "Invalid user_id format", http.StatusBadRequest)
			return
		}

		// - 本人の情報のみ取得可能
		if objectID != authUser.ID {
			utils.RespondWithError(w, "Forbidden: cannot access other user's data", http.StatusForbidden)
			return
		}

		user, err := h.userRepo.GetUserData(objectID)
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

		// - 本人の情報のみ更新可能（他ユーザーの更新を防止）
		if objectID != authUser.ID {
			utils.RespondWithError(w, "Forbidden: cannot modify other user's data", http.StatusForbidden)
			return
		}

		var update map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		updatedUser, err := h.userRepo.UpdateUserData(objectID, update)
		if err != nil {
			utils.RespondWithError(w, "Failed to update user info", http.StatusInternalServerError)
			return
		}
		// - REST標準: PUTレスポンスに更新後のリソースを返却
		utils.RespondWithJSON(w, updatedUser, http.StatusOK)

	default:
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// UserByFirebaseUIDHandler GET /api/provider_user/firebase_uid?uid=xxxx
// - セカンダリログインまたはセッション開始エンドポイントとして機能する
func (h *UserInfoHandler) UserByFirebaseUIDHandler(w http.ResponseWriter, r *http.Request) {
	// - 1. 認証チェック
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
		return
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	firebaseUID, err := h.authSvc.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
		return
	}

	// - 2. リクエストUIDの検証
	uid := r.URL.Query().Get("uid")
	if uid == "" {
		utils.RespondWithError(w, "Missing uid parameter", http.StatusBadRequest)
		return
	}
	if firebaseUID != uid {
		utils.RespondWithError(w, "Token UID does not match request UID", http.StatusForbidden)
		return
	}

	// - 3. トークン再生成の意図確認
	regenerateToken := r.URL.Query().Get("regenerate_token") == "true"

	// - 4. ユーザー情報取得
	user, err := h.userRepo.GetUserDataByFirebaseUID(uid)
	if err != nil {
		utils.RespondWithError(w, "User not found", http.StatusNotFound)
		return
	}

	if regenerateToken {
		// - 新しいログイントークン（セッションID）を生成
		newLoginToken := utils.GenerateRandomString(32)

		// - DBのユーザー情報を新しいトークンで更新
		_, err = h.userRepo.UpdateUserData(user.ID, map[string]interface{}{
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
