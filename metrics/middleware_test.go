package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"yoyaku_mate_server/models"
)

// mockRequestRecorder RequestRecorderインターフェースのテスト用モック実装
// - 呼び出された引数をそのままメモリに蓄積し、テスト側でアサーションできるようにする
type mockRequestRecorder struct {
	recordedLogs []models.RequestLog
}

func (m *mockRequestRecorder) RecordRequest(reqLog models.RequestLog) {
	m.recordedLogs = append(m.recordedLogs, reqLog)
}

// mockErrorRecorder ErrorRecorderインターフェースのテスト用モック実装
type mockErrorRecorder struct {
	recordedLogs []models.ErrorLog
}

func (m *mockErrorRecorder) RecordError(errLog models.ErrorLog) {
	m.recordedLogs = append(m.recordedLogs, errLog)
}

// - extractClientIP()의 다양한 요청 형태에 대한 동작을 검증하는 table-driven 테스트
// - 002-active-user-ip-port-issue 트러블슈팅에서 발견된 포트 미제거/IPv6 중복 카운트 버그의 회귀 방지 목적
func TestExtractClientIP(t *testing.T) {
	testCases := []struct {
		name           string
		remoteAddr     string
		forwardedFor   string
		expectedResult string
	}{
		{
			name:           "RemoteAddr에서 포트가 정상적으로 제거된다",
			remoteAddr:     "192.168.1.10:54321",
			forwardedFor:   "",
			expectedResult: "192.168.1.10",
		},
		{
			name:           "동일 IP라도 임시 포트가 매번 달라도 같은 결과로 정규화된다",
			remoteAddr:     "192.168.1.10:9999",
			forwardedFor:   "",
			expectedResult: "192.168.1.10",
		},
		{
			name:           "IPv6 루프백(::1)은 IPv4 루프백(127.0.0.1)으로 정규화된다",
			remoteAddr:     "[::1]:54321",
			forwardedFor:   "",
			expectedResult: "127.0.0.1",
		},
		{
			name:           "IPv4 루프백은 그대로 유지된다",
			remoteAddr:     "127.0.0.1:12345",
			forwardedFor:   "",
			expectedResult: "127.0.0.1",
		},
		{
			name:           "포트가 없는 RemoteAddr은 SplitHostPort 실패 시 원본 그대로 사용된다",
			remoteAddr:     "192.168.1.10",
			forwardedFor:   "",
			expectedResult: "192.168.1.10",
		},
		{
			name:           "X-Forwarded-For 헤더가 있으면 RemoteAddr보다 우선한다",
			remoteAddr:     "10.0.0.1:8080",
			forwardedFor:   "203.0.113.5",
			expectedResult: "203.0.113.5",
		},
		{
			name:           "X-Forwarded-For에 프록시 체인(콤마 구분)이 있으면 첫 번째 IP를 사용한다",
			remoteAddr:     "10.0.0.1:8080",
			forwardedFor:   "203.0.113.5, 70.41.3.18, 150.172.238.178",
			expectedResult: "203.0.113.5",
		},
		{
			name:           "X-Forwarded-For 값 앞뒤 공백은 트림된다",
			remoteAddr:     "10.0.0.1:8080",
			forwardedFor:   "  203.0.113.5  ,  70.41.3.18",
			expectedResult: "203.0.113.5",
		},
		{
			name:           "X-Forwarded-For로 IPv6 루프백이 직접 온 경우도 정규화된다",
			remoteAddr:     "10.0.0.1:8080",
			forwardedFor:   "::1",
			expectedResult: "127.0.0.1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.forwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tc.forwardedFor)
			}

			result := extractClientIP(req)

			if result != tc.expectedResult {
				t.Errorf("extractClientIP() = %q, expected %q (remoteAddr=%q, forwardedFor=%q)",
					result, tc.expectedResult, tc.remoteAddr, tc.forwardedFor)
			}
		})
	}
}

// - 동일 기기에서 폴링 요청마다 임시 포트가 바뀌어도 중복 카운트가 발생하지 않는지 검증
// - 002-active-user-ip-port-issue.ko.md의 "지속 증가 현상" 재현 시나리오에 대한 회귀 테스트
func TestExtractClientIP_ConsistentAcrossEphemeralPorts(t *testing.T) {
	ports := []string{"51000", "51001", "62345", "8080"}
	var results []string

	for _, port := range ports {
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.RemoteAddr = "127.0.0.1:" + port

		results = append(results, extractClientIP(req))
	}

	for i, result := range results {
		if result != "127.0.0.1" {
			t.Errorf("port %s에서 추출된 IP가 다름: got %q, expected %q", ports[i], result, "127.0.0.1")
		}
	}
}

