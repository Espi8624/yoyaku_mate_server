# SSE (Server-Sent Events) 구현

> 최종 수정: 2026-07-10  
> 관련 파일: [`events/broker.go`](../../events/broker.go), [`events/waiting_user_broker.go`](../../events/waiting_user_broker.go), [`handlers/waiting_list_handler.go`](../../handlers/waiting_list_handler.go)

## 개요

폴링(Polling) 방식에서 SSE 방식으로 전환하여 불필요한 네트워크 요청을 제거하고, 대기 상태 변경을 실시간으로 전달합니다.

**사용 채널 (2종류)**

| 채널 | 엔드포인트 | 구독 키 | 사용처 |
|------|-----------|---------|--------|
| Store Broker | `/api/waiting-list/stream` | `store_id` | 관리자 앱 / 순번 표시판 |
| User Broker | `/api/waiting-list/stream-user` | `store_id:waiting_id` | 손님 대기 화면 |

---

## 아키텍처

```
                    ┌─────────────────────────┐
                    │         Broker          │
                    │  (in-memory singleton)  │
                    │                         │
                    │  Clients map:           │
                    │  "store_A" → [ch1, ch2] │
                    │  "store_B" → [ch3]      │
                    └────────────┬────────────┘
                                 │ Broadcast()
               ┌─────────────────┼─────────────────┐
               ▼                 ▼                  ▼
          [ch1: Admin]     [ch2: Board]        [ch3: Admin]
          goroutine        goroutine           goroutine
               │                │                   │
         SSE Response     SSE Response         SSE Response
```

---

## Broker 구조

```go
// events/broker.go
type Broker struct {
    Clients     map[string]map[chan string]bool
    connectedAt map[chan string]time.Time        // 각 채널의 연결 시각 (좀비 감지 및 평균 유지 시간 계산용)
    Mutex       sync.RWMutex
}
```

- **싱글톤 패턴**: `sync.Once`로 인스턴스 1개만 유지하며, 초기화 시 `startHeartbeat()` 백그라운드 고루틴을 실행
- **RWMutex**: 읽기(Broadcast, GetStats)는 병렬 허용, 쓰기(Add/Remove, pingAndClean)는 직렬화
- **채널 버퍼**: `make(chan string, 10)` — 슬로우 클라이언트로 인한 블로킹 방지
- **Heartbeat & 좀비 제거**: 30초 주기로 전송을 시도하여 블로킹 상태인 좀비 채널을 감지 및 자동 `close` 정리

---

## 연결 흐름

```go
// handlers/waiting_list_handler.go - HandleWaitingListStream()

// 1. SSE 헤더 설정
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")

// 2. 클라이언트 등록
clientChan := make(chan string, 10)
broker.AddClient(storeID, clientChan)
defer broker.RemoveClient(storeID, clientChan)  // 연결 종료 시 자동 정리

// 3. 초기 데이터 전송 (연결 즉시)
go func() {
    waitingList, _ := data.GetWaitingListData(storeID)
    jsonData, _ := json.Marshal(waitingList)
    broker.Broadcast(storeID, string(jsonData))
}()

// 4. 이벤트 루프
for {
    select {
    case <-r.Context().Done():
        // 클라이언트 연결 종료 → SSE_DISCONNECT 에러 로그 기록
        metrics.GetTracker().RecordError(models.ErrorLog{
            Timestamp: time.Now().UTC(),
            ErrorType: "SSE_DISCONNECT",
            Message:   "SSE Client Disconnected",
            Path:      r.URL.Path,
            Method:    r.Method,
            ClientIP:  r.RemoteAddr,
        })
        return
    case msg := <-clientChan:
        fmt.Fprintf(w, "data: %s\n\n", msg)
        w.(http.Flusher).Flush()
    }
}
```

---

## 브로드캐스트 트리거

대기열에 변경이 발생하는 모든 지점에서 `notifyStore(storeID)`를 호출합니다.

```
notifyStore(storeID)  ──→  Broker.Broadcast()
                            │
                            └──→  notifyWaitingUsers()
                                   (개별 손님 채널)
```

> **100ms 지연**: 대기 등록 직후 브로드캐스트 시 MongoDB에서 새 데이터가 즉시 조회되지 않는 이슈를 방지하기 위해, 등록 후 100ms 대기 후 비동기 브로드캐스트합니다.

```go
go func() {
    time.Sleep(100 * time.Millisecond)
    notifyStore(newWaiting.StoreID)
}()
```

---

## 슬로우 클라이언트 처리

채널이 가득 차있을 경우 해당 클라이언트로의 전송을 건너뛰어(skip), 서버 전체가 블로킹되는 상황을 방지합니다.

```go
// events/broker.go - Broadcast()
select {
case clientChan <- message:
default:
    // 채널 블로킹 → 해당 클라이언트 스킵 (다른 클라이언트에는 영향 없음)
}
```

---

## SSE 데이터 포맷

```
data: {"store_id":"abc","waiting_id":"...","queue_number":5,...}\n\n
```

표준 SSE 포맷 (`data: <payload>\n\n`)을 사용하며, 별도의 이벤트 타입(`event:`)은 현재 미사용입니다.

---

## 폴링 레거시 엔드포인트

`GET /api/waiting-list/poll` — SSE를 지원하지 않는 환경을 위한 레거시 엔드포인트로, 내부적으로 동일한 `GetWaitingListData`를 호출합니다.

---

## 관련 문서

- [대기열 기능 사양](../features/waiting-list.ko.md)
- [기술 선택 근거 (SSE vs WebSocket)](../decisions/ADR-001-use-sse.ko.md)
