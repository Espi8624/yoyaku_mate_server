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

    Shifts          []Shift    `bson:"shifts,omitempty" json:"shifts,omitempty"`     // 下書き
    PublishedShifts []Shift    `bson:"published_shifts,omitempty" json:"-"`          // 確定版
    PublishedAt     *time.Time `bson:"published_at,omitempty" json:"published_at,omitempty"`

    CreatedAt time.Time `bson:"created_at" json:"created_at"`
    UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`

    HasUnpublishedChanges bool `bson:"-" json:"has_unpublished_changes"` // GET時に算出
}
```

`store_staff_info` の `Availability` embedded struct と同様、シフトを別コレクションに分離せず `ShiftTable` ドキュメント内に配列で埋め込む設計とした(週×店舗単位でしか参照されないデータのため、join不要でシンプルに保てる)。コレクション名は `shift_tables` (`data/constants.go` の `CollectionShiftTables`)。

### 下書きと確定版を同一ドキュメントの2フィールドで持つ理由

マネージャーの作りかけがスタッフに見えないよう、編集中の下書きと公開済みの確定版を分離している(→ [ADR-007](../decisions/ADR-007-shift-table-draft-publish.md))。別ドキュメントに分けず同じドキュメント内の2配列にしたことで、既存の編集エンドポイント (`AddShift`/`UpdateShift`/`DeleteShift`/自動配置/修正依頼の適用) は `shifts` を触るまま一切変更せずに済んでいる。

`PublishedShifts` は `json:"-"` でレスポンスに出さない。代わりに `GetShiftTableHandler` が呼び出し元の役割に応じて `Shifts` に載せ替えて返すため、クライアントは下書き/確定版を区別せず `shifts` を描画するだけでよい。

`HasUnpublishedChanges` は `bson:"-"` で永続化せず、GET のたびに算出する派生値。

---

## 2. リポジトリ層 (`data/shift_table_repo.go`)

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

* `GetShiftTable` は `store_id` + `week_start_date` の複合条件で `FindOne`。見つからない場合は `mongo.ErrNoDocuments` をそのまま呼び出し元に返し、ハンドラー側で「未作成」として `404` に変換する(`staff_repo.go` の `GetStoreStaffByUserAndStore` と同じパターン)。
* `AddShift` は `$push` でシフト配列に1件追加。
* `UpdateShift` は `shifts._id` を条件に含めた positional operator (`shifts.$`) で該当シフトのみ置換。
* `DeleteShift` は `$pull` で該当 `_id` のシフトを配列から除去。
* `ReplaceShifts` は `shifts` 配列を丸ごと置換。自動配置・修正依頼の一括適用・下書きの破棄が使う。
* `PublishShifts` は `shifts` の内容を `published_shifts` へコピーし `published_at` を更新する。**シフト表がスタッフから見えるようになる唯一の書き込み**。空配列で上書きする必要があるため、`nil` を渡された場合は明示的に `[]models.Shift{}` に変換してから `$set` する (`omitempty` でフィールドごと消えるのを防ぐ)。
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
| `AutoGenerateShiftsHandler` | マネージャーのみ |
| `PublishShiftTableHandler` / `DiscardShiftTableDraftHandler` | マネージャーのみ |
| `ApplyShiftChangeRequestsHandler` / `DeleteShiftChangeRequestHandler` | マネージャーのみ |
| `CreateShiftChangeRequestHandler` / `GetShiftChangeRequestsHandler` | マネージャー、または承認済みスタッフ |

`hasViewAccess`/`hasManageAccess` は `store_staff_handler.go` の `hasStaffManagementAccess` と同じ `userRepo.CheckStorePermission` ベースのパターンを踏襲。

### 3.2 入力値検証

* `isValidWeekStartDate`: `time.Parse("2006-01-02", ...)` でパース後、`Weekday() == time.Monday` を確認。
* `validateShiftRequest`: 曜日が `monday`〜`sunday` のいずれかであること、`start_time`/`end_time` が正規表現 `^([01]\d|2[0-3]):([0-5]\d)$` に一致し `start_time < end_time` であることを確認(文字列比較で判定可能な `HH:MM` 形式のため)。
* `validateAndBuildShift`: 上記に加え、`staffRepo.GetStoreStaffByID` で対象スタッフが当該店舗に所属し `APPROVED` 状態であることを確認してから `models.Shift` を構築する共通処理。`AddShiftHandler`/`UpdateShiftHandler` の両方から呼ばれる。

### 3.3 役割による応答の出し分け (`GetShiftTableHandler`)

`hasManageAccess` の結果で返す内容を変える。

* **スタッフ**: `PublishedAt == nil` (一度も確定していない) なら `404` を返し、「まだ作成されていない」と同じ空状態にクライアントを寄せる。確定済みなら `Shifts = PublishedShifts` に載せ替えて返し、下書きは一切漏らさない。
* **マネージャー**: 下書きをそのまま返し、`hasUnpublishedChanges` を添える。

