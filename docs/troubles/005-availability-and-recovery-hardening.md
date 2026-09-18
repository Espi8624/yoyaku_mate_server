# 005: 可用性と復旧経路の点検で見つかった4件

> 作成日: 2026-09-18
> 状態: 解決済み (Resolved)
> 関連ファイル: [`handlers/translation_handler.go`](../../handlers/translation_handler.go), [`handlers/middleware.go`](../../handlers/middleware.go), [`db/mongo.go`](../../db/mongo.go), [`main.go`](../../main.go), [`fly.toml`](../../fly.toml)

---

## 経緯

[004](./004-production-readiness-hardening.md) がセキュリティと情報漏洩を中心とした点検だったのに対し、
今回は**「ネットワークや依存先が落ちたとき、サービスがどう振る舞い、どう戻るか」**という観点で点検した。

いずれも障害報告があったわけではなく、先んじて確認した結果である。
以下は全て実際のコードと `flyctl` の出力で確認したもので、推測は含まない。

前提として、この構成は**マシン1台・shared-cpu-1x・256MB**である
(`flyctl scale show -a rusui-dev`)。SSEブローカーがインメモリのため
スケールアウトできない ([ADR-001](../decisions/ADR-001-use-sse.md)) 以上、
1台が詰まる = サービス全体が止まる、という前提で各項目を評価している。

---

## 1. Gemini API呼び出しにタイムアウトが無かった

### 現象

翻訳とチャットボットが共有するヘルパー `callGeminiForText` が `http.Post` を使っていた。

```go
resp, err := http.Post(geminiURL, "application/json", bytes.NewBuffer(reqBody))  // 修正前
```

`http.Post` は `http.DefaultClient` を使い、**`Timeout` はゼロ値 = 無制限**である。
Geminiが応答を返さない限り、このリクエストはゴルーチンとfly.ioプロキシの接続枠を
掴んだまま永久に解放されない。

呼び出し元の一方 `/api/public/ai-chat` は**無認証の公開エンドポイント**であり、
`store_id` を知っていれば誰でも叩ける。マシン1台構成では、これが詰まる = サービス全体が止まる。

なお同じリポジトリ内でも、Slack通知ワーカーは
[`metrics/alert_worker.go`](../../metrics/alert_worker.go) で
`&http.Client{Timeout: 5 * time.Second}` を正しく設定していた。ここだけが抜けていた。

### 解決策

専用クライアントを持たせ、呼び出し元のリクエストコンテキストを繋ぐ。

```go
var geminiClient = &http.Client{Timeout: geminiTimeout}  // 15秒

func callGeminiForText(ctx context.Context, prompt string) (string, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, geminiURL, bytes.NewReader(reqBody))
    ...
    resp, err := geminiClient.Do(req)
```

設計上の判断が3つある。

- **クライアントはパッケージレベルで1つだけ持つ。** リクエストごとに生成すると
  コネクションプールが共有されず、毎回TCP+TLSハンドシェイクからやり直しになる
- **`r.Context()` を渡す。** 客がチャット画面を閉じた時点でGemini呼び出しも中断される。
  タイムアウトだけでは「誰も待っていない応答」を15秒間待ち続けることになる
- **15秒。** 両方の呼び出し経路とも `ThinkingBudget: 0` (推論無効) のため通常は数秒で返る。
  「遅いが正常」を切り捨てない範囲での上限として選んだ

### ついでに直したもの: APIキーがクエリパラメータに載っていた

```go
// 修正前: ?key=<APIキー> をURLに含めていた
geminiURL := fmt.Sprintf("...:generateContent?key=%s", apiKey)
```

`net/http` の通信エラーは `*url.Error` として**URL全体をメッセージに含む**。
つまりタイムアウトを入れると、タイムアウト1回ごとに
`failed to reach AI service: Post "https://...?key=AIza..."` としてAPIキーがログに流れ出る。
タイムアウトを入れなければ起きなかった漏洩を、入れたことで作り込むところだった。

`x-goog-api-key` ヘッダに移すことで、エラーメッセージにキーが載る経路そのものを断った。

エラー応答の読み取りも `io.LimitReader` で8KiBに制限した。原因特定にはそれで足りる。

---

## 2. fly.ioにヘルスチェックが設定されていなかった

### 現象

```
$ flyctl status -a rusui-dev
 PROCESS │ ID             │ VERSION │ REGION │ STATE   │ ROLE │ CHECKS
 app     │ d8d4017a6e3278 │ 63      │ nrt    │ stopped │      │
                                                              ^^^^^^^ 空
```

[`handlers/health_handler.go`](../../handlers/health_handler.go) は
MongoDBへのPingまで確認する `/health` を提供していたが、**fly.io側がそれを見ていなかった。**

結果として2つの穴があった。

- DBに繋がっていないマシンにもプロキシがトラフィックを流し続ける
- デプロイ時の検証が効かない。壊れたリリースがそのまま出る

### 解決策

