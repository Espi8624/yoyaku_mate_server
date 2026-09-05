package data

import (
	"context"
	"errors"
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// - last_seen_at の更新間隔。リクエストのたびに書き込むとDBへの書き込みが無駄に増えるため、
//   この間隔を超えた場合にのみ更新する
const SessionTouchInterval = 5 * time.Minute

// SessionRepository 端末セッションの操作を抽象化するインターフェース
type SessionRepository interface {
	Create(session models.Session) (*models.Session, error)
	FindBySessionID(sessionID string) (*models.Session, error)
	FindActiveByDevice(userID primitive.ObjectID, deviceID string) (*models.Session, error)
	RevokeOthers(userID primitive.ObjectID, keepSessionID string, reason string) error
	Revoke(sessionID string, reason string) error
	TouchLastSeen(session *models.Session) error
}

type MongoSessionRepo struct{}

// Create セッションを1件作成する (作成日時・最終アクセス日時はここで確定させる)
func (r *MongoSessionRepo) Create(session models.Session) (*models.Session, error) {
	collection := db.GetCollection(DatabaseName, CollectionSessions)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	now := time.Now()
	session.CreatedAt = now
	session.LastSeenAt = now
	session.RevokedAt = nil
	session.RevokedReason = ""

	result, err := collection.InsertOne(ctx, session)
	if err != nil {
		log.Printf("Failed to create session: %v", err)
		return nil, err
	}

	if oid, ok := result.InsertedID.(primitive.ObjectID); ok {
		session.ID = oid
	}
	return &session, nil
}

// FindBySessionID セッショントークンからセッションを取得する。
// - 無効化済みのものも含めて返す。「未知のトークン」と「無効化されたトークン」を
//   呼び出し側で区別する必要があるため、ここでフィルタしてはいけない
// - 見つからない場合は (nil, nil) を返す
func (r *MongoSessionRepo) FindBySessionID(sessionID string) (*models.Session, error) {
	collection := db.GetCollection(DatabaseName, CollectionSessions)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var session models.Session
	err := collection.FindOne(ctx, bson.M{"session_id": sessionID}).Decode(&session)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		log.Printf("Failed to find session: %v", err)
		return nil, err
	}
	return &session, nil
}

// FindActiveByDevice 同一端末の有効なセッションを取得する。
// - 同じ端末からの再発行要求で新規セッションを作ってしまうと、その端末が自分自身を
//   無効化して無限ログアウトに陥るため、再利用できるセッションを探すために使う
// - 見つからない場合は (nil, nil) を返す
func (r *MongoSessionRepo) FindActiveByDevice(userID primitive.ObjectID, deviceID string) (*models.Session, error) {
	collection := db.GetCollection(DatabaseName, CollectionSessions)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{
		"user_id":    userID,
		"device_id":  deviceID,
		"revoked_at": nil,
	}

	var session models.Session
	err := collection.FindOne(ctx, filter).Decode(&session)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		log.Printf("Failed to find active session by device: %v", err)
		return nil, err
	}
	return &session, nil
}

// RevokeOthers 指定セッション以外の、そのユーザーの有効なセッションを全て無効化する
// - 1端末のみ許可ポリシーの実体。keepSessionID には今ログインした端末のセッションを渡す
func (r *MongoSessionRepo) RevokeOthers(userID primitive.ObjectID, keepSessionID string, reason string) error {
	collection := db.GetCollection(DatabaseName, CollectionSessions)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{
		"user_id":    userID,
		"session_id": bson.M{"$ne": keepSessionID},
		"revoked_at": nil,
	}
	update := bson.M{
		"$set": bson.M{
			"revoked_at":     time.Now(),
			"revoked_reason": reason,
		},
	}

	_, err := collection.UpdateMany(ctx, filter, update)
	if err != nil {
		log.Printf("Failed to revoke other sessions: %v", err)
		return err
	}
	return nil
}

// Revoke 指定セッションを無効化する (明示的なログアウト等)
func (r *MongoSessionRepo) Revoke(sessionID string, reason string) error {
	collection := db.GetCollection(DatabaseName, CollectionSessions)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{"session_id": sessionID, "revoked_at": nil}
	update := bson.M{
		"$set": bson.M{
			"revoked_at":     time.Now(),
			"revoked_reason": reason,
		},
	}

	_, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		log.Printf("Failed to revoke session: %v", err)
		return err
	}
	return nil
}

// TouchLastSeen 最終アクセス日時を更新する
// - SessionTouchInterval 以内に更新済みの場合は何もしない (書き込み削減)
func (r *MongoSessionRepo) TouchLastSeen(session *models.Session) error {
	if session == nil || time.Since(session.LastSeenAt) < SessionTouchInterval {
		return nil
	}

	collection := db.GetCollection(DatabaseName, CollectionSessions)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	update := bson.M{"$set": bson.M{"last_seen_at": time.Now()}}
	_, err := collection.UpdateOne(ctx, bson.M{"session_id": session.SessionID}, update)
	if err != nil {
		log.Printf("Failed to update session last_seen_at: %v", err)
		return err
	}
	return nil
}
