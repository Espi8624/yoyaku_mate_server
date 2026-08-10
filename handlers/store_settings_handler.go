package handlers

import (
	"encoding/json"
	"net/http"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"
)

// StoreSettingsRepository 店舗設定の取得と更新を抽象化するインターフェース
type StoreSettingsRepository interface {
	GetSettings(storeID string) (*models.StoreSetting, error)
	UpsertStoreSettings(storeID string, reqBody map[string]interface{}) error
}

// StoreSettingsHandler 店舗設定関連のHTTPリクエストを処理するハンドラ
type StoreSettingsHandler struct {
	storeRepo StoreSettingsRepository
	userRepo  UserRepository // UserRepository for permission check
}

func NewStoreSettingsHandler(storeRepo StoreSettingsRepository, userRepo UserRepository) *StoreSettingsHandler {
	return &StoreSettingsHandler{
		storeRepo: storeRepo,
		userRepo:  userRepo,
	}
}

// GetStoreSettingsHandler 店舗設定の取得 (GET) - 公開エンドポイント、認証不要
// GET /api/store_settings?store_id=xxx
func (h *StoreSettingsHandler) GetStoreSettingsHandler(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}
	settings, err := h.storeRepo.GetSettings(storeID)
	if err != nil {
		utils.RespondWithError(w, "Store settings not found", http.StatusNotFound)
		return
	}
	utils.RespondWithJSON(w, settings, http.StatusOK)
}

// UpdateStoreSettingsHandler 店舗設定の更新 (PUT) - 認証が必要なエンドポイント
// PUT /api/store_settings?store_id=xxx
// - RequireAuthMiddlewareを通過後に呼び出される
// - マネージャーまたは承認済みスタッフのみ更新可能
func (h *StoreSettingsHandler) UpdateStoreSettingsHandler(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// - ミドルウェアで格納された認証済みユーザーを取得
	authUser, ok := GetUserFromContext(r.Context())
	if !ok {
		utils.RespondWithError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// - 権限チェック: マネージャーまたは承認済みスタッフのみ更新可能
	hasPermission, err := h.userRepo.CheckStorePermission(authUser.ID, storeID, authUser.Role, "")
	if err != nil {
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasPermission {
		utils.RespondWithError(w, "この店舗の設定を変更する権限がありません。", http.StatusForbidden)
		return
	}

	var reqBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.storeRepo.UpsertStoreSettings(storeID, reqBody); err != nil {
		utils.RespondWithError(w, "Failed to update store settings", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]bool{"success": true}, http.StatusOK)
}
