package handlers

import (
	"testing"
	"time"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// - 下書き/確定版の分離は「スタッフに作りかけが見えない」ことが要件そのものなので、
//   未確定判定をテストで固定する (純粋な判定ロジックのテストは shift パッケージ側)

func newTestShift(staffID primitive.ObjectID, day, start, end string) models.Shift {
	return models.Shift{
		ID: primitive.NewObjectID(), StaffID: staffID,
		Day: day, StartTime: start, EndTime: end,
	}
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
	draft := []models.Shift{newTestShift(staffA, "monday", "09:00", "15:30")}

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
		stale := []models.Shift{newTestShift(staffA, "tuesday", "09:00", "15:30")}
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
