# ⚙️ Yoyaku Mate - 統合APIバックエンドサーバー (Go Backend)

> **Yoyaku Mate** バックエンドサーバーは、リアルタイム待機列サービスのビジネスロジック全体を処理する **GoベースのRESTful APIサーバー**です。MongoDB Atlasデータベースモデルの設計、Cloudflare R2ファイルアップロード、Firebase Admin SDK連携によるユーザー認証処理およびリアルタイム同期をサポートします。

---

## 🛠 Tech Stack (技術スタック)

- **Language:** Go (Golang) 1.23
- **Router:** `gorilla/mux` (HTTPマルチプレクサーによるルーティング)
- **Database:** MongoDB Atlas (NoSQLデータストア)
- **Object Storage:** Cloudflare R2 (S3互換の高パフォーマンスファイル/アセットストレージ)
- **External Integration:**
  - Firebase Admin SDK (ユーザー認証トークンの検証)
- **Security & Middleware:**
  - `rs/cors` (CORSポリシー管理)
  - `didip/tollbooth` (Rate LimitingベースのAPI過剰リクエスト制限)
- **Deployment:** Fly.io (Dockerコンテナベースのグローバル仮想サーバーホスティング)

---

## ✨ Key Features (主な機能)

- **待機列リアルタイム管理API:** 店舗待機申請、順番変更、待機ステータス変更ロジックの処理およびデータトランザクションの保証。
- **Firebase JWT認証連携:** クライアント（アプリ/Web）から渡されたFirebaseトークンの有効性をバックエンド側で検証し、安全なデータの読み書きを保証します。
- **Cloudflare R2連携:** 静的ファイルのアップロード時、S3 API互換ライブラリを通じてファイル保存を効率的に代行します。
- **安全なAPI構造:** 速度制限（Rate Limit）ミドルウェアを導入し、異常なDDoS形態の過剰なAPI攻撃を制限します。

---

## 📂 Project Structure (ディレクトリ構造)

```bash
├── auth/           # Firebase Admin SDK連携認証およびトークン発行/検証ロジック
├── config/         # JSONファイルおよび環境変数による設定管理ロジック (config.go)
├── data/           # 静的翻訳テンプレートおよびデータリソース
├── db/             # MongoDB Atlas接続確立およびドライバー設定 (mongo.go)
├── handlers/       # ルーターエンドポイント別のビジネスハンドラー関数
├── models/         # MongoDBスキーマにマッピングされるGo構造体定義
├── utils/          # HMACトークン発行、ロガー、日付ガイドのユーティリティ関数
├── Dockerfile      # Fly.ioデプロイのためのDockerマルチステージビルド環境の構成
├── fly.toml        # Fly.ioアプリケーション仮想サーバーの設定
├── main.go         # サーバー実行のエントリーポイントおよびミドルウェア/ルーティング登録
└── go.mod          # 依存モジュール定義ファイル
```

---

## 🚀 Getting Started (はじめに)

### 1. 環境変数の設定
ローカル起動の際、`.env.example`ファイルをコピーして、プロジェクトのルートディレクトリに `.env` ファイルを作成します。

```bash
# テンプレートファイルをコピーしてローカル設定ファイルを作成
cp .env.example .env
```

コピーした `.env` ファイルを開き、必要な環境変数（`MONGODB_URI`、`R2_ACCESS_KEY` など）を入力します。

### 2. ローカル実行
設定ファイルを利用して起動する場合は、`.example` ファイルをコピーして以下の設定ファイルを `config/` ディレクトリに配置します。

* **`config/development.json`** (MongoDB等の接続先設定):
  ```bash
  cp config/development.json.example config/development.json
  ```
* **`config/serviceAccountKey.json`** (Firebase管理者鍵): Firebaseコンソールから取得したサービスアカウント鍵ファイルを配置します（テンプレートは `config/serviceAccountKey.json.example` を参照してください）。

配置が完了したら、以下のコマンドで実行します。

```bash
# 依存モジュールのインストール
go mod download

# サーバーの起動
go run main.go
```
サーバーが起動すると、 `http://localhost:8080` ポートでAPIサービスが動作します。

---

## 🐋 Deploy (デプロイ)

本バックエンドプロジェクトは、**Fly.io**にDockerベースのマルチステージビルド方式でデプロイされます。
GitHub Actions連携時、 `fly-deploy.yml` ワークフローを通じて `main` ブランチにプッシュした際に自動デプロイが実行されます。

```bash
# ローカルからFly.io CLIでデプロイを実行する場合
flyctl deploy
```
