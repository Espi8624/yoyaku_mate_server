package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/utils"
)

// MenuListHandler handles requests for menu lists
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

// handleGetMenuList handles GET requests for menu lists
func handleGetMenuList(w http.ResponseWriter, r *http.Request) {
	// storeID를 쿼리 파라미터에서 가져오기
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// 데이터 가져오기
	menuListItems, err := data.GetMenuListData(storeID)
	if err != nil {
		log.Printf("Failed to fetch menu list: %v", err)
		utils.RespondWithError(w, "Failed to fetch menu list", http.StatusInternalServerError)
		return
	}

	// 평평한 리스트로 변환
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

	// JSON 응답
	utils.RespondWithJSON(w, response, http.StatusOK)
}

// handleBulkSaveMenuList handles POST requests to bulk save menu lists
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
