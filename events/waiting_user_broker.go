package events

import (
	"sync"
	"time"
)

// WaitingUserBroker は、個別待機顧客のSSEクライアントを管理し、メッセージをブロードキャストします
type WaitingUserBroker struct {
	// key: "storeID:waitingID" → クライアントチャネルマップ
	Clients map[string]map[chan string]bool

	// 各チャネルの接続時刻を記録（ゾンビ接続検知および平均維持時間の計算用）
	connectedAt map[chan string]time.Time

	// Clientsマップを保護するためのRWMutex
	Mutex sync.RWMutex
}

// WaitingUserBrokerStats はWaitingUserBrokerの統計データを示します
type WaitingUserBrokerStats struct {
	// 購読中のユーザーキー（"storeID:waitingID"）数
	ActiveKeys int
	// 全体の有効な接続（チャネル）数
	TotalConnections int
	// 平均接続維持時間（秒）
	AvgUptimeSeconds float64
}

var (
	// シングルトンインスタンス
	waitingUserBrokerInstance *WaitingUserBroker
	waitingUserBrokerOnce     sync.Once
)

// GetWaitingUserBroker はWaitingUserBrokerのシングルトンインスタンスを返し、初期化時にHeartbeatゴルーチンを実行します
func GetWaitingUserBroker() *WaitingUserBroker {
	waitingUserBrokerOnce.Do(func() {
		waitingUserBrokerInstance = &WaitingUserBroker{
			Clients:     make(map[string]map[chan string]bool),
			connectedAt: make(map[chan string]time.Time),
		}
		go waitingUserBrokerInstance.startHeartbeat()
	})
	return waitingUserBrokerInstance
}

// AddClient は特定の待機顧客キーに新しいクライアントチャネルを追加し、接続時刻を記録します
// key形式: "storeID:waitingID"
func (b *WaitingUserBroker) AddClient(key string, clientChan chan string) {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	if _, ok := b.Clients[key]; !ok {
		b.Clients[key] = make(map[chan string]bool)
	}
	b.Clients[key][clientChan] = true
	b.connectedAt[clientChan] = time.Now()
}

// RemoveClient はクライアントチャネルおよび接続時刻の情報を削除します
func (b *WaitingUserBroker) RemoveClient(key string, clientChan chan string) {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	if clients, ok := b.Clients[key]; ok {
		delete(clients, clientChan)
		delete(b.connectedAt, clientChan)
		close(clientChan)
		if len(clients) == 0 {
			delete(b.Clients, key)
		}
	}
}

// Broadcast は特定の待機顧客を購読しているすべてのクライアントにメッセージを送信します
func (b *WaitingUserBroker) Broadcast(key string, message string) {
	b.Mutex.RLock()
	defer b.Mutex.RUnlock()

	if clients, ok := b.Clients[key]; ok {
		for clientChan := range clients {
			select {
			case clientChan <- message:
			default:
				// チャネルがブロックされている場合、全体のブロードキャスト中断を防ぐためにスキップします
			}
		}
	}
}

// startHeartbeat は30秒周期でpingAndCleanを実行するバックグラウンドゴルーチンです
func (b *WaitingUserBroker) startHeartbeat() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		b.pingAndClean()
	}
}

// pingAndClean は全体のチャネルにpingを送信し、ブロックされたチャネル（ゾンビ接続）を削除します
// SSE仕様のコメント形式（":ping\n\n"）はクライアント側でイベントとして受信されません
//
// - 以前は走査中ずっと排他Lockを保持しており、全待機顧客分のping送信が終わるまでBroadcast(RLock)が
//   待たされていた。notifyWaitingUsersが待機顧客ごとにBroadcastを呼ぶホットパスであるため、
//   このブローカーでは特に影響が大きい。
// - 送信は非ブロッキング(select-default)でマップを書き換えないため、RLockで走査すれば
//   Broadcastとは同時実行できる。ゾンビ削除だけを短時間の排他Lockに分離する。
func (b *WaitingUserBroker) pingAndClean() {
	b.Mutex.RLock()
	var zombies []zombieChannel
	for key, clients := range b.Clients {
		for ch := range clients {
			select {
			case ch <- ":ping":
				// 正常チャネル: keep-aliveを維持
			default:
				// チャネルブロック = ゾンビ接続 → 削除対象として記録(走査中はまだ削除しない)
				zombies = append(zombies, zombieChannel{key, ch})
			}
		}
	}
	b.Mutex.RUnlock()

	if len(zombies) == 0 {
		return
	}

	b.Mutex.Lock()
	defer b.Mutex.Unlock()
	for _, z := range zombies {
		clients, ok := b.Clients[z.storeID]
		if !ok {
			continue
		}
		// - RUnlockからLock取得までの間にRemoveClientで既に正常削除されている可能性があるため確認
		if _, exists := clients[z.ch]; !exists {
			continue
		}
		delete(clients, z.ch)
		delete(b.connectedAt, z.ch)
		close(z.ch)
		if len(clients) == 0 {
			delete(b.Clients, z.storeID)
		}
	}
}

// GetStats は現在のWaitingUserBrokerの接続統計を返します
func (b *WaitingUserBroker) GetStats() WaitingUserBrokerStats {
	b.Mutex.RLock()
	defer b.Mutex.RUnlock()

	totalConnections := 0
	for _, clients := range b.Clients {
		totalConnections += len(clients)
	}

	// 平均接続維持時間の計算
	var totalUptimeSeconds float64
	now := time.Now()
	for ch, connTime := range b.connectedAt {
		_ = ch
		totalUptimeSeconds += now.Sub(connTime).Seconds()
	}

	var avgUptime float64
	if totalConnections > 0 {
		avgUptime = totalUptimeSeconds / float64(totalConnections)
	}

	return WaitingUserBrokerStats{
		ActiveKeys:       len(b.Clients),
		TotalConnections: totalConnections,
		AvgUptimeSeconds: avgUptime,
	}
}

