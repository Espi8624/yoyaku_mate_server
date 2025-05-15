package data

import (
	"context"
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

func GetUserCommentData(userID string) ([]models.CommentInfoItem, error) {
	collection := db.GetCollection("yoyaku_mate_db", "comments_info")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// MongoDB aggregation pipeline
	pipeline := mongo.Pipeline{
		// 1. user_id でFiletering
		{
			{Key: "$match", Value: bson.D{{Key: "user_id", Value: userID}}},
		},
		// store_id를 문자열로 변환 (ObjectId인 경우)
		{
			{Key: "$addFields", Value: bson.D{
				{Key: "store_id", Value: bson.D{{Key: "$toString", Value: "$store_id"}}},
			}},
		},
		// 2. stores コレクションとジョイン
		{
			{Key: "$lookup", Value: bson.D{
				{Key: "from", Value: "store_info"},       // Joinするコレクション名
				{Key: "localField", Value: "store_id"},   // 現在コレクションのフィールド
				{Key: "foreignField", Value: "store_id"}, // Joinするコレクションのフィールド
				{Key: "as", Value: "store_info"},         // 結果を保存するフィールド名
			}},
		},
		// 3. store_info Fild展開
		{
			{Key: "$unwind", Value: bson.D{
				{Key: "path", Value: "$store_info"},              // 展開するフィールド
				{Key: "preserveNullAndEmptyArrays", Value: true}, // データがない場合も保持
			}},
		},
		// 4. users コレクションとジョイン
		// user_id를 문자열로 변환 (ObjectId인 경우)
		{
			{Key: "$addFields", Value: bson.D{
				{Key: "user_id", Value: bson.D{{Key: "$toString", Value: "$user_id"}}},
			}},
		},
		{
			{Key: "$lookup", Value: bson.D{
				{Key: "from", Value: "user_info"},       // Joinするコレクション名
				{Key: "localField", Value: "user_id"},   // 現在コレクションのフィールド
				{Key: "foreignField", Value: "user_id"}, // Joinするコレクションのフィールド
				{Key: "as", Value: "user_info"},         // 結果を保存するフィールド名
			}},
		},
		// 5. user_info Fild展開
		{
			{Key: "$unwind", Value: bson.D{
				{Key: "path", Value: "$user_info"},               // 展開するフィールド
				{Key: "preserveNullAndEmptyArrays", Value: true}, // データがない場合も保持
			}},
		},
	}

	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		log.Printf("Failed to fetch reviews with store info: %v", err)
		return nil, err
	}
	defer cursor.Close(ctx)

	var userCommentsData []models.CommentInfoItem
	if err := cursor.All(ctx, &userCommentsData); err != nil {
		log.Printf("Failed to decode reviews with store info: %v", err)
		return nil, err
	}

	// for i, comment := range userCommentsData {
	// 	log.Printf("Comment %d: ID=%s, StoreInfo=%+v, UserInfo=%+v",
	// 		i, comment.ID, comment.StoreInfo, comment.UserInfo)
	// }

	return userCommentsData, nil
}

func GetStoreCommentData(storeID string) ([]models.StoreCommentItem, error) {
	collection := db.GetCollection("yoyaku_mate_db", "comments_info")
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
