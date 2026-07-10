# 待機列機能 (Waiting List)

> 最終更新: 2026-07-10

## 概要

お客様がQRコードをスキャンして整理券を発行し、リアルタイムで待機順を確認するコア機能です。  
店舗オーナー/スタッフはアプリを通じて待機状態を管理し、SSEを通じてすべてのクライアントに変更事項がリアルタイムで伝達されます.

---

## 主要概念

| 概念 | 説明 |
|------|------|
| `waiting_id` | クライアントが生成するUUID形式の一意の識別子。冪等性キーとして活用 |
| `queue_number` | 営業日単位で発行される連番 (Atomic Counterベース) |
| `source` | 登録ルート — `"web"` (QRスキャン), `"app"` (スタッフ直接登録) |
| `business_day` | 営業時間基準の日付境界 (Dynamic Cutoff)。深夜営業をサポート |

---

## 状態遷移図

```
waiting ──→ notified ──→ completed
    │                        
    └──→ cancelled  (お客様直接 / スタッフ操作)
    └──→ no_show    (営業日経過時に自動的に失効)
```

### 状態説明

| 状態 | 説明 | called_time | entry_time |
|------|------|:-----------:|:----------:|
| `waiting` | 待機中 | - | - |
| `notified` | 呼び出し済み | Y | - |
| `completed` | 入店完了 | Y | |
| `cancelled` | キャンセル | - | - |
| `no_show` | 自動失効 (営業日経過) | - | - |

---

## APIエンドポイント

### `GET /api/waiting-list`

現在の営業日の待機列全体の照会。

**Query Parameters**

| パラメータ | 必須 | 説明 |
|---------|:----:|------|
| `store_id` | Y | 店舗ID |
| `action=average_waiting_time` | - | 平均待ち時間照会モード |
| `action=qr_token` | - | 当日QRトークン発行モード |

**Response (待機列照会)**
```json
[
  {
    "id": "...",
    "store_id": "abc123",
    "waiting_id": "20260710-120000-001",
    "queue_number": 5,
    "party_size": 2,
    "nationality": "JP",
    "registration_time": "2026-07-10T12:00:00.000+09:00",
    "status": "waiting",
    "estimated_wait_time": 40,
    "menu_items": [],
    "source": "web"
  }
]
```

---

### `POST /api/waiting-list`

新規待機登録。

**Query Parameters**

| パラメータ | 必須 | 説明 |
|---------|:----:|------|
| `v_token` | Y | 当日HMACベースのQRトークン (偽造防止) |

**Request Body**
```json
{
  "store_id": "abc123",
  "waiting_id": "client-generated-uuid",
  "party_size": 2,
  "nationality": "JP",
  "contact": "090-1234-5678",
  "menu_items": [
    { "menu_id": "m1", "name": "ラーメン", "quantity": 2 }
  ]
}
```

**Authorization (任意)**
- ヘッダーなし: お客様 (QRスキャン)として処理, `source = "web"`
- `Authorization: Bearer <firebase_token>` + `X-Login-Token: <session_token>`: スタッフ登録, `source = "app"`

**ビジネスルール**
- `v_token` HMAC検証 → 失敗時は `403`
- `party_size`が店舗設定の `max_waiting_count` を超過した場合は `400`
- `enable_menu_selection = true`の場合、 `menu_items` は必須
- `require_one_menu_per_person = true`の場合、メニュー数量の合計 ≥ party_size
- 店舗の `license_status` が `APPROVED` でない場合は `403`
- `waiting_id` が既に存在する場合、既存のデータをそのまま返却 (冪等性の保証)

---

### `PATCH /api/waiting-list?action=status`

待機状態の変更。

**Request Body**
```json
{
  "store_id": "abc123",
  "waiting_id": "...",
  "status": "notified"
}
```

**権限**
- `status = "cancelled"`: 認証不要 (お客様自身でのキャンセルを許可)
- その他: Firebase Auth + X-Login-Token + 店舗権限が必須

---

### `POST /api/waiting-list?action=clear`

営業日の待機列全体の初期化 (`waiting` 状態のすべてを `cancelled` に変更)。

**権限**: Firebase Auth + X-Login-Token + 店舗権限が必須

---

### `GET /api/waiting-list/stream`

店舗の待機列全体のリアルタイムストリーム (SSE)。

**Query Parameters**: `store_id`

接続と同時に現在の待機列データを初期値として送信し、以降変更が発生した際に自動的にブロードキャストされます。  
→ [SSE実装詳細](../implementation/sse.md)

---

### `GET /api/waiting-list/stream-user`

個別のお客様のリアルタイム待機状態ストリーム (SSE)。

**Query Parameters**: `store_id`, `waiting_id`

**Response 含まれるフィールド (WaitingUserResponse)**
```json
{
  "...waiting_list_fields": "...",
  "waiting_count": 3,
  "estimated_waiting_time": "30 mins"
}
```

- `waiting_count`: 全体の有効な待機数 (`waiting` + `notified` 状態の合算)
- `estimated_waiting_time`: 自身より **前のグループ数** × `estimated_wait_time` 設定値 (自身のqueue順序インデックス基準)

---

### `GET /api/waiting-list/poll`

ポーリング方式の待機列照会 (SSE非対応環境向けのレガシーエンドポイント)。

---

## 主要ロジック

### QRトークン検証

当日の日付 + store_id を HMACで署名したトークンで、QRコードの偽造を防止します。  
Dynamic Cutoffベースの営業日を基準にトークンの有効性を判断します。  
→ [詳細: utils/hmac.go]

### Dynamic Business Day Cutoff

`GetBusinessDayCutoff(storeID, now)` 関数は、店舗の営業時間設定に基づいて「本日の営業日」を計算します。

| 条件 | Cutoff |
|------|--------|
| 24時間営業 | `reset_time` (デフォルト 06:00) |
| 通常営業 | `start_time - 1h` |
| 設定なし | 04:00 AM |

深夜営業（例：23:00開店）の場合、前日と本日のデータを正しくグループ化する役割を果たします。

### 予想待ち時間の計算

```
estimated_wait_time = 自身より前のグループ数 × minutesPerTeam
```

- `minutesPerTeam`: 店舗設定 `estimated_wait_time` (デフォルト値: 10分)
- N+1クエリ防止のため、設定はループ外で1回のみ取得
- 将来的にはAIベースのロジックに代替予定 (`CalculateEstimatedWaitTime` 関数の差し替え)

### 自動失効 (Auto Expire)

`GetWaitingListData` 呼び出し時に自動的に `AutoExpireWaitingItems` が実行され、  
現在の営業日 Cutoff 以前に登録された `waiting`/`notified` 状態の項目を `no_show` に変更します。

---

## 関連ドキュメント

- [冪等性実装](../implementation/idempotency.md)
- [SSE構造](../implementation/sse.md)
- [Atomic Counter](../implementation/atomic-counter.md)
