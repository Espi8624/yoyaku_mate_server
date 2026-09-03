# 기능 명세서: 시프트표 (Shift Table)

본 문서는 `yoyaku_mate_server`에 구현된 매장 주 단위 시프트표(Shift Table) 기능 명세를 정의합니다.

> 작성일: 2026-09-01
> 관련 문서: [시프트표 구현 상세서](../implementation/shift-table.ko.md), [스태프 근무가능시간 기능 명세서](./staff-availability.ko.md)

---

## 1. 개요 (Overview)

매장 매니저가 주 단위로 각 스태프의 근무 시프트(요일·시간대·담당 스태프)를 관리할 수 있는 기능입니다. [스태프 근무가능시간](./staff-availability.ko.md) 기능에서 언급되었던 "시프트표 자동 생성"의 전 단계로, 우선 매니저가 수동으로 시프트를 짜는 기능을 구현했습니다.

시프트표는 주(월요일 시작) 단위로 독립된 문서로 관리되며, **매니저가 명시적으로 생성하지 않는 한 해당 주의 시프트표는 존재하지 않습니다**. 미생성 주를 조회하면 "미생성" 상태로 취급됩니다.

---

## 2. 주요 기능 (Key Features)

1. **주 단위 시프트표 생성 (매니저 전용)**:
   - 대상 주의 월요일 날짜를 지정해 빈 시프트표를 신규 생성합니다.
   - 이미 같은 주의 시프트표가 존재하면 생성할 수 없습니다(중복 생성 방지).
2. **시프트 추가/수정/삭제 (매니저 전용)**:
   - 생성된 시프트표에 스태프·요일·시작/종료 시각을 지정해 시프트를 1건 단위로 추가/수정/삭제합니다.
   - 시프트에 배정 가능한 스태프는 해당 매장의 승인된(`APPROVED`) 스태프로 제한됩니다.
3. **시프트표 조회 (매니저 / 승인된 스태프)**:
   - 매니저와 승인된 스태프는 주를 지정해 시프트표(등록된 시프트 목록)를 조회할 수 있습니다.
   - 조회는 스태프에게도 허용되지만, 생성·추가·수정·삭제는 매니저만 가능합니다.

---

## 3. 데이터 구조 (Shift Table Schema)

```json
{
  "_id": "66f1...",
  "store_id": "store_abc",
  "week_start_date": "2026-09-07",
  "shifts": [
    {
      "_id": "66f2...",
      "staff_id": "66e0...",
      "day": "monday",
      "start_time": "09:00",
      "end_time": "17:00"
    }
  ]
}
```

* `week_start_date`는 해당 주의 월요일을 나타내는 `YYYY-MM-DD` 형식 문자열입니다. 월요일이 아닌 날짜는 잘못된 값으로 거부됩니다.
* `day`는 [스태프 근무가능시간](./staff-availability.ko.md)과 동일한 요일 키 체계(`monday`~`sunday`)입니다.
* `start_time` / `end_time`은 `HH:MM`(24시간 표기) 형식이며, `start_time < end_time`이어야 합니다.

---

## 4. API 명세 (API Specification)

### `GET /api/stores/{storeId}/shift-tables/{weekStartDate}`

* **설명**: 지정 주의 시프트표를 조회합니다.
* **권한**: 매니저, 또는 승인된 스태프.
* **응답**: `200 OK` — 위 스키마 반환. `404 Not Found` — 해당 주 시프트표 미생성.

### `POST /api/stores/{storeId}/shift-tables`

* **설명**: 지정 주의 빈 시프트표를 신규 생성합니다.
* **권한**: 매니저만.
* **요청 본문**: `{ "week_start_date": "2026-09-07" }`
* **응답**: `201 Created`. `409 Conflict` — 이미 같은 주의 시프트표가 존재하는 경우.

### `POST /api/stores/{storeId}/shift-tables/{weekStartDate}/shifts`

* **설명**: 시프트를 1건 추가합니다.
* **권한**: 매니저만.
* **요청 본문**: `{ "staff_id": "...", "day": "monday", "start_time": "09:00", "end_time": "17:00" }`
* **응답**: `201 Created`. `404 Not Found` — 시프트표가 미생성인 경우(먼저 생성 필요).

### `PATCH /api/stores/{storeId}/shift-tables/{weekStartDate}/shifts/{shiftId}`

* **설명**: 기존 시프트를 1건 수정합니다.
* **권한**: 매니저만.
* **요청 본문**: `POST /shifts`와 동일.

### `DELETE /api/stores/{storeId}/shift-tables/{weekStartDate}/shifts/{shiftId}`

* **설명**: 기존 시프트를 1건 삭제합니다.
* **권한**: 매니저만.

---

## 5. 클라이언트 UI

`yoyaku_mate_provider`의 스태프 관리 화면은 2페이지 구성이며, 페이지1(스태프 관리)을 오른쪽으로 스와이프하면 페이지2(시프트표)로 전환됩니다. 시프트표 화면은 Outlook / Microsoft Teams의 주간 캘린더 화면을 참고한, 요일×시간 그리드 UI입니다. 자세한 내용은 [클라이언트 측 구현 상세서](../../../yoyaku_mate_provider/docs/implementation/shift-table.ko.md)를 참조하세요.

---

## 6. 향후 계획 (Roadmap)

현재는 수동 시프트 생성만 지원합니다. [스태프 근무가능시간](./staff-availability.ko.md)에서 축적한 데이터를 기반으로 시프트표를 자동 생성하는 기능은 아직 구현되지 않았습니다.
