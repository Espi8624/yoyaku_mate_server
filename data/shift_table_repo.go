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
)

// ShiftTableRepository 店舗の週単位シフト表の操作を抽象化するインターフェース
type ShiftTableRepository interface {
	GetShiftTable(storeID, weekStartDate string) (*models.ShiftTable, error)
	CreateShiftTable(table models.ShiftTable) error
	AddShift(shiftTableID string, shift models.Shift) error
	UpdateShift(shiftTableID, shiftID string, shift models.Shift) error
	DeleteShift(shiftTableID, shiftID string) error
	ReplaceShifts(shiftTableID string, shifts []models.Shift) error
	GetStaffShiftCounts(storeID, beforeWeekStartDate string, lookbackWeeks int) (map[primitive.ObjectID]int, error)
}

type MongoShiftTableRepo struct{}

// GetShiftTable は、店舗・週開始日(月曜日)の組み合わせに対応するシフト表を1件取得
// 該当週にシフト表がまだ作成されていない場合は mongo.ErrNoDocuments を返す
func (r *MongoShiftTableRepo) GetShiftTable(storeID, weekStartDate string) (*models.ShiftTable, error) {
	collection := db.GetCollection(DatabaseName, CollectionShiftTables)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var table models.ShiftTable
	err := collection.FindOne(ctx, bson.M{
		"store_id":        storeID,
		"week_start_date": weekStartDate,
	}).Decode(&table)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, err
		}
		log.Printf("Failed to find shift table: %v", err)
		return nil, err
	}

	return &table, nil
}

// CreateShiftTable は、指定された週の空のシフト表を新規作成
func (r *MongoShiftTableRepo) CreateShiftTable(table models.ShiftTable) error {
	collection := db.GetCollection(DatabaseName, CollectionShiftTables)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	table.CreatedAt = time.Now()
	table.UpdatedAt = time.Now()

	_, err := collection.InsertOne(ctx, table)
	if err != nil {
		log.Printf("Failed to create shift table: %v", err)
		return err
	}
	return nil
}

// AddShift は、既存のシフト表にシフトを1件追加
func (r *MongoShiftTableRepo) AddShift(shiftTableID string, shift models.Shift) error {
	collection := db.GetCollection(DatabaseName, CollectionShiftTables)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	objID, err := primitive.ObjectIDFromHex(shiftTableID)
	if err != nil {
		return err
	}

	update := bson.M{
		"$push": bson.M{"shifts": shift},
		"$set":  bson.M{"updated_at": time.Now()},
	}

	_, err = collection.UpdateOne(ctx, bson.M{"_id": objID}, update)
	if err != nil {
		log.Printf("Failed to add shift: %v", err)
		return err
	}
	return nil
}

// UpdateShift は、シフト表内の特定シフト1件を更新
func (r *MongoShiftTableRepo) UpdateShift(shiftTableID, shiftID string, shift models.Shift) error {
	collection := db.GetCollection(DatabaseName, CollectionShiftTables)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tableObjID, err := primitive.ObjectIDFromHex(shiftTableID)
	if err != nil {
		return err
	}
	shiftObjID, err := primitive.ObjectIDFromHex(shiftID)
	if err != nil {
		return err
	}
	shift.ID = shiftObjID

	filter := bson.M{
		"_id":        tableObjID,
		"shifts._id": shiftObjID,
	}
	update := bson.M{
		"$set": bson.M{
			"shifts.$":   shift,
			"updated_at": time.Now(),
		},
	}

	_, err = collection.UpdateOne(ctx, filter, update)
	if err != nil {
		log.Printf("Failed to update shift: %v", err)
		return err
	}
	return nil
}

// ReplaceShifts は、シフト表の shifts 配列全体を丸ごと置き換える (自動配置機能用)
func (r *MongoShiftTableRepo) ReplaceShifts(shiftTableID string, shifts []models.Shift) error {
	collection := db.GetCollection(DatabaseName, CollectionShiftTables)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	objID, err := primitive.ObjectIDFromHex(shiftTableID)
	if err != nil {
		return err
	}

	update := bson.M{
		"$set": bson.M{
			"shifts":     shifts,
			"updated_at": time.Now(),
		},
	}

	_, err = collection.UpdateOne(ctx, bson.M{"_id": objID}, update)
	if err != nil {
		log.Printf("Failed to replace shifts: %v", err)
		return err
	}
	return nil
}

// GetStaffShiftCounts は、beforeWeekStartDate より前の直近 lookbackWeeks 週分の
// シフト表から、スタッフごとの担当シフト数(直近の累計)を集計する。
// 自動配置が「その週だけ」でなく複数週にまたがって公平に人員を回せるようにするために使う
func (r *MongoShiftTableRepo) GetStaffShiftCounts(storeID, beforeWeekStartDate string, lookbackWeeks int) (map[primitive.ObjectID]int, error) {
	collection := db.GetCollection(DatabaseName, CollectionShiftTables)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	beforeDate, err := time.Parse("2006-01-02", beforeWeekStartDate)
	if err != nil {
		return nil, err
	}
	sinceWeekStartDate := beforeDate.AddDate(0, 0, -7*lookbackWeeks).Format("2006-01-02")

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"store_id": storeID,
			// beforeWeekStartDate(今回自動配置する週)自体は含めない。
			// その週の分は呼び出し側で assignedCount に別途反映されるため
			"week_start_date": bson.M{
				"$gte": sinceWeekStartDate,
				"$lt":  beforeWeekStartDate,
			},
		}}},
		{{Key: "$unwind", Value: "$shifts"}},
		{{Key: "$group", Value: bson.M{
			"_id":   "$shifts.staff_id",
			"count": bson.M{"$sum": 1},
		}}},
	}

	cursor, err := collection.Aggregate(ctx, pipeline)
	if err != nil {
		log.Printf("Failed to aggregate staff shift counts: %v", err)
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []struct {
		StaffID primitive.ObjectID `bson:"_id"`
		Count   int                `bson:"count"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		log.Printf("Failed to decode staff shift counts: %v", err)
		return nil, err
	}

	counts := make(map[primitive.ObjectID]int, len(results))
	for _, res := range results {
		counts[res.StaffID] = res.Count
	}
	return counts, nil
}

// DeleteShift は、シフト表内の特定シフト1件を削除
func (r *MongoShiftTableRepo) DeleteShift(shiftTableID, shiftID string) error {
	collection := db.GetCollection(DatabaseName, CollectionShiftTables)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tableObjID, err := primitive.ObjectIDFromHex(shiftTableID)
	if err != nil {
		return err
	}
	shiftObjID, err := primitive.ObjectIDFromHex(shiftID)
	if err != nil {
		return err
	}

	update := bson.M{
		"$pull": bson.M{"shifts": bson.M{"_id": shiftObjID}},
		"$set":  bson.M{"updated_at": time.Now()},
	}

	_, err = collection.UpdateOne(ctx, bson.M{"_id": tableObjID}, update)
	if err != nil {
		log.Printf("Failed to delete shift: %v", err)
		return err
	}
	return nil
}
