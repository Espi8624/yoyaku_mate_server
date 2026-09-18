# 004: 本番運用に向けた点検で見つかった4件

> 作成日: 2026-09-18
> 状態: コード側は解決済み (Resolved) / 認証情報のローテーションは未実施
> 関連ファイル: [`db/mongo.go`](../../db/mongo.go), [`utils/safego.go`](../../utils/safego.go), [`handlers/waiting_list_handler.go`](../../handlers/waiting_list_handler.go), [`main.go`](../../main.go)

---

## 経緯

[003](./003-sse-heartbeat-and-zombie-cleanup.md) のSSE調査が一段落したあと、
「本番で問題になりそうな箇所が他に無いか」という観点でサーバーを点検した。
症状の報告があったわけではなく、先回りの確認から出てきた4件である。

以下はすべて実際のコードとfly.ioのログで確認したもので、推測は含まない。

---

## 1. MongoDB認証情報がログに平文で残っていた

### 現象

`InitMongoDB` が接続文字列をそのままログへ出していた。

```go
log.Printf("Try mongoDB connect: %s", uri)   // 修正前
```

ローカルの標準出力に出るだけと認識されていたが、`flyctl logs -a rusui-dev` で
**開発サーバーのログにも同じ行が蓄積している**ことを確認した。
ユーザー名・パスワード・クラスタホスト・DB名がすべて平文で含まれる。

fly.ioのログは保管され、アプリの閲覧権限があれば誰でも読める。
認証情報の置き場所としては最悪に近い。

### 解決策

認証情報を伏せ、接続先の特定に必要なスキーム・ホスト・DB名だけを出す。

```go
masked := *parsed
if parsed.User != nil {
    masked.User = url.User("REDACTED")
}
masked.RawQuery = ""
```

設計上の判断が2つある。

- **解析に失敗したら URI を一切出さない。** 原文へフォールバックすると
  「伏せたつもりが出ている」という最も危険な状態になる
- **伏せ字は記号ではなく英字を使う。** `"***"` は `url.User` によって
  パーセントエンコードされ `%2A%2A%2A` になり、ログが読みづらい

### 未完了の作業

**コードの修正だけでは終わらない。** 既にログへ出てしまった認証情報は
無効化されないため、Atlas側でのローテーションが別途必要になる。

```
① Atlasで新しいDBユーザーを作成  →  ② InfisicalのMONGODB_URIを更新
→  ③ 再デプロイして接続を確認     →  ④ 旧ユーザーを削除
```

順序を入れ替えるとダウンタイムが発生する。④を先に行うとサーバーは即座に停止する。

---

## 2. バックグラウンドゴルーチンにpanic保護が無かった

### 現象

リポジトリ全体で `recover()` の使用箇所が **ゼロ** だった。

`net/http` はハンドラ内のpanicをコネクション単位でrecoverするが、
**ハンドラの外で起動したゴルーチンは保護されない**。該当するものが11個あり、
どれか1つがpanicすればプロセスごと落ちる。
全店舗のSSE接続とメトリクスのバッファが同時に失われることになる。

見落としやすいのは、**ハンドラの中に書かれていてもrecoverの範囲外**である点。

```go
func (h *WaitingListHandler) HandleStream(...) {   // ← ここはrecoverされる
    go func() {                                     // ← ここはされない
        ...
    }()
}
```

`waiting_list_handler.go` のSSE初期データ送信と待機作成後の通知が
まさにこの形だった。

### 解決策

`utils.Go` / `utils.GoForever` を追加し、全ての起動箇所をこれに通した。

**`GoForever` が単なるrecoverではなく「再開」する理由**が重要である。
recoverして終了するだけだと、ワーカーが静かに止まったままプロセスは生き続ける。
heartbeatが止まればゾンビ接続の回収が永久に効かなくなるが、
プロセス死と違って外から気づけない。**落ちるより発見が遅れる分たちが悪い。**

再開間隔は、panicの原因が解消していない場合にログを溢れさせないため5秒空ける。

---

## 3. 公開SSEから他の客のnotesが配信されていた

### 現象

`/waiting-list/stream` と `/waiting-list/poll` は無認証の公開エンドポイントで、
`store_id` さえ分かれば誰でも購読できる。`store_id` はQRのURLとモニターボードの
URLに含まれるため、**一度QRを読んだ客はその店舗の待機リストを恒久的に購読できる**。

