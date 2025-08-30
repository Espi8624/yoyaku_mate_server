package data

import (
	"context"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
)

// GetStoreLicenseByStoreID는 store_id를 사용하여 가게 인증 정보를 조회합니다.
func GetStoreLicenseByStoreID(storeID string) (*models.StoreLicense, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreLicense)
	filter := bson.M{"store_id": storeID}

	var license models.StoreLicense
	err := collection.FindOne(context.Background(), filter).Decode(&license)
	if err != nil {
		return nil, err
	}

	return &license, nil
}
