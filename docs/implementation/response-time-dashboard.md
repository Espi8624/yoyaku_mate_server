# 実装詳細書: Response Timeダッシュボード (Response Time Dashboard)

本文書は、`yoyaku_mate_server` および `yoyaku_mate_admin` に実装されたAPIレイテンシモニタリング機能の技術的設計および詳細な実装事項を説明します。

> 作成日: 2026-07-20  
> 関連文書: [機能仕様書: Response Timeダッシュボード](../features/response-time-dashboard.md)

---

## 1. アーキテクチャおよびデータフロー (System Flow)

```mermaid
sequenceDiagram
    autonumber
    actor Client as ユーザー / アプリ
    participant Middleware as MetricsMiddleware
    participant Tracker as "RequestTracker (In-Memory)"
    participant Worker as "Batch Worker (Goroutine)"
    participant Handler as GetResponseTimeMetricsHandler
    database DB as "MongoDB Atlas (request_logs)"
    actor Admin as 管理者ブラウザ

    Client->>Middleware: 1. APIリクエスト
    Middleware->>Middleware: 2. レスポンス時間測定 (time.Since)
    Middleware->>Tracker: 3. RecordRequest(path, method, status, response_time)
    Tracker->>Tracker: 4. インメモリバッファに一時蓄積 (最大1,000件、Mutex保護)

    loop 5秒周期 (Ticker)
        Worker->>Tracker: 5. バッファ抽出および初期化
        Worker->>DB: 6. InsertMany() 一括保存
    end

    Note over DB: 7. TTLインデックスにより3日後に自動消滅

    Admin->>Handler: 8. GET /api/admin/metrics/response-time?range=1h
    Handler->>DB: 9. Aggregate (エンドポイント別集計 + 全体サマリー)
    DB-->>Handler: 10. $percentile集計結果返却
    Handler-->>Admin: 11. ResponseTimeMetrics JSONレスポンス
```

---

## 2. データベース設計 (Database Schema)

### 2.1 `request_logs` コレクション構造 (BSON)

```json
{
  "_id": "ObjectId",
  "timestamp": "ISODate (UTC)",
  "path": "string (APIエンドポイントパス, 例: /api/waiting-list)",
  "method": "string (GET / POST / PATCH / DELETE)",
  "status_code": "int (HTTPステータスコード)",
  "response_time": "int64 (レスポンス時間、ミリ秒)",
  "client_ip": "string (IPv4 または X-Forwarded-For の最初の値)"
}
```

### 2.2 インデックス構成

| インデックス名 | フィールド | 用途 |
|---|---|---|
| `idx_request_logs_ttl` | `timestamp` | 3日 (259,200秒) 経過後に自動削除 |
| `idx_request_logs_timestamp` | `timestamp` | Aggregation `$match` 範囲フィルターの高速化 |

---

## 3. サーバー実装詳細 (`yoyaku_mate_server`)

### 3.1 新規ファイル一覧

| ファイル | 役割 |
|---|---|
| `models/response_time.go` | `ResponseTimeMetrics`, `ResponseTimeSummary`, `EndpointLatency` 構造体定義 |
| `handlers/metrics.go` (追加) | `GetResponseTimeMetricsHandler`, `toFloat64()`, `toInt64()` ヘルパー追加 |
| `handlers/router.go` (追加) | `GET /api/admin/metrics/response-time` ルート登録 |
| `metrics/middleware.go` (修正) | `/api/admin/metrics` パスのログ記録フィルター追加 |

### 3.2 MongoDB Aggregationの設計

`$percentile` 演算子 (MongoDB 7.0以上、`approximate` アルゴリズム) を活用してP95 / P99を集計します。

**エンドポイント別集計パイプライン:**

```
$match  →  指定時間以降のデータをフィルタリング
$group  →  path + method でグルーピング
           avg_ms:      $avg(response_time)
           p95_ms:      $percentile(p=[0.95], method="approximate")
           p99_ms:      $percentile(p=[0.99], method="approximate")
           count:       $sum(1)
           error_count: $sum($cond(status_code >= 400, 1, 0))
$sort   →  avg_ms 降順
$limit  →  10件
```

**注意**: MongoDB `$percentile` は単一の `p` 配列を受け取り、配列で返します。  
Goレベルで `bson.A` 型として受信後、最初の要素を取り出します。

```go
if arr, ok := raw["p99_ms"].(bson.A); ok && len(arr) > 0 {
    ep.P99Ms = math.Round(toFloat64(arr[0])*10) / 10
}
```

### 3.3 時間範囲パラメーター処理

`?range=` クエリパラメーターで集計基準時間を動的に決定します。

