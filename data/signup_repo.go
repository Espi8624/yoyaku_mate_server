package data

import (
	"context"
	"fmt"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// signUpTransactionTimeout 会員登録・店舗追加トランザクション全体のタイムアウト
const signUpTransactionTimeout = 15 * time.Second

// MongoSignUpRepo 会員登録・店舗追加・重複チェックのMongoDBリポジトリ実装
//   - 複数コレクション(user_info / store_info / store_license / store_settings / store_staff_info)
//     にまたがる作成はトランザクションでまとめて行い、途中失敗で中途半端なデータを残さない
type MongoSignUpRepo struct{}

// RegisterUser 会員登録をトランザクションで行い、作成したユーザーを返す
//   - store/settings が nil でなければ、このユーザーを所有者とする店舗を同時に作成する (マネージャー)
//   - joinStoreID が空でなければ、その店舗へ承認待ちのスタッフとして参加させる (スタッフ)
//   - 退会済み(WITHDRAWN)アカウントは重複判定から除外する: 退会は連絡先を残す
//     ソフトデリートのため、同じメール/電話番号で改めて新規登録できてよい
//     (Firebase Auth側のアカウントは退会時に削除済みなので衝突しない)
func (r *MongoSignUpRepo) RegisterUser(user models.User, store *models.Store, settings *models.StoreSetting, joinStoreID string) (*models.User, error) {
	err := runSignUpTransaction(func(sessCtx mongo.SessionContext) error {
		userCollection := db.GetCollection(DatabaseName, CollectionUserInfo)

		// ユーザー重複確認 (FirebaseUID, メールアドレス, 個人電話番号)
		var existingUser models.User
		err := userCollection.FindOne(sessCtx, bson.M{
			"status": bson.M{"$ne": models.UserStatusWithdrawn},
			"$or": []bson.M{
				{"firebase_uid": user.FirebaseUID},
				{"email": user.Email},
				{"phone": user.Phone},
			},
		}).Decode(&existingUser)
		if err == nil {
			return ErrUserAlreadyExists
		} else if err != mongo.ErrNoDocuments {
			return fmt.Errorf("database error during user check: %w", err)
		}

		if store != nil {
			if err := insertStoreWithDefaults(sessCtx, store, settings, user.ID); err != nil {
				return err
			}
			user.StoreID = store.StoreID
		}

		if joinStoreID != "" {
			var existingStore models.Store
			err := db.GetCollection(DatabaseName, CollectionStoreInfo).
				FindOne(sessCtx, bson.M{"store_id": joinStoreID}).Decode(&existingStore)
			if err == mongo.ErrNoDocuments {
				return ErrStoreNotFound
			} else if err != nil {
				return fmt.Errorf("database error while verifying store: %w", err)
			}
			user.StoreID = existingStore.StoreID
		}

		// ユーザー生成 (退会済みアカウントと同じメール/電話番号でも、別ドキュメント・
		// 別UIDの新規ユーザーとして作成する。退会済みの古いドキュメントはそのまま
		// 履歴として残る)
		if _, err := userCollection.InsertOne(sessCtx, user); err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}

		// スタッフとして店舗に参加する場合のみ store_staff_info にも追加 (初期状態は承認待ち)
		if joinStoreID != "" {
			now := time.Now()
			staffInfo := models.StoreStaffInfo{
				ID:        primitive.NewObjectID(),
				UserID:    user.ID,
				Role:      "staff",
				StoreID:   user.StoreID,
				Status:    models.StaffStatusPending,
				CreatedAt: now,
				UpdatedAt: now,
			}
			if _, err := db.GetCollection(DatabaseName, CollectionStoreStaffInfo).InsertOne(sessCtx, staffInfo); err != nil {
				return fmt.Errorf("failed to create store staff info: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// AddStoreForManager ログイン済みマネージャーの店舗を追加作成し、所有者のユーザーを返す
// - store/settings の所有者フィールド(UserID / ManagerID)はここで設定する
func (r *MongoSignUpRepo) AddStoreForManager(firebaseUID string, store *models.Store, settings *models.StoreSetting) (*models.User, error) {
	var owner models.User
	err := runSignUpTransaction(func(sessCtx mongo.SessionContext) error {
		err := db.GetCollection(DatabaseName, CollectionUserInfo).
			FindOne(sessCtx, bson.M{"firebase_uid": firebaseUID}).Decode(&owner)
		if err == mongo.ErrNoDocuments {
			return ErrUserNotFound
		} else if err != nil {
			return fmt.Errorf("database error while finding user: %w", err)
		}

		// 'manager'でない場合、新しい店舗登録を防ぐ
		if owner.Role != "manager" {
			return ErrNotManager
		}
		return insertStoreWithDefaults(sessCtx, store, settings, owner.ID)
	})
	if err != nil {
		return nil, err
	}
	return &owner, nil
}

// StoreExists 指定の store_id を持つ店舗が存在するかどうか
func (r *MongoSignUpRepo) StoreExists(storeID string) (bool, error) {
	return countExists(CollectionStoreInfo, bson.M{"store_id": storeID})
}

// IsEmailInUse 退会済みを除くユーザーがそのメールアドレスを使っているかどうか
// (退会は連絡先を残すソフトデリートのため、退会済みは新規登録を妨げない)
func (r *MongoSignUpRepo) IsEmailInUse(email string) (bool, error) {
	return countExists(CollectionUserInfo, bson.M{
		"email":  email,
		"status": bson.M{"$ne": models.UserStatusWithdrawn},
	})
}

// IsPhoneInUse 退会済みを除くユーザーがその電話番号を使っているかどうか
func (r *MongoSignUpRepo) IsPhoneInUse(phone string) (bool, error) {
	return countExists(CollectionUserInfo, bson.M{
		"phone":  phone,
		"status": bson.M{"$ne": models.UserStatusWithdrawn},
	})
}

// runSignUpTransaction fn を1つのMongoDBトランザクション内で実行する
func runSignUpTransaction(fn func(sessCtx mongo.SessionContext) error) error {
	client := db.Client()
	if client == nil {
		return ErrDatabaseUnavailable
	}

	ctx, cancel := context.WithTimeout(context.Background(), signUpTransactionTimeout)
	defer cancel()

	session, err := client.StartSession()
	if err != nil {
		return fmt.Errorf("failed to start database session: %w", err)
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sessCtx mongo.SessionContext) (interface{}, error) {
		return nil, fn(sessCtx)
	})
	return err
}

// insertStoreWithDefaults 店舗と、それに付随する初期データ(営業許可証の未提出レコード・店舗設定)を作成する
// - 会員登録時の同時作成と、ログイン後の店舗追加の両方で使う共通処理
// - 店舗電話番号の重複は ErrStorePhoneExists として返す
func insertStoreWithDefaults(sessCtx mongo.SessionContext, store *models.Store, settings *models.StoreSetting, ownerID primitive.ObjectID) error {
	store.UserID = ownerID
	settings.StoreID = store.StoreID
	settings.ManagerID = ownerID.Hex()

	storeCollection := db.GetCollection(DatabaseName, CollectionStoreInfo)
	count, err := storeCollection.CountDocuments(sessCtx, bson.M{"phone": store.Phone})
	if err != nil {
		return fmt.Errorf("database error during store phone check: %w", err)
	}
	if count > 0 {
		return ErrStorePhoneExists
	}

	if _, err := storeCollection.InsertOne(sessCtx, store); err != nil {
		return fmt.Errorf("failed to create store: %w", err)
	}

	now := time.Now()
	license := models.StoreLicense{
		ID:                 primitive.NewObjectID(),
		StoreID:            store.StoreID,
		VerificationStatus: models.StatusNotSubmitted,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if _, err := db.GetCollection(DatabaseName, CollectionStoreLicense).InsertOne(sessCtx, license); err != nil {
		return fmt.Errorf("failed to create license info: %w", err)
	}

	if _, err := db.GetCollection(DatabaseName, CollectionStoreSettings).InsertOne(sessCtx, settings); err != nil {
		return fmt.Errorf("failed to create default store settings: %w", err)
	}
	return nil
}

// countExists filter に一致するドキュメントが1件以上あるかどうか
func countExists(collectionName string, filter bson.M) (bool, error) {
	collection := db.GetCollection(DatabaseName, collectionName)
	if collection == nil {
		return false, ErrDatabaseUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
