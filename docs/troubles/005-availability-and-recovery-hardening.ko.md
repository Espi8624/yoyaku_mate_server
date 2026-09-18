# 005: 가용성과 복구 경로 점검에서 발견된 4건

> 작성일: 2026-09-18
> 상태: 해결됨 (Resolved)
> 관련 파일: [`handlers/translation_handler.go`](../../handlers/translation_handler.go), [`handlers/middleware.go`](../../handlers/middleware.go), [`db/mongo.go`](../../db/mongo.go), [`main.go`](../../main.go), [`fly.toml`](../../fly.toml)

---

## 경위

[004](./004-production-readiness-hardening.ko.md)가 보안과 정보 노출 중심의 점검이었다면,
이번에는 **"네트워크나 의존 대상이 죽었을 때 서비스가 어떻게 동작하고 어떻게 돌아오는가"**라는
관점으로 점검했다.

모두 장애 신고가 있었던 것이 아니라 선제적 확인에서 나온 것이다.
아래는 전부 실제 코드와 `flyctl` 출력으로 확인한 것이며, 추측은 포함하지 않았다.

전제로, 이 구성은 **머신 1대 · shared-cpu-1x · 256MB**다
(`flyctl scale show -a rusui-dev`). SSE 브로커가 인메모리라 스케일아웃할 수 없는 이상
([ADR-001](../decisions/ADR-001-use-sse.ko.md)), **1대가 막히면 서비스 전체가 멈춘다**는
전제로 각 항목을 평가했다.

---

## 1. Gemini API 호출에 타임아웃이 없었다

### 현상

번역과 챗봇이 공유하는 헬퍼 `callGeminiForText`가 `http.Post`를 쓰고 있었다.

```go
resp, err := http.Post(geminiURL, "application/json", bytes.NewBuffer(reqBody))  // 수정 전
```

`http.Post`는 `http.DefaultClient`를 사용하고, **`Timeout`은 제로값 = 무제한**이다.
Gemini가 응답하지 않으면 그 요청은 고루틴과 fly.io 프록시 접속 슬롯을 잡은 채
영원히 해제되지 않는다.

호출처 중 하나인 `/api/public/ai-chat`은 **무인증 공개 엔드포인트**로,
`store_id`만 알면 누구나 호출할 수 있다. 머신 1대 구성에서는 이게 막히면 서비스 전체가 멈춘다.

참고로 같은 저장소 안에서도 Slack 알림 워커는
[`metrics/alert_worker.go`](../../metrics/alert_worker.go)에서
`&http.Client{Timeout: 5 * time.Second}`를 제대로 설정하고 있었다. 여기만 빠져 있었다.

### 해결책

전용 클라이언트를 두고, 호출처의 요청 컨텍스트를 연결한다.

```go
var geminiClient = &http.Client{Timeout: geminiTimeout}  // 15초

func callGeminiForText(ctx context.Context, prompt string) (string, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, geminiURL, bytes.NewReader(reqBody))
    ...
    resp, err := geminiClient.Do(req)
```

설계상의 판단이 세 가지 있다.

- **클라이언트는 패키지 레벨로 하나만 둔다.** 요청마다 생성하면 커넥션 풀이 공유되지 않아
  매번 TCP+TLS 핸드셰이크부터 다시 한다
- **`r.Context()`를 넘긴다.** 손님이 챗봇 화면을 닫는 시점에 Gemini 호출도 중단된다.
  타임아웃만으로는 "아무도 기다리지 않는 응답"을 15초간 계속 기다리게 된다
- **15초.** 두 호출 경로 모두 `ThinkingBudget: 0`(추론 비활성)이라 보통 수 초 내에 돌아온다.
  "느리지만 정상"을 잘라내지 않는 범위의 상한으로 골랐다

### 겸사겸사 고친 것: API 키가 쿼리 파라미터에 실려 있었다

```go
// 수정 전: ?key=<API키>를 URL에 포함
geminiURL := fmt.Sprintf("...:generateContent?key=%s", apiKey)
```

`net/http`의 통신 에러는 `*url.Error`로 **URL 전체를 메시지에 포함한다.**
즉 타임아웃을 넣으면, 타임아웃 한 번마다
`failed to reach AI service: Post "https://...?key=AIza..."` 형태로 API 키가 로그에 흘러나온다.
**타임아웃을 넣지 않았으면 일어나지 않았을 유출을, 넣으면서 만들 뻔했다.**

