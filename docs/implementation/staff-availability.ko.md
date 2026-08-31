# 구현 상세서: 스태프 근무 가능 시간 (Staff Availability)

본 문서는 `yoyaku_mate_server`에 구현된 스태프 근무 가능 시간(Staff Availability) 기능의 기술적 설계 및 구현 상세를 설명합니다.

> 작성일: 2026-08-30  
> 관련 문서: [스태프 근무 가능 시간 기능 사양서](../features/staff-availability.ko.md)

---

## 1. 데이터 모델 (`models/store_staff_info_model.go`)

```go
const (
    TimeBlockMorning   = "MORNING"
    TimeBlockAfternoon = "AFTERNOON"
)

type Availability struct {
    Monday    []string `bson:"monday,omitempty" json:"monday,omitempty"`
    Tuesday   []string `bson:"tuesday,omitempty" json:"tuesday,omitempty"`
    Wednesday []string `bson:"wednesday,omitempty" json:"wednesday,omitempty"`
    Thursday  []string `bson:"thursday,omitempty" json:"thursday,omitempty"`
    Friday    []string `bson:"friday,omitempty" json:"friday,omitempty"`
    Saturday  []string `bson:"saturday,omitempty" json:"saturday,omitempty"`
    Sunday    []string `bson:"sunday,omitempty" json:"sunday,omitempty"`
}
```

`StoreStaffInfo`에 `Availability` 필드를 추가했다. 각 요일 필드가 빈 리스트인 것 자체가 "근무 불가"를 의미하도록 설계하여, 별도의 boolean 플래그는 두지 않았다.

---

## 2. 리포지토리 계층 (`data/staff_repo.go`)

`StaffRepository` 인터페이스에 다음을 추가:

* `GetStoreStaffByUserAndStore(userID, storeID) (*models.StoreStaffInfo, error)` — 본인 조회용. `user_id` + `store_id`로 `FindOne`.
* `UpdateStoreStaffAvailability(staffID string, availability models.Availability) error` — `$set`을 이용한 부분 업데이트 (`updated_at`도 함께 갱신).

기존 `GetStoreStaffByStoreID`의 aggregation 파이프라인(`$project`)에 `availability: 1`을 추가하여, 매니저용 스태프 목록 조회 시 `availability` 필드도 함께 반환되도록 했다.

---

## 3. 핸들러 계층 (`handlers/store_staff_handler.go`)

### 3.1 공통 인증 처리

```go
func (h *StoreStaffHandler) authenticateAndGetOwnStaffInfo(
    r *http.Request, storeID string,
) (*models.StoreStaffInfo, int, error)
```

토큰 검증 → `GetByFirebaseUID`로 사용자 특정 → `GetStoreStaffByUserAndStore`로 본인 스태프 레코드 조회 → `Status == StaffStatusApproved` 확인을 한 번에 처리한다. 본인용 엔드포인트(`GetMyAvailabilityHandler` / `UpdateMyAvailabilityHandler`)에서 사용한다.

### 3.2 핸들러 목록

| 핸들러 | 권한 모델 |
|---|---|
| `GetMyAvailabilityHandler` | 본인(승인된 스태프)만. 매니저 권한 불필요 |
| `UpdateMyAvailabilityHandler` | 위와 동일 |
| `UpdateStoreStaffAvailabilityHandler` | 매니저만 (`CheckStorePermission(user.ID, storeID, "manager", "")`). 기존 `UpdateStoreStaffStatusHandler` 등과 동일한 권한 체크 패턴을 따름 |

### 3.3 입력값 검증

```go
var validTimeBlocks = map[string]bool{
    models.TimeBlockMorning:   true,
    models.TimeBlockAfternoon: true,
}

func validateAvailability(a models.Availability) bool
```

7개 요일 리스트 내 값이 모두 `validTimeBlocks`에 포함되는지 검증하며, 잘못된 값이 있으면 `400 Bad Request`를 반환한다.

---

## 4. 라우팅 (`handlers/router.go`)

```go
api.HandleFunc("/stores/{storeId}/staff/me/availability", storeStaffHandler.GetMyAvailabilityHandler).Methods("GET", "OPTIONS")
api.HandleFunc("/stores/{storeId}/staff/me/availability", storeStaffHandler.UpdateMyAvailabilityHandler).Methods("PATCH", "OPTIONS")
api.HandleFunc("/stores/{storeId}/staff/{staffId}", storeStaffHandler.UpdateStoreStaffStatusHandler).Methods("PATCH", "OPTIONS")
api.HandleFunc("/stores/{storeId}/staff/{staffId}/permissions", storeStaffHandler.UpdateStoreStaffPermissionsHandler).Methods("PATCH", "OPTIONS")
api.HandleFunc("/stores/{storeId}/staff/{staffId}/availability", storeStaffHandler.UpdateStoreStaffAvailabilityHandler).Methods("PATCH", "OPTIONS")
```

**주의사항**: gorilla/mux는 등록된 순서대로 라우트 매칭을 시도하므로, 리터럴 경로 `/staff/me/availability`를 변수 경로 `/staff/{staffId}/availability`보다 **먼저** 등록해야 한다. 순서를 반대로 하면 `me`가 `{staffId}`로 캡처되어 본인용 엔드포인트에 도달하지 못하게 된다.

---

## 5. 클라이언트 연동

| 호출 주체 (`yoyaku_mate_provider`) | 사용 엔드포인트 |
|---|---|
| 개인 프로필 화면 (본인 입력) | `GET` / `PATCH /staff/me/availability` |
| 스태프 관리 화면 (매니저) | `PATCH /staff/{staffId}/availability`, 그리고 `GET /staff` 목록에 포함된 `availability` 필드 |

→ 클라이언트 측 상세는 [`yoyaku_mate_provider` 측 구현 상세서](../../../yoyaku_mate_provider/docs/implementation/staff-availability.ko.md)를 참조.
