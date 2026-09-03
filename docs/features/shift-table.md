# 機能仕様書: シフト表 (Shift Table)

本文書は、`yoyaku_mate_server` に実装された店舗の週単位シフト表 (Shift Table) 機能仕様を定義します。

> 作成日: 2026-09-01
> 関連文書: [シフト表実装詳細書](../implementation/shift-table.md)、[スタッフ勤務可能時間機能仕様書](./staff-availability.md)

---

## 1. 概要 (Overview)

店舗マネージャーが週単位で各スタッフの勤務シフト(曜日・時間帯・担当スタッフ)を管理できる機能です。[スタッフ勤務可能時間](./staff-availability.md)機能で挙がっていた「シフト表の自動生成」の前段として、まずマネージャーが手動でシフトを組む機能を実装しました。

シフト表は週(月曜始まり)ごとに独立したドキュメントで管理され、**マネージャーが明示的に作成しない限り、その週のシフト表は存在しません**。未作成の週を照会した場合は「未作成」として扱われます。

---

## 2. 主な機能 (Key Features)

1. **週単位のシフト表作成 (マネージャー専用)**:
   - 対象週の月曜日の日付を指定して、空のシフト表を新規作成します。
   - 既に同じ週のシフト表が存在する場合は作成できません(重複作成の防止)。
2. **シフトの追加/修正/削除 (マネージャー専用)**:
   - 作成済みのシフト表に対し、スタッフ・曜日・開始/終了時刻を指定してシフトを1件単位で追加/修正/削除します。
   - シフトに割り当てられるスタッフは、当該店舗の承認済み (`APPROVED`) スタッフに限られます。
3. **シフト表の照会 (マネージャー / 承認済みスタッフ)**:
   - マネージャーおよび承認済みスタッフは、週を指定してシフト表(登録済みシフト一覧)を照会できます。
   - 照会のみはスタッフにも許可されますが、作成・追加・修正・削除はマネージャーのみ可能です。

---

## 3. データ構造 (Shift Table Schema)

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

* `week_start_date` はその週の月曜日を表す `YYYY-MM-DD` 形式の文字列。月曜日以外の日付は不正値として拒否されます。
* `day` は [スタッフ勤務可能時間](./staff-availability.md)と同じ曜日キー体系 (`monday`〜`sunday`)。
* `start_time` / `end_time` は `HH:MM` (24時間表記) 形式で、`start_time < end_time` である必要があります。

---

## 4. API 仕様 (API Specification)

### `GET /api/stores/{storeId}/shift-tables/{weekStartDate}`

* **説明**: 指定週のシフト表を取得します。
* **権限**: マネージャー、または承認済みスタッフ。
* **レスポンス**: `200 OK` — 上記スキーマを返却。`404 Not Found` — その週にシフト表が未作成。

### `POST /api/stores/{storeId}/shift-tables`

* **説明**: 指定週の空のシフト表を新規作成します。
* **権限**: マネージャーのみ。
* **リクエストボディ**: `{ "week_start_date": "2026-09-07" }`
* **レスポンス**: `201 Created`。`409 Conflict` — 既に同じ週のシフト表が存在する場合。

### `POST /api/stores/{storeId}/shift-tables/{weekStartDate}/shifts`

* **説明**: シフトを1件追加します。
* **権限**: マネージャーのみ。
* **リクエストボディ**: `{ "staff_id": "...", "day": "monday", "start_time": "09:00", "end_time": "17:00" }`
* **レスポンス**: `201 Created`。`404 Not Found` — シフト表が未作成の場合(先に作成が必要)。

### `PATCH /api/stores/{storeId}/shift-tables/{weekStartDate}/shifts/{shiftId}`

* **説明**: 既存シフトを1件更新します。
* **権限**: マネージャーのみ。
* **リクエストボディ**: `POST /shifts` と同様。

### `DELETE /api/stores/{storeId}/shift-tables/{weekStartDate}/shifts/{shiftId}`

* **説明**: 既存シフトを1件削除します。
* **権限**: マネージャーのみ。

---

## 5. クライアントUI

`yoyaku_mate_provider` のスタッフ管理画面は2ページ構成になっており、ページ1(スタッフ管理)を右にスワイプするとページ2(シフト表)に切り替わります。シフト表画面は Outlook / Microsoft Teams の週間カレンダー表示を参考にした、曜日×時間のグリッドUIです。詳細は [クライアント側実装詳細書](../../../yoyaku_mate_provider/docs/implementation/shift-table.md) を参照。

---

## 6. 今後の計画 (Roadmap)

現時点では手動でのシフト作成のみをサポートしています。[スタッフ勤務可能時間](./staff-availability.md)で蓄積したデータをもとに、シフト表を自動生成する機能は未実装です。
