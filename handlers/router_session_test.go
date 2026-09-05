package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"yoyaku_mate_server/models"

	"github.com/gorilla/mux"
)

// - セッション検証ミドルウェアが「付いているべきルート」と「付いていてはいけないルート」を
//   取り違えると、顧客ウェブが壊れる/点主アプリの保護が抜ける、のどちらかが起きる。
//   ルート再配置の意図をテストで固定しておく

type stubMiddlewareUserRepo struct{}

func (s *stubMiddlewareUserRepo) GetByFirebaseUID(uid string) (*models.User, error) {
	return nil, nil
}

type stubMiddlewareSessionRepo struct{}

func (s *stubMiddlewareSessionRepo) FindBySessionID(sessionID string) (*models.Session, error) {
	return nil, nil
}

func (s *stubMiddlewareSessionRepo) TouchLastSeen(session *models.Session) error { return nil }

// newTestRouter ルーティング定義のみを組み立てる (ハンドラ本体はnil)
// - 保護されたルートはミドルウェアが先に応答するためハンドラには到達しない
// - 公開ルートはハンドラに到達してnil参照でパニックする。それ自体が
//   「ミドルウェアに遮られていない」証拠になるため、テスト側でrecoverして判定する
func newTestRouter() *mux.Router {
	r := mux.NewRouter()
	RegisterRoutes(
		r,
		&stubMiddlewareUserRepo{},
		&stubMiddlewareSessionRepo{},
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	return r
}

// serveResult ルートにリクエストを通した結果
type serveResult struct {
	status        int
	body          string
	reachedRouter bool // ハンドラに到達した (nil参照でパニックした) か
}

func serve(r *mux.Router, method, path string) (res serveResult) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)

	defer func() {
		if rec := recover(); rec != nil {
			// - ハンドラ本体に到達した = ミドルウェアに遮られていない
			res.reachedRouter = true
		}
	}()

	r.ServeHTTP(rec, req)
	res.status = rec.Code
	res.body = rec.Body.String()
	return res
}

// TestFullyPublicRoutesReachHandler
// 顧客ウェブ・ゲストが認証なしで叩くルートが、認証で弾かれずハンドラまで到達することを確認する
func TestFullyPublicRoutesReachHandler(t *testing.T) {
	r := newTestRouter()

	cases := []struct {
		method string
		path   string
		desc   string
	}{
		{http.MethodGet, "/api/menu-list", "顧客ウェブのメニュー表示"},
		{http.MethodGet, "/api/store_settings", "顧客ウェブの店舗設定取得"},
		{http.MethodGet, "/api/provider_store", "顧客ウェブの店舗情報取得"},
		{http.MethodGet, "/api/waiting-list", "ゲストの待機リスト参照"},
		{http.MethodGet, "/api/waiting-list/stream", "SSE購読"},
		{http.MethodGet, "/api/waiting-list/poll", "ポーリング"},
	}

	for _, c := range cases {
		var match mux.RouteMatch
		req := httptest.NewRequest(c.method, c.path, nil)
		if !r.Match(req, &match) {
			t.Errorf("%s %s (%s): ルートが一致しない", c.method, c.path, c.desc)
			continue
		}

		// - ハンドラに到達すれば、nil参照でパニックするか、クエリパラメータ検証で400を返す。
		//   いずれも「認証で遮られていない」ことの証拠。401ならミドルウェアに弾かれている
		res := serve(r, c.method, c.path)
		if !res.reachedRouter && res.status == http.StatusUnauthorized {
			t.Errorf("%s %s (%s): 公開ルートが401で弾かれた (body=%s)",
				c.method, c.path, c.desc, strings.TrimSpace(res.body))
		}
	}
}