`x-goog-api-key` 헤더로 옮겨서 에러 메시지에 키가 실리는 경로 자체를 끊었다.

에러 응답 읽기도 `io.LimitReader`로 8KiB 제한을 걸었다. 원인 파악에는 그 정도면 충분하다.

---

## 2. fly.io에 헬스체크가 설정되어 있지 않았다

### 현상

```
$ flyctl status -a rusui-dev
 PROCESS │ ID             │ VERSION │ REGION │ STATE   │ ROLE │ CHECKS
 app     │ d8d4017a6e3278 │ 63      │ nrt    │ stopped │      │
                                                              ^^^^^^^ 비어 있음
```

[`handlers/health_handler.go`](../../handlers/health_handler.go)는 MongoDB Ping까지 확인하는
`/health`를 제공하고 있었는데, **fly.io 쪽이 그걸 보고 있지 않았다.**

그 결과 구멍이 두 개 있었다.

- DB에 연결되지 않은 머신에도 프록시가 계속 트래픽을 보낸다
- 배포 시 검증이 동작하지 않는다. 깨진 릴리스가 그대로 나간다

### 해결책

```toml
[[http_service.checks]]
  interval = '30s'
  timeout = '5s'
  grace_period = '40s'
  method = 'GET'
  path = '/health'
```

`grace_period`를 40초로 잡은 것은 기동 시퀀스
(Infisical 시크릿 취득 → MongoDB 연결, 실패 시 5초 간격 최대 5회 재시도)를
넘길 수 있는 길이가 필요하기 때문이다. 너무 짧으면 정상 기동 중인 머신을 계속 unhealthy로 판정한다.

머신 레벨 체크는 프록시 요청으로 집계되지 않으므로 `auto_stop_machines` 판정에는 영향이 없다.

---

## 3. MongoDB에 연결되지 않은 채 기동하고, 두 번 다시 복구되지 않았다

### 현상

`main.go`는 기동 시 연결 실패를 로그로 떨구고 끝이었다.

```go
if err := db.InitMongoDB(cfg.MongoDB.URI); err != nil {
    log.Printf("MongoDB初期化失敗: %v", err)   // 수정 전: 여기서 끝
}
```

이러면 두 가지가 일어난다.

**(a) nil 참조로 panic한다.** `db.GetCollection`은 미연결 시 nil을 반환하는데,
저장소 계층은 그걸 nil 체크 없이 그대로 쓴다.

```go
collection := db.GetCollection(DatabaseName, CollectionWaitingList)
cursor, err := collection.Find(ctx, filter)   // collection == nil → panic
```

해당 호출이 **109군데** 있었고, `db.MongoClient.StartSession()`을 직접 부르는 곳도 2군데 있었다.

`net/http`는 핸들러 내부의 panic을 recover하지만 **응답을 전혀 쓰지 않고 연결을 닫는다.**
손님 입장에서는 503조차 아니고 그냥 통신 에러다.

**(b) 복구되지 않는다.** 드라이버의 자동 재연결은 클라이언트가 생성된 이후의 이야기지,
`mongo.Connect` 자체가 실패하면 클라이언트는 nil로 남는다.
Atlas가 복구되어도 **재배포하기 전까지 서비스는 돌아오지 않는다.**

### 해결책

셋으로 나눠 대처했다.

**프로세스는 죽이지 않는다.** `log.Fatal`로 바꾸는 안은 채택하지 않았다. 머신 1대 구성에서는
Atlas가 돌아올 때까지 크래시 루프에 들어가고, 재시작할 때마다 기동 시퀀스(시크릿 취득 포함)를
처음부터 다시 한다. **503을 반환하며 기다렸다가 복구되는 순간 스스로 돌아오는 쪽이 복귀가 빠르다.**

**입구 한 곳에서 막는다.** `RequireDatabaseMiddleware`를 추가해 미연결 시
`/api` 하위를 503으로 반환한다. 109군데에 nil 체크를 넣는 것은 현실적이지 않다.

```go
if !db.IsReady() {
    w.Header().Set("Retry-After", "30")
    utils.RespondWithError(w, "Database is temporarily unavailable", http.StatusServiceUnavailable)
    return
}
```

배치에는 제약이 셋 있다.

- **인증 미들웨어보다 바깥.** 인증 자체가 사용자 조회로 DB를 읽기 때문에,
  안쪽에 두면 인증 시점에 panic한다
