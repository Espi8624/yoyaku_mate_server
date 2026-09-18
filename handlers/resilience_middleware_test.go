package handlers

import (
	"bytes"
	"io"
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

// TestLimitRequestBodyMiddleware リクエストボディの上限が効くことを確認する
//
// 上限が無いと1リクエストのデコードだけでメモリを食い潰せる。
// shared-cpu-1x / 256MB の1台構成では、OOMは「静かな再起動」として現れ、
// 全店舗のSSE接続が同時に切れる
func TestLimitRequestBodyMiddleware(t *testing.T) {
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			// - MaxBytesReader は読み取り時にエラーを返す。ハンドラから見ると
			//   デコード失敗として現れる経路
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	post := func(path string, size int) *http.Request {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(make([]byte, size)))
		req.ContentLength = int64(size)
		return req
	}

	t.Run("上限内のJSONは通る", func(t *testing.T) {
		rec := httptest.NewRecorder()
		LimitRequestBodyMiddleware(echo).ServeHTTP(rec, post("/api/waiting-list", 1024))
		if rec.Code != http.StatusOK {
			t.Errorf("通常のリクエストが弾かれた: status=%d", rec.Code)
		}
	})

	t.Run("上限超過のJSONは413で止まる", func(t *testing.T) {
		// - Content-Lengthが分かっている場合は読む前に弾く。
		//   最後まで受け取ってから弾くのでは帯域と時間を浪費する
		rec := httptest.NewRecorder()
		LimitRequestBodyMiddleware(echo).ServeHTTP(rec, post("/api/waiting-list", maxJSONBodyBytes+1))
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("413が返っていない: status=%d", rec.Code)
		}
	})

	t.Run("アップロードはJSONより大きい上限が使われる", func(t *testing.T) {
		// - ハンドラ側の ParseMultipartForm(10MiB) を通せる余地を残す。
		//   JSONと同じ上限にすると正規の画像アップロードが壊れる
		size := maxJSONBodyBytes + 1
		for _, path := range []string{
			"/api/stores/upload-license",
			"/api/provider_user/image",
			"/api/provider_menu/abc/image",
		} {
			rec := httptest.NewRecorder()
			LimitRequestBodyMiddleware(echo).ServeHTTP(rec, post(path, size))
			if rec.Code != http.StatusOK {
				t.Errorf("%s: 正規のアップロードが弾かれた: status=%d", path, rec.Code)
			}
		}
	})

	t.Run("アップロードでも上限を超えれば413", func(t *testing.T) {
		rec := httptest.NewRecorder()
		LimitRequestBodyMiddleware(echo).ServeHTTP(rec, post("/api/provider_user/image", maxUploadBodyBytes+1))
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("413が返っていない: status=%d", rec.Code)
		}
	})

	t.Run("Content-Lengthを詐称してもMaxBytesReaderが止める", func(t *testing.T) {
		// - chunkedや詐称でContent-Lengthが当てにならない場合の受け皿。
		//   ここが無いと、上の事前チェックを迂回して無制限に読めてしまう
		body := bytes.NewReader(make([]byte, maxJSONBodyBytes+1))
		req := httptest.NewRequest(http.MethodPost, "/api/waiting-list", body)
		req.ContentLength = -1 // 不明として扱わせる

		rec := httptest.NewRecorder()
		LimitRequestBodyMiddleware(echo).ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Error("上限を超えたボディが最後まで読めてしまった")
		}
	})

	t.Run("GETは対象外", func(t *testing.T) {
		// - SSE(GET)は接続が長く、ここでラップする意味が無い
		rec := httptest.NewRecorder()
		LimitRequestBodyMiddleware(echo).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/waiting-list/stream", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GETが弾かれた: status=%d", rec.Code)
		}
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
