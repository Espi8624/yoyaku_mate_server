# 004: 실서비스 대비 점검에서 발견된 4건

> 작성일: 2026-09-18
> 상태: 코드 측은 해결됨 (Resolved) / 자격증명 로테이션은 미실시
> 관련 파일: [`db/mongo.go`](../../db/mongo.go), [`utils/safego.go`](../../utils/safego.go), [`handlers/waiting_list_handler.go`](../../handlers/waiting_list_handler.go), [`main.go`](../../main.go)

---

## 경위

[003](./003-sse-heartbeat-and-zombie-cleanup.ko.md)의 SSE 조사가 일단락된 뒤,
"실서비스에서 문제가 될 만한 곳이 또 없는지"라는 관점으로 서버를 점검했다.
증상 신고가 있었던 것이 아니라 선제적 확인에서 나온 4건이다.

아래는 모두 실제 코드와 fly.io 로그로 확인한 것이며, 추측은 포함하지 않았다.

---

## 1. MongoDB 자격증명이 로그에 평문으로 남아 있었다

### 현상

`InitMongoDB`가 접속 문자열을 그대로 로그에 출력하고 있었다.

```go
log.Printf("Try mongoDB connect: %s", uri)   // 수정 전
```

로컬 표준 출력에만 나오는 것으로 인식되어 있었지만, `flyctl logs -a rusui-dev`로
**개발 서버 로그에도 같은 행이 쌓이고 있는 것**을 확인했다.
사용자명·비밀번호·클러스터 호스트·DB명이 전부 평문으로 포함된다.

fly.io 로그는 보존되고 앱 조회 권한이 있으면 누구나 읽을 수 있다.
자격증명을 둘 곳으로는 최악에 가깝다.

### 해결책

자격증명을 가리고, 접속 대상 특정에 필요한 스킴·호스트·DB명만 남긴다.

```go
masked := *parsed
if parsed.User != nil {
    masked.User = url.User("REDACTED")
}
masked.RawQuery = ""
```

설계상의 판단이 두 가지 있다.

- **파싱에 실패하면 URI를 아예 출력하지 않는다.** 원문으로 폴백하면
  "가린 줄 알았는데 나오고 있는" 가장 위험한 상태가 된다
- **가림 문자는 기호가 아니라 영문자를 쓴다.** `"***"`는 `url.User`에 의해
  퍼센트 인코딩되어 `%2A%2A%2A`가 되고 로그 가독성이 나빠진다

### 미완료 작업

**코드 수정만으로는 끝나지 않는다.** 이미 로그에 나가버린 자격증명은
무효화되지 않으므로, Atlas 쪽에서 로테이션이 별도로 필요하다.

```
① Atlas에서 새 DB 유저 생성  →  ② Infisical의 MONGODB_URI 갱신
→  ③ 재배포 후 연결 확인      →  ④ 기존 유저 삭제
```

순서를 바꾸면 다운타임이 발생한다. ④를 먼저 하면 서버가 즉시 멈춘다.

---

## 2. 백그라운드 goroutine에 패닉 보호가 없었다

### 현상

저장소 전체에서 `recover()` 사용처가 **0건**이었다.

`net/http`는 핸들러 안의 패닉을 커넥션 단위로 recover하지만,
**핸들러 밖에서 기동한 goroutine은 보호되지 않는다.** 해당하는 것이 11개 있었고,
그중 하나라도 패닉하면 프로세스가 통째로 죽는다.
전 매장의 SSE 커넥션과 메트릭스 버퍼가 동시에 사라지게 된다.

놓치기 쉬운 점은, **핸들러 안에 쓰여 있어도 recover 범위 밖**이라는 것이다.

```go
func (h *WaitingListHandler) HandleStream(...) {   // ← 여기는 recover됨
    go func() {                                     // ← 여기는 안 됨
        ...
    }()
}
```

`waiting_list_handler.go`의 SSE 초기 데이터 전송과 대기 생성 후 알림이
정확히 이 형태였다.

### 해결책

`utils.Go` / `utils.GoForever`를 추가하고 모든 기동 지점을 여기에 통과시켰다.

**`GoForever`가 단순 recover가 아니라 "재개"하는 이유**가 중요하다.
recover하고 종료만 하면 워커가 조용히 멈춘 채 프로세스는 계속 살아 있다.
heartbeat가 멈추면 좀비 커넥션 회수가 영구히 동작하지 않게 되는데,
프로세스 사망과 달리 밖에서 알아챌 수 없다. **죽는 것보다 발견이 늦어 더 나쁘다.**

재개 간격은 패닉 원인이 해소되지 않은 경우 로그가 넘치지 않도록 5초를 둔다.

---

## 3. 공개 SSE에서 다른 손님의 notes가 전송되고 있었다

### 현상

`/waiting-list/stream`과 `/waiting-list/poll`은 무인증 공개 엔드포인트로,
`store_id`만 알면 누구나 구독할 수 있다. `store_id`는 QR URL과 모니터 보드
URL에 포함되므로, **한 번이라도 QR을 읽은 손님은 그 매장 대기 리스트를 영구히 구독할 수 있다.**

가리고 있던 것은 `contact`뿐이었고, `notes`(손님이 자유 기술한 요청사항)와
`menu_items`(주문 내용)는 그대로 전송되고 있었다.
특히 자유 기술은 무엇이 적힐지 통제할 수 없다.

### 해결책

`redactContacts`를 `redactForPublic`으로 개명하고 제거 대상을 넓혔다.

