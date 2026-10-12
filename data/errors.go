package data

import "errors"

// リポジトリが返す既知のエラー
//   - 呼び出し側(ハンドラ)は errors.Is で判定してHTTPステータスを決める。
//     エラー文字列の部分一致で判定すると、文言の大文字小文字や言い回しの違いで
//     400のつもりが500になる、といった取りこぼしが起きるため
//   - 一部はメッセージがそのままクライアントへ返るため、既存のレスポンス文言を変えないこと
var (
	// ErrDatabaseUnavailable DBに未接続 (起動直後や障害中で db.GetCollection が nil を返す状態)
	ErrDatabaseUnavailable = errors.New("database is not connected")

	ErrUserAlreadyExists = errors.New("user with this email or phone number already exists")
	ErrUserNotFound      = errors.New("user not found")
	ErrNotManager        = errors.New("only managers can add new stores")
	ErrStoreNotFound     = errors.New("store with the provided ID not found")
	ErrStorePhoneExists  = errors.New("a store with this phone number already exists")
)
