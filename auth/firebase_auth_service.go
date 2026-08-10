package auth

import (
	"context"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// FirebaseAuthService Firebase認証サービスの実装
// - handlers.AuthService インターフェースを満たす
type FirebaseAuthService struct{}

// VerifyIDToken Firebase IDトークンを検証し、UIDを返す
func (s *FirebaseAuthService) VerifyIDToken(ctx context.Context, idToken string) (string, error) {
	return VerifyIDToken(ctx, idToken)
}

// VerifyLoginToken 指定ユーザーのログイントークンを検証する
func (s *FirebaseAuthService) VerifyLoginToken(userID primitive.ObjectID, token string) (bool, error) {
	return VerifyLoginToken(userID, token)
}
