package events

import (
	"testing"
	"time"
)

func newTestBroker() *Broker {
	return &Broker{
		Clients:     make(map[string]map[chan string]bool),
		connectedAt: make(map[chan string]time.Time),
	}
}

// TestRemoveClientAfterZombieCleanup はゾンビ回収済みチャネルに対する
// RemoveClientが二重closeでpanicしないことを検証する。
//
// 再現条件: 同一店舗に複数クライアントが居る状態で、片方のチャネルが溢れて
// pingAndCleanにゾンビ判定される。この時 b.Clients[storeID] 自体は
// 残っているクライアントのぶん存在し続けるため、チャネル単位の登録確認が
// 無いとRemoveClientがcloseまで到達してしまう。
func TestRemoveClientAfterZombieCleanup(t *testing.T) {
	b := newTestBroker()
	const storeID = "store-1"

	// - 溢れさせる側。ハンドラが受信しない状態を作るためバッファを埋める
	zombie := make(chan string, 10)
	// - 同じ店舗に残る側。これが居ることで b.Clients[storeID] が保持される
	healthy := make(chan string, 10)

	b.AddClient(storeID, zombie)
	b.AddClient(storeID, healthy)

	for i := 0; i < cap(zombie); i++ {
		zombie <- "filler"
	}

	b.pingAndClean()

	if _, exists := b.Clients[storeID][zombie]; exists {
		t.Fatal("ゾンビチャネルが回収されていない")
	}
	if _, exists := b.Clients[storeID][healthy]; !exists {
		t.Fatal("正常チャネルまで回収されている")
	}

	// - ハンドラのdeferに相当。修正前はここで panic: close of closed channel
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RemoveClientがpanicした: %v", r)
		}
	}()
	b.RemoveClient(storeID, zombie)
}

// TestPingAndCleanSendsHeartbeatSentinel は正常なチャネルにセンチネルが
// 届くことを確認する。ハンドラ側はこの値でSSEコメント行への分岐を行うため、
// 値が変わると "data: :ping" に逆戻りする
func TestPingAndCleanSendsHeartbeatSentinel(t *testing.T) {
	b := newTestBroker()
	const storeID = "store-1"

	ch := make(chan string, 10)
	b.AddClient(storeID, ch)

	b.pingAndClean()

	select {
	case msg := <-ch:
		if msg != HeartbeatMessage {
			t.Fatalf("期待値 %q, 実際 %q", HeartbeatMessage, msg)
		}
	default:
		t.Fatal("heartbeatが送信されていない")
	}
}

// TestRemoveClientDeletesEmptyStoreKey は最後のクライアント削除時に
// 店舗キー自体が消えることを確認する (登録確認ガード追加による退行防止)
func TestRemoveClientDeletesEmptyStoreKey(t *testing.T) {
	b := newTestBroker()
	const storeID = "store-1"

	ch := make(chan string, 10)
	b.AddClient(storeID, ch)
	b.RemoveClient(storeID, ch)

	if _, exists := b.Clients[storeID]; exists {
		t.Fatal("クライアントが居なくなった店舗キーが残っている")
	}
	if _, exists := b.connectedAt[ch]; exists {
		t.Fatal("接続時刻の記録が残っている")
	}
}

// TestSendToOnlyReachesTargetClient は初期データが接続元にだけ届くことを検証する。
//
// 以前はBroadcastで送っており、客が1人接続するたびに同じ店舗の全接続へ
// 全件が再送されていた。接続数に比例して転送量が増えるため、混むほど遅くなる
func TestSendToOnlyReachesTargetClient(t *testing.T) {
	b := newTestBroker()
	target := make(chan string, 1)
	other := make(chan string, 1)
	b.AddClient("store-1", target)
	b.AddClient("store-1", other)

	b.SendTo("store-1", target, "initial")

	select {
	case got := <-target:
		if got != "initial" {
			t.Errorf("届いた内容が違う: %q", got)
		}
	default:
		t.Fatal("接続元に初期データが届いていない")
	}

	select {
	case got := <-other:
		t.Errorf("無関係の接続にまで配信された: %q", got)
	default:
		// 期待通り
	}
}

// TestSendToAfterRemoveDoesNotPanic は回収済みチャネルへの送信でpanicしないことを検証する。
//
// 初期データは別ゴルーチンで送るため、その間に客が画面を閉じると
// RemoveClient がチャネルをcloseする。closeされたチャネルへの送信はpanicになる。
// utils.Go のpanic保護でプロセスは落ちないが、切断のたびにスタックトレースが積み上がる
func TestSendToAfterRemoveDoesNotPanic(t *testing.T) {
	b := newTestBroker()
	ch := make(chan string, 1)
	b.AddClient("store-1", ch)
	b.RemoveClient("store-1", ch) // ここで close される

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("回収済みチャネルへの送信でpanicした: %v", rec)
		}
	}()
	b.SendTo("store-1", ch, "initial")
}

// TestSendToFullChannelDoesNotBlock は受信側が詰まっていても送信側が止まらないことを検証する。
//
// ブロックすると、初期データ用のゴルーチンが客の切断まで解放されない
func TestSendToFullChannelDoesNotBlock(t *testing.T) {
	b := newTestBroker()
	ch := make(chan string, 1)
	b.AddClient("store-1", ch)
	ch <- "既に詰まっている"

	done := make(chan struct{})
	go func() {
		b.SendTo("store-1", ch, "initial")
		close(done)
	}()

	select {
	case <-done:
		// 期待通り
	case <-time.After(time.Second):
		t.Fatal("詰まったチャネルへの送信でブロックした")
	}
}
