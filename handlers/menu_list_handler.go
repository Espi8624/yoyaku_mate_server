package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"

	"github.com/gorilla/mux"
)

// メニューリストのリクエストを処理
func MenuListHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleGetMenuList(w, r)
	case http.MethodPost:
		handleBulkSaveMenuList(w, r)
	case http.MethodPatch:
		handleUpdateSingleMenu(w, r)
	default:
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// メニューリストの取得を処理
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
			"id":             item.ID.Hex(),
			"store_id":       item.StoreID,
			"menu_id":        item.MenuID,
			"category":       item.Category,
			"title":          item.Title,
			"description":    item.Description,
			"price":          item.Price,
			"menu_image_url": item.MenuImageURL,
			"created_at":     item.CreatedAt,
			"updated_at":     item.UpdatedAt,
			"menu_status":    item.MenuStatus,
		}
		response = append(response, menuItem)
	}

	// JSON 応答
	utils.RespondWithJSON(w, response, http.StatusOK)
}

// 単一メニューの更新を処理
func handleUpdateSingleMenu(w http.ResponseWriter, r *http.Request) {
	var menuData map[string]interface{}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&menuData); err != nil {
		utils.RespondWithError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	updatedMenu, err := data.UpdateSingleMenu(menuData)
	if err != nil {
		log.Printf("Failed to update menu: %v", err)
		utils.RespondWithError(w, "Failed to update menu", http.StatusInternalServerError)
		return
	}

	menuItem := map[string]interface{}{
		"id":             updatedMenu.ID.Hex(),
		"store_id":       updatedMenu.StoreID,
		"menu_id":        updatedMenu.MenuID,
		"category":       updatedMenu.Category,
		"title":          updatedMenu.Title,
		"description":    updatedMenu.Description,
		"price":          updatedMenu.Price,
		"menu_image_url": updatedMenu.MenuImageURL,
		"created_at":     updatedMenu.CreatedAt,
		"updated_at":     updatedMenu.UpdatedAt,
		"menu_status":    updatedMenu.MenuStatus,
	}

	utils.RespondWithJSON(w, menuItem, http.StatusOK)
}

// メニューリストの一括保存を処理
func handleBulkSaveMenuList(w http.ResponseWriter, r *http.Request) {
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
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
			"id":             item.ID.Hex(),
			"store_id":       item.StoreID,
			"menu_id":        item.MenuID,
			"category":       item.Category,
			"title":          item.Title,
			"description":    item.Description,
			"price":          item.Price,
			"menu_image_url": item.MenuImageURL,
			"created_at":     item.CreatedAt,
			"updated_at":     item.UpdatedAt,
			"menu_status":    item.MenuStatus,
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