// TestAuthOnlyRoutesAreNotSessionProtected
// Firebase認証は必要だがセッション検証を課してはいけないルートを確認する
// - これらにセッション検証を掛けると、セッションを確立する手段が無い状態で
//   セッションを要求することになり、新規登録・ログイン導線が成立しなくなる
func TestAuthOnlyRoutesAreNotSessionProtected(t *testing.T) {
	r := newTestRouter()

	cases := []struct {
		method string
		path   string
		desc   string
	}{
		{http.MethodPost, "/api/auth/signup", "会員登録"},
		{http.MethodPost, "/api/stores/add", "登録直後の店舗作成"},
		{http.MethodPost, "/api/stores/join", "登録直後の店舗参加"},
		{http.MethodGet, "/api/provider_user/firebase_uid", "セッション確立前のプロフィール取得"},
		{http.MethodPost, "/api/auth/session", "セッション発行そのもの"},
	}

	for _, c := range cases {
		res := serve(r, c.method, c.path)
		// - 認証ヘッダが無いため401自体は正常。ただしそれが「セッションが無い」ことを
		//   理由とした401であってはならない
		if strings.Contains(res.body, ErrCodeSessionRequired) ||
			strings.Contains(res.body, ErrCodeSessionRevoked) {
			t.Errorf("%s %s (%s): セッション検証が掛かっている。body=%s",
				c.method, c.path, c.desc, strings.TrimSpace(res.body))
		}
	}
}

