# 멱등성 구현 (Idempotency)

> 최종 수정: 2026-07-10  
> 관련 파일: [`data/waiting_list.go`](../../data/waiting_list.go)

## 문제 배경

모바일 환경에서는 네트워크 불안정으로 인해 동일한 API 요청이 중복 전송될 수 있습니다.  
특히 대기 등록(`POST /api/waiting-list`)에서 중복 등록이 발생하면 손님이 두 개의 번호표를 받게 되는 치명적인 오류가 생깁니다.

**기존 방식의 문제점**
- 서버 측에서 "같은 손님인지"를 판단할 명확한 기준이 없음
- 네트워크 재시도 시 무조건 새 레코드가 생성됨

---

## 해결 방법: 클라이언트 생성 멱등성 키

클라이언트(Flutter 앱 또는 웹)가 요청 전에 **UUID 형태의 `waiting_id`를 직접 생성**하고, 요청 Body에 포함하여 전송합니다.

```
클라이언트                         서버
   │                                 │
   │── waiting_id 생성 (UUID) ──→   │
   │                                 │
   │── POST /waiting-list ──────────→│
   │   { waiting_id: "abc-123", ... }│
   │                                 │── DB 조회: waiting_id 존재?
   │                                 │
   │   [첫 번째 요청]                │── 없음 → 새 레코드 삽입
   │←── 201 Created ────────────────│
   │                                 │
   │   [중복 요청 (재시도)]          │── 있음 → 기존 데이터 반환
   │←── 201 Created (동일 데이터) ──│  (새 삽입 없음)
```

---

## 구현 코드

```go
// data/waiting_list.go - CreateWaitingListItem()

// 冪等성검증: 클라이언트가 waiting_id를 송신한 경우 중복 체크 실행
if item.WaitingID != "" {
    var existingItem models.WaitingList
    dupFilter := bson.M{
        "store_id":   item.StoreID,
        "waiting_id": item.WaitingID,
    }
    err := collection.FindOne(ctx, dupFilter).Decode(&existingItem)
    if err == nil {
        // 이미 존재 → 저장하지 않고 멱등하게 기존 데이터 반환
        log.Printf("Duplicate waiting registration detected (idempotent). store_id: %s, waiting_id: %s", item.StoreID, item.WaitingID)
        return &existingItem, nil
    } else if err != mongo.ErrNoDocuments {
        return nil, fmt.Errorf("failed to check duplicate waiting item: %v", err)
    }
}
```

---

## 폴백(Fallback) 처리

클라이언트가 `waiting_id`를 전송하지 않은 경우(레거시 클라이언트 등),  
서버에서 현재 시각 기반의 ID를 생성합니다.

```go
if item.WaitingID == "" {
    now := time.Now()
    // Format: YYYYMMDD-HHmmss-SSS
    item.WaitingID = now.Format("20060102-150405") + "-" + fmt.Sprintf("%03d", now.Nanosecond()/1e6)
}
```

> **경고: 폴백 ID는 멱등성을 보장하지 않습니다.** 클라이언트가 반드시 UUID를 생성하여 전송해야 완전한 보호가 됩니다.

---

## DB 스키마 (waiting_list 컬렉션)

```
{ store_id, waiting_id }  →  복합 유니크 인덱스 권장
```

현재는 애플리케이션 레벨에서만 중복을 차단하고 있으나, DB 레벨 유니크 인덱스를 추가하면 Race Condition까지 완전 차단 가능합니다.

---

## 관련 문서

- [대기열 기능 사양](../features/waiting-list.ko.md)
- [Atomic Counter 구현](./atomic-counter.ko.md)
