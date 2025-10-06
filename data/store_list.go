package data

import (
	"context"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// firebase_uidを使用し、該当ユーザーが接近可能なすべての店舗リストを返却
func GetStoresByFirebaseUID(firebaseUid string) ([]models.Store, error) {
	// log.Printf("--- [GetStoresByFirebaseUID] 함수 시작. firebaseUid: %s 로 사용자 조회를 시작합니다.", firebaseUid)
	userCollection := db.GetCollection(DatabaseName, CollectionUserInfo)
	storeCollection := db.GetCollection(DatabaseName, CollectionStoreInfo)
	ctx := context.Background()

	var user models.User
	err := userCollection.FindOne(ctx, bson.M{"firebase_uid": firebaseUid}).Decode(&user)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			// log.Printf("--- [GetStoresByFirebaseUID] 경고: firebaseUid '%s'를 가진 사용자를 DB에서 찾지 못했습니다. 빈 목록을 반환합니다.", firebaseUid)
			return []models.Store{}, nil
		}
		// log.Printf("--- [GetStoresByFirebaseUID] 에러: 사용자 조회 중 DB 에러 발생: %v", err)
		return nil, err
	}

	// log.Printf("--- [GetStoresByFirebaseUID] 성공: 사용자 '%s' (ID: %s, Role: %s)를 찾았습니다. 이제 가게를 조회합니다.", user.Username, user.ID.Hex(), user.Role)
	var stores []models.Store

	switch user.Role {
	case "manager":
		cursor, err := storeCollection.Find(ctx, bson.M{"user_id": user.ID})
		if err != nil {
			// log.Printf("--- [GetStoresByFirebaseUID] 에러: 매니저의 가게 목록 조회 중 DB 에러: %v", err)
			return nil, err
		}
		defer cursor.Close(ctx)

		if err = cursor.All(ctx, &stores); err != nil {
			// log.Printf("--- [GetStoresByFirebaseUID] 에러: 커서 처리 중 에러: %v", err)
			return nil, err
		}

	// 職員の場合、該当職員が所属する店舗のみを返却
	case "staff":
		var store models.Store
		err := storeCollection.FindOne(ctx, bson.M{"store_id": user.StoreID}).Decode(&store)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				return []models.Store{}, nil
			}
			return nil, err
		}
		stores = append(stores, store)
	}
	// log.Printf("--- [GetStoresByFirebaseUID] 최종 결과: %d개의 가게를 찾았습니다. 함수를 종료합니다.", len(stores))

	return stores, nil
}
