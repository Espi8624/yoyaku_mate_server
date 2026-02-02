package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"yoyaku_mate_server/auth"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"

	"github.com/gorilla/mux"
)

// MenuListHandler メニューリストリクエストを処理
func MenuListHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleGetMenuList(w, r)
	case http.MethodPost:
		HandleBulkSaveMenuList(w, r)
	case http.MethodPatch:
		handleUpdateSingleMenu(w, r)
	default:
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetMenuList メニューリストの取得を処理
func handleGetMenuList(w http.ResponseWriter, r *http.Request) {
	// storeID をクエリパラメータから取得
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// データ取得
	menuListItems, err := data.GetMenuListData(storeID)
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
		}
		response = append(response, menuItem)
	}

	// JSON 応答
	utils.RespondWithJSON(w, response, http.StatusOK)
}

// verifyMenuEditPermission 権限チェック用ヘルパー
func verifyMenuEditPermission(r *http.Request, storeID string) error {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return fmt.Errorf("Authorization header is required")
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	firebaseUID, err := auth.VerifyIDToken(r.Context(), idToken)
	if err != nil {
		return fmt.Errorf("Invalid or expired token")
	}

	user, err := data.GetUserByFirebaseUID(firebaseUID)
	if err != nil || user == nil {
		return fmt.Errorf("User not found")
	}

	// 権限チェック: マネージャーまたは「menu_edit」権限を持つスタッフ
	hasPermission, err := data.CheckUserStorePermission(user.ID, storeID, user.Role, "menu_edit")
	if err != nil {
		return err
	}
	if !hasPermission {
		return fmt.Errorf("修正権限がありません。")
	}
	return nil
}

// handleUpdateSingleMenu 単一メニューの更新を処理
func handleUpdateSingleMenu(w http.ResponseWriter, r *http.Request) {
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

	if err := verifyMenuEditPermission(r, storeID); err != nil {
		utils.RespondWithError(w, err.Error(), http.StatusForbidden)
		return
	}

	updatedMenu, err := data.UpdateSingleMenu(menuData)
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
	}

	utils.RespondWithJSON(w, menuItem, http.StatusOK)
}

// HandleBulkSaveMenuList メニューリストの一括保存を処理
func HandleBulkSaveMenuList(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	if err := verifyMenuEditPermission(r, storeID); err != nil {
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

	insertedItems, err := data.InsertMenuListData(storeID, menuData)
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
		}
		response = append(response, menuItem)
	}

	utils.RespondWithJSON(w, response, http.StatusOK)
}

func (h *UploadHandler) UploadMenuImage(w http.ResponseWriter, r *http.Request) {
	// menuId取得
	vars := mux.Vars(r)
	menuId := vars["menuId"]

	// 'menuImage'ファイルをマルチパートフォームから取得
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		utils.RespondWithError(w, "Could not parse multipart form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("menuImage")
	if err != nil {
		utils.RespondWithError(w, "Could not get uploaded file named 'menuImage'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	assetsPublicDomain := os.Getenv("R2_ASSETS_PUBLIC_DOMAIN")

	// MinIOにアップロード
	fileURL, err := h.Minio.UploadFile("saboten-assets", assetsPublicDomain, file, header)
	if err != nil {
		log.Printf("Error uploading file to Minio: %v", err)
		utils.RespondWithError(w, "Could not upload file", http.StatusInternalServerError)
		return
	}

	// DBのメニュー情報をアップデート
	updatedMenu, err := data.UpdateMenuImageURL(menuId, fileURL)
	if err != nil {
		log.Printf("Error updating menu image URL in database: %v", err)
		utils.RespondWithError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.RespondWithJSON(w, updatedMenu, http.StatusOK)
}
