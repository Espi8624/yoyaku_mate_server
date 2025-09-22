package data

import (
	"context"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// firebase_uidを使用し、該当ユーザーが接近可能なすべての店舗リストを返却
func GetStoresByFirebaseUID(firebaseUid string) ([]models.Store, error) {
	userCollection := db.GetCollection(DatabaseName, "user_info")
	storeCollection := db.GetCollection(DatabaseName, "store_info")
	ctx := context.Background()

	var user models.User
	err := userCollection.FindOne(ctx, bson.M{"firebase_uid": firebaseUid}).Decode(&user)
	if err != nil {
		return nil, err
	}

	var stores []models.Store

	switch user.Role {
	case "manager":
		cursor, err := storeCollection.Find(ctx, bson.M{"user_id": user.ID})
		if err != nil {
			return nil, err
		}
		defer cursor.Close(ctx)

		if err = cursor.All(ctx, &stores); err != nil {
			return nil, err
		}

	// 職員の場合、該当職員が所属する店舗のみを返却
	case "staff":
		var store models.Store
		err := storeCollection.FindOne(ctx, bson.M{"store_id": user.StoreID}).Decode(&store)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				return []models.Store{}, nil
			}
			return nil, err
		}
		stores = append(stores, store)
	}

	return stores, nil
}
