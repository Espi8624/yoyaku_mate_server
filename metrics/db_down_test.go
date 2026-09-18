package metrics

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// TestSkipFlushWhileDBDown DB未接続時にバッチ保存を見送り、ログは状態が変わったときだけ出すことを確認する
//
//   - テスト環境ではMongoDBに接続しないため db.IsReady() は常にfalse。
//     つまりここで検証できるのは「未接続時の振る舞い」であり、それがまさに確認したい側
//   - ログ抑制はこのテストでしか守られない。3つのワーカーが5秒周期で回るため、
//     抑制が外れると分あたり数十行になり、本当の障害原因がfly.ioのログから見えなくなる
func TestSkipFlushWhileDBDown(t *testing.T) {
	// - 他のテストが先にフラグを立てている可能性があるため、明示的に初期化する
	dbDownLogged.Store(false)

	var logged bytes.Buffer
	original := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() {
		log.SetOutput(original)
		dbDownLogged.Store(false)
	})

	if !skipFlushWhileDBDown() {
		t.Fatal("DB未接続なのに保存を見送っていない (nil参照でpanicする経路)")
	}
	if !strings.Contains(logged.String(), "MongoDB未接続") {
		t.Errorf("最初の1回でログが出ていない: %q", logged.String())
	}

	// - 2回目以降は同じ状態が続いているだけなので、ログを重ねない
	logged.Reset()
	for i := 0; i < 5; i++ {
		if !skipFlushWhileDBDown() {
			t.Fatalf("%d回目で保存を見送らなくなった", i+2)
		}
	}
	if logged.Len() != 0 {
		t.Errorf("同じ状態が続いている間もログが出続けている: %q", logged.String())
	}
}
