package db

import (
	"context"
	"log"
	"time"
	"yoyaku_mate_server/config"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	DatabaseName          = "project_rusui"
	CollectionWaitingList = "waiting_list"
	CollectionErrorLogs   = "error_logs"
)

var MongoClient *mongo.Client

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

		// Create indexes
		if err := EnsureIndexes(); err != nil {
			log.Printf("Failed to create indexes: %v", err)
			// Index creation failure should not stop server startup, but warn loudly
		}

		return nil
	}

	log.Println("MongoDB connect failed after 5 attempts")
	return err
}

// // MongoDB collection 取得
func GetCollection(database, collection string) *mongo.Collection {
	if MongoClient == nil {
		log.Println("MongoDB 클라이언트가 초기화되지 않음")
		return nil
	}
	return MongoClient.Database(database).Collection(collection)
}

// コレクションのインデックスを作成
func EnsureIndexes() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	collection := GetCollection(DatabaseName, CollectionWaitingList)
	if collection == nil {
		return nil
	}

	// 統計用複合インデックス: store_id + registration_time (降順)
	// これにより、store_idによるフィルタリングとregistration_timeによる範囲クエリ（今日の統計など）が最適化されます
	indexModel := mongo.IndexModel{
		Keys: bson.D{
			{Key: "store_id", Value: 1},
			{Key: "registration_time", Value: -1},
		},
		Options: options.Index().SetName("idx_store_reg_time"),
	}

	_, err := collection.Indexes().CreateOne(ctx, indexModel)
	if err != nil {
		return err
	}
	log.Println("Created compound index: idx_store_reg_time on waiting_list")

	errorLogsCollection := GetCollection(DatabaseName, CollectionErrorLogs)
	if errorLogsCollection != nil {
		ttlIndexModel := mongo.IndexModel{
			Keys: bson.D{{Key: "timestamp", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(604800).SetName("idx_error_logs_ttl"),
		}
		_, err := errorLogsCollection.Indexes().CreateOne(ctx, ttlIndexModel)
		if err != nil {
			log.Printf("Failed to create TTL index for error_logs: %v", err)
		} else {
			log.Println("Created TTL index: idx_error_logs_ttl on error_logs (7 days)")
		}

		typeIndexModel := mongo.IndexModel{
			Keys: bson.D{{Key: "error_type", Value: 1}},
			Options: options.Index().SetName("idx_error_type"),
		}
		_, err = errorLogsCollection.Indexes().CreateOne(ctx, typeIndexModel)
		if err != nil {
			log.Printf("Failed to create error_type index: %v", err)
		} else {
			log.Println("Created index: idx_error_type on error_logs")
		}
	}

	return nil
}