// TestRequireSessionMiddleware セッションミドルウェア単体の挙動を確認する
// - ルーター経由の検証は認証ミドルウェアが先に401を返すため、セッション層まで到達しない。
//   (到達させるには有効なFirebaseトークンが必要でテストでは用意できない)
//   そのため、認証済みユーザーをcontextに入れた状態でミドルウェア単体を検証する
func TestRequireSessionMiddleware(t *testing.T) {
	authedRequest := func(sessionID string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/provider_menu", nil)
		if sessionID != "" {
			req.Header.Set("X-Session-Id", sessionID)
		}
		ctx := context.WithValue(req.Context(), ContextKeyUser, &models.User{})
		return req.WithContext(ctx)
	}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	t.Run("有効なセッションは通過する", func(t *testing.T) {
		nextCalled = false
		rec := httptest.NewRecorder()
		RequireSessionMiddleware(&activeSessionRepo{})(next).ServeHTTP(rec, authedRequest("valid-token"))

		if !nextCalled {
			t.Errorf("有効なセッションが弾かれた (status=%d, body=%s)",
				rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	})

	t.Run("無効化済みセッションは通過させない", func(t *testing.T) {
		nextCalled = false
		rec := httptest.NewRecorder()
		RequireSessionMiddleware(&revokedSessionRepo{})(next).ServeHTTP(rec, authedRequest("revoked-token"))

		if nextCalled {
			t.Fatal("無効化済みセッションが通過してしまった")
		}
		if !strings.Contains(rec.Body.String(), ErrCodeSessionRevoked) {
			t.Errorf("SESSION_REVOKED が返っていない: %s", strings.TrimSpace(rec.Body.String()))
		}
	})

	t.Run("CORSプリフライトは検証対象外", func(t *testing.T) {
		nextCalled = false
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/api/provider_menu", nil)
		RequireSessionMiddleware(&revokedSessionRepo{})(next).ServeHTTP(rec, req)

		if !nextCalled {
			t.Error("プリフライトが弾かれた")
		}
	})
}

// activeSessionRepo 有効なセッションを返すスタブ
type activeSessionRepo struct{}

func (s *activeSessionRepo) FindBySessionID(sessionID string) (*models.Session, error) {
	return &models.Session{SessionID: sessionID, LastSeenAt: time.Now()}, nil
}

func (s *activeSessionRepo) TouchLastSeen(session *models.Session) error { return nil }

// TestProviderRoutesAreSessionProtected 点主アプリ専用ルートが保護されていることを確認する
// - 認証ヘッダ無しでは必ず401で止まり、ハンドラに到達してはいけない
// - なお認証ミドルウェアが先に401を返すため、このテストはセッション層の有無までは判別しない。
//   セッション層の挙動は TestRequireSessionMiddleware が担保する
func TestProviderRoutesAreSessionProtected(t *testing.T) {
	r := newTestRouter()

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/provider_menu"},
		{http.MethodGet, "/api/statistics"},
		{http.MethodGet, "/api/provider_stores/store-list"},
		{http.MethodGet, "/api/provider_store/license"},
		{http.MethodPost, "/api/menu-list"},
		{http.MethodPut, "/api/provider_store"},
		{http.MethodPut, "/api/store_settings"},
		{http.MethodGet, "/api/provider_user"},
		{http.MethodGet, "/api/stores/abc/staff"},
		{http.MethodGet, "/api/stores/abc/shift-tables/2026-01-05"},
		{http.MethodPost, "/api/stores/abc/shift-tables/2026-01-05/change-requests"},
	}

	for _, c := range cases {
		res := serve(r, c.method, c.path)
		if res.reachedRouter {
			t.Errorf("%s %s: 保護されておらずハンドラに到達した", c.method, c.path)
			continue
		}
		if res.status != http.StatusUnauthorized {
			t.Errorf("%s %s: 401で止まらなかった (status=%d)", c.method, c.path, res.status)
		}
	}
}

// TestSessionErrorCodesAreDistinguished
// セッション未確立(SESSION_REQUIRED)と無効化(SESSION_REVOKED)が別コードで返ることを確認する
// - ここを1つのコードにまとめると、再インストール等で静かに復旧できる場面でも
//   「他端末でログインされました」と誤って表示され、誤検知の原因になる
func TestSessionErrorCodesAreDistinguished(t *testing.T) {
	user := &models.User{}

	t.Run("セッションヘッダ無しはSESSION_REQUIRED", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/provider_menu", nil)

		if VerifySessionForUser(rec, req, &stubMiddlewareSessionRepo{}, user) {
			t.Fatal("セッション無しで検証が通ってしまった")
		}
		if !strings.Contains(rec.Body.String(), ErrCodeSessionRequired) {
			t.Errorf("SESSION_REQUIRED が返っていない: %s", strings.TrimSpace(rec.Body.String()))
		}
	})

	t.Run("未知のセッショントークンもSESSION_REQUIRED", func(t *testing.T) {
		// - 端末の再インストール等でローカルのトークンが失われたケース。
		//   奪われたわけではないので、ユーザーに通知せず再発行を促すコードでなければならない
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/provider_menu", nil)
		req.Header.Set("X-Session-Id", "unknown-token")

		if VerifySessionForUser(rec, req, &stubMiddlewareSessionRepo{}, user) {
			t.Fatal("未知のトークンで検証が通ってしまった")
		}
		if !strings.Contains(rec.Body.String(), ErrCodeSessionRequired) {
			t.Errorf("SESSION_REQUIRED が返っていない: %s", strings.TrimSpace(rec.Body.String()))
		}
	})

	t.Run("無効化済みセッションはSESSION_REVOKED", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/provider_menu", nil)
		req.Header.Set("X-Session-Id", "revoked-token")

		if VerifySessionForUser(rec, req, &revokedSessionRepo{}, user) {
			t.Fatal("無効化済みセッションで検証が通ってしまった")
		}
		if !strings.Contains(rec.Body.String(), ErrCodeSessionRevoked) {
			t.Errorf("SESSION_REVOKED が返っていない: %s", strings.TrimSpace(rec.Body.String()))
		}
	})
}

// revokedSessionRepo 他端末のログインによって無効化されたセッションを返すスタブ
type revokedSessionRepo struct{}

func (s *revokedSessionRepo) FindBySessionID(sessionID string) (*models.Session, error) {
	revokedAt := time.Now()
	return &models.Session{
		SessionID:     sessionID,
		RevokedAt:     &revokedAt,
		RevokedReason: models.SessionRevokedReasonNewLogin,
	}, nil
}

func (s *revokedSessionRepo) TouchLastSeen(session *models.Session) error { return nil }
