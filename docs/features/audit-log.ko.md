# 기능 사양서: 감사 로그 (Audit Log)

본 문서는 `yoyaku_mate_server`에 구현된 관리자 행위 감사 로그 (Audit Log) 기능 사양을 정의합니다.

> 작성일: 2026-07-22  
> 관련 문서: [감사 로그 구현 상세서](../implementation/audit-log.ko.md)

---

## 1. 개요 (Overview)

플랫폼의 안전한 운영과 시스템 변경 이력의 추적성을 확보하기 위해, 관리자가 수행한 주요 데이터 변경 행위(가게 승인/반려 등)를 실시간으로 자동 기록 및 관리합니다.

---

## 2. 주요 기능 (Key Features)

1. **관리자 행위 자동 기록**:
   - `adminApi` 서브라우터를 통해 실행되는 주요 상태 변경 액션을 자동 캡처하여 데이터베이스에 저장합니다.
2. **자동 보관 기간 관리 (90일 TTL)**:
   - MongoDB의 TTL (Time-To-Live) 인덱스를 활용하여 90일이 지나 지난 감사 로그를 자동 삭제함으로써 디스크 용량을 최적화합니다.
3. **최신순 목록 조회 API**:
   - `GET /api/admin/metrics/audit-logs` 엔드포인트를 통해 최신 작업 이력 최대 100건을 조회할 수 있습니다.

---

## 3. 기록 대상 액션 (Audit Actions)

| 액션명 | 발생 조건 | 기록 내용 (Target / Details) |
|---|---|---|
| `STORE_APPROVED` | 입점 신청 승인 처리 시 | 대상 가게 ID, 승인 코멘트 (선택) |
| `STORE_REJECTED` | 입점 신청 반려 처리 시 | 대상 가게 ID, 반려 사유 코멘트 |
| `STORE_PENDING_REVIEW` | 심사 보류 상태 변경 시 | 대상 가게 ID, 보류 사유 코멘트 |

---

## 4. API 명세 (API Specification)

### `GET /api/admin/metrics/audit-logs`

* **설명**: 최근 감사 로그를 최신순으로 최대 100건 조회합니다.
* **응답**: `200 OK`
```json
[
  {
    "id": "669fc25a8123456789abcdef",
    "timestamp": "2026-07-22T14:30:00Z",
    "action": "STORE_APPROVED",
    "target": "Store ID: 694e6e05ca47b4563c73617d",
    "status": "SUCCESS",
    "details": "서류 확인 완료"
  }
]
```
