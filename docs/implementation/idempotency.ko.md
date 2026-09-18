# 멱등성 구현 (Idempotency)

> 최종 수정: 2026-09-18
> 관련 파일: [`data/waiting_list_repo.go`](../../data/waiting_list_repo.go), [`db/mongo.go`](../../db/mongo.go), [`handlers/waiting_list_handler.go`](../../handlers/waiting_list_handler.go)

## 문제 배경

모바일 환경에서는 네트워크 불안정으로 인해 동일한 API 요청이 중복 전송될 수 있습니다.
특히 대기 등록(`POST /api/waiting-list`)에서 중복 등록이 발생하면 손님이 두 개의 번호표를 받게 되는 치명적인 오류가 생깁니다.

---

## 세 개의 층으로 막는다

멱등성은 애플리케이션 계층만으로는 지킬 수 없다. 여기서는 셋을 조합한다.

| 층 | 역할 | 없으면 |
|---|---|---|
| ① 클라이언트 생성 멱등 키 | "같은 요청"을 서버가 식별할 수 있게 함 | 재시도마다 새 번호표가 나옴 |
| ② DB 유니크 인덱스 | 동시에 도착한 재시도를 1건으로 수렴 | ①을 빠져나감(아래 경합) |
| ③ 중복키 에러의 출처별 분기 | "재시도"와 "남남의 충돌"을 구별 | 손님 B에게 손님 A의 번호표를 줌 |

---

## ① 클라이언트 생성 멱등성 키

클라이언트(고객 웹·점주 앱)가 요청 전에 `waiting_id`를 생성해 Body에 담아 보낸다.
서버는 먼저 `(store_id, waiting_id)`로 조회하고, 이미 있으면 그것을 반환한다(패스트 패스).

```
클라이언트                         서버
   │── waiting_id 생성 ─────────→   │
   │── POST /waiting-list ──────────→│── DB 조회: waiting_id 존재?
   │   [최초]                        │── 없음 → 새 레코드 삽입
   │←── 201 Created ────────────────│
   │   [재시도]                      │── 있음 → 기존 데이터 반환
   │←── 201 Created (동일 데이터) ──│  (새 삽입 없음)
```

`waiting_id` 형식은 클라이언트마다 다르고 시기에 따라서도 바뀌어 왔다.
**서버는 형식을 검증하지 않는다.** 길이 상한(`maxWaitingIDLength` = 64자)만 부과한다.

> 이전에는 `utils.IsValidWaitingID`라는 형식 검증 함수가 있었으나 삭제했다.
> 어디서도 호출되지 않았고, 그대로 방치되어 실태와 어긋나 있었다
> (현재 클라이언트가 보내는 23자 형식을 거부하는 상태였다).
> 형식을 검증하면 클라이언트가 형식을 바꿀 때마다 따라가야 하고,
> 잊어도 호출되지 않으면 알아챌 수 없다. 상한만 두면 클라이언트가 바뀌어도 안 깨진다.

---

## ② DB 유니크 인덱스

```go
// db/mongo.go - EnsureIndexes()
Options: options.Index().SetName("idx_store_waiting_id_unique").SetUnique(true)
```

**①만으로는 못 막는다.** "조회 후 삽입"은 원자적이지 않아서, 재시도가 동시에 두 개 도착하면
둘 다 "없음"을 보고 둘 다 삽입으로 넘어간다. 네트워크 불안정에 의한 재시도는 바로 그 동시 도착이므로,
①이 지키려던 케이스에서 정확히 빠져나간다.

### 마이그레이션 주의

기존 non-unique `idx_store_waiting_id`와 **다른 이름**으로 만들었다.
같은 이름으로 옵션만 바꿔 `CreateOne`하면 `IndexOptionsConflict`로 실패한다.
"새 것을 만든 뒤 옛것을 제거" 순서이며, 반대로 하면 전환 순간에만 인덱스가 없는 구간이 생긴다
(이 키는 대기 호출 때마다 조회되는 핫패스다).

