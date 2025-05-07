package data

import (
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"golang.org/x/net/context"
)

// 店情報データ取得
func GetStoreInfoData(storeID int32) (models.StoreInfoItem, error) {
	collection := db.GetCollection("yoyaku_mate_db", "store_info")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var storeInfoData models.StoreInfoItem
	filter := bson.M{"store_id": storeID}

	log.Printf("Querying store_info with filter: %v", filter)

	err := collection.FindOne(ctx, filter).Decode(&storeInfoData)
	if err != nil {
		log.Printf("Failed to fetch store info: %v", err)
		return models.StoreInfoItem{}, err
	}

	return storeInfoData, nil
}
