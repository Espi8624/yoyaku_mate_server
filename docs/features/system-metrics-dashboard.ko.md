# 기능 사양서: 시스템 메트릭스 대시보드 (System Metrics Dashboard)

본 문서는 관리자용 대시보드의 **시스템 메트릭스 (System Metrics)** 기능에 대한 요구사항 및 사양을 정의합니다.

> 작성일: 2026-07-23  
> 대상: `yoyaku_mate_admin` (React UI), `yoyaku_mate_server` (Go Backend)

---

## 1. 개요 (Overview)
관리자가 서버(fly.io 인스턴스 또는 호스팅 환경)의 하드웨어 리소스 상태를 직관적이고 실시간으로 파악하여 장애를 예방하고 서버 스케일링(Scale-up/out) 시점을 결정할 수 있도록 돕는 대시보드 화면입니다.

## 2. 주요 기능 및 요구사항 (Key Features & Requirements)

### 2.1 하드웨어 리소스 실시간(Real-time) 조회
- **조회 항목**:
  1. **CPU Usage (CPU 사용량)**: 서버의 전체 CPU 사용률 (%)
  2. **Memory Usage (메모리 사용량)**: 서버의 전체 가상 메모리 사용률 (%)
  3. **Disk Space (디스크 사용량)**: 루트(`/`) 파티션의 디스크 사용률 (%)
- **데이터 성격**: 과거 데이터 누적이 아닌 API 호출 시점의 **현재 운영체제(OS)** 상태 값 반환.

### 2.2 폴링 방식의 5초 주기 자동 갱신
- 웹 소켓(WebSocket)이나 SSE 대신 구현이 단순하고 서버 부하가 적은 **5초(5000ms) 주기 폴링(Polling)** 방식을 채택합니다.
- 관리자가 대시보드 화면에 머무는 동안에만 백그라운드에서 데이터를 갱신합니다.

### 2.3 직관적인 UI 제공
- **Progress Bar**: 수치뿐만 아니라 선형 프로그레스 바(Linear Progress)를 통해 직관적으로 사용량을 색상별로 구분하여 제공합니다. (예: CPU - Primary, Memory - Info, Disk - Warning 색상 사용)
- **에러 핸들링**: 통신 장애 시 화면 레이아웃이 무너지지 않고, 자연스럽게 오류 메시지를 출력합니다.

---

## 3. 관련 문서
- [시스템 메트릭스 대시보드 구현 상세서](../implementation/system-metrics-dashboard.ko.md)
