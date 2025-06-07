package data

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// メニューデータ取得
// GetMenuListData retrieves menu list data from the database
func GetMenuListData(storeID string) ([]models.MenuListItem, error) {
	storeID = "store-001"
	collection := db.GetCollection("yoyaku_mate_provider_db", "menu_list")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// storeID 검증
	if storeID == "" {
		return nil, fmt.Errorf("store_id is required")
	}

	var menuListItems []models.MenuListItem

	// menu_status가 "disable"이 아닌 데이터만 조회
	filter := bson.M{
		"store_id":    storeID,
		"menu_status": bson.M{"$ne": "disable"},
	}
	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		log.Printf("Failed to find menu items for storeID %s: %v", storeID, err)
		return nil, err
	}
	defer cursor.Close(ctx)

	// 결과를 MenuListItem 구조체로 변환
	for cursor.Next(ctx) {
		var item models.MenuListItem
		if err := cursor.Decode(&item); err != nil {
			log.Printf("Failed to decode menu item: %v", err)
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

// InsertMenuListData handles bulk insertion or upsert of menu data
func InsertMenuListData(storeID string, menuData []map[string]interface{}) ([]models.MenuListItem, error) {
	collection := db.GetCollection("yoyaku_mate_provider_db", "menu_list")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if storeID == "" {
		return nil, fmt.Errorf("store_id is required")
	}

	// bulkWrite를 위한 모델 리스트
	var writeModels []mongo.WriteModel
	var insertedItems []models.MenuListItem

	// 배치 크기 설정 (예: 1000개 단위)
	batchSize := 1000
	for i, item := range menuData {
		// _id 처리: 비어 있으면 ObjectID 생성
		var id primitive.ObjectID
		if idStr := getStringValue(item, "id"); idStr != "" {
			var err error
			id, err = primitive.ObjectIDFromHex(idStr)
			if err != nil {
				log.Printf("Invalid ObjectID format for id %s, generating new one", idStr)
				id = primitive.NewObjectID()
			}
		} else {
			id = primitive.NewObjectID() // 새 ObjectID 생성
		}

		// upsert를 위한 업데이트 문서 준비
		update := bson.M{
			"$set": bson.M{
				"store_id":    storeID,
				"menu_id":     getStringValue(item, "menuId"),
				"category":    getStringValue(item, "category"),
				"title":       getStringValue(item, "title"),
				"description": getStringValue(item, "description"),
				"price":       getIntValue(item, "price"),
				"image":       getStringValue(item, "image"),
				"updated_at":  parseTimeToString(getStringValue(item, "updatedAt")),
				"menu_status": getStringValue(item, "menuStatus"),
			},
			"$setOnInsert": bson.M{
				"created_at": parseTimeToString(getStringValue(item, "createdAt")),
			},
		}

		// bulkWrite 모델 추가
		writeModel := mongo.NewUpdateOneModel().
			SetFilter(bson.M{"_id": id}).
			SetUpdate(update).
			SetUpsert(true)
		writeModels = append(writeModels, writeModel)

		// 배치 크기에 도달하면 bulkWrite 실행
		if (i+1)%batchSize == 0 || i == len(menuData)-1 {
			_, err := collection.BulkWrite(ctx, writeModels) // result, err
			if err != nil {
				log.Printf("Failed to bulk write menu items: %v", err)
				// 일부 실패 시에도 진행 (선택 사항: 전체 롤백 가능)
				continue
			}

			// 성공한 항목 처리 (Upsert된 ID 기반으로 조회)
			for _, model := range writeModels {
				if updateModel, ok := model.(*mongo.UpdateOneModel); ok {
					id := updateModel.Filter.(bson.M)["_id"].(primitive.ObjectID)
					var menuItem models.MenuListItem
					err := collection.FindOne(ctx, bson.M{"_id": id}).Decode(&menuItem)
					if err == nil {
						menuItem.ID = id // 문자열로 변환하여 응답
						insertedItems = append(insertedItems, menuItem)
					}
				}
			}
			writeModels = []mongo.WriteModel{} // 배치 초기화
		}
	}

	return insertedItems, nil
}

// // getFloatValue safely converts a value to float64
// func getFloatValue(item map[string]interface{}, key string) float64 {
// 	if val, ok := item[key]; ok {
// 		switch v := val.(type) {
// 		case float64:
// 			return v
// 		case int:
// 			return float64(v)
// 		case string:
// 			if num, err := strconv.ParseFloat(v, 64); err == nil {
// 				return num
// 			}
// 		}
// 		log.Printf("Value for key '%s' cannot be converted to float64: %v", key, val)
// 	}
// 	return 0
// }

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
