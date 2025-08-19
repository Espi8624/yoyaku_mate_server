package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"
)

// メニューリストのリクエストを処理
func MenuListHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleGetMenuList(w, r)
	case http.MethodPost:
		handleBulkSaveMenuList(w, r)
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
			"id":          item.ID.Hex(),
			"storeId":     item.StoreID,
			"menuId":      item.MenuID,
			"category":    item.Category,
			"title":       item.Title,
			"description": item.Description,
			"price":       item.Price,
			"image":       item.ImageURL,
			"createdAt":   item.CreatedAt,
			"updatedAt":   item.UpdatedAt,
			"menu_status": item.MenuStatus,
		}
		response = append(response, menuItem)
	}

	// JSON 応答
	utils.RespondWithJSON(w, response, http.StatusOK)
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
			"id":          item.ID,
			"storeId":     item.StoreID,
			"menuId":      item.MenuID,
			"category":    item.Category,
			"title":       item.Title,
			"description": item.Description,
			"price":       item.Price,
			"image":       item.ImageURL,
			"createdAt":   item.CreatedAt,
			"updatedAt":   item.UpdatedAt,
			"menu_status": item.MenuStatus,
		}
		response = append(response, menuItem)
	}

	utils.RespondWithJSON(w, response, http.StatusOK)
}
