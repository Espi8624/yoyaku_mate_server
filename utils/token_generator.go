package utils

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
	"yoyaku_mate_server/config"
)

// - 管理者共有ログインで発行するセッショントークンの有効期間
const AdminSessionTokenTTL = 12 * time.Hour

var (
	hmacOnce sync.Once
)

// configパッケージからHMAC秘密鍵を取得する補助関数
func getHMACSecret() []byte {
	secret := config.Get().HMACSecret
	if secret == "" {
		hmacOnce.Do(func() {
			// 環境変数/JSON設定で HMAC_SECRET が指定されていない場合、警告ログを出力
			log.Println("WARNING: HMAC_SECRET is not set. Using insecure fallback for local development only.")
		})
		return []byte("local-dev-only-do-not-use-in-production")
	}
	return []byte(secret)
}

// HMACトークンを生成する
func GenerateHMACDateToken(storeID string, dateStr string) string {
	h := hmac.New(sha256.New, getHMACSecret())
	h.Write([]byte(storeID + ":" + dateStr))
	return hex.EncodeToString(h.Sum(nil))
}

// HMACトークンを検証する
func VerifyHMACDateToken(storeID string, dateStr string, token string) bool {
	expectedToken := GenerateHMACDateToken(storeID, dateStr)
	return hmac.Equal([]byte(token), []byte(expectedToken))
}

// security的に安安全なランダムトークン文字列を生成
func GenerateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// - 管理者共有ログイン成功時に発行する有効期限付き署名トークンを生成する
// - "{有効期限Unix秒}.{HMAC署名}" 形式の自己検証トークンで、DB/メモリにセッションを保持しない
//   (サーバー再起動やスケールダウンでも検証ロジックだけで有効性を確認できる)
func GenerateAdminSessionToken(secret string) string {
	expiry := time.Now().Add(AdminSessionTokenTTL).Unix()
	payload := strconv.FormatInt(expiry, 10)

	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	signature := hex.EncodeToString(h.Sum(nil))

	return payload + "." + signature
}

// - 管理者セッショントークンの署名と有効期限を検証する
func VerifyAdminSessionToken(secret, token string) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}
	payload, signature := parts[0], parts[1]

	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	expectedSignature := hex.EncodeToString(h.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return false
	}

	expiry, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix() < expiry
}
