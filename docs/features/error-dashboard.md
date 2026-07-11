# エラーダッシュボード機能 (Error Dashboard)

> 最終更新: 2026-07-11

## 概要

アプリケーションの安定性と保守性を高めるため、サーバー内で発生する各種エラー（HTTP 4xx/5xx、データベースエラー、リアルタイムストリーミング切断など）をリアルタイムで収集・追跡し、管理者ダッシュボードに可視化する機能です。  
メインAPI of 処理速度に影響を与えないよう、**「インメモリバッファリング ➡️ 非同期バッチ書き込み」**方式を採用しています。

---

## 主要概念

| 概念 | 説明 |
|------|------|
| `ErrorCaptureMiddleware` | HTTPリクエストを傍受し、4xx/5xxレスポンスを検知する共通ミドルウェア |
| `ErrorTracker` | 発生したエラーをメモリ上に集計・一時保存するシングルトン管理クラス |
| `logBuffer` | エラーが発生した際、メインスレッドをブロックせずに一旦蓄積するインメモリ領域（最大1,000件） |
| `Batch Worker` | 5秒周期で起動し、メモリ内のバッファをクリアしてMongoDBに一括保存（Bulk Insert）するバックグラウンド処理 |
| `TTL Index` | 7日間（604,800秒）が経過したエラーログをMongoDBが自動でバックグラウンド削除するライフサイクル管理 |

---

## 収集対象エラーと状態定義

収集されるエラーは以下の4つのカテゴリーに分類され、[ErrorCountPage.jsx](../../yoyaku_mate_admin/src/pages/ErrorCountPage.jsx)の上部カードに集計されて表示されます。

| エラータイプ (`error_type`) | 収集の契機 | 主要収集データ |
|---------------------------|------------|----------------|
| `500_INTERNAL_ERROR`      | APIハンドラーの内部処理失敗、または想定外のPanic発生時 | メッセージ、APIパス、メソッド、クライアントIP |
| `400_BAD_REQUEST`         | クライアントから不正なパラメータの送信、存在しないAPIへのアクセス時 | メッセージ、APIパス、メソッド、クライアントIP |
| `DATABASE_ERROR`          | MongoDBクエリの実行エラー、コネクション切断発生時 | データベースエラー詳細ログ |
| `SSE_DISCONNECT`          | 待機列通知ストリーム接続中に、クライアントが離脱（切断）した時 | 接続されていたAPIパス、RemoteAddr |

---

## データフロー

```mermaid
sequenceDiagram
    autonumber
    actor Client as ユーザー / 管理者
    participant Router as Gorilla Mux / Middleware
    participant Tracker as ErrorTracker (In-Memory)
    participant Worker as Batch Worker (Goroutine)
    database DB as MongoDB Atlas

    Client->>Router: 1. APIリクエスト送信 (または接続切断)
    Router-->>Client: 2. API処理およびレスポンス返却 (400/500エラー発生)
    
    note over Router, Tracker: メインスレッドの処理速度を守るため、非同期で転送
    Router->>Tracker: 3. RecordError(models.ErrorLog) 呼び出し
    Tracker->>Tracker: 4. インメモリ logBuffer に一時保存 (ロック時間は最小限)

    loop 5秒周期 (Ticker)
        Worker->>Tracker: 5. バッファデータの抽出・クリア
        Worker->>DB: 6. InsertMany() による一括非同期書き込み (Bulk Write)
    end

    Note over DB: 7. TTLインデックスにより7日経過後に自動削除
```

---

## データベース設計

### 1. `error_logs` コレクション構造 (BSON)

```json
{
  "_id": "ObjectId",
  "timestamp": "ISODate (UTC)",
  "error_type": "string (500_INTERNAL_ERROR / 400_BAD_REQUEST / DATABASE_ERROR / SSE_DISCONNECT)",
  "message": "string (エラー原因の要約)",
  "path": "string (APIエンドポイントパス)",
  "method": "string (GET / POST / PATCH / DELETE)",
  "client_ip": "string (IPv4 / IPv6 / X-Forwarded-Forの最初の値)"
}
```

### 2. パフォーマンス最適化のためのインデックス設定

書き込み・読み込み時のデータベース負荷を極限まで低減させるため、起動時に自動的に以下のインデックスを作成します。

* **`idx_error_logs_ttl`**
  - キー: `{"timestamp": 1}`
  - オプション: `ExpireAfterSeconds: 604800` (7日間)
  - 効果: コレクションの肥大化と追加のストレージコスト発生を自動的に防ぎます。
* **`idx_error_type`**
  - キー: `{"error_type": 1}`
  - 効果: 管理者ダッシュボード起動時に、エラータイプ別の集計（Count）をインデックススキャンのみで高速に処理します。

---

## ダッシュボード提供API

管理者ダッシュボード向けに以下のREST APIを分離して提供します。

1. **エラーメトリクス統計 API**
   - パス: `/api/admin/metrics/errors`
   - 方式: `GET`
   - 説明: エラータイプごとの累積件数をMongoDBのカウントクエリから高速に取得します。
2. **詳細エラーログリスト API**
   - パス: `/api/admin/metrics/error-logs`
   - 方式: `GET`
   - 説明: 直近に発生した詳細ログ50件を最新順にソートして返却します。

---

## 技術決定 (ADR)

本機能の実装にあたって、リアルタイム待機列管理（SSE採用）との性能要件の違いを比較し、あえてポーリング方式を採用した背景については、以下の技術決定書（ADR）を参照してください。

* [ADR-002: エラーダッシュボードにおけるHTTPポーリングの採用](../decisions/ADR-002-use-polling-for-error-dashboard.md)


