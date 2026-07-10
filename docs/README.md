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

---

## Implementation (実装詳細)

| ドキュメント | 説明 |
|------|------|
| [architecture.md](./implementation/architecture.md) | サーバー全体のアーキテクチャおよびレイヤー構造 |
| [background.md](./implementation/background.md) | プロジェクトの背景および設計意図 |
| [sse.md](./implementation/sse.md) | SSE Brokerの実装 (リアルタイムPush) |
| [idempotency.md](./implementation/idempotency.md) | 冪等性処理 (重複登録の防止) |
| [atomic-counter.md](./implementation/atomic-counter.md) | 待機順番号のAtomic発番ロジック |

---

## Decisions (技術決定)

| ドキュメント | 決定内容 |
|------|----------|
| [ADR-001-use-sse.md](./decisions/ADR-001-use-sse.md) | WebSocketの代わりにSSEを選択した理由 |

---

## Troubles (トラブルシューティング / 振り返り)

| ドキュメント | 説明 |
|------|------|
| [001-lessons-learned.md](./troubles/001-lessons-learned.md) | Goroutineリーク、Rate Limiter調整など開発プロセスの振り返り |

---

## Refactoring (リファクタリング)

*記録予定*
