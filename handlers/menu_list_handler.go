package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"
)

// ==========================================================
// MenuListHandler 構造体 & コンストラクタ
// ==========================================================

// MenuListHandler メニューリスト関連のHTTPリクエストを処理するハンドラ
type MenuListHandler struct {
	menuRepo data.MenuRepository
	userRepo UserRepository
	authSvc  AuthService
}

// NewMenuListHandler MenuListHandlerのコンストラクタ
func NewMenuListHandler(
	menuRepo data.MenuRepository,
	userRepo UserRepository,
	authSvc AuthService,
) *MenuListHandler {
	return &MenuListHandler{
		menuRepo: menuRepo,
		userRepo: userRepo,
		authSvc:  authSvc,
	}
}

// ==========================================================
// 公開ハンドラメソッド
// ==========================================================

// Handle メニューリストリクエストを処理するメインハンドラ
func (h *MenuListHandler) Handle(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleGetMenuList(w, r)
	case http.MethodPost:
		h.HandleBulkSaveMenuList(w, r)
	case http.MethodPatch:
		h.handleUpdateSingleMenu(w, r)
	case http.MethodDelete:
		h.handleDeleteSingleMenu(w, r)
	default:
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ==========================================================
// プライベートハンドラメソッド
// ==========================================================

// handleGetMenuList メニューリストの取得を処理
func (h *MenuListHandler) handleGetMenuList(w http.ResponseWriter, r *http.Request) {
	// storeID をクエリパラメータから取得
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// データ取得
	menuListItems, err := h.menuRepo.GetMenuListData(storeID)
	if err != nil {
		log.Printf("Failed to fetch menu list: %v", err)
		utils.RespondWithError(w, "Failed to fetch menu list", http.StatusInternalServerError)
		return
	}

	// 各メニューアイテムをマップに変換
	var response []map[string]interface{}
	for _, item := range menuListItems {
		menuItem := map[string]interface{}{
			"id":                       item.ID.Hex(),
			"store_id":                 item.StoreID,
			"menu_id":                  item.MenuID,
			"category":                 item.Category,
			"title":                    item.Title,
			"description":              item.Description,
			"price":                    item.Price,
			"menu_image_url":           item.MenuImageURL,
			"created_at":               item.CreatedAt,
			"updated_at":               item.UpdatedAt,
			"menu_status":              item.MenuStatus,
			"is_pre_order_available":   item.IsPreOrderAvailable,
			"title_translations":       item.TitleTranslations,
			"description_translations": item.DescriptionTranslations,
			"category_translations":    item.CategoryTranslations,
		}
		response = append(response, menuItem)
	}

	// JSON 応答
	utils.RespondWithJSON(w, response, http.StatusOK)
}

// verifyMenuEditPermission 権限チェック用ヘルパー
func (h *MenuListHandler) verifyMenuEditPermission(r *http.Request, storeID string) error {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return fmt.Errorf("Authorization header is required")
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	firebaseUID, err := h.authSvc.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		return fmt.Errorf("Invalid or expired token")
	}

	user, err := h.userRepo.GetByFirebaseUID(firebaseUID)
	if err != nil || user == nil {
		return fmt.Errorf("User not found")
	}

	// 権限チェック: マネージャーまたは「menu_edit」権限を持つスタッフ
	hasPermission, err := h.userRepo.CheckStorePermission(user.ID, storeID, user.Role, "menu_edit")
	if err != nil {
		return err
	}
	if !hasPermission {
		return fmt.Errorf("修正権限がありません。")
	}
	return nil
}

// handleUpdateSingleMenu 単一メニューの更新を処理
func (h *MenuListHandler) handleUpdateSingleMenu(w http.ResponseWriter, r *http.Request) {
	var menuData map[string]interface{}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		utils.RespondWithError(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes)) // Restore

	if err := json.Unmarshal(bodyBytes, &menuData); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	storeID, ok := menuData["store_id"].(string)
	if !ok || storeID == "" {
		utils.RespondWithError(w, "store_id is required", http.StatusBadRequest)
		return
	}

	if err := h.verifyMenuEditPermission(r, storeID); err != nil {
		utils.RespondWithError(w, err.Error(), http.StatusForbidden)
		return
	}

	updatedMenu, err := h.menuRepo.UpdateSingleMenu(menuData)
	if err != nil {
		log.Printf("Failed to update menu: %v", err)
		utils.RespondWithError(w, "Failed to update menu", http.StatusInternalServerError)
		return
	}

	menuItem := map[string]interface{}{
		"id":                       updatedMenu.ID.Hex(),
		"store_id":                 updatedMenu.StoreID,
		"menu_id":                  updatedMenu.MenuID,
		"category":                 updatedMenu.Category,
		"title":                    updatedMenu.Title,
		"description":              updatedMenu.Description,
		"price":                    updatedMenu.Price,
		"menu_image_url":           updatedMenu.MenuImageURL,
		"created_at":               updatedMenu.CreatedAt,
		"updated_at":               updatedMenu.UpdatedAt,
		"menu_status":              updatedMenu.MenuStatus,
		"is_pre_order_available":   updatedMenu.IsPreOrderAvailable,
		"title_translations":       updatedMenu.TitleTranslations,
		"description_translations": updatedMenu.DescriptionTranslations,
		"category_translations":    updatedMenu.CategoryTranslations,
	}

	utils.RespondWithJSON(w, menuItem, http.StatusOK)
}

