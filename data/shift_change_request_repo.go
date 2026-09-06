package data

import (
	"context"
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ShiftChangeRequestRepository 週間シフト表に対する修正依頼の操作を抽象化するインターフェース
type ShiftChangeRequestRepository interface {
	CreateRequest(req models.ShiftChangeRequest) error
	GetRequestsForWeek(storeID, weekStartDate string) ([]models.ShiftChangeRequest, error)
	ResolveAppliedForWeek(storeID, weekStartDate string) ([]models.ShiftChangeRequest, error)
	RevertAppliedForWeek(storeID, weekStartDate string) (int, error)
	MarkRequestApplied(requestID string) error
	DeleteRequest(requestID string) error
}

type MongoShiftChangeRequestRepo struct{}

// CreateRequest 修正依頼を1件作成する (ステータス・作成日時はここで確定させる)
func (r *MongoShiftChangeRequestRepo) CreateRequest(req models.ShiftChangeRequest) error {
	collection := db.GetCollection(DatabaseName, CollectionShiftChangeRequests)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req.CreatedAt = time.Now()
	req.Status = models.ShiftChangeRequestStatusPending

	_, err := collection.InsertOne(ctx, req)
	if err != nil {
		log.Printf("Failed to create shift change request: %v", err)
		return err
	}
	return nil
}

// GetRequestsForWeek 指定週の修正依頼(pending/resolved問わず)を作成日時の新しい順に全件取得する
func (r *MongoShiftChangeRequestRepo) GetRequestsForWeek(storeID, weekStartDate string) ([]models.ShiftChangeRequest, error) {
	collection := db.GetCollection(DatabaseName, CollectionShiftChangeRequests)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{"store_id": storeID, "week_start_date": weekStartDate}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		log.Printf("Failed to find shift change requests: %v", err)
		return nil, err
	}
	defer cursor.Close(ctx)

	requests := []models.ShiftChangeRequest{}
	if err := cursor.All(ctx, &requests); err != nil {
		log.Printf("Failed to decode shift change requests: %v", err)
		return nil, err
	}
	return requests, nil
}

// ResolveAppliedForWeek 指定週の「下書きへ反映済み・未確定(applied)」な依頼を全て処理済み(resolved)にする。
// シフト表の確定(PublishShiftTableHandler)と同時に呼び、下書き上の対応を確定と同じタイミングで
// スタッフに見せるためのもの。まだ手を付けていない pending の依頼は未対応のまま残す
func (r *MongoShiftChangeRequestRepo) ResolveAppliedForWeek(storeID, weekStartDate string) ([]models.ShiftChangeRequest, error) {
	collection := db.GetCollection(DatabaseName, CollectionShiftChangeRequests)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{
		"store_id":        storeID,
		"week_start_date": weekStartDate,
		"status":          models.ShiftChangeRequestStatusApplied,
	}

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		log.Printf("Failed to find applied shift change requests: %v", err)
		return nil, err
	}
	applied := []models.ShiftChangeRequest{}
	decodeErr := cursor.All(ctx, &applied)
	cursor.Close(ctx)
	if decodeErr != nil {
		log.Printf("Failed to decode applied shift change requests: %v", decodeErr)
		return nil, decodeErr
	}

	if len(applied) == 0 {
		return applied, nil
	}

	resolvedAt := time.Now()
	update := bson.M{"$set": bson.M{
		"status":      models.ShiftChangeRequestStatusResolved,
		"resolved_at": resolvedAt,
	}}
	if _, err := collection.UpdateMany(ctx, filter, update); err != nil {
		log.Printf("Failed to resolve applied shift change requests: %v", err)
		return nil, err
	}

	for i := range applied {
		applied[i].Status = models.ShiftChangeRequestStatusResolved
		applied[i].ResolvedAt = &resolvedAt
	}
	return applied, nil
}

// RevertAppliedForWeek 指定週の「下書きへ反映済み・未確定(applied)」な依頼を未対応(pending)へ戻す。
// 下書きの破棄(DiscardShiftTableDraftHandler)と同時に呼ぶ。反映先の下書きを捨てる以上、
// 「反映済み」の状態だけ残すと、シフト表に無い変更が対応済み扱いで消えてしまうため
func (r *MongoShiftChangeRequestRepo) RevertAppliedForWeek(storeID, weekStartDate string) (int, error) {
	collection := db.GetCollection(DatabaseName, CollectionShiftChangeRequests)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{
		"store_id":        storeID,
		"week_start_date": weekStartDate,
		"status":          models.ShiftChangeRequestStatusApplied,
	}
	update := bson.M{
		"$set":   bson.M{"status": models.ShiftChangeRequestStatusPending},
		"$unset": bson.M{"resolved_at": ""},
	}

	result, err := collection.UpdateMany(ctx, filter, update)
	if err != nil {
		log.Printf("Failed to revert applied shift change requests: %v", err)
		return 0, err
	}
	return int(result.ModifiedCount), nil
}

// MarkRequestApplied 修正依頼を1件だけ「下書きへ反映済み・未確定(applied)」にする。
// 一括適用(ApplyShiftChangeRequestsHandler)は下書きしか触らないため、ここでは resolved にしない。
// 確定されるまでスタッフには pending に伏せて見せる
func (r *MongoShiftChangeRequestRepo) MarkRequestApplied(requestID string) error {
	collection := db.GetCollection(DatabaseName, CollectionShiftChangeRequests)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	objID, err := primitive.ObjectIDFromHex(requestID)
	if err != nil {
		return err
	}

	update := bson.M{"$set": bson.M{"status": models.ShiftChangeRequestStatusApplied}}
	if _, err := collection.UpdateOne(ctx, bson.M{"_id": objID}, update); err != nil {
		log.Printf("Failed to mark shift change request %s as applied: %v", requestID, err)
		return err
	}
	return nil
}

// DeleteRequest 修正依頼を1件削除する。一括適用で対応されずに残った依頼を、
// マネージャーが手動で一覧から消せるようにするために使う
func (r *MongoShiftChangeRequestRepo) DeleteRequest(requestID string) error {
	collection := db.GetCollection(DatabaseName, CollectionShiftChangeRequests)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	objID, err := primitive.ObjectIDFromHex(requestID)
	if err != nil {
		return err
	}

	if _, err := collection.DeleteOne(ctx, bson.M{"_id": objID}); err != nil {
		log.Printf("Failed to delete shift change request %s: %v", requestID, err)
		return err
	}
	return nil
}
