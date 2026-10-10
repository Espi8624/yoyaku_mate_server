package shift

import (
	"testing"
	"time"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// - 修正依頼の適用ロジックは、シフトと依頼で別のID体系を突き合わせる点と、
//   同一人物の出し直しを畳む点が壊れやすい。両方をテストで固定する

func staffEntry(staffDocID, userID primitive.ObjectID, name string) map[string]interface{} {
	return map[string]interface{}{
		"_id":       staffDocID,
		"user_id":   userID,
		"user_name": name,
	}
}

// TestResolveShiftStaffIDs 依頼者のuser_info._idから、シフト表上のStaffIDを解決できることを確認する
func TestResolveShiftStaffIDs(t *testing.T) {
	staffDocID := primitive.NewObjectID()
	userID := primitive.NewObjectID()
	managerUserID := primitive.NewObjectID()
	otherStaffDocID := primitive.NewObjectID()
	otherUserID := primitive.NewObjectID()

	staffList := []map[string]interface{}{
		staffEntry(staffDocID, userID, "スタッフA"),
		staffEntry(otherStaffDocID, otherUserID, "スタッフB"),
	}

	t.Run("スタッフはstore_staff_info._idに解決される", func(t *testing.T) {
		// - ここが解決できないと、シフトが存在していても「元のシフトが無い」と誤判定される
		ids := resolveShiftStaffIDs(userID, staffList, managerUserID.Hex())
		if !containsObjectID(ids, staffDocID) {
			t.Errorf("store_staff_info._id に解決されていない: %v", ids)
		}
		if containsObjectID(ids, otherStaffDocID) {
			t.Errorf("他人のIDが混ざっている: %v", ids)
		}
	})

	t.Run("store_staff_infoを持たないマネージャーは自身のuser_idに解決される", func(t *testing.T) {
		// - マネージャーのシフトには user_info._id がそのまま入るため
		ids := resolveShiftStaffIDs(managerUserID, staffList, managerUserID.Hex())
		if !containsObjectID(ids, managerUserID) {
			t.Errorf("マネージャー自身のIDに解決されていない: %v", ids)
		}
	})

	t.Run("店舗に属さないユーザーは解決されない", func(t *testing.T) {
		ids := resolveShiftStaffIDs(primitive.NewObjectID(), staffList, managerUserID.Hex())
		if len(ids) != 0 {
			t.Errorf("無関係なユーザーが解決されてしまった: %v", ids)
		}
	})
}

// TestFindShiftIndexUsesResolvedStaffID 解決済みIDでシフトを特定できることを確認する
func TestFindShiftIndexUsesResolvedStaffID(t *testing.T) {
	staffDocID := primitive.NewObjectID()
	userID := primitive.NewObjectID()
	staffList := []map[string]interface{}{staffEntry(staffDocID, userID, "スタッフA")}

	shifts := []models.Shift{
		{ID: primitive.NewObjectID(), StaffID: staffDocID, Day: "tuesday", StartTime: "09:00", EndTime: "15:30"},
	}

	// - 依頼側は user_info._id を持つ。これをそのまま突き合わせると一致しない
	if idx := findShiftIndex(shifts, []primitive.ObjectID{userID}, "tuesday", "09:00", "15:30"); idx != -1 {
		t.Fatal("前提が崩れている: user_info._id でシフトが一致してしまった")
	}

	ids := resolveShiftStaffIDs(userID, staffList, "")
	idx := findShiftIndex(shifts, ids, "tuesday", "09:00", "15:30")
	if idx != 0 {
		t.Errorf("解決済みIDでシフトを特定できなかった (idx=%d)", idx)
	}
}

// TestFindSupersededRequests 同一人物が同じ枠へ出し直した場合、古い方だけが畳まれることを確認する
func TestFindSupersededRequests(t *testing.T) {
	staffA := primitive.NewObjectID()
	staffB := primitive.NewObjectID()
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

	older := models.ShiftChangeRequest{
		ID: primitive.NewObjectID(), StaffID: staffA, CreatedAt: base,
		FromDay: "tuesday", FromStartTime: "09:00", FromEndTime: "15:30",
		ToDay: "wednesday", ToStartTime: "09:00", ToEndTime: "15:30",
	}
	newer := models.ShiftChangeRequest{
		ID: primitive.NewObjectID(), StaffID: staffA, CreatedAt: base.Add(time.Hour),
		FromDay: "friday", FromStartTime: "09:00", FromEndTime: "15:30",
		ToDay: "wednesday", ToStartTime: "09:00", ToEndTime: "15:30",
	}

	t.Run("同一人物の同じ枠への依頼は古い方が畳まれる", func(t *testing.T) {
		superseded := findSupersededRequests([]models.ShiftChangeRequest{older, newer})

		if !superseded[older.ID.Hex()] {
			t.Error("古い依頼が畳まれていない")
		}
		if superseded[newer.ID.Hex()] {
			t.Error("新しい依頼まで畳まれてしまった")
		}
	})

	t.Run("別人の依頼は畳まない", func(t *testing.T) {
		// - 別人同士は「どちらを優先するか」をマネージャーが判断すべき衝突として残す
		otherPerson := newer
		otherPerson.ID = primitive.NewObjectID()
		otherPerson.StaffID = staffB

		superseded := findSupersededRequests([]models.ShiftChangeRequest{older, otherPerson})
		if len(superseded) != 0 {
			t.Errorf("別人の依頼が畳まれてしまった: %v", superseded)
		}
	})

	t.Run("同一人物でも希望先が重ならなければ畳まない", func(t *testing.T) {
		// - 「火と金の両方を水の別々の直へ移したい」は矛盾しないため両方活かす
		differentSlot := newer
		differentSlot.ID = primitive.NewObjectID()
		differentSlot.ToStartTime = "15:30"
		differentSlot.ToEndTime = "22:00"

		superseded := findSupersededRequests([]models.ShiftChangeRequest{older, differentSlot})
		if len(superseded) != 0 {
			t.Errorf("重ならない依頼が畳まれてしまった: %v", superseded)
		}
	})
}

// TestFindCompetingRequestsIgnoresSameStaff 同一人物を競合相手として扱わないことを確認する
// - 扱ってしまうと「本人 vs 本人、どちらを優先するか」という選べないダイアログが出る
func TestFindCompetingRequestsIgnoresSameStaff(t *testing.T) {
	staffA := primitive.NewObjectID()
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

	current := models.ShiftChangeRequest{
		ID: primitive.NewObjectID(), StaffID: staffA, StaffName: "AIRES TEST", CreatedAt: base,
		ToDay: "wednesday", ToStartTime: "09:00", ToEndTime: "15:30",
	}
	sameStaff := models.ShiftChangeRequest{
		ID: primitive.NewObjectID(), StaffID: staffA, StaffName: "AIRES TEST", CreatedAt: base.Add(time.Hour),
		ToDay: "wednesday", ToStartTime: "09:00", ToEndTime: "15:30",
	}
	otherStaff := models.ShiftChangeRequest{
		ID: primitive.NewObjectID(), StaffID: primitive.NewObjectID(), StaffName: "他の人", CreatedAt: base,
		ToDay: "wednesday", ToStartTime: "09:00", ToEndTime: "15:30",
	}

	pending := []models.ShiftChangeRequest{current, sameStaff, otherStaff}
	competitors := findCompetingRequests(pending, current, map[string]bool{}, map[string]bool{})

	for _, c := range competitors {
		if c.StaffID == current.StaffID {
			t.Error("同一人物が競合相手に含まれている")
		}
	}
	if len(competitors) != 1 {
		t.Errorf("別人1件だけが競合になるはず (実際: %d件)", len(competitors))
	}
}

// TestFindOccupantsExcludesRequesterByResolvedID 定員判定で依頼者本人を除外できることを確認する
func TestFindOccupantsExcludesRequesterByResolvedID(t *testing.T) {
	staffDocID := primitive.NewObjectID()
	userID := primitive.NewObjectID()
	staffList := []map[string]interface{}{staffEntry(staffDocID, userID, "スタッフA")}

	shifts := []models.Shift{
		{ID: primitive.NewObjectID(), StaffID: staffDocID, Day: "wednesday", StartTime: "09:00", EndTime: "15:30"},
		{ID: primitive.NewObjectID(), StaffID: primitive.NewObjectID(), Day: "wednesday", StartTime: "09:00", EndTime: "15:30"},
	}

	ids := resolveShiftStaffIDs(userID, staffList, "")
	occupants := findOccupants(shifts, "wednesday", "09:00", "15:30", ids)

	if len(occupants) != 1 {
		t.Errorf("依頼者本人を除外できていない (occupants=%d)", len(occupants))
	}
	if len(occupants) == 1 && occupants[0].StaffID == staffDocID {
		t.Error("除外すべき本人が残っている")
	}
}

// TestMaskAppliedAsPending スタッフには applied を pending として見せることを確認する
func TestMaskAppliedAsPending(t *testing.T) {
	resolvedAt := time.Now()
	requests := []models.ShiftChangeRequest{
		{ID: primitive.NewObjectID(), Status: models.ShiftChangeRequestStatusPending},
		{ID: primitive.NewObjectID(), Status: models.ShiftChangeRequestStatusApplied, ResolvedAt: &resolvedAt},
		{ID: primitive.NewObjectID(), Status: models.ShiftChangeRequestStatusResolved, ResolvedAt: &resolvedAt},
	}

	masked := MaskAppliedAsPending(requests)

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

// TestApplyChangeRequests 修正依頼の一括適用で、通常適用と3種類の衝突が正しく判定されることを確認する
// - ハンドラから切り出した純粋関数なので、DBを介さずに判定の分岐をここで固定する
func TestApplyChangeRequests(t *testing.T) {
	staffADoc, staffAUser := primitive.NewObjectID(), primitive.NewObjectID()
	staffBDoc, staffBUser := primitive.NewObjectID(), primitive.NewObjectID()
	staffList := []map[string]interface{}{
		staffEntry(staffADoc, staffAUser, "スタッフA"),
		staffEntry(staffBDoc, staffBUser, "スタッフB"),
	}
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	noRequirement := &models.Settings{}

	// 火曜 09:00-15:30 → 水曜 09:00-15:30 への移動依頼
	moveRequest := func(staffUser primitive.ObjectID, name string, createdAt time.Time) models.ShiftChangeRequest {
		return models.ShiftChangeRequest{
			ID: primitive.NewObjectID(), StaffID: staffUser, StaffName: name, CreatedAt: createdAt,
			FromDay: "tuesday", FromStartTime: "09:00", FromEndTime: "15:30",
			ToDay: "wednesday", ToStartTime: "09:00", ToEndTime: "15:30",
		}
	}

	t.Run("衝突が無ければシフトが移動し、引数のスライスは書き換えない", func(t *testing.T) {
		shifts := []models.Shift{
			{ID: primitive.NewObjectID(), StaffID: staffADoc, Day: "tuesday", StartTime: "09:00", EndTime: "15:30"},
		}
		req := moveRequest(staffAUser, "スタッフA", base)

		got := ApplyChangeRequests(shifts, []models.ShiftChangeRequest{req}, nil, noRequirement, staffList, "", "")

		if got.Conflict != nil {
			t.Fatalf("想定外の衝突: %+v", got.Conflict)
		}
		if !got.Applied[req.ID.Hex()] {
			t.Error("依頼が適用済みになっていない")
		}
		if got.Shifts[0].Day != "wednesday" {
			t.Errorf("シフトが移動していない: %s", got.Shifts[0].Day)
		}
		if shifts[0].Day != "tuesday" {
			t.Error("引数のシフトが書き換えられている")
		}
	})

	t.Run("元のシフトが無い依頼はstaleとして返す", func(t *testing.T) {
		req := moveRequest(staffAUser, "スタッフA", base)

		got := ApplyChangeRequests(nil, []models.ShiftChangeRequest{req}, nil, noRequirement, staffList, "", "")

		if !got.Stale[req.ID.Hex()] || len(got.Applied) != 0 {
			t.Errorf("staleとして扱われていない: applied=%v stale=%v", got.Applied, got.Stale)
		}
	})

	t.Run("別人同士が同じ枠を希望すると判断待ちで止まる", func(t *testing.T) {
		shifts := []models.Shift{
			{ID: primitive.NewObjectID(), StaffID: staffADoc, Day: "tuesday", StartTime: "09:00", EndTime: "15:30"},
			{ID: primitive.NewObjectID(), StaffID: staffBDoc, Day: "tuesday", StartTime: "09:00", EndTime: "15:30"},
		}
		reqA := moveRequest(staffAUser, "スタッフA", base)
		reqB := moveRequest(staffBUser, "スタッフB", base.Add(time.Hour))
		pending := []models.ShiftChangeRequest{reqA, reqB}

		got := ApplyChangeRequests(shifts, pending, nil, noRequirement, staffList, "", "")
		if got.Conflict == nil || got.Conflict.Type != ConflictTypeRequestConflict {
			t.Fatalf("依頼同士の衝突になっていない: %+v", got.Conflict)
		}
		if len(got.Conflict.Candidates) != 2 || len(got.Applied) != 0 {
			t.Errorf("候補2名・適用0件のはず: candidates=%d applied=%d", len(got.Conflict.Candidates), len(got.Applied))
		}

		// - マネージャーがBを優先すると、Bだけが適用されAは今回見送られる
		resolutions := []Resolution{{RequestID: reqA.ID.Hex(), Action: ActionPrioritize, TargetStaffID: staffBUser.Hex()}}
		got = ApplyChangeRequests(shifts, pending, resolutions, noRequirement, staffList, "", "")
		if got.Conflict != nil {
			t.Fatalf("判断済みなのに止まった: %+v", got.Conflict)
		}
		if !got.Applied[reqB.ID.Hex()] || got.Applied[reqA.ID.Hex()] {
			t.Errorf("優先した方だけが適用されるはず: %v", got.Applied)
		}
	})

	t.Run("希望先が定員一杯なら判断待ちで止まり、swapで入れ替わる", func(t *testing.T) {
		shifts := []models.Shift{
			{ID: primitive.NewObjectID(), StaffID: staffADoc, Day: "tuesday", StartTime: "09:00", EndTime: "15:30"},
			{ID: primitive.NewObjectID(), StaffID: staffBDoc, Day: "wednesday", StartTime: "09:00", EndTime: "15:30"},
		}
		settings := &models.Settings{RequiredStaffCount: map[string]models.DayStaffRequirement{"wednesday": {Count: 1}}}
		req := moveRequest(staffAUser, "スタッフA", base)

		got := ApplyChangeRequests(shifts, []models.ShiftChangeRequest{req}, nil, settings, staffList, "", "")
		if got.Conflict == nil || got.Conflict.Type != ConflictTypeCapacity {
			t.Fatalf("定員超過の衝突になっていない: %+v", got.Conflict)
		}
		if len(got.Conflict.Candidates) != 1 || got.Conflict.Candidates[0].StaffName != "スタッフB" {
			t.Errorf("スワップ候補がスタッフBになっていない: %+v", got.Conflict.Candidates)
		}

		resolutions := []Resolution{{RequestID: req.ID.Hex(), Action: ActionSwap, TargetStaffID: staffBDoc.Hex()}}
		got = ApplyChangeRequests(shifts, []models.ShiftChangeRequest{req}, resolutions, settings, staffList, "", "")
		if got.Conflict != nil {
			t.Fatalf("判断済みなのに止まった: %+v", got.Conflict)
		}
		if got.Shifts[0].Day != "wednesday" || got.Shifts[1].Day != "tuesday" {
			t.Errorf("AとBの枠が入れ替わっていない: A=%s B=%s", got.Shifts[0].Day, got.Shifts[1].Day)
		}
	})

	t.Run("本人の別シフトと重なると判断待ちで止まり、それまでの適用分は残る", func(t *testing.T) {
		shifts := []models.Shift{
			{ID: primitive.NewObjectID(), StaffID: staffBDoc, Day: "monday", StartTime: "09:00", EndTime: "15:30"},
			{ID: primitive.NewObjectID(), StaffID: staffADoc, Day: "tuesday", StartTime: "09:00", EndTime: "15:30"},
			{ID: primitive.NewObjectID(), StaffID: staffADoc, Day: "wednesday", StartTime: "12:00", EndTime: "18:00"},
		}
		// - 先に処理されるBの依頼(月→木)は衝突しないので適用される
		reqB := models.ShiftChangeRequest{
			ID: primitive.NewObjectID(), StaffID: staffBUser, StaffName: "スタッフB", CreatedAt: base,
			FromDay: "monday", FromStartTime: "09:00", FromEndTime: "15:30",
			ToDay: "thursday", ToStartTime: "09:00", ToEndTime: "15:30",
		}
		reqA := moveRequest(staffAUser, "スタッフA", base.Add(time.Hour))

		got := ApplyChangeRequests(shifts, []models.ShiftChangeRequest{reqB, reqA}, nil, noRequirement, staffList, "", "")
		if got.Conflict == nil || got.Conflict.Type != ConflictTypeSelfOverlap {
			t.Fatalf("本人の時間重複の衝突になっていない: %+v", got.Conflict)
		}
		if !got.Applied[reqB.ID.Hex()] || got.Shifts[0].Day != "thursday" {
			t.Error("衝突より前に処理した依頼の適用結果が失われている")
		}
	})
}