유니크화는 기존 데이터에 중복이 있으면 실패한다. 그 경우 구 인덱스를 그대로 남긴다
(제거하면 핫패스가 컬렉션 스캔이 된다).

---

## ③ 중복키 에러의 출처별 분기

**같은 "중복키 에러"라도 `waiting_id`를 누가 만들었는지에 따라 의미가 정반대다.**

```go
// data/waiting_list_repo.go - CreateItem()
clientSupplied := item.WaitingID != ""   // 생성으로 덮이기 전에 확정
```

| 출처 | 중복키 에러의 의미 | 올바른 대응 |
|---|---|---|
| 클라이언트 생성 | 같은 멱등 키 = **같은 요청의 재시도** | 기존 레코드 반환 |
| 서버 생성 | 단순 충돌 = **서로 다른 손님** | ID를 다시 뽑아 재시도 |

서버 생성인데 기존을 반환해버리면 **손님 B에게 손님 A의 번호표를 주게 된다.**
중복 등록을 막으려다 더 나쁜 결과를 만드는 셈이다.

### 채번은 루프 밖에서 한 번만

재시도마다 `GetNextQueueNumber`를 부르면 충돌할 때마다 번호가 뛰어 **손님에게 보이는 결번**이 생긴다.
채번은 루프 진입 전에 끝내고, 루프 안에서는 ID 재생성과 INSERT만 한다.

---

## 서버 측 폴백 ID

클라이언트가 `waiting_id`를 보내지 않은 경우 서버가 생성한다.

```go
// 형식: YYYYMMDD-HHMMSS-xxxxxx (JST, 끝은 crypto/rand 기반 6자)
suffix := utils.GenerateRandomString(6)
return time.Now().In(jst).Format("20060102-150405") + "-" + suffix, nil
```

이전에는 `YYYYMMDD-HHMMSS-mmm`으로 밀리초까지의 시각뿐이어서 **같은 밀리초에 등록이 겹치면 충돌했다.**
유니크 인덱스를 건 이상, 충돌은 조용한 이중 등록이 아니라 등록 실패로 겉에 드러난다.

`utils.GenerateRandomString`은 crypto/rand 실패 시 빈 문자열을 반환하므로 그 지점에서 에러로 처리한다.
모르고 지나가면 접미사 없는 ID가 양산되어 충돌이 되살아난다.

> **폴백 ID는 멱등성을 보장하지 않는다.** 재시도마다 다른 ID가 되므로,
> 이중 등록을 막으려면 ①이 필요하다. 폴백이 지키는 것은 "충돌하지 않는 것"뿐이다.

---

## 멱등 경로의 응답은 가린다

멱등 경로에서는 **기존 레코드가 그대로 반환된다.** 즉 `waiting_id`를 추측해서 던지기만 하면
다른 손님의 레코드(`contact` = 전화번호 포함)를 읽을 수 있는 경로가 된다.

시각 기반 ID는 추측하기 쉽고, 특히 구 형식은 초 단위까지밖에 없어 전수 탐색이 현실적이었다.

```go
// handlers/waiting_list_handler.go - handleCreateWaitingList()
responseItem := *createdItem
if newWaiting.Source == "web" {
    responseItem = redactItemForPublic(responseItem)
}
```

판정은 **호출자가 인증을 통과했는지**(`newWaiting.Source`)로 한다.
기존 레코드 쪽 `source`로 판정하면, 남이 앱으로 등록한 손님의 정보가 샌다.

게스트 자신이 곤란할 일은 없다. 손님에게 필요한 것은 `waiting_id`와 번호·예상 시간이고,
자기가 보낸 내용은 손에 있다.

---

## DB 스키마 (waiting_list 컬렉션)

```
{ store_id: 1, waiting_id: 1 }  →  idx_store_waiting_id_unique (unique: true)
```

---

## 관련 문서

- [대기열 기능 사양](../features/waiting-list.ko.md)
- [Atomic Counter 구현](./atomic-counter.ko.md)
