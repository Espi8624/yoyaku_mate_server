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
	collection := db.GetCollection("yoyaku_mate_provider", "user_info")
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

// Provider User 데이터 수정
func UpdateProviderUserData(userID primitive.ObjectID, update map[string]interface{}) error {
	collection := db.GetCollection("yoyaku_mate_provider", "user_info")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"_id": userID}
	updateDoc := bson.M{"$set": update}
	_, err := collection.UpdateOne(ctx, filter, updateDoc)
	if err != nil {
		log.Printf("Failed to update provider user info: %v", err)
		return err
	}
	return nil
}

func GetProviderUserDataByFirebaseUID(firebaseUID string) (models.User, error) {
	collection := db.GetCollection("yoyaku_mate_provider", "user_info")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var user models.User
	filter := bson.M{"firebase_uid": firebaseUID}

	err := collection.FindOne(ctx, filter).Decode(&user)
	if err != nil {
		log.Printf("Failed to fetch provider user info by firebase_uid: %v", err)
		return models.User{}, err
	}

	return user, nil
}