제거 범위의 선긋기에는 근거가 있다.

- 유일한 공개 구독자인 모니터 보드
  (`yoyaku_mate/src/containers/board/Board.jsx`)는
  `status` / `queue_number` / `waiting_id` **3개만 참조한다.**
  호출 지점을 확인한 뒤 제거했으므로 표시가 깨지지 않는다
- `party_size`와 `nationality`는 남긴다. 개인을 특정하는 정보가 아니며,
  제거 범위를 넓힐수록 파악하지 못한 사용처를 깨뜨릴 위험이 커진다
- 스태프 단말은 `X-Session-Id`를 보내므로 기존대로 전 항목을 받는다
  (이 헤더가 붙게 된 경위는 `yoyaku_mate_provider`의
  `docs/troubles/003-sse-stale-connection-recovery.ko.md` 참조)

---

## 4. HTTP 서버에 타임아웃이 하나도 없었다

### 현상

`http.ListenAndServe`를 직접 호출하고 있어 `ReadHeaderTimeout`도 `IdleTimeout`도
미설정이었다. 기본값이 무제한이므로 헤더를 조금씩 계속 보내는 것만으로 커넥션을
점유할 수 있다(Slowloris). `shared-cpu-1x:256MB`에서는 특히 잘 통한다.

### 해결책

`http.Server`를 명시적으로 구성하고, 함께 graceful shutdown도 마련했다.

```go
ReadHeaderTimeout: 10 * time.Second    // Slowloris를 막는 데는 이 한 지점으로 충분
IdleTimeout:      120 * time.Second    // keep-alive 대기 중 커넥션 회수
// ReadTimeout / WriteTimeout 은 설정하지 않음
```

### 이 설정을 바꾸려는 사람에게

**`WriteTimeout`을 설정해서는 안 된다.** 넣으면 그 시간마다
모든 SSE 커넥션이 응답 도중에 강제 절단된다.
`IdleTimeout`은 요청과 요청 "사이"에만 적용되고
핸들러 실행 중에는 관여하지 않으므로 SSE에 영향이 없다
(로컬에서 200초 커넥션 유지를 실측해 확인함).

**Shutdown 유예를 늘려서는 안 된다.** 3초로 둔 데에는 이유가 있다.

- SSE 커넥션은 스스로 종료하지 않으므로 `Shutdown`은 **반드시 유예를 다 쓴다**
- 한편 fly.io는 SIGTERM 5초 뒤(`kill_timeout` 기본값)에 SIGKILL을 보낸다
- 유예를 그보다 길게 잡으면 매번 SIGKILL이 먼저 도착해,
  그 뒤의 `metrics.FlushAll()`에 도달하지 못한다 = 감사 로그가 매번 유실된다

유예를 늘리고 싶다면 `fly.toml`의 `kill_timeout`을 먼저 올릴 것.
실측으로 종료까지 3초이며 `FlushAll`까지 도달한다.

유예 초과 후에는 `srv.Close()`로 남은 SSE 커넥션을 명시적으로 닫는다.
클라이언트는 재연결로 복귀한다.

---

## 점검했으나 문제가 없던 곳

선제적 점검은 "괜찮았던 범위"도 기록해두지 않으면 같은 곳을 반복해서 조사하게 된다.

- **메트릭스 버퍼**: 1000건 상한이 있어 스파이크 시에도 OOM되지 않음
- **타입 단언**: 전부 `switch .(type)` 또는 `, ok`로 가드되어 있음
- ~~**시크릿 커밋**: 히스토리상으로도 없음
  (`config.development.json`은 추적되고 있으나 자격증명 미포함)~~
  → **이 판단은 틀렸다.** 2026-09-18에 [006](./006-committed-credential-in-history.ko.md)에서
  `config.development.json`에 실제 Atlas 접속 문자열(계정·비밀번호 포함)이 들어간 채
  커밋되어 있었고, 히스토리 51개 커밋에 남아 있음을 확인했다.
  당시 점검은 **현재 시점의 파일만 보고** 히스토리를 보지 않았다.
  삭제(`379d149`)된 파일은 `git ls-files`에도 `git grep`에도 나오지 않는다
- **`config/config.go`의 로그**: 환경변수의 "이름"만 출력하고 값은 출력하지 않음

---

## 교훈

- **"로컬만"이라는 전제는 확인하지 않으면 무너진다.** 자격증명 로그는
  로컬 개발 중의 이야기로 보류되어 있었지만, 실제로는 원격 개발 서버에서도
  같은 코드가 돌고 있었다. 보류 판단 자체는 타당해도 전제가 틀렸다
- **패닉의 영향 범위는 "어디에 쓰여 있는가"가 아니라 "어느 goroutine인가"로 정해진다.**
  핸들러 안의 `go func(){}`는 보호되지 않는다
- **타임아웃은 "길게 잡아두면 안전"이 아니다.** 상류(fly.io의 `kill_timeout`)보다
  긴 유예는 유예가 없는 것과 같은 결과가 된다

---

## 관련 문서

- [003: SSE heartbeat 형식과 좀비 커넥션 회수 시 panic](./003-sse-heartbeat-and-zombie-cleanup.ko.md)
- [ADR-001: 실시간 통신에 SSE 채택](../decisions/ADR-001-use-sse.ko.md)
- [ADR-005: SSE 좀비 커넥션 감지와 회수](../decisions/ADR-005-sse-zombie-detection.ko.md)
- [001: 개발 과정 회고](./001-lessons-learned.ko.md)