伏せていたのは `contact` のみで、`notes`(客が自由記述した要望)と
`menu_items`(注文内容)はそのまま配信されていた。
特に自由記述は何が書かれるか制御できない。

### 解決策

`redactContacts` を `redactForPublic` へ改名し、除去対象を広げた。

除去範囲の線引きには根拠がある。

- 唯一の公開購読者であるモニターボード
  (`yoyaku_mate/src/containers/board/Board.jsx`)は
  `status` / `queue_number` / `waiting_id` **3つしか参照していない**。
  呼び出し箇所を確認したうえで落としているため表示は壊れない
- `party_size` と `nationality` は残す。個人を特定する情報ではなく、
  除去範囲を広げるほど未把握の利用箇所を壊すリスクが上がる
- スタッフ端末は `X-Session-Id` を送るため従来どおり全項目を受け取る
  (このヘッダが付くようになった経緯は `yoyaku_mate_provider` の
  `docs/troubles/003-sse-stale-connection-recovery.md` を参照)

---

## 4. HTTPサーバーにタイムアウトが1つも無かった

### 現象

`http.ListenAndServe` を直接呼んでおり、`ReadHeaderTimeout` も `IdleTimeout` も
未設定だった。既定値は無制限のため、ヘッダを少しずつ送り続けるだけで接続を
占有できる(Slowloris)。`shared-cpu-1x:256MB` では特に効きやすい。

### 解決策

`http.Server` を明示的に組み立て、あわせてgraceful shutdownも用意した。

```go
ReadHeaderTimeout: 10 * time.Second    // Slowlorisを止めるのはこの一点で足りる
IdleTimeout:      120 * time.Second    // keep-alive待機中の接続を回収
// ReadTimeout / WriteTimeout は設定しない
```

### この設定を変更する人への注意

**`WriteTimeout` を設定してはならない。** 入れるとその時間ごとに
全てのSSE接続が応答の途中で強制切断される。
`IdleTimeout` はリクエストとリクエストの「間」にのみ適用され、
ハンドラ実行中には関与しないためSSEに影響しない
(ローカルで200秒の接続維持を実測して確認済み)。

**Shutdownの猶予を延ばしてはならない。** 3秒にしているのには理由がある。

- SSE接続は自分から終了しないため、`Shutdown` は**必ず猶予を使い切る**
- 一方 fly.io は SIGTERM の5秒後(`kill_timeout` の既定値)に SIGKILL を送る
- 猶予をそれより長く取ると毎回 SIGKILL が先に届き、
  その後の `metrics.FlushAll()` に到達しない = 監査ログが毎回失われる

猶予を延ばしたい場合は、`fly.toml` の `kill_timeout` を先に上げること。
実測では終了まで3秒で、`FlushAll` まで到達している。

猶予切れ後は `srv.Close()` で残りのSSE接続を明示的に閉じる。
クライアントは再接続で復帰する。

---

## 点検して問題が無かった箇所

先回りの点検は「大丈夫だった範囲」も記録しておかないと、同じ場所を繰り返し調べることになる。

- **メトリクスのバッファ**: 1000件の上限があり、スパイク時もOOMしない
- **型アサーション**: 全て `switch .(type)` または `, ok` でガードされている
- **シークレットのコミット**: 履歴上も無し
  (`config.development.json` は追跡されているが認証情報を含まない)
- **`config/config.go` のログ**: 環境変数の「名前」だけを出しており値は出さない

---

## 教訓

- **「ローカルだけ」という前提は確認しないと崩れる。** 認証情報のログは
  ローカル開発中の話として保留されていたが、実際にはリモートの開発サーバーでも
  同じコードが動いていた。保留の判断そのものは妥当でも、前提が誤っていた
- **panicの影響範囲は「どこに書かれているか」ではなく「どのゴルーチンか」で決まる。**
  ハンドラの中の `go func(){}` は保護されない
- **タイムアウトは「長めに設定しておけば安全」ではない。** 上流(fly.ioの
  `kill_timeout`)より長い猶予は、猶予が無いのと同じ結果になる

---

## 関連ドキュメント

- [003: SSEのheartbeat形式とゾンビ接続回収時のpanic](./003-sse-heartbeat-and-zombie-cleanup.md)
- [ADR-001: リアルタイム通信にSSEを採用](../decisions/ADR-001-use-sse.md)
- [ADR-005: SSEゾンビ接続の検知と回収](../decisions/ADR-005-sse-zombie-detection.md)
- [001: 開発プロセスの振り返り](./001-lessons-learned.md)
