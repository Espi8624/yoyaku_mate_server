package utils

import (
	"encoding/json"
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

// IsValidWaitingID는 웨이팅 ID의 형식이 올바른지 검증합니다
// 예상 형식: "YYYYMMDD-HHMMSS" (예: "20250616-143022")
func IsValidWaitingID(id string) bool {
	if len(id) != 15 {
		return false
	}

	// "-" 위치 확인
	if id[8] != '-' {
		return false
	}

	// 날짜와 시간 부분이 숫자인지 확인
	dateTime := id[:8] + id[9:]
	for _, ch := range dateTime {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	// 날짜 유효성 검사
	month := id[4:6]
	day := id[6:8]
	hour := id[9:11]
	minute := id[11:13]
	second := id[13:15]

	// 기본적인 범위 검사
	if month < "01" || month > "12" ||
		day < "01" || day > "31" ||
		hour < "00" || hour > "23" ||
		minute < "00" || minute > "59" ||
		second < "00" || second > "59" {
		return false
	}

	return true
}
