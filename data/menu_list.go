package data

import (
	"context"
	"log"
	"strconv"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
)

// メニューデータ取得
func GetMenuListData(storeID string) ([]models.MenuListItem, error) {
	collection := db.GetCollection("yoyaku_mate_provider_db", "menu_list")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 店 ID でデータ取得
	storeID = "store-001" // 任意データ
	var menuListItems []models.MenuListItem

	filter := bson.M{"store_id": storeID}
	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	// 結果を MenuListItem 構造体に変換
	for cursor.Next(ctx) {
		var item models.MenuListItem
		if err := cursor.Decode(&item); err != nil {
			return nil, err
		}
		menuListItems = append(menuListItems, item)
	}

	if err := cursor.Err(); err != nil {
		log.Printf("Cursor error: %v", err)
		return nil, err
	}

	return menuListItems, nil
}

// InsertMenuListData inserts menu list data into the database
func InsertMenuListData(storeID string, menuData map[string][]map[string]interface{}) error {
	collection := db.GetCollection("yoyaku_mate_provider_db", "menu_list")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for category, items := range menuData {
		for _, item := range items {
			// 안전한 타입 변환 함수들
			createdAtStr := getStringValue(item, "created_at")
			updatedAtStr := getStringValue(item, "updated_at")
			title := getStringValue(item, "title")
			description := getStringValue(item, "description")
			imageURL := getStringValue(item, "image")
			price := getIntValue(item, "price")

			// 필수 필드 검증
			if title == "" {
				log.Printf("Title is empty for item in category %s, skipping", category)
				continue
			}

			menuItem := models.MenuListItem{
				StoreID:     storeID,
				Category:    category,
				Title:       title,
				Description: description,
				Price:       price,
				ImageURL:    imageURL,
				CreatedAt:   parseTimeToString(createdAtStr),
				UpdatedAt:   parseTimeToString(updatedAtStr),
			}

			_, err := collection.InsertOne(ctx, menuItem)
			if err != nil {
				log.Printf("Failed to insert menu item: %v", err)
				return err
			}
		}
	}

	return nil
}

// parseTimeToString converts a string to time.Time then back to string (ISO format)
func parseTimeToString(timeStr string) string {
	if timeStr == "" {
		return time.Now().Format(time.RFC3339)
	}

	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"2006/01/02 15:04:05",
		"2006/01/02",
	}

	for _, format := range formats {
		if parsedTime, err := time.Parse(format, timeStr); err == nil {
			return parsedTime.Format(time.RFC3339) // 표준 형식으로 반환
		}
	}

	log.Printf("Failed to parse time string '%s', using current time", timeStr)
	return time.Now().Format(time.RFC3339)
}

// 헬퍼 함수들
func getStringValue(item map[string]interface{}, key string) string {
	if val, ok := item[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
		log.Printf("Value for key '%s' is not a string: %v", key, val)
	}
	return ""
}

func getIntValue(item map[string]interface{}, key string) int {
	if val, ok := item[key]; ok {
		switch v := val.(type) {
		case float64:
			return int(v)
		case int:
			return v
		case string:
			// 문자열로 된 숫자 파싱 시도
			if num, err := strconv.Atoi(v); err == nil {
				return num
			}
		}
		log.Printf("Value for key '%s' cannot be converted to int: %v", key, val)
	}
	return 0
}
