package data

import (
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"golang.org/x/net/context"
)

// 店舗設定データ取得
func GetStoreSettingsData(storeID string) (models.StoreSettings, error) {
	collection := db.GetCollection("yoyaku_mate_provider_db", "store_settings")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var storeSettings models.StoreSettings
	filter := bson.M{"store_id": storeID}

	err := collection.FindOne(ctx, filter).Decode(&storeSettings)
	if err != nil {
		log.Printf("Failed to fetch store settings: %v", err)
		return models.StoreSettings{}, err
	}

	return storeSettings, nil
}

// store_settings upsert (저장/수정)
func UpsertStoreSettings(storeID string, reqBody map[string]interface{}) error {
	collection := db.GetCollection("yoyaku_mate_provider_db", "store_settings")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"store_id": storeID}
	update := bson.M{"$set": reqBody}
	// Upsert 옵션을 명시적으로 true로 설정하지 않고, UpdateOne만 사용 (기본 라이브러리 스타일)
	_, err := collection.UpdateOne(ctx, filter, update)
	return err
}
