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
			{"$match", bson.D{{"user_id", userID}}},
		},
		// store_id를 문자열로 변환 (ObjectId인 경우)
		{
			{"$addFields", bson.D{
				{"store_id", bson.D{{"$toString", "$store_id"}}},
			}},
		},
		// 2. stores コレクションとジョイン
		{
			{"$lookup", bson.D{
				{"from", "store_info"},       // Joinするコレクション名
				{"localField", "store_id"},   // 現在コレクションのフィールド
				{"foreignField", "store_id"}, // Joinするコレクションのフィールド
				{"as", "store_info"},         // 結果を保存するフィールド名
			}},
		},
		// 3. store_info Fild展開
		{
			{"$unwind", bson.D{
				{"path", "$store_info"},              // 展開するフィールド
				{"preserveNullAndEmptyArrays", true}, // データがない場合も保持
			}},
		},
		// 4. users コレクションとジョイン
		// user_id를 문자열로 변환 (ObjectId인 경우)
		{
			{"$addFields", bson.D{
				{"user_id", bson.D{{"$toString", "$user_id"}}},
			}},
		},
		{
			{"$lookup", bson.D{
				{"from", "user_info"},       // Joinするコレクション名
				{"localField", "user_id"},   // 現在コレクションのフィールド
				{"foreignField", "user_id"}, // Joinするコレクションのフィールド
				{"as", "user_info"},         // 結果を保存するフィールド名
			}},
		},
		// 5. user_info Fild展開
		{
			{"$unwind", bson.D{
				{"path", "$user_info"},               // 展開するフィールド
				{"preserveNullAndEmptyArrays", true}, // データがない場合も保持
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
