package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// allowedStoreUpdateFields PUT /api/provider_store で書き換えてよいフィールド。
// store_id・user_idや営業許可証関連は専用フローでのみ変更されるべきなので、
// ここには含めない(store_image_urlも専用アップロードエンドポイントがあるため除外)
var allowedStoreUpdateFields = map[string]bool{
	"store_name":        true,
	"business_category": true,
	"zip_code":          true,
	"prefecture":        true,
	"city":              true,
	"address":           true,
	"building":          true,
	"phone":             true,
}

// StoreInfoRepository 店舗情報の取得と更新を抽象化するインターフェース
type StoreInfoRepository interface {
	GetStoreData(storeID string) (*models.Store, error)
	GetStoreDataByUserID(userID primitive.ObjectID) (*models.Store, error)
	UpdateStoreData(storeID string, update map[string]interface{}) (*models.Store, error)
}

// StoreInfoHandler 店舗情報関連のHTTPリクエストを処理するハンドラ
type StoreInfoHandler struct {
	storeRepo StoreInfoRepository
	userRepo  UserRepository // UserRepository for permission check
}

func NewStoreInfoHandler(storeRepo StoreInfoRepository, userRepo UserRepository) *StoreInfoHandler {
	return &StoreInfoHandler{
		storeRepo: storeRepo,
		userRepo:  userRepo,
	}
}

// GetStoreHandler 店舗情報の取得 (GET) - 公開エンドポイント、認証不要
// GET /api/provider_store?store_id=xxx または ?user_id=xxx
func (h *StoreInfoHandler) GetStoreHandler(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	userID := r.URL.Query().Get("user_id")

	if storeID != "" {
		// - store_id で照会
		store, err := h.storeRepo.GetStoreData(storeID)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				utils.RespondWithError(w, "Store not found by store_id", http.StatusNotFound)
			} else {
				utils.RespondWithError(w, "Internal server error", http.StatusInternalServerError)
			}
			return
		}
		utils.RespondWithJSON(w, store, http.StatusOK)
		return
	}

	if userID != "" {
		// - user_id で照会
		objectID, err := primitive.ObjectIDFromHex(userID)
		if err != nil {
			utils.RespondWithError(w, "Invalid user_id format", http.StatusBadRequest)
			return
		}

		store, err := h.storeRepo.GetStoreDataByUserID(objectID)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				utils.RespondWithError(w, "Store not found for the given user_id", http.StatusNotFound)
			} else {
				utils.RespondWithError(w, "Internal server error", http.StatusInternalServerError)
			}
			return
		}
		utils.RespondWithJSON(w, store, http.StatusOK)
		return
	}

	utils.RespondWithError(w, "Missing required query parameter: store_id or user_id", http.StatusBadRequest)
}

// UpdateStoreHandler 店舗情報の更新 (PUT) - 認証が必要なエンドポイント
// PUT /api/provider_store?store_id=xxx
// - RequireAuthMiddlewareを通過後に呼び出される
// - マネージャーまたは承認済みスタッフのみ更新可能
func (h *StoreInfoHandler) UpdateStoreHandler(w http.ResponseWriter, r *http.Request) {
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
		log.Printf("Failed to check user permission: %v", err)
		utils.RespondWithError(w, "Failed to verify permissions", http.StatusInternalServerError)
		return
	}
	if !hasPermission {
		log.Printf("User %s does not have permission for store %s", authUser.ID.Hex(), storeID)
		utils.RespondWithError(w, "この店舗の情報を修正する権限がありません。", http.StatusForbidden)
		return
	}

	var update map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// マスアサインメント対策: store_id・user_idなど本来クライアントが
	// 書き換えてはいけないフィールドを弾くため、許可されたフィールドのみ残す
	update = utils.FilterAllowedFields(update, allowedStoreUpdateFields)
	if len(update) == 0 {
		utils.RespondWithError(w, "No valid fields to update", http.StatusBadRequest)
		return
	}

	// 業種タグは必須項目のため、送信された場合のみ許可された値かどうか検証する
	// (空文字や未定義の値で必須タグが消えてしまうのを防ぐ)
	if rawCategory, exists := update["business_category"]; exists {
		category, isString := rawCategory.(string)
		if !isString || !models.IsValidStoreCategory(category) {
			utils.RespondWithError(w, "Invalid business_category", http.StatusBadRequest)
			return
		}
	}

	updatedStore, err := h.storeRepo.UpdateStoreData(storeID, update)
	if err != nil {
		utils.RespondWithError(w, "Failed to update store info", http.StatusInternalServerError)
		return
	}

	// - REST標準: PUTレスポンスに更新後のリソースを返却 (200 OK)
	utils.RespondWithJSON(w, updatedStore, http.StatusOK)
}
