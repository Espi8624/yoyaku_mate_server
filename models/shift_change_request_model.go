package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ShiftChangeRequestStatus 修正依頼のステータス値
//
// pending -> applied -> resolved と遷移する。applied はマネージャーが下書きへ反映済みだが
// まだ確定していない中間状態で、マネージャーにしか見えない。スタッフ向けのレスポンスでは
// pending に伏せて返すため、確定前に「対応済み」と誤解されることがない
const (
	ShiftChangeRequestStatusPending = "pending"
	// ShiftChangeRequestStatusApplied 下書きへ反映済み・未確定 (マネージャーのみが見る中間状態)
	ShiftChangeRequestStatusApplied  = "applied"
	ShiftChangeRequestStatusResolved = "resolved"
)

// ShiftChangeRequest 週間シフト表の特定ブロックに対する修正依頼(スタッフ→マネージャー)。
// スタッフが自分の割当ブロックをタップし、「現在の割当(From)→希望する割当(To)」を
// 送信する。マネージャーは依頼一覧を見ながら通常のシフト編集機能で直接シフト表を修正し、
// 対応が済んだらまとめて処理済み(resolved)にする運用を想定
//
// From/To は自由記述ではなく、シフト表の Shift と同じ day/start_time/end_time 形式で
// 保持する。将来「依頼内容をそのまま UpdateShift の入力として使い、依頼から直接シフト表に
// 反映する」機能を追加する予定のため、その時にそのまま流用できるようあえて形式を統一している
type ShiftChangeRequest struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"_id,omitempty"`
	StoreID       string             `bson:"store_id" json:"store_id"`
	WeekStartDate string             `bson:"week_start_date" json:"week_start_date"`
	StaffID       primitive.ObjectID `bson:"staff_id" json:"staff_id"`
	// StaffName 依頼者の表示名。GET時に毎回名前解決するのを避けるため、
	// 依頼作成時点のuser_nameをそのまま保存する(本人が後で改名しても
	// 依頼一覧上の表示は変わらないが、この機能の用途上は問題ない)
	StaffName string `bson:"staff_name" json:"staff_name"`

	// TargetShiftID 依頼元になったシフトブロックのID(参考情報)。シフト表側で
	// 該当シフトが編集/削除されても依頼自体は残るため、必須項目としては扱わない
	TargetShiftID string `bson:"target_shift_id,omitempty" json:"target_shift_id,omitempty"`

	// From* 依頼時点の現在の割当
	FromDay       string `bson:"from_day" json:"from_day"`
	FromStartTime string `bson:"from_start_time" json:"from_start_time"`
	FromEndTime   string `bson:"from_end_time" json:"from_end_time"`

	// To* 希望する変更先
	ToDay       string `bson:"to_day" json:"to_day"`
	ToStartTime string `bson:"to_start_time" json:"to_start_time"`
	ToEndTime   string `bson:"to_end_time" json:"to_end_time"`

	Status     string     `bson:"status" json:"status"`
	CreatedAt  time.Time  `bson:"created_at" json:"created_at"`
	ResolvedAt *time.Time `bson:"resolved_at,omitempty" json:"resolved_at,omitempty"`
}