```toml
[[http_service.checks]]
  interval = '30s'
  timeout = '5s'
  grace_period = '40s'
  method = 'GET'
  path = '/health'
```

`grace_period` を40秒としたのは、起動シーケンス
(Infisicalからのシークレット取得 → MongoDB接続、失敗時は5秒間隔で最大5回リトライ)
を跨げる長さが必要なため。短すぎると正常に起動中のマシンをunhealthyと判定し続ける。

マシンレベルのチェックはプロキシのリクエストとして集計されないため、
`auto_stop_machines` の停止判定には影響しない。

---

## 3. MongoDBに繋がらないまま起動し、二度と復旧しなかった

### 現象

`main.go` は起動時の接続失敗をログに落とすだけだった。

```go
if err := db.InitMongoDB(cfg.MongoDB.URI); err != nil {
    log.Printf("MongoDB初期化失敗: %v", err)   // 修正前: ここで終わり
}
```

これで起きることが2つある。

**(a) nil参照でpanicする。** `db.GetCollection` は未接続時にnilを返すが、
リポジトリ層はそれをnilチェックせずそのまま使う。

```go
collection := db.GetCollection(DatabaseName, CollectionWaitingList)
cursor, err := collection.Find(ctx, filter)   // collection == nil → panic
```

該当する呼び出しは**109か所**あり、`db.MongoClient.StartSession()` を直接呼ぶ箇所も2つあった。

`net/http` はハンドラ内のpanicをrecoverするが、**応答を一切書かずに接続を閉じる**。
客から見ると503ですらなく、ただの通信エラーになる。

**(b) 復旧しない。** ドライバの自動再接続はクライアントが生成済みの場合の話であって、
`mongo.Connect` 自体に失敗した場合はクライアントがnilのまま残る。
Atlasが復旧しても、**再デプロイするまでサービスは戻らない。**

### 解決策

3つに分けて対処した。

**プロセスは落とさない。** `log.Fatal` にする案は採らなかった。マシン1台構成では
Atlasが戻るまでクラッシュループに入り、再起動のたびに起動シーケンス
(シークレット取得を含む) をやり直すことになる。
503を返しながら待ち、復旧した瞬間に自力で戻る方が復帰は早い。

**入口の1か所で止める。** `RequireDatabaseMiddleware` を追加し、未接続時は
`/api` 配下を503で返す。109か所にnilチェックを足すのは現実的でない。

```go
if !db.IsReady() {
    w.Header().Set("Retry-After", "30")
    utils.RespondWithError(w, "Database is temporarily unavailable", http.StatusServiceUnavailable)
    return
}
```

配置には3つの制約がある。

- **認証ミドルウェアより外側。** 認証自体がユーザー照会でDBを引くため、
  内側に置くと認証の時点でpanicする
- **CORSミドルウェアより内側。** 外側だと503応答にCORSヘッダが付かず、
  ブラウザには本来の503ではなく原因不明のCORSエラーとして表示される
- **ルーターではなくmain.goのハンドラチェーンに掛ける。** ルーター(`RegisterRoutes`)に
  掛けると、DBを持たないテスト環境では全ルートが503になり、認証・セッション保護を
  検証している [`router_session_test.go`](../../handlers/router_session_test.go) が
  素通りしてしまう。実際に一度そう実装してテストが落ち、それで気づいた

**再接続ワーカーを置く。** `db.StartReconnectWatcher()` が30秒ごとに接続を試み続ける。

ここで扱うのは**「一度も繋がっていない」状態だけ**である。
接続確立後の一時的な切断はドライバ自身がトポロジを監視して復旧するため、
こちらが手を出すと健全なコネクションプールを壊すことになる。

同じ理由で `IsReady()` は**疎通ではなくクライアントの存在**だけを見る。
pingの失敗でfalseにすると、ドライバが自力で復旧できる一時的な切断のたびに
健全なリクエストまで巻き添えで503にしてしまう。
実際の疎通確認は `/health` の役割として分けてある。

**付随して**、公開変数 `db.MongoClient` を非公開にし `db.Client()` 経由に変えた。
再接続ワーカーが書き、全ハンドラが読むようになるため、そのままではデータ競合になる。
`go test -race` で確認済み。

### 検証中に見つかった: メトリクスワーカーが5秒ごとにpanicしていた

MongoDBに到達できないURIで実際に起動して確認したところ、
**ミドルウェアでは塞げない経路**が残っていた。

```
[safego] ゴルーチン metrics_error_batch がpanicしました: runtime error: invalid memory address or nil pointer dereference
	.../metrics/tracker.go:95
[safego] metrics_error_batch: 5s後に再開します
```

`ErrorTracker.flush` が `db.GetCollection` の戻り値をnilチェックせず `InsertMany` を呼んでいた。
`utils.GoForever` が復帰させるためプロセスは死なないが、
**fly.ioのログに5秒おきにスタックトレースが積み上がり、本当の障害原因が埋もれる。**
これはHTTPリクエストの経路ではないため、`RequireDatabaseMiddleware` では防げない。

