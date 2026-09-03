# 実装詳細書: シフト表 (Shift Table)

本文書は、`yoyaku_mate_server` に実装されたシフト表 (Shift Table) 機能の技術的設計および実装詳細を説明します。

> 作成日: 2026-09-01
> 関連文書: [シフト表機能仕様書](../features/shift-table.md)

---

## 1. データモデル (`models/shift_table_model.go`)

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

`store_staff_info` の `Availability` embedded struct と同様、シフトを別コレクションに分離せず `ShiftTable` ドキュメント内に配列で埋め込む設計とした(週×店舗単位でしか参照されないデータのため、join不要でシンプルに保てる)。コレクション名は `shift_tables` (`data/constants.go` の `CollectionShiftTables`)。

---

## 2. リポジトリ層 (`data/shift_table_repo.go`)

```go
type ShiftTableRepository interface {
    GetShiftTable(storeID, weekStartDate string) (*models.ShiftTable, error)
    CreateShiftTable(table models.ShiftTable) error
    AddShift(shiftTableID string, shift models.Shift) error
    UpdateShift(shiftTableID, shiftID string, shift models.Shift) error
    DeleteShift(shiftTableID, shiftID string) error
}
```

* `GetShiftTable` は `store_id` + `week_start_date` の複合条件で `FindOne`。見つからない場合は `mongo.ErrNoDocuments` をそのまま呼び出し元に返し、ハンドラー側で「未作成」として `404` に変換する(`staff_repo.go` の `GetStoreStaffByUserAndStore` と同じパターン)。
* `AddShift` は `$push` でシフト配列に1件追加。
* `UpdateShift` は `shifts._id` を条件に含めた positional operator (`shifts.$`) で該当シフトのみ置換。
* `DeleteShift` は `$pull` で該当 `_id` のシフトを配列から除去。
* 週の一意性(店舗×週開始日)は、`CreateShiftTable` 呼び出し前にハンドラー側で `GetShiftTable` による存在チェックを行うことで保証している(専用のユニークインデックスは未導入。既存コレクション群も同様の運用方針)。

`StaffRepository` (`data/staff_repo.go`) にはシフト対象スタッフの検証用に `GetStoreStaffByID(staffID string) (*models.StoreStaffInfo, error)` を追加した。

---

## 3. ハンドラー層 (`handlers/shift_table_handler.go`)

### 3.1 権限モデル

| ハンドラー | 権限 |
|---|---|
| `GetShiftTableHandler` | マネージャー、または承認済みスタッフ (`hasViewAccess`) |
| `CreateShiftTableHandler` | マネージャーのみ (`hasManageAccess`) |
| `AddShiftHandler` / `UpdateShiftHandler` / `DeleteShiftHandler` | マネージャーのみ |

`hasViewAccess`/`hasManageAccess` は `store_staff_handler.go` の `hasStaffManagementAccess` と同じ `userRepo.CheckStorePermission` ベースのパターンを踏襲。

### 3.2 入力値検証

* `isValidWeekStartDate`: `time.Parse("2006-01-02", ...)` でパース後、`Weekday() == time.Monday` を確認。
* `validateShiftRequest`: 曜日が `monday`〜`sunday` のいずれかであること、`start_time`/`end_time` が正規表現 `^([01]\d|2[0-3]):([0-5]\d)$` に一致し `start_time < end_time` であることを確認(文字列比較で判定可能な `HH:MM` 形式のため)。
* `validateAndBuildShift`: 上記に加え、`staffRepo.GetStoreStaffByID` で対象スタッフが当該店舗に所属し `APPROVED` 状態であることを確認してから `models.Shift` を構築する共通処理。`AddShiftHandler`/`UpdateShiftHandler` の両方から呼ばれる。

### 3.3 シフト表未作成時のハンドリング

`AddShiftHandler`/`UpdateShiftHandler`/`DeleteShiftHandler` はいずれも、まず `shiftTableRepo.GetShiftTable` でシフト表の存在を確認し、`mongo.ErrNoDocuments` の場合は `404` で「先にシフト表を作成してください」という趣旨のエラーを返す。シフトはシフト表のサブリソースであり、親ドキュメントが存在しない状態でのシフト追加は許可しない設計。

---

## 4. ルーティング (`handlers/router.go`)

```go
api.HandleFunc("/stores/{storeId}/shift-tables", shiftTableHandler.CreateShiftTableHandler).Methods("POST", "OPTIONS")
api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}", shiftTableHandler.GetShiftTableHandler).Methods("GET", "OPTIONS")
api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/shifts", shiftTableHandler.AddShiftHandler).Methods("POST", "OPTIONS")
api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/shifts/{shiftId}", shiftTableHandler.UpdateShiftHandler).Methods("PATCH", "OPTIONS")
api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/shifts/{shiftId}", shiftTableHandler.DeleteShiftHandler).Methods("DELETE", "OPTIONS")
```

Staff Management endpoints グループの直後に配置。他エンドポイントと異なり `{weekStartDate}` と固定セグメント `shift-tables`(引数なし)がパスの深さ1の位置で分岐するため、`staff/me/availability` のような登録順の罠は発生しない。

`main.go` では `shiftTableRepo := &data.MongoShiftTableRepo{}` を生成し、既存の `staffRepo`/`userRepo`/`authSvc` と合わせて `handlers.NewShiftTableHandler(...)` に注入する(既存ハンドラー群と同じ DI パターン)。

---

## 5. クライアント連携

→ クライアント側の詳細は [`yoyaku_mate_provider` 側の実装詳細書](../../../yoyaku_mate_provider/docs/implementation/shift-table.md) を参照。
