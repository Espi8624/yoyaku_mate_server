# 기능 사양서: 점포 업종 태그 (Store Business Category)

본 문서는 `yoyaku_mate_server`에 구현된 점포 업종 태그(Business Category) 기능 사양을 정의합니다.

> 작성일: 2026-09-09

---

## 1. 개요 (Overview)

점포(`store_info`)에 해당 점포가 어떤 업종에 속하는지를 나타내는 필수 태그 `business_category`를 추가했습니다. 점포 등록 시 반드시 선택해야 하며, 등록 이후에도 점포 설정 화면에서 변경할 수 있습니다.

---

## 2. 허용된 값 (Allowed Values)

고정된 5종의 문자열 중 하나만 허용됩니다 (`models.IsValidStoreCategory`로 검증).

| 값 | 의미 |
|---|---|
| `RESTAURANT` | 음식점 |
| `CAFE_DESSERT` | 카페·디저트 |
| `BEAUTY` | 미용실·뷰티 |
| `RETAIL` | 소매업 |
| `OTHER` | 기타 |

---

## 3. API 사양 (API Specification)

### `POST /api/auth/signup`, `POST /stores/add`

* 점포를 신규 생성하는 경우 요청 본문에 `business_category`(string)가 필수.
* 미지정 또는 빈 문자열인 경우: `400 Bad Request` (`"business category is required"`).
* 허용되지 않은 값인 경우: `400 Bad Request` (`"Invalid business category"` / `"invalid business category"`).

### `PUT /api/provider_store?store_id=xxx`

* 요청 본문에 `business_category`를 포함하면 변경 가능 (`allowedStoreUpdateFields`에 등록됨).
* 포함된 경우에만 검증: 문자열이 아니거나 허용된 5개 값 이외인 경우 `400 Bad Request` (`"Invalid business_category"`).
* 포함되지 않은 경우 기존 값 유지 (다른 항목만 부분 수정 가능).

### `GET /api/provider_store`, `GET /api/admin/stores?status=xxx`

* 응답의 점포 객체에 `business_category` 필드가 포함됨.

---

## 4. 관련 화면

* `yoyaku_mate_provider`: 점포 등록 위저드의 필수 스텝, 그리고 설정 → 점포 → 기본 정보의 편집 항목.
* `yoyaku_mate_admin`: 출점 심사(`StoreApprovalPage`) 목록·상세 모달에 참고 정보로 표시.
