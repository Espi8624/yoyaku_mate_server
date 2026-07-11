# 에러 대시보드 기능 (Error Dashboard)

> 최종 수정: 2026-07-11

## 개요

어플리케이션의 안정성과 유지보수성을 높이기 위해, 서버 내에서 발생하는 다양한 에러(HTTP 4xx/5xx, 데이터베이스 에러, 실시간 스트리밍 연결 유실 등)를 실시간으로 수집하고 어드민 대시보드에 시각화해 주는 기능입니다.  
메인 API의 처리 속도에 영향을 주지 않기 위해 **"인메모리 버퍼링 ➡️ 비동기 배치 일괄 쓰기"** 방식을 채택하고 있습니다.

---

## 주요 개념

| 개념 | 설명 |
|------|------|
| `ErrorCaptureMiddleware` | HTTP 요청을 가로채고, 4xx/5xx 응답 코드를 감지하는 공통 미들웨어 |
| `ErrorTracker` | 발생한 에러를 메모리 상에 임시 집계 및 보관하는 싱글톤 관리 클래스 |
| `logBuffer` | 에러 발생 시, 메인 쓰레드를 블로킹하지 않고 데이터를 임시 보관하는 인메모리 영역 (최대 1,000건) |
| `Batch Worker` | 5초 주기로 기동하며, 메모리 내 버퍼를 비우고 MongoDB로 일괄 저장(Bulk Insert)하는 백그라운드 프로세스 |
| `TTL Index` | 7일(604,800초)이 경과한 에러 로그 데이터를 MongoDB가 백그라운드에서 자동으로 자동 삭제하도록 관리하는 인덱스 |

---

## 수집 대상 에러 및 상태 정의

수집되는 에러는 아래의 4가지 카테고리로 분류되며, [ErrorCountPage.jsx](../../yoyaku_mate_admin/src/pages/ErrorCountPage.jsx)의 상단 카드에 실시간 개수가 집계됩니다.

| 에러 타입 (`error_type`) | 수집 시점 | 주요 수집 데이터 |
|---------------------------|------------|----------------|
| `500_INTERNAL_ERROR`      | API 핸들러 내부 처리 실패 및 예외, 런타임 패닉 발생 시 | 에러 메시지, API 경로, 메소드, 클라이언트 IP |
| `400_BAD_REQUEST`         | 클라이언트의 파라미터 유효성 검증 실패, 존재하지 않는 API 호출 시 | 에러 메시지, API 경로, 메소드, 클라이언트 IP |
| `DATABASE_ERROR`          | MongoDB 쿼리 실행 실패 및 네트워크 끊김 발생 시 | 데이터베이스 상세 에러 로그 |
| `SSE_DISCONNECT`          | 실시간 대기열 알림 스트림 연동 중, 클라이언트 연결이 강제 유실될 시 | 연결 유실이 발생한 API 경로, RemoteAddr |

---

## 데이터 흐름

```mermaid
sequenceDiagram
    autonumber
    actor Client as 사용자 / 관리자
    participant Router as Gorilla Mux / Middleware
    participant Tracker as ErrorTracker (In-Memory)
    participant Worker as Batch Worker (Goroutine)
    database DB as MongoDB Atlas

    Client->>Router: 1. API 요청 전송 (또는 연결 강제 종료)
    Router-->>Client: 2. API 처리 및 응답 반환 (400/500 에러 발생)
    
    note over Router, Tracker: 메인 쓰레드 성능 보장을 위해 비동기로 전송
    Router->>Tracker: 3. RecordError(models.ErrorLog) 호출
    Tracker->>Tracker: 4. 인메모리 logBuffer에 임시 저장 (최소한의 Lock 사용)

    loop 5초 주기 (Ticker)
        Worker->>Tracker: 5. 버퍼 데이터 추출 및 클리어
        Worker->>DB: 6. InsertMany() 호출로 일괄 비동기 저장 (Bulk Write)
    end

    Note over DB: 7. TTL 인덱스에 의해 7일 경과 후 자동 소멸
```

---

## 데이터베이스 설계

### 1. `error_logs` 컬렉션 구조 (BSON)

```json
{
  "_id": "ObjectId",
  "timestamp": "ISODate (UTC)",
  "error_type": "string (500_INTERNAL_ERROR / 400_BAD_REQUEST / DATABASE_ERROR / SSE_DISCONNECT)",
  "message": "string (에러 요약 메시지)",
  "path": "string (API 엔드포인트 경로)",
  "method": "string (GET / POST / PATCH / DELETE)",
  "client_ip": "string (IPv4 / IPv6 또는 프록시 헤더 최초 값)"
}
```

### 2. 성능 최적화용 인덱스 설정

읽기 및 쓰기 시의 데이터베이스 부하를 최소화하기 위해 백엔드 시작 시 자동으로 다음 인덱스들을 빌드합니다.

* **`idx_error_logs_ttl`**
  - 키: `{"timestamp": 1}`
  - 옵션: `ExpireAfterSeconds: 604800` (7일)
  - 효과: 용량 비대화 방지 및 장기 스토리지 추가 비용 차단
* **`idx_error_type`**
  - 키: `{"error_type": 1}`
  - 효과: 어드민 화면 로딩 시 카테고리별 카운팅 조회 속도를 극대화 (인덱스 온리 스캔 지원)

---

## 대시보드 연동 API

어드민 화면을 위해 아래 두 가지 분리된 REST API를 제공합니다.

1. **에러 통계 요약 API**
   - 경로: `/api/admin/metrics/errors`
   - 메소드: `GET`
   - 설명: 에러 타입별 누적 개수를 MongoDB CountDocuments 쿼리를 통해 초고속으로 조회하여 카드 영역에 반환합니다.
2. **에러 상세 로그 목록 API**
   - 경로: `/api/admin/metrics/error-logs`
   - 메소드: `GET`
   - 설명: 가장 최근에 발생한 상세 에러 로그 50건을 최신 정렬로 조회하여 하단 테이블 영역에 반환합니다.

---

## 기술 결정 (ADR)

본 기능의 구현에 있어, 실시간 대기열 관리(SSE 채택)와의 요구 성능 차이를 비교하고 폴링 방식을 채택하게 된 배경에 대해서는 아래의 기술 결정서(ADR)를 참조해 주세요.

* [ADR-002: 에러 대시보드 내 HTTP 폴링 방식 채택](../decisions/ADR-002-use-polling-for-error-dashboard.ko.md)


