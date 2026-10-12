# 패키지 구성 정리 (utils 해체·도메인 계층 분리·DB 접근의 리포지토리 집약)

> 최종 업데이트: 2026-10-10
> 관련 파일: [`shift/`](../../shift/), [`storage/r2.go`](../../storage/r2.go), [`safego/safego.go`](../../safego/safego.go), [`auth/token.go`](../../auth/token.go), [`data/signup_repo.go`](../../data/signup_repo.go), [`data/admin_metrics_repo.go`](../../data/admin_metrics_repo.go), [`data/errors.go`](../../data/errors.go)
> 관련 문서: [001-di-repository-pattern](./001-di-repository-pattern.ko.md), [architecture](../implementation/architecture.ko.md), [shift-table](../implementation/shift-table.ko.md)

## 배경

디렉토리 구성을 최근 Go 프로젝트 관례와 비교해 다시 검토했다. 결론적으로 최상위의 기술 레이어별 구성(`handlers/` → `data/` → `models/`)은 1.7만 줄 규모 서비스로서 타당하므로 `cmd/` + `internal/`로 전면 이전하지 않는다. 다만 디렉토리 배치가 아니라 "어떤 코드가 어느 패키지에 들어가 있는가"에 다음 문제가 있었다.

| 문제 | 내용 |
|---|---|
| 도메인 로직이 핸들러에 있음 | `shift_table_handler.go`가 2094줄. 자동 배치 알고리즘·수정 의뢰 적용 판정 등 HTTP와 무관한 순수 계산이 대부분이었다 |
| 리포지토리를 거치지 않는 DB 접근 | `sign_up_handler.go`(14곳)·`metrics.go`(9곳)·`statistics_handler.go`(1곳)가 `db.GetCollection`을 직접 호출했다 |
| 인터페이스 정의 위치 혼재 | 대부분은 사용하는 쪽(handlers)에서 정의했지만 `data/`에도 5개가 있었고, 같은 이름으로 이중 정의된 것도 있었다 |
| `utils` 패키지 | safego·토큰·HTTP 응답 헬퍼·포인터 헬퍼처럼 성격이 다른 것들이 섞여 있었다 |
| R2 클라이언트의 위치와 이름 | Mongo 리포지토리인 `data/`에 `MinioClient`라는 이름으로, struct와 메서드가 다른 파일에 나뉘어 있었다 |

## 변경 내용

4단계로 나누어, 각 단계에서 동작을 바꾸지 않고(`go build` / `go vet` / `go test`가 통과하는 상태로) 진행했다.

### A. 인터페이스를 사용하는 쪽으로 통일

- `data/`의 `MenuRepository` / `SessionRepository` / `StaffRepository` / `ShiftTableRepository` / `ShiftChangeRequestRepository` 삭제
- `data.MenuRepository` 등을 직접 참조하던 핸들러는 실제로 쓰는 메서드만 가진 인터페이스를 handlers 쪽에 정의한다(`AIContextMenuRepository` 등 기존 명명 규칙을 따름)
- 구현체가 인터페이스를 만족하는지는 `main.go`에서 구체 타입을 넘기는 시점에 컴파일러가 검증한다

### B. `utils` 해체와 `storage` 분리

| 이동 전 | 이동 후 |
|---|---|
| `utils/safego.go` | `safego/` (`safego.Go` / `safego.GoForever`) |
| `utils/token_generator.go`, `GenerateRandomString` | `auth/token.go` |
| `RespondWithJSON` / `RespondWithError` / `RespondWithErrorCode` | `handlers/response.go` (핸들러 전용이라 비공개) |
| `FilterAllowedFields`, `Get{Int,Bool,String}PointerValue` | `handlers/request.go` (후자는 제네릭 `valueOr` 하나로 통합) |
| `data.MinioClient` (+ `data/minio.go`) | `storage/r2.go`의 `storage.Client` |

### C. 시프트표 도메인 로직을 `shift/`로 분리

| 파일 | 내용 |
|---|---|
| `shift/slot.go` | 입력값 검증, 시각 계산, 시프트 목록 내용 비교 |
| `shift/autoassign.go` | 자동 배치 (`BuildCandidates` / `AutoAssign` / `ComputeShortages`) |
| `shift/change_request.go` | 수정 의뢰 일괄 적용 판정 (`ApplyChangeRequests`), 스태프용 가림 처리 |

