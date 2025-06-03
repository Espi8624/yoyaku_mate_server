package handlers

import (
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
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetMenuList handles GET requests for menu lists
func handleGetMenuList(w http.ResponseWriter, r *http.Request) {
	// storeID를 쿼리 파라미터에서 가져오기
	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		http.Error(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// 데이터 가져오기
	menuListItems, err := data.GetMenuListData(storeID)
	if err != nil {
		log.Printf("Failed to fetch menu list: %v", err)
		http.Error(w, "Failed to fetch menu list", http.StatusInternalServerError)
		return
	}

	// 카테고리별로 정리
	categories := make(map[string][]map[string]interface{})
	for _, item := range menuListItems {
		menuItem := map[string]interface{}{
			"menu_id":     item.MenuID,
			"image":       item.ImageURL, // 이미지 URL을 사용
			"title":       item.Title,
			"description": item.Description,
			"price":       item.Price,
			"created_at":  item.CreatedAt,
			"updated_at":  item.UpdatedAt,
			"menu_status": item.MenuStatus,
		}
		categories[item.Category] = append(categories[item.Category], menuItem)
	}

	// JSON 응답
	utils.RespondWithJSON(w, categories, http.StatusOK)
}
