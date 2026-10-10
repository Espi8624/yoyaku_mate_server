package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"yoyaku_mate_server/auth"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// SessionRepository 端末セッションの発行・破棄を抽象化するインターフェース
// - 検証・最終利用時刻の更新はミドルウェア側の MiddlewareSessionRepository が担う
type SessionRepository interface {
	Create(session models.Session) (*models.Session, error)
	FindActiveByDevice(userID primitive.ObjectID, deviceID string) (*models.Session, error)
	RevokeOthers(userID primitive.ObjectID, keepSessionID string, reason string) error
	Revoke(sessionID string, reason string) error
}

// SessionHandler 端末セッションの発行・破棄を扱うハンドラ
type SessionHandler struct {
	sessionRepo SessionRepository
}

func NewSessionHandler(sessionRepo SessionRepository) *SessionHandler {
	return &SessionHandler{sessionRepo: sessionRepo}
}

// createSessionRequest セッション発行リクエストのボディ
type createSessionRequest struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	Platform   string `json:"platform"`
}

// Handle POST/DELETE /api/auth/session
// - RequireAuthMiddleware の配下に置く (Firebase認証は必要だが、セッション検証の対象にはできない。
//   このエンドポイント自体がセッションを発行するため)
func (h *SessionHandler) Handle(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleCreate(w, r)
	case http.MethodDelete:
		h.handleDelete(w, r)
	case http.MethodOptions:
		w.WriteHeader(http.StatusOK)
	default:
		respondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCreate セッションを発行する
// - 同一端末に有効なセッションが既にあればそれを再利用する。ここで毎回新規発行してしまうと、
//   セッションを再確立するたびにその端末が自分自身を無効化して無限ログアウトに陥る
// - 発行後、同一ユーザーの他端末のセッションは全て無効化する (1端末のみ許可ポリシー)
func (h *SessionHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	user, ok := GetUserFromContext(r.Context())
	if !ok {
		respondWithError(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.DeviceID == "" {
		respondWithError(w, "device_id is required", http.StatusBadRequest)
		return
	}

	// - 同一端末の既存セッションを探す
	existing, err := h.sessionRepo.FindActiveByDevice(user.ID, req.DeviceID)
	if err != nil {
		respondWithError(w, "Failed to look up session", http.StatusInternalServerError)
		return
	}

	session := existing
	if session == nil {
		created, err := h.sessionRepo.Create(models.Session{
			SessionID:  auth.GenerateRandomString(32),
			UserID:     user.ID,
			DeviceID:   req.DeviceID,
			DeviceName: req.DeviceName,
			Platform:   req.Platform,
		})
		if err != nil {
			respondWithError(w, "Failed to create session", http.StatusInternalServerError)
			return
		}
		session = created
	}

	// - 他端末のセッションを無効化する。失敗してもこの端末のログイン自体は成立しているため、
	//   リクエストは成功として返し、ログにだけ残す (次回の発行時に再度無効化が試みられる)
	if err := h.sessionRepo.RevokeOthers(user.ID, session.SessionID, models.SessionRevokedReasonNewLogin); err != nil {
		log.Printf("Failed to revoke other sessions for user %s: %v", user.ID.Hex(), err)
	}

	respondWithJSON(w, session, http.StatusOK)
}

// handleDelete 自分のセッションを破棄する (明示的なログアウト)
// - 破棄しておかないと、ログアウト済みの端末のセッションが有効なまま残り続ける
func (h *SessionHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("X-Session-Id")
	if sessionID == "" {
		respondWithError(w, "X-Session-Id header is required", http.StatusBadRequest)
		return
	}

	if err := h.sessionRepo.Revoke(sessionID, models.SessionRevokedReasonLogout); err != nil {
		respondWithError(w, "Failed to revoke session", http.StatusInternalServerError)
		return
	}

	respondWithJSON(w, map[string]bool{"revoked": true}, http.StatusOK)
}
