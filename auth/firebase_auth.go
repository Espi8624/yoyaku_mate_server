package auth

import (
	"context"
	"errors"
	"log"
	"os"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/option"
)

// - 認証情報ファイルの既定パス。プロセスの作業ディレクトリからの相対パスであることに注意
const defaultCredentialsPath = "config/serviceAccountKey.json"

// - コンテナ環境(Infisical等)から認証情報JSONを直接受け取る場合の環境変数名
//   ファイルは.gitignore対象でイメージに含まれないため、本番/dev環境ではこちらを使う
const credentialsJSONEnvVar = "FIREBASE_SERVICE_ACCOUNT_JSON"

var firebaseAuth *auth.Client

// ErrFirebaseNotInitialized InitFirebaseを呼ばずに認証機能を使おうとした場合のエラー
var ErrFirebaseNotInitialized = errors.New("firebase auth is not initialized: call auth.InitFirebase() first")

// InitFirebase Firebase Authクライアントを初期化する
//   - main から明示的に呼ぶ。init() で行うと、Firebaseを使わないテストであっても
//     パッケージをimportしただけで認証情報ファイルを要求してしまい、
//     しかもパスが作業ディレクトリ相対のためテスト実行時に解決できない
//   - 起動時に失敗させたい (fail-fast) 判断は呼び出し側に委ねる
//   - config.Load()と同じ方針: 環境変数(Infisical注入)があれば優先し、なければ
//     ローカル開発用のファイルにフォールバックする
func InitFirebase() error {
	var opt option.ClientOption
	if credentialsJSON := os.Getenv(credentialsJSONEnvVar); credentialsJSON != "" {
		log.Printf("Using %s from environment variable", credentialsJSONEnvVar)
		opt = option.WithCredentialsJSON([]byte(credentialsJSON))
	} else {
		opt = option.WithCredentialsFile(defaultCredentialsPath)
	}

	app, err := firebase.NewApp(context.Background(), nil, opt)
	if err != nil {
		return err
	}

	client, err := app.Auth(context.Background())
	if err != nil {
		return err
	}

	firebaseAuth = client
	log.Println("Firebase Authクライアント初期化完了")
	return nil
}

// フロントエンドから受け取ったIDトークンを検証し、UIDを返却
func VerifyIDToken(ctx context.Context, idToken string) (string, error) {
	if firebaseAuth == nil {
		return "", ErrFirebaseNotInitialized
	}
	token, err := firebaseAuth.VerifyIDToken(ctx, idToken)
	if err != nil {
		return "", err
	}
	return token.UID, nil
}

// IDトークンを検証し、UID と emailVerified の両方を返却（会員登録専用）
func VerifyIDTokenWithEmailVerified(ctx context.Context, idToken string) (string, bool, error) {
	if firebaseAuth == nil {
		return "", false, ErrFirebaseNotInitialized
	}
	token, err := firebaseAuth.VerifyIDToken(ctx, idToken)
	if err != nil {
		return "", false, err
	}
	emailVerified, _ := token.Claims["email_verified"].(bool)
	return token.UID, emailVerified, nil
}

// メールアドレスでFirebaseユーザーを取得（存在確認用）
func GetUserByEmail(ctx context.Context, email string) (*auth.UserRecord, error) {
	if firebaseAuth == nil {
		return nil, ErrFirebaseNotInitialized
	}
	return firebaseAuth.GetUserByEmail(ctx, email)
}

// UIDでFirebaseユーザーを削除（放置された未認証アカウント・会員退会時の整理用）
func DeleteUser(ctx context.Context, uid string) error {
	if firebaseAuth == nil {
		return ErrFirebaseNotInitialized
	}
	return firebaseAuth.DeleteUser(ctx, uid)
}