### 3.4 未確定判定 (`hasUnpublishedChanges` / `shiftsContentEqual`)

`updated_at` と `published_at` の時刻比較ではなく、シフトの**内容比較**で判定する。削除して同じ内容を作り直すと `_id` だけが変わるため、時刻で見ると「実質何も変えていないのに未確定」と誤表示されるため。`shiftsContentEqual` は `staff_id|day|start|end` のキーを両者で作ってソートし比較する(`_id` は見ない)。

加えて `applied` な修正依頼が1件でもあれば `true` を返す。出し直し(superseded)や陳腐化(stale)の依頼はシフト表を変えずにステータスだけが変わるため、これを拾わないと確定ボタンが出ず `applied` のまま残り続ける。

### 3.5 確定と破棄

| ハンドラー | 動作 |
|---|---|
| `PublishShiftTableHandler` | `shifts` → `published_shifts` へコピー。続けて `ResolveAppliedForWeek` で `applied` の修正依頼を `resolved` へ進める |
| `DiscardShiftTableDraftHandler` | `published_shifts` → `shifts` へ戻す(確定版は不変)。続けて `RevertAppliedForWeek` で `applied` を `pending` へ戻す |

どちらも修正依頼のステータス更新は本体の書き込み成功後に行い、失敗してもログに残して処理は続行する(シフト表自体は既に確定/破棄済みのため、そこで `500` を返すと状態がより分かりにくくなる)。

一度も確定していない週に対する破棄は、戻し先が「空のシフト表」になる。ここを拒否すると、シフトを削除しただけの状態から自動配置をやり直す手段が無くなる(下部ボタンが確定に切り替わっているため)ので許可している。

### 3.6 修正依頼のステータス (`applied` の導入)

`pending` → `applied` → `resolved` の3状態。`applied` は「下書きへ反映済みだが未確定」を表す中間状態で、マネージャーにしか見えない。

* `ApplyShiftChangeRequestsHandler` は反映できた依頼を `MarkRequestApplied` で `applied` にする(以前は直接 `resolved` にしていた)。superseded / stale の片付けも同様に `applied` 止まりにする。
* `GetShiftChangeRequestsHandler` は、呼び出し元がマネージャーでなければ `maskAppliedAsPending` で `applied` を `pending` に伏せて返す。適用しただけで「対応済み」と見せると、スタッフのシフト表は変わっていないのに対応が完了したと誤解されるため。伏せる処理はコピーに対して行い、引数のスライスは書き換えない。

### 3.7 シフト表未作成時のハンドリング

`AddShiftHandler`/`UpdateShiftHandler`/`DeleteShiftHandler` はいずれも、まず `shiftTableRepo.GetShiftTable` でシフト表の存在を確認し、`mongo.ErrNoDocuments` の場合は `404` で「先にシフト表を作成してください」という趣旨のエラーを返す。シフトはシフト表のサブリソースであり、親ドキュメントが存在しない状態でのシフト追加は許可しない設計。

---

## 4. ルーティング (`handlers/router.go`)

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

いずれも点主アプリ専用ルート (`providerApi`) 配下にあり、Firebase 認証と端末セッション検証のミドルウェアが自動的に適用される。

Staff Management endpoints グループの直後に配置。他エンドポイントと異なり `{weekStartDate}` と固定セグメント `shift-tables`(引数なし)がパスの深さ1の位置で分岐するため、`staff/me/availability` のような登録順の罠は発生しない。

`main.go` では `shiftTableRepo := &data.MongoShiftTableRepo{}` を生成し、既存の `staffRepo`/`userRepo`/`authSvc` と合わせて `handlers.NewShiftTableHandler(...)` に注入する(既存ハンドラー群と同じ DI パターン)。

---

## 5. データ移行 (`scripts/migrate_shift_table_publish/`)

`published_shifts` / `published_at` を後から追加したため、既存ドキュメントにはこれらが無い。移行せずに新サーバーをデプロイすると `published_at == nil` が「一度も確定していない」と判定され、**公開済みだったシフト表が全スタッフから見えなくなる**(マネージャー画面は正常に見えるため発見が遅れる)。

移行用に一度きりの Go プログラムを用意した。`config.Load()` を再利用するため接続先はサーバーと常に一致する。既定は確認のみで、`-apply` を付けた時だけ書き込む。`published_at` を持たないドキュメントのみを対象にするため冪等。

```bash
go run ./scripts/migrate_shift_table_publish          # 対象件数の確認
go run ./scripts/migrate_shift_table_publish -apply   # 実際に適用
```

現時点で存在するシフト表は全て公開済みだった扱いとし、`shifts` をそのまま `published_shifts` へ写し、`published_at` には既存の `updated_at`(無ければ `created_at`)を充てる。

---

## 6. クライアント連携

→ クライアント側の詳細は [`yoyaku_mate_provider` 側の実装詳細書](../../../yoyaku_mate_provider/docs/implementation/shift-table.md) を参照。
