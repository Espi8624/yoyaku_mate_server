package data

import (
	"context"
	"fmt"
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
	GetStaffPairCounts(storeID, beforeWeekStartDate string, lookbackWeeks int) (map[string]int, error)
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

// GetStaffPairCounts は、beforeWeekStartDate より前の直近 lookbackWeeks 週分の
// シフト表から、同じ曜日・時間帯が重なって一緒に働いたスタッフの組み合わせ(ペア)ごとの
// 回数を集計する。自動配置で「同じ2人ばかり組ませない」ようペア反復にペナルティを
// かけるための初期値として使う。ペアの重なり判定は自己結合になり集計パイプラインで
// 表現するより複雑になるため、対象期間のシフト表をそのまま取得してGo側で判定する
func (r *MongoShiftTableRepo) GetStaffPairCounts(storeID, beforeWeekStartDate string, lookbackWeeks int) (map[string]int, error) {
	collection := db.GetCollection(DatabaseName, CollectionShiftTables)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	beforeDate, err := time.Parse("2006-01-02", beforeWeekStartDate)
	if err != nil {
		return nil, err
	}
	sinceWeekStartDate := beforeDate.AddDate(0, 0, -7*lookbackWeeks).Format("2006-01-02")

	cursor, err := collection.Find(ctx, bson.M{
		"store_id": storeID,
		// beforeWeekStartDate(今回自動配置する週)自体は含めない。呼び出し側で
		// 今回の割り当て分は別途 pairAssignedCount に反映されるため
		"week_start_date": bson.M{
			"$gte": sinceWeekStartDate,
			"$lt":  beforeWeekStartDate,
		},
	})
	if err != nil {
		log.Printf("Failed to fetch shift tables for pair counts: %v", err)
		return nil, err
	}
	defer cursor.Close(ctx)

	var tables []models.ShiftTable
	if err := cursor.All(ctx, &tables); err != nil {
		log.Printf("Failed to decode shift tables for pair counts: %v", err)
		return nil, err
	}

	counts := map[string]int{}
	for _, table := range tables {
		// 曜日ごとにグループ化してから、その曜日内で時間帯が重なる組み合わせだけを数える
		byDay := map[string][]models.Shift{}
		for _, s := range table.Shifts {
			byDay[s.Day] = append(byDay[s.Day], s)
		}
		for _, shifts := range byDay {
			for i := 0; i < len(shifts); i++ {
				for j := i + 1; j < len(shifts); j++ {
					if shifts[i].StaffID == shifts[j].StaffID {
						continue
					}
					if shiftTimesOverlap(shifts[i], shifts[j]) {
						counts[pairKey(shifts[i].StaffID, shifts[j].StaffID)]++
					}
				}
			}
		}
	}
	return counts, nil
}

// shiftTimesOverlap 2つのシフトの [start_time, end_time) が重なるかどうか
func shiftTimesOverlap(a, b models.Shift) bool {
	aStart, ok1 := parseHHMM(a.StartTime)
	aEnd, ok2 := parseHHMM(a.EndTime)
	bStart, ok3 := parseHHMM(b.StartTime)
	bEnd, ok4 := parseHHMM(b.EndTime)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return false
	}
	return aStart < bEnd && bStart < aEnd
}

// parseHHMM "HH:MM" 形式の時刻を、0時からの経過分に変換する
func parseHHMM(t string) (int, bool) {
	var h, m int
	if _, err := fmt.Sscanf(t, "%d:%d", &h, &m); err != nil {
		return 0, false
	}
	return h*60 + m, true
}

// pairKey 2人分のIDから、順序に依存しない一意なマップキーを作る("aHex|bHex"、必ず
// 小さい方が先)。handlers側(shift_table_handler.go)にも全く同じフォーマットの
// pairKeyがあり、シフト実績からの初期値(このファイル)と自動配置実行中の累積値
// (handlers側)でキーが一致する必要があるため、フォーマットを変更する際は両方合わせること
func pairKey(a, b primitive.ObjectID) string {
	aHex, bHex := a.Hex(), b.Hex()
	if aHex > bHex {
		aHex, bHex = bHex, aHex
	}
	return aHex + "|" + bHex
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
