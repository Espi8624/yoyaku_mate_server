package handlers

import (
	"net/http"
)

func RegisterRoutes(mux *http.ServeMux, uploadHandler *UploadHandler) {
	// API endpoints
	mux.HandleFunc("/api/waiting-list", WaitingListHandler)
	mux.HandleFunc("/api/waiting-list/poll", HandleWaitingListPolling)
	mux.HandleFunc("/api/menu-list", MenuListHandler)
	mux.HandleFunc("/api/menu-list/bulk-save", handleBulkSaveMenuList)
	mux.HandleFunc("/api/store_settings", StoreSettingsHandler)
	mux.HandleFunc("/api/provider_user", UserHandler)
	mux.HandleFunc("/api/provider_store", StoreHandler)
	mux.HandleFunc("/api/provider_store/license", GetStoreLicenseHandler)

	mux.HandleFunc("/api/provider_user/firebase_uid", UserByFirebaseUIDHandler)

	// Auth endpoints
	mux.HandleFunc("/api/provider_stores/store-list", GetMyStoresHandler)

	mux.HandleFunc("/api/auth/signup", SignUpHandler)
	mux.HandleFunc("/api/stores/add", AddNewStoreHandler)
	mux.HandleFunc("/api/auth/check-store", StoreExistsHandler)
	mux.HandleFunc("/api/auth/check-email", EmailCheckHandler)
	mux.HandleFunc("/api/auth/check-phone", PhoneCheckHandler)

	mux.HandleFunc("/api/stores/upload-license", uploadHandler.UploadLicense)
	mux.HandleFunc("/api/auth/line/callback", LineCallbackHandler)
	mux.HandleFunc("/api/line/webhook", LineWebhookHandler)
}
