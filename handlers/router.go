package handlers

import (
	"github.com/gorilla/mux"
)

func RegisterRoutes(r *mux.Router, uploadHandler *UploadHandler) {
	// API endpoints
	r.HandleFunc("/api/waiting-list", WaitingListHandler)
	r.HandleFunc("/api/waiting-list-user", WaitingListUserHandler)
	r.HandleFunc("/api/waiting-list/poll", HandleWaitingListPolling)
	r.HandleFunc("/api/menu-list", MenuListHandler)
	r.HandleFunc("/api/menu-list/bulk-save", handleBulkSaveMenuList)
	r.HandleFunc("/api/store_settings", StoreSettingsHandler)
	r.HandleFunc("/api/provider_user", UserHandler)
	r.HandleFunc("/api/provider_store", StoreHandler)
	r.HandleFunc("/api/provider_store/license", GetStoreLicenseHandler)

	r.HandleFunc("/api/provider_user/firebase_uid", UserByFirebaseUIDHandler)

	// Auth endpoints
	r.HandleFunc("/api/provider_stores/store-list", GetMyStoresHandler)

	r.HandleFunc("/api/auth/signup", SignUpHandler)
	r.HandleFunc("/api/stores/add", AddNewStoreHandler)
	r.HandleFunc("/api/auth/check-store", StoreExistsHandler)
	r.HandleFunc("/api/auth/check-email", EmailCheckHandler)
	r.HandleFunc("/api/auth/check-phone", PhoneCheckHandler)

	r.HandleFunc("/api/stores/upload-license", uploadHandler.UploadLicense)
	r.HandleFunc("/api/auth/line/callback", LineCallbackHandler)
	r.HandleFunc("/api/line/webhook", LineWebhookHandler)

	// Admin
	r.HandleFunc("/api/admin/stores", GetStoresHandler)
	r.HandleFunc("/api/admin/stores/{storeId}/status", UpdateStoreStatusHandler).Methods("PATCH", "OPTIONS")
}
