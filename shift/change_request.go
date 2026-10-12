package shift

import (
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// 衝突1件に対してマネージャーが下す判断
const (
	ActionPrioritize     = "prioritize"      // 衝突(依頼同士)で、この依頼を優先する
	ActionSwap           = "swap"            // 衝突(定員超過)で、指定した相手と自分の枠を入れ替える
	ActionConfirmOverlap = "confirm_overlap" // 衝突(本人の時間重複)を承知の上で適用する
	ActionSkip           = "skip"            // この依頼は今回見送る(保留のまま残す)
)

// 衝突の種類
const (
	ConflictTypeRequestConflict = "request_conflict"      // 複数の依頼が同じ枠を希望
	ConflictTypeCapacity        = "capacity_conflict"     // 希望先の枠が既に定員一杯
	ConflictTypeSelfOverlap     = "self_overlap_conflict" // 依頼者本人の他のシフトと重複
)

// Resolution クライアントが衝突1件に対して下した判断
type Resolution struct {
	RequestID     string `json:"request_id"`
	Action        string `json:"action"`
	TargetStaffID string `json:"target_staff_id,omitempty"`
}

// ConflictCandidate 衝突ダイアログに並べる選択肢1件(名前ボタン)
type ConflictCandidate struct {
	StaffID   string `json:"staff_id"`
	StaffName string `json:"staff_name"`
	// RequestID 依頼同士の衝突(request_conflict)で、この候補自身の依頼IDを表す。
	// 定員超過(capacity)の候補(単なる既存シフトの担当者)には無い
	RequestID string `json:"request_id,omitempty"`
}

// Conflict マネージャーの判断待ちの衝突1件
type Conflict struct {
	Type        string              `json:"type"`
	RequestID   string              `json:"request_id"`
	StaffName   string              `json:"staff_name"`
	ToDay       string              `json:"to_day"`
	ToStartTime string              `json:"to_start_time"`
	ToEndTime   string              `json:"to_end_time"`
	Candidates  []ConflictCandidate `json:"candidates,omitempty"`
}

// ApplyResult ApplyChangeRequests の結果
type ApplyResult struct {
	// Shifts 依頼を反映した後のシフト一覧 (引数のスライスとは別のコピー)
	Shifts []models.Shift
	// Applied 今回シフトへ反映した依頼ID
	Applied map[string]bool
	// Superseded 本人が出し直したため不要になった古い依頼ID
	Superseded map[string]bool
	// Stale 依頼元のシフトが既に無く、適用しようがない依頼ID
	Stale map[string]bool
	// Conflict マネージャーの判断待ちで処理を止めた衝突 (nil なら最後まで処理済み)
	Conflict *Conflict
}

// ApplyChangeRequests 未処理(pending)の修正依頼を、pending の並び順(古い順を想定)に
// シフトへ反映していく。衝突(3種類)のうち resolutions に判断が無いものに当たった時点で
// 処理を止め、その1件を Conflict に入れて返す
// - DBには触れない純粋関数。反映結果の保存・依頼状態の更新は呼び出し側が行う
// - 衝突で止まった場合でも、それまでに反映できた分は Shifts / Applied に含まれる
func ApplyChangeRequests(shifts []models.Shift, pending []models.ShiftChangeRequest, resolutions []Resolution,
	settings *models.Settings, staffList []map[string]interface{}, managerID, managerName string) ApplyResult {
	resolutionByRequestID := make(map[string]Resolution, len(resolutions))
	for _, res := range resolutions {
		resolutionByRequestID[res.RequestID] = res
	}
	staffNames := resolveStaffNames(staffList, managerID, managerName)

	shifts = append([]models.Shift{}, shifts...)
	appliedRequestIDs := map[string]bool{}
	skippedThisRun := map[string]bool{}
	// - 本人が出し直した結果、古い方が不要になった依頼
	supersededRequestIDs := findSupersededRequests(pending)
	// - 依頼元のシフトが既に無く、適用しようがない依頼
	staleRequestIDs := map[string]bool{}
	var conflict *Conflict

requestLoop:
	for _, cr := range pending {
		reqIDHex := cr.ID.Hex()
		if skippedThisRun[reqIDHex] || appliedRequestIDs[reqIDHex] {
			continue
		}
		// - 本人の新しい依頼に置き換えられたものは処理せず、後でまとめて解決済みにする
		if supersededRequestIDs[reqIDHex] {
			continue
		}

		// 依頼者(user_info._id)に対応するシフト表上のStaffIDを解決する。
		// 両者はID体系が異なるため、そのまま突き合わせると常に不一致になる
		shiftStaffIDs := resolveShiftStaffIDs(cr.StaffID, staffList, managerID)

		// 依頼元のシフトがまだそのまま存在するか (スタッフ・曜日・時刻が完全一致するか)。
		// target_shift_id はあくまで参考情報のため一致判定には使わない
		originalIdx := findShiftIndex(shifts, shiftStaffIDs, cr.FromDay, cr.FromStartTime, cr.FromEndTime)
		if originalIdx == -1 {
			staleRequestIDs[reqIDHex] = true
			continue
		}

		resolution, hasResolution := resolutionByRequestID[reqIDHex]
		if hasResolution && resolution.Action == ActionSkip {
			continue
		}

		// --- 衝突①: 他のまだ未処理の依頼と希望先が重なっていないか ---
		competitors := findCompetingRequests(pending, cr, skippedThisRun, appliedRequestIDs)
		if len(competitors) > 0 {
			if !hasResolution || resolution.Action != ActionPrioritize {
				candidates := []ConflictCandidate{
					{StaffID: cr.StaffID.Hex(), StaffName: cr.StaffName, RequestID: reqIDHex},
				}
				for _, c := range competitors {
					candidates = append(candidates, ConflictCandidate{
						StaffID: c.StaffID.Hex(), StaffName: c.StaffName, RequestID: c.ID.Hex(),
					})
				}
				conflict = &Conflict{
					Type: ConflictTypeRequestConflict, RequestID: reqIDHex, StaffName: cr.StaffName,
					ToDay: cr.ToDay, ToStartTime: cr.ToStartTime, ToEndTime: cr.ToEndTime,
					Candidates: candidates,
				}
				break requestLoop
			}
			// 優先する人を決定 -> 選ばれなかった全員(自分自身も含めうる)は今回見送る
			for _, c := range append([]models.ShiftChangeRequest{cr}, competitors...) {
				if c.StaffID.Hex() != resolution.TargetStaffID {
					skippedThisRun[c.ID.Hex()] = true
				}
			}
			if cr.StaffID.Hex() != resolution.TargetStaffID {
				// この依頼は敗れた側 -> 次の依頼へ(勝者は別途この後の周回で処理される)
				continue
			}
		}

		// --- 衝突②: 希望先の枠が既に必要人数分埋まっていないか ---
		swapWithIdx := -1
		required := settings.RequiredStaffCount[cr.ToDay].Count
		if required > 0 {
			occupants := findOccupants(shifts, cr.ToDay, cr.ToStartTime, cr.ToEndTime, shiftStaffIDs)
			if len(occupants) >= required {
				if hasResolution && resolution.Action == ActionSwap {
					swapWithIdx = findShiftIndexByStaff(shifts, resolution.TargetStaffID, cr.ToDay, cr.ToStartTime, cr.ToEndTime)
					if swapWithIdx == -1 {
						// スワップ相手が(別の変更で)既にいなくなっている -> 保留のまま次へ
						continue
					}
				} else {
					candidates := make([]ConflictCandidate, 0, len(occupants))
					for _, occ := range occupants {
						candidates = append(candidates, ConflictCandidate{
							StaffID: occ.StaffID.Hex(), StaffName: staffNameOf(staffNames, occ.StaffID),
						})
					}
					conflict = &Conflict{
						Type: ConflictTypeCapacity, RequestID: reqIDHex, StaffName: cr.StaffName,
						ToDay: cr.ToDay, ToStartTime: cr.ToStartTime, ToEndTime: cr.ToEndTime,
						Candidates: candidates,
					}
					break requestLoop
				}
			}
		}

		// --- 衝突③: 依頼者本人の他のシフトと時間が重ならないか ---
		if hasOtherOverlappingShift(shifts, shiftStaffIDs, originalIdx, cr.ToDay, cr.ToStartTime, cr.ToEndTime) {
			if !hasResolution || resolution.Action != ActionConfirmOverlap {
				conflict = &Conflict{
					Type: ConflictTypeSelfOverlap, RequestID: reqIDHex, StaffName: cr.StaffName,
					ToDay: cr.ToDay, ToStartTime: cr.ToStartTime, ToEndTime: cr.ToEndTime,
				}
				break requestLoop
			}
		}

		// ここまで来たら適用確定
		shifts[originalIdx].Day = cr.ToDay
		shifts[originalIdx].StartTime = cr.ToStartTime
		shifts[originalIdx].EndTime = cr.ToEndTime
		if swapWithIdx != -1 {
			shifts[swapWithIdx].Day = cr.FromDay
			shifts[swapWithIdx].StartTime = cr.FromStartTime
			shifts[swapWithIdx].EndTime = cr.FromEndTime
		}
		appliedRequestIDs[reqIDHex] = true
	}

	return ApplyResult{
		Shifts:     shifts,
		Applied:    appliedRequestIDs,
		Superseded: supersededRequestIDs,
		Stale:      staleRequestIDs,
		Conflict:   conflict,
	}
}

// MaskAppliedAsPending applied な依頼を pending として見せるためのコピーを返す
// (呼び出し側のスライスは書き換えない)
func MaskAppliedAsPending(requests []models.ShiftChangeRequest) []models.ShiftChangeRequest {
	masked := make([]models.ShiftChangeRequest, len(requests))
	copy(masked, requests)
	for i := range masked {
		if masked[i].Status == models.ShiftChangeRequestStatusApplied {
			masked[i].Status = models.ShiftChangeRequestStatusPending
			masked[i].ResolvedAt = nil
		}
	}
	return masked
}

// findShiftIndex スタッフ・曜日・開始/終了時刻が完全一致するシフトを探す
// (修正依頼の元になったシフトが、まだそのまま存在するかの確認に使う)
func findShiftIndex(shifts []models.Shift, staffIDs []primitive.ObjectID, day, startTime, endTime string) int {
	for i, s := range shifts {
		if containsObjectID(staffIDs, s.StaffID) && s.Day == day && s.StartTime == startTime && s.EndTime == endTime {
			return i
		}
	}
	return -1
}

// resolveShiftStaffIDs 修正依頼の依頼者(user_info._id)に対応する、シフト表上のStaffID候補を返す
//
// シフトのStaffIDと修正依頼のStaffIDは別のID体系である点に注意:
//   - Shift.StaffID       : 通常は store_staff_info._id。ただしstore_staff_infoを持たない
//     マネージャーのシフトには user_info._id (= ManagerID) がそのまま入る
//   - ShiftChangeRequest.StaffID : 常に user_info._id (依頼作成時の認証ユーザー)
//
// 両体系がデータ上混在しうるため、単一の値ではなく候補集合として扱う
func resolveShiftStaffIDs(requesterID primitive.ObjectID, staffList []map[string]interface{}, managerID string) []primitive.ObjectID {
	ids := make([]primitive.ObjectID, 0, 2)

	for _, entry := range staffList {
		userID, ok := entry["user_id"].(primitive.ObjectID)
		if !ok || userID != requesterID {
			continue
		}
		if staffDocID, ok := entry["_id"].(primitive.ObjectID); ok {
			ids = append(ids, staffDocID)
		}
	}

	// - store_staff_info を持たないマネージャーのシフトは user_info._id で登録されているため、
	//   依頼者がマネージャー本人の場合はその値も候補に含める
	if managerID != "" && requesterID.Hex() == managerID {
		ids = append(ids, requesterID)
	}

	return ids
}

// findShiftIndexByStaff 指定スタッフの、指定曜日・時間帯に重なるシフトを探す
// (定員超過の衝突で、スワップ相手として指定された人の現在のシフトを特定するのに使う)
func findShiftIndexByStaff(shifts []models.Shift, staffIDHex, day, startTime, endTime string) int {
	startMin, ok1 := ParseTimeToMinutes(startTime)
	endMin, ok2 := ParseTimeToMinutes(endTime)
	if !ok1 || !ok2 {
		return -1
	}
	for i, s := range shifts {
		if s.Day != day || s.StaffID.Hex() != staffIDHex {
			continue
		}
		if shiftOverlapsRange(s, startMin, endMin) {
			return i
		}
	}
	return -1
}

// findOccupants 指定曜日・時間帯に重なる、excludeStaffID以外のシフトを全て返す
// (定員超過かどうかの判定、およびスワップ候補一覧の表示に使う)
func findOccupants(shifts []models.Shift, day, startTime, endTime string, excludeStaffIDs []primitive.ObjectID) []models.Shift {
	startMin, ok1 := ParseTimeToMinutes(startTime)
	endMin, ok2 := ParseTimeToMinutes(endTime)
	if !ok1 || !ok2 {
		return nil
	}
	var occupants []models.Shift
	for _, s := range shifts {
		if s.Day != day || containsObjectID(excludeStaffIDs, s.StaffID) {
			continue
		}
		if shiftOverlapsRange(s, startMin, endMin) {
			occupants = append(occupants, s)
		}
	}
	return occupants
}

// hasOtherOverlappingShift staffIDが、excludeIdx以外のシフトで指定曜日・時間帯と
// 重なるものを持っているかどうか(依頼者本人の二重予約チェックに使う)
func hasOtherOverlappingShift(shifts []models.Shift, staffIDs []primitive.ObjectID, excludeIdx int, day, startTime, endTime string) bool {
	startMin, ok1 := ParseTimeToMinutes(startTime)
	endMin, ok2 := ParseTimeToMinutes(endTime)
	if !ok1 || !ok2 {
		return false
	}
	for i, s := range shifts {
		if i == excludeIdx || !containsObjectID(staffIDs, s.StaffID) || s.Day != day {
			continue
		}
		if shiftOverlapsRange(s, startMin, endMin) {
			return true
		}
	}
	return false
}

// findCompetingRequests current と希望先(曜日・時間帯)が重なる、他のまだ未処理の
// pending依頼を全て探す(依頼同士の衝突判定に使う)
func findCompetingRequests(pending []models.ShiftChangeRequest, current models.ShiftChangeRequest,
	skippedThisRun, appliedThisRun map[string]bool) []models.ShiftChangeRequest {
	var competitors []models.ShiftChangeRequest
	for _, other := range pending {
		if other.ID == current.ID {
			continue
		}
		// - 同一人物の重複依頼は「誰を優先するか」の問題ではないため競合として扱わない。
		//   (新しい方だけを本人の意思とみなす処理が findSupersededRequests 側で行われる)
		if other.StaffID == current.StaffID {
			continue
		}
		otherIDHex := other.ID.Hex()
		if skippedThisRun[otherIDHex] || appliedThisRun[otherIDHex] {
			continue
		}
		if other.ToDay != current.ToDay {
			continue
		}
		if timeRangesOverlap(current.ToStartTime, current.ToEndTime, other.ToStartTime, other.ToEndTime) {
			competitors = append(competitors, other)
		}
	}
	return competitors
}

// findSupersededRequests 同一人物が同じ枠へ複数の依頼を出している場合に、
// 古い方の依頼IDを返す
//
// 「この日を変えたい」と出した後で「やっぱりこの日のを変えたい」と出し直す運用を想定しており、
// 後から出した依頼を本人の最終的な意思とみなす。1人が同じ枠へ2つのシフトを移すことは
// できないため、古い依頼はここで畳んでおかないとマネージャーに
// 「本人 vs 本人、どちらを優先するか」という無意味な選択を迫ることになる
func findSupersededRequests(pending []models.ShiftChangeRequest) map[string]bool {
	superseded := make(map[string]bool)
	for _, a := range pending {
		for _, b := range pending {
			if a.ID == b.ID || a.StaffID != b.StaffID {
				continue
			}
			if a.ToDay != b.ToDay {
				continue
			}
			if !timeRangesOverlap(a.ToStartTime, a.ToEndTime, b.ToStartTime, b.ToEndTime) {
				continue
			}
			// - bの方が新しければ、aは置き換えられたものとして扱う
			if b.CreatedAt.After(a.CreatedAt) {
				superseded[a.ID.Hex()] = true
			}
		}
	}
	return superseded
}

// resolveStaffNames store_staff_info一覧から、シフトのstaffId(store_staff_infoの_id)を
// 表示名に変換するマップを作る。マネージャーがstore_staff_infoを持たない(通常ケース)場合は
// managerID/managerNameをフォールバックとして補う(shift_table_view.dartのクライアント側
// ロジックと同じ考え方)
func resolveStaffNames(staffList []map[string]interface{}, managerID, managerName string) map[string]string {
	names := make(map[string]string, len(staffList)+1)
	for _, entry := range staffList {
		id, ok := entry["_id"].(primitive.ObjectID)
		if !ok {
			continue
		}
		name, _ := entry["user_name"].(string)
		if name == "" {
			name = "不明"
		}
		names[id.Hex()] = name
	}
	if managerID != "" {
		if _, exists := names[managerID]; !exists {
			if managerName != "" {
				names[managerID] = managerName
			} else {
				names[managerID] = "マネージャー"
			}
		}
	}
	return names
}

// staffNameOf names から staffID の表示名を引く(無ければ「不明」)
func staffNameOf(names map[string]string, staffID primitive.ObjectID) string {
	if name, ok := names[staffID.Hex()]; ok {
		return name
	}
	return "不明"
}
