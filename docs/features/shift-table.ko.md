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
4. **초안과 확정의 분리 (매니저 전용)**:
   - 매니저의 편집·자동배치·수정의뢰 적용은 모두 **초안**에만 반영됩니다.
   - 「확정」을 누른 시점에 초안이 확정본으로 복사되고, 그때 비로소 스태프에게 공개됩니다.

---

## 3. 초안과 확정 (Draft / Publish)

시프트표는 「매니저가 편집 중인 초안」과 「스태프에게 공개된 확정본」 두 벌로 보관합니다. 작업 중인 시프트표가 스태프에게 보이면, 확정되지 않은 예정으로 출근일을 오인할 수 있기 때문입니다.

| 조작 | 반영 대상 | 스태프에게 보이는가 |
|---|---|---|
| 시프트 추가/수정/삭제 | 초안 | 안 보임 |
| 자동배치 (auto-generate) | 초안 | 안 보임 |
| 수정의뢰 적용 (change-requests/apply) | 초안 | 안 보임 |
| **확정 (publish)** | 초안 → 확정본 | **여기서 공개됨** |
| 초안 파기 (discard-draft) | 확정본 → 초안 | 안 보임 (확정본은 불변) |

* 스태프용 `GET`은 항상 확정본을 반환합니다. 한 번도 확정하지 않은 주는 「미생성」과 동일하게 `404`를 반환하며, 클라이언트는 기존의 빈 상태 표시를 그대로 사용합니다.
* 매니저용 `GET`은 초안을 반환하고, 미확정 변경이 남아 있는지를 `has_unpublished_changes`로 전달합니다.
* `has_unpublished_changes`는 타임스탬프가 아니라 **내용 비교**로 판정합니다(`_id`는 무시). 삭제 후 같은 내용을 다시 만든 경우에 「미확정 변경 있음」으로 잘못 표시하지 않기 위해서입니다. 여기에 더해, 후술할 `applied` 상태의 수정의뢰가 1건이라도 남아 있으면 `true`가 됩니다.

### 수정의뢰 상태 연동

수정의뢰도 같은 원칙을 따라 `pending` → `applied` → `resolved` 3단계를 가집니다.

| 상태 | 의미 | 스태프에게 보이는 모습 |
|---|---|---|
| `pending` | 미대응 | 미대응 |
| `applied` | 초안에 반영됨·**미확정** | `pending`으로 가려서 반환 |
| `resolved` | 확정됨 | 대응 완료 |

적용만 했는데 「대응 완료」로 보이면, 스태프의 시프트표는 그대로인데 대응이 끝났다고 오해하게 됩니다. 그래서 `applied`는 매니저에게만 보이며, 확정과 동시에 `resolved`로 넘어갑니다.

---

## 4. 데이터 구조 (Shift Table Schema)

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
  ],
  "published_shifts": [],
  "published_at": "2026-09-06T12:00:00Z"
}
```

* `shifts`는 초안, `published_shifts`는 확정본입니다. 둘 다 동일한 Shift 구조입니다.
* `published_shifts`는 DB상의 필드이며 API 응답에는 나타나지 않습니다. `shifts`에 「그 사용자가 봐야 할 버전」을 실어 반환하므로, 클라이언트는 둘을 구분하지 않고 그릴 수 있습니다.
* `published_at`이 `null`(미설정)인 주는 한 번도 확정되지 않았으며 스태프에게 미공개입니다.
* `week_start_date`는 해당 주의 월요일을 나타내는 `YYYY-MM-DD` 형식 문자열입니다. 월요일이 아닌 날짜는 잘못된 값으로 거부됩니다.
* `day`는 [스태프 근무가능시간](./staff-availability.ko.md)과 동일한 요일 키 체계(`monday`~`sunday`)입니다.
* `start_time` / `end_time`은 `HH:MM`(24시간 표기) 형식이며, `start_time < end_time`이어야 합니다.

---

## 5. API 명세 (API Specification)

### `GET /api/stores/{storeId}/shift-tables/{weekStartDate}`

* **설명**: 지정 주의 시프트표를 조회합니다. 호출자의 역할에 따라 반환하는 버전이 달라집니다.
* **권한**: 매니저, 또는 승인된 스태프.
* **응답**:
  * 매니저: `200 OK` — `shifts`에 초안, `has_unpublished_changes`에 미확정 플래그.
  * 스태프: `200 OK` — `shifts`에 확정본. 한 번도 확정하지 않은 주는 `404 Not Found`.
  * `404 Not Found` — 해당 주 시프트표 미생성.

### `POST /api/stores/{storeId}/shift-tables/{weekStartDate}/publish`

* **설명**: 초안을 확정해 스태프에게 공개합니다. 동시에 `applied` 상태의 수정의뢰를 `resolved`로 넘깁니다.
* **권한**: 매니저만.
* **요청 바디**: 없음.
* **응답**: `200 OK` — `{ "shift_table": { ... }, "resolved_request_count": 2 }`. `404 Not Found` — 시프트표 미생성.

### `POST /api/stores/{storeId}/shift-tables/{weekStartDate}/discard-draft`

* **설명**: 초안을 파기하고 확정본의 내용으로 되돌립니다. 확정본 자체는 바뀌지 않으므로, 스태프에게 보이는 시프트표는 이 조작으로 변하지 않습니다. 동시에 `applied` 상태의 수정의뢰를 `pending`으로 되돌립니다(반영 대상인 초안을 버리는 이상, 반영됨 상태만 남길 수 없기 때문). 한 번도 확정하지 않은 주는 「빈 시프트표」가 되돌릴 대상이 됩니다.
* **권한**: 매니저만.
* **요청 바디**: 없음.
* **응답**: `200 OK` — `{ "shift_table": { ... }, "reverted_request_count": 1 }`. `404 Not Found` — 시프트표 미생성.

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

## 6. 클라이언트 UI

`yoyaku_mate_provider`의 스태프 관리 화면은 2페이지 구성이며, 페이지1(스태프 관리)을 오른쪽으로 스와이프하면 페이지2(시프트표)로 전환됩니다. 시프트표 화면은 Outlook / Microsoft Teams의 주간 캘린더 화면을 참고한, 요일×시간 그리드 UI입니다. 자세한 내용은 [클라이언트 측 구현 상세서](../../../yoyaku_mate_provider/docs/implementation/shift-table.ko.md)를 참조하세요.

---

## 7. 향후 계획 (Roadmap)

스태프에게의 공개는 「확정」을 누른 시점의 시프트표 사본을 배포할 뿐이며, 스태프 화면으로 실시간 통지하는 구조(SSE / 푸시 알림)는 아직 없습니다. 스태프는 화면을 다시 열 때 최신 확정본을 가져옵니다.
