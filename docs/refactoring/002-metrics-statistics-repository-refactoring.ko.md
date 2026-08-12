# 어드민 매트릭스 및 통계 대시보드 리팩토링 (DI 및 Repository 패턴 적용)

> 최종 업데이트: 2026-08-12

## 배경 및 문제점

1차 DI 리팩토링 이후에도 `handlers/metrics.go`와 `handlers/statistics_handler.go` 등 일부 복잡한 핸들러 내부에서는 여전히 `db.GetCollection()`이나 `db.MongoClient` 같은 전역 변수를 직접 참조하여 MongoDB 파이프라인을 실행하고 있었습니다.

```go
// AS-IS: 핸들러 내부에서 전역 DB 객체를 직접 참조
func (h *StatisticsHandler) CalculateStatistics(...) {
    collection := db.GetCollection(db.DatabaseName, db.CollectionWaitingList)
    // 수백 줄에 달하는 $facet 파이프라인 구성 및 실행 로직...
}
```

이 방식은 다음과 같은 문제점을 안고 있었습니다:
1. **비즈니스 로직과 데이터 접근 로직의 혼재**: 핸들러 파일 하나가 수백 줄의 MongoDB 쿼리를 포함하여 가독성이 현저히 떨어졌습니다.
2. **글로벌 의존성 잔재**: 여전히 전역 DB 커넥션에 강하게 의존하고 있어 유닛 테스트 작성을 방해했습니다.

## 해결책: 데이터 레이어 분리 및 Aggregation 로직 이관

글로벌 상태를 참조하는 부분을 모두 제거하고, 클린 아키텍처 원칙에 따라 데이터베이스에 직접 접근하는 로직을 `data/` 레이어(Repository)로 완전히 이관했습니다. (*참고: 회원가입(`sign_up_handler.go`) 기능은 구조적 재설계가 필요하여 이번 페이즈에서 제외되었습니다.)

### 1. `AdminMetrics` 리팩토링
- **Repository 신설**: `data/metrics_repo.go`를 생성하여 MongoDB를 조회하는 로직(`GetErrorMetrics`, `GetDAUMAU` 등)을 캡슐화했습니다.
- **Handler 의존성 주입**: 개별 함수로 분리되어 있던 로직들을 `AdminMetricsHandler` 구조체로 통합하고, `AdminMetricsRepository` 인터페이스를 주입받아 사용하도록 개선했습니다. (CPU/메모리 등 OS 레벨 메트릭은 DB 접근이 불필요하므로 핸들러에 유지)

### 2. `Statistics` 통계 파이프라인 분리
- **복잡한 쿼리 이관**: 기존 핸들러 내부에 하드코딩 되어 있던 거대한 `$facet` MongoDB 집계(Aggregation) 파이프라인을 `data/waiting_list_repo.go`의 `GetStatisticsAggregation` 메서드로 이동시켰습니다.
- **인터페이스 분리**: 핸들러는 `StatsWaitingListRepository` 인터페이스를 통해 파이프라인의 실행 결과(bson.M)만 전달받고, JSON 응답으로의 매핑에만 집중하게 되었습니다.

### 3. `main.go` 통합 연결
```go
// TO-BE: main.go에서 구체적인 Repository 생성 후 주입
metricsRepo := &data.MongoMetricsRepo{}
adminMetricsHandler := handlers.NewAdminMetricsHandler(metricsRepo)

waitingRepo := &data.MongoWaitingListRepo{}
statisticsHandler := handlers.NewStatisticsHandler(userRepo, storeRepo, waitingRepo)
```

## 기대 효과 (Benefits)

1. **가독성 및 유지보수성 극대화**: 핸들러 코드에서 복잡한 BSON 파이프라인 쿼리가 사라짐으로써 코드가 훨씬 깔끔해졌고, 비즈니스 흐름을 파악하기 쉬워졌습니다.
2. **테스트 커버리지 향상 기반 마련**: DB를 모킹(Mocking)할 수 있는 완벽한 환경이 갖춰져, 백엔드의 모든 핵심 조회 로직을 단위 테스트할 수 있게 되었습니다.
3. **아키텍처 일관성**: 제외된 회원가입 기능을 뺀 서버 내 거의 모든 로직이 동일한 레이어드(Layered) 아키텍처 규칙을 따르게 되었습니다.
