# 기능 사양서: 활성 사용자 대시보드 (Active User Dashboard)

본 문서는 `yoyaku_mate_admin` 본사 관리자 웹에 구현된 실시간 활성 사용자 모니터링 시스템(Active User Dashboard)의 기능 명세를 설명합니다.

> 최종 수정: 2026-07-14  
> 관련 문서: [리퀘스트 대시보드 사양서](./request-counter.ko.md), [ADR-004: 활성 사용자 트래킹 의사결정 문서](../decisions/ADR-004-active-user-tracking.ko.md), [구현 상세서: 활성 사용자 트래킹](../implementation/active-user-tracking.ko.md)

---

## 1. 개요 (Overview)

플랫폼의 안정성과 사용자 반응을 즉각적으로 파악하기 위해, 실시간 동시 접속자 수 및 활성 사용자 트렌드(DAU, MAU)를 모니터링하는 백오피스 대시보드 화면입니다.

---

## 2. 화면 구성 및 핵심 지표 (UI Metrics)

관리자 대시보드 상단 요약 카드 형태로 노출되며, 5초 주기로 자동 갱신(HTTP Polling)됩니다.

### 2.1 상단 요약 카드 (Metrics Cards)

* **CURRENT ACTIVE USERS (실시간 동시 접속자)**
  * **설명**: 현재 플랫폼을 실제로 조작하며 API 요청을 지속해서 발생시키고 있는 유저 수입니다.
  * **반영 기준**: 최근 5분 이내에 최소 1회 이상 API 요청을 보낸 고유 기기(IP) 수입니다.
* **DAILY ACTIVE USERS (DAU - 오늘 접속자 수)**
  * **설명**: 오늘 하루 동안 서비스를 이용한 누적 고유 사용자 수입니다.
  * **반영 기준**: 금일(00:00 ~ 현재)까지 1회 이상 접속 이력이 기록된 중복 없는 IP 수입니다.
* **MONTHLY ACTIVE USERS (MAU - 지난 30일간 접속자 수)**
  * **설명**: 지난 30일 동안 서비스를 이용한 누적 고유 사용자 수입니다.
  * **반영 기준**: 최근 30일 동안 1회 이상 접속 이력이 기록된 중복 없는 IP 수입니다.

---

## 3. 향후 고도화 로드맵 (Roadmap)

핵심 지표가 안정화된 이후 비즈니스 분석 및 인프라 대응 능력을 극대화하기 위해 단계적으로 고도화를 진행합니다.

### 3.1 2단계: 비즈니스 고도화 지표
* **DAU/MAU 비율 (서비스 점착도 - Stickiness)**
  * 월간 사용자 중 매일 방문하는 유저의 비율을 계산하여 유저 리텐션 및 충성도 변화를 분석합니다.
* **전일 대비 증감률 (Growth Indicator)**
  * 오늘 접속자 수 하단에 전일 동시간대 대비 트래픽 증감 추이를 퍼센트(%)로 표시하여 성장을 직관적으로 트래킹합니다.

### 3.2 3단계: 인프라 및 플랫폼 분석
* **오늘 피크 타임 및 최대 동시 접속자 수 (Peak Concurrent Users & Time)**
  * 오늘 가장 트래픽이 몰렸던 피크 시간과 최대 동시 접속자 수를 보여줌으로써 서버 스케일링 계획 수립을 돕습니다.
* **기기 및 플랫폼별 접속 비율 (Web vs App)**
  * 활성 사용자의 플랫폼 분포(Web / Android App / iOS App)를 도넛 차트 형태로 시각화하여 프론트엔드 최적화 우선순위에 활용합니다.

---

## 관련 문서
- [구현 상세서: 활성 사용자 트래킹](../implementation/active-user-tracking.ko.md)
- [ADR-004: 인메모리 슬라이딩 윈도우 및 일별 활성 사용자 컬렉션을 활용한 접속자 트래킹](../decisions/ADR-004-active-user-tracking.ko.md)
- [트러블슈팅: 002-active-user-ip-port-issue](../troubles/002-active-user-ip-port-issue.ko.md)

