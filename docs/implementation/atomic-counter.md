# Atomic Counter (待機順番号の発行)

> 最終更新: 2026-07-10  
> 関連ファイル: [`data/counters.go`](../../data/counters.go)

## 問題の背景

待機順番号(`queue_number`)は、営業日ごとに1番から始まる単調増加番号です。  
同時に複数のお客様が登録した場合、単純な `MAX + 1` 方式では **重複番号**が発行される可能性があります。

---

## 解決方法: MongoDB FindOneAndUpdate ベースの Atomic Increment

MongoDBの `$inc` 演算子と `FindOneAndUpdate` を組み合わせて、  
カウンターの読み取り + 増加を **単一のアトミック(atomic)操作** として処理します。

```go
// data/counters.go - GetNextSequence()

filter := bson.M{
    "_id": models.CounterID{
        StoreID: storeID,
        Date:    dateStr,  // "20260710" (営業日基準)
    },
}
update := bson.M{
    "$inc": bson.M{"seq": 1},
}
opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

err := collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedCounter)
```

**Counter ドキュメント構造**
```json
{
  "_id": { "store_id": "abc123", "date": "20260710" },
  "seq": 7
}
```

---

## 初回登録の処理 (Lazy Initialization)

カウンタードキュメントが存在しない場合(当日の最初の登録)、 `mongo.ErrNoDocuments` を検知して、  
現在の待機列の最大 `queue_number` を照会した後にカウンターを初期化します。

```
FindOneAndUpdate → ErrNoDocuments
        ↓
getMaxQueueNumberInternal()  ← 現在のDBの最大値を照会
        ↓
InsertOne(seq = max + 1)
        ↓
[Race Condition] DuplicateKeyError?
        ↓ YES
    再帰的に再試行 (GetNextSequence)
```

---

## 競合状態 (Race Condition) の処理

同時に2つのリクエストが共に `ErrNoDocuments` を受け取った場合、  
双方が `InsertOne` を試行すると、一方は `DuplicateKeyError` が発生します。  
これを検知して **再帰的に再試行** することで、正確な連番を保証します。

```go
_, err = collection.InsertOne(ctx, newCounter)
if err != nil {
    if mongo.IsDuplicateKeyError(err) {
        log.Printf("[GetNextSequence] Race detected. Retrying increment.")
        return GetNextSequence(storeID, businessDate)  // 再帰的に再試行
    }
    return 0, fmt.Errorf("failed to insert new counter: %w", err)
}
```

---

## 営業日基準のリセット

カウンターIDに営業日の日付(`date`)が含まれているため、日付が変わると自動的に新しいカウンターが生成されます。  
Dynamic Cutoff基準であるため、深夜営業の店舗でも正しく動作します。

```
{ store_id: "abc", date: "20260710" } → seq: 15  (7月10日営業日)
{ store_id: "abc", date: "20260711" } → seq: 1   (7月11日営業日、新しく開始)
```

---

## 関連ドキュメント

- [待機列機能仕様](../features/waiting-list.md)
- [冪等性実装](./idempotency.md)
