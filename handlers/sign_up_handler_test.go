package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"
)

// - 店舗作成の入力検証は、以前トランザクション内でエラー文字列の部分一致により
//   ステータスを決めていたため、400のはずが500になる取りこぼしがあった。
//   検証とステータス変換をそれぞれテストで固定する

func validNewStoreRequest() models.SignUpRequest {
	return models.SignUpRequest{
		StoreName:      strPtr("テスト店舗"),
		StoreTelNumber: strPtr("03-1234-5678"),
		StoreAddress:   strPtr("東京都千代田区1-1-1"),
		StoreCategory:  strPtr(models.StoreCategoryOther),
	}
}

// TestValidateNewStoreRequest 店舗作成リクエストの検証を確認する
func TestValidateNewStoreRequest(t *testing.T) {
	t.Run("必須項目が揃っていれば通る", func(t *testing.T) {
		req := validNewStoreRequest()
		if err := validateNewStoreRequest(&req); err != nil {
			t.Errorf("正しいリクエストが弾かれた: %v", err)
		}
	})

	cases := []struct {
		name   string
		mutate func(*models.SignUpRequest)
	}{
		// - 以前は "invalid ..." が "Invalid" の部分一致に掛からず500になっていた
		{"店舗電話番号の形式が不正", func(r *models.SignUpRequest) { r.StoreTelNumber = strPtr("0312345678") }},
		{"店舗電話番号が未指定", func(r *models.SignUpRequest) { r.StoreTelNumber = nil }},
		// - 以前は未指定のままポインタを参照してpanicしていた
		{"店舗住所が未指定", func(r *models.SignUpRequest) { r.StoreAddress = nil }},
		{"店舗住所が空文字", func(r *models.SignUpRequest) { r.StoreAddress = strPtr("") }},
		{"業種が未指定", func(r *models.SignUpRequest) { r.StoreCategory = nil }},
		{"業種が不正", func(r *models.SignUpRequest) { r.StoreCategory = strPtr("UNKNOWN") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := validNewStoreRequest()
			c.mutate(&req)
			if err := validateNewStoreRequest(&req); err == nil {
				t.Error("不正なリクエストが通ってしまった")
			}
		})
	}
}

// TestBuildNewStoreWithoutOptionalFields 任意項目が無くても店舗を組み立てられることを確認する
func TestBuildNewStoreWithoutOptionalFields(t *testing.T) {
	req := validNewStoreRequest()
	store, settings := buildNewStore(&req)

	if store.Address != "東京都千代田区1-1-1" || store.Building != "" {
		t.Errorf("住所の組み立てが想定と違う: address=%q building=%q", store.Address, store.Building)
	}
	if settings.Settings.WaitingPolicy.MaxWaitingCount != 10 {
		t.Errorf("最大待機数の既定値が10になっていない: %d", settings.Settings.WaitingPolicy.MaxWaitingCount)
	}
	if len(settings.Settings.OperatingHours) != 7 {
		t.Errorf("営業時間の既定値が7曜日分ない: %d", len(settings.Settings.OperatingHours))
	}
}

// TestRespondSignUpError リポジトリのエラーがHTTPステータスへ正しく変換されることを確認する
func TestRespondSignUpError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"ユーザー重複", data.ErrUserAlreadyExists, http.StatusConflict},
		{"店舗電話番号の重複", data.ErrStorePhoneExists, http.StatusConflict},
		{"参加先の店舗が無い", data.ErrStoreNotFound, http.StatusNotFound},
		{"ユーザーが無い", data.ErrUserNotFound, http.StatusNotFound},
		// - 以前は文字列判定に掛からず500になっていた
		{"マネージャー以外の店舗追加", data.ErrNotManager, http.StatusForbidden},
		{"ラップされた既知のエラー", fmt.Errorf("transaction: %w", data.ErrStoreNotFound), http.StatusNotFound},
		{"未知のエラー", errors.New("connection reset"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			respondSignUpError(rec, c.err)
			if rec.Code != c.want {
				t.Errorf("status=%d, want %d", rec.Code, c.want)
			}
		})
	}
}
