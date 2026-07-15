# 기능 사양서: SSE 상태 모니터링 (SSE Status Monitoring)

본 문서는 `yoyaku_mate_server` Go 백엔드 서버에서 제공하는 SSE 브로커 연결 현황 실시간 모니터링 기능의 명세를 설명합니다.

> 작성일: 2026-07-15  
> 관련 문서: [구현 상세서: SSE 상태 모니터링](../implementation/sse-monitoring.ko.md), [구현 상세서: SSE 실시간 연결 아키텍처](./sse.ko.md)

---

## 1. 개요 (Overview)

`yoyaku_mate_server`는 두 종류의 SSE 브로커를 운영합니다.

| 브로커 | 역할 | 구독 키 |
|--------|------|---------|
| `Broker` | 점포 전체 대기열 변경 사항 실시간 전달 (직원/점주 대상) | `storeID` |
| `WaitingUserBroker` | 개별 대기 고객 상태 변경 실시간 전달 (고객 대상) | `storeID:waitingID` |

SSE는 서버 → 클라이언트 단방향 연결로, 클라이언트의 연결 해제 신호를 서버가 즉각 감지할 수 없습니다. 이 특성 때문에 연결은 유지되어 보이지만 실제로는 끊어진 **좀비 연결(Zombie Connection)** 이 메모리에 누적될 수 있습니다.

본 기능은 두 브로커의 연결 현황을 실시간으로 집계하여 Admin 대시보드에 노출하고, **30초 주기 Heartbeat 고루틴**으로 좀비 연결을 자동 제거합니다.

---

## 2. 핵심 지표 (Key Metrics)

Admin 대시보드 `/sse-status` 페이지에서 제공되며, 5초 주기 HTTP 폴링으로 갱신됩니다.

### 2.1 요약 지표 카드 (Metrics Cards)

* **TOTAL CONNECTIONS (전체 활성 연결 수)**
  * **설명**: 두 브로커의 활성 채널 수를 합산한 현재 총 SSE 연결 수입니다.
  * **산출 기준**: `Broker.TotalConnections + WaitingUserBroker.TotalConnections`

* **CONNECTION HEALTH (연결 건강 상태)**
  * **설명**: 전체 연결 수 기준으로 판단한 브로커 동작 상태입니다.
  * **반영 기준**: `TotalConnections > 0` → `HEALTHY` / `0` → `IDLE`

* **STORE BROKER (점포 대기열 브로커)**
  * **설명**: 점포 대기열 SSE를 구독 중인 점포 수 및 연결 수, 평균 유지 시간입니다.
  * **반영 기준**: `Broker.GetStats()` 호출 결과 (인메모리 조회, DB 접근 없음)

* **USER BROKER (개별 대기 고객 브로커)**
  * **설명**: 개별 대기 고객 SSE를 구독 중인 사용자 키 수 및 연결 수, 평균 유지 시간입니다.
  * **반영 기준**: `WaitingUserBroker.GetStats()` 호출 결과 (인메모리 조회, DB 접근 없음)

---

## 3. Heartbeat — 좀비 연결 자동 제거

### 3.1 동작 방식

1. 서버 초기화 시 각 브로커 싱글톤에서 `startHeartbeat()` 고루틴이 자동 실행됩니다.
2. **30초 주기**로 `pingAndClean()`이 전체 채널에 SSE 주석 이벤트(`:ping`)를 논블로킹 전송(`select-default`)합니다.
3. 채널이 블로킹 상태(버퍼 포화 또는 수신 고루틴 종료) → 좀비 판단 → `close()` + 맵에서 즉시 제거합니다.
4. `:ping`은 SSE 스펙 주석 형식이므로 정상 클라이언트는 이를 이벤트로 수신하지 않습니다.

### 3.2 감지 가능한 케이스

* 클라이언트 앱 강제 종료 (defer 미실행)
* 네트워크 단절 후 재연결 없이 방치
* 앱 배경 전환으로 인한 연결 유지 중단

---

## 4. 향후 고도화 로드맵 (Roadmap)

### 4.1 2단계: 연결 이력 분석

* **피크 연결 수 기록**: 서버 실행 이후 최대 동시 연결 수를 인메모리로 기록하여 대시보드에 노출합니다.
* **채널 버퍼 크기 조정**: 좀비 감지 민감도를 높이기 위해 채널 버퍼 크기 설정을 별도 환경변수로 관리합니다.

### 4.2 3단계: 운영 고도화

* **강제 연결 해제 API**: Admin UI에서 특정 storeID 또는 waitingID 구독을 강제 종료하는 엔드포인트를 추가합니다.
* **Heartbeat 주기 Runtime 조정**: 환경변수 또는 Remote Config를 통해 Heartbeat 주기를 서버 재시작 없이 변경할 수 있도록 합니다.

---

## 관련 문서
- [구현 상세서: SSE 상태 모니터링 (서버)](../implementation/sse-monitoring.ko.md)
- [기능 사양서: SSE 실시간 연결 아키텍처](./sse.ko.md)
- [ADR-005: SSE 좀비 연결 감지 방식](../decisions/ADR-005-sse-zombie-detection.ko.md)
- [ADR-006: SSE 모니터링 대시보드의 통신 격리 및 HTTP 폴링 방식 채택](../decisions/ADR-006-sse-monitoring-polling.ko.md)
