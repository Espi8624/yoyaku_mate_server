# 기능 사양서: 스태프 근무 가능 시간 (Staff Availability)

본 문서는 `yoyaku_mate_server`에 구현된 스태프 요일별 근무 가능 시간대(Staff Availability) 기능 사양을 정의합니다.

> 작성일: 2026-08-30  
> 관련 문서: [스태프 근무 가능 시간 구현 상세서](../implementation/staff-availability.ko.md)

---

## 1. 개요 (Overview)

향후 "버튼 클릭 한 번으로 시프트표 자동 생성" 기능의 기반 데이터로, 매장 스태프 본인이 요일별로 근무 가능한 시간대(오전·오후·야간)를 입력하고, 매장 매니저가 이를 조회·수정할 수 있는 기능입니다.

각 요일은 시간대 리스트로 표현되며, 리스트가 비어 있는 요일은 "근무 불가"를 의미합니다. 별도의 불가 플래그는 두지 않습니다.

---

## 2. 주요 기능 (Key Features)

1. **스태프 본인에 의한 입력/조회**:
   - 승인(`APPROVED`) 상태의 스태프 본인이 자신의 근무 가능한 요일·시간대를 등록·변경할 수 있습니다.
2. **매니저에 의한 조회/수정**:
   - 매장 매니저는 스태프 관리 화면에서 각 스태프의 근무 가능일을 요일 단위로 조회·수정할 수 있습니다.
3. **시간대 단위**:
   - `MORNING`(오전) / `AFTERNOON`(오후) / `EVENING`(야간) 3구분 중, 요일별로 복수 선택이 가능합니다.

---

## 3. 데이터 구조 (Availability Schema)

```json
{
  "monday": ["MORNING"],
  "tuesday": [],
  "wednesday": ["AFTERNOON", "EVENING"],
  "thursday": [],
  "friday": ["MORNING", "AFTERNOON", "EVENING"],
  "saturday": [],
  "sunday": []
}
```

* 각 요일 필드의 값은 `MORNING` / `AFTERNOON` / `EVENING` 중 하나로 이루어진 문자열 리스트입니다.
* 리스트가 비어 있거나 필드 자체가 생략된 요일은 "근무 불가"로 처리됩니다.

---

## 4. API 사양 (API Specification)

### `GET /api/stores/{storeId}/staff/me/availability`

* **설명**: 로그인 중인 스태프 본인의 근무 가능한 요일·시간대를 조회합니다.
* **권한**: 요청 사용자가 해당 매장의 승인(`APPROVED`) 스태프여야 합니다.
* **응답**: `200 OK` — 위의 Availability 스키마를 그대로 반환.

### `PATCH /api/stores/{storeId}/staff/me/availability`

* **설명**: 로그인 중인 스태프 본인의 근무 가능한 요일·시간대를 수정합니다.
* **권한**: `GET`과 동일.
* **요청 본문**:
```json
{ "availability": { "monday": ["MORNING"], "tuesday": [] } }
```
* **응답**: `200 OK`

### `PATCH /api/stores/{storeId}/staff/{staffId}/availability`

* **설명**: 매니저가 특정 스태프의 근무 가능한 요일·시간대를 수정합니다.
* **권한**: 요청 사용자가 해당 매장의 매니저(`manager`)여야 합니다.
* **요청 본문**: 위와 동일한 `availability` 객체.
* **응답**: `200 OK`

### `GET /api/stores/{storeId}/staff` (기존 API)

* 기존 스태프 목록 조회 API 응답에 `availability` 필드가 추가되어, 매니저가 목록 화면에서 각 스태프의 근무 가능일을 한 번에 확인할 수 있습니다.

---

## 5. 향후 계획 (Roadmap)

본 기능으로 축적한 근무 가능 시간 데이터를 입력값으로 하여, "버튼 클릭 한 번으로 시프트표를 자동 생성"하는 기능을 별도로 구현할 예정입니다 (미구현).
