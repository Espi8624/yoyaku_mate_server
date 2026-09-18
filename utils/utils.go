package utils

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
)

// GenerateRandomString generates a random string of specified length
func GenerateRandomString(n int) string {
	const letters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	ret := make([]byte, n)
	for i := 0; i < n; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			return ""
		}
		ret[i] = letters[num.Int64()]
	}
	return string(ret)
}

// JSON応答を返すヘルパー関数
func RespondWithJSON(w http.ResponseWriter, data interface{}, statusCode int) {
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
func RespondWithError(w http.ResponseWriter, message string, statusCode int) {
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
func RespondWithErrorCode(w http.ResponseWriter, code, message string, statusCode int) {
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

// IsValidWaitingID は削除した。
//
//   - 呼び出し元がどこにも無い死んだコードだった。そして許可していたのは15文字と22文字の
//     2形式だけで、現在クライアントが実際に送っている23文字形式
//     (YYYYMMDD-HHmmss-SSS-NNN、顧客ウェブと点主アプリの双方) を弾く状態になっていた
//   - つまり「後から誰かが呼び出しを繋いだ瞬間、全ての待機登録が400で止まる」地雷だった。
//     呼ばれていないから誰も気づかない、というのが一番たちが悪い
//   - 形式の検証はクライアントが形式を変えるたびに追随が必要で、追随を忘れても
//     (呼ばれていなければ) 気づけない。代わりに handlers 側で長さの上限だけを課している
//     (maxWaitingIDLength)。形式ではなく上限なら、クライアントが変わっても壊れない

// GetIntPointerValue extracts int from pointer or returns default
func GetIntPointerValue(ptr *int, defaultValue int) int {
	if ptr != nil {
		return *ptr
	}
	return defaultValue
}

// GetBoolPointerValue extracts bool from pointer or returns default
func GetBoolPointerValue(ptr *bool, defaultValue bool) bool {
	if ptr != nil {
		return *ptr
	}
	return defaultValue
}

// GetStringPointerValue extracts string from pointer or returns default
func GetStringPointerValue(ptr *string, defaultValue string) string {
	if ptr != nil {
		return *ptr
	}
	return defaultValue
}

// FilterAllowedFields クライアントから受け取った汎用$set用updateマップから、
// 許可されたフィールド名(allowed)に含まれるキーだけを残した新しいマップを返す。
// 汎用PUTエンドポイント(map[string]interface{}をそのまま$setする実装)が
// role・store_id・firebase_uidなど本来クライアントが書き換えてはいけない
// フィールドまで無検証で受け入れてしまうマスアサインメント脆弱性への対策
func FilterAllowedFields(update map[string]interface{}, allowed map[string]bool) map[string]interface{} {
	filtered := make(map[string]interface{}, len(update))
	for key, value := range update {
		if allowed[key] {
			filtered[key] = value
		}
	}
	return filtered
}

// FormatDuration は秒数を "X分Y秒" または "X秒" 形式にフォーマットします
func FormatDuration(seconds int) string {
	if seconds < 0 {
		return "--分"
	}
	min := seconds / 60
	sec := seconds % 60
	if min > 0 {
		return fmt.Sprintf("%d分%d秒", min, sec)
	}
	return fmt.Sprintf("%d秒", sec)
}
