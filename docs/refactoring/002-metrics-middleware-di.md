# MetricsMiddleware の依存性注入 (DI) リファクタリング

> 最終更新: 2026-08-20
> 関連ファイル: [`metrics/middleware.go`](../../metrics/middleware.go), [`metrics/middleware_test.go`](../../metrics/middleware_test.go), [`main.go`](../../main.go)
> 関連ドキュメント: [001-di-repository-pattern](./001-di-repository-pattern.md), [トラブルシューティング: 002-active-user-ip-port-issue](../troubles/002-active-user-ip-port-issue.md)

## 背景および問題点

`001-di-repository-pattern` リファクタリングで各ハンドラーと `RequireAuthMiddleware` はインターフェース注入構造へ移行しましたが、すべてのAPIリクエストを傍受する `MetricsMiddleware` はその対象から外れていました。

```go
// AS-IS: ミドルウェア内部でトラッカーのシングルトンを直接呼び出す
func MetricsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // ...
        GetRequestTracker().RecordRequest(models.RequestLog{ /* ... */ })
        // ...
        GetTracker().RecordError(models.ErrorLog{ /* ... */ })
    })
}
```

この構造には以下の問題がありました:
1. **テスト不可能なコアロジック**: 実接続者の重複カウントバグ（[トラブルシューティング 002](../troubles/002-active-user-ip-port-issue.md)）を引き起こしたIP抽出・正規化ロジックがミドルウェアのクロージャ内に閉じ込められており、`httptest` でリクエストを流しても、実際の `RequestTracker`/`ErrorTracker` シングルトン（goroutine、MongoDBバッチ保存を含む）まで一緒に起動しない限り検証する手段がありませんでした。
2. **シングルトンへの強結合**: `GetRequestTracker()` / `GetTracker()` をコード内部で直接呼び出しており、Mockへの差し替えができませんでした。
3. **回帰防止の空白**: 実際に過去の障害を引き起こしたロジックであるにもかかわらず、回帰テストを仕込める構造になっていませんでした。

## 解決策: インターフェース分離 + 純粋関数の抽出

`001-di-repository-pattern` と同じ規約（実行時に依存オブジェクトを受け取るファクトリ関数パターン）をミドルウェアにも適用しました。

### 1. IP抽出ロジックを純粋関数として分離

```go
// - リクエストからクライアントの実IPアドレスを抽出する純粋関数
func extractClientIP(r *http.Request) string {
    clientIP := r.Header.Get("X-Forwarded-For")
    // ... X-Forwarded-Forのパース、RemoteAddrのポート除去、IPv6ループバック正規化
    return clientIP
}
```

### 2. トラッカーをインターフェースとして抽象化

```go
// TO-BE: トラッカーが実装すべき最小限のインターフェースのみを定義
type RequestRecorder interface {
    RecordRequest(reqLog models.RequestLog)
}

type ErrorRecorder interface {
    RecordError(errLog models.ErrorLog)
}
```

`*RequestTracker`、`*ErrorTracker` は既にそれぞれ `RecordRequest`、`RecordError` メソッドを持っていたため、追加のアダプターなしでそのままこのインターフェースを満たします。

### 3. ミドルウェアをファクトリ関数パターンへ変更

```go
// TO-BE: RequireAuthMiddlewareと同じファクトリ関数パターン
func MetricsMiddleware(reqTracker RequestRecorder, errTracker ErrorRecorder) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // reqTracker.RecordRequest(...), errTracker.RecordError(...) を使用
        })
    }
}
```

### 4. `main.go` での明示的な配線（Wiring）

```go
// AS-IS
handler := metrics.MetricsMiddleware(c.Handler(r))

// TO-BE
handler := metrics.MetricsMiddleware(metrics.GetRequestTracker(), metrics.GetTracker())(c.Handler(r))
```

本番環境での動作は従来と100%同一です（引き続き同じシングルトンを使用）。ただし依存関係が関数シグネチャに明示的に現れるようになり、テスト時にはMockへ差し替え可能になりました。

### 5. Mockを活用した単体テスト

```go
type mockRequestRecorder struct {
    recordedLogs []models.RequestLog
}

func (m *mockRequestRecorder) RecordRequest(reqLog models.RequestLog) {
    m.recordedLogs = append(m.recordedLogs, reqLog)
}
```

`metrics/middleware_test.go` に以下のケースを追加しました:
- `extractClientIP()` の単体テスト9種（X-Forwarded-Forの優先順位、プロキシチェーン、IPv6正規化など）
- 一時ポートが毎回変わっても同一IPに正規化されるかを検証する回帰テスト（トラブルシューティング002の再現シナリオ）
- `MetricsMiddleware` 自体に対するMockベースのテスト: 正常リクエストの記録、`/api/admin/metrics` ポーリングの除外、4xx/5xxエラー分類（カスタム `X-Error-Type` ヘッダーを含む）

## 検証

- `go build ./...`、`go vet ./...`、`go test ./...` すべて成功
- `metrics` パッケージ: 単体テスト15種すべて成功（既存0件 → 15件）
- 他のパッケージは従来通り `[no test files]`（今回のリファクタリング対象外）

## 期待される効果 (Benefits)

1. **回帰防止**: 過去に実際の障害（3万円相当ではないもののダッシュボードの信頼性を損なった重複カウントバグ）を引き起こしたロジックが、これでCI上で自動的に検証されるようになりました。
2. **DB・goroutine不要のテスト**: `RequestTracker` のMongoDBバッチ保存ワーカーを起動せずとも、ミドルウェアのルーティング・記録ロジックだけを独立して検証できます。
3. **規約の一貫性**: `001-di-repository-pattern` で確立した「ファクトリ関数による依存性注入」パターンが、サーバー全体のミドルウェアに一貫して適用されました。
