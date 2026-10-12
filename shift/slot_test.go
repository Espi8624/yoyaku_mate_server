package shift

import (
	"testing"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// - 下書き/確定版の分離は「スタッフに作りかけが見えない」ことが要件そのものなので、
//   未確定判定をテストで固定する

func newShift(staffID primitive.ObjectID, day, start, end string) models.Shift {
	return models.Shift{
		ID: primitive.NewObjectID(), StaffID: staffID,
		Day: day, StartTime: start, EndTime: end,
	}
}

// TestContentEqual シフトの同一判定が_idではなく中身で行われることを確認する
func TestContentEqual(t *testing.T) {
	staffA := primitive.NewObjectID()
	staffB := primitive.NewObjectID()

	t.Run("_idが違っても中身が同じなら等しい", func(t *testing.T) {
		// - 削除して同じ内容を作り直すとIDだけが変わる。これを未確定の変更として
		//   扱うと、実質何も変えていないのに確定ボタンが出続けてしまう
		a := []models.Shift{newShift(staffA, "monday", "09:00", "15:30")}
		b := []models.Shift{newShift(staffA, "monday", "09:00", "15:30")}
		if !ContentEqual(a, b) {
			t.Error("_idの違いだけで別物と判定された")
		}
	})

	t.Run("並び順が違っても等しい", func(t *testing.T) {
		a := []models.Shift{
			newShift(staffA, "monday", "09:00", "15:30"),
			newShift(staffB, "tuesday", "15:30", "22:00"),
		}
		b := []models.Shift{
			newShift(staffB, "tuesday", "15:30", "22:00"),
			newShift(staffA, "monday", "09:00", "15:30"),
		}
		if !ContentEqual(a, b) {
			t.Error("並び順の違いで別物と判定された")
		}
	})

	t.Run("担当者が違えば等しくない", func(t *testing.T) {
		a := []models.Shift{newShift(staffA, "monday", "09:00", "15:30")}
		b := []models.Shift{newShift(staffB, "monday", "09:00", "15:30")}
		if ContentEqual(a, b) {
			t.Error("担当者の違いが検出されていない")
		}
	})

	t.Run("時間帯が違えば等しくない", func(t *testing.T) {
		a := []models.Shift{newShift(staffA, "monday", "09:00", "15:30")}
		b := []models.Shift{newShift(staffA, "monday", "15:30", "22:00")}
		if ContentEqual(a, b) {
			t.Error("時間帯の違いが検出されていない")
		}
	})

	t.Run("件数が違えば等しくない", func(t *testing.T) {
		a := []models.Shift{newShift(staffA, "monday", "09:00", "15:30")}
		b := []models.Shift{
			newShift(staffA, "monday", "09:00", "15:30"),
			newShift(staffB, "tuesday", "09:00", "15:30"),
		}
		if ContentEqual(a, b) {
			t.Error("件数の違いが検出されていない")
		}
	})

	t.Run("空同士は等しい", func(t *testing.T) {
		if !ContentEqual(nil, []models.Shift{}) {
			t.Error("nilと空スライスが別物と判定された")
		}
	})
}
