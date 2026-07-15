# サーバーアーキテクチャの概要

> 最終更新: 2026-07-10

## Tech Stack

| 項目 | 技術 |
|------|------|
| 言語 | Go |
| HTTP ルーター | `gorilla/mux` |
| DB | MongoDB Atlas |
| 認証 | Firebase Auth |
| ファイルストレージ | Cloudflare R2 (MinIO互換クライアント) |
| デプロイ | fly.io |
| Rate Limiting | `tollbooth` (5 req/s per IP, burst 10) |
| CORS | `rs/cors` |

---

## ディレクトリ構造

```
yoyaku_mate_server/
│
├── main.go              # エントリーポイント: DB初期化、ミドルウェア、サーバー起動
│
├── handlers/            # HTTPハンドラー (リクエストパース / レスポンス返却)
│   ├── router.go        # ルーター登録
│   ├── waiting_list_handler.go
│   ├── sign_up_handler.go
│   ├── statistics_handler.go
│   ├── metrics.go       # メトリクスダッシュボードクエリAPIハンドラー (エラー、リクエスト、同時接続、SSEステータス)
│   └── ...
│
├── data/                # データアクセス層 (MongoDBクエリ)
│   ├── waiting_list.go
│   ├── counters.go      # Atomic Counter
│   ├── store_info.go
│   └── ...
│
├── models/              # Go構造体 (DBスキーマ / JSONシリアライズ)
│   ├── waiting_list_model.go
│   ├── sse_metrics.go   # SSEブローカーメトリクス構造体
│   └── ...
│
├── events/              # SSE Broker (インメモリ pub/sub)
│   ├── broker.go        # 店舗単位のブロードキャスト + Heartbeatによるゾンビ接続の自動クリア
│   └── waiting_user_broker.go  # 個別のお客様単位 + Heartbeatによるゾンビ接続の自動クリア
│
├── metrics/             # メトリクスパイプライン (インメモリバッファリングおよび非同期バッチワーカー)
│   ├── tracker.go       # エラー、APIリクエスト、アクティブユーザートラッキングのインメモリバッファ
│   └── middleware.go    # HTTPトラフィック収集用のミドルウェア
│
├── auth/                # 認証関連ロジック
│   ├── firebase_auth.go # Firebase ID Token検証
│   └── login_token.go   # 重複ログイン防止トークン検証
│
├── config/              # 環境変数ローディング
├── db/                  # MongoDB接続管理
└── utils/               # 共通ユーティリティ (JSONレスポンス、HMACトークンなど)
```

---

## レイヤー構造およびランタイムフロー

```
       [ Client HTTP Request / SSE Connection ]
                          │
                          ▼
             [ Metrics Middleware ] ──(非同期ロギング)──► [ RequestTracker ]
                          │                                │ (5秒バッチ)
                          ▼                                ▼
              [ Firebase Auth Middleware ]           [ MongoDB Atlas ]
                          │
                          ▼
                   [ Route Handlers ]
                    /            \
                   /              \
  (REST API 呼び出し) ▼             ▼ (SSE 接続維持)
    [ data ] MongoDB クエリ     [ events ] SSE Brokers
           │                       │ (30秒 Heartbeat)
           ▼                       ▼
    [ MongoDB Atlas ]       [ Client Push Messages ]
```

---

## 認証フロー

```
Request
    │
    ├── Authorizationヘッダーなし → 公開エンドポイント (お客様QR登録など)
    │
    └── Authorization: Bearer <id_token>
            │
            ▼
        auth.VerifyIDToken()  →  Firebase UID抽出
            │
            ▼
        data.GetUserByFirebaseUID()  →  内部Userオブジェクト
            │
            ▼
        auth.VerifyLoginToken()  →  X-Login-Tokenヘッダー検証
            │                        (重複ログイン防止)
            ▼
        data.CheckUserStorePermission()  →  店舗権限確認
```

---

## 主要なAPIグループ

| パス | 説明 |
|------|------|
| `/api/waiting-list` | 待機列 CRUD + SSE ストリーム |
| `/api/menu-list`, `/api/provider_menu` | メニュー管理 |
| `/api/provider_user`, `/api/provider_store` | ユーザー/店舗情報 |
| `/api/auth/signup`, `/api/stores/add` | 会員登録 / 店舗登録 |
| `/api/admin/*` | 管理者専用 (店舗承認など) |
| `/api/admin/metrics/errors` | エラーメトリクスの要約および最近の詳細ログ一覧の取得 |
| `/api/admin/metrics/requests` | APIリクエスト統計および詳細ログ一覧の取得 |
| `/api/admin/metrics/active-users` | リアルタイム同時接続者数およびDAU/MAUの要約メトリクスの取得 |
| `/api/admin/metrics/sse-status` | SSEブローカーの接続状況および平均接続時間の取得 (インメモリ) |
| `/api/stores/{storeId}/staff` | スタッフ管理 |
| `/api/statistics` | 待機統計 |
| `/api/public/ai-chat` | AIチャット (公開) |

---

## 関連ドキュメント

- [待機列機能仕様](../features/waiting-list.md)
- [エラーダッシュボード実装詳細](./error-dashboard.md)
- [リクエストカウンター実装詳細](./request-counter.md)
- [アクティブユーザートラッキング実装詳細](./active-user-tracking.md)
- [SSEステータス監視実装詳細](./sse-monitoring.md)
- [SSE実装詳細](./sse.md)
- [冪等性実装詳細](./idempotency.md)
- [Atomic Counter発番詳細](./atomic-counter.md)
- [ADR-001: WebSocketの代わりにSSEを選択した理由](../decisions/ADR-001-use-sse.md)
- [ADR-002: エラーダッシュボードにおけるHTTPポーリング採用の理由](../decisions/ADR-002-use-polling-for-error-dashboard.md)
- [ADR-003: 独自メトリクス収集およびリクエストカウンターアーキテクチャの採用](../decisions/ADR-003-request-counter-architecture.md)
- [ADR-004: インメモリのスライディングウィンドウおよび日別アクティブユーザーコレクションを活用した接続者トラッキングの採用理由](../decisions/ADR-004-active-user-tracking.md)
- [ADR-005: SSEゾンビ接続検知方式の採用](../decisions/ADR-005-sse-zombie-detection.md)
- [ADR-006: SSE監視ダッシュボードにおける通信の分離およびHTTPポーリング方式採用の理由](../decisions/ADR-006-sse-monitoring-polling.md)
