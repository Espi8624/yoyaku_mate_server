package data

import (
	"context"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// StoreWithStatus は店舗情報とスタッフステータスを含む構造体
type StoreWithStatus struct {
	models.Store
	StaffStatus        string `json:"staff_status,omitempty" bson:"-"`
	VerificationStatus string `json:"verification_status,omitempty" bson:"-"`
}

// firebase_uidを使用し、該当ユーザーが接近可能なすべての店舗リストを返却
func GetStoresByFirebaseUID(firebaseUid string) ([]StoreWithStatus, error) {
	// log.Printf("--- [GetStoresByFirebaseUID] 함수 시작. firebaseUid: %s 로 사용자 조회를 시작합니다.", firebaseUid)
	userCollection := db.GetCollection(DatabaseName, CollectionUserInfo)
	storeCollection := db.GetCollection(DatabaseName, CollectionStoreInfo)
	ctx := context.Background()

	var user models.User
	err := userCollection.FindOne(ctx, bson.M{"firebase_uid": firebaseUid}).Decode(&user)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			// log.Printf("--- [GetStoresByFirebaseUID] 경고: firebaseUid '%s'를 가진 사용자를 DB에서 찾지 못했습니다. 빈 목록을 반환합니다.", firebaseUid)
			return []StoreWithStatus{}, nil
		}
		// log.Printf("--- [GetStoresByFirebaseUID] 에러: 사용자 조회 중 DB 에러 발생: %v", err)
		return nil, err
	}

	// log.Printf("--- [GetStoresByFirebaseUID] 성공: 사용자 '%s' (ID: %s, Role: %s)를 찾았습니다. 이제 가게를 조회합니다.", user.UserName, user.ID.Hex(), user.Role)
	var storesWithStatus []StoreWithStatus

	switch user.Role {
	case "manager":
		cursor, err := storeCollection.Find(ctx, bson.M{"user_id": user.ID})
		if err != nil {
			// log.Printf("--- [GetStoresByFirebaseUID] 에러: 매니저의 가게 목록 조회 중 DB 에러: %v", err)
			return nil, err
		}
		defer cursor.Close(ctx)

		var stores []models.Store
		if err = cursor.All(ctx, &stores); err != nil {
			// log.Printf("--- [GetStoresByFirebaseUID] 에러: 커서 처리 중 에러: %v", err)
			return nil, err
		}

		// マネージャーの場合、各店舗のverification_statusをstore_licenseから取得
		licenseCollection := db.GetCollection(DatabaseName, CollectionStoreLicense)
		for _, store := range stores {
			var license models.StoreLicense
			verificationStatus := "NOT_SUBMITTED" // デフォルト値

			// store_licenseからverification_statusを取得
			err := licenseCollection.FindOne(ctx, bson.M{"store_id": store.StoreID}).Decode(&license)
			if err == nil {
				verificationStatus = license.VerificationStatus
			} else if err != mongo.ErrNoDocuments {
				// エラーがあってもスキップして続行（ログ出力は必要に応じて）
				// log.Printf("--- [GetStoresByFirebaseUID] 경고: store_id '%s'의 라이선스 조회 중 에러: %v", store.StoreID, err)
			}

			storesWithStatus = append(storesWithStatus, StoreWithStatus{
				Store:              store,
				VerificationStatus: verificationStatus,
			})
		}

	// 職員の場合、store_staff_infoテーブルから承認された店舗を取得
	case "staff":
		staffCollection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)

		// ユーザーIDでPENDINGまたはAPPROVED状態の店舗スタッフ情報を検索
		cursor, err := staffCollection.Find(ctx, bson.M{
			"user_id": user.ID,
			"status": bson.M{
				"$in": []string{models.StaffStatusPending, models.StaffStatusApproved, models.StaffStatusRejected},
			},
		})
		if err != nil {
			return nil, err
		}
		defer cursor.Close(ctx)

		var staffInfos []models.StoreStaffInfo
		if err = cursor.All(ctx, &staffInfos); err != nil {
			return nil, err
		}

		// 各StoreStaffInfoからstore_idを取得し、対応する店舗情報を取得
		for _, staffInfo := range staffInfos {
			var store models.Store
			err := storeCollection.FindOne(ctx, bson.M{"store_id": staffInfo.StoreID}).Decode(&store)
			if err != nil {
				if err == mongo.ErrNoDocuments {
					continue // 店舗が見つからない場合はスキップ
				}
				return nil, err
			}
			// StaffStatusを含めて返却
			storesWithStatus = append(storesWithStatus, StoreWithStatus{
				Store:       store,
				StaffStatus: staffInfo.Status,
			})
		}
	}
	// log.Printf("--- [GetStoresByFirebaseUID] 최종 결과: %d개의 가게를 찾았습니다. 함수를 종료합니다.", len(storesWithStatus))

	return storesWithStatus, nil
}
