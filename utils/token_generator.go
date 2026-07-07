package utils

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"sync"
	"yoyaku_mate_server/config"
)

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
