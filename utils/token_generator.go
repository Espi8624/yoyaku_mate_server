package utils

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// 開発用のFallback
var HMACSecret = []byte(os.Getenv("HMAC_SECRET"))

func init() {
	if len(HMACSecret) == 0 {
		// Fallback for development (not secure for production)
		HMACSecret = []byte("yoyaku-mate-fallback-secret-2024")
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
