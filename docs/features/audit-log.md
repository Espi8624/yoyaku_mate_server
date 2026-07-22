# 機能仕様書: 監査ログ (Audit Log)

本文書は、`yoyaku_mate_server` に実装された管理者操作の監査ログ (Audit Log) 機能仕様を定義します。

> 作成日: 2026-07-22  
> 関連文書: [監査ログ実装詳細書](../implementation/audit-log.md)

---

## 1. 概要 (Overview)

プラットフォームの安全な運営とシステム変更履歴の追跡性を確保するため、管理者が実行した主要なデータ変更操作（店舗承認・拒否等）をリアルタイムに自動記録・管理します。

---

## 2. 主な機能 (Key Features)

1. **管理者操作の自動記録**:
   - `adminApi` サブルーターを通じて実行される主要な状態変更アクションを自動キャッチし、データベースに保存します。
2. **自動保持期間管理 (90日 TTL)**:
   - MongoDB の TTL (Time-To-Live) インデックスを活用し、90日経過した過去の監査ログを自動削除してストレージ容量を最適化します。
3. **最新順一覧取得 API**:
   - `GET /api/admin/metrics/audit-logs` エンドポイントを介して、最新の操作履歴最大100件を取得できます。

---

## 3. 記録対象アクション (Audit Actions)

| アクション名 | 発生条件 | 記録内容 (Target / Details) |
|---|---|---|
| `STORE_APPROVED` | 出店申請の承認処理時 | 対象店舗 ID、承認コメント（任意） |
| `STORE_REJECTED` | 出店申請の拒否処理時 | 対象店舗 ID、拒否理由コメント |
| `STORE_PENDING_REVIEW` | 審査保留状態への変更時 | 対象店舗 ID、保留理由コメント |

---

## 4. API 仕様 (API Specification)

### `GET /api/admin/metrics/audit-logs`

* **説明**: 直近の監査ログを最新順で最大100件取得します。
* **レスポンス**: `200 OK`
```json
[
  {
    "id": "669fc25a8123456789abcdef",
    "timestamp": "2026-07-22T14:30:00Z",
    "action": "STORE_APPROVED",
    "target": "Store ID: 694e6e05ca47b4563c73617d",
    "status": "SUCCESS",
    "details": "書類確認完了"
  }
]
```
