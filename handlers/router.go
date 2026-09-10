package handlers

import (
	"net/http"

	"yoyaku_mate_server/metrics"

	"github.com/gorilla/mux"
)

func RegisterRoutes(
	r *mux.Router,
	userRepo MiddlewareUserRepository,
	sessionRepo MiddlewareSessionRepository,
	sessionHandler *SessionHandler,
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

	// ============================================================
	// 公開ルート (ゲスト・顧客ウェブ・ログイン前の処理が利用するためセッション検証の対象外)
	// ============================================================

	// - 待機リスト: ゲスト登録/ゲストキャンセルと直員操作が同一エンドポイントを共有しており、
	//   認証要否がリクエスト内容によって変わるため、ミドルウェアではなくハンドラ内で
	//   VerifySessionForUser を呼び分ける
	api.HandleFunc("/waiting-list", waitingHandler.Handle)
	api.HandleFunc("/waiting-list/poll", waitingHandler.HandlePolling)
	// - SSEストリームは店舗単位の公開データ (顧客ウェブも購読する)
	api.HandleFunc("/waiting-list/stream", waitingHandler.HandleStream)
	api.HandleFunc("/waiting-list/stream-user", waitingHandler.HandleWaitingItemStream)

	api.HandleFunc("/public/store_ai_context", storeAiContextHandler.HandleGet)
	// - システムプロンプトはクライアントから受け取らず、storeAiContextHandler経由でサーバーが自前で構築する
	aiChatHandler := NewAIChatHandler(storeAiContextHandler)
	api.HandleFunc("/public/ai-chat", aiChatHandler.Handle).Methods("POST", "OPTIONS")

	// - メニュー一覧の取得は顧客ウェブも利用するため公開 (更新系は点主アプリ専用ルートで処理)
	api.HandleFunc("/menu-list", menuHandler.Handle).Methods("GET", "OPTIONS")
	// - 店舗設定の取得 (公開、GETのみ許可)
	api.HandleFunc("/store_settings", storeSettingsHandler.GetStoreSettingsHandler).Methods("GET", "OPTIONS")
	// - 店舗情報の取得 (公開、GETのみ許可)
	api.HandleFunc("/provider_store", storeInfoHandler.GetStoreHandler).Methods("GET", "OPTIONS")

	// - 会員登録・重複チェックはログイン前に呼ばれるため認証・セッションともに不要
	api.HandleFunc("/auth/signup", SignUpHandler)
	api.HandleFunc("/auth/check-store", StoreExistsHandler)
	api.HandleFunc("/auth/check-email", EmailCheckHandler)
	api.HandleFunc("/auth/check-phone", PhoneCheckHandler)

	// - 会員登録直後(セッション確立前)の店舗作成・参加・営業許可証アップロード。
	//   Firebase認証はハンドラ内で行うが、セッション検証を課すと登録フローが成立しないため対象外
	api.HandleFunc("/stores/add", AddNewStoreHandler)
	api.HandleFunc("/stores/join", storeStaffHandler.JoinStoreHandler)
	api.HandleFunc("/stores/upload-license", uploadHandler.UploadLicense)

	// - セッション発行の起点。このエンドポイント自体がセッションを発行するため、
	//   セッション検証の対象にはできない (Firebase認証のみ要求する)
	sessionApi := api.PathPrefix("").Subrouter()
	sessionApi.Use(RequireAuthMiddleware(userRepo))
	sessionApi.HandleFunc("/auth/session", sessionHandler.Handle).Methods("POST", "DELETE", "OPTIONS")

	// - プロフィール取得。セッション確立前(アプリ起動直後)にも呼ばれるため対象外
	api.HandleFunc("/provider_user/firebase_uid", userInfoHandler.UserByFirebaseUIDHandler)

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

	// ============================================================
	// 点主アプリ専用ルート (Firebase認証 + 端末セッション検証)
	// - ハンドラごとに検証を書くと必ず付け忘れが発生するため、ここにルートを登録するだけで
	//   検証が掛かるようにしている。新しい点主アプリ向けAPIは全てこのグループに追加すること
	// ============================================================
	providerApi := api.PathPrefix("").Subrouter()
	providerApi.Use(RequireAuthMiddleware(userRepo))
	providerApi.Use(RequireSessionMiddleware(sessionRepo))

	// - ユーザー情報 (個人情報保護: GET/PUT/DELETE ともに認証が必要。DELETEは会員退会)
	providerApi.HandleFunc("/provider_user", userInfoHandler.HandleUser).Methods("GET", "PUT", "DELETE", "OPTIONS")
	providerApi.HandleFunc("/provider_user/image", uploadHandler.UploadUserImage).Methods("POST", "OPTIONS")

	// - 店舗情報・店舗設定の更新 (GETは公開ルートで処理)
	providerApi.HandleFunc("/provider_store", storeInfoHandler.UpdateStoreHandler).Methods("PUT", "OPTIONS")
	providerApi.HandleFunc("/store_settings", storeSettingsHandler.UpdateStoreSettingsHandler).Methods("PUT", "OPTIONS")
	// - モニターボードのQRトークン発行を認可するための店舗別シークレット。未設定なら初回アクセス時に生成
	providerApi.HandleFunc("/store_settings/board_key", storeSettingsHandler.GetBoardKeyHandler).Methods("GET", "OPTIONS")
	providerApi.HandleFunc("/provider_store/{storeId}/image", uploadHandler.UploadStoreImage).Methods("POST", "OPTIONS")
	providerApi.HandleFunc("/provider_store/license", storeLicenseCallHandler.GetStoreLicenseHandler)
	providerApi.HandleFunc("/provider_stores/store-list", storeListHandler.GetMyStoresHandler)

	providerApi.HandleFunc("/statistics", statisticsHandler.HandleGet)

	// Menu endpoints
	providerApi.HandleFunc("/menu-list", menuHandler.Handle).Methods("POST", "PATCH")
	providerApi.HandleFunc("/menu-list/bulk-save", menuHandler.HandleBulkSaveMenuList)
	providerApi.HandleFunc("/menus/{menuId}/image", uploadHandler.UploadMenuImage).Methods("POST", "OPTIONS")
	providerApi.HandleFunc("/provider_menu", menuHandler.Handle).Methods("GET", "POST", "PATCH", "DELETE", "OPTIONS")
	providerApi.HandleFunc("/provider_menu/{menuId}/image", uploadHandler.UploadMenuImage).Methods("POST", "OPTIONS")
	providerApi.HandleFunc("/provider_menu/category/bulk-update", menuHandler.HandleBulkUpdateCategory).Methods("POST", "OPTIONS")
	providerApi.HandleFunc("/provider_menu/category/bulk-delete", menuHandler.HandleBulkDeleteCategory).Methods("DELETE", "OPTIONS")
	providerApi.HandleFunc("/provider_menu/all/bulk-delete", menuHandler.HandleBulkDeleteAllMenus).Methods("DELETE", "OPTIONS")

	// - 自動翻訳プロキシ (メニュー名/カテゴリー名/待機メモ)。GEMINI_API_KEYはサーバー側のみが保持する
	providerApi.HandleFunc("/provider_translate", HandleTranslate).Methods("POST", "OPTIONS")
	providerApi.HandleFunc("/provider_translate/multi", HandleTranslateMulti).Methods("POST", "OPTIONS")

	// Staff Management endpoints
	providerApi.HandleFunc("/stores/{storeId}/staff", storeStaffHandler.GetStoreStaffHandler).Methods("GET", "OPTIONS")
	providerApi.HandleFunc("/stores/{storeId}/staff/{staffId}", storeStaffHandler.UpdateStoreStaffStatusHandler).Methods("PATCH", "OPTIONS")
	providerApi.HandleFunc("/stores/{storeId}/staff/{staffId}/permissions", storeStaffHandler.UpdateStoreStaffPermissionsHandler).Methods("PATCH", "OPTIONS")
	providerApi.HandleFunc("/stores/{storeId}/staff/{staffId}/availability", storeStaffHandler.UpdateStoreStaffAvailabilityHandler).Methods("PATCH", "OPTIONS")

	// Shift Table endpoints
	providerApi.HandleFunc("/stores/{storeId}/shift-tables", shiftTableHandler.CreateShiftTableHandler).Methods("POST", "OPTIONS")
	providerApi.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}", shiftTableHandler.GetShiftTableHandler).Methods("GET", "OPTIONS")
	providerApi.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/shifts", shiftTableHandler.AddShiftHandler).Methods("POST", "OPTIONS")
	providerApi.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/shifts/{shiftId}", shiftTableHandler.UpdateShiftHandler).Methods("PATCH", "OPTIONS")
	providerApi.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/shifts/{shiftId}", shiftTableHandler.DeleteShiftHandler).Methods("DELETE", "OPTIONS")
	providerApi.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/auto-generate", shiftTableHandler.AutoGenerateShiftsHandler).Methods("POST", "OPTIONS")
	// 下書きを確定してスタッフに公開する。シフト表がスタッフから見える唯一の経路
	providerApi.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/publish", shiftTableHandler.PublishShiftTableHandler).Methods("POST", "OPTIONS")
	// 下書きを破棄して確定版へ戻す (確定版そのものは変更しない)
	providerApi.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/discard-draft", shiftTableHandler.DiscardShiftTableDraftHandler).Methods("POST", "OPTIONS")

	// Shift Change Request endpoints (週間シフト表に対する修正依頼)
	providerApi.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/change-requests", shiftTableHandler.CreateShiftChangeRequestHandler).Methods("POST", "OPTIONS")
	providerApi.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/change-requests", shiftTableHandler.GetShiftChangeRequestsHandler).Methods("GET", "OPTIONS")
	providerApi.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/change-requests/apply", shiftTableHandler.ApplyShiftChangeRequestsHandler).Methods("POST", "OPTIONS")
	providerApi.HandleFunc("/stores/{storeId}/shift-tables/{weekStartDate}/change-requests/{requestId}", shiftTableHandler.DeleteShiftChangeRequestHandler).Methods("DELETE", "OPTIONS")
}
