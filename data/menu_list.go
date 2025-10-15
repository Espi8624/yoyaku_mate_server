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
func GetMenuListData(storeID string) ([]models.MenuList, error) {
	// storeID = "store-001"
	collection := db.GetCollection(DatabaseName, CollectionMenuList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// storeID 検証
	if storeID == "" {
		return nil, fmt.Errorf("store_id is required")
	}

	var menuListItems []models.MenuList

	// menu_status が "disable" ではないデータのみ照会
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

	// 結果を MenuList 構造体に変換
	for cursor.Next(ctx) {
		var item models.MenuList
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

// InsertMenuListData は、メニューデータの一括挿入またはアップサートを処理
func InsertMenuListData(storeID string, menuData []map[string]interface{}) ([]models.MenuList, error) {
	collection := db.GetCollection(DatabaseName, CollectionMenuList)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if storeID == "" {
		return nil, fmt.Errorf("store_id is required")
	}

	// bulkWrite モデルを格納するスライス
	var writeModels []mongo.WriteModel
	var insertedItems []models.MenuList

	// バッチサイズ設定（例：1000個単位）
	batchSize := 1000
	for i, item := range menuData {
		// _id が存在しない場合は新しい ObjectID を生成
		var id primitive.ObjectID
		if idStr := getStringValue(item, "id"); idStr != "" {
			var err error
			id, err = primitive.ObjectIDFromHex(idStr)
			if err != nil {
				log.Printf("Invalid ObjectID format for id %s, generating new one", idStr)
				id = primitive.NewObjectID()
			}
		} else {
			id = primitive.NewObjectID() // 新しい ObjectID 生成
		}

		// upsert するためのアップデート文書を準備
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

		// bulkWrite モデル追加
		writeModel := mongo.NewUpdateOneModel().
			SetFilter(bson.M{"_id": id}).
			SetUpdate(update).
			SetUpsert(true)
		writeModels = append(writeModels, writeModel)

		// バッチサイズに達したら bulkWrite を実行
		if (i+1)%batchSize == 0 || i == len(menuData)-1 {
			_, err := collection.BulkWrite(ctx, writeModels) // result, err
			if err != nil {
				log.Printf("Failed to bulk write menu items: %v", err)
				// 一部失敗しても処理を続行
				continue
			}

			// 成功項目処理 (upsert された ID を基に照会)
			for _, model := range writeModels {
				if updateModel, ok := model.(*mongo.UpdateOneModel); ok {
					id := updateModel.Filter.(bson.M)["_id"].(primitive.ObjectID)
					var menuItem models.MenuList
					err := collection.FindOne(ctx, bson.M{"_id": id}).Decode(&menuItem)
					if err == nil {
						menuItem.ID = id
						insertedItems = append(insertedItems, menuItem)
					}
				}
			}
			writeModels = []mongo.WriteModel{}
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

// 時間文字列をパースし、標準形式に変換
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
			return parsedTime.Format(time.RFC3339) // 標準形式に変換
		}
	}

	log.Printf("Failed to parse time string '%s', using current time", timeStr)
	return time.Now().Format(time.RFC3339)
}

// 文字列値を取得し、存在しない場合は空文字列を返却
func getStringValue(item map[string]interface{}, key string) string {
	if val, ok := item[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
		log.Printf("Value for key '%s' is not a string: %v", key, val)
	}
	return ""
}

// 整数値を取得し、存在しない場合は0を返却
func getIntValue(item map[string]interface{}, key string) int {
	if val, ok := item[key]; ok {
		switch v := val.(type) {
		case float64:
			return int(v)
		case int:
			return v
		case string:
			// 文字列から整数に変換
			if num, err := strconv.Atoi(v); err == nil {
				return num
			}
		}
		log.Printf("Value for key '%s' cannot be converted to int: %v", key, val)
	}
	return 0
}

func UpdateMenuImageURL(menuID string, imageURL string) (*models.MenuList, error) {
	collection := db.GetCollection(DatabaseName, CollectionMenuList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	objID, err := primitive.ObjectIDFromHex(menuID)
	if err != nil {
		return nil, fmt.Errorf("invalid menu ID format: %w", err)
	}

	filter := bson.M{"_id": objID}

	update := bson.M{
		"$set": bson.M{
			"image_url":  imageURL,
			"updated_at": time.Now().Format(time.RFC3339),
		},
	}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return nil, fmt.Errorf("failed to execute update on menu_list: %w", err)
	}

	if result.MatchedCount == 0 {
		return nil, fmt.Errorf("no menu found with ID: %s", menuID)
	}

	var updatedMenu models.MenuList
	err = collection.FindOne(ctx, filter).Decode(&updatedMenu)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated menu document: %w", err)
	}

	return &updatedMenu, nil
}
