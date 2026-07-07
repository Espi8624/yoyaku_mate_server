package auth

import (
	"context"
	"log"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
)

var firebaseAuth *auth.Client

func init() {
	opt := option.WithCredentialsFile("config/serviceAccountKey.json")
	app, err := firebase.NewApp(context.Background(), nil, opt)
	if err != nil {
		log.Fatalf("Firebase初期化エラー: %v\n", err)
	}

	client, err := app.Auth(context.Background())
	if err != nil {
		log.Fatalf("Firebase Authクライアント生成エラー: %v\n", err)
	}
	firebaseAuth = client
	log.Println("Firebase Authクライアント初期化完了")
}

// フロントエンドから受け取ったIDトークンを検証し、UIDを返却
func VerifyIDToken(ctx context.Context, idToken string) (string, error) {
	token, err := firebaseAuth.VerifyIDToken(ctx, idToken)
	if err != nil {
		return "", err
	}
	return token.UID, nil
}

// IDトークンを検証し、UID と emailVerified の両方を返却（会員登録専用）
func VerifyIDTokenWithEmailVerified(ctx context.Context, idToken string) (string, bool, error) {
	token, err := firebaseAuth.VerifyIDToken(ctx, idToken)
	if err != nil {
		return "", false, err
	}
	emailVerified, _ := token.Claims["email_verified"].(bool)
	return token.UID, emailVerified, nil
}

// メールアドレスでFirebaseユーザーを取得（存在確認用）
func GetUserByEmail(ctx context.Context, email string) (*auth.UserRecord, error) {
	return firebaseAuth.GetUserByEmail(ctx, email)
}
