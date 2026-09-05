package handlers

import (
	"net/http"

	"yoyaku_mate_server/metrics"

	"github.com/gorilla/mux"
)

func RegisterRoutes(
	r *mux.Router,
	userRepo MiddlewareUserRepository,
	uploadHandler *UploadHandler,
	waitingHandler *WaitingListHandler,
	menuHandler *MenuListHandler,
	userInfoHandler *UserInfoHandler,
	storeInfoHandler *StoreInfoHandler,
	storeSettingsHandler *StoreSettingsHandler,
	storeStaffHandler *StoreStaffHandler,
	shiftTableHandler *ShiftTableHandler,
	storeListHandler *StoreListHandler,
	storeAiContextHandler *StoreAIContextHandler,
	adminHandler *StoreInfoAdminHandler,
	storeLicenseCallHandler *StoreLicenseCallHandler,
	statisticsHandler *StatisticsHandler,
) {
	// ローカルファイルアップロードの静的ファイルサービングを設定
	r.PathPrefix("/uploads/").Handler(http.StripPrefix("/uploads/", http.FileServer(http.Dir("./uploads"))))

	// API endpoints
	api := r.PathPrefix("/api").Subrouter()

	api.HandleFunc("/waiting-list", waitingHandler.Handle)
	api.HandleFunc("/waiting-list/poll", waitingHandler.HandlePolling)
	api.HandleFunc("/waiting-list/stream", waitingHandler.HandleStream)
	api.HandleFunc("/waiting-list/stream-user", waitingHandler.HandleWaitingItemStream)
	api.HandleFunc("/statistics", statisticsHandler.HandleGet)

	api.HandleFunc("/public/store_ai_context", storeAiContextHandler.HandleGet)
	api.HandleFunc("/public/ai-chat", AIChatHandler).Methods("POST", "OPTIONS")

	api.HandleFunc("/menu-list", menuHandler.Handle).Methods("GET", "POST", "OPTIONS", "PATCH")
	api.HandleFunc("/menu-list/bulk-save", menuHandler.HandleBulkSaveMenuList)
	api.HandleFunc("/menus/{menuId}/image", uploadHandler.UploadMenuImage).Methods("POST", "OPTIONS")

	// - 店舗設定の取得 (公開、GETのみ許可)
	api.HandleFunc("/store_settings", storeSettingsHandler.GetStoreSettingsHandler).Methods("GET", "OPTIONS")
	// ProviderMenu endpoints
	api.HandleFunc("/provider_menu", menuHandler.Handle).Methods("GET", "POST", "PATCH", "DELETE", "OPTIONS")
	api.HandleFunc("/provider_menu/{menuId}/image", uploadHandler.UploadMenuImage).Methods("POST", "OPTIONS")
	api.HandleFunc("/provider_menu/category/bulk-update", menuHandler.HandleBulkUpdateCategory).Methods("POST", "OPTIONS")
	api.HandleFunc("/provider_menu/category/bulk-delete", menuHandler.HandleBulkDeleteCategory).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/provider_menu/all/bulk-delete", menuHandler.HandleBulkDeleteAllMenus).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/provider_user/image", uploadHandler.UploadUserImage).Methods("POST", "OPTIONS")
	// - 店舗情報の取得 (公開、GETのみ許可)
	api.HandleFunc("/provider_store", storeInfoHandler.GetStoreHandler).Methods("GET", "OPTIONS")
	api.HandleFunc("/provider_store/{storeId}/image", uploadHandler.UploadStoreImage).Methods("POST", "OPTIONS")
	api.HandleFunc("/provider_store/license", storeLicenseCallHandler.GetStoreLicenseHandler)
	api.HandleFunc("/provider_user/firebase_uid", userInfoHandler.UserByFirebaseUIDHandler)

	// - 認証が必要なルートグループ (RequireAuthMiddlewareを適用)
	authApi := api.PathPrefix("").Subrouter()
	authApi.Use(RequireAuthMiddleware(userRepo))

	// - ユーザー情報 (個人情報保護: GET/PUT ともに認証が必要)
	authApi.HandleFunc("/provider_user", userInfoHandler.HandleUser).Methods("GET", "PUT", "OPTIONS")
	// - 店舗情報の更新 (認証が必要、GETは公開ルートで処理)
	authApi.HandleFunc("/provider_store", storeInfoHandler.UpdateStoreHandler).Methods("PUT", "OPTIONS")
	// - 店舗設定の更新 (認証が必要、GETは公開ルートで処理)
	authApi.HandleFunc("/store_settings", storeSettingsHandler.UpdateStoreSettingsHandler).Methods("PUT", "OPTIONS")

	// Auth endpoints
	api.HandleFunc("/provider_stores/store-list", storeListHandler.GetMyStoresHandler)

	api.HandleFunc("/auth/signup", SignUpHandler)
	api.HandleFunc("/stores/add", AddNewStoreHandler)
	api.HandleFunc("/stores/join", storeStaffHandler.JoinStoreHandler)
	api.HandleFunc("/auth/check-store", StoreExistsHandler)
	api.HandleFunc("/auth/check-email", EmailCheckHandler)
	api.HandleFunc("/auth/check-phone", PhoneCheckHandler)

	api.HandleFunc("/stores/upload-license", uploadHandler.UploadLicense)

	// Admin endpoints
	adminApi := api.PathPrefix("/admin").Subrouter()
	// Admin専用監査ログミドルウェア（MetricsMiddlewareと独立して適用）
	adminApi.Use(metrics.AuditMiddleware)

	adminApi.HandleFunc("/stores", adminHandler.GetStoresHandler)
	adminApi.HandleFunc("/stores/{storeId}/status", adminHandler.UpdateStoreStatusHandler).Methods("PATCH", "OPTIONS")
	adminApi.HandleFunc("/license-image-url", uploadHandler.GetLicenseImageURLHandler).Methods("GET", "OPTIONS")
	adminApi.HandleFunc("/metrics/errors", GetErrorMetricsHandler).Methods("GET", "OPTIONS")
	adminApi.HandleFunc("/metrics/error-logs", GetErrorLogsHandler).Methods("GET", "OPTIONS")
	adminApi.HandleFunc("/metrics/requests", GetRequestMetricsHandler).Methods("GET", "OPTIONS")
	adminApi.HandleFunc("/metrics/request-logs", GetRequestLogsHandler).Methods("GET", "OPTIONS")
	adminApi.HandleFunc("/metrics/active-users", GetActiveUserMetricsHandler).Methods("GET", "OPTIONS")
	adminApi.HandleFunc("/metrics/sse-status", GetSSEMetricsHandler).Methods("GET", "OPTIONS")
	adminApi.HandleFunc("/metrics/response-time", GetResponseTimeMetricsHandler).Methods("GET", "OPTIONS")
	adminApi.HandleFunc("/metrics/audit-logs", GetAuditLogsHandler).Methods("GET", "OPTIONS")
	adminApi.HandleFunc("/metrics/system", GetSystemMetricsHandler).Methods("GET", "OPTIONS")
	adminApi.HandleFunc("/metrics/db", GetDBMetricsHandler).Methods("GET", "OPTIONS")

	// Staff Management endpoints
	api.HandleFunc("/stores/{storeId}/staff", storeStaffHandler.GetStoreStaffHandler).Methods("GET", "OPTIONS")
	api.HandleFunc("/stores/{storeId}/staff/{staffId}", storeStaffHandler.UpdateStoreStaffStatusHandler).Methods("PATCH", "OPTIONS")
	api.HandleFunc("/stores/{storeId}/staff/{staffId}/permissions", storeStaffHandler.UpdateStoreStaffPermissionsHandler).Methods("PATCH", "OPTIONS")
	api.HandleFunc("/stores/{storeId}/staff/{staffId}/availability", storeStaffHandler.UpdateStoreStaffAvailabilityHandler).Methods("PATCH", "OPTIONS")

	// Shift Table endpoints
	api.HandleFunc("/stores/{storeId}/shift-tables", shiftTableHandler.CreateShiftTableHandler).Methods("POST", "OPTIONS")
	api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}", shiftTableHandler.GetShiftTableHandler).Methods("GET", "OPTIONS")
	api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/shifts", shiftTableHandler.AddShiftHandler).Methods("POST", "OPTIONS")
	api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/shifts/{shiftId}", shiftTableHandler.UpdateShiftHandler).Methods("PATCH", "OPTIONS")
	api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/shifts/{shiftId}", shiftTableHandler.DeleteShiftHandler).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/auto-generate", shiftTableHandler.AutoGenerateShiftsHandler).Methods("POST", "OPTIONS")

	// Shift Change Request endpoints (週間シフト表に対する修正依頼)
	api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/change-requests", shiftTableHandler.CreateShiftChangeRequestHandler).Methods("POST", "OPTIONS")
	api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/change-requests", shiftTableHandler.GetShiftChangeRequestsHandler).Methods("GET", "OPTIONS")
	api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/change-requests/resolve", shiftTableHandler.ResolveShiftChangeRequestsHandler).Methods("POST", "OPTIONS")
	api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/change-requests/apply", shiftTableHandler.ApplyShiftChangeRequestsHandler).Methods("POST", "OPTIONS")
	api.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/change-requests/{requestId}", shiftTableHandler.DeleteShiftChangeRequestHandler).Methods("DELETE", "OPTIONS")
}
