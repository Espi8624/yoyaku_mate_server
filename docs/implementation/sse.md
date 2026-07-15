# SSE (Server-Sent Events) 実装

> 最終更新: 2026-07-10  
> 関連ファイル: [`events/broker.go`](../../events/broker.go), [`events/waiting_user_broker.go`](../../events/waiting_user_broker.go), [`handlers/waiting_list_handler.go`](../../handlers/waiting_list_handler.go)

## 概要

ポーリング（Polling）方式からSSE方式へリプレイスすることで、不要なネットワークリクエストを排除し、待機状態の変更をリアルタイムに伝達します。

**使用チャネル (2種類)**

| チャネル | エンドポイント | 購読キー | 用途 |
|------|-----------|---------|--------|
| Store Broker | `/api/waiting-list/stream` | `store_id` | 管理者アプリ / 順番号表示板 |
| User Broker | `/api/waiting-list/stream-user` | `store_id:waiting_id` | お客様用待機画面 |

---

## アーキテクチャ

```
                    ┌─────────────────────────┐
                    │         Broker          │
                    │  (in-memory singleton)  │
                    │                         │
                    │  Clients map:           │
                    │  "store_A" → [ch1, ch2] │
                    │  "store_B" → [ch3]      │
                    └────────────┬────────────┘
                                 │ Broadcast()
               ┌─────────────────┼─────────────────┐
               ▼                 ▼                  ▼
          [ch1: Admin]     [ch2: Board]        [ch3: Admin]
          goroutine        goroutine           goroutine
               │                │                   │
         SSE Response     SSE Response         SSE Response
```

---

## Broker 構造

```go
// events/broker.go
type Broker struct {
    Clients     map[string]map[chan string]bool
    connectedAt map[chan string]time.Time        // 各チャネルの接続時刻を記録（ゾンビ接続検知および平均維持時間の計算用）
    Mutex       sync.RWMutex
}
```

- **シングルトンパターン**: `sync.Once` によりインスタンスを1つのみ維持し、初期化時に `startHeartbeat()` バックグラウンドゴルーチンを実行
- **RWMutex**: 読み取り(Broadcast、GetStats)は並行処理を許可、書き込み(Add/Remove、pingAndClean)は排他制御
- **チャネルバッファ**: `make(chan string, 10)` — スロークライアントによるブロックを防止
- **Heartbeat & ゾンビクリーンアップ**: 30秒周期で送信を試み、ブロックされたチャネル（ゾンビ接続）を検知して自動で `close` クリーンアップ

---

## 接続フロー

```go
// handlers/waiting_list_handler.go - HandleWaitingListStream()

// 1. SSEヘッダー設定
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")

// 2. クライアント登録
clientChan := make(chan string, 10)
broker.AddClient(storeID, clientChan)
defer broker.RemoveClient(storeID, clientChan)  // 接続終了時に自動クリーンアップ

// 3. 初期データの送信 (接続直後)
go func() {
    waitingList, _ := data.GetWaitingListData(storeID)
    jsonData, _ := json.Marshal(waitingList)
    broker.Broadcast(storeID, string(jsonData))
}()

// 4. イベントループ
for {
    select {
    case <-r.Context().Done():
        // クライアント接続終了 → SSE_DISCONNECT エラーログを記録
        metrics.GetTracker().RecordError(models.ErrorLog{
            Timestamp: time.Now().UTC(),
            ErrorType: "SSE_DISCONNECT",
            Message:   "SSE Client Disconnected",
            Path:      r.URL.Path,
            Method:    r.Method,
            ClientIP:  r.RemoteAddr,
        })
        return
    case msg := <-clientChan:
        fmt.Fprintf(w, "data: %s\n\n", msg)
        w.(http.Flusher).Flush()
    }
}
```

---

## ブロードキャストのトリガー

待機列に変更が発生するすべての箇所において、 `notifyStore(storeID)` を呼び出します。

```
notifyStore(storeID)  ──→  Broker.Broadcast()
                            │
                            └──→  notifyWaitingUsers()
                                   (個別のお客様チャネル)
```

> **100msの遅延**: 待機登録直後のブロードキャストにおいて、MongoDBから新規データが即座に取得できない問題を防止するため、登録後に100ms待機してから非同期でブロードキャストを行います。

```go
go func() {
    time.Sleep(100 * time.Millisecond)
    notifyStore(newWaiting.StoreID)
}()
```

---

## スロークライアントへの対応

チャネルが一杯になっている場合、該当クライアントへの送信をスキップ(skip)し、サーバー全体がブロックされる状況を防止します。

```go
// events/broker.go - Broadcast()
select {
case clientChan <- message:
default:
    // チャネルブロック → 該当クライアントをスキップ (他のクライアントへの影響なし)
}
```

---

## SSEデータフォーマット

```
data: {"store_id":"abc","waiting_id":"...","queue_number":5,...}\n\n
```

標準SSEフォーマット (`data: <payload>\n\n`)を使用し、独自のイベントタイプ(`event:`)は現在使用していません。

---

## ポーリング方式のレガシーエンドポイント

`GET /api/waiting-list/poll` — SSEをサポートしていない環境向けレガシーエンドポイントで、内部的に同一の `GetWaitingListData` を呼び出します。

---

## 関連ドキュメント

- [待機列機能仕様](../features/waiting-list.md)
- [技術選定根拠 (SSE vs WebSocket)](../decisions/ADR-001-use-sse.md)
