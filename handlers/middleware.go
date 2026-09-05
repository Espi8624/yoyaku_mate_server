package handlers

import (
	"context"
	"log"
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

// セッション検証の結果を表すエラーコード
// - クライアントはこのコードで挙動を分岐する。両者を1つのコードにまとめてしまうと、
//   静かに復旧できる状況でもログアウトダイアログが出てしまい、ユーザーは障害だと受け取る
const (
	// - セッショントークンが無い/サーバーに存在しない。端末の再インストール等でローカルの
	//   セッションが失われた場合に起きる。クライアントは静かに再発行して1度だけリトライする
	ErrCodeSessionRequired = "SESSION_REQUIRED"
	// - セッションが無効化されている。他端末でログインされた場合に起きる。
	//   ここだけがユーザーへの通知(ログアウト)対象
	ErrCodeSessionRevoked = "SESSION_REVOKED"
)

// MiddlewareSessionRepository セッション検証ミドルウェアが必要とする操作のみを切り出したインターフェース
type MiddlewareSessionRepository interface {
	FindBySessionID(sessionID string) (*models.Session, error)
	TouchLastSeen(session *models.Session) error
}

// RequireSessionMiddleware 端末セッションを検証するミドルウェア
// - RequireAuthMiddleware の後段に置くこと (contextの認証済みユーザーを前提とする)
// - ハンドラごとに手で検証を書くと必ず付け忘れが発生するため、付け忘れようのない位置に置く
func RequireSessionMiddleware(repo MiddlewareSessionRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// - CORSプリフライトは認証情報を伴わないため検証対象外
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			user, ok := GetUserFromContext(r.Context())
			if !ok {
				utils.RespondWithError(w, "Authentication required", http.StatusUnauthorized)
				return
			}

			if !VerifySessionForUser(w, r, repo, user) {
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// VerifySessionForUser セッションを検証し、無効な場合はエラーレスポンスを書き込んで false を返す
// - ミドルウェアを適用できないルート (ゲストと直員が同じエンドポイントを共有しており、
//   認証要否がリクエスト内容によって変わるもの) から直接呼び出すためのヘルパー
func VerifySessionForUser(w http.ResponseWriter, r *http.Request, repo MiddlewareSessionRepository, user *models.User) bool {
	sessionID := r.Header.Get("X-Session-Id")
	if sessionID == "" {
		utils.RespondWithErrorCode(w, ErrCodeSessionRequired, "セッションの再確立が必要です。", http.StatusUnauthorized)
		return false
	}

	session, err := repo.FindBySessionID(sessionID)
	if err != nil {
		utils.RespondWithError(w, "Failed to verify session", http.StatusInternalServerError)
		return false
	}

	// - 未知のトークン、または他ユーザーのトークン。奪われたわけではないので再発行を促すだけに留める
	if session == nil || session.UserID != user.ID {
		utils.RespondWithErrorCode(w, ErrCodeSessionRequired, "セッションの再確立が必要です。", http.StatusUnauthorized)
		return false
	}

	// - 無効化済み: 他端末でログインされたケース
	if !session.IsActive() {
		utils.RespondWithErrorCode(w, ErrCodeSessionRevoked, "他の端末でログインされたため、ログアウトします。", http.StatusUnauthorized)
		return false
	}

	// - 最終アクセス日時の更新に失敗してもリクエスト自体は通す (可用性を優先)
	if err := repo.TouchLastSeen(session); err != nil {
		log.Printf("Failed to touch session last_seen: %v", err)
	}
	return true
}

// GetUserFromContext 認証ミドルウェアが格納したユーザー情報をcontextから取り出すヘルパー
// - RequireAuthMiddlewareが適用されたルートでのみ使用可能
// - ユーザーが存在しない場合はnil, falseを返す
func GetUserFromContext(ctx context.Context) (*models.User, bool) {
	user, ok := ctx.Value(ContextKeyUser).(*models.User)
	return user, ok
}
