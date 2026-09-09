package data

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"
)

// MongoStoreRepo 店舗情報のMongoDBリポジトリ実装
type MongoStoreRepo struct{}

// MongoUserRepo ユーザー情報のMongoDBリポジトリ実装
type MongoUserRepo struct{}

// store_id で店舗情報を取得
func (r *MongoStoreRepo) GetStoreData(storeID string) (*models.Store, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var store models.Store
	filter := bson.M{"store_id": storeID}

	err := collection.FindOne(ctx, filter).Decode(&store)
	if err != nil {
		log.Printf("Failed to fetch store info by store_id '%s': %v", storeID, err)
		// エラー発生時、エラーを返却
		return nil, err
	}

	return &store, nil
}

// 店舗情報を更新し、更新後のドキュメントを返却 (REST 標準: PUT レスポンスに更新後リソースを包含)
func (r *MongoStoreRepo) UpdateStoreData(storeID string, update map[string]interface{}) (*models.Store, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"store_id": storeID}
	updateDoc := bson.M{"$set": update}

	after := options.After
	var updatedStore models.Store
	err := collection.FindOneAndUpdate(
		ctx,
		filter,
		updateDoc,
		&options.FindOneAndUpdateOptions{ReturnDocument: &after},
	).Decode(&updatedStore)
	if err != nil {
		log.Printf("Failed to update store info for store_id '%s': %v", storeID, err)
		return nil, err
	}
	return &updatedStore, nil
}

func (r *MongoStoreRepo) UpdateStoreImageURL(storeID string, storeImageURL string) (*models.Store, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"store_id": storeID}

	update := bson.M{
		"$set": bson.M{
			"store_image_url": storeImageURL,
			"updated_at":      time.Now(),
		},
	}

	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return nil, fmt.Errorf("failed to update store image: %w", err)
	}
	if result.MatchedCount == 0 {
		return nil, fmt.Errorf("no store found with ID: %s", storeID)
	}

	var updatedStore models.Store
	err = collection.FindOne(ctx, filter).Decode(&updatedStore)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated store document: %w", err)
	}

	return &updatedStore, nil
}

// user_id でユーザー情報を取得し、店舗情報を取得
func (r *MongoStoreRepo) GetStoreDataByUserID(userID primitive.ObjectID) (*models.Store, error) {
	// user_id で user_info から使用者情報を照会
	userCollection := db.GetCollection(DatabaseName, CollectionUserInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var user models.User
	userFilter := bson.M{"_id": userID}
	err := userCollection.FindOne(ctx, userFilter).Decode(&user)
	if err != nil {
		// ユーザーが見つからない場合、エラーログを出力し、エラーを返却
		log.Printf("Failed to find user with _id '%s': %v", userID.Hex(), err)
		return nil, err
	}

	// ユーザーが所属する店舗IDを確認
	if user.StoreID == "" {
		// ユーザーが店舗に所属していない場合、エラーログを出力し、エラーを返却
		log.Printf("User with _id '%s' is not associated with any store.", userID.Hex())
		return nil, mongo.ErrNoDocuments
	}

	// ユーザーの店舗IDを使用し、店舗情報を取得
	return r.GetStoreData(user.StoreID)
}

// CountStoresByOwner 指定ユーザーがオーナー(user_id)として持つ店舗数を返す。
// user_infoのstore_idは1件しか保持できないが、マネージャーは複数店舗を
// 所有できるため、会員退会時の安全チェックには store_info を直接 user_id で検索する
func (r *MongoStoreRepo) CountStoresByOwner(userID primitive.ObjectID) (int64, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	count, err := collection.CountDocuments(ctx, bson.M{"user_id": userID})
	if err != nil {
		log.Printf("Failed to count stores owned by user '%s': %v", userID.Hex(), err)
		return 0, err
	}
	return count, nil
}

// 店舗設定データ取得
func (r *MongoStoreRepo) GetSettings(storeID string) (*models.StoreSetting, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreSettings)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var storeSettings models.StoreSetting
	filter := bson.M{"store_id": storeID}

	err := collection.FindOne(ctx, filter).Decode(&storeSettings)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, err
		}
		// required_staff_count が旧スキーマ(曜日ごとのオブジェクト)のまま残っていると、
		// 新スキーマ(曜日ごとの配列)へのデコードがドキュメント全体で失敗してしまう。
		// そのフィールドだけ取り除いて自動修復を試みる (値は失われるため再入力が必要になる)
		log.Printf("Failed to decode store settings for store_id=%s, attempting to repair legacy required_staff_count: %v", storeID, err)
		if _, repairErr := collection.UpdateOne(ctx, filter, bson.M{
			"$unset": bson.M{"settings.required_staff_count": ""},
		}); repairErr != nil {
			log.Printf("Failed to repair store settings for store_id=%s: %v", storeID, repairErr)
			return nil, err
		}

		if retryErr := collection.FindOne(ctx, filter).Decode(&storeSettings); retryErr != nil {
			log.Printf("Failed to fetch store settings for store_id=%s after repair: %v", storeID, retryErr)
			return nil, retryErr
		}
	}

	return &storeSettings, nil
}

