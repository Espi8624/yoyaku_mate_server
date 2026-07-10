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
│   └── ...
│
├── events/              # SSE Broker (インメモリ pub/sub)
│   ├── broker.go        # 店舗単位のブロードキャスト
│   └── waiting_user_broker.go  # 個別のお客様単位
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

## レイヤー構造

```
Request
    │
    ▼
[handlers]      HTTPパース、認証チェック、ビジネスルール検証
    │
    ▼
[data]          MongoDBクエリ (try-catchパターン)
    │
    ▼
[models]        Go構造体 ↔ BSON/JSONシリアライズ
    │
    ▼
MongoDB Atlas
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
| `/api/stores/{storeId}/staff` | スタッフ管理 |
| `/api/statistics` | 待機統計 |
| `/api/public/ai-chat` | AIチャット (公開) |

---

## 関連ドキュメント

- [待機列機能仕様](../features/waiting-list.md)
- [SSE実装](./sse.md)
- [冪等性実装](./idempotency.md)
- [Atomic Counter](./atomic-counter.md)
