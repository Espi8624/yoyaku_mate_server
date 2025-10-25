package db

import (
	"context"
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

// ウェイティングリストの更新を監視するための構造体
type WaitingListUpdate struct {
	OperationType string             `json:"operationType"`
	FullDocument  models.WaitingList `json:"fullDocument"`
	DocumentKey   interface{}        `json:"documentKey"`
}

// Initialize MongoDB connection
func InitMongoDB(uri string) error {
	log.Printf("Try mongoDB connect: %s", uri)

	clientOptions := options.Client().
		ApplyURI(uri).
		SetConnectTimeout(config.GetMongoTimeout()). // 30秒(production.jsonから設定)
		SetServerSelectionTimeout(30 * time.Second).
		SetSocketTimeout(45 * time.Second).
		SetMaxPoolSize(10).
		SetRetryWrites(true).
		SetRetryReads(true).
		SetMaxConnecting(5).
		SetServerAPIOptions(options.ServerAPI(options.ServerAPIVersion1))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var err error
	for i := 0; i < 5; i++ {
		MongoClient, err = mongo.Connect(ctx, clientOptions)
		if err != nil {
			log.Printf("MongoDB connect failed (%d/5): %v", i+1, err)
			time.Sleep(5 * time.Second)
			continue
		}

		// connect test (Ping)
		err = MongoClient.Ping(ctx, nil)
		if err != nil {
			log.Printf("MongoDB Ping failed (%d/5): %v", i+1, err)
			MongoClient.Disconnect(ctx)
			time.Sleep(5 * time.Second)
			continue
		}

		log.Println("MongoDB connect success")
		return nil
	}

	log.Println("MongoDB connect failed after 5 attempts")
	return err
}

// 　ウェイティングコレクションの変更を監視する
func MonitorWaitingList(collection *mongo.Collection) {
	if collection == nil {
		log.Println("collection is nil, need MongoDB connection check")
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var lastData []models.WaitingList
	ctx := context.Background()

	for range ticker.C {
		// Fetch current data
		cursor, err := collection.Find(ctx, bson.M{})
		if err != nil {
			log.Printf("Error fetching waiting list data: %v", err)
			continue
		}

		var currentData []models.WaitingList
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
					// log.Printf("Change detected for store %s", storeID)
				}
			}
			lastData = currentData
		}
	}
}

// // MongoDB collection 取得
func GetCollection(database, collection string) *mongo.Collection {
	if MongoClient == nil {
		log.Println("MongoDB 클라이언트가 초기화되지 않음")
		return nil
	}
	return MongoClient.Database(database).Collection(collection)
}