- **CORS 미들웨어보다 안쪽.** 바깥이면 503 응답에 CORS 헤더가 붙지 않아,
  브라우저에는 원래의 503이 아니라 원인 불명의 CORS 에러로 표시된다
- **라우터가 아니라 main.go의 핸들러 체인에 건다.** 라우터(`RegisterRoutes`)에 걸면
  DB가 없는 테스트 환경에서 전 라우트가 503이 되어, 인증·세션 보호를 검증하는
  [`router_session_test.go`](../../handlers/router_session_test.go)가 무의미해진다.
  실제로 한 번 그렇게 구현했다가 테스트가 깨져서 알게 됐다

**재연결 워커를 둔다.** `db.StartReconnectWatcher()`가 30초마다 연결을 계속 시도한다.

여기서 다루는 것은 **"한 번도 연결된 적 없는" 상태뿐**이다.
연결 확립 후의 일시적 단절은 드라이버가 토폴로지를 감시하며 스스로 복구하므로,
여기서 손대면 멀쩡한 커넥션 풀을 망가뜨리게 된다.

같은 이유로 `IsReady()`는 **소통 여부가 아니라 클라이언트의 존재**만 본다.
ping 실패로 false를 만들면, 드라이버가 자력으로 복구할 수 있는 일시적 단절마다
멀쩡한 요청까지 휩쓸려 503이 된다.
실제 소통 확인은 `/health`의 역할로 분리해 두었다.

**부수적으로**, 공개 변수 `db.MongoClient`를 비공개로 바꾸고 `db.Client()` 경유로 변경했다.
재연결 워커가 쓰고 전 핸들러가 읽게 되므로 그대로 두면 데이터 레이스가 된다.
`go test -race`로 확인했다.

### 검증 중에 발견: 메트릭 워커가 5초마다 panic하고 있었다

MongoDB에 도달할 수 없는 URI로 실제 기동해 확인했더니,
**미들웨어로는 막을 수 없는 경로**가 남아 있었다.

```
[safego] ゴルーチン metrics_error_batch がpanicしました: runtime error: invalid memory address or nil pointer dereference
	.../metrics/tracker.go:95
[safego] metrics_error_batch: 5s後に再開します
```

`ErrorTracker.flush`가 `db.GetCollection`의 반환값을 nil 체크 없이 `InsertMany`에 넘기고 있었다.
`utils.GoForever`가 복귀시켜 주므로 프로세스는 죽지 않지만,
**fly.io 로그에 5초마다 스택 트레이스가 쌓여 진짜 장애 원인이 묻힌다.**
이것은 HTTP 요청 경로가 아니므로 `RequireDatabaseMiddleware`로는 막을 수 없다.

`RequestTracker`와 `AuditTracker`에는 nil 체크가 있었지만
**버퍼를 비운 뒤**에 놓여 있었다. 즉 panic은 하지 않지만,
DB가 죽어 있는 동안의 로그는 복구되어도 돌아오지 않고 버려지고 있었다.

셋 다 `skipFlushWhileDBDown` 판정을 **버퍼를 꺼내기 전**에 두었다.
쌓아둔 채로 복구를 기다릴 수 있게 된다. 버퍼는 모두 1,000건에서 상한에 걸리므로
기다리는 동안 메모리를 잠식하는 일은 없다.

**첫 수정은 문제를 바꾸기만 했다.** 견송 사실을 매번 로그로 남기게 했더니
워커 3개 × 5초 주기로 분당 수십 행이 쌓였다. 스택 트레이스가 메시지로 바뀌었을 뿐,
진짜 원인이 묻히는 것은 똑같았다. 상태가 바뀔 때만 기록하도록 고쳤다.

```go
if !dbDownLogged.Swap(true) {
    log.Println("MongoDB未接続のため、メトリクスのバッチ保存を見送ります ...")
}
```

이 억제는 테스트로만 지켜진다(`metrics/db_down_test.go`). 풀리면 또 같은 상태로 돌아간다.

---

## 4. 핸들러의 panic이 500으로 기록되지 않았다

### 현상

[004](./004-production-readiness-hardening.ko.md)에서 백그라운드 고루틴에는 panic 보호를 넣었지만,
**핸들러 본체는 `net/http` 기본 recover에 맡긴 상태였다.**

기본 recover는 응답을 쓰지 않고 연결을 닫는다. 그래서:

