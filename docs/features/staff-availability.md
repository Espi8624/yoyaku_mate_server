# 機能仕様書: スタッフ勤務可能時間 (Staff Availability)

本文書は、`yoyaku_mate_server` に実装されたスタッフの曜日別勤務可能時間帯 (Staff Availability) 機能仕様を定義します。

> 作成日: 2026-08-30  
> 関連文書: [スタッフ勤務可能時間実装詳細書](../implementation/staff-availability.md)

---

## 1. 概要 (Overview)

将来的な「ボタン一つでのシフト表自動生成」機能の土台となるデータとして、店舗スタッフ本人が曜日ごとに勤務可能な時間帯（午前・午後・夜間）を入力し、店舗マネージャーがそれを照会・修正できる機能です。

各曜日は時間帯のリストで表現され、リストが空の曜日は「勤務不可」を意味します。専用の不可フラグは持ちません。

---

## 2. 主な機能 (Key Features)

1. **スタッフ本人による入力/照会**:
   - 承認済み (`APPROVED`) スタッフ本人が、自分の勤務可能な曜日・時間帯を登録・変更できます。
2. **マネージャーによる照会/修正**:
   - 店舗マネージャーは、スタッフ管理画面から各スタッフの勤務可能日を曜日単位で照会・修正できます。
3. **時間帯の単位**:
   - `MORNING` (午前) / `AFTERNOON` (午後) / `EVENING` (夜間) の3区分から、曜日ごとに複数選択可能です。

---

## 3. データ構造 (Availability Schema)

```json
{
  "monday": ["MORNING"],
  "tuesday": [],
  "wednesday": ["AFTERNOON", "EVENING"],
  "thursday": [],
  "friday": ["MORNING", "AFTERNOON", "EVENING"],
  "saturday": [],
  "sunday": []
}
```

* 各曜日フィールドの値は `MORNING` / `AFTERNOON` / `EVENING` のいずれかの文字列リスト。
* リストが空、またはフィールド自体が省略されている曜日は「勤務不可」として扱われます。

---

## 4. API 仕様 (API Specification)

### `GET /api/stores/{storeId}/staff/me/availability`

* **説明**: ログイン中のスタッフ本人の勤務可能な曜日・時間帯を取得します。
* **権限**: リクエストユーザーが当該店舗の承認済み (`APPROVED`) スタッフであること。
* **レスポンス**: `200 OK` — 上記の Availability スキーマをそのまま返却。

### `PATCH /api/stores/{storeId}/staff/me/availability`

* **説明**: ログイン中のスタッフ本人の勤務可能な曜日・時間帯を更新します。
* **権限**: `GET` と同様。
* **リクエストボディ**:
```json
{ "availability": { "monday": ["MORNING"], "tuesday": [] } }
```
* **レスポンス**: `200 OK`

### `PATCH /api/stores/{storeId}/staff/{staffId}/availability`

* **説明**: マネージャーが特定スタッフの勤務可能な曜日・時間帯を更新します。
* **権限**: リクエストユーザーが当該店舗のマネージャー (`manager`) であること。
* **リクエストボディ**: 上記と同様の `availability` オブジェクト。
* **レスポンス**: `200 OK`

### `GET /api/stores/{storeId}/staff`（既存API）

* 既存のスタッフ一覧取得APIのレスポンスに `availability` フィールドが追加され、マネージャーが一覧画面で各スタッフの勤務可能日をまとめて確認できます。

---

## 5. 今後の計画 (Roadmap)

本機能で蓄積した勤務可能時間データを入力値として、「ボタン一つでシフト表を自動生成する」機能を別途実装予定です（未実装）。
