package data

import (
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"golang.org/x/net/context"
)

// ユーザデータ取得
func GetUserInfoData(userID string) (models.UserInfoItem, error) {
	collection := db.GetCollection("yoyaku_mate_db", "user_info")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var userInfoData models.UserInfoItem
	filter := bson.M{"user_id": userID}

	// log.Printf("Querying user_info with filter: %v", filter)

	err := collection.FindOne(ctx, filter).Decode(&userInfoData)
	if err != nil {
		log.Printf("Failed to fetch user info: %v", err)
		return models.UserInfoItem{}, err
	}

	return userInfoData, nil
}
