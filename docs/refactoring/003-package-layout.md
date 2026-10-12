# パッケージ構成の整理 (utils 解体・ドメイン層の切り出し・DB アクセスのリポジトリ集約)

> 最終更新: 2026-10-10
> 関連ファイル: [`shift/`](../../shift/), [`storage/r2.go`](../../storage/r2.go), [`safego/safego.go`](../../safego/safego.go), [`auth/token.go`](../../auth/token.go), [`data/signup_repo.go`](../../data/signup_repo.go), [`data/admin_metrics_repo.go`](../../data/admin_metrics_repo.go), [`data/errors.go`](../../data/errors.go)
> 関連ドキュメント: [001-di-repository-pattern](./001-di-repository-pattern.md), [architecture](../implementation/architecture.md), [shift-table](../implementation/shift-table.md)

## 背景

ディレクトリ構成を最近の Go プロジェクトの慣習と比べて見直した。結論として、トップレベルの技術レイヤー別構成 (`handlers/` → `data/` → `models/`) は 1.7 万行規模のサービスとして妥当で、`cmd/` + `internal/` への全面移行は行わない。一方で、ディレクトリ配置ではなく「どのコードがどのパッケージに入っているか」に次の問題があった。

| 問題 | 内容 |
|---|---|
| ドメインロジックがハンドラにある | `shift_table_handler.go` が 2094 行。自動配置アルゴリズム・修正依頼の適用判定など HTTP と無関係な純粋計算が大半を占めていた |
| リポジトリを経由しない DB アクセス | `sign_up_handler.go` (14 箇所)・`metrics.go` (9 箇所)・`statistics_handler.go` (1 箇所) が `db.GetCollection` を直接呼んでいた |
| インターフェースの定義場所が混在 | 大半は利用側 (handlers) で定義していたが、`data/` にも 5 つあり、同名で二重定義されているものもあった |
| `utils` パッケージ | safego・トークン・HTTP 応答ヘルパー・ポインタヘルパーという性質の違うものが同居していた |
| R2 クライアントの置き場所と名前 | Mongo リポジトリの `data/` に `MinioClient` として、struct とメソッドが別ファイルに分かれて置かれていた |

## 変更内容

4 段階に分けて、各段階で振る舞いを変えずに (`go build` / `go vet` / `go test` が通る状態で) 進めた。

### A. インターフェースを利用側に統一

- `data/` の `MenuRepository` / `SessionRepository` / `StaffRepository` / `ShiftTableRepository` / `ShiftChangeRequestRepository` を削除
- `data.MenuRepository` などを直接参照していたハンドラは、実際に使うメソッドだけを持つインターフェースを handlers 側で定義する (`AIContextMenuRepository` など、既存の命名規則に合わせた)
- 実装が満たしているかどうかは `main.go` で具象型を渡す時点でコンパイラが検証する

### B. `utils` の解体と `storage` の分離

| 移動前 | 移動後 |
|---|---|
| `utils/safego.go` | `safego/` (`safego.Go` / `safego.GoForever`) |
| `utils/token_generator.go`、`GenerateRandomString` | `auth/token.go` |
| `RespondWithJSON` / `RespondWithError` / `RespondWithErrorCode` | `handlers/response.go` (ハンドラ専用のため非公開) |
| `FilterAllowedFields`、`Get{Int,Bool,String}PointerValue` | `handlers/request.go` (後者はジェネリクスの `valueOr` 1 つに統合) |
| `data.MinioClient` (+ `data/minio.go`) | `storage/r2.go` の `storage.Client` |

### C. シフト表のドメインロジックを `shift/` へ切り出し

| ファイル | 内容 |
|---|---|
| `shift/slot.go` | 入力値検証、時刻計算、シフト一覧の内容比較 |
| `shift/autoassign.go` | 自動配置 (`BuildCandidates` / `AutoAssign` / `ComputeShortages`) |
| `shift/change_request.go` | 修正依頼の一括適用判定 (`ApplyChangeRequests`)、スタッフ向けの伏せ字化 |

- 関数本体は移動のみ (公開のための名前変更を除き無変更)。レスポンスの JSON 形式も不変
- `ApplyShiftChangeRequestsHandler` 内の約 110 行の適用ループを純粋関数 `ApplyChangeRequests` に抽出し、通常適用と 3 種類の衝突のテストを追加
- `data` と `handlers` に同じフォーマットで二重定義されていた `pairKey` を `shift.PairKey` に一本化 (「変えるときは両方合わせること」というコメントで同期していた)
- ハンドラは認証・権限・リポジトリ呼び出し・応答だけになり、2094 行 → 1000 行

### D. ハンドラからの DB 直接アクセスを無くす

| ハンドラ | 移動先 |
|---|---|
| 会員登録・店舗追加・重複チェック | `data/signup_repo.go` (`MongoSignUpRepo`)、ハンドラは `SignUpHandler` 構造体に |
| 管理者メトリクス | `data/admin_metrics_repo.go` (`MongoAdminMetricsRepo`)、ハンドラは `AdminMetricsHandler` 構造体に |
| 店舗統計 | `MongoWaitingListRepo.AggregateStatistics` (`data/waiting_statistics_repo.go`) |

- 会員登録時と店舗追加時に重複していた「店舗 + 営業許可証 + 初期設定」の作成を `insertStoreWithDefaults` に統合
- エラー判定を、エラー文字列の部分一致から `data/errors.go` のセンチネルエラー + `errors.Is` に変更
- `health_handler.go` の ping と `RequireDatabaseMiddleware` の `db.IsReady()` はインフラ層の接続確認のため残した

## 同時に直したバグ

D でエラー判定と入力値検証の位置を見直した結果、以下を修正した。いずれも原因は「入力値の検証をトランザクション内で行い、エラー文字列の部分一致でステータスを決めていた」こと。

| 症状 | 原因 | 修正後 |
|---|---|---|
| 会員登録で店舗電話番号の形式が不正だと 500 | エラー文言が小文字の `invalid ...` で、大文字 `"Invalid"` の部分一致に掛からなかった | 400 |
| 店舗住所 (`store_address`) 未指定で店舗作成すると panic | `*req.StoreAddress` を nil チェックせず参照していた | 400 `store address is required` |
| マネージャー以外が店舗追加すると 500 | `only managers ...` がどの部分一致にも掛からなかった | 403 |

入力値の検証は DB に触れる前の `validateNewStoreRequest` で行い、ステータス変換は `respondSignUpError` に集約した。どちらも `handlers/sign_up_handler_test.go` で固定している。

## 採用しなかったもの

- **`cmd/` + `internal/` への移行**: 実行ファイルはサーバー 1 つ (+ 一度きりの移行スクリプト) で、ライブラリとして公開もしないため利点が少なく、差分だけが大きくなる
- **サービス層の一律導入**: ロジックが重いシフト表だけを `shift/` に切り出し、薄いハンドラにまで層を足すことはしない
- **サービスとバッチの分離**: サーバー内のバックグラウンド処理 (SSE heartbeat、メトリクスの flush、Mongo 再接続) はプロセス内の状態に依存するため分離対象ではない。定期バッチが実際に必要になった時点で `cmd/server` + `cmd/<バッチ名>` に分ける

## 残課題

- 既存ファイルのうち約 20 個が `gofmt` に引っかかる (大半は箇条書きコメントのインデント)。機能と無関係なので別コミットで一括整形する
- リポジトリ実装は空の struct のまま、グローバルな `db.GetCollection` に依存している。`*mongo.Database` をフィールドで受け取る形にすればテストで差し替えやすくなるが、今回は対象外
