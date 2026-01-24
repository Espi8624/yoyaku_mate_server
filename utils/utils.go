package utils

import (
	"encoding/json"
	"fmt"
	"net/http"
)

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
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "error",
		"message": message,
	})
}

// IsValidWaitingID는 웨이팅 IDの形式が正しいか検証します
// 許容形式: "YYYYMMDD-HHMMSS" または "YYYYMMDD-HHMMSS-xxxxxx"（ランダム英数字6桁）
func IsValidWaitingID(id string) bool {
	if len(id) == 15 { // 旧形式: YYYYMMDD-HHMMSS
		if id[8] != '-' {
			return false
		}
		dateTime := id[:8] + id[9:]
		for _, ch := range dateTime {
			if ch < '0' || ch > '9' {
				return false
			}
		}
		month := id[4:6]
		day := id[6:8]
		hour := id[9:11]
		minute := id[11:13]
		second := id[13:15]
		if month < "01" || month > "12" ||
			day < "01" || day > "31" ||
			hour < "00" || hour > "23" ||
			minute < "00" || minute > "59" ||
			second < "00" || second > "59" {
			return false
		}
		return true
	}
	if len(id) == 22 && id[8] == '-' && id[15] == '-' { // 新形式: YYYYMMDD-HHMMSS-xxxxxx
		dateTime := id[:8] + id[9:15]
		for _, ch := range dateTime {
			if ch < '0' || ch > '9' {
				return false
			}
		}
		month := id[4:6]
		day := id[6:8]
		hour := id[9:11]
		minute := id[11:13]
		second := id[13:15]
		if month < "01" || month > "12" ||
			day < "01" || day > "31" ||
			hour < "00" || hour > "23" ||
			minute < "00" || minute > "59" ||
			second < "00" || second > "59" {
			return false
		}
		// ランダム英数字6桁チェック
		for _, ch := range id[16:] {
			if !(ch >= '0' && ch <= '9') && !(ch >= 'a' && ch <= 'z') && !(ch >= 'A' && ch <= 'Z') {
				return false
			}
		}
		return true
	}
	return false
}

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
