package data

import (
	"context"
	"fmt"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

func GetStoresByStatus(status string) ([]models.StoreWithLicense, error) {
	licenseCollection := db.GetCollection(DatabaseName, "store_license")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pipeline := mongo.Pipeline{}

	if status != "" {
		matchStage := bson.D{{Key: "$match", Value: bson.D{{Key: "verification_status", Value: status}}}}
		pipeline = append(pipeline, matchStage)
	}

	lookupStage := bson.D{{Key: "$lookup", Value: bson.D{
		{Key: "from", Value: "store_info"},
		{Key: "localField", Value: "store_id"},
		{Key: "foreignField", Value: "store_id"},
		{Key: "as", Value: "storeDetails"},
	}}}
	pipeline = append(pipeline, lookupStage)

	unwindStage := bson.D{{Key: "$unwind", Value: bson.M{
		"path":                       "$storeDetails",
		"preserveNullAndEmptyArrays": true,
	}}}
	pipeline = append(pipeline, unwindStage)

	// User Info Lookup
	userLookupStage := bson.D{{Key: "$lookup", Value: bson.D{
		{Key: "from", Value: "user_info"}, // CollectionUserInfo constant would be better but string works
		{Key: "localField", Value: "storeDetails.user_id"},
		{Key: "foreignField", Value: "_id"},
		{Key: "as", Value: "userDetails"},
	}}}
	pipeline = append(pipeline, userLookupStage)

	userUnwindStage := bson.D{{Key: "$unwind", Value: bson.M{
		"path":                       "$userDetails",
		"preserveNullAndEmptyArrays": true,
	}}}
	pipeline = append(pipeline, userUnwindStage)

	projectStage := bson.D{{Key: "$project", Value: bson.D{
		{Key: "store_id", Value: "$store_id"},
		{Key: "store_name", Value: "$storeDetails.store_name"},
		{Key: "address", Value: "$storeDetails.address"},
		{Key: "phone", Value: "$storeDetails.phone"},
		{Key: "license_image_url", Value: "$license_image_url"},
		{Key: "verification_status", Value: "$verification_status"},
		{Key: "created_at", Value: "$created_at"},
		{Key: "user_name", Value: "$userDetails.user_name"},
		{Key: "user_email", Value: "$userDetails.email"},
		{Key: "user_phone", Value: "$userDetails.phone"},
		{Key: "_id", Value: 0},
	}}}
	pipeline = append(pipeline, projectStage)

	sortStage := bson.D{{Key: "$sort", Value: bson.D{{Key: "created_at", Value: -1}}}}
	pipeline = append(pipeline, sortStage)

	// Aggregation実行
	cursor, err := licenseCollection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []models.StoreWithLicense
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	return results, nil
}

func UpdateLicenseStatus(storeID string, status string, comment string) error {
	licenseCollection := db.GetCollection(DatabaseName, "store_license")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"store_id": storeID}

	if status != models.StatusApproved && status != models.StatusRejected {
		return fmt.Errorf("invalid status provided: %s", status)
	}

	update := bson.M{
		"$set": bson.M{
			"verification_status": status,
			"admin_comment":       comment,
			"updated_at":          time.Now(),
		},
	}

	result, err := licenseCollection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to execute update on store_license: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("no license information found for store ID: %s", storeID)
	}

	return nil
}
