package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHealthRouteAcceptsHead 死活監視の HEAD リクエストが405にならないことを確認する
//   - UptimeRobot は既定で HEAD を使う。405になると監視の度にエラーとして記録され、
//     Slackのエラー率アラートが鳴り続けた (2026-10-11〜12)
//   - テスト環境ではDB未接続のため503が返るが、ルートに到達していれば405にはならない
func TestHealthRouteAcceptsHead(t *testing.T) {
	r := newTestRouter()

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, "/health", nil))
		if rec.Code == http.StatusMethodNotAllowed {
			t.Errorf("%s /health が405になった", method)
		}
	}
}
