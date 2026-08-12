# 管理者メトリクスおよび統計ダッシュボードのリファクタリング (DIとRepositoryパターンの適用)

> 最終更新日: 2026-08-12

## 背景と課題

フェーズ1のDIリファクタリング以降も、`handlers/metrics.go`や`handlers/statistics_handler.go`など一部の複雑なハンドラー内では、依然として`db.GetCollection()`や`db.MongoClient`といったグローバル変数を直接参照し、MongoDBの集計パイプラインを実行していました。

```go
// AS-IS: ハンドラー内部でグローバルDBオブジェクトを直接参照
func (h *StatisticsHandler) CalculateStatistics(...) {
    collection := db.GetCollection(db.DatabaseName, db.CollectionWaitingList)
    // 何百行にも及ぶ $facet パイプラインの構成と実行ロジック...
}
```

このアプローチには以下の課題がありました：
1. **ビジネスロジックとデータアクセスロジックの混在**: 1つのハンドラーファイルに数百行のMongoDBクエリが含まれており、可読性が著しく低下していました。
2. **グローバル依存の残骸**: 依然としてグローバルなDB接続に強く依存しており、ユニットテストの作成を妨げていました。

## 解決策: データレイヤーの分離とAggregationロジックの移管

グローバル状態を参照する部分をすべて削除し、クリーンアーキテクチャの原則に従って、データベースに直接アクセスするロジックを完全に`data/`レイヤー(Repository)に移管しました。（※注: 会員登録(`sign_up_handler.go`)機能は構造の再設計が必要なため、今回のフェーズからは除外されました。）

### 1. `AdminMetrics` リファクタリング
- **Repositoryの新設**: `data/metrics_repo.go`を作成し、MongoDBを照会するロジック(`GetErrorMetrics`, `GetDAUMAU`など)をカプセル化しました。
- **Handlerへの依存性の注入**: 個別の関数に分かれていたロジックを`AdminMetricsHandler`構造体に統合し、`AdminMetricsRepository`インターフェースを注入して使用するように改善しました。（CPU/メモリなどのOSレベルのメトリクスはDBアクセスが不要なため、ハンドラーに残しています）

### 2. `Statistics` 統計パイプラインの分離
- **複雑なクエリの移管**: 既存のハンドラー内部にハードコーディングされていた巨大な `$facet` MongoDB集計(Aggregation)パイプラインを、`data/waiting_list_repo.go`の`GetStatisticsAggregation`メソッドに移動させました。
- **インターフェースの分離**: ハンドラーは`StatsWaitingListRepository`インターフェースを介してパイプラインの実行結果(bson.M)のみを受け取り、JSONレスポンスへのマッピングに専念するようになりました。

### 3. `main.go` での統合接続(Wiring)
```go
// TO-BE: main.goで具体的なRepositoryを生成し、注入する
metricsRepo := &data.MongoMetricsRepo{}
adminMetricsHandler := handlers.NewAdminMetricsHandler(metricsRepo)

waitingRepo := &data.MongoWaitingListRepo{}
statisticsHandler := handlers.NewStatisticsHandler(userRepo, storeRepo, waitingRepo)
```

## 期待される効果 (Benefits)

1. **可読性と保守性の最大化**: ハンドラーコードから複雑なBSONパイプラインクエリが消えたことで、コードがはるかにすっきりし、ビジネスフローが把握しやすくなりました。
2. **テストカバレッジ向上の基盤構築**: DBをモック(Mocking)できる完璧な環境が整い、バックエンドのすべての主要な照会ロジックをユニットテストできるようになりました。
3. **アーキテクチャの一貫性**: 除外された会員登録機能を除く、サーバー内のほぼすべてのロジックが同一の階層化(Layered)アーキテクチャ規則に従うようになりました。
