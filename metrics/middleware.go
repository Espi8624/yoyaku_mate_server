package metrics

import (
	"net"
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

// - すべてのAPIリクエストの応答時間を測定し、詳細リクエストログとエラーログを収集してトラッカーへ伝達するミドルウェア
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriterWrapper{w, http.StatusOK}
		
		next.ServeHTTP(rw, r)

		duration := time.Since(start).Milliseconds()

		clientIP := r.Header.Get("X-Forwarded-For")
		if clientIP == "" {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err == nil {
				clientIP = ip
			} else {
				clientIP = r.RemoteAddr
			}
		} else {
			ips := strings.Split(clientIP, ",")
			clientIP = strings.TrimSpace(ips[0])
		}

		// Normalize IPv6 loopback to IPv4 loopback for local dev consistency
		if clientIP == "::1" {
			clientIP = "127.0.0.1"
		}

		// - リクエストトラッカーにすべてのAPIリクエストデータを記録
		GetRequestTracker().RecordRequest(models.RequestLog{
			Timestamp:    time.Now().UTC(),
			Path:         r.URL.Path,
			Method:       r.Method,
			StatusCode:   rw.statusCode,
			ResponseTime: duration,
			ClientIP:     clientIP,
		})

		// - エラー応答(4xx/5xx)の発生時、既存のエラートラッカーにも記録
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
