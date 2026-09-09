# 機能仕様書: 店舗業種タグ (Store Business Category)

本文書は、`yoyaku_mate_server` に実装された店舗の業種タグ (Business Category) 機能仕様を定義します。

> 作成日: 2026-09-09

---

## 1. 概要 (Overview)

店舗 (`store_info`) に、その店舗がどの業種に属するかを表す必須タグ `business_category` を追加しました。店舗登録時に必ず選択する必要があり、登録後も店舗設定画面から変更できます。

---

## 2. 許可された値 (Allowed Values)

固定された5種類の文字列のいずれかのみ許可されます (`models.IsValidStoreCategory` で検証)。

| 値 | 意味 |
|---|---|
| `RESTAURANT` | 飲食店 |
| `CAFE_DESSERT` | カフェ・デザート |
| `BEAUTY` | 美容室・ビューティー |
| `RETAIL` | 小売業 |
| `OTHER` | その他 |

---

## 3. API 仕様 (API Specification)

### `POST /api/auth/signup`, `POST /stores/add`

* 店舗を新規作成する場合、リクエストボディに `business_category` (string) が必須。
* 未指定または空文字の場合: `400 Bad Request` (`"business category is required"`)。
* 許可されていない値の場合: `400 Bad Request` (`"Invalid business category"` / `"invalid business category"`)。

### `PUT /api/provider_store?store_id=xxx`

* リクエストボディに `business_category` を含めることで変更可能 (`allowedStoreUpdateFields` に登録済み)。
* 含まれる場合のみ検証: 文字列でない、または許可された5値以外の場合は `400 Bad Request` (`"Invalid business_category"`)。
* 含まれない場合は既存値を維持 (他の項目のみの部分更新が可能)。

### `GET /api/provider_store`, `GET /api/admin/stores?status=xxx`

* レスポンスの店舗オブジェクトに `business_category` フィールドが含まれる。

---

## 4. 関連画面

* `yoyaku_mate_provider`: 店舗登録ウィザードの必須ステップ、および 設定 → 店舗 → 基本情報 の編集項目。
* `yoyaku_mate_admin`: 出店審査 (`StoreApprovalPage`) の一覧・詳細モーダルに参考情報として表示。
