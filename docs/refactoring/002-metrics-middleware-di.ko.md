# MetricsMiddleware 의존성 주입(DI) 리팩토링

> 최종 업데이트: 2026-08-20
> 관련 파일: [`metrics/middleware.go`](../../metrics/middleware.go), [`metrics/middleware_test.go`](../../metrics/middleware_test.go), [`main.go`](../../main.go)
> 관련 문서: [001-di-repository-pattern](./001-di-repository-pattern.ko.md), [트러블슈팅: 002-active-user-ip-port-issue](../troubles/002-active-user-ip-port-issue.ko.md)

## 배경 및 문제점

`001-di-repository-pattern` 리팩토링에서 각 핸들러와 `RequireAuthMiddleware`는 인터페이스 주입 구조로 전환되었지만, 모든 API 요청을 가로채는 `MetricsMiddleware`는 대상에서 빠져 있었습니다.

```go
// AS-IS: 미들웨어 내부에서 트래커 싱글턴을 직접 호출
func MetricsMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // ...
        GetRequestTracker().RecordRequest(models.RequestLog{ /* ... */ })
        // ...
        GetTracker().RecordError(models.ErrorLog{ /* ... */ })
    })
}
```

이 구조에는 다음과 같은 문제가 있었습니다:
1. **테스트 불가능한 핵심 로직**: 실시간 접속자 중복 카운트 버그([트러블슈팅 002](../troubles/002-active-user-ip-port-issue.ko.md))가 발생했던 IP 추출·정규화 로직이 미들웨어 클로저 안에 갇혀 있어, `httptest`로 요청을 흘려보내도 실제 `RequestTracker`/`ErrorTracker` 싱글턴(고루틴, MongoDB 배치 저장 포함)까지 함께 기동되지 않으면 검증할 방법이 없었습니다.
2. **싱글턴 강결합**: `GetRequestTracker()` / `GetTracker()`를 코드 내부에서 직접 호출하고 있어 Mock으로 교체가 불가능했습니다.
3. **회귀 방지 공백**: 실제로 과거 장애를 유발한 로직임에도 불구하고 회귀 테스트를 걸 수 있는 구조가 아니었습니다.

## 해결책: 인터페이스 분리 + 순수 함수 추출

`001-di-repository-pattern`과 동일한 컨벤션(요청 시점에 의존 객체를 주입받는 팩토리 함수 패턴)을 미들웨어에도 적용했습니다.

### 1. IP 추출 로직을 순수 함수로 분리

```go
// - リクエストからクライアントの実IPアドレスを抽出する純粋関数
func extractClientIP(r *http.Request) string {
    clientIP := r.Header.Get("X-Forwarded-For")
    // ... X-Forwarded-For 파싱, RemoteAddr 포트 제거, IPv6 루프백 정규화
    return clientIP
}
```

### 2. 트래커를 인터페이스로 추상화

```go
// TO-BE: 트래커가 구현해야 할 최소 인터페이스만 정의
type RequestRecorder interface {
    RecordRequest(reqLog models.RequestLog)
}

type ErrorRecorder interface {
    RecordError(errLog models.ErrorLog)
}
```

`*RequestTracker`, `*ErrorTracker`는 이미 각각 `RecordRequest`, `RecordError` 메서드를 갖고 있었기 때문에 별도 어댑터 없이 그대로 이 인터페이스를 만족합니다.

### 3. 미들웨어를 팩토리 함수 패턴으로 전환

```go
// TO-BE: RequireAuthMiddleware와 동일한 팩토리 함수 패턴
func MetricsMiddleware(reqTracker RequestRecorder, errTracker ErrorRecorder) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // reqTracker.RecordRequest(...), errTracker.RecordError(...) 사용
        })
    }
}
```

### 4. `main.go`에서의 명시적 배선(Wiring)

```go
// AS-IS
handler := metrics.MetricsMiddleware(c.Handler(r))

// TO-BE
handler := metrics.MetricsMiddleware(metrics.GetRequestTracker(), metrics.GetTracker())(c.Handler(r))
```

운영 환경에서의 동작은 기존과 100% 동일합니다 (여전히 동일한 싱글턴을 사용). 다만 이제 그 의존성이 함수 시그니처에 명시적으로 드러나며, 테스트 시점에는 Mock으로 교체할 수 있습니다.

### 5. Mock을 활용한 단위 테스트

```go
type mockRequestRecorder struct {
    recordedLogs []models.RequestLog
}

func (m *mockRequestRecorder) RecordRequest(reqLog models.RequestLog) {
    m.recordedLogs = append(m.recordedLogs, reqLog)
}
```

`metrics/middleware_test.go`에 다음 케이스를 추가했습니다:
- `extractClientIP()` 단위 테스트 9종 (X-Forwarded-For 우선순위, 프록시 체인, IPv6 정규화 등)
- 임시 포트가 매번 달라져도 동일 IP로 정규화되는지 검증하는 회귀 테스트 (트러블슈팅 002 재현 시나리오)
- `MetricsMiddleware` 자체에 대한 Mock 기반 테스트: 정상 요청 기록, `/api/admin/metrics` 폴링 제외, 4xx/5xx 에러 분류(커스텀 `X-Error-Type` 헤더 포함)

## 검증

- `go build ./...`, `go vet ./...`, `go test ./...` 전부 통과
- `metrics` 패키지: 단위 테스트 15종 전부 통과 (기존 0개 → 15개)
- 나머지 패키지는 기존과 동일하게 `[no test files]` (이번 리팩토링 범위 밖)

## 기대 효과 (Benefits)

1. **회귀 방지**: 과거 실제 장애(3만 엔 상당은 아니지만 대시보드 신뢰도를 해친 중복 카운트 버그)를 유발했던 로직이 이제 CI 상에서 자동으로 검증됩니다.
2. **DB/고루틴 없는 테스트**: `RequestTracker`의 MongoDB 배치 저장 워커를 기동하지 않고도 미들웨어의 라우팅·기록 로직만 독립적으로 검증할 수 있습니다.
3. **컨벤션 일관성**: `001-di-repository-pattern`에서 확립한 "팩토리 함수로 의존성 주입" 패턴이 서버 전체 미들웨어에 일관되게 적용되었습니다.
