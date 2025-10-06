package data

import (
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/net/context"
)

// User データ取得
func GetUserData(userID primitive.ObjectID) (models.User, error) {
	collection := db.GetCollection(DatabaseName, CollectionUserInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var user models.User
	filter := bson.M{"_id": userID}

	err := collection.FindOne(ctx, filter).Decode(&user)
	if err != nil {
		log.Printf("Failed to fetch user info: %v", err)
		return models.User{}, err
	}

	return user, nil
}

// User データ更新
func UpdateUserData(userID primitive.ObjectID, update map[string]interface{}) error {
	collection := db.GetCollection(DatabaseName, CollectionUserInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"_id": userID}
	updateDoc := bson.M{"$set": update}
	_, err := collection.UpdateOne(ctx, filter, updateDoc)
	if err != nil {
		log.Printf("Failed to update user info: %v", err)
		return err
	}
	return nil
}

func GetUserDataByFirebaseUID(firebaseUID string) (models.User, error) {
	// log.Printf("--- [GetUserDataByFirebaseUID] 함수 시작. firebaseUid: %s 로 사용자 조회를 시작합니다.", firebaseUID)

	collection := db.GetCollection(DatabaseName, CollectionUserInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var user models.User
	filter := bson.M{"firebase_uid": firebaseUID}

	err := collection.FindOne(ctx, filter).Decode(&user)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			// log.Printf("--- [GetUserDataByFirebaseUID] 경고: firebaseUid '%s'를 가진 사용자를 DB에서 찾지 못했습니다. 빈 객체를 반환합니다.", firebaseUID)
			return models.User{}, nil
		}

		// log.Printf("--- [GetUserDataByFirebaseUID] 에러: 사용자 조회 중 DB 에러 발생: %v", err)
		return models.User{}, err
	}

	// log.Printf("--- [GetUserDataByFirebaseUID] 성공: 사용자 '%s' (ID: %s)를 찾았습니다. 함수를 종료합니다.", user.Username, user.ID.Hex())

	return user, nil
}
