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

// CreateStoreStaffInfo creates a new store_staff_info record
func CreateStoreStaffInfo(info models.StoreStaffInfo) error {
	collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	info.CreatedAt = time.Now()
	info.UpdatedAt = time.Now()

	_, err := collection.InsertOne(ctx, info)
	if err != nil {
		log.Printf("Failed to create store_staff_info: %v", err)
		return err
	}
	return nil
}

// CheckStoreStaffExists checks if a user is already associated with a store
func CheckStoreStaffExists(userID primitive.ObjectID, storeID string) (bool, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{
		"user_id":  userID,
		"store_id": storeID,
	}

	count, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		log.Printf("Failed to check store_staff_info existence: %v", err)
		return false, err
	}

	return count > 0, nil
}

// CheckStaffApprovalStatus は、指定されたユーザーが指定された店舗でAPPROVED状態かどうかを確認
func CheckStaffApprovalStatus(userID primitive.ObjectID, storeID string) (bool, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var staffInfo models.StoreStaffInfo
	err := collection.FindOne(ctx, bson.M{
		"user_id":  userID,
		"store_id": storeID,
	}).Decode(&staffInfo)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			return false, nil // スタッフ情報が存在しない
		}
		return false, err
	}

	// APPROVED状態のみtrueを返す
	return staffInfo.Status == models.StaffStatusApproved, nil
}

// GetStoreStaffByStoreID retrieves all staff members for a store with user details
func GetStoreStaffByStoreID(storeID string) ([]map[string]interface{}, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Aggregation pipeline to join with user_info
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"store_id": storeID}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         CollectionUserInfo,
			"localField":   "user_id",
			"foreignField": "_id",
			"as":           "user_details",
		}}},
		{{Key: "$unwind", Value: "$user_details"}},
		{{Key: "$project", Value: bson.M{
			"_id":        1,
			"user_id":    1,
			"store_id":   1,
			"role":       1,
			"status":     1,
			"created_at": 1,
			"updated_at": 1,
			"user_name":  "$user_details.user_name",
			"email":      "$user_details.email",
		}}},
	}

	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		log.Printf("Failed to aggregate store staff: %v", err)
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []map[string]interface{}
	if err = cursor.All(ctx, &results); err != nil {
		log.Printf("Failed to decode aggregation results: %v", err)
		return nil, err
	}

	return results, nil
}

// UpdateStoreStaffStatus updates the status of a staff member
func UpdateStoreStaffStatus(staffID string, status string) error {
	collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	objID, err := primitive.ObjectIDFromHex(staffID)
	if err != nil {
		return err
	}

	update := bson.M{
		"$set": bson.M{
			"status":     status,
			"updated_at": time.Now(),
		},
	}

	_, err = collection.UpdateOne(ctx, bson.M{"_id": objID}, update)
	if err != nil {
		log.Printf("Failed to update store staff status: %v", err)
		return err
	}

	return nil
}
