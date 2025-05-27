package events

import (
	"log"
	"sync"
)

// UpdateNotifier is used to notify when data changes occur
type UpdateNotifier interface {
	NotifyUpdate()
}

var (
	notifiers   = make(map[string][]UpdateNotifier)
	notifiersMu sync.RWMutex
)

// RegisterNotifier registers a notifier for a specific store
func RegisterNotifier(storeID string, notifier UpdateNotifier) {
	notifiersMu.Lock()
	defer notifiersMu.Unlock()
	notifiers[storeID] = append(notifiers[storeID], notifier)
}

// UnregisterNotifier removes a notifier for a specific store
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

// NotifyStoreUpdate notifies all registered notifiers for a specific store
func NotifyStoreUpdate(storeID string) {
	notifiersMu.RLock()
	defer notifiersMu.RUnlock()

	if ns, ok := notifiers[storeID]; ok {
		for _, n := range ns {
			go n.NotifyUpdate()
		}
	}
	log.Printf("Notified update for store: %s", storeID)
}
