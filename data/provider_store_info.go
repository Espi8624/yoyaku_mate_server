package data

import (
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"golang.org/x/net/context"
)

// Provider Store 데이터 조회
func GetProviderStoreData(storeID string) (models.Store, error) {
	collection := db.GetCollection("yoyaku_mate_provider", "store_info")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var store models.Store
	filter := bson.M{"store_id": storeID}

	err := collection.FindOne(ctx, filter).Decode(&store)
	if err != nil {
		log.Printf("Failed to fetch provider store info: %v", err)
		return models.Store{}, err
	}

	return store, nil
}

// Provider Store 데이터 수정
func UpdateProviderStoreData(storeID string, update map[string]interface{}) error {
	collection := db.GetCollection("yoyaku_mate_provider", "store_info")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"store_id": storeID}
	updateDoc := bson.M{"$set": update}
	_, err := collection.UpdateOne(ctx, filter, updateDoc)
	if err != nil {
		log.Printf("Failed to update provider store info: %v", err)
		return err
	}
	return nil
}
