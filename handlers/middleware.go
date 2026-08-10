package handlers

import (
	"context"
	"net/http"
	"strings"
	"yoyaku_mate_server/auth"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"
)

// - contextキータイプ: グローバルなstring型との衝突を防ぐために別途定義
type contextKey string

// - 認証済みユーザー情報を格納するcontextキー
const ContextKeyUser contextKey = "authenticated_user"

// MiddlewareUserRepository 認証ミドルウェアでFirebase UIDに基づいて内部システムのユーザー情報を取得するためのインターフェース
type MiddlewareUserRepository interface {
	GetByFirebaseUID(uid string) (*models.User, error)
}

// RequireAuthMiddleware Firebase IDトークンを検証し、認証済みユーザーをcontextに格納するミドルウェア
// - Authorizationヘッダーが存在しない場合、またはトークン検証失敗時は401を返す
// - DBでのユーザー取得失敗時は401を返す
// - 成功時は *models.User をcontextに格納し、次のハンドラを呼び出す
func RequireAuthMiddleware(repo MiddlewareUserRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// - Authorizationヘッダーを取得
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			utils.RespondWithError(w, "Authorization header is required", http.StatusUnauthorized)
			return
		}

		// - "Bearer " プレフィックスを除去してトークンを取得
		idToken := strings.TrimPrefix(authHeader, "Bearer ")
		if idToken == authHeader {
			// - "Bearer " プレフィックスが存在しない場合は不正なフォーマット
			utils.RespondWithError(w, "Invalid Authorization header format", http.StatusUnauthorized)
			return
		}

		// - Firebase IDトークンを検証
		firebaseUID, err := auth.VerifyIDToken(r.Context(), idToken)
		if err != nil {
			utils.RespondWithError(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}

		// - DBからユーザー情報を取得
		user, err := repo.GetByFirebaseUID(firebaseUID)
		if err != nil || user == nil {
			utils.RespondWithError(w, "User not found", http.StatusUnauthorized)
			return
		}

		// - 認証済みユーザーをcontextに格納し、次のハンドラへ渡す
		ctx := context.WithValue(r.Context(), ContextKeyUser, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
	}
}

// GetUserFromContext 認証ミドルウェアが格納したユーザー情報をcontextから取り出すヘルパー
// - RequireAuthMiddlewareが適用されたルートでのみ使用可能
// - ユーザーが存在しない場合はnil, falseを返す
func GetUserFromContext(ctx context.Context) (*models.User, bool) {
	user, ok := ctx.Value(ContextKeyUser).(*models.User)
	return user, ok
}
