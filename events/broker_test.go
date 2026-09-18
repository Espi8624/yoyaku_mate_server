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
