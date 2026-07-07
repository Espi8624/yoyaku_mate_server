package events

import (
	"sync"
)

// Broker はSSEクライアントを管理し、メッセージをブロードキャストします
type Broker struct {
	// 店舗IDとクライアントチャンネルリストのマップ
	Clients map[string]map[chan string]bool

	// Clientsマップを保護するためのMutex
	Mutex sync.RWMutex
}

var (
	// シングルトンインスタンス
	Instance *Broker
	Once     sync.Once
)

// GetBroker はBrokerのシングルトンインスタンスを返します
func GetBroker() *Broker {
	Once.Do(func() {
		Instance = &Broker{
			Clients: make(map[string]map[chan string]bool),
		}
	})
	return Instance
}

// AddClient は特定の店舗に新しいクライアントチャンネルを追加します
func (b *Broker) AddClient(storeID string, clientChan chan string) {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	if _, ok := b.Clients[storeID]; !ok {
		b.Clients[storeID] = make(map[chan string]bool)
	}
	b.Clients[storeID][clientChan] = true
	// log.Printf("Client added to store %s. Total clients: %d", storeID, len(b.Clients[storeID]))
}

// RemoveClient はクライアントチャンネルを削除します
func (b *Broker) RemoveClient(storeID string, clientChan chan string) {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	if clients, ok := b.Clients[storeID]; ok {
		delete(clients, clientChan)
		close(clientChan)
		// log.Printf("Client removed from store %s. Remaining clients: %d", storeID, len(clients))
		if len(clients) == 0 {
			delete(b.Clients, storeID)
		}
	}
}

// Broadcast は特定の店舗にサブスクライブしているすべてのクライアントにメッセージを送信します
func (b *Broker) Broadcast(storeID string, message string) {
	b.Mutex.RLock()
	defer b.Mutex.RUnlock()

	if clients, ok := b.Clients[storeID]; ok {
		// log.Printf("Broadcasting to %d clients for store %s", len(clients), storeID)
		for clientChan := range clients {
			select {
			case clientChan <- message:
			default:
				// チャンネルがブロックされている場合、ブロードキャストの停止を防ぐためにスキップします
				// log.Printf("Client channel blocked for store %s, skipping", storeID)
			}
		}
	}
}
