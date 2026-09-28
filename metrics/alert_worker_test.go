package metrics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// - notify()がSlack Webhookへ正しいJSONペイロードをPOSTすることを検証する
func TestAlertWorker_Notify_PostsToWebhook(t *testing.T) {
	var receivedCount int32
	var receivedText string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&receivedCount, 1)
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode webhook payload: %v", err)
		}
		receivedText = body["text"]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	w := &AlertWorker{
		webhookURL: server.URL,
		lastSentAt: make(map[string]time.Time),
	}

	w.notify("test_alert", "test message")

	if atomic.LoadInt32(&receivedCount) != 1 {
		t.Fatalf("expected 1 webhook call, got %d", receivedCount)
	}
	if receivedText != "test message" {
		t.Fatalf("expected message %q, got %q", "test message", receivedText)
	}
}

// - クールダウン期間内は同じアラート種別を再送しないことを検証する(Slackへのスパム防止)
func TestAlertWorker_Notify_RespectsCooldown(t *testing.T) {
	var receivedCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&receivedCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	w := &AlertWorker{
		webhookURL: server.URL,
		lastSentAt: make(map[string]time.Time),
	}

	w.notify("cpu", "1st")
	w.notify("cpu", "2nd (cooldown中なので送信されないはず)")

	if atomic.LoadInt32(&receivedCount) != 1 {
		t.Fatalf("expected cooldown to suppress the 2nd call, got %d webhook calls", receivedCount)
	}

	// - 種別が異なるアラートはクールダウンの影響を受けない
	w.notify("latency", "different alert key")
	if atomic.LoadInt32(&receivedCount) != 2 {
		t.Fatalf("expected a different alert key to bypass cooldown, got %d webhook calls", receivedCount)
	}
}
