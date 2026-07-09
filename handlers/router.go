package handlers

import (
	"net/http"

	"github.com/gorilla/mux"
)

func RegisterRoutes(r *mux.Router, uploadHandler *UploadHandler) {
	// ローカルファイルアップロードの静的ファイルサービングを設定
	r.PathPrefix("/uploads/").Handler(http.StripPrefix("/uploads/", http.FileServer(http.Dir("./uploads"))))

	// API endpoints
	api := r.PathPrefix("/api").Subrouter()

	api.HandleFunc("/waiting-list", WaitingListHandler)
	api.HandleFunc("/waiting-list/poll", HandleWaitingListPolling)
	api.HandleFunc("/waiting-list/stream", HandleWaitingListStream)
	api.HandleFunc("/waiting-list/stream-user", HandleWaitingItemStream)
	api.HandleFunc("/statistics", StatisticsHandler)

	api.HandleFunc("/public/store_ai_context", StoreAIContextHandler)
	api.HandleFunc("/public/ai-chat", AIChatHandler).Methods("POST", "OPTIONS")

	api.HandleFunc("/menu-list", MenuListHandler).Methods("GET", "POST", "OPTIONS", "PATCH")
	api.HandleFunc("/menu-list/bulk-save", HandleBulkSaveMenuList)
	api.HandleFunc("/menus/{menuId}/image", uploadHandler.UploadMenuImage).Methods("POST", "OPTIONS")

	api.HandleFunc("/store_settings", StoreSettingsHandler)
	// ProviderMenu endpoints
	api.HandleFunc("/provider_menu", MenuListHandler).Methods("GET", "POST", "PATCH", "DELETE", "OPTIONS")
	api.HandleFunc("/provider_menu/{menuId}/image", uploadHandler.UploadMenuImage).Methods("POST", "OPTIONS")
	api.HandleFunc("/provider_menu/category/bulk-update", HandleBulkUpdateCategory).Methods("POST", "OPTIONS")
	api.HandleFunc("/provider_menu/category/bulk-delete", HandleBulkDeleteCategory).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/provider_menu/all/bulk-delete", HandleBulkDeleteAllMenus).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/provider_user", UserHandler)
	api.HandleFunc("/provider_user/image", uploadHandler.UploadUserImage).Methods("POST", "OPTIONS")
	api.HandleFunc("/provider_store", StoreHandler)
	api.HandleFunc("/provider_store/{storeId}/image", uploadHandler.UploadStoreImage).Methods("POST", "OPTIONS")
	api.HandleFunc("/provider_store/license", GetStoreLicenseHandler)

	api.HandleFunc("/provider_user/firebase_uid", UserByFirebaseUIDHandler)

	// Auth endpoints
	api.HandleFunc("/provider_stores/store-list", GetMyStoresHandler)

	api.HandleFunc("/auth/signup", SignUpHandler)
	api.HandleFunc("/stores/add", AddNewStoreHandler)
	api.HandleFunc("/stores/join", JoinStoreHandler)
	api.HandleFunc("/auth/check-store", StoreExistsHandler)
	api.HandleFunc("/auth/check-email", EmailCheckHandler)
	api.HandleFunc("/auth/check-phone", PhoneCheckHandler)

	api.HandleFunc("/stores/upload-license", uploadHandler.UploadLicense)

	// Admin endpoints
	adminApi := api.PathPrefix("/admin").Subrouter()

	adminApi.HandleFunc("/stores", GetStoresHandler)
	adminApi.HandleFunc("/stores/{storeId}/status", UpdateStoreStatusHandler).Methods("PATCH", "OPTIONS")
	adminApi.HandleFunc("/license-image-url", uploadHandler.GetLicenseImageURLHandler).Methods("GET", "OPTIONS")

	// Staff Management endpoints
	api.HandleFunc("/stores/{storeId}/staff", GetStoreStaffHandler).Methods("GET", "OPTIONS")
	api.HandleFunc("/stores/{storeId}/staff/{staffId}", UpdateStoreStaffStatusHandler).Methods("PATCH", "OPTIONS")
	api.HandleFunc("/stores/{storeId}/staff/{staffId}/permissions", UpdateStoreStaffPermissionsHandler).Methods("PATCH", "OPTIONS")
}
