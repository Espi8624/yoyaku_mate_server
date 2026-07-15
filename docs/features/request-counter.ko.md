# 기능 사양서: 리퀘스트 대시보드 (Request Dashboard)

본 문서는 `yoyaku_mate_admin` 본사 관리자 웹에 구현된 실시간 API 트래픽 모니터링 시스템(리퀘스트 카운터)의 기능 명세를 설명합니다.

> 최종 수정: 2026-07-14  
> 관련 문서: [ADR-003: 자체 메트릭 수집 및 리퀘스트 카운터 아키텍처 채택](../decisions/ADR-003-request-counter-architecture.ko.md), [구현 상세서: 리퀘스트 카운터](../implementation/request-counter.ko.md)

---

## 1. 개요 (Overview)

플랫폼의 전체 API 트래픽 흐름과 정상 처리율을 본사 관리자 대시보드에서 실시간으로 관찰하고, 병목이 발생하는 API 엔드포인트를 식별하여 시스템의 전반적인 상태를 한눈에 파악하기 위한 모니터링 화면입니다.

---

## 2. 화면 구성 요소

화면 상단에는 주요 메트릭 카드 3개가 가로로 나열되며, 하단에는 개별 리퀘스트 로우 데이터 목록을 제공합니다. 5초 주기로 자동 갱신(HTTP Polling)됩니다.

### 2.1 상단 요약 카드 (Metrics Cards)
* **TOTAL REQUESTS (24H)**: 최근 24시간 동안 서버로 유입된 전체 HTTP API 요청 수입니다.
* **SUCCESS RATE**: 24시간 전체 요청 중 정상 응답(HTTP status code가 2xx 및 3xx대)을 돌려준 성공률(%) 지표입니다. 에러 응답(4xx/5xx)이 늘어날수록 실시간으로 성공률이 감소합니다.
* **PEAK TPS (1H)**: 최근 1시간 동안 가장 트래픽이 집중되었던 초당 최대 요청 처리량(Transactions Per Second)입니다.

### 2.2 실시간 API 리퀘스트 로그 테이블 (Latest Logs Table)
가장 최근에 발생한 50개의 API 요청 이력을 최신순으로 정렬하여 그리드로 노출합니다.
* **노출 열**: 일시(Timestamp), HTTP 메서드, API 경로, 상태 코드(Status Code), 응답 속도(Latency), 클라이언트 IP.
* **시각적 강조 기능**:
  * **오류 행 하이라이트**: 상태 코드가 400 대(Bad Request 등)인 행은 주황색 틴트, 500 대(Internal Error)인 행은 붉은색 틴트로 행 배경을 하이라이트 처리하여 장애 상황을 즉시 감지할 수 있도록 돕습니다.
  * **지연 시간 경고**: API 응답 지연 속도가 500ms 이상~1000ms 미만인 경우 주황색 볼드, 1000ms 이상인 경우 빨간색 볼드로 텍스트를 강조하여 응답 지연 구간을 추적합니다.

---

## 3. 향후 고도화 계획
* **차트 라이브러리 도입**: 추후 React 어드민(`rusui-admin`)에 `Recharts` 라이브러리를 추가 설치하여, 시간에 따른 TPS 트렌드 선 그래프(Line Chart) 및 상태 코드별 성공/실패 비율 도넛 차트(Donut Chart) 시각화 피쳐 개발 예정.

---

## 관련 문서
- [구현 상세서: 리퀘스트 카운터](../implementation/request-counter.ko.md)
- [ADR-003: 자체 메트릭 수집 및 리퀘스트 카운터 아키텍처 채택](../decisions/ADR-003-request-counter-architecture.ko.md)
