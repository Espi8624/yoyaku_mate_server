package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// セッション無効化の理由
// - 問い合わせ対応時に「なぜログアウトされたのか」を追跡できるようにするため、無効化時は必ず理由を残す
const (
	// - 別端末で新たにログインしたため無効化された (1端末のみ許可ポリシー)
	SessionRevokedReasonNewLogin = "new_login"
	// - 本人が明示的にログアウトした
	SessionRevokedReasonLogout = "logout"
)

// sessions モデル (端末ごとのログインセッション)
// - ユーザー文書上の単一フィールドではなく端末ごとに1ドキュメントとして保持することで、
//   「セッションを持っていない」と「他端末に奪われた」を区別できるようにしている。
//   前者は静かに再発行すべき状況、後者だけがユーザーに通知すべき状況であり、
//   単一フィールド方式では両者が区別できず誤検知の原因になる
type Session struct {
	ID primitive.ObjectID `bson:"_id,omitempty" json:"-"`

	// - クライアントに渡す不透明なトークン。ObjectIDをそのまま使うと生成時刻等が推測可能になるため、
	//   独立したランダム文字列を用いる
	SessionID string `bson:"session_id" json:"session_id"`

	UserID primitive.ObjectID `bson:"user_id" json:"-"`

	// - クライアントが生成し端末内に保存する識別子。
	//   再インストールすると変わるが、それは「別の端末として扱う」という意図通りの挙動
	DeviceID string `bson:"device_id" json:"device_id"`

	// - 表示用の端末名・プラットフォーム (将来の「ログイン中の端末一覧」表示を想定)
	DeviceName string `bson:"device_name,omitempty" json:"device_name,omitempty"`
	Platform   string `bson:"platform,omitempty" json:"platform,omitempty"`

	CreatedAt  time.Time `bson:"created_at" json:"created_at"`
	LastSeenAt time.Time `bson:"last_seen_at" json:"last_seen_at"`

	// - RevokedAtがnilのセッションのみ有効
	RevokedAt     *time.Time `bson:"revoked_at,omitempty" json:"revoked_at,omitempty"`
	RevokedReason string     `bson:"revoked_reason,omitempty" json:"revoked_reason,omitempty"`
}

// IsActive セッションが有効(未無効化)かどうかを返す
func (s *Session) IsActive() bool {
	return s != nil && s.RevokedAt == nil
}