// store_settings upsert (保存/修正)
func (r *MongoStoreRepo) UpsertStoreSettings(storeID string, reqBody map[string]interface{}) error {
	collection := db.GetCollection(DatabaseName, CollectionStoreSettings)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"store_id": storeID}
	update := bson.M{"$set": reqBody}
	// upsert:true を明示。false のままだと store_id に一致するドキュメントが
	// 存在しない場合、UpdateOne は何もマッチせず(matchedCount=0)エラーも出さずに
	// サイレントに書き込みが失われるため、関数名の"Upsert"通りに動作するよう修正
	_, err := collection.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	if err != nil {
		log.Printf("Failed to upsert store settings for store_id=%s: %v", storeID, err)
		return err
	}
	return nil
}

type MinioClient struct {
	S3Client *s3.Client
	Bucket   string
	Endpoint string
}

// MinIO クライアントを作成して初期化
func NewMinioClient(endpoint, accessKey, secretKey, bucket string) (*MinioClient, error) {
	resolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:           endpoint,
			SigningRegion: "us-east-1", // MinIO uses a single region
			PartitionID:   "aws",
			Source:        aws.EndpointSourceCustom,
		}, nil
	})

	creds := credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithEndpointResolverWithOptions(resolver),
		config.WithCredentialsProvider(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load s3 config: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true // MinIO uses path-style requests
	})

	return &MinioClient{
		S3Client: s3Client,
		Bucket:   bucket,
		Endpoint: endpoint,
	}, nil
}

// ファイルを MinIO バケットにアップロード
// func (c *MinioClient) UploadFile(file multipart.File, header *multipart.FileHeader) (string, error) {
// 	// ファイル名衝突を回避するため、ユニークなファイル名を生成
// 	uniqueFileName := uuid.New().String() + filepath.Ext(header.Filename)

// 	_, err := c.S3Client.PutObject(context.TODO(), &s3.PutObjectInput{
// 		Bucket:      aws.String(c.Bucket),
// 		Key:         aws.String(uniqueFileName),
// 		Body:        file,
// 		ContentType: aws.String(header.Header.Get("Content-Type")),
// 	})
// 	if err != nil {
// 		return "", fmt.Errorf("failed to upload file to minio: %w", err)
// 	}

// 	// ファイルの URL を生成
// 	// 実際には Endpoint を設定ファイルから読み込む必要がある
// 	fileURL := fmt.Sprintf("%s/%s/%s", c.Endpoint, c.Bucket, uniqueFileName)
// 	log.Printf("Successfully uploaded file: %s", fileURL)

// 	return fileURL, nil
// }

// MongoDB　の 'stores' コレクションのライセンス情報を更新
func (r *MongoStoreRepo) UpdateStoreLicenseInfo(storeID string, imageURL string) error {
	// MongoDB コレクションを取得
	collection := db.GetCollection(DatabaseName, CollectionStoreLicense)

	// 文字列形式の storeID をそのまま使用し、フィルター作成
	filter := bson.M{"store_id": storeID}

	// 更新内容を定義
	update := bson.M{
		"$set": bson.M{
			"license_image_url":   imageURL,
			"verification_status": models.StatusPending,
			"updated_at":          time.Now(),
		},
	}

	// コンテキストを作成
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// DB 更新を実行
	// Filter: 'store_id'が渡された storeID(string) と一致するドキュメントを検索
	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		log.Printf("Error: Failed to update store document in MongoDB. storeID: %s, err: %v", storeID, err)
		return err
	}

	// 結果確認
	if result.MatchedCount == 0 {
		log.Printf("Warning: No store document found with the given ID. storeID: %s", storeID)
		return nil
	}

	log.Printf("DATABASE: Successfully updated license info for storeID: %s. Documents matched: %d, Documents modified: %d", storeID, result.MatchedCount, result.ModifiedCount)
	return nil
}