- 함수 본문은 이동만 했다(공개를 위한 이름 변경 제외 무변경). 응답 JSON 형식도 그대로
- `ApplyShiftChangeRequestsHandler` 안의 약 110줄짜리 적용 루프를 순수 함수 `ApplyChangeRequests`로 추출하고, 통상 적용과 3종류 충돌 테스트를 추가
- `data`와 `handlers`에 같은 포맷으로 이중 정의되어 있던 `pairKey`를 `shift.PairKey` 하나로 통합("바꿀 때는 양쪽 다 맞출 것"이라는 주석으로 동기화하고 있었다)
- 핸들러는 인증·권한·리포지토리 호출·응답만 남아 2094줄 → 1000줄

### D. 핸들러의 DB 직접 접근 제거

| 핸들러 | 이동 위치 |
|---|---|
| 회원가입·매장 추가·중복 체크 | `data/signup_repo.go` (`MongoSignUpRepo`), 핸들러는 `SignUpHandler` 구조체로 |
| 관리자 메트릭 | `data/admin_metrics_repo.go` (`MongoAdminMetricsRepo`), 핸들러는 `AdminMetricsHandler` 구조체로 |
| 매장 통계 | `MongoWaitingListRepo.AggregateStatistics` (`data/waiting_statistics_repo.go`) |

- 회원가입 시와 매장 추가 시에 중복되어 있던 "매장 + 영업허가증 + 초기 설정" 생성을 `insertStoreWithDefaults`로 통합
- 에러 판정을 에러 문자열 부분 일치에서 `data/errors.go`의 센티널 에러 + `errors.Is`로 변경
- `health_handler.go`의 ping과 `RequireDatabaseMiddleware`의 `db.IsReady()`는 인프라 계층의 접속 확인이므로 그대로 두었다

## 함께 고친 버그

D에서 에러 판정과 입력값 검증 위치를 재검토한 결과 다음을 수정했다. 원인은 모두 "입력값 검증을 트랜잭션 안에서 하고, 에러 문자열 부분 일치로 상태 코드를 정하던 것"이다.

| 증상 | 원인 | 수정 후 |
|---|---|---|
| 회원가입 시 매장 전화번호 형식이 틀리면 500 | 에러 문구가 소문자 `invalid ...`라서 대문자 `"Invalid"` 부분 일치에 걸리지 않았다 | 400 |
| 매장 주소(`store_address`) 없이 매장을 만들면 panic | `*req.StoreAddress`를 nil 체크 없이 참조했다 | 400 `store address is required` |
| 매니저가 아닌 사용자가 매장을 추가하면 500 | `only managers ...`가 어느 부분 일치에도 걸리지 않았다 | 403 |

입력값 검증은 DB에 닿기 전의 `validateNewStoreRequest`에서 하고, 상태 코드 변환은 `respondSignUpError`로 모았다. 둘 다 `handlers/sign_up_handler_test.go`로 고정했다.

## 채택하지 않은 것

- **`cmd/` + `internal/`로 이전**: 실행 파일이 서버 하나(+ 일회성 마이그레이션 스크립트)이고 라이브러리로 공개하지도 않으므로 이점이 적고 diff만 커진다
- **서비스 계층 일괄 도입**: 로직이 무거운 시프트표만 `shift/`로 분리하고, 얇은 핸들러에까지 계층을 늘리지 않는다
- **서비스와 배치 분리**: 서버 안의 백그라운드 처리(SSE heartbeat, 메트릭 flush, Mongo 재연결)는 프로세스 내부 상태에 의존하므로 분리 대상이 아니다. 정기 배치가 실제로 필요해지는 시점에 `cmd/server` + `cmd/<배치명>`으로 나눈다

## 남은 과제

- 기존 파일 중 약 20개가 `gofmt`에 걸린다(대부분 개조식 주석 들여쓰기). 기능과 무관하므로 별도 커밋으로 일괄 정리한다
- 리포지토리 구현체는 빈 struct인 채 전역 `db.GetCollection`에 의존한다. `*mongo.Database`를 필드로 받는 형태로 바꾸면 테스트에서 교체하기 쉬워지지만 이번 범위에서는 제외했다
