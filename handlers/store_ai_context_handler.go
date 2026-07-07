package handlers

import (
	"fmt"
	"log"
	"net/http"
	"time"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"
	"yoyaku_mate_server/utils"
)

// StoreAIContextResponse AIに注入する店舗コンテキスト情報構造体
type StoreAIContextResponse struct {
	StoreName               string                          `json:"store_name"`
	Phone                   string                          `json:"phone"`
	Address                 string                          `json:"address"`
	OpeningHours            string                          `json:"opening_hours"`       // 表示用文字列 (簡易版)
	CurrentWaitCount        int                             `json:"current_wait_count"`  // 現在の待機組数
	EstimatedWaitTime       int                             `json:"estimated_wait_time"` // 予想待機時間 (分)
	MaxCapacity             int                             `json:"max_capacity"`        // 最大待機可能組数
	LastUpdated             string                          `json:"last_updated"`
	Menus                   []models.MenuList               `json:"menus"`                       // メニューリスト
	OperatingHoursMap       map[string]models.StoreDayHours `json:"operating_hours_map"`         // 詳細な営業時間設定
	ClosedDays              models.ClosedDays               `json:"closed_days"`                 // 定休日設定
	RequireOneMenuPerPerson bool                            `json:"require_one_menu_per_person"` // 1人1メニュー制
	AIAdditionalInfo        string                          `json:"ai_additional_info"`          // AIへの追加情報
}

// StoreAIContextHandler AIチャットボット用リアルタイム店舗情報提供ハンドラ
// GET /api/public/store_ai_context?store_id=xxx
func StoreAIContextHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.RespondWithError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	storeID := r.URL.Query().Get("store_id")
	if storeID == "" {
		utils.RespondWithError(w, "Missing store_id parameter", http.StatusBadRequest)
		return
	}

	// 1. 店舗基本情報取得
	store, err := data.GetStoreData(storeID)
	if err != nil {
		utils.RespondWithError(w, "Store not found", http.StatusNotFound)
		return
	}

	// 2. 店舗設定情報取得 (最大待機人数、組あたりの待機時間、営業時間、定休日)
	settings, err := data.GetStoreSettingsData(storeID)
	minutesPerTeam := 10 // デフォルト値
	maxCapacity := 0     // 0なら無制限
	formattedHours := ""
	var operatingHoursMap map[string]models.StoreDayHours
	var closedDays models.ClosedDays
	var requireOneMenuPerPerson bool
	var aiAdditionalInfo string

	if err == nil {
		if settings.Settings.WaitingPolicy.EstimatedWaitTime > 0 {
			minutesPerTeam = settings.Settings.WaitingPolicy.EstimatedWaitTime
		}
		maxCapacity = settings.Settings.WaitingPolicy.MaxWaitingCount

		// 詳細な営業時間と定休日データを取得
		operatingHoursMap = settings.Settings.OperatingHours
		closedDays = settings.Settings.ClosedDays
		requireOneMenuPerPerson = settings.Settings.WaitingPolicy.RequireOneMenuPerPerson // 設定値を反映
		aiAdditionalInfo = settings.Settings.AIAdditionalInfo                             // 追加情報を反映

		// 営業時間のフォーマット (簡易表示用)
		if len(settings.Settings.OperatingHours) > 0 {
			daysOrder := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
			for _, day := range daysOrder {
				if hours, ok := settings.Settings.OperatingHours[day]; ok {
					if hours.Start != "" && hours.End != "" {
						formattedHours += fmt.Sprintf("%s: %s-%s, ", day, hours.Start, hours.End)
					}
				}
			}
		}
	}

	// 3. 現在の待機リストを取得して待機組数を計算
	// 効率のためにCountDocumentsクエリを使用することもできますが、
	// 既存ロジックとの一貫性のためにGetWaitingListDataを使用してフィルタリングします（データ量が多くないと仮定）
	waitingList, err := data.GetWaitingListData(storeID)
	currentWaitCount := 0
	if err == nil {
		for _, item := range waitingList {
			if item.Status == "waiting" || item.Status == "notified" {
				currentWaitCount++
			}
		}
	}

	// 4. メニューリスト取得 (品切れ情報をリアルタイムに反映するため)
	menuList, err := data.GetMenuListData(storeID)
	if err != nil {
		log.Printf("Failed to fetch menu list for AI context: %v", err)
		menuList = []models.MenuList{} // エラー時は空リスト
	}

	// 5. 予想待機時間計算
	totalEstimatedTime := currentWaitCount * minutesPerTeam

	// 6. レスポンス構成
	finalOpeningHours := store.OpeningHours
	if finalOpeningHours == "" {
		finalOpeningHours = formattedHours
	}

	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	response := StoreAIContextResponse{
		StoreName:               store.StoreName,
		Phone:                   store.Phone,
		Address:                 store.Address,
		OpeningHours:            finalOpeningHours,
		CurrentWaitCount:        currentWaitCount,
		EstimatedWaitTime:       totalEstimatedTime,
		MaxCapacity:             maxCapacity,
		LastUpdated:             time.Now().In(jst).Format(time.RFC3339),
		Menus:                   menuList,
		OperatingHoursMap:       operatingHoursMap,
		ClosedDays:              closedDays,
		RequireOneMenuPerPerson: requireOneMenuPerPerson,
		AIAdditionalInfo:        aiAdditionalInfo,
	}

	// 7. JSONレスポンス送信
	utils.RespondWithJSON(w, response, http.StatusOK)
	log.Printf("[AI Context] Served context for store %s (Wait: %d, Time: %dmin, Menus: %d)", storeID, currentWaitCount, totalEstimatedTime, len(menuList))
}
