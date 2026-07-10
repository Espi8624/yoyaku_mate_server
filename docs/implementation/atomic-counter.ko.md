# Atomic Counter (대기 순번 발급)

> 최종 수정: 2026-07-10  
> 관련 파일: [`data/counters.go`](../../data/counters.go)

## 문제 배경

대기 순번(`queue_number`)은 영업일마다 1번부터 시작하는 단조 증가 번호입니다.  
동시에 여러 손님이 등록할 경우, 단순 `MAX + 1` 방식으로는 **중복 번호**가 발급될 수 있습니다.

---

## 해결 방법: MongoDB FindOneAndUpdate 기반 Atomic Increment

MongoDB의 `$inc` 연산자와 `FindOneAndUpdate`를 조합하여,  
카운터 읽기 + 증가를 **단일 원자적(atomic) 연산**으로 처리합니다.

```go
// data/counters.go - GetNextSequence()

filter := bson.M{
    "_id": models.CounterID{
        StoreID: storeID,
        Date:    dateStr,  // "20260710" (영업일 기준)
    },
}
update := bson.M{
    "$inc": bson.M{"seq": 1},
}
opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

err := collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedCounter)
```

**Counter 문서 구조**
```json
{
  "_id": { "store_id": "abc123", "date": "20260710" },
  "seq": 7
}
```

---

## 첫 번째 등록 처리 (Lazy Initialization)

카운터 문서가 없는 경우(당일 첫 등록), `mongo.ErrNoDocuments`를 감지하여  
현재 대기열의 최대 `queue_number`를 조회한 뒤 카운터를 초기화합니다.

```
FindOneAndUpdate → ErrNoDocuments
        ↓
getMaxQueueNumberInternal()  ← 현재 DB의 최대값 조회
        ↓
InsertOne(seq = max + 1)
        ↓
[Race Condition] DuplicateKeyError?
        ↓ YES
    재귀 재시도 (GetNextSequence)
```

---

## Race Condition 처리

동시에 두 개의 요청이 모두 `ErrNoDocuments`를 받은 경우,  
둘 다 `InsertOne`을 시도하면 하나는 `DuplicateKeyError`가 발생합니다.  
이를 감지하여 **재귀적으로 재시도**함으로써 정확한 순번을 보장합니다.

```go
_, err = collection.InsertOne(ctx, newCounter)
if err != nil {
    if mongo.IsDuplicateKeyError(err) {
        log.Printf("[GetNextSequence] Race detected. Retrying increment.")
        return GetNextSequence(storeID, businessDate)  // 재귀 재시도
    }
    return 0, fmt.Errorf("failed to insert new counter: %w", err)
}
```

---

## 영업일 기반 리셋

카운터 ID에 영업일 날짜(`date`)가 포함되어 있어, 날짜가 바뀌면 자동으로 새 카운터가 생성됩니다.  
Dynamic Cutoff 기반이므로 심야 영업 점포에서도 올바르게 동작합니다.

```
{ store_id: "abc", date: "20260710" } → seq: 15  (7월 10일 영업일)
{ store_id: "abc", date: "20260711" } → seq: 1   (7월 11일 영업일, 새로 시작)
```

---

## 관련 문서

- [대기열 기능 사양](../features/waiting-list.ko.md)
- [멱등성 구현](./idempotency.ko.md)
