# ハンドラーおよびミドルウェアの依存性注入 (DI) リファクタリング

> 最終更新: 2026-08-10

## 背景および問題点

初期の開発フェーズでは、実装のスピードを優先したため、HTTPハンドラー内で直接データベースのRepositoryインスタンスを生成してメソッドを呼び出す方式を取っていました。

```go
// AS-IS: ハンドラー内部での暗黙的な依存関係の生成
func StoreInfoHandler(w http.ResponseWriter, r *http.Request) {
    // グローバルに近い形で直接インスタンス化
    store, err := (&data.MongoStoreRepo{}).GetStoreData(storeID)
    // ...
}
```

このアプローチは以下の問題を引き起こしました：
1. **グローバルステートへの依存**: ハンドラーが具体的なDB実装（MongoDB）に強く結合していました。
2. **テストが困難**: `MongoStoreRepo`をモック（Mock）に差し替えることができず、ハンドラーのユニットテストがDB接続なしでは不可能でした。
3. **コードの複雑化**: ミドルウェア（`RequireAuthMiddleware`など）でも同様に内部でインスタンス化を行っており、アーキテクチャの境界線が曖昧になっていました。

## 解決策：依存性の注入（Dependency Injection）とリポジトリパターンの適用

すべてのハンドラーと主要なミドルウェアをリファクタリングし、**外部（`main.go`）から依存オブジェクトを注入する（Dependency Injection）**構造へと変更しました。

### 1. インターフェースの定義と構造体への変更
各ハンドラーファイル内に必要なメソッドのみを定義したインターフェースを作成し、ハンドラーを構造体（Struct）として定義しました。

```go
// TO-BE: インターフェースを通じた依存性の注入
type AIContextStoreRepository interface {
    GetStoreData(storeID string) (*models.Store, error)
    GetSettings(storeID string) (*models.StoreSetting, error)
}

type StoreAIContextHandler struct {
    storeRepo AIContextStoreRepository
}

func NewStoreAIContextHandler(repo AIContextStoreRepository) *StoreAIContextHandler {
    return &StoreAIContextHandler{storeRepo: repo}
}
```

### 2. ミドルウェアのファクトリ関数化
`RequireAuthMiddleware`のような認証ミドルウェアも、実行時に依存オブジェクトを受け取れるようにクロージャ（Closure）を用いたファクトリ関数パターンへ変更しました。

```go
// TO-BE: リポジトリを注入可能なミドルウェア
func RequireAuthMiddleware(repo MiddlewareUserRepository) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // repo.GetByFirebaseUID() を使用
        })
    }
}
```

### 3. `main.go` での中央集権的な配線（Wiring）
すべてのRepository（MongoDBの実装体）は `main.go` の起動時に一度だけインスタンス化され、各ハンドラーへパラメーターとして渡されます。その後、ハンドラーは `router.go` に登録されます。

## 期待される効果 (Benefits)

1. **完全な関心の分離 (Separation of Concerns)**: HTTPレイヤーはHTTPのパースとレスポンスのみを担当し、データアクセスは注入されたインターフェースに委譲されます。
2. **テスタビリティの向上 (High Testability)**: データベース接続を必要としない純粋なHTTPハンドラーのユニットテストが可能になりました。
3. **高い拡張性**: 今後MongoDBからPostgreSQL等に移行する場合でも、`handlers` パッケージのコードを一行も変更することなく、`data` パッケージの実装を差し替えるだけで対応可能です。
