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

// GetUserByFirebaseUID retrieves a user by their Firebase UID
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

// CheckUserStorePermission checks if a user has permission to access a store
// For managers: checks if they own the store
// For staff: checks if they are APPROVED for the store
func CheckUserStorePermission(userID primitive.ObjectID, storeID string, role string) (bool, error) {
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

	// スタッフの場合、APPROVED状態かどうかを確認
	return CheckStaffApprovalStatus(userID, storeID)
}
