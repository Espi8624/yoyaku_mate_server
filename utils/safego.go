package utils

import (
	"log"
	"runtime/debug"
	"time"
)

// net/http はハンドラ内のpanicをコネクション単位でrecoverするが、
// ハンドラの外で起動したゴルーチンは保護されない。そこでpanicが起きると
// プロセス全体が落ち、全店舗のSSE接続とメトリクスのバッファが同時に失われる。
//
// バックグラウンドで動かすものは、必ずこのパッケージの Go / GoForever を通すこと。

// panicRestartDelay はGoForeverがpanic後に再開するまでの待機時間。
// panicの原因が解消していない場合にログを溢れさせないための間隔。
// テストから短縮するため変数にしている (実行時に変更してはならない)
var panicRestartDelay = 5 * time.Second

// Go は一度きりの処理をpanic保護付きで実行する。
// panicしても呼び出し元のプロセスは落とさず、ログに記録して終了する。
//
// 繰り返し実行が必要なワーカーには GoForever を使うこと
func Go(name string, fn func()) {
	go func() {
		defer recoverAndLog(name)
		fn()
	}()
}

// GoForever は終了しないワーカー(heartbeat、バッチフラッシュ等)を
// panic保護付きで実行し、panicした場合は待機してからループを再開する。
//
// - recoverして終了するだけでは、ワーカーが静かに止まったまま
//   プロセスは生き続けることになる。heartbeatが止まればゾンビ接続の
//   回収が効かなくなり、メトリクスのフラッシュが止まれば記録が欠落するが、
//   どちらもプロセス死と違って外から気づけない。落ちるより発見が遅れる分たちが悪い
func GoForever(name string, fn func()) {
	go func() {
		for {
			if completed := runGuarded(name, fn); completed {
				// - panicせずに戻った = 意図した終了とみなす
				return
			}
			log.Printf("[safego] %s: %v後に再開します", name, panicRestartDelay)
			time.Sleep(panicRestartDelay)
		}
	}()
}

// runGuarded は fn を実行し、panicせず正常に戻ったかどうかを返す
func runGuarded(name string, fn func()) (completed bool) {
	defer func() {
		if r := recover(); r != nil {
			logPanic(name, r)
			completed = false
		}
	}()

	fn()
	return true
}

func recoverAndLog(name string) {
	if r := recover(); r != nil {
		logPanic(name, r)
	}
}

func logPanic(name string, r any) {
	log.Printf("[safego] ゴルーチン %s がpanicしました: %v\n%s", name, r, debug.Stack())
}