// handleDeleteSingleMenu 単一メニューの削除を処理
func (h *MenuListHandler) handleDeleteSingleMenu(w http.ResponseWriter, r *http.Request) {
	menuID := r.URL.Query().Get("id")
	storeID := r.URL.Query().Get("store_id")

	if menuID == "" {
		utils.RespondWithError(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// 権限チェック
	if err := h.verifyMenuEditPermission(r, storeID); err != nil {
		utils.RespondWithError(w, err.Error(), http.StatusForbidden)
		return
	}

	// 削除実行
	if err := h.menuRepo.DeleteSingleMenu(menuID); err != nil {
		log.Printf("Failed to delete menu: %v", err)
		utils.RespondWithError(w, "Failed to delete menu", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]interface{}{"success": true}, http.StatusOK)
}

// HandleBulkSaveMenuList メニューリストの一括保存を処理
func (h *MenuListHandler) HandleBulkSaveMenuList(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	if err := h.verifyMenuEditPermission(r, storeID); err != nil {
		utils.RespondWithError(w, err.Error(), http.StatusForbidden)
		return
	}

	var menuData []map[string]interface{}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&menuData); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	insertedItems, err := h.menuRepo.InsertMenuListData(storeID, menuData)
	if err != nil {
		log.Printf("Failed to insert menu items: %v", err)
		utils.RespondWithError(w, "Failed to insert menu items", http.StatusInternalServerError)
		return
	}

	var response []map[string]interface{}
	for _, item := range insertedItems {
		menuItem := map[string]interface{}{
			"id":                       item.ID.Hex(),
			"store_id":                 item.StoreID,
			"menu_id":                  item.MenuID,
			"category":                 item.Category,
			"title":                    item.Title,
			"description":              item.Description,
			"price":                    item.Price,
			"menu_image_url":           item.MenuImageURL,
			"created_at":               item.CreatedAt,
			"updated_at":               item.UpdatedAt,
			"menu_status":              item.MenuStatus,
			"is_pre_order_available":   item.IsPreOrderAvailable,
			"title_translations":       item.TitleTranslations,
			"description_translations": item.DescriptionTranslations,
			"category_translations":    item.CategoryTranslations,
		}
		response = append(response, menuItem)
	}

	utils.RespondWithJSON(w, response, http.StatusOK)
}

// HandleBulkUpdateCategory カテゴリー名の一括変更を処理
func (h *MenuListHandler) HandleBulkUpdateCategory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	if err := h.verifyMenuEditPermission(r, storeID); err != nil {
		utils.RespondWithError(w, err.Error(), http.StatusForbidden)
		return
	}

	var reqBody struct {
		OldCategory          string            `json:"old_category"`
		NewCategory          string            `json:"new_category"`
		CategoryTranslations map[string]string `json:"category_translations"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&reqBody); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if reqBody.OldCategory == "" || reqBody.NewCategory == "" {
		utils.RespondWithError(w, "old_category and new_category are required", http.StatusBadRequest)
		return
	}

	modifiedCount, err := h.menuRepo.BulkUpdateMenuCategory(storeID, reqBody.OldCategory, reqBody.NewCategory, reqBody.CategoryTranslations)
	if err != nil {
		log.Printf("Failed to bulk update categories: %v", err)
		utils.RespondWithError(w, "Failed to bulk update categories", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]interface{}{
		"success":        true,
		"modified_count": modifiedCount,
	}, http.StatusOK)
}

// HandleBulkDeleteCategory カテゴリーの全メニュ一括削除 (disable)
func (h *MenuListHandler) HandleBulkDeleteCategory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	if err := h.verifyMenuEditPermission(r, storeID); err != nil {
		utils.RespondWithError(w, err.Error(), http.StatusForbidden)
		return
	}

	var reqBody struct {
		Category string `json:"category"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&reqBody); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if reqBody.Category == "" {
		utils.RespondWithError(w, "category is required", http.StatusBadRequest)
		return
	}

	modifiedCount, err := h.menuRepo.BulkDeleteMenuCategory(storeID, reqBody.Category)
	if err != nil {
		log.Printf("Failed to bulk delete category menus: %v", err)
		utils.RespondWithError(w, "Failed to bulk delete category menus", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]interface{}{
		"success":        true,
		"modified_count": modifiedCount,
	}, http.StatusOK)
}

// HandleBulkDeleteAllMenus ストアの全メニュ一括削除 (disable)
func (h *MenuListHandler) HandleBulkDeleteAllMenus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	if err := h.verifyMenuEditPermission(r, storeID); err != nil {
		utils.RespondWithError(w, err.Error(), http.StatusForbidden)
		return
	}

	modifiedCount, err := h.menuRepo.BulkDeleteAllMenus(storeID)
	if err != nil {
		log.Printf("Failed to bulk delete all menus: %v", err)
		utils.RespondWithError(w, "Failed to bulk delete all menus", http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, map[string]interface{}{
		"success":        true,
		"modified_count": modifiedCount,
	}, http.StatusOK)
}