- 클라이언트에서는 500이 아니라 통신 에러로 보여 원인 분리가 안 된다
- `MetricsMiddleware`가 상태 코드를 관측할 수 없어
  **에러 대시보드에 전혀 집계되지 않는다. 장애가 숫자로 나타나지 않는다**

### 해결책

`RecoverMiddleware`를 추가하고 `MetricsMiddleware`의 **안쪽**에 두었다.
여기서 쓴 500을 바깥의 메트릭스가 주워서 대시보드에 반영한다.

최종 체인은 다음과 같다.

```
레이트리밋 → 메트릭스 → panic 복구 → CORS → DB 준비 확인 → 라우터
```

구현상 주의점이 셋 있다.

- **응답이 시작된 뒤에는 덮어쓰지 않는다.** SSE는 헤더 송출과 Flush를 마친 뒤 계속 배신한다.
  거기에 500을 덮어도 응답은 바뀌지 않고 `superfluous response.WriteHeader` 로그만 남는다.
  헤더 송출 여부를 기록해 두고, 그 경우에는 응답을 끝내는 데 그친다(클라이언트는 재접속으로 복귀)
- **래퍼는 `http.Flusher`를 구현한다.** SSE 핸들러가 `w.(http.Flusher)`로 꺼내기 때문에,
  구현하지 않으면 SSE 접속이 확립되는 순간 panic한다.
  `Unwrap()`도 마련해 `http.ResponseController`에서 원래 기능에 닿을 수 있게 했다
- **`http.ErrAbortHandler`는 삼키지 않는다.** net/http 규약상 "의도적 중단" 신호이지 이상이 아니다.
  500으로 바꾸면 의도한 중단이 장애로 기록된다

스택 트레이스는 로그에만 남기고 응답 본문에는 포함하지 않는다. 내부 구조가 밖으로 샌다.

---

## 검증

`handlers/resilience_middleware_test.go`를 추가했다.
**장애 시의 동작은 평상시 테스트에서 전혀 밟히지 않으므로** 여기가 유일한 방파제가 된다.

- panic이 500이 되는 것 / 정상 응답에 간섭하지 않는 것
- 응답 시작 후의 panic이 이미 보낸 본문을 망가뜨리지 않는 것(Flusher 구현 확인 겸용)
- `ErrAbortHandler`가 그대로 통과하는 것
- DB 미연결 시 `/api`가 503 + `Retry-After` + `X-Error-Type: DATABASE_ERROR`를 반환하는 것
- `/health`와 CORS 프리플라이트가 대상 외인 것

추가로 MongoDB에 도달할 수 없는 URI를 주고 실제로 기동해 다음을 확인했다.

- 기동은 되지만 `/health`가 503을 반환
- `/api/*`가 panic 없이 503을 반환
- MongoDB가 복귀하면 재배포 없이 서비스가 돌아옴

---

## 남아 있는 과제

이번 점검에서는 다루지 않았지만, 같은 관점에서 확인된 항목을 기록해 둔다.

| 항목 | 내용 |
|---|---|
| Infisical 기동 시 의존 | [`Dockerfile`](../../Dockerfile)의 CMD가 머신 기동 때마다 `app.infisical.com`을 호출한다. `min_machines_running = 0`이라 이 일이 자주 일어난다. 게다가 `curl \| jq`는 curl의 실패를 삼킨다([004](./004-production-readiness-hardening.ko.md)와 같은 함정) |
| 멱등성에 유니크 제약이 없음 | `idx_store_waiting_id`가 non-unique라, [idempotency](../implementation/idempotency.ko.md)의 "조회 후 insert"가 동시 실행에서 중복 등록을 허용한다 |
| 요청 본문 크기 무제한 | `MaxBytesReader` 사용처가 0건. 256MB 머신에서 JSON을 무제한으로 디코딩하고 있다 |
| fly.io 프록시 동시 실행 수 미설정 | `[http_service.concurrency]`가 없다. SSE는 접속을 계속 유지하므로 이 값이 실질적인 동시 접속 상한이 된다 |
| SSE 초기 데이터가 Broadcast됨 | 한 명이 접속할 때마다 같은 매장의 전체 접속자에게 전건이 재전송된다 |
| 정상 종료가 에러로 기록됨 | `SSE_DISCONNECT`가 `error_logs`에 들어가서 에러율 기반 알림이 제 기능을 하기 어렵다 |
