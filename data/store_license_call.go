package data

import (
	"context"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
)

// store_idで店舗認証情報照会
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
