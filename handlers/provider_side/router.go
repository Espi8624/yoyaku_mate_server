package handlers

import (
	"net/http"
)

func RegisterRoutes(mux *http.ServeMux) {
	// API endpoints
	mux.HandleFunc("/api/waiting-list", WaitingListHandler)
	mux.HandleFunc("/api/waiting-list/poll", HandleWaitingListPolling)
	mux.HandleFunc("/api/menu-list", MenuListHandler)
	mux.HandleFunc("/api/menu-list/bulk-save", handleBulkSaveMenuList)
}