`RequestTracker` と `AuditTracker` にはnilチェックがあったが、
**バッファを空にした後**に置かれていた。つまりpanicはしないが、
DBが落ちている間のログは復旧しても戻らず捨てられていた。

3つとも `skipFlushWhileDBDown` による判定を**バッファを取り出す前**に置いた。
溜めたまま復旧を待てるようになる。バッファはいずれも1,000件で頭打ちになるため、
待っている間にメモリを食い潰すことはない。

**最初の修正は問題を置き換えただけだった。** 見送りを毎回ログに残すようにしたところ、
ワーカー3つ × 5秒周期で分あたり数十行が積み上がった。スタックトレースがメッセージに
変わっただけで、本当の原因が埋もれることは何も変わっていない。
状態が変わったときだけ記録するように直した。

```go
if !dbDownLogged.Swap(true) {
    log.Println("MongoDB未接続のため、メトリクスのバッチ保存を見送ります ...")
}
```

この抑制はテストでしか守られない (`metrics/db_down_test.go`)。外れれば元の状態に戻る。

---

## 4. ハンドラのpanicが500として記録されていなかった

### 現象

[004](./004-production-readiness-hardening.md) でバックグラウンドゴルーチンには
panic保護を入れたが、**ハンドラ本体は `net/http` 既定のrecoverに任せたままだった。**

既定のrecoverは応答を書かずに接続を閉じる。そのため:

- クライアントからは500ではなく通信エラーに見え、原因の切り分けができない
- `MetricsMiddleware` はステータスコードを観測できず、
  **エラーダッシュボードに一切計上されない。障害が数字に現れない**

### 解決策

`RecoverMiddleware` を追加し、`MetricsMiddleware` の**内側**に置いた。
ここで書いた500を外側のメトリクスが拾い、ダッシュボードに反映される。

最終的なチェーンは以下になる。

```
レートリミット → メトリクス → panic復帰 → CORS → DB準備確認 → ルーター
```

実装上の注意点が3つある。

- **応答開始後は上書きしない。** SSEはヘッダ送出とFlushを済ませてから配信を続ける。
  そこに500を被せても応答は変わらず `superfluous response.WriteHeader` のログが出るだけ。
  ヘッダ送出済みかを記録しておき、その場合は応答を終えるに留める (クライアントは再接続で復帰する)
- **ラッパーは `http.Flusher` を実装する。** SSEハンドラは `w.(http.Flusher)` で取り出すため、
  実装していないとSSE接続が確立した瞬間にpanicする。
  `Unwrap()` も用意し、`http.ResponseController` から元の機能に到達できるようにした
- **`http.ErrAbortHandler` は握り潰さない。** net/httpの規約上「意図的な中断」の合図であり、
  異常ではない。500に変換すると意図した中断が障害として記録されてしまう

スタックトレースはログのみに出し、応答本文には含めない。内部構造が外へ漏れる。

---

## 検証

`handlers/resilience_middleware_test.go` を追加した。
**障害時の振る舞いは平常時のテストでは一切踏まれない**ため、ここが唯一の防波堤になる。

- panicが500になること / 正常応答に干渉しないこと
- 応答開始後のpanicが配信済み本文を壊さないこと (Flusher実装の確認を兼ねる)
- `ErrAbortHandler` が素通りすること
- DB未接続時に `/api` が503 + `Retry-After` + `X-Error-Type: DATABASE_ERROR` を返すこと
- `/health` とCORSプリフライトが対象外であること

加えて、MongoDBに到達できないURIを与えて実際に起動し、以下を確認した。

- 起動はするが `/health` が503を返す
- `/api/*` がpanicせず503を返す
- MongoDBが復帰すると再デプロイなしにサービスが戻る

---

## 残っている課題

この点検では扱わなかったが、同じ観点で確認済みの項目を記録しておく。

| 項目 | 内容 |
|---|---|
| Infisicalへの起動時依存 | [`Dockerfile`](../../Dockerfile) のCMDがマシン起動のたびに `app.infisical.com` を叩く。`min_machines_running = 0` のためこれは頻繁に起きる。さらに `curl \| jq` はcurlの失敗を握り潰す ([004](./004-production-readiness-hardening.md) と同じ罠) |
| 冪等性にユニーク制約が無い | `idx_store_waiting_id` が非ユニークのため、[idempotency](../implementation/idempotency.md) の「照会してからinsert」が同時実行で二重登録を許す |
| リクエストボディのサイズ無制限 | `MaxBytesReader` の使用箇所が0件。256MBのマシンでJSONを無制限にデコードしている |
| fly.ioプロキシの同時実行数が未設定 | `[http_service.concurrency]` が無い。SSEは接続を保持し続けるため、この値が実質の同時接続上限になる |
| SSE初期データがBroadcastされている | 1人が接続するたびに同一店舗の全接続へ全件が再送される |
| 正常切断がエラーとして記録される | `SSE_DISCONNECT` が `error_logs` に入るため、エラー率ベースのアラートが機能しにくい |
