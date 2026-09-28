# 003: SSEのheartbeat形式とゾンビ接続回収時のpanic

> 作成日: 2026-09-18
> 状態: 解決済み (Resolved) / 一部は環境側の制約として保留
> 関連ファイル: [`events/broker.go`](../../events/broker.go), [`events/waiting_user_broker.go`](../../events/waiting_user_broker.go), [`handlers/waiting_list_handler.go`](../../handlers/waiting_list_handler.go), [`fly.toml`](../../fly.toml)

---

## 現象 (Symptom)

店舗アプリ (`yoyaku_mate_provider`) で「待機を追加しても画面にすぐ反映されない」という報告があった。
待機リストはSSE (`/api/waiting-list/stream`) でリアルタイム更新される設計のため、まずサーバー側の配信を疑って調査した。

---

## 調査 (Investigation)

開発サーバー (`rusui-dev`) へ直接SSE接続して実測したところ、**配信自体は正常**だった。

```
21:55:24 data: []        ← 接続直後の初期データ
21:55:48 data: :ping     ← 30秒ごとのheartbeat
21:56:18 data: :ping
```

- fly.ioプロキシによるバッファリングは無し (接続直後に初期データが届く)
- `metrics.MetricsMiddleware` のResponseWriterラッパーは `http.Flusher` を実装済みで、Flushは効いている
- マシンは1台のみ。ブローカーがインメモリであることによる分散問題ではない
- クライアントと同じコード (Dartの`package:http`) で再現しても正常受信

配信は正しい一方で、この実測から**サーバー側の欠陥が2つ**見つかった。

---

## 原因分析 (Root Cause)

### 1. heartbeatがSSEコメントではなくデータイベントとして配信されていた

`pingAndClean` はブローカーのチャネルへ `":ping"` を流し、ハンドラはチャネルから受けた文字列を一律で
`data: %s\n\n` に包んでいた。結果として `data: :ping` という**データイベント**になっていた。

```go
// 修正前: handlers/waiting_list_handler.go
case msg := <-clientChan:
    fmt.Fprintf(w, "data: %s\n\n", h.filterStreamMessage(msg, isStaff))
```

`pingAndClean` のコメントには「SSE仕様のコメント形式（`:ping\n\n`）はクライアント側でイベントとして
受信されません」と書かれていたが、**実際の動作はその逆**だった。コードとコメントが矛盾しており、
これを読んだ人が「heartbeatはクライアントに届かない」と誤解する状態になっていた。

そのしわ寄せとして、クライアント側がそれぞれ除外処理を持つ羽目になっていた。
顧客ウェブは `yoyaku_mate/src/api/waitingService.js` で `:` 始まりを明示的に弾いており、
店舗アプリは `json.decode(":ping")` の例外を握り潰していた。

### 2. ゾンビ接続回収済みのチャネルを `RemoveClient` が再度closeしてpanicする

`RemoveClient` は「その店舗のマップが存在するか」だけを見て `close(clientChan)` を呼んでいた。

```go
// 修正前
if clients, ok := b.Clients[storeID]; ok {
    delete(clients, clientChan)
    delete(b.connectedAt, clientChan)
    close(clientChan)   // ← 既にcloseされたチャネルでもここに到達する
    ...
}
```

以下の順序で `close of closed channel` のpanicが発生する。

1. あるクライアントのチャネル (バッファ10) が埋まる = 消費が追いつかない状態
2. `pingAndClean` がそれをゾンビと判定し、`close(ch)` してマップから削除
3. **同じ店舗に他のクライアントが残っていれば `b.Clients[storeID]` は存在し続ける**
4. 該当接続のハンドラが終了し、`defer RemoveClient` が走る
5. チャネル単位の登録確認が無いため `close()` まで到達 → panic

`net/http` はハンドラのpanicをコネクション単位でrecoverするためプロセス全体は落ちないが、
スタックトレースがログに出続け、その接続は強制的に切断される。

加えて 2. の直後、ハンドラの受信側にも問題がある。

```go
case msg := <-clientChan:   // closeされたチャネルは即座にゼロ値を返し続ける
```

closeされたチャネルからの受信はブロックせず即座に空文字列を返すため、
リクエストのコンテキストがキャンセルされるまで**ループがCPUを焼き続ける**。
`shared-cpu-1x:256MB` のマシンでは他のリクエストにも影響する。

### 3. (環境側) fly.ioのマシン自動停止がインメモリブローカーと噛み合わない

