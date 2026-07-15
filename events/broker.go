package events

import (
	"sync"
	"time"
)

// Broker はSSEクライアントを管理し、メッセージをブロードキャストします
type Broker struct {
	// 店舗IDとクライアントチャネルリストのマップ
	Clients map[string]map[chan string]bool

	// 各チャネルの接続時刻を記録（ゾンビ接続検知および平均維持時間の計算用）
	connectedAt map[chan string]time.Time

	// Clientsマップを保護するためのRWMutex
	Mutex sync.RWMutex
}

// BrokerStats はBrokerの統計データを示します
type BrokerStats struct {
	// 購読中の店舗（キー）数
	ActiveKeys int
	// 全体の有効な接続（チャネル）数
	TotalConnections int
	// 平均接続維持時間（秒）
	AvgUptimeSeconds float64
}

var (
	// シングルトンインスタンス
	Instance *Broker
	Once     sync.Once
)

// GetBroker はBrokerのシングルトンインスタンスを返し、初期化時にHeartbeatゴルーチンを実行します
func GetBroker() *Broker {
	Once.Do(func() {
		Instance = &Broker{
			Clients:     make(map[string]map[chan string]bool),
			connectedAt: make(map[chan string]time.Time),
		}
		go Instance.startHeartbeat()
	})
	return Instance
}

// AddClient は特定の店舗に新しいクライアントチャネルを追加し、接続時刻を記録します
func (b *Broker) AddClient(storeID string, clientChan chan string) {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	if _, ok := b.Clients[storeID]; !ok {
		b.Clients[storeID] = make(map[chan string]bool)
	}
	b.Clients[storeID][clientChan] = true
	b.connectedAt[clientChan] = time.Now()
}

// RemoveClient はクライアントチャネルおよび接続時刻の情報を削除します
func (b *Broker) RemoveClient(storeID string, clientChan chan string) {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	if clients, ok := b.Clients[storeID]; ok {
		delete(clients, clientChan)
		delete(b.connectedAt, clientChan)
		close(clientChan)
		if len(clients) == 0 {
			delete(b.Clients, storeID)
		}
	}
}

// Broadcast は特定の店舗を購読しているすべてのクライアントにメッセージを送信します
func (b *Broker) Broadcast(storeID string, message string) {
	b.Mutex.RLock()
	defer b.Mutex.RUnlock()

	if clients, ok := b.Clients[storeID]; ok {
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
func (b *Broker) startHeartbeat() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		b.pingAndClean()
	}
}

// pingAndClean は全体のチャネルにpingを送信し、ブロックされたチャネル（ゾンビ接続）を即座に削除します
// SSE仕様のコメント形式（":ping\n\n"）はクライアント側でイベントとして受信されません
func (b *Broker) pingAndClean() {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	for storeID, clients := range b.Clients {
		for ch := range clients {
			select {
			case ch <- ":ping":
				// 正常チャネル: keep-aliveを維持
			default:
				// チャネルブロック = ゾンビ接続 → 即座に削除
				delete(clients, ch)
				delete(b.connectedAt, ch)
				close(ch)
			}
		}
		if len(clients) == 0 {
			delete(b.Clients, storeID)
		}
	}
}

// GetStats は現在のBrokerの接続統計を返します
func (b *Broker) GetStats() BrokerStats {
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

	return BrokerStats{
		ActiveKeys:       len(b.Clients),
		TotalConnections: totalConnections,
		AvgUptimeSeconds: avgUptime,
	}
}
