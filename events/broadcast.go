package events

import (
	"sync"
)

// データの更新が発生したことを通知
type UpdateNotifier interface {
	NotifyUpdate()
}

var (
	notifiers   = make(map[string][]UpdateNotifier)
	notifiersMu sync.RWMutex
)

// 特定のストアに対してノーティファイア（通知先）を登録
func RegisterNotifier(storeID string, notifier UpdateNotifier) {
	notifiersMu.Lock()
	defer notifiersMu.Unlock()
	notifiers[storeID] = append(notifiers[storeID], notifier)
}

// 特定のストアに対してノーティファイアを登録解除
func UnregisterNotifier(storeID string, notifier UpdateNotifier) {
	notifiersMu.Lock()
	defer notifiersMu.Unlock()
	if ns, ok := notifiers[storeID]; ok {
		for i, n := range ns {
			if n == notifier {
				notifiers[storeID] = append(ns[:i], ns[i+1:]...)
				break
			}
		}
	}
}

// 特定のストアに対して更新通知を行う
func NotifyStoreUpdate(storeID string) {
	notifiersMu.RLock()
	defer notifiersMu.RUnlock()

	if ns, ok := notifiers[storeID]; ok {
		for _, n := range ns {
			go n.NotifyUpdate()
		}
	}
	// log.Printf("Notified update for store: %s", storeID)
}
