package data

import (
	"context"
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// store_id で店舗情報を取得
func GetStoreData(storeID string) (*models.Store, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var store models.Store
	filter := bson.M{"store_id": storeID}

	err := collection.FindOne(ctx, filter).Decode(&store)
	if err != nil {
		log.Printf("Failed to fetch store info by store_id '%s': %v", storeID, err)
		// エラー発生時、エラーを返却
		return nil, err
	}

	return &store, nil
}

// 店舗情報を更新
func UpdateStoreData(storeID string, update map[string]interface{}) error {
	collection := db.GetCollection(DatabaseName, CollectionStoreInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"store_id": storeID}
	updateDoc := bson.M{"$set": update}
	_, err := collection.UpdateOne(ctx, filter, updateDoc)
	if err != nil {
		log.Printf("Failed to update store info for store_id '%s': %v", storeID, err)
		return err
	}
	return nil
}

// user_id でユーザー情報を取得し、店舗情報を取得
func GetStoreDataByUserID(userID primitive.ObjectID) (*models.Store, error) {
	// user_id で user_info から使用者情報を照会
	userCollection := db.GetCollection(DatabaseName, CollectionUserInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var user models.User
	userFilter := bson.M{"_id": userID}
	err := userCollection.FindOne(ctx, userFilter).Decode(&user)
	if err != nil {
		// ユーザーが見つからない場合、エラーログを出力し、エラーを返却
		log.Printf("Failed to find user with _id '%s': %v", userID.Hex(), err)
		return nil, err
	}

	// ユーザーが所属する店舗IDを確認
	if user.StoreID == "" {
		// ユーザーが店舗に所属していない場合、エラーログを出力し、エラーを返却
		log.Printf("User with _id '%s' is not associated with any store.", userID.Hex())
		return nil, mongo.ErrNoDocuments
	}

	// ユーザーの店舗IDを使用し、店舗情報を取得
	return GetStoreData(user.StoreID)
}
