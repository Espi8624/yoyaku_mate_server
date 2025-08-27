package utils

import (
	"crypto/rand"
	"encoding/hex"
)

// security的に安全なランダムトークン文字列を生成
func GenerateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
