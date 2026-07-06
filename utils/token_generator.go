package utils

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
)

// 開発用のFallback
var HMACSecret = []byte(os.Getenv("HMAC_SECRET"))

func init() {
	if len(HMACSecret) == 0 {
		// 환경변수 HMAC_SECRET이 설정되지 않은 경우 (로컬 개발 전용)
		// Production 환경에서는 반드시 HMAC_SECRET 환경변수를 설정해야 함
		log.Println("WARNING: HMAC_SECRET is not set. Using insecure fallback for local development only.")
		HMACSecret = []byte("local-dev-only-do-not-use-in-production")
	}
}

// HMACトークンを生成する
func GenerateHMACDateToken(storeID string, dateStr string) string {
	h := hmac.New(sha256.New, HMACSecret)
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
