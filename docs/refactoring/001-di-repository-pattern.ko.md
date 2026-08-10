# 핸들러 및 미들웨어 의존성 주입(DI) 리팩토링

> 최종 업데이트: 2026-08-10

## 배경 및 문제점

초기 개발 단계에서는 빠른 구현을 우선시했기 때문에, HTTP 핸들러 내부에서 데이터베이스 Repository 인스턴스를 직접 생성하여 메서드를 호출하는 방식을 취했습니다.

```go
// AS-IS: 핸들러 내부에서 암시적 의존성 생성
func StoreInfoHandler(w http.ResponseWriter, r *http.Request) {
    // 전역에 가깝게 직접 인스턴스화
    store, err := (&data.MongoStoreRepo{}).GetStoreData(storeID)
    // ...
}
```

이 접근 방식은 다음과 같은 문제를 야기했습니다:
1. **전역 상태 의존**: 핸들러가 구체적인 DB 구현체(MongoDB)와 강하게 결합되어 있었습니다.
2. **테스트의 어려움**: `MongoStoreRepo`를 모의 객체(Mock)로 대체할 수 없어, DB 연결 없이는 핸들러의 유닛 테스트가 불가능했습니다.
3. **코드의 복잡성**: 미들웨어(`RequireAuthMiddleware` 등) 내부에서도 마찬가지로 직접 인스턴스화를 진행하여 아키텍처 경계가 모호해졌습니다.

## 해결책: 의존성 주입(Dependency Injection)과 리포지토리 패턴 적용

모든 핸들러와 주요 미들웨어를 리팩토링하여 **외부(`main.go`)에서 의존 객체를 주입(Dependency Injection)**하는 구조로 변경했습니다.

### 1. 인터페이스 정의 및 구조체 전환
각 핸들러 파일 내에 필요한 메서드만 정의한 인터페이스를 작성하고, 핸들러를 구조체(Struct)로 정의했습니다.

```go
// TO-BE: 인터페이스를 통한 의존성 주입
type AIContextStoreRepository interface {
    GetStoreData(storeID string) (*models.Store, error)
    GetSettings(storeID string) (*models.StoreSetting, error)
}

type StoreAIContextHandler struct {
    storeRepo AIContextStoreRepository
}

func NewStoreAIContextHandler(repo AIContextStoreRepository) *StoreAIContextHandler {
    return &StoreAIContextHandler{storeRepo: repo}
}
```

### 2. 미들웨어의 팩토리 함수화
`RequireAuthMiddleware`와 같은 인증 미들웨어도 실행 시점에 의존 객체를 받을 수 있도록 클로저(Closure)를 활용한 팩토리 함수 패턴으로 변경했습니다.

```go
// TO-BE: 리포지토리를 주입받는 미들웨어
func RequireAuthMiddleware(repo MiddlewareUserRepository) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // repo.GetByFirebaseUID() 사용
        })
    }
}
```

### 3. `main.go`에서의 중앙 집중적 연결(Wiring)
모든 Repository(MongoDB 구현체)는 `main.go` 시작 시 단 한 번만 인스턴스화되며, 각 핸들러로 매개변수를 통해 전달됩니다. 이후 핸들러는 `router.go`에 등록됩니다.

## 기대 효과 (Benefits)

1. **완벽한 관심사의 분리(Separation of Concerns)**: HTTP 레이어는 HTTP 파싱과 응답만 담당하며, 데이터 접근은 주입된 인터페이스에 위임됩니다.
2. **테스트 용이성 향상(High Testability)**: 데이터베이스 연결이 필요 없는 순수한 HTTP 핸들러의 유닛 테스트가 가능해졌습니다.
3. **높은 확장성**: 향후 MongoDB에서 PostgreSQL 등으로 마이그레이션하더라도 `handlers` 패키지의 코드는 한 줄도 수정할 필요 없이 `data` 패키지의 구현체만 교체하면 됩니다.
