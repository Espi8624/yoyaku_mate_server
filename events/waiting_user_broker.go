package events

import (
	"sync"
)

// WaitingUserBrokerは、個別待機顧客のSSEクライアントを管理し、メッセージをブロードキャストします。
type WaitingUserBroker struct {
	// key: "storeID:waitingID" -> value: クライアントチャネルマップ
	Clients map[string]map[chan string]bool

	// Clientsマップを保護するためのRWMutex
	Mutex sync.RWMutex
}

var (
	// シングルトンインスタンス
	waitingUserBrokerInstance *WaitingUserBroker
	waitingUserBrokerOnce     sync.Once
)

// GetWaitingUserBrokerは、WaitingUserBrokerのシングルトンインスタンスを返します。
func GetWaitingUserBroker() *WaitingUserBroker {
	waitingUserBrokerOnce.Do(func() {
		waitingUserBrokerInstance = &WaitingUserBroker{
			Clients: make(map[string]map[chan string]bool),
		}
	})
	return waitingUserBrokerInstance
}

// AddClientは、特定の待機顧客に新しいクライアントチャネルを追加します。
// key形式: "storeID:waitingID"
func (b *WaitingUserBroker) AddClient(key string, clientChan chan string) {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	if _, ok := b.Clients[key]; !ok {
		b.Clients[key] = make(map[chan string]bool)
	}
	b.Clients[key][clientChan] = true
}

// RemoveClientは、クライアントチャネルを削除します。
func (b *WaitingUserBroker) RemoveClient(key string, clientChan chan string) {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	if clients, ok := b.Clients[key]; ok {
		delete(clients, clientChan)
		close(clientChan)
		if len(clients) == 0 {
			delete(b.Clients, key)
		}
	}
}

// Broadcastは、特定の待機顧客を購読中のすべてのクライアントチャネルにメッセージを送信します。
func (b *WaitingUserBroker) Broadcast(key string, message string) {
	b.Mutex.RLock()
	defer b.Mutex.RUnlock()

	if clients, ok := b.Clients[key]; ok {
		for clientChan := range clients {
			select {
			case clientChan <- message:
			default:
				// チャネルがブロックされている場合、全体のブロードキャストが待機するのを防ぐためにスキップします。
			}
		}
	}
}
