# 서버 아키텍처 개요

> 최종 수정: 2026-07-10

## 기술 스택

| 항목 | 기술 |
|------|------|
| 언어 | Go |
| HTTP 라우터 | `gorilla/mux` |
| DB | MongoDB Atlas |
| 인증 | Firebase Auth |
| 파일 스토리지 | Cloudflare R2 (MinIO 호환 클라이언트) |
| 배포 | fly.io |
| Rate Limiting | `tollbooth` (5 req/s per IP, burst 10) |
| CORS | `rs/cors` |

---

## 디렉토리 구조

```
yoyaku_mate_server/
│
├── main.go              # 진입점: DB 초기화, 미들웨어, 서버 시작
│
├── handlers/            # HTTP 핸들러 (요청 파싱 / 응답 반환)
│   ├── router.go        # 라우터 등록
│   ├── waiting_list_handler.go
│   ├── sign_up_handler.go
│   ├── statistics_handler.go
│   ├── metrics.go       # 메트릭 대시보드 조회 API 핸들러 (에러, 리퀘스트, 동시접속, SSE 상태)
│   └── ...
│
├── data/                # 데이터 접근 계층 (MongoDB 쿼리)
│   ├── waiting_list.go
│   ├── counters.go      # Atomic Counter
│   ├── store_info.go
│   └── ...
│
├── models/              # Go 구조체 (DB 스키마 / JSON 직렬화)
│   ├── waiting_list_model.go
│   ├── sse_metrics.go   # SSE 브로커 메트릭 구조체
│   └── ...
│
├── events/              # SSE Broker (in-memory pub/sub)
│   ├── broker.go        # Store 단위 브로드캐스트 + Heartbeat 좀비 제거
│   └── waiting_user_broker.go  # 개별 손님 단위 + Heartbeat 좀비 제거
│
├── metrics/             # 메트릭 파이프라인 (인메모리 버퍼링 및 비동기 배치 워커)
│   ├── tracker.go       # 에러, API 리퀘스트, 활성 사용자 트래킹 인메모리 버퍼
│   └── middleware.go    # HTTP 트래픽 수집용 미들웨어
│
├── auth/                # 인증 관련 로직
│   ├── firebase_auth.go # Firebase ID Token 검증
│   └── login_token.go   # 중복 로그인 방지 토큰 검증
│
├── config/              # 환경 변수 로딩
├── db/                  # MongoDB 연결 관리
└── utils/               # 공통 유틸리티 (JSON 응답, HMAC 토큰 등)
```

---

## 레이어 구조 및 런타임 흐름

```
       [ Client HTTP Request / SSE Connection ]
                          │
                          ▼
             [ Metrics Middleware ] ──(비동기 로깅)──► [ RequestTracker ]
                          │                                │ (5초 배치)
                          ▼                                ▼
              [ Firebase Auth Middleware ]           [ MongoDB Atlas ]
                          │
                          ▼
                   [ Route Handlers ]
                    /            \
                   /              \
  (REST API 호출) ▼                ▼ (SSE 연결 유지)
    [ data ] MongoDB 쿼리       [ events ] SSE Brokers
           │                       │ (30초 Heartbeat)
           ▼                       ▼
    [ MongoDB Atlas ]       [ Client Push Messages ]
```

---

## 인증 흐름

```
Request
    │
    ├── Authorization 헤더 없음 → 공개 엔드포인트 (손님 QR 등록 등)
    │
    └── Authorization: Bearer <id_token>
            │
            ▼
        auth.VerifyIDToken()  →  Firebase UID 추출
            │
            ▼
        data.GetUserByFirebaseUID()  →  내부 User 객체
            │
            ▼
        auth.VerifyLoginToken()  →  X-Login-Token 헤더 검증
            │                        (중복 로그인 방지)
            ▼
        data.CheckUserStorePermission()  →  점포 권한 확인
```

---

## 주요 API 그룹

| 경로 | 설명 |
|------|------|
| `/api/waiting-list` | 대기열 CRUD + SSE 스트림 |
| `/api/menu-list`, `/api/provider_menu` | 메뉴 관리 |
| `/api/provider_user`, `/api/provider_store` | 유저/점포 정보 |
| `/api/auth/signup`, `/api/stores/add` | 회원가입 / 점포 등록 |
| `/api/admin/*` | 관리자 전용 (점포 승인 등) |
| `/api/admin/metrics/errors` | 에러 메트릭 요약 및 최근 상세 로그 목록 조회 |
| `/api/admin/metrics/requests` | API 리퀘스트 통계 및 상세 로그 목록 조회 |
| `/api/admin/metrics/active-users` | 실시간 동시 접속자 수 및 DAU/MAU 요약 메트릭 조회 |
| `/api/admin/metrics/sse-status` | SSE 브로커 연결 현황 및 평균 연결 시간 조회 (인메모리) |
| `/api/admin/metrics/audit-logs` | 관리자 작업 감사 로그 목록 조회 |
| `/api/stores/{storeId}/staff` | 스태프 관리 |
| `/api/statistics` | 대기 통계 |
| `/api/public/ai-chat` | AI 채팅 (공개) |

---

## 관련 문서

- [대기열 기능 사양](../features/waiting-list.ko.md)
- [에러 대시보드 구현 상세](./error-dashboard.ko.md)
- [리퀘스트 카운터 구현 상세](./request-counter.ko.md)
- [활성 사용자 트래킹 구현 상세](./active-user-tracking.ko.md)
- [SSE 상태 모니터링 구현 상세](./sse-monitoring.ko.md)
- [감사 로그 구현 상세](./audit-log.ko.md)
- [SSE 구현 상세](./sse.ko.md)
- [멱등성 구현 상세](./idempotency.ko.md)
- [Atomic Counter 발급 상세](./atomic-counter.ko.md)
- [ADR-001: WebSocket 대신 SSE 선택 이유](../decisions/ADR-001-use-sse.ko.md)
- [ADR-002: 에러 대시보드 내 HTTP 폴링 방식 채택](../decisions/ADR-002-use-polling-for-error-dashboard.ko.md)
- [ADR-003: 자체 메트릭 수집 및 리퀘스트 카운터 아키텍처 채택](../decisions/ADR-003-request-counter-architecture.ko.md)
- [ADR-004: 인메모리 슬라이딩 윈도우 및 일별 활성 사용자 컬렉션을 활용한 접속자 트래킹 채택](../decisions/ADR-004-active-user-tracking.ko.md)
- [ADR-005: SSE 좀비 연결 감지 방식 채택](../decisions/ADR-005-sse-zombie-detection.ko.md)
- [ADR-006: SSE 모니터링 대시보드의 통신 격리 및 HTTP 폴링 방식 채택](../decisions/ADR-006-sse-monitoring-polling.ko.md)
