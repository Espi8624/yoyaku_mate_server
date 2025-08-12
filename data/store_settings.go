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
func GetStoreSettingsData(storeID string) (models.StoreSetting, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreSettings)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var storeSettings models.StoreSetting
	filter := bson.M{"store_id": storeID}

	err := collection.FindOne(ctx, filter).Decode(&storeSettings)
	if err != nil {
		log.Printf("Failed to fetch store settings: %v", err)
		return models.StoreSetting{}, err
	}

	return storeSettings, nil
}

// store_settings upsert (保存/修正)
func UpsertStoreSettings(storeID string, reqBody map[string]interface{}) error {
	collection := db.GetCollection(DatabaseName, CollectionStoreSettings)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"store_id": storeID}
	update := bson.M{"$set": reqBody}
	// upsert オプションを明示的に true に設定せず、UpdateOne のみ使用 (基本ライブラリスタイル)
	_, err := collection.UpdateOne(ctx, filter, update)
	return err
}
