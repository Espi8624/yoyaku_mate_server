# docs — yoyaku_mate_server 문서 인덱스

## 구조

```
docs/
├── features/           # 기능 사양 (무엇을 하는가)
├── implementation/     # 기술 구현 상세 (어떻게 구현했는가)
├── decisions/          # 기술 선택 근거 (왜 이걸 선택했는가)
├── troubles/           # 트러블슈팅 / 회고 기록
└── refactoring/        # 리팩토링 기록
```

---

## Features (기능 사양)

| 문서 | 설명 |
|------|------|
| [waiting-list.ko.md](./features/waiting-list.ko.md) | 대기열 등록·관리·실시간 알림 기능 |
| [error-dashboard.ko.md](./features/error-dashboard.ko.md) | 에러 메트릭·로그의 실시간 수집 및 모니터링 대시보드 |
| [request-counter.ko.md](./features/request-counter.ko.md) | 리퀘스트 메트릭·로그의 실시간 수집 및 모니터링 대시보드 |
| [active-user-dashboard.ko.md](./features/active-user-dashboard.ko.md) | 활성 사용자(동시 접속자/DAU/MAU) 수집 및 모니터링 대시보드 |

---

## Implementation (구현 상세)

| 문서 | 설명 |
|------|------|
| [architecture.ko.md](./implementation/architecture.ko.md) | 서버 전체 아키텍처 및 레이어 구조 |
| [background.ko.md](./implementation/background.ko.md) | 프로젝트 배경 및 설계 의도 |
| [sse.ko.md](./implementation/sse.ko.md) | SSE Broker 구현 (실시간 Push) |
| [idempotency.ko.md](./implementation/idempotency.ko.md) | 멱등성 처리 (중복 등록 방지) |
| [atomic-counter.ko.md](./implementation/atomic-counter.ko.md) | 대기 순번 Atomic 발급 로직 |
| [error-dashboard.ko.md](./implementation/error-dashboard.ko.md) | 에러 대시보드 백엔드 파이프라인 및 버퍼링 구현 상세 |
| [request-counter.ko.md](./implementation/request-counter.ko.md) | 리퀘스트 카운터 백엔드 파이프라인 및 버퍼링 구현 상세 |
| [active-user-tracking.ko.md](./implementation/active-user-tracking.ko.md) | 활성 사용자 트래킹 및 실시간/통계 수집 구현 상세 |

---

## Decisions (기술 결정)

| 문서 | 결정 내용 |
|------|----------|
| [ADR-001-use-sse.ko.md](./decisions/ADR-001-use-sse.ko.md) | WebSocket 대신 SSE를 선택한 이유 |
| [ADR-002-use-polling-for-error-dashboard.ko.md](./decisions/ADR-002-use-polling-for-error-dashboard.ko.md) | 에러 대시보드 내 HTTP 폴링 방식 채택 이유 |
| [ADR-003-request-counter-architecture.ko.md](./decisions/ADR-003-request-counter-architecture.ko.md) | 자체 메트릭 수집 및 리퀘스트 카운터 아키텍처 채택 이유 |
| [ADR-004-active-user-tracking.ko.md](./decisions/ADR-004-active-user-tracking.ko.md) | 인메모리 슬라이딩 윈도우 및 일별 활성 사용자 컬렉션을 활용한 접속자 트래킹 채택 이유 |

---

## Troubles (트러블슈팅 / 회고)

| 문서 | 설명 |
|------|------|
| [001-lessons-learned.ko.md](./troubles/001-lessons-learned.ko.md) | Goroutine 리크, Rate Limiter 조정 등 개발 과정 회고 |
| [002-active-user-ip-port-issue.ko.md](./troubles/002-active-user-ip-port-issue.ko.md) | 실시간 접속자의 에피메럴 포트 및 IPv6 중복 카운트 방지 해결 과정 |

---

## Refactoring (리팩토링)

*기록 예정*

