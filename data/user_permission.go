package data

import (
	"context"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// GetUserByFirebaseUID Firebase UIDを使用してユーザーを取得
func GetUserByFirebaseUID(firebaseUID string) (*models.User, error) {
	collection := db.GetCollection(DatabaseName, CollectionUserInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var user models.User
	err := collection.FindOne(ctx, bson.M{"firebase_uid": firebaseUID}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // User not found
		}
		return nil, err
	}

	return &user, nil
}

// CheckUserStorePermission ユーザーが店舗にアクセスする権限があるかを確認
// マネージャーの場合: 店舗の所有者かどうかを確認
// スタッフの場合: 店舗でAPPROVED状態であり、かつ必要な権限を持っているかを確認(指定されている場合)
func CheckUserStorePermission(userID primitive.ObjectID, storeID string, role string, requiredPermission string) (bool, error) {
	if role == "manager" {
		// マネージャーの場合、店舗の所有者かどうかを確認
		storeCollection := db.GetCollection(DatabaseName, CollectionStoreInfo)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var store models.Store
		err := storeCollection.FindOne(ctx, bson.M{
			"store_id": storeID,
			"user_id":  userID,
		}).Decode(&store)

		if err != nil {
			if err == mongo.ErrNoDocuments {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}

	// スタッフの場合
	// 1. APPROVED状態かどうかを確認
	approved, err := CheckStaffApprovalStatus(userID, storeID)
	if err != nil || !approved {
		return false, err
	}

	// 2. 権限チェック (requiredPermissionが指定されている場合)
	if requiredPermission != "" {
		collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var staffInfo models.StoreStaffInfo
		err := collection.FindOne(ctx, bson.M{
			"user_id":  userID,
			"store_id": storeID,
		}).Decode(&staffInfo)

		if err != nil {
			return false, err
		}

		hasPermission := false
		for _, p := range staffInfo.Permissions {
			if p == requiredPermission {
				hasPermission = true
				break
			}
		}
		return hasPermission, nil
	}

	return true, nil
}
