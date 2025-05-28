package db

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"time"
	"yoyaku_mate_server/config"
	"yoyaku_mate_server/events"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var MongoClient *mongo.Client

// WaitingListUpdate represents a waiting list update
type WaitingListUpdate struct {
	OperationType string                 `json:"operationType"`
	FullDocument  models.WaitingListItem `json:"fullDocument"`
	DocumentKey   interface{}            `json:"documentKey"`
}

// Initialize MongoDB connection
func InitMongoDB(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), config.GetMongoTimeout())
	defer cancel()

	clientOptions := options.Client().ApplyURI(url)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %v", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("failed to ping MongoDB: %v", err)
	}

	log.Println("Connected to MongoDB")
	MongoClient = client
	return nil
}

// MonitorWaitingList monitors changes in the waiting list collection
func MonitorWaitingList(collection *mongo.Collection) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var lastData []models.WaitingListItem
	ctx := context.Background()

	for range ticker.C {
		// Fetch current data
		cursor, err := collection.Find(ctx, bson.M{})
		if err != nil {
			log.Printf("Error fetching waiting list data: %v", err)
			continue
		}

		var currentData []models.WaitingListItem
		if err := cursor.All(ctx, &currentData); err != nil {
			log.Printf("Error decoding waiting list data: %v", err)
			cursor.Close(ctx)
			continue
		}
		cursor.Close(ctx)

		// Compare with last data
		if !reflect.DeepEqual(lastData, currentData) {
			// Data has changed, notify clients
			for _, item := range currentData {
				if storeID := item.StoreID; storeID != "" {
					events.NotifyStoreUpdate(storeID)
					log.Printf("Change detected for store %s", storeID)
				}
			}
			lastData = currentData
		}
	}
}

// GetCollection returns a MongoDB collection
func GetCollection(database, collection string) *mongo.Collection {
	return MongoClient.Database(database).Collection(collection)
}
