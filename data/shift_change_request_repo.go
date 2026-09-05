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
	ResolvePendingForWeek(storeID, weekStartDate string) ([]models.ShiftChangeRequest, error)
	ResolveRequest(requestID string) error
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

// ResolvePendingForWeek 指定週の未処理(pending)な修正依頼を全て処理済み(resolved)にする。
// マネージャーが編集のたびに個別処理するのではなく、まとめて「確定」した時に一括で呼ぶ想定。
// 通知(将来のプッシュ通知等)の送信対象を呼び出し側で判断できるよう、処理した依頼一覧を返す
func (r *MongoShiftChangeRequestRepo) ResolvePendingForWeek(storeID, weekStartDate string) ([]models.ShiftChangeRequest, error) {
	collection := db.GetCollection(DatabaseName, CollectionShiftChangeRequests)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	filter := bson.M{
		"store_id":        storeID,
		"week_start_date": weekStartDate,
		"status":          models.ShiftChangeRequestStatusPending,
	}

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		log.Printf("Failed to find pending shift change requests: %v", err)
		return nil, err
	}
	pending := []models.ShiftChangeRequest{}
	decodeErr := cursor.All(ctx, &pending)
	cursor.Close(ctx)
	if decodeErr != nil {
		log.Printf("Failed to decode pending shift change requests: %v", decodeErr)
		return nil, decodeErr
	}

	if len(pending) == 0 {
		return pending, nil
	}

	resolvedAt := time.Now()
	update := bson.M{"$set": bson.M{
		"status":      models.ShiftChangeRequestStatusResolved,
		"resolved_at": resolvedAt,
	}}
	if _, err := collection.UpdateMany(ctx, filter, update); err != nil {
		log.Printf("Failed to resolve shift change requests: %v", err)
		return nil, err
	}

	for i := range pending {
		pending[i].Status = models.ShiftChangeRequestStatusResolved
		pending[i].ResolvedAt = &resolvedAt
	}
	return pending, nil
}

// ResolveRequest 修正依頼を1件だけ処理済み(resolved)にする。一括適用(ApplyShiftChangeRequestsHandler)が
// 実際にシフト表へ反映できた依頼を、その場でresolvedにするために使う
func (r *MongoShiftChangeRequestRepo) ResolveRequest(requestID string) error {
	collection := db.GetCollection(DatabaseName, CollectionShiftChangeRequests)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	objID, err := primitive.ObjectIDFromHex(requestID)
	if err != nil {
		return err
	}

	resolvedAt := time.Now()
	update := bson.M{"$set": bson.M{
		"status":      models.ShiftChangeRequestStatusResolved,
		"resolved_at": resolvedAt,
	}}
	if _, err := collection.UpdateOne(ctx, bson.M{"_id": objID}, update); err != nil {
		log.Printf("Failed to resolve shift change request %s: %v", requestID, err)
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
