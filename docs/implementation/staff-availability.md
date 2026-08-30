# 実装詳細書: スタッフ勤務可能時間 (Staff Availability)

本文書は、`yoyaku_mate_server` に実装されたスタッフ勤務可能時間 (Staff Availability) 機能の技術的設計および実装詳細を説明します。

> 作成日: 2026-08-30  
> 関連文書: [スタッフ勤務可能時間機能仕様書](../features/staff-availability.md)

---

## 1. データモデル (`models/store_staff_info_model.go`)

```go
const (
    TimeBlockMorning   = "MORNING"
    TimeBlockAfternoon = "AFTERNOON"
    TimeBlockEvening   = "EVENING"
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

`StoreStaffInfo` に `Availability` フィールドを追加。各曜日フィールドが空リストであることが「勤務不可」を表すため、別途 boolean フラグは持たない設計とした。

---

## 2. リポジトリ層 (`data/staff_repo.go`)

`StaffRepository` インターフェースに以下を追加:

* `GetStoreStaffByUserAndStore(userID, storeID) (*models.StoreStaffInfo, error)` — 本人照会用。`user_id` + `store_id` で `FindOne`。
* `UpdateStoreStaffAvailability(staffID string, availability models.Availability) error` — `$set` による部分更新 (`updated_at` も同時更新)。

既存の `GetStoreStaffByStoreID` の集計パイプライン (`$project`) に `availability: 1` を追加し、マネージャー向けスタッフ一覧取得時に `availability` フィールドも一緒に返却されるようにした。

---

## 3. ハンドラー層 (`handlers/store_staff_handler.go`)

### 3.1 共通認証処理

```go
func (h *StoreStaffHandler) authenticateAndGetOwnStaffInfo(
    r *http.Request, storeID string,
) (*models.StoreStaffInfo, int, error)
```

トークン検証 → `GetByFirebaseUID` によるユーザー特定 → `GetStoreStaffByUserAndStore` による本人のスタッフレコード取得 → `Status == StaffStatusApproved` の確認、をまとめて行う。本人用エンドポイント (`GetMyAvailabilityHandler` / `UpdateMyAvailabilityHandler`) から利用する。

### 3.2 ハンドラー一覧

| ハンドラー | 権限モデル |
|---|---|
| `GetMyAvailabilityHandler` | 本人 (承認済みスタッフ) のみ。マネージャー権限は不要 |
| `UpdateMyAvailabilityHandler` | 同上 |
| `UpdateStoreStaffAvailabilityHandler` | マネージャーのみ (`CheckStorePermission(user.ID, storeID, "manager", "")`)。既存の `UpdateStoreStaffStatusHandler` などと同一の権限チェックパターンを踏襲 |

### 3.3 入力値検証

```go
var validTimeBlocks = map[string]bool{
    models.TimeBlockMorning:   true,
    models.TimeBlockAfternoon: true,
    models.TimeBlockEvening:   true,
}

func validateAvailability(a models.Availability) bool
```

7曜日すべてのリスト内の値が `validTimeBlocks` に含まれるかを検証し、不正な値があれば `400 Bad Request` を返す。

---

## 4. ルーティング (`handlers/router.go`)

```go
api.HandleFunc("/stores/{storeId}/staff/me/availability", storeStaffHandler.GetMyAvailabilityHandler).Methods("GET", "OPTIONS")
api.HandleFunc("/stores/{storeId}/staff/me/availability", storeStaffHandler.UpdateMyAvailabilityHandler).Methods("PATCH", "OPTIONS")
api.HandleFunc("/stores/{storeId}/staff/{staffId}", storeStaffHandler.UpdateStoreStaffStatusHandler).Methods("PATCH", "OPTIONS")
api.HandleFunc("/stores/{storeId}/staff/{staffId}/permissions", storeStaffHandler.UpdateStoreStaffPermissionsHandler).Methods("PATCH", "OPTIONS")
api.HandleFunc("/stores/{storeId}/staff/{staffId}/availability", storeStaffHandler.UpdateStoreStaffAvailabilityHandler).Methods("PATCH", "OPTIONS")
```

**注意点**: gorilla/mux はルートを登録順にマッチングを試みるため、リテラルパス `/staff/me/availability` を変数パス `/staff/{staffId}/availability` より **先に** 登録する必要がある。順序を誤ると `me` が `{staffId}` として解釈され、本人用エンドポイントに到達できなくなる。

---

## 5. クライアント連携

| 呼び出し元 (`yoyaku_mate_provider`) | 使用エンドポイント |
|---|---|
| 個人プロフィール画面 (本人入力) | `GET` / `PATCH /staff/me/availability` |
| スタッフ管理画面 (マネージャー) | `PATCH /staff/{staffId}/availability`、および `GET /staff` 一覧に含まれる `availability` フィールド |

→ クライアント側の詳細は [`yoyaku_mate_provider` 側の実装詳細書](../../../yoyaku_mate_provider/docs/implementation/staff-availability.md) を参照。
