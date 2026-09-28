package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"yoyaku_mate_server/auth"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// allowedUserUpdateFields PUT /api/provider_user で本人が書き換えてよいフィールド。
// role・store_id・firebase_uid・email・terms_agreed等は専用フローでのみ変更されるべきなので、
// ここには含めない(user_image_urlも専用エンドポイント[/provider_user/image]があるため除外)
var allowedUserUpdateFields = map[string]bool{
	"user_name":          true,
	"user_name_furigana": true,
	"birthdate":          true,
	"zip_code":           true,
	"prefecture":         true,
	"city":               true,
	"address":            true,
	"building":           true,
}

// UserInfoRepository ユーザー情報の取得と更新を抽象化するインターフェース
type UserInfoRepository interface {
	GetUserData(userID primitive.ObjectID) (*models.User, error)
	UpdateUserData(userID primitive.ObjectID, update map[string]interface{}) (*models.User, error)
	GetUserDataByFirebaseUID(uid string) (*models.User, error)
	MarkUserWithdrawn(userID primitive.ObjectID) error
}

// UserAccountStoreOwnershipRepository 会員退会時、店舗オーナーかどうかの確認に使う最小インターフェース
type UserAccountStoreOwnershipRepository interface {
	CountStoresByOwner(userID primitive.ObjectID) (int64, error)
}

// UserAccountStaffMembershipRepository 会員退会時、スタッフの店舗所属情報更新に使う最小インターフェース
type UserAccountStaffMembershipRepository interface {
	MarkStoreStaffWithdrawnByUserID(userID primitive.ObjectID) error
}

// UserInfoHandler ユーザー情報関連のHTTPリクエストを処理するハンドラ
type UserInfoHandler struct {
	userRepo  UserInfoRepository
	storeRepo UserAccountStoreOwnershipRepository
	staffRepo UserAccountStaffMembershipRepository
	authSvc   AuthService
}

func NewUserInfoHandler(
	userRepo UserInfoRepository,
	storeRepo UserAccountStoreOwnershipRepository,
	staffRepo UserAccountStaffMembershipRepository,
	authSvc AuthService,
) *UserInfoHandler {
	return &UserInfoHandler{
		userRepo:  userRepo,
		storeRepo: storeRepo,
		staffRepo: staffRepo,
		authSvc:   authSvc,
	}
}

// HandleUser GET /api/provider_user?user_id=xxx
// HandleUser PUT /api/provider_user?user_id=xxx
// HandleUser DELETE /api/provider_user?user_id=xxx (会員退会。ソフトデリートで連絡先は保持する)
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

		// マスアサインメント対策: role・store_id・firebase_uidなど本来クライアントが
		// 書き換えてはいけないフィールドを弾くため、許可されたフィールドのみ残す
		update = utils.FilterAllowedFields(update, allowedUserUpdateFields)
		if len(update) == 0 {
			utils.RespondWithError(w, "No valid fields to update", http.StatusBadRequest)
			return
		}

		updatedUser, err := h.userRepo.UpdateUserData(objectID, update)
		if err != nil {
			utils.RespondWithError(w, "Failed to update user info", http.StatusInternalServerError)
			return
		}
		// - REST標準: PUTレスポンスに更新後のリソースを返却
		utils.RespondWithJSON(w, updatedUser, http.StatusOK)

	case http.MethodDelete:
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

		// - 本人のアカウントのみ削除可能（他ユーザーの退会を防止）
		if objectID != authUser.ID {
			utils.RespondWithError(w, "Forbidden: cannot delete other user's account", http.StatusForbidden)
			return
		}

		user, err := h.userRepo.GetUserData(objectID)
		if err != nil {
			utils.RespondWithError(w, "User not found", http.StatusNotFound)
			return
		}

		switch user.Role {
		case "manager":
			// マネージャーが店舗を持ったまま退会すると、待機リスト・メニュー・
			// スタッフ所属・シフト表など下位データが宙に浮いてしまうため、
			// 店舗を全て削除(または譲渡)してからでないと退会できないようにする
			storeCount, err := h.storeRepo.CountStoresByOwner(objectID)
			if err != nil {
				utils.RespondWithError(w, "Failed to check owned stores", http.StatusInternalServerError)
				return
			}
			if storeCount > 0 {
				utils.RespondWithError(w,
					"店舗を保有したまま退会することはできません。先に店舗を削除してください。",
					http.StatusConflict)
				return
			}
		case "staff":
			// スタッフは複数店舗に所属し得るため、所属情報は削除せず全てWITHDRAWNに
			// マークする。店舗のスタッフ一覧に「退会済み」として残り続け、
			// マネージャーが後から連絡先を確認できるようにするため
			if err := h.staffRepo.MarkStoreStaffWithdrawnByUserID(objectID); err != nil {
				utils.RespondWithError(w, "Failed to update store memberships", http.StatusInternalServerError)
				return
			}
		}

		// ソフトデリート: 電話番号・住所などの連絡先は保持したまま、
		// ステータスをWITHDRAWNにしてログインのみ不可にする。
		// (退会後も運営側から本人に連絡できるようにするための方針)
		if err := h.userRepo.MarkUserWithdrawn(objectID); err != nil {
			utils.RespondWithError(w, "Failed to withdraw user account", http.StatusInternalServerError)
			return
		}

		// Firebase Auth側のアカウントは削除する(連絡先はMongo側に残るため、
		// ログイン用の認証情報自体を残しておく必要はない)
		if user.FirebaseUID != "" {
			if err := auth.DeleteUser(r.Context(), user.FirebaseUID); err != nil {
				log.Printf("会員退会時のFirebaseアカウント削除に失敗しました (uid=%s): %v", user.FirebaseUID, err)
			}
		}

		utils.RespondWithJSON(w, map[string]string{"message": "Account withdrawn successfully"}, http.StatusOK)

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

	// - 3. ユーザー情報取得
	// - 端末セッションの発行は POST /api/auth/session が担うため、ここでは行わない
	user, err := h.userRepo.GetUserDataByFirebaseUID(uid)
	if err != nil {
		utils.RespondWithError(w, "User not found", http.StatusNotFound)
		return
	}

	utils.RespondWithJSON(w, user, http.StatusOK)
}
