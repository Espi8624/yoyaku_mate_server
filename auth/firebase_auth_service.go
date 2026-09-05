package auth

import (
	"context"
)

// FirebaseAuthService Firebase認証サービスの実装
// - handlers.AuthService インターフェースを満たす
type FirebaseAuthService struct{}

// VerifyIDToken Firebase IDトークンを検証し、UIDを返す
func (s *FirebaseAuthService) VerifyIDToken(ctx context.Context, idToken string) (string, error) {
	return VerifyIDToken(ctx, idToken)
}

