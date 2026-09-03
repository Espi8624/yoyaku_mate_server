package data

import (
	"context"
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type StaffRepository interface {
	CheckStoreStaffExists(userID primitive.ObjectID, storeID string) (bool, error)
	CreateStoreStaffInfo(staffInfo models.StoreStaffInfo) error
	GetStoreStaffByStoreID(storeID string) ([]map[string]interface{}, error)
	GetStoreStaffByUserAndStore(userID primitive.ObjectID, storeID string) (*models.StoreStaffInfo, error)
	GetStoreStaffByID(staffID string) (*models.StoreStaffInfo, error)
	UpdateStoreStaffStatus(staffID, status string) error
	UpdateStoreStaffPermissions(staffID string, permissions []string) error
	UpdateStoreStaffAvailability(staffID string, availability models.Availability) error
}

type MongoStaffRepo struct{}

// decodeStoreStaffInfoWithRepair は FindOne の結果を models.StoreStaffInfo にデコードする。
// availability が旧スキーマ(曜日ごとに "MORNING"/"AFTERNOON" 等の文字列配列)のまま
// 残っていると、新スキーマ([]UnavailableRange{all_day,start_time,end_time})への
// デコードがドキュメント全体で失敗してしまう(store_user_repo.go の GetSettings に
// ある required_staff_count の自動修復と同じ考え方)。そのため availability フィールド
// だけ取り除いて再取得する自動修復を試みる(値は失われるため、対象スタッフはアプリの
// 勤務可能時間設定画面を開いて保存し直す必要がある)
func decodeStoreStaffInfoWithRepair(ctx context.Context, collection *mongo.Collection, filter bson.M) (*models.StoreStaffInfo, error) {
	var staffInfo models.StoreStaffInfo
	err := collection.FindOne(ctx, filter).Decode(&staffInfo)
	if err == nil {
		return &staffInfo, nil
	}
	if err == mongo.ErrNoDocuments {
		return nil, err
	}

	log.Printf("Failed to decode store_staff_info, attempting to repair legacy availability: %v", err)
	if _, repairErr := collection.UpdateOne(ctx, filter, bson.M{
		"$unset": bson.M{"availability": ""},
	}); repairErr != nil {
		log.Printf("Failed to repair store_staff_info: %v", repairErr)
		return nil, err
	}

	if retryErr := collection.FindOne(ctx, filter).Decode(&staffInfo); retryErr != nil {
		log.Printf("Failed to fetch store_staff_info after repair: %v", retryErr)
		return nil, retryErr
	}
	return &staffInfo, nil
}

func (r *MongoStaffRepo) CheckStoreStaffExists(userID primitive.ObjectID, storeID string) (bool, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{
		"user_id":  userID,
		"store_id": storeID,
	}

	count, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		log.Printf("Failed to check store_staff_info existence: %v", err)
		return false, err
	}

	return count > 0, nil
}

func (r *MongoStaffRepo) CreateStoreStaffInfo(info models.StoreStaffInfo) error {
	collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	info.CreatedAt = time.Now()
	info.UpdatedAt = time.Now()

	_, err := collection.InsertOne(ctx, info)
	if err != nil {
		log.Printf("Failed to create store_staff_info: %v", err)
		return err
	}
	return nil
}

func (r *MongoStaffRepo) GetStoreStaffByStoreID(storeID string) ([]map[string]interface{}, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"store_id": storeID}}},
		{{Key: "$lookup", Value: bson.M{
			"from":         CollectionUserInfo,
			"localField":   "user_id",
			"foreignField": "_id",
			"as":           "user_details",
		}}},
		{{Key: "$unwind", Value: "$user_details"}},
		{{Key: "$project", Value: bson.M{
			"_id":          1,
			"user_id":      1,
			"store_id":     1,
			"role":         1,
			"status":       1,
			"permissions":  1,
			"created_at":   1,
			"updated_at":   1,
			"availability": 1,
			"user_name":    "$user_details.user_name",
			"email":        "$user_details.email",
		}}},
	}

	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		log.Printf("Failed to aggregate store staff: %v", err)
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []map[string]interface{}
	if err = cursor.All(ctx, &results); err != nil {
		log.Printf("Failed to decode aggregation results: %v", err)
		return nil, err
	}

	return results, nil
}

