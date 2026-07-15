# docs — yoyaku_mate_server ドキュメントインデックス

## 構造

```
docs/
├── features/           # 機能仕様 (何をするのか)
├── implementation/     # 技術実装詳細 (どのように実装したのか)
├── decisions/          # 技術選定根拠 (なぜこれを選択したのか / ADR)
├── troubles/           # トラブルシューティング / 振り返り記録
└── refactoring/        # リファクタリング記録
```

---

## Features (機能仕様)

| ドキュメント | 説明 |
|------|------|
| [waiting-list.md](./features/waiting-list.md) | 待機列の登録・管理・リアルタイム通知機能 |
| [error-dashboard.md](./features/error-dashboard.md) | エラーメトリクス・ログのリアルタイム収集および監視ダッシュボード |
| [request-counter.md](./features/request-counter.md) | リクエストメトリクス・ログのリアルタイム収集および監視ダッシュボード |
| [active-user-dashboard.md](./features/active-user-dashboard.md) | アクティブユーザー(同時接続者/DAU/MAU)収集および監視ダッシュボード |
| [sse-monitoring.md](./features/sse-monitoring.md) | SSEステータス（接続数、維持時間）およびゾンビ接続監視モニタリング |


---

## Implementation (実装詳細)

| ドキュメント | 説明 |
|------|------|
| [architecture.md](./implementation/architecture.md) | サーバー全体のアーキテクチャおよびレイヤー構造 |
| [background.md](./implementation/background.md) | プロジェクトの背景および設計意図 |
| [sse.md](./implementation/sse.md) | SSE Brokerの実装 (リアルタイムPush) |
| [idempotency.md](./implementation/idempotency.md) | 冪等性処理 (重複登録の防止) |
| [atomic-counter.md](./implementation/atomic-counter.md) | 待機順番号のAtomic発番ロジック |
| [error-dashboard.md](./implementation/error-dashboard.md) | エラーダッシュボードのバックエンドパイプラインおよびバッファリング実装詳細 |
| [request-counter.md](./implementation/request-counter.md) | リクエストカウンターのバックエンドパイプラインおよびバッファリング実装詳細 |
| [active-user-tracking.md](./implementation/active-user-tracking.md) | アクティブユーザートラッキングおよびリアルタイム/統計収集実装詳細 |
| [sse-monitoring.md](./implementation/sse-monitoring.md) | SSEブローカーのステータス収集およびHeartbeatによるゾンビ接続の自動クリア実装詳細 |

---

## Decisions (技術決定)

| ドキュメント | 決定内容 |
|------|----------|
| [ADR-001-use-sse.md](./decisions/ADR-001-use-sse.md) | WebSocketの代わりにSSEを選択した理由 |
| [ADR-002-use-polling-for-error-dashboard.md](./decisions/ADR-002-use-polling-for-error-dashboard.md) | エラーダッシュボードにおけるHTTPポーリング採用の理由 |
| [ADR-003-request-counter-architecture.md](./decisions/ADR-003-request-counter-architecture.md) | 独自メトリクス収集およびリクエストカウンターアーキテクチャの採用 |
| [ADR-004-active-user-tracking.md](./decisions/ADR-004-active-user-tracking.md) | インメモリのスライディングウィンドウおよび日別アクティブユーザーコレクションを活用した接続者トラッキングの採用理由 |
| [ADR-005-sse-zombie-detection.md](./decisions/ADR-005-sse-zombie-detection.md) | SSEゾンビ接続検知方式 — select-defaultによるノンブロッキング送信の採用理由 |
| [ADR-006-sse-monitoring-polling.md](./decisions/ADR-006-sse-monitoring-polling.md) | SSE監視ダッシュボードにおける通信の分離およびHTTPポーリング方式採用の理由 |


---

## Troubles (トラブルシューティング / 振り返り)

| ドキュメント | 説明 |
|------|------|
| [001-lessons-learned.md](./troubles/001-lessons-learned.md) | Goroutineリーク、Rate Limiter調整など開発プロセスの振り返り |
| [002-active-user-ip-port-issue.md](./troubles/002-active-user-ip-port-issue.md) | リアルタイム接続者のエフェメラルポートおよびIPv6重複カウント防止解決プロセス |

---

## Refactoring (リファクタリング)

*記録予定*
