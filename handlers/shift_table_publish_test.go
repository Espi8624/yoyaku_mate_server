package handlers

import (
	"testing"
	"time"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// - 下書き/確定版の分離は「スタッフに作りかけが見えない」ことが要件そのものなので、
//   未確定判定・スタッフ向けの伏せ字化をテストで固定する

func shift(staffID primitive.ObjectID, day, start, end string) models.Shift {
	return models.Shift{
		ID: primitive.NewObjectID(), StaffID: staffID,
		Day: day, StartTime: start, EndTime: end,
	}
}

// TestShiftsContentEqual シフトの同一判定が_idではなく中身で行われることを確認する
func TestShiftsContentEqual(t *testing.T) {
	staffA := primitive.NewObjectID()
	staffB := primitive.NewObjectID()

	t.Run("_idが違っても中身が同じなら等しい", func(t *testing.T) {
		// - 削除して同じ内容を作り直すとIDだけが変わる。これを未確定の変更として
		//   扱うと、実質何も変えていないのに確定ボタンが出続けてしまう
		a := []models.Shift{shift(staffA, "monday", "09:00", "15:30")}
		b := []models.Shift{shift(staffA, "monday", "09:00", "15:30")}
		if !shiftsContentEqual(a, b) {
			t.Error("_idの違いだけで別物と判定された")
		}
	})

	t.Run("並び順が違っても等しい", func(t *testing.T) {
		a := []models.Shift{
			shift(staffA, "monday", "09:00", "15:30"),
			shift(staffB, "tuesday", "15:30", "22:00"),
		}
		b := []models.Shift{
			shift(staffB, "tuesday", "15:30", "22:00"),
			shift(staffA, "monday", "09:00", "15:30"),
		}
		if !shiftsContentEqual(a, b) {
			t.Error("並び順の違いで別物と判定された")
		}
	})

	t.Run("担当者が違えば等しくない", func(t *testing.T) {
		a := []models.Shift{shift(staffA, "monday", "09:00", "15:30")}
		b := []models.Shift{shift(staffB, "monday", "09:00", "15:30")}
		if shiftsContentEqual(a, b) {
			t.Error("担当者の違いが検出されていない")
		}
	})

	t.Run("時間帯が違えば等しくない", func(t *testing.T) {
		a := []models.Shift{shift(staffA, "monday", "09:00", "15:30")}
		b := []models.Shift{shift(staffA, "monday", "15:30", "22:00")}
		if shiftsContentEqual(a, b) {
			t.Error("時間帯の違いが検出されていない")
		}
	})

	t.Run("件数が違えば等しくない", func(t *testing.T) {
		a := []models.Shift{shift(staffA, "monday", "09:00", "15:30")}
		b := []models.Shift{
			shift(staffA, "monday", "09:00", "15:30"),
			shift(staffB, "tuesday", "09:00", "15:30"),
		}
		if shiftsContentEqual(a, b) {
			t.Error("件数の違いが検出されていない")
		}
	})

	t.Run("空同士は等しい", func(t *testing.T) {
		if !shiftsContentEqual(nil, []models.Shift{}) {
			t.Error("nilと空スライスが別物と判定された")
		}
	})
}

// TestMaskAppliedAsPending スタッフには applied を pending として見せることを確認する
func TestMaskAppliedAsPending(t *testing.T) {
	resolvedAt := time.Now()
	requests := []models.ShiftChangeRequest{
		{ID: primitive.NewObjectID(), Status: models.ShiftChangeRequestStatusPending},
		{ID: primitive.NewObjectID(), Status: models.ShiftChangeRequestStatusApplied, ResolvedAt: &resolvedAt},
		{ID: primitive.NewObjectID(), Status: models.ShiftChangeRequestStatusResolved, ResolvedAt: &resolvedAt},
	}

	masked := maskAppliedAsPending(requests)

	t.Run("appliedはpendingに伏せられる", func(t *testing.T) {
		// - 確定前に「対応済み」と見えると、スタッフのシフト表は変わっていないのに
		//   対応が終わったと誤解される
		if masked[1].Status != models.ShiftChangeRequestStatusPending {
			t.Errorf("appliedが伏せられていない: %s", masked[1].Status)
		}
		if masked[1].ResolvedAt != nil {
			t.Error("伏せた依頼に対応日時が残っている")
		}
	})

	t.Run("確定済みのresolvedはそのまま見せる", func(t *testing.T) {
		if masked[2].Status != models.ShiftChangeRequestStatusResolved {
			t.Errorf("resolvedが書き換わっている: %s", masked[2].Status)
		}
	})

	t.Run("元のスライスを書き換えない", func(t *testing.T) {
		// - マネージャー向けの応答と共有されるため、破壊的に書き換えてはいけない
		if requests[1].Status != models.ShiftChangeRequestStatusApplied {
			t.Error("引数のスライスが書き換えられている")
		}
	})
}

// stubChangeRequestRepo hasUnpublishedChanges の判定だけを見るための最小スタブ
type stubChangeRequestRepo struct {
	requests []models.ShiftChangeRequest
}

func (s *stubChangeRequestRepo) CreateRequest(models.ShiftChangeRequest) error { return nil }
func (s *stubChangeRequestRepo) GetRequestsForWeek(string, string) ([]models.ShiftChangeRequest, error) {
	return s.requests, nil
}
func (s *stubChangeRequestRepo) ResolveAppliedForWeek(string, string) ([]models.ShiftChangeRequest, error) {
	return nil, nil
}
func (s *stubChangeRequestRepo) RevertAppliedForWeek(string, string) (int, error) {
	return 0, nil
}
func (s *stubChangeRequestRepo) MarkRequestApplied(string) error { return nil }
func (s *stubChangeRequestRepo) DeleteRequest(string) error      { return nil }

// TestHasUnpublishedChanges 確定ボタンの出し分け条件を確認する
func TestHasUnpublishedChanges(t *testing.T) {
	staffA := primitive.NewObjectID()
	publishedAt := time.Now()
	draft := []models.Shift{shift(staffA, "monday", "09:00", "15:30")}

	newTable := func(published []models.Shift, at *time.Time) *models.ShiftTable {
		return &models.ShiftTable{
			StoreID: "store1", WeekStartDate: "2026-09-07",
			Shifts: draft, PublishedShifts: published, PublishedAt: at,
		}
	}

	t.Run("一度も確定していなければ未確定", func(t *testing.T) {
		h := &ShiftTableHandler{changeRequestRepo: &stubChangeRequestRepo{}}
		got, err := h.hasUnpublishedChanges(newTable(nil, nil))
		if err != nil {
			t.Fatalf("想定外のエラー: %v", err)
		}
		if !got {
			t.Error("未確定と判定されていない")
		}
	})

	t.Run("下書きと確定版が同じで applied も無ければ確定済み", func(t *testing.T) {
		h := &ShiftTableHandler{changeRequestRepo: &stubChangeRequestRepo{
			requests: []models.ShiftChangeRequest{
				{Status: models.ShiftChangeRequestStatusResolved},
				{Status: models.ShiftChangeRequestStatusPending},
			},
		}}
		// - 未対応(pending)の依頼が残っていること自体は「未確定の変更」ではない。
		//   マネージャーがまだ手を付けていないだけなので、確定ボタンは出さない
		got, err := h.hasUnpublishedChanges(newTable(draft, &publishedAt))
		if err != nil {
			t.Fatalf("想定外のエラー: %v", err)
		}
		if got {
			t.Error("変更が無いのに未確定と判定された")
		}
	})

	t.Run("下書きが確定版と違えば未確定", func(t *testing.T) {
		h := &ShiftTableHandler{changeRequestRepo: &stubChangeRequestRepo{}}
		stale := []models.Shift{shift(staffA, "tuesday", "09:00", "15:30")}
		got, err := h.hasUnpublishedChanges(newTable(stale, &publishedAt))
		if err != nil {
			t.Fatalf("想定外のエラー: %v", err)
		}
		if !got {
			t.Error("シフト内容の差分が検出されていない")
		}
	})

	t.Run("シフトが同じでも applied な依頼があれば未確定", func(t *testing.T) {
		// - 出し直し(superseded)や陳腐化(stale)の依頼はシフト表を変えずに状態だけが変わる。
		//   ここを拾わないと確定ボタンが出ず、applied のまま永久に残ってしまう
		h := &ShiftTableHandler{changeRequestRepo: &stubChangeRequestRepo{
			requests: []models.ShiftChangeRequest{
				{Status: models.ShiftChangeRequestStatusApplied},
			},
		}}
		got, err := h.hasUnpublishedChanges(newTable(draft, &publishedAt))
		if err != nil {
			t.Fatalf("想定外のエラー: %v", err)
		}
		if !got {
			t.Error("applied な依頼が検出されていない")
		}
	})
}
