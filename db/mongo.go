package db

import (
	"context"
	"fmt"
	"log"
	"time"
	"yoyaku_mate_server/config"
	"yoyaku_mate_server/events"
	"yoyaku_mate_server/models"

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
	backoff := time.Second * 5
	maxBackoff := time.Minute * 5

	for {
		if err := watchWaitingList(collection); err != nil {
			log.Printf("Change monitoring error: %v, retrying in %v...", err, backoff)
			time.Sleep(backoff)

			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		backoff = time.Second * 5
	}
}

func watchWaitingList(collection *mongo.Collection) error {
	ctx := context.Background()
	pipeline := mongo.Pipeline{}
	opts := options.ChangeStream().SetFullDocument(options.UpdateLookup)

	stream, err := collection.Watch(ctx, pipeline, opts)
	if err != nil {
		return fmt.Errorf("failed to start change stream: %v", err)
	}
	defer stream.Close(ctx)

	log.Println("Monitoring waiting list changes...")

	for stream.Next(ctx) {
		var changeEvent WaitingListUpdate
		if err := stream.Decode(&changeEvent); err != nil {
			log.Printf("Failed to decode change event: %v", err)
			continue
		}

		// Extract store ID and notify relevant clients
		if storeID := changeEvent.FullDocument.StoreID; storeID != "" {
			events.NotifyStoreUpdate(storeID)
			log.Printf("Change detected for store %s: %s", storeID, changeEvent.OperationType)
		}
	}

	if err := stream.Err(); err != nil {
		return fmt.Errorf("stream error: %v", err)
	}

	return nil
}

// GetCollection returns a MongoDB collection
func GetCollection(database, collection string) *mongo.Collection {
	return MongoClient.Database(database).Collection(collection)
}
