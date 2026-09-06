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

    Shifts          []Shift    `bson:"shifts,omitempty" json:"shifts,omitempty"`     // 초안
    PublishedShifts []Shift    `bson:"published_shifts,omitempty" json:"-"`          // 확정본
    PublishedAt     *time.Time `bson:"published_at,omitempty" json:"published_at,omitempty"`

    CreatedAt time.Time `bson:"created_at" json:"created_at"`
    UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`

    HasUnpublishedChanges bool `bson:"-" json:"has_unpublished_changes"` // GET 시 산출
}
```

`store_staff_info`의 `Availability` 임베디드 구조체와 마찬가지로, 시프트를 별도 컬렉션으로 분리하지 않고 `ShiftTable` 문서 내에 배열로 임베드하는 설계를 채택했다(주×매장 단위로만 참조되는 데이터라 join 없이 단순하게 유지 가능). 컬렉션명은 `shift_tables`(`data/constants.go`의 `CollectionShiftTables`).

### 초안과 확정본을 같은 문서의 두 필드로 두는 이유

매니저의 작업 중인 표가 스태프에게 보이지 않도록, 편집 중인 초안과 공개된 확정본을 분리했다(→ [ADR-007](../decisions/ADR-007-shift-table-draft-publish.ko.md)). 별도 문서로 나누지 않고 같은 문서 안의 두 배열로 둔 덕분에, 기존 편집 엔드포인트(`AddShift`/`UpdateShift`/`DeleteShift`/자동배치/수정의뢰 적용)는 `shifts`를 다루는 그대로 전혀 손대지 않아도 됐다.

`PublishedShifts`는 `json:"-"`로 응답에 내보내지 않는다. 대신 `GetShiftTableHandler`가 호출자의 역할에 따라 `Shifts`에 실어 반환하므로, 클라이언트는 초안/확정본을 구분하지 않고 `shifts`만 그리면 된다.

`HasUnpublishedChanges`는 `bson:"-"`로 영속화하지 않고 GET마다 산출하는 파생값이다.

---

## 2. 리포지토리 계층 (`data/shift_table_repo.go`)

```go
type ShiftTableRepository interface {
    GetShiftTable(storeID, weekStartDate string) (*models.ShiftTable, error)
    CreateShiftTable(table models.ShiftTable) error
    AddShift(shiftTableID string, shift models.Shift) error
    UpdateShift(shiftTableID, shiftID string, shift models.Shift) error
    DeleteShift(shiftTableID, shiftID string) error
    ReplaceShifts(shiftTableID string, shifts []models.Shift) error
    PublishShifts(shiftTableID string, shifts []models.Shift, publishedAt time.Time) error
}
```

* `GetShiftTable`은 `store_id` + `week_start_date` 복합 조건으로 `FindOne`. 찾지 못하면 `mongo.ErrNoDocuments`를 그대로 호출측에 반환하고, 핸들러에서 "미생성" 상태로 `404`로 변환한다(`staff_repo.go`의 `GetStoreStaffByUserAndStore`와 동일한 패턴).
* `AddShift`는 `$push`로 시프트 배열에 1건 추가.
* `UpdateShift`는 `shifts._id`를 조건에 포함한 positional operator(`shifts.$`)로 해당 시프트만 치환.
* `DeleteShift`는 `$pull`로 해당 `_id`의 시프트를 배열에서 제거.
* `ReplaceShifts`는 `shifts` 배열을 통째로 치환. 자동배치·수정의뢰 일괄 적용·초안 파기가 사용한다.
* `PublishShifts`는 `shifts`의 내용을 `published_shifts`로 복사하고 `published_at`을 갱신한다. **시프트표가 스태프에게 보이게 되는 유일한 쓰기**다. 빈 배열로 덮어쓸 필요가 있으므로, `nil`이 넘어오면 명시적으로 `[]models.Shift{}`로 변환한 뒤 `$set`한다(`omitempty`로 필드가 통째로 사라지는 것을 방지).
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
| `AutoGenerateShiftsHandler` | 매니저만 |
| `PublishShiftTableHandler` / `DiscardShiftTableDraftHandler` | 매니저만 |
| `ApplyShiftChangeRequestsHandler` / `DeleteShiftChangeRequestHandler` | 매니저만 |
| `CreateShiftChangeRequestHandler` / `GetShiftChangeRequestsHandler` | 매니저, 또는 승인된 스태프 |

