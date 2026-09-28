# 003: SSE heartbeat 형식과 좀비 커넥션 회수 시 panic

> 작성일: 2026-09-18
> 상태: 해결됨 (Resolved) / 일부는 환경 측 제약으로 보류
> 관련 파일: [`events/broker.go`](../../events/broker.go), [`events/waiting_user_broker.go`](../../events/waiting_user_broker.go), [`handlers/waiting_list_handler.go`](../../handlers/waiting_list_handler.go), [`fly.toml`](../../fly.toml)

---

## 현상 (Symptom)

점주용 앱(`yoyaku_mate_provider`)에서 "대기를 추가해도 화면에 바로 반영되지 않는다"는 보고가 있었다.
대기 리스트는 SSE(`/api/waiting-list/stream`)로 실시간 갱신되는 설계이므로, 먼저 서버 측 전송을 의심하고 조사했다.

---

## 조사 (Investigation)

개발 서버(`rusui-dev`)에 직접 SSE로 접속해 실측한 결과, **전송 자체는 정상**이었다.

```
21:55:24 data: []        ← 접속 직후 초기 데이터
21:55:48 data: :ping     ← 30초 주기 heartbeat
21:56:18 data: :ping
```

- fly.io 프록시의 버퍼링 없음 (접속 직후 초기 데이터가 도달)
- `metrics.MetricsMiddleware`의 ResponseWriter 래퍼는 `http.Flusher`를 구현하고 있어 Flush가 동작함
- 머신은 1대뿐. 브로커가 in-memory인 데서 오는 분산 문제가 아님
- 클라이언트와 동일한 코드(Dart `package:http`)로 재현해도 정상 수신

전송은 정상인 한편, 이 실측 과정에서 **서버 측 결함 2건**이 드러났다.

---

## 원인 분석 (Root Cause)

### 1. heartbeat가 SSE 주석이 아니라 데이터 이벤트로 전송되고 있었다

`pingAndClean`은 브로커 채널에 `":ping"`을 흘려보내고, 핸들러는 채널에서 받은 문자열을 일괄적으로
`data: %s\n\n`으로 감쌌다. 결과적으로 `data: :ping`이라는 **데이터 이벤트**가 되어 있었다.

```go
// 수정 전: handlers/waiting_list_handler.go
case msg := <-clientChan:
    fmt.Fprintf(w, "data: %s\n\n", h.filterStreamMessage(msg, isStaff))
```

`pingAndClean`의 주석에는 "SSE 규격의 주석 형식(`:ping\n\n`)은 클라이언트에서 이벤트로 수신되지 않습니다"라고
쓰여 있었지만 **실제 동작은 그 반대**였다. 코드와 주석이 모순된 상태였고, 이를 읽은 사람이
"heartbeat는 클라이언트에 도달하지 않는다"고 오해할 수 있었다.

그 부담은 클라이언트가 떠안고 있었다. 고객 웹은 `yoyaku_mate/src/api/waitingService.js`에서
`:`로 시작하는 메시지를 명시적으로 걸러내고 있었고, 점주 앱은 `json.decode(":ping")`의 예외를 삼키고 있었다.

### 2. 좀비 커넥션 회수가 끝난 채널을 `RemoveClient`가 다시 close해 panic

`RemoveClient`는 "해당 매장의 맵이 존재하는가"만 보고 `close(clientChan)`을 호출했다.

```go
// 수정 전
if clients, ok := b.Clients[storeID]; ok {
    delete(clients, clientChan)
    delete(b.connectedAt, clientChan)
    close(clientChan)   // ← 이미 close된 채널이어도 여기까지 도달한다
    ...
}
```

다음 순서로 `close of closed channel` panic이 발생한다.

1. 어떤 클라이언트의 채널(버퍼 10)이 가득 참 = 소비가 따라가지 못하는 상태
2. `pingAndClean`이 이를 좀비로 판정해 `close(ch)` 후 맵에서 제거
3. **같은 매장에 다른 클라이언트가 남아 있으면 `b.Clients[storeID]`는 계속 존재한다**
4. 해당 커넥션의 핸들러가 종료되며 `defer RemoveClient`가 실행됨
5. 채널 단위 등록 확인이 없어 `close()`까지 도달 → panic

`net/http`가 핸들러 panic을 커넥션 단위로 recover하므로 프로세스 전체가 죽지는 않지만,
스택트레이스가 로그에 계속 쌓이고 해당 커넥션은 강제 종료된다.

여기에 더해 2번 직후, 핸들러 수신 측에도 문제가 있다.

```go
case msg := <-clientChan:   // close된 채널은 즉시 제로값을 계속 반환한다
```

close된 채널에서의 수신은 블로킹되지 않고 즉시 빈 문자열을 반환하므로,
요청 컨텍스트가 취소될 때까지 **루프가 CPU를 태운다**.
`shared-cpu-1x:256MB` 머신에서는 다른 요청에도 영향을 준다.

### 3. (환경 측) fly.io 머신 자동 정지가 in-memory 브로커와 맞지 않음

