package utils

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestGoRecoversPanic は一度きりの処理がpanicしてもプロセスが落ちないことを検証する。
// テストプロセス自体が生き残ること自体が検証になっている (recoverが無ければテストバイナリごと落ちる)
func TestGoRecoversPanic(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	Go("test_panic", func() {
		defer wg.Done()
		panic("意図的なpanic")
	})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("ゴルーチンが完了しなかった")
	}
}

// TestGoForeverRestartsAfterPanic はワーカーがpanicしたあと再開されることを検証する。
//
// recoverして終了するだけの実装だと、ワーカーが静かに止まったままプロセスは生き続ける。
// heartbeatがこの状態になるとゾンビ接続の回収が永久に止まるため、
// 「再開すること」までがこの関数の契約
func TestGoForeverRestartsAfterPanic(t *testing.T) {
	// - テスト時間を縮めるため再開間隔を一時的に短くする
	original := panicRestartDelay
	panicRestartDelay = 10 * time.Millisecond
	defer func() { panicRestartDelay = original }()

	var attempts int32
	restarted := make(chan struct{})

	GoForever("test_forever", func() {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			panic("1回目は落ちる")
		}
		// - 2回目に到達した = 再開された
		close(restarted)
		select {} // 終了しないワーカーを模す
	})

	select {
	case <-restarted:
	case <-time.After(3 * time.Second):
		t.Fatalf("panic後に再開されなかった (実行回数=%d)", atomic.LoadInt32(&attempts))
	}
}

// TestGoForeverStopsOnNormalReturn は正常にreturnした場合は再実行しないことを検証する。
// 意図した終了まで再開し続けると無限ループになる
func TestGoForeverStopsOnNormalReturn(t *testing.T) {
	original := panicRestartDelay
	panicRestartDelay = 10 * time.Millisecond
	defer func() { panicRestartDelay = original }()

	var calls int32
	GoForever("test_normal_return", func() {
		atomic.AddInt32(&calls, 1)
	})

	time.Sleep(200 * time.Millisecond)

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("正常終了したワーカーが再実行された: 実行回数=%d", got)
	}
}