`hasViewAccess`/`hasManageAccess`는 `store_staff_handler.go`의 `hasStaffManagementAccess`와 동일한 `userRepo.CheckStorePermission` 기반 패턴을 따른다.

### 3.2 입력값 검증

* `isValidWeekStartDate`: `time.Parse("2006-01-02", ...)`로 파싱 후 `Weekday() == time.Monday`를 확인.
* `validateShiftRequest`: 요일이 `monday`~`sunday` 중 하나인지, `start_time`/`end_time`이 정규식 `^([01]\d|2[0-3]):([0-5]\d)$`에 일치하고 `start_time < end_time`인지 확인(문자열 비교로 판정 가능한 `HH:MM` 형식이기 때문).
* `validateAndBuildShift`: 위에 더해 `staffRepo.GetStoreStaffByID`로 대상 스태프가 해당 매장 소속이고 `APPROVED` 상태인지 확인한 뒤 `models.Shift`를 구성하는 공통 처리. `AddShiftHandler`/`UpdateShiftHandler` 양쪽에서 호출된다.

### 3.3 역할에 따른 응답 분기 (`GetShiftTableHandler`)

`hasManageAccess` 결과로 반환 내용을 바꾼다.

* **스태프**: `PublishedAt == nil`(한 번도 확정 안 됨)이면 `404`를 반환해 「아직 작성되지 않음」과 같은 빈 상태로 클라이언트를 유도한다. 확정됐으면 `Shifts = PublishedShifts`로 실어 반환하고 초안은 일절 노출하지 않는다.
* **매니저**: 초안을 그대로 반환하고 `has_unpublished_changes`를 덧붙인다.

### 3.4 미확정 판정 (`hasUnpublishedChanges` / `shiftsContentEqual`)

`updated_at`과 `published_at`의 시각 비교가 아니라 시프트의 **내용 비교**로 판정한다. 삭제 후 같은 내용을 다시 만들면 `_id`만 바뀌므로, 시각으로 보면 「실질적으로 아무것도 안 바꿨는데 미확정」으로 잘못 표시되기 때문이다. `shiftsContentEqual`은 `staff_id|day|start|end` 키를 양쪽에서 만들어 정렬 후 비교한다(`_id`는 보지 않음).

여기에 더해 `applied` 상태의 수정의뢰가 1건이라도 있으면 `true`를 반환한다. 출고 재요청(superseded)이나 진부화(stale) 의뢰는 시프트표를 바꾸지 않고 상태만 변하므로, 이를 잡지 않으면 확정 버튼이 나오지 않아 `applied`인 채로 계속 남는다.

### 3.5 확정과 파기

| 핸들러 | 동작 |
|---|---|
| `PublishShiftTableHandler` | `shifts` → `published_shifts`로 복사. 이어서 `ResolveAppliedForWeek`으로 `applied` 의뢰를 `resolved`로 진행 |
| `DiscardShiftTableDraftHandler` | `published_shifts` → `shifts`로 되돌림(확정본은 불변). 이어서 `RevertAppliedForWeek`으로 `applied`를 `pending`으로 복귀 |

둘 다 수정의뢰 상태 갱신은 본체 쓰기 성공 후에 수행하며, 실패해도 로그만 남기고 처리를 계속한다(시프트표 자체는 이미 확정/파기됐으므로, 거기서 `500`을 반환하면 상태가 더 알기 어려워진다).

한 번도 확정하지 않은 주에 대한 파기는 되돌릴 대상이 「빈 시프트표」가 된다. 여기를 거부하면 시프트를 삭제한 상태에서 자동배치를 다시 돌릴 수단이 없어지므로(하단 버튼이 확정으로 바뀌어 있기 때문) 허용하고 있다.

### 3.6 수정의뢰 상태 (`applied` 도입)

`pending` → `applied` → `resolved` 3단계. `applied`는 「초안에 반영됐지만 미확정」을 나타내는 중간 상태로, 매니저에게만 보인다.

