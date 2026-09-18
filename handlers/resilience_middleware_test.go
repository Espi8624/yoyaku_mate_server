package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// - 障害時の振る舞いは平常時のテストでは一切踏まれないため、ここで固定しておく。
//   壊れても気づけるのはこのテストだけになる

// TestRecoverMiddleware ハンドラ内のpanicが500応答に変換されることを確認する
// - net/http既定のrecoverは応答を書かずに接続を閉じるだけで、クライアントからは
//   500ではなく通信エラーに見える。監視上もエラーとして計上されない
func TestRecoverMiddleware(t *testing.T) {
	t.Run("panicは500応答になる", func(t *testing.T) {
		h := RecoverMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var repo *struct{ Name string }
			_ = repo.Name // nil参照 = リポジトリ層で起きるのと同じ形のpanic
		}))

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/waiting-list", nil))

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("500が返っていない: status=%d", rec.Code)
		}
		// - スタックトレースが応答本文に混ざると内部構造が外へ漏れる
		if strings.Contains(rec.Body.String(), "goroutine") {
			t.Errorf("応答本文にスタックトレースが含まれている: %s", strings.TrimSpace(rec.Body.String()))
		}
	})

	t.Run("正常なハンドラには干渉しない", func(t *testing.T) {
		h := RecoverMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("ok"))
		}))

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/waiting-list", nil))

		if rec.Code != http.StatusCreated || rec.Body.String() != "ok" {
			t.Errorf("正常応答が変化した: status=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("応答開始後のpanicは500で上書きしない", func(t *testing.T) {
		// - SSEはヘッダ送出とFlushを済ませてから配信を続ける。そこに500を被せようとしても
		//   応答は変わらず "superfluous response.WriteHeader" のログが出るだけになる
		h := RecoverMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: {}\n\n"))
			w.(http.Flusher).Flush() // ラッパーがFlusherを実装していないとここでpanicする
			panic("配信中の障害")
		}))

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/waiting-list/stream", nil))

		if rec.Code != http.StatusOK {
			t.Errorf("開始済みの応答が書き換えられた: status=%d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "data: {}") {
			t.Errorf("配信済みの本文が失われた: %s", rec.Body.String())
		}
	})

	t.Run("ErrAbortHandlerは握り潰さない", func(t *testing.T) {
		// - net/httpの規約上「意図的な中断」の合図であり、異常ではない。
		//   ここで500に変換すると、意図した中断が障害として記録されてしまう
		defer func() {
			if rec := recover(); rec != http.ErrAbortHandler {
				t.Errorf("ErrAbortHandlerが素通りしていない: %v", rec)
			}
		}()

		h := RecoverMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic(http.ErrAbortHandler)
		}))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/waiting-list", nil))
	})
}

// TestRequireDatabaseMiddleware DB未接続時にAPIが503で止まることを確認する
// - テスト環境ではMongoDBに接続しないため db.IsReady() は常にfalse。
//   つまりここで検証できるのは「未接続時の振る舞い」であり、それがまさに確認したい側
func TestRequireDatabaseMiddleware(t *testing.T) {
	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	t.Run("DB未接続時のAPIリクエストは503で止まる", func(t *testing.T) {
		reached = false
		rec := httptest.NewRecorder()
		RequireDatabaseMiddleware(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/waiting-list", nil))

		if reached {
			t.Fatal("DB未接続なのにハンドラへ到達した (nil参照でpanicする経路)")
		}
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("503が返っていない: status=%d", rec.Code)
		}
		// - クライアントが再試行の目安を判断できるようにする
		if rec.Header().Get("Retry-After") == "" {
			t.Error("Retry-Afterヘッダが付いていない")
		}
		// - エラーダッシュボードでDB障害として分類されるためのヘッダ (utils.RespondWithErrorが付与)
		if got := rec.Header().Get("X-Error-Type"); got != "DATABASE_ERROR" {
			t.Errorf("X-Error-Typeが DATABASE_ERROR でない: %q", got)
		}
	})

	t.Run("/health は対象外", func(t *testing.T) {
		// - 死活監視には「実際にPingが通るか」を答えさせる必要がある。
		//   その手前で503を返すと、DBの状態に関わらず常に同じ応答になってしまう
		reached = false
		rec := httptest.NewRecorder()
		RequireDatabaseMiddleware(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

		if !reached {
			t.Errorf("/health が遮られた: status=%d", rec.Code)
		}
	})

	t.Run("CORSプリフライトは対象外", func(t *testing.T) {
		// - ここで503を返すと、ブラウザには本来のエラーではなくCORSエラーとして表示される
		reached = false
		rec := httptest.NewRecorder()
		RequireDatabaseMiddleware(next).ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "/api/waiting-list", nil))

		if !reached {
			t.Errorf("プリフライトが遮られた: status=%d", rec.Code)
		}
	})
}
