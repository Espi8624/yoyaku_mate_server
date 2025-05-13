package data

import (
	"context"
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
)

func GetStoreCommentData(storeID string) ([]models.StoreCommentItem, error) {
	collection := db.GetCollection("yoyaku_mate_db", "store_comments")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var storeCommentsData []models.StoreCommentItem
	filter := bson.M{"store_id": storeID}

	// log.Printf("Querying store_comments with filter: %v", filter)

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		log.Printf("Failed to fetch store comment data: %v", err)
		return nil, err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var commentItem models.StoreCommentItem
		if err := cursor.Decode(&commentItem); err != nil {
			log.Printf("Failed to decode comment item: %v", err)
			continue
		}
		storeCommentsData = append(storeCommentsData, commentItem)
	}

	if err := cursor.Err(); err != nil {
		log.Printf("Cursor error: %v", err)
		return nil, err
	}

	return storeCommentsData, nil
}
