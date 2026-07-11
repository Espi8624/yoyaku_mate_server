package metrics

import (
	"net/http"
	"strings"
	"time"
	"yoyaku_mate_server/models"
)

// - HTTPレスポンスのステータスコードを傍受して監視するためのカスタムResponseWriterラッパー構造体
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

// - HTTPレスポンスヘッダーを書き込む際にステータスコードを傍受し、構造体内部に保持する
func (rw *responseWriterWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// - SSE（Server-Sent Events）などのリアルタイムストリーミング通信において、データを即座にフラッシュするためにhttp.Flusherインターフェースを実装する
func (rw *responseWriterWrapper) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// - すべてのAPIリクエストのレスポンスを監視し、400 Bad Requestまたは500 Internal Server Errorの発生を自動で検知するミドルウェア
func ErrorCaptureMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &responseWriterWrapper{w, http.StatusOK}
		
		next.ServeHTTP(rw, r)

		if rw.statusCode >= 400 {
			var errType string
			customErrType := rw.Header().Get("X-Error-Type")
			if customErrType != "" {
				errType = customErrType
			} else if rw.statusCode >= 500 {
				errType = "500_INTERNAL_ERROR"
			} else {
				errType = "400_BAD_REQUEST"
			}

			clientIP := r.Header.Get("X-Forwarded-For")
			if clientIP == "" {
				clientIP = r.RemoteAddr
			} else {
				ips := strings.Split(clientIP, ",")
				clientIP = strings.TrimSpace(ips[0])
			}

			GetTracker().RecordError(models.ErrorLog{
				Timestamp: time.Now().UTC(),
				ErrorType: errType,
				Message:   http.StatusText(rw.statusCode), 
				Path:      r.URL.Path,
				Method:    r.Method,
				ClientIP:  clientIP,
			})
		}
	})
}
