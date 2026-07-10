# 冪等性の実装 (Idempotency)

> 最終更新: 2026-07-10  
> 関連ファイル: [`data/waiting_list.go`](../../data/waiting_list.go)

## 問題の背景

モバイル環境では、ネットワークの不安定さにより同一のAPIリクエストが重複して送信される可能性があります。  
特に待機登録(`POST /api/waiting-list`)において重複登録が発生すると、お客様が2つの整理券を受け取ってしまう致命的なエラーが発生します。

**従来の方式の課題**
- サーバー側で「同一の顧客であるか」を判断する明確な基準がない
- ネットワークの再試行時に無条件で新しいレコードが生成される

---

## 解決方法: クライアント生成の冪等性キー

クライアント(FlutterアプリまたはWeb)がリクエスト送信前に **UUID形式の `waiting_id` を直接生成**し、リクエストBodyに含めて送信します。

```
クライアント                         サーバー
   │                                 │
   │── waiting_id 生成 (UUID) ──→   │
   │                                 │
   │── POST /waiting-list ──────────→│
   │   { waiting_id: "abc-123", ... }│
   │                                 │── DB照会: waiting_id が存在？
   │                                 │
   │   [初回リクエスト]              │── なし → 新規レコード挿入
   │←── 201 Created ────────────────│
   │                                 │
   │   [重複リクエスト (再試行)]     │── あり → 既存データを返却
   │←── 201 Created (同一データ) ───│  (新規挿入は行わない)
```

---

## 実装コード

```go
// data/waiting_list.go - CreateWaitingListItem()

// 冪等性の検証: クライアントが waiting_id を送信した場合、重複チェックを実行
if item.WaitingID != "" {
    var existingItem models.WaitingList
    dupFilter := bson.M{
        "store_id":   item.StoreID,
        "waiting_id": item.WaitingID,
    }
    err := collection.FindOne(ctx, dupFilter).Decode(&existingItem)
    if err == nil {
        // 既に存在 → 保存せずに冪等に既存データを返却
        log.Printf("Duplicate waiting registration detected (idempotent). store_id: %s, waiting_id: %s", item.StoreID, item.WaitingID)
        return &existingItem, nil
    } else if err != mongo.ErrNoDocuments {
        return nil, fmt.Errorf("failed to check duplicate waiting item: %v", err)
    }
}
```

---

## フォールバック(Fallback)処理

クライアントが `waiting_id` を送信しなかった場合(レガシークライアントなど)、  
サーバー側で現在の時刻を基準にしたIDを生成します。

```go
if item.WaitingID == "" {
    now := time.Now()
    // Format: YYYYMMDD-HHmmss-SSS
    item.WaitingID = now.Format("20060102-150405") + "-" + fmt.Sprintf("%03d", now.Nanosecond()/1e6)
}
```

> **警告: フォールバックIDは冪等性を保証しません。** 完全な保護のためには、クライアント側で必ずUUIDを生成して送信する必要があります。

---

## DBスキーマ (waiting_list コレクション)

```
{ store_id, waiting_id }  →  複合ユニークインデックスを推奨
```

現在はアプリケーションレベルでのみ重複を遮断していますが、DBレベルでユニークインデックスを追加することで、競合状態(Race Condition)まで完全に遮断することが可能です。

---

## 関連ドキュメント

- [待機列機能仕様](../features/waiting-list.md)
- [Atomic Counter実装](./atomic-counter.md)
