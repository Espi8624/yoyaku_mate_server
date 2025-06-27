package data

import (
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/net/context"
)

// Provider User 데이터 조회
func GetProviderUserData(userID primitive.ObjectID) (models.User, error) {
	collection := db.GetCollection("yoyaku_mate_provider_db", "user_info")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var user models.User
	filter := bson.M{"_id": userID}

	err := collection.FindOne(ctx, filter).Decode(&user)
	if err != nil {
		log.Printf("Failed to fetch provider user info: %v", err)
		return models.User{}, err
	}

	return user, nil
}