// - MetricsMiddleware가 mock RequestRecorder에 요청 정보를 정확히 전달하는지 검증
// - 001-di-repository-pattern 컨벤션에 따라 인터페이스 주입 후 실제 트래커 싱글턴 없이 테스트 가능함을 확인
func TestMetricsMiddleware_RecordsRequest(t *testing.T) {
	reqRecorder := &mockRequestRecorder{}
	errRecorder := &mockErrorRecorder{}

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := MetricsMiddleware(reqRecorder, errRecorder)(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/waiting-list", nil)
	req.RemoteAddr = "192.168.1.10:54321"
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if len(reqRecorder.recordedLogs) != 1 {
		t.Fatalf("RecordRequest 호출 횟수 = %d, expected 1", len(reqRecorder.recordedLogs))
	}

	logged := reqRecorder.recordedLogs[0]
	if logged.Path != "/api/waiting-list" {
		t.Errorf("Path = %q, expected %q", logged.Path, "/api/waiting-list")
	}
	if logged.Method != http.MethodGet {
		t.Errorf("Method = %q, expected %q", logged.Method, http.MethodGet)
	}
	if logged.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, expected %d", logged.StatusCode, http.StatusOK)
	}
	if logged.ClientIP != "192.168.1.10" {
		t.Errorf("ClientIP = %q, expected %q", logged.ClientIP, "192.168.1.10")
	}

	if len(errRecorder.recordedLogs) != 0 {
		t.Errorf("200 응답인데 RecordError가 %d번 호출됨, expected 0", len(errRecorder.recordedLogs))
	}
}

// - /api/admin/metrics 하위 경로는 모니터링 폴링용이므로 통계 오염 방지를 위해 기록 대상에서 제외되는지 검증
func TestMetricsMiddleware_ExcludesAdminMetricsPolling(t *testing.T) {
	reqRecorder := &mockRequestRecorder{}
	errRecorder := &mockErrorRecorder{}

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := MetricsMiddleware(reqRecorder, errRecorder)(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/metrics/requests", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if len(reqRecorder.recordedLogs) != 0 {
		t.Errorf("/api/admin/metrics 경로인데 RecordRequest가 %d번 호출됨, expected 0", len(reqRecorder.recordedLogs))
	}
}

// - 4xx/5xx 응답 시 ErrorRecorder에도 함께 기록되는지, 상태 코드에 따라 ErrorType이 올바르게 분류되는지 검증
func TestMetricsMiddleware_RecordsErrorOnFailureStatus(t *testing.T) {
	testCases := []struct {
		name              string
		statusCode        int
		customErrType     string
		expectedErrorType string
	}{
		{
			name:              "500 응답은 500_INTERNAL_ERROR로 분류된다",
			statusCode:        http.StatusInternalServerError,
			expectedErrorType: "500_INTERNAL_ERROR",
		},
		{
			name:              "400 응답은 400_BAD_REQUEST로 분류된다",
			statusCode:        http.StatusBadRequest,
			expectedErrorType: "400_BAD_REQUEST",
		},
		{
			name:              "X-Error-Type 헤더가 있으면 그 값이 우선한다",
			statusCode:        http.StatusInternalServerError,
			customErrType:     "DATABASE_ERROR",
			expectedErrorType: "DATABASE_ERROR",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reqRecorder := &mockRequestRecorder{}
			errRecorder := &mockErrorRecorder{}

			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.customErrType != "" {
					w.Header().Set("X-Error-Type", tc.customErrType)
				}
				w.WriteHeader(tc.statusCode)
			})

			middleware := MetricsMiddleware(reqRecorder, errRecorder)(nextHandler)

			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			if len(errRecorder.recordedLogs) != 1 {
				t.Fatalf("RecordError 호출 횟수 = %d, expected 1", len(errRecorder.recordedLogs))
			}

			if got := errRecorder.recordedLogs[0].ErrorType; got != tc.expectedErrorType {
				t.Errorf("ErrorType = %q, expected %q", got, tc.expectedErrorType)
			}
		})
	}
}
