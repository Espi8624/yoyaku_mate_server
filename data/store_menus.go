package data

import (
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"golang.org/x/net/context"
)

// すべてのメニューデータ取得
func GetStoreMenuData(storeID string) ([]models.StoreMenuItem, error) {
	collection := db.GetCollection("yoyaku_mate_db", "store_menus")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var storeMenusData []models.StoreMenuItem
	filter := bson.M{"store_id": storeID}

	// log.Printf("Querying store_info with filter: %v", filter)

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		log.Printf("Failed to fetch store menu data: %v", err)
		return nil, err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var menuItem models.StoreMenuItem
		if err := cursor.Decode(&menuItem); err != nil {
			log.Printf("Failed to decode menu item: %v", err)
			continue
		}
		storeMenusData = append(storeMenusData, menuItem)
	}

	if err := cursor.Err(); err != nil {
		log.Printf("Cursor error: %v", err)
		return nil, err
	}

	return storeMenusData, nil
}
