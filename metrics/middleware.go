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

// - リクエストからクライアントの実IPアドレスを抽出する純粋関数
// - X-Forwarded-Forヘッダーを優先し、無ければRemoteAddrからポート番号を除去する
// - IPv6ループバック（::1）はローカル開発環境の一貫性のためIPv4ループバックに正規化する
func extractClientIP(r *http.Request) string {
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

	return clientIP
}

// RequestRecorder すべてのAPIリクエストのログを記録するトラッカーが実装すべきインターフェース
// - *RequestTracker が本番実装。テスト時はMockに差し替え可能
type RequestRecorder interface {
	RecordRequest(reqLog models.RequestLog)
}

// ErrorRecorder エラー応答(4xx/5xx)のログを記録するトラッカーが実装すべきインターフェース
// - *ErrorTracker が本番実装。テスト時はMockに差し替え可能
type ErrorRecorder interface {
	RecordError(errLog models.ErrorLog)
}

// - すべてのAPIリクエストの応答時間を測定し、詳細リクエストログとエラーログを収集してトラッカーへ伝達するミドルウェア
// - /api/admin/metrics 配下はモニタリング用ポーリングのため、ログ記録対象から除外する
// - reqTracker/errTracker は外部（main.go）から注入し、シングルトンへの直接依存を排除してテスト可能にする
func MetricsMiddleware(reqTracker RequestRecorder, errTracker ErrorRecorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// - 管理ダッシュボードのポーリングリクエスト自体が統計を汚染しないよう除外
			if strings.HasPrefix(r.URL.Path, "/api/admin/metrics") {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rw := &responseWriterWrapper{w, http.StatusOK}

			next.ServeHTTP(rw, r)

			duration := time.Since(start).Milliseconds()

			clientIP := extractClientIP(r)

			// - リクエストトラッカーにすべてのAPIリクエストデータを記録
			reqTracker.RecordRequest(models.RequestLog{
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

				errTracker.RecordError(models.ErrorLog{
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
}
