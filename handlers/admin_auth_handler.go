package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"yoyaku_mate_server/config"
	"yoyaku_mate_server/utils"
)

// - 管理者ログインリクエストのボディ
type adminLoginRequest struct {
	Password string `json:"password"`
}

// AdminLoginHandler は管理者画面(yoyaku_mate_admin)の共有パスワードを検証し、
// 成功時に署名付き管理者セッショントークンを発行する。
// - パスワード/署名鍵は環境変数(ADMIN_PASSWORD, ADMIN_TOKEN_SECRET)のみで管理し、コードには含めない
// - 未設定の場合は誰もログインできないよう安全側に倒す(空文字列を正解として扱わない)
func AdminLoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		return
	}
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := config.Get()
	if cfg.AdminPassword == "" || cfg.AdminTokenSecret == "" {
		utils.RespondWithError(w, "Admin login is not configured", http.StatusServiceUnavailable)
		return
	}

	var req adminLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// - タイミング攻撃を防ぐため定数時間比較を使用
	if subtle.ConstantTimeCompare([]byte(req.Password), []byte(cfg.AdminPassword)) != 1 {
		utils.RespondWithError(w, "Invalid password", http.StatusUnauthorized)
		return
	}

	token := utils.GenerateAdminSessionToken(cfg.AdminTokenSecret)
	utils.RespondWithJSON(w, map[string]string{"token": token}, http.StatusOK)
}
