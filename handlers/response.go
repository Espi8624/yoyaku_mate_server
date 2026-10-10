package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
)

// JSON応答を返すヘルパー関数
func respondWithJSON(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(statusCode)

	response := map[string]interface{}{
		"status": "success",
		"data":   data,
	}
	json.NewEncoder(w).Encode(response)
}

// エラーレスポンスを返すヘルパー関数
func respondWithError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// エラーメッセージにデータベース関連のキーワードが含まれている場合、ヘッダーに識別用のエラータイプを設定
	lowerMsg := strings.ToLower(message)
	if strings.Contains(lowerMsg, "database") || strings.Contains(lowerMsg, "mongo") || strings.Contains(lowerMsg, "query") {
		w.Header().Set("X-Error-Type", "DATABASE_ERROR")
	}

	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "error",
		"message": message,
	})
}

// エラーコード付きのエラーレスポンスを返すヘルパー関数
//   - クライアント側が「静かに復旧すべき状況」と「ユーザーに通知すべき状況」を区別できるようにするため、
//     人間向けのmessageとは別に機械可読なcodeを返す
func respondWithErrorCode(w http.ResponseWriter, code, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "error",
		"code":    code,
		"message": message,
	})
}