func (r *MongoStaffRepo) UpdateStoreStaffStatus(staffID string, status string) error {
	collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	objID, err := primitive.ObjectIDFromHex(staffID)
	if err != nil {
		return err
	}

	update := bson.M{
		"$set": bson.M{
			"status":     status,
			"updated_at": time.Now(),
		},
	}

	_, err = collection.UpdateOne(ctx, bson.M{"_id": objID}, update)
	if err != nil {
		log.Printf("Failed to update store staff status: %v", err)
		return err
	}

	return nil
}

func (r *MongoStaffRepo) UpdateStoreStaffPermissions(staffID string, permissions []string) error {
	collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	objID, err := primitive.ObjectIDFromHex(staffID)
	if err != nil {
		return err
	}

	update := bson.M{
		"$set": bson.M{
			"permissions": permissions,
			"updated_at":  time.Now(),
		},
	}

	_, err = collection.UpdateOne(ctx, bson.M{"_id": objID}, update)
	if err != nil {
		log.Printf("Failed to update store staff permissions: %v", err)
		return err
	}

	return nil
}

// GetStoreStaffByUserAndStore は、指定されたユーザー・店舗の組み合わせに対応する店舗スタッフ情報を1件取得
func (r *MongoStaffRepo) GetStoreStaffByUserAndStore(userID primitive.ObjectID, storeID string) (*models.StoreStaffInfo, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	staffInfo, err := decodeStoreStaffInfoWithRepair(ctx, collection, bson.M{
		"user_id":  userID,
		"store_id": storeID,
	})
	if err != nil {
		if err != mongo.ErrNoDocuments {
			log.Printf("Failed to find store staff by user and store: %v", err)
		}
		return nil, err
	}

	return staffInfo, nil
}

// GetStoreStaffByID は、店舗スタッフ情報のIDから該当スタッフ情報を1件取得
func (r *MongoStaffRepo) GetStoreStaffByID(staffID string) (*models.StoreStaffInfo, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	objID, err := primitive.ObjectIDFromHex(staffID)
	if err != nil {
		return nil, err
	}

	staffInfo, err := decodeStoreStaffInfoWithRepair(ctx, collection, bson.M{"_id": objID})
	if err != nil {
		if err != mongo.ErrNoDocuments {
			log.Printf("Failed to find store staff by id: %v", err)
		}
		return nil, err
	}

	return staffInfo, nil
}

// UpdateStoreStaffAvailability は、スタッフの勤務可能な曜日・時間帯を更新
func (r *MongoStaffRepo) UpdateStoreStaffAvailability(staffID string, availability models.Availability) error {
	collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	objID, err := primitive.ObjectIDFromHex(staffID)
	if err != nil {
		return err
	}

	update := bson.M{
		"$set": bson.M{
			"availability": availability,
			"updated_at":   time.Now(),
		},
	}

	_, err = collection.UpdateOne(ctx, bson.M{"_id": objID}, update)
	if err != nil {
		log.Printf("Failed to update store staff availability: %v", err)
		return err
	}

	return nil
}

// CheckStaffApprovalStatus は、指定されたユーザーが指定された店舗でAPPROVED状態かどうかを確認 (Not in interface but used by user_permission.go logic)
func CheckStaffApprovalStatus(userID primitive.ObjectID, storeID string) (bool, error) {
	collection := db.GetCollection(DatabaseName, CollectionStoreStaffInfo)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// ここでは status しか使わないため、projectionで明示的にそれだけ取得する
	// (availabilityを含む全体をデコードすると、旧スキーマ(曜日ごとに文字列配列で
	// 保存された勤務可能時間帯)のドキュメントでデコードエラーになってしまうため)
	var staffInfo struct {
		Status string `bson:"status"`
	}
	err := collection.FindOne(ctx, bson.M{
		"user_id":  userID,
		"store_id": storeID,
	}, options.FindOne().SetProjection(bson.M{"status": 1})).Decode(&staffInfo)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			return false, nil // スタッフ情報が存在しない
		}
		return false, err
	}

	// APPROVED状態のみtrueを返す
	return staffInfo.Status == models.StaffStatusApproved, nil
}