// store_license コレクションのドキュメントを更新し、更新後のドキュメントを返却 (REST 標準)
func (r *MongoStoreRepo) UpdateLicenseInfoAfterUpload(storeID string, imageURL string) (*models.StoreLicense, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreLicense)

	filter := bson.M{"store_id": storeID}

	update := bson.M{
		"$set": bson.M{
			"license_image_url":   imageURL,
			"verification_status": models.StatusPendingReview,
			"updated_at":          time.Now(),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	after := options.After
	var updatedLicense models.StoreLicense
	err := collection.FindOneAndUpdate(
		ctx,
		filter,
		update,
		&options.FindOneAndUpdateOptions{ReturnDocument: &after},
	).Decode(&updatedLicense)
	if err != nil {
		log.Printf("Error: Failed to update license document in MongoDB. storeID: %s, err: %v", storeID, err)
		return nil, err
	}

	log.Printf("DATABASE: Successfully updated license info for storeID: %s.", storeID)
	return &updatedLicense, nil
}

// store_idで店舗認証情報照会
func (r *MongoStoreRepo) GetLicense(storeID string) (*models.StoreLicense, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreLicense)
	filter := bson.M{"store_id": storeID}

	var license models.StoreLicense
	err := collection.FindOne(context.Background(), filter).Decode(&license)
	if err != nil {
		return nil, err
	}

	return &license, nil
}

// StoreWithStatus は店舗情報とスタッフステータスを含む構造体
type StoreWithStatus struct {
	models.Store
	StaffStatus        string `json:"staff_status,omitempty" bson:"-"`
	VerificationStatus string `json:"verification_status,omitempty" bson:"-"`
}