`fly.toml` が `auto_stop_machines = 'stop'` + `min_machines_running = 0` になっており、
アイドル時にマシンが停止する。調査中のcurlがコールドスタートを引き起こしたログが残っている。

```
12:55:12Z runner  Machine started in 1.275s
12:55:18Z app     Server starting on :8080...
12:55:18Z proxy   machine became reachable in 6.296s
```

マシンが止まるとSSE接続とブローカーの購読者リストが丸ごと消える。
その間に発生した更新のブロードキャストは**購読者0名なので永久に失われ**、
クライアントは再接続時のスナップショットでしか追いつけない。コールドスタートは約6秒。

---

## 解決策 (Solution)

### 1. heartbeatセンチネルの導入とコメント行への変換

ゾンビ検知のためにheartbeatはチャネルを通す必要がある (チャネルが詰まっていること自体が検知条件)
ため、送出する値をセンチネルとして定義し、**SSE行への変換はハンドラの責務**に分離した。

```go
// events/broker.go
const HeartbeatMessage = ":ping"
```

```go
// handlers/waiting_list_handler.go
case msg, ok := <-clientChan:
    if !ok {
        return
    }
    if msg == events.HeartbeatMessage {
        fmt.Fprintf(w, "%s\n\n", msg)          // → ":ping\n\n" (SSEコメント)
    } else {
        fmt.Fprintf(w, "data: %s\n\n", h.filterStreamMessage(msg, isStaff))
    }
    w.(http.Flusher).Flush()
```

`HandleStream` / `HandleWaitingItemStream` の両方に適用した。

### 2. `RemoveClient` にチャネル単位の登録確認を追加

```go
clients, ok := b.Clients[storeID]
if !ok {
    return
}
if _, exists := clients[clientChan]; !exists {
    return   // pingAndCleanが既に回収済み。二重closeを避ける
}
```

`Broker` と `WaitingUserBroker` は同じ構造のため、両方に同じ修正を入れた。
あわせて受信を `msg, ok := <-clientChan` に変更し、チャネルが閉じた時点で接続を畳むようにした。

### 3. 回帰テストの追加

[`events/broker_test.go`](../../events/broker_test.go) に3件追加した。
特に `TestRemoveClientAfterZombieCleanup` は、ガードを外すと実際に
`RemoveClientがpanicした: close of closed channel` で落ちることを確認済みで、
この修正が消えたら検知できる。

### 4. fly.ioの設定は開発環境では据え置き

`auto_stop_machines` は開発環境ではコスト面の判断からそのままとし、
代わりに**クライアント側に切断検知と再接続を実装**して10秒以内に復帰できるようにした
(`yoyaku_mate_provider` の `docs/troubles/003-sse-stale-connection-recovery.md` を参照)。

本番構築時には `auto_stop_machines = 'off'` + `min_machines_running = 1` に上げる必要がある。

---

## 結果と整理 (Consequences)

- heartbeatが `:ping\n\n` のコメント行として配信されるようになり、クライアント側の除外処理が不要になった
  (既存クライアントは `:` 始まりを弾く実装のため、そのまま動作する)
- ゾンビ接続回収後のpanicとCPUスピンが解消された
- **教訓1**: keep-aliveをデータイベントとして送ると、全クライアントがその除外を実装する義務を負う。
  プロトコルの逸脱は、実装した本人ではなく利用側にコストを押し付ける形で表面化する
- **教訓2**: `close()` を伴うリソース回収経路が複数ある場合 (定期クリーンアップと `defer`)、
  「まだ自分の管理下にあるか」の確認をどちらにも入れる。片方にしか無いと、
  第三の条件 (同じキーに他のクライアントが残っている) が揃ったときだけ再現する
- **教訓3**: コードとコメントが矛盾していた箇所が、そのまま実バグだった。
  「コメント上は正しいので大丈夫」と読み飛ばさず、実際に配信されるバイト列を確認する

---

## 関連ドキュメント

- [ADR-001: リアルタイム通信にSSEを採用](../decisions/ADR-001-use-sse.md)
- [ADR-005: SSEゾンビ接続の検知と回収](../decisions/ADR-005-sse-zombie-detection.md) — 本件で修正した `pingAndClean` / `RemoveClient` の設計根拠
- [機能仕様書: 待機リスト](../features/waiting-list.md)
- [001: 開発プロセスの振り返り (Goroutineリーク、Rate Limiter調整)](./001-lessons-learned.md)
- [002: リアルタイム接続者の重複カウント防止](./002-active-user-ip-port-issue.md)
