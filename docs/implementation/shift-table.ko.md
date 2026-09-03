# 구현 상세서: 시프트표 (Shift Table)

본 문서는 `yoyaku_mate_server`에 구현된 시프트표(Shift Table) 기능의 기술적 설계 및 구현 상세를 설명합니다.

> 작성일: 2026-09-01
> 관련 문서: [시프트표 기능 명세서](../features/shift-table.ko.md)

---

## 1. 데이터 모델 (`models/shift_table_model.go`)

```go
type Shift struct {
    ID        primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
    StaffID   primitive.ObjectID `bson:"staff_id" json:"staff_id"`
    Day       string             `bson:"day" json:"day"`
    StartTime string             `bson:"start_time" json:"start_time"`
    EndTime   string             `bson:"end_time" json:"end_time"`
}

type ShiftTable struct {
    ID            primitive.ObjectID `bson:"_id,omitempty" json:"_id"`
    StoreID       string             `bson:"store_id" json:"store_id"`
    WeekStartDate string             `bson:"week_start_date" json:"week_start_date"`
    Shifts        []Shift            `bson:"shifts,omitempty" json:"shifts,omitempty"`
    CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
    UpdatedAt     time.Time          `bson:"updated_at" json:"updated_at"`
}
```

`store_staff_info`의 `Availability` 임베디드 구조체와 마찬가지로, 시프트를 별도 컬렉션으로 분리하지 않고 `ShiftTable` 문서 내에 배열로 임베드하는 설계를 채택했다(주×매장 단위로만 참조되는 데이터라 join 없이 단순하게 유지 가능). 컬렉션명은 `shift_tables`(`data/constants.go`의 `CollectionShiftTables`).

---

## 2. 리포지토리 계층 (`data/shift_table_repo.go`)

```go
type ShiftTableRepository interface {
    GetShiftTable(storeID, weekStartDate string) (*models.ShiftTable, error)
    CreateShiftTable(table models.ShiftTable) error
    AddShift(shiftTableID string, shift models.Shift) error
    UpdateShift(shiftTableID, shiftID string, shift models.Shift) error
    DeleteShift(shiftTableID, shiftID string) error
}
```

* `GetShiftTable`은 `store_id` + `week_start_date` 복합 조건으로 `FindOne`. 찾지 못하면 `mongo.ErrNoDocuments`를 그대로 호출측에 반환하고, 핸들러에서 "미생성" 상태로 `404`로 변환한다(`staff_repo.go`의 `GetStoreStaffByUserAndStore`와 동일한 패턴).
* `AddShift`는 `$push`로 시프트 배열에 1건 추가.
* `UpdateShift`는 `shifts._id`를 조건에 포함한 positional operator(`shifts.$`)로 해당 시프트만 치환.
* `DeleteShift`는 `$pull`로 해당 `_id`의 시프트를 배열에서 제거.
* 주의 유일성(매장×주 시작일)은 `CreateShiftTable` 호출 전 핸들러에서 `GetShiftTable`을 통한 존재 확인으로 보장한다(전용 유니크 인덱스는 미도입. 기존 컬렉션들과 동일한 운영 방식).

`StaffRepository`(`data/staff_repo.go`)에는 시프트 대상 스태프 검증용으로 `GetStoreStaffByID(staffID string) (*models.StoreStaffInfo, error)`를 추가했다.

---

## 3. 핸들러 계층 (`handlers/shift_table_handler.go`)

### 3.1 권한 모델

| 핸들러 | 권한 |
|---|---|
| `GetShiftTableHandler` | 매니저, 또는 승인된 스태프 (`hasViewAccess`) |
| `CreateShiftTableHandler` | 매니저만 (`hasManageAccess`) |
| `AddShiftHandler` / `UpdateShiftHandler` / `DeleteShiftHandler` | 매니저만 |

`hasViewAccess`/`hasManageAccess`는 `store_staff_handler.go`의 `hasStaffManagementAccess`와 동일한 `userRepo.CheckStorePermission` 기반 패턴을 따른다.

### 3.2 입력값 검증

* `isValidWeekStartDate`: `time.Parse("2006-01-02", ...)`로 파싱 후 `Weekday() == time.Monday`를 확인.
* `validateShiftRequest`: 요일이 `monday`~`sunday` 중 하나인지, `start_time`/`end_time`이 정규식 `^([01]\d|2[0-3]):([0-5]\d)$`에 일치하고 `start_time < end_time`인지 확인(문자열 비교로 판정 가능한 `HH:MM` 형식이기 때문).
* `validateAndBuildShift`: 위에 더해 `staffRepo.GetStoreStaffByID`로 대상 스태프가 해당 매장 소속이고 `APPROVED` 상태인지 확인한 뒤 `models.Shift`를 구성하는 공통 처리. `AddShiftHandler`/`UpdateShiftHandler` 양쪽에서 호출된다.

### 3.3 시프트표 미생성 시 처리

`AddShiftHandler`/`UpdateShiftHandler`/`DeleteShiftHandler`는 모두 먼저 `shiftTableRepo.GetShiftTable`로 시프트표 존재 여부를 확인하고, `mongo.ErrNoDocuments`인 경우 "먼저 시프트표를 생성하세요"라는 취지의 오류를 `404`로 반환한다. 시프트는 시프트표의 서브 리소스이며, 부모 문서가 없는 상태에서의 시프트 추가는 허용하지 않는 설계다.

---

## 4. 라우팅 (`handlers/router.go`)

```go
api.HandleFunc("/stores/{storeId}/shift-tables", shiftTableHandler.CreateShiftTableHandler).Methods("POST", "OPTIONS")
api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}", shiftTableHandler.GetShiftTableHandler).Methods("GET", "OPTIONS")
api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/shifts", shiftTableHandler.AddShiftHandler).Methods("POST", "OPTIONS")
api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/shifts/{shiftId}", shiftTableHandler.UpdateShiftHandler).Methods("PATCH", "OPTIONS")
api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/shifts/{shiftId}", shiftTableHandler.DeleteShiftHandler).Methods("DELETE", "OPTIONS")
```

Staff Management endpoints 그룹 바로 뒤에 배치. 다른 엔드포인트와 달리 `{weekStartDate}`와 고정 세그먼트 `shift-tables`(인자 없음)가 경로 깊이 1 위치에서 갈라지기 때문에, `staff/me/availability`와 같은 등록 순서 함정은 발생하지 않는다.

`main.go`에서는 `shiftTableRepo := &data.MongoShiftTableRepo{}`를 생성하고, 기존 `staffRepo`/`userRepo`/`authSvc`와 함께 `handlers.NewShiftTableHandler(...)`에 주입한다(기존 핸들러들과 동일한 DI 패턴).

---

## 5. 클라이언트 연동

→ 클라이언트 측 상세는 [`yoyaku_mate_provider` 측 구현 상세서](../../../yoyaku_mate_provider/docs/implementation/shift-table.ko.md)를 참조하세요.