* `ApplyShiftChangeRequestsHandler`는 반영한 의뢰를 `MarkRequestApplied`로 `applied`로 만든다(이전에는 곧바로 `resolved`로 했다). superseded / stale 정리도 마찬가지로 `applied`에서 멈춘다.
* `GetShiftChangeRequestsHandler`는 호출자가 매니저가 아니면 `maskAppliedAsPending`으로 `applied`를 `pending`으로 가려 반환한다. 적용만 했는데 「대응 완료」로 보이면, 스태프의 시프트표는 그대로인데 대응이 끝났다고 오해하기 때문이다. 가리는 처리는 복사본에 하며 인자로 받은 슬라이스는 변경하지 않는다.

### 3.7 시프트표 미생성 시 처리

`AddShiftHandler`/`UpdateShiftHandler`/`DeleteShiftHandler`는 모두 먼저 `shiftTableRepo.GetShiftTable`로 시프트표 존재 여부를 확인하고, `mongo.ErrNoDocuments`인 경우 "먼저 시프트표를 생성하세요"라는 취지의 오류를 `404`로 반환한다. 시프트는 시프트표의 서브 리소스이며, 부모 문서가 없는 상태에서의 시프트 추가는 허용하지 않는 설계다.

---

## 4. 라우팅 (`handlers/router.go`)

```go
api.HandleFunc("/stores/{storeId}/shift-tables", shiftTableHandler.CreateShiftTableHandler).Methods("POST", "OPTIONS")
api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}", shiftTableHandler.GetShiftTableHandler).Methods("GET", "OPTIONS")
api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/shifts", shiftTableHandler.AddShiftHandler).Methods("POST", "OPTIONS")
api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/shifts/{shiftId}", shiftTableHandler.UpdateShiftHandler).Methods("PATCH", "OPTIONS")
api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/shifts/{shiftId}", shiftTableHandler.DeleteShiftHandler).Methods("DELETE", "OPTIONS")
api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/auto-generate", shiftTableHandler.AutoGenerateShiftsHandler).Methods("POST", "OPTIONS")
api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/publish", shiftTableHandler.PublishShiftTableHandler).Methods("POST", "OPTIONS")
api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/discard-draft", shiftTableHandler.DiscardShiftTableDraftHandler).Methods("POST", "OPTIONS")
```

모두 점주 앱 전용 라우트(`providerApi`) 아래에 있어 Firebase 인증과 단말 세션 검증 미들웨어가 자동으로 적용된다.

Staff Management endpoints 그룹 바로 뒤에 배치. 다른 엔드포인트와 달리 `{weekStartDate}`와 고정 세그먼트 `shift-tables`(인자 없음)가 경로 깊이 1 위치에서 갈라지기 때문에, `staff/me/availability`와 같은 등록 순서 함정은 발생하지 않는다.

`main.go`에서는 `shiftTableRepo := &data.MongoShiftTableRepo{}`를 생성하고, 기존 `staffRepo`/`userRepo`/`authSvc`와 함께 `handlers.NewShiftTableHandler(...)`에 주입한다(기존 핸들러들과 동일한 DI 패턴).

---

## 5. 데이터 마이그레이션 (`scripts/migrate_shift_table_publish/`)

`published_shifts` / `published_at`을 나중에 추가했기 때문에 기존 문서에는 이 필드들이 없다. 마이그레이션하지 않고 새 서버를 배포하면 `published_at == nil`이 「한 번도 확정 안 됨」으로 판정되어, **공개돼 있던 시프트표가 전 스태프에게서 사라진다**(매니저 화면은 정상으로 보이므로 발견이 늦다).

이관용으로 일회성 Go 프로그램을 두었다. `config.Load()`를 재사용하므로 접속처가 서버와 항상 일치한다. 기본은 확인만 하고, `-apply`를 붙였을 때만 기록한다. `published_at`이 없는 문서만 대상으로 하므로 멱등하다.

```bash
go run ./scripts/migrate_shift_table_publish          # 대상 건수 확인
go run ./scripts/migrate_shift_table_publish -apply   # 실제 적용
```

현시점에 존재하는 시프트표는 전부 공개됐던 것으로 간주해 `shifts`를 그대로 `published_shifts`로 옮기고, `published_at`에는 기존 `updated_at`(없으면 `created_at`)을 넣는다.

---

## 6. 클라이언트 연동

→ 클라이언트 측 상세는 [`yoyaku_mate_provider` 측 구현 상세서](../../../yoyaku_mate_provider/docs/implementation/shift-table.ko.md)를 참조하세요.