```go
switch rangeParam {
case "5m":  since = time.Now().UTC().Add(-5 * time.Minute)
case "24h": since = time.Now().UTC().Add(-24 * time.Hour)
default:    since = time.Now().UTC().Add(-1 * time.Hour)  // "1h" または未指定
}
```

### 3.4 モニタリングリクエストのログ除外 (汚染防止)

`MetricsMiddleware` にパスプレフィックスフィルターを追加し、Adminポーリングリクエストが統計に含まれないよう処理します。

```go
if strings.HasPrefix(r.URL.Path, "/api/admin/metrics") {
    next.ServeHTTP(w, r)  // ハンドラーは正常に実行
    return                 // request_logs への記録のみスキップ
}
```

### 3.5 型変換ヘルパー

BSON の `interface{}` から取り出した値はランタイム型が不明確なため、変換先の型別ヘルパー関数で安全に変換します。

| 関数 | 用途 |
|---|---|
| `toFloat64(v interface{})` | avg_ms, p95_ms, p99_ms など小数点を含む値 |
| `toInt64(v interface{})` | count, error_count など整数カウント値 |

---

## 4. フロントエンド実装詳細 (`yoyaku_mate_admin`)

### 4.1 新規 / 修正ファイル一覧

| ファイル | 役割 |
|---|---|
| `src/pages/ResponseTimePage.jsx` | 全面再作成 (ダミー → 実際のAPI連携) |
| `src/api/adminService.js` (追加) | `getResponseTimeMetrics(range)` 関数追加 |

### 4.2 時間範囲タブ (`ToggleButtonGroup`)

MUI `ToggleButtonGroup` の隣接ボタン境界線共有のデフォルト動作により、選択されたボタンの右側の境界線が次のボタンの裏に隠れて切れて見える問題が発生します。

**解決方法**: `gap` でボタン間隔を分離し、`MuiToggleButtonGroup-grouped` クラスに `border !important` を直接付与して各ボタンを独立した境界線として処理します。

```jsx
sx={{
  gap: 0.75,
  '& .MuiToggleButtonGroup-grouped': {
    border: `1px solid ${COLORS.borderLight} !important`,
    borderRadius: '6px !important',
    mx: 0,
  },
}}
```

### 4.3 色の動的適用ロジック

```js
// レイテンシ閾値別の色
const getLatencyColor = (ms) => {
  if (ms === 0 || ms === null || ms === undefined) return COLORS.textMuted;
  if (ms < 100) return COLORS.success;  // 緑
  if (ms < 500) return COLORS.warning;  // 橙
  return COLORS.error;                   // 赤 (≥ 500ms)
};

// エラー率閾値別の色 (ERROR RATEカードおよびテーブルのERROR %カラム)
// ≤ 1% → 緑 / 1~5% → 橙 / > 5% → 赤
```

### 4.4 5秒ポーリングおよび範囲連動

`range` 状態変更時に即座にデータを再取得し、以降5秒インターバルで自動更新します。  
`useCallback` で `fetchData` をメモイズして、`range` 変更時のみ関数が再生成されるよう処理します。

```jsx
const fetchData = useCallback(async () => {
  try {
    const data = await getResponseTimeMetrics(range);
    setSummary(data?.summary || { avg_ms: 0, p95_ms: 0, p99_ms: 0, error_rate_pct: 0 });
    setEndpoints(data?.endpoints || []);
  } catch (err) {
    console.error('Failed to load response time metrics', err);
  } finally {
    setLoading(false);
  }
}, [range]);

useEffect(() => {
  setLoading(true);
  fetchData();
  const interval = setInterval(fetchData, 5000);
  return () => clearInterval(interval);
}, [fetchData]);
```

---

## 5. API仕様書 (API Specification)

### `GET /api/admin/metrics/response-time`

**クエリパラメーター:**

| パラメーター | 型 | デフォルト | 説明 |
|---|---|---|---|
| `range` | string | `1h` | 集計範囲 (`5m` / `1h` / `24h`) |

**Response (200 OK):**

```json
{
  "summary": {
    "avg_ms": 45.2,
    "p95_ms": 220.0,
    "p99_ms": 850.5,
    "error_rate_pct": 1.2
  },
  "endpoints": [
    {
      "method": "GET",
      "path": "/api/admin/stores",
      "avg_ms": 180.3,
      "p95_ms": 420.0,
      "p99_ms": 850.5,
      "count": 1240,
      "error_pct": 2.1
    }
  ]
}
```

---

## 関連ドキュメント
- [機能仕様書: Response Timeダッシュボード](../features/response-time-dashboard.md)