// firebase_uidを使用し、該当ユーザーが接近可能なすべての店舗リストを返却
func (r *MongoStoreRepo) GetStoresByFirebaseUID(firebaseUid string) ([]StoreWithStatus, error) {
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

	// Sort by _id ascending (Registration Order)
	findOptions := options.Find()
	findOptions.SetSort(bson.D{{Key: "_id", Value: 1}})

	switch user.Role {
	case "manager":
		cursor, err := storeCollection.Find(ctx, bson.M{"user_id": user.ID}, findOptions)
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

		// ユーザーIDでPENDINGまたはAPPROVED状態の店舗スタッフ情報を検索。
		// ここでは store_id と status しか使わないため、projectionで明示的にその2つだけ
		// 取得する(availability等の他フィールドは、旧スキーマ(曜日ごとに文字列配列で
		// 保存された勤務可能時間帯)のドキュメントが混在しているとデコードエラーになり、
		// このAPI全体が失敗して店舗一覧が丸ごと表示されなくなってしまうため取得自体を避ける)
		staffFindOptions := options.Find().
			SetSort(bson.D{{Key: "_id", Value: 1}}).
			SetProjection(bson.M{"store_id": 1, "status": 1})
		cursor, err := staffCollection.Find(ctx, bson.M{
			"user_id": user.ID,
			"status": bson.M{
				"$in": []string{models.StaffStatusPending, models.StaffStatusApproved, models.StaffStatusRejected},
			},
		}, staffFindOptions)
		if err != nil {
			return nil, err
		}
		defer cursor.Close(ctx)

		var staffInfos []struct {
			StoreID string `bson:"store_id"`
			Status  string `bson:"status"`
		}
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

// User データ取得
func (r *MongoUserRepo) GetUserData(userID primitive.ObjectID) (*models.User, error) {
	collection := db.GetCollection(DatabaseName, CollectionUserInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var user models.User
	filter := bson.M{"_id": userID}

	err := collection.FindOne(ctx, filter).Decode(&user)
	if err != nil {
		log.Printf("Failed to fetch user info: %v", err)
		return nil, err
	}

	return &user, nil
}

// User データ更新、更新後のドキュメントを返却 (REST 標準)
func (r *MongoUserRepo) UpdateUserData(userID primitive.ObjectID, update map[string]interface{}) (*models.User, error) {
	collection := db.GetCollection(DatabaseName, CollectionUserInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"_id": userID}
	updateDoc := bson.M{"$set": update}

	after := options.After
	var updatedUser models.User
	err := collection.FindOneAndUpdate(
		ctx,
		filter,
		updateDoc,
		&options.FindOneAndUpdateOptions{ReturnDocument: &after},
	).Decode(&updatedUser)
	if err != nil {
		log.Printf("Failed to update user info: %v", err)
		return nil, err
	}
	return &updatedUser, nil
}

// MarkUserWithdrawn 指定ユーザーを退会済み(WITHDRAWN)としてマークする(ソフトデリート)。
// 電話番号・住所などの連絡先は削除せず保持したまま、ログインのみ不可にする方針のため、
// ドキュメント自体は消さずstatus/withdrawn_atだけ更新する。
// Firebase Auth側のアカウント削除は呼び出し側(handler)がauth.DeleteUserで別途行う
func (r *MongoUserRepo) MarkUserWithdrawn(userID primitive.ObjectID) error {
	collection := db.GetCollection(DatabaseName, CollectionUserInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	update := bson.M{
		"$set": bson.M{
			"status":       models.UserStatusWithdrawn,
			"withdrawn_at": time.Now(),
		},
	}
	_, err := collection.UpdateOne(ctx, bson.M{"_id": userID}, update)
	if err != nil {
		log.Printf("Failed to mark user withdrawn for user '%s': %v", userID.Hex(), err)
		return err
	}
	return nil
}

func (r *MongoUserRepo) UpdateUserImageURL(firebaseUID string, userImageURL string) (*models.User, error) {
	collection := db.GetCollection(DatabaseName, CollectionUserInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"firebase_uid": firebaseUID}

	update := bson.M{
		"$set": bson.M{
			"user_image_url": userImageURL,
			"updated_at":     time.Now(),
		},
	}

	// update実行
	result, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return nil, fmt.Errorf("failed to execute update on provider_users: %w", err)
	}

	if result.MatchedCount == 0 {
		return nil, fmt.Errorf("no user found with firebase UID: %s", firebaseUID)
	}

	// 更新された全ユーザー情報を再取得して返す
	var updatedUser models.User
	err = collection.FindOne(ctx, filter).Decode(&updatedUser)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated user document: %w", err)
	}

	return &updatedUser, nil
}

func (r *MongoUserRepo) GetUserDataByFirebaseUID(firebaseUID string) (*models.User, error) {
	// log.Printf("--- [GetUserDataByFirebaseUID] 함수 시작. firebaseUid: %s 로 사용자 조회를 시작합니다.", firebaseUID)

	collection := db.GetCollection(DatabaseName, CollectionUserInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var user models.User
	filter := bson.M{"firebase_uid": firebaseUID}

	err := collection.FindOne(ctx, filter).Decode(&user)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			// log.Printf("--- [GetUserDataByFirebaseUID] 경고: firebaseUid '%s'를 가진 사용자를 DB에서 찾지 못했습니다. ErrNoDocuments 반환.", firebaseUID)
			return nil, mongo.ErrNoDocuments
		}

		// log.Printf("--- [GetUserDataByFirebaseUID] 에러: 사용자 조회 중 DB 에러 발생: %v", err)
		return nil, err
	}

	// log.Printf("--- [GetUserDataByFirebaseUID] 성공: 사용자 '%s' (ID: %s)를 찾았습니다. 함수를 종료합니다.", user.Username, user.ID.Hex())

	return &user, nil
}

// GetUserByFirebaseUID Firebase UIDを使用してユーザーを取得
func (r *MongoUserRepo) GetByFirebaseUID(firebaseUID string) (*models.User, error) {
	collection := db.GetCollection(DatabaseName, CollectionUserInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var user models.User
	err := collection.FindOne(ctx, bson.M{"firebase_uid": firebaseUID}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // User not found
		}
		return nil, err
	}

	return &user, nil
}

// CheckUserStorePermission ユーザーが店舗にアクセスする権限があるかを確認
// マネージャーの場合: 店舗の所有者かどうかを確認
// スタッフの場合: 店舗でAPPROVED状態であり、かつ必要な権限を持っているかを確認(指定されている場合)
func (r *MongoUserRepo) CheckStorePermission(userID primitive.ObjectID, storeID string, role string, requiredPermission string) (bool, error) {
	if role == "manager" {
		// マネージャーの場合、店舗の所有者かどうかを確認
		storeCollection := db.GetCollection(DatabaseName, CollectionStoreInfo)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var store models.Store
		err := storeCollection.FindOne(ctx, bson.M{
			"store_id": storeID,
			"user_id":  userID,
		}).Decode(&store)

		if err != nil {
			if err == mongo.ErrNoDocuments {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}

	// スタッフの場合
	// 1. APPROVED状態かどうかを確認
	approved, err := CheckStaffApprovalStatus(userID, storeID)
	if err != nil || !approved {
		return false, err
	}

	// 2. 権限チェック (requiredPermissionが指定されている場合)
	if requiredPermission != "" {
		collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// ここでは permissions しか使わないため、projectionで明示的にそれだけ取得する
		// (availabilityを含む全体をデコードすると、旧スキーマ(曜日ごとに文字列配列で
		// 保存された勤務可能時間帯)のドキュメントでデコードエラーになってしまうため)
		var staffInfo struct {
			Permissions []string `bson:"permissions"`
		}
		err := collection.FindOne(ctx, bson.M{
			"user_id":  userID,
			"store_id": storeID,
		}, options.FindOne().SetProjection(bson.M{"permissions": 1})).Decode(&staffInfo)

		if err != nil {
			return false, err
		}

		hasPermission := false
		for _, p := range staffInfo.Permissions {
			if p == requiredPermission {
				hasPermission = true
				break
			}
		}
		return hasPermission, nil
	}

	return true, nil
}

func (r *MongoStoreRepo) GetStoresByStatus(status string) ([]models.StoreWithLicense, error) {
	licenseCollection := db.GetCollection(DatabaseName, "store_license")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pipeline := mongo.Pipeline{}

	if status != "" {
		matchStage := bson.D{{Key: "$match", Value: bson.D{{Key: "verification_status", Value: status}}}}
		pipeline = append(pipeline, matchStage)
	}

	lookupStage := bson.D{{Key: "$lookup", Value: bson.D{
		{Key: "from", Value: "store_info"},
		{Key: "localField", Value: "store_id"},
		{Key: "foreignField", Value: "store_id"},
		{Key: "as", Value: "storeDetails"},
	}}}
	pipeline = append(pipeline, lookupStage)

	unwindStage := bson.D{{Key: "$unwind", Value: bson.M{
		"path":                       "$storeDetails",
		"preserveNullAndEmptyArrays": true,
	}}}
	pipeline = append(pipeline, unwindStage)

	// User Info Lookup
	userLookupStage := bson.D{{Key: "$lookup", Value: bson.D{
		{Key: "from", Value: "user_info"}, // CollectionUserInfo constant would be better but string works
		{Key: "localField", Value: "storeDetails.user_id"},
		{Key: "foreignField", Value: "_id"},
		{Key: "as", Value: "userDetails"},
	}}}
	pipeline = append(pipeline, userLookupStage)

	userUnwindStage := bson.D{{Key: "$unwind", Value: bson.M{
		"path":                       "$userDetails",
		"preserveNullAndEmptyArrays": true,
	}}}
	pipeline = append(pipeline, userUnwindStage)

	projectStage := bson.D{{Key: "$project", Value: bson.D{
		{Key: "store_id", Value: "$store_id"},
		{Key: "store_name", Value: "$storeDetails.store_name"},
		{Key: "business_category", Value: "$storeDetails.business_category"},
		{Key: "address", Value: "$storeDetails.address"},
		{Key: "phone", Value: "$storeDetails.phone"},
		{Key: "license_image_url", Value: "$license_image_url"},
		{Key: "verification_status", Value: "$verification_status"},
		{Key: "created_at", Value: "$created_at"},
		{Key: "user_name", Value: "$userDetails.user_name"},
		{Key: "user_email", Value: "$userDetails.email"},
		{Key: "user_phone", Value: "$userDetails.phone"},
		{Key: "_id", Value: 0},
	}}}
	pipeline = append(pipeline, projectStage)

	sortStage := bson.D{{Key: "$sort", Value: bson.D{{Key: "created_at", Value: -1}}}}
	pipeline = append(pipeline, sortStage)

	// Aggregation実行
	cursor, err := licenseCollection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []models.StoreWithLicense
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	return results, nil
}

func (r *MongoStoreRepo) UpdateLicenseStatus(storeID string, status string, comment string) error {
	licenseCollection := db.GetCollection(DatabaseName, "store_license")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{"store_id": storeID}

	if status != models.StatusApproved && status != models.StatusRejected {
		return fmt.Errorf("invalid status provided: %s", status)
	}

	update := bson.M{
		"$set": bson.M{
			"verification_status": status,
			"admin_comment":       comment,
			"updated_at":          time.Now(),
		},
	}

	result, err := licenseCollection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to execute update on store_license: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("no license information found for store ID: %s", storeID)
	}

	return nil
}
