# 대기열 기능 (Waiting List)

> 최종 수정: 2026-07-10

## 개요

손님이 QR 코드를 스캔하여 번호표를 발급받고, 실시간으로 대기 순서를 확인하는 핵심 기능입니다.  
점주/스태프는 앱을 통해 대기 상태를 관리하며, SSE를 통해 모든 클라이언트에 변경 사항이 실시간으로 전달됩니다.

---

## 주요 개념

| 개념 | 설명 |
|------|------|
| `waiting_id` | 클라이언트가 생성하는 UUID 형식의 고유 식별자. 멱등성 키로 활용 |
| `queue_number` | 영업일 단위로 발급되는 순번 (Atomic Counter 기반) |
| `source` | 등록 경로 — `"web"` (QR 스캔), `"app"` (스태프 직접 등록) |
| `business_day` | 영업 시간 기반의 날짜 경계 (Dynamic Cutoff). 심야 영업 지원 |

---

## 상태 전이도

```
waiting ──→ notified ──→ completed
    │                        
    └──→ cancelled  (손님 직접 / 스태프 조작)
    └──→ no_show    (영업일 경과 시 자동 만료)
```

### 상태 설명

| 상태 | 설명 | called_time | entry_time |
|------|------|:-----------:|:----------:|
| `waiting` | 대기 중 | - | - |
| `notified` | 호출됨 | Y | - |
| `completed` | 입장 완료 | Y | |
| `cancelled` | 취소 | - | - |
| `no_show` | 자동 만료 (영업일 초과) | - | - |

---

## API 엔드포인트

### `GET /api/waiting-list`

현재 영업일의 대기열 전체 조회.

**Query Parameters**

| 파라미터 | 필수 | 설명 |
|---------|:----:|------|
| `store_id` | Y | 점포 ID |
| `action=average_waiting_time` | - | 평균 대기 시간 조회 모드 |
| `action=qr_token` | - | 당일 QR 토큰 발급 모드 |

**Response (대기열 조회)**
```json
[
  {
    "id": "...",
    "store_id": "abc123",
    "waiting_id": "20260710-120000-001",
    "queue_number": 5,
    "party_size": 2,
    "nationality": "JP",
    "registration_time": "2026-07-10T12:00:00.000+09:00",
    "status": "waiting",
    "estimated_wait_time": 40,
    "menu_items": [],
    "source": "web"
  }
]
```

---

### `POST /api/waiting-list`

신규 대기 등록.

**Query Parameters**

| 파라미터 | 필수 | 설명 |
|---------|:----:|------|
| `v_token` | Y | 당일 HMAC 기반 QR 토큰 (위변조 방지) |

**Request Body**
```json
{
  "store_id": "abc123",
  "waiting_id": "client-generated-uuid",
  "party_size": 2,
  "nationality": "JP",
  "contact": "090-1234-5678",
  "menu_items": [
    { "menu_id": "m1", "name": "라멘", "quantity": 2 }
  ]
}
```

**Authorization (선택)**
- 헤더 없음: 손님(QR 스캔)으로 처리, `source = "web"`
- `Authorization: Bearer <firebase_token>` + `X-Login-Token: <session_token>`: 스태프 등록, `source = "app"`

**비즈니스 규칙**
- `v_token` HMAC 검증 → 실패 시 `403`
- `party_size`가 점포 설정의 `max_waiting_count` 초과 시 `400`
- `enable_menu_selection = true`이면 `menu_items` 필수
- `require_one_menu_per_person = true`이면 메뉴 수량 합 ≥ party_size
- 점포 `license_status`가 `APPROVED`가 아니면 `403`
- `waiting_id`가 이미 존재하면 기존 데이터를 그대로 반환 (멱등성 보장)

---

### `PATCH /api/waiting-list?action=status`

대기 상태 변경.

**Request Body**
```json
{
  "store_id": "abc123",
  "waiting_id": "...",
  "status": "notified"
}
```

**권한**
- `status = "cancelled"`: 인증 불필요 (손님 직접 취소 허용)
- 그 외: Firebase Auth + X-Login-Token + 점포 권한 필수

---

### `POST /api/waiting-list?action=clear`

영업일 전체 대기열 초기화 (`waiting` 상태 전체를 `cancelled`로 변경).

**권한**: Firebase Auth + X-Login-Token + 점포 권한 필수

---

### `GET /api/waiting-list/stream`

점포 대기열 전체 실시간 스트림 (SSE).

**Query Parameters**: `store_id`

연결 즉시 현재 대기열 데이터를 초기값으로 전송하며, 이후 변경 발생 시 자동 브로드캐스트됩니다.  
→ [SSE 구현 상세](../implementation/sse.ko.md)

---

### `GET /api/waiting-list/stream-user`

개별 손님의 실시간 대기 상태 스트림 (SSE).

**Query Parameters**: `store_id`, `waiting_id`

**Response 포함 필드 (WaitingUserResponse)**
```json
{
  "...waiting_list_fields": "...",
  "waiting_count": 3,
  "estimated_waiting_time": "30 mins"
}
```

- `waiting_count`: 전체 활성 대기 수 (`waiting` + `notified` 상태 합산)
- `estimated_waiting_time`: 자신보다 **앞선 팀 수** × `estimated_wait_time` 설정값 (자신의 queue 순서 인덱스 기준)

---

### `GET /api/waiting-list/poll`

폴링 방식의 대기열 조회 (SSE 미지원 환경 대비 레거시 엔드포인트).

---

## 주요 로직

### QR 토큰 검증

당일 날짜 + store_id를 HMAC으로 서명한 토큰으로, QR 코드 위변조를 방지합니다.  
Dynamic Cutoff 기반의 영업일 기준으로 토큰 유효성을 판단합니다.  
→ [상세: utils/hmac.go]

### Dynamic Business Day Cutoff

`GetBusinessDayCutoff(storeID, now)` 함수는 점포의 운영 시간 설정을 기반으로 "오늘의 영업일"을 계산합니다.

| 조건 | Cutoff |
|------|--------|
| 24시간 영업 | `reset_time` (기본 06:00) |
| 일반 영업 | `start_time - 1h` |
| 설정 없음 | 04:00 AM |

심야 영업 (예: 23:00 개점)의 경우 전날과 오늘의 데이터를 올바르게 묶어주는 역할을 합니다.

### 예상 대기 시간 계산

```
estimated_wait_time = 자신보다 앞선 팀 수 × minutesPerTeam
```

- `minutesPerTeam`: 점포 설정 `estimated_wait_time` (기본값: 10분)
- N+1 쿼리 방지를 위해 설정은 루프 밖에서 1회만 조회
- 미래에는 AI 기반 로직으로 대체 예정 (`CalculateEstimatedWaitTime` 함수 교체)

### 자동 만료 (Auto Expire)

`GetWaitingListData` 호출 시 자동으로 `AutoExpireWaitingItems`가 실행되어,  
현재 영업일 Cutoff 이전에 등록된 `waiting`/`notified` 상태 항목을 `no_show`로 변경합니다.

---

## 관련 문서

- [멱등성 구현](../implementation/idempotency.ko.md)
- [SSE 구조](../implementation/sse.ko.md)
- [Atomic Counter](../implementation/atomic-counter.ko.md)
