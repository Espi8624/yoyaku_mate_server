package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"
)

// mockUploadStoreRepo UploadStoreRepositoryのテスト用モック実装
type mockUploadStoreRepo struct {
	updatedStoreID string
}

func (m *mockUploadStoreRepo) UpdateStoreImageURL(storeID string, imageURL string) (*models.Store, error) {
	return nil, nil
}

func (m *mockUploadStoreRepo) UpdateLicenseInfoAfterUpload(storeID string, imageURL string) (*models.StoreLicense, error) {
	m.updatedStoreID = storeID
	return &models.StoreLicense{
		StoreID:            storeID,
		LicenseImageURL:    imageURL,
		VerificationStatus: models.StatusPendingReview,
	}, nil
}

// - 営業許可証アップロード成功時に、審査待ち通知がSlackへ送信されることを検証する
// - 通知漏れの回帰防止: これが壊れると管理者が新規申請の発生に気づけなくなる
func TestUploadLicense_NotifiesSlackOnSuccess(t *testing.T) {
	var receivedCount int32
	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&receivedCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer slackServer.Close()

	storeRepo := &mockUploadStoreRepo{}
	h := &UploadHandler{
		Minio:           &data.MinioClient{},
		StoreRepo:       storeRepo,
		BizBucketName:   "test-biz-bucket",
		SlackWebhookURL: slackServer.URL,
	}

	body, contentType := buildLicenseUploadRequestBody(t, "store-123")
	req := httptest.NewRequest(http.MethodPost, "/api/stores/upload-license", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	h.UploadLicense(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if storeRepo.updatedStoreID != "store-123" {
		t.Fatalf("expected UpdateLicenseInfoAfterUpload to be called with store-123, got %q", storeRepo.updatedStoreID)
	}

	// - Slack送信はgoroutineで非同期実行されるため、少し待ってから確認する
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&receivedCount) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if atomic.LoadInt32(&receivedCount) != 1 {
		t.Fatalf("expected 1 Slack notification, got %d", receivedCount)
	}
}

// buildLicenseUploadRequestBody テスト用のmultipart/form-dataリクエストボディを生成する
// - アップロードされたファイルは./uploadsに実体が作成されるため、テスト終了時に削除する
func buildLicenseUploadRequestBody(t *testing.T, storeID string) (*bytes.Buffer, string) {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	if err := writer.WriteField("storeId", storeID); err != nil {
		t.Fatalf("failed to write storeId field: %v", err)
	}

	part, err := writer.CreateFormFile("licenseImage", "license.txt")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := part.Write([]byte("dummy license content")); err != nil {
		t.Fatalf("failed to write file content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	t.Cleanup(func() {
		entries, err := os.ReadDir("./uploads")
		if err != nil {
			return
		}
		cutoff := time.Now().Add(-1 * time.Minute)
		for _, entry := range entries {
			info, err := entry.Info()
			if err == nil && info.ModTime().After(cutoff) {
				os.Remove(filepath.Join("./uploads", entry.Name()))
			}
		}
	})

	return body, writer.FormDataContentType()
}