`fly.toml`이 `auto_stop_machines = 'stop'` + `min_machines_running = 0`으로 되어 있어
유휴 시 머신이 정지한다. 조사 중 날린 curl이 콜드 스타트를 유발한 로그가 남아 있다.

```
12:55:12Z runner  Machine started in 1.275s
12:55:18Z app     Server starting on :8080...
12:55:18Z proxy   machine became reachable in 6.296s
```

머신이 멈추면 SSE 커넥션과 브로커의 구독자 목록이 통째로 사라진다.
그 사이 발생한 갱신의 브로드캐스트는 **구독자가 0명이므로 영구히 유실**되고,
클라이언트는 재연결 시 스냅샷으로만 따라잡을 수 있다. 콜드 스타트는 약 6초.

---

## 해결책 (Solution)

### 1. heartbeat 센티널 도입과 주석 행으로의 변환

좀비 감지를 위해 heartbeat는 채널을 통과해야 하므로(채널이 막혀 있다는 것 자체가 감지 조건),
전송하는 값을 센티널로 정의하고 **SSE 행으로의 변환은 핸들러의 책임**으로 분리했다.

```go
// events/broker.go
const HeartbeatMessage = ":ping"
```

```go
// handlers/waiting_list_handler.go
case msg, ok := <-clientChan:
    if !ok {
        return
    }
    if msg == events.HeartbeatMessage {
        fmt.Fprintf(w, "%s\n\n", msg)          // → ":ping\n\n" (SSE 주석)
    } else {
        fmt.Fprintf(w, "data: %s\n\n", h.filterStreamMessage(msg, isStaff))
    }
    w.(http.Flusher).Flush()
```

`HandleStream` / `HandleWaitingItemStream` 양쪽 모두에 적용했다.

### 2. `RemoveClient`에 채널 단위 등록 확인 추가

```go
clients, ok := b.Clients[storeID]
if !ok {
    return
}
if _, exists := clients[clientChan]; !exists {
    return   // pingAndClean이 이미 회수함. 이중 close 방지
}
```

`Broker`와 `WaitingUserBroker`는 동일한 구조이므로 양쪽에 같은 수정을 넣었다.
아울러 수신을 `msg, ok := <-clientChan`으로 바꿔 채널이 닫힌 시점에 커넥션을 정리하도록 했다.

### 3. 회귀 테스트 추가

[`events/broker_test.go`](../../events/broker_test.go)에 3건 추가했다.
특히 `TestRemoveClientAfterZombieCleanup`은 가드를 제거하면 실제로
`RemoveClientがpanicした: close of closed channel`로 실패하는 것을 확인했으므로,
이 수정이 사라지면 감지할 수 있다.

### 4. fly.io 설정은 개발 환경에서는 유지

`auto_stop_machines`는 개발 환경에서는 비용 측면의 판단으로 그대로 두고,
대신 **클라이언트 측에 끊김 감지와 재연결을 구현**해 10초 이내에 복구되도록 했다
(`yoyaku_mate_provider`의 `docs/troubles/003-sse-stale-connection-recovery.ko.md` 참조).

프로덕션 구축 시에는 `auto_stop_machines = 'off'` + `min_machines_running = 1`로 올려야 한다.

---

## 결과와 정리 (Consequences)

- heartbeat가 `:ping\n\n` 주석 행으로 전송되어, 클라이언트 측의 제외 처리가 불필요해졌다
  (기존 클라이언트는 `:` 시작을 걸러내는 구현이므로 그대로 동작한다)
- 좀비 커넥션 회수 후의 panic과 CPU 스핀이 해소되었다
- **교훈 1**: keep-alive를 데이터 이벤트로 보내면 모든 클라이언트가 그 제외 처리를 구현할 의무를 진다.
  프로토콜 이탈은 구현한 본인이 아니라 사용하는 쪽에 비용을 떠넘기는 형태로 드러난다
- **교훈 2**: `close()`를 동반하는 리소스 회수 경로가 여러 개일 때(정기 클린업과 `defer`),
  "아직 내 관리 하에 있는가" 확인을 양쪽 모두에 넣어야 한다. 한쪽에만 있으면
  제3의 조건(같은 키에 다른 클라이언트가 남아 있음)이 갖춰졌을 때만 재현된다
- **교훈 3**: 코드와 주석이 모순된 자리가 그대로 실제 버그였다.
  "주석상 맞으니 괜찮다"고 넘기지 말고, 실제로 전송되는 바이트열을 확인할 것

---

## 관련 문서

- [ADR-001: 실시간 통신에 SSE 채택](../decisions/ADR-001-use-sse.ko.md)
- [ADR-005: SSE 좀비 커넥션 감지와 회수](../decisions/ADR-005-sse-zombie-detection.ko.md) — 이번에 수정한 `pingAndClean` / `RemoveClient`의 설계 근거
- [기능 사양서: 대기 리스트](../features/waiting-list.ko.md)
- [001: 개발 과정 회고 (Goroutine 리크, Rate Limiter 조정)](./001-lessons-learned.ko.md)
- [002: 실시간 접속자 중복 카운트 방지](./002-active-user-ip-port-issue.ko.md)
