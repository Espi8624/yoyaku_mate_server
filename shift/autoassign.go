package shift

import (
	"sort"
	"strings"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// --- 自動配置アルゴリズム ---

// weekdayOrder / weekdayJapaneseLabels Weekday定数の順序と、休業日判定に使う日本語ラベルの対応
var weekdayOrder = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}
var weekdayJapaneseLabels = []string{"月", "火", "水", "木", "金", "土", "日"}

// weekdayJapaneseLabelOf 曜日キー(英語小文字)から休業日判定用の日本語ラベルを取得
func weekdayJapaneseLabelOf(day string) string {
	for i, d := range weekdayOrder {
		if d == day {
			return weekdayJapaneseLabels[i]
		}
	}
	return ""
}

// IsClosedDay 曜日が定休日(regular_weekly)に含まれているかどうか
func IsClosedDay(day string, closedDays models.ClosedDays) bool {
	label := weekdayJapaneseLabelOf(day)
	if label == "" {
		return false
	}
	for _, d := range closedDays.RegularWeekly {
		if d == label {
			return true
		}
	}
	return false
}

// dayStartMinutes 指定曜日の営業開始時刻(分)を算出する。24時間営業なら0時、
// 営業時間データが無い/不正な場合は 9:00 をデフォルトとする
// (自動配置で必要人員のシフト開始時刻の起点として使う)
func dayStartMinutes(day string, settings *models.Settings) int {
	if settings.Is24Hours {
		return 0
	}
	hours, exists := settings.OperatingHours[day]
	if !exists {
		return 9 * 60
	}
	startMin, ok := ParseTimeToMinutes(hours.Start)
	if !ok {
		return 9 * 60
	}
	return startMin
}

// maxValidShiftMinutes シフトの終了時刻として許容される最大値 (23:59)。
// timeFormatPattern が "24:00" を許容しないため、24時間営業/日またぎの
// 閉店時刻はこの値に丸める
const maxValidShiftMinutes = 23*60 + 59

// dayEndMinutes 指定曜日の営業終了時刻(分)を算出する。自動配置で1日の
// 営業時間をShiftCount個のブロックに均等分割するための終端として使う。
// 24時間営業なら23:59(24:00はシフト時刻として表現できないため)、
// 営業時間データが無い場合は dayStartMinutes の9:00デフォルトと対になる18:00とする。
// 閉店時刻が開始時刻以前(データ不正、または日をまたぐ営業時間)の場合は、
// シフトが日をまたぐことに未対応で均等分割の基準にできないため、
// 開始時刻そのものを返して呼び出し側に当該曜日の自動配置を諦めさせる
func dayEndMinutes(day string, settings *models.Settings, startMin int) int {
	if settings.Is24Hours {
		return maxValidShiftMinutes
	}
	hours, exists := settings.OperatingHours[day]
	if !exists {
		return 18 * 60
	}
	endMin, ok := ParseTimeToMinutes(hours.End)
	if !ok || endMin <= startMin {
		return startMin
	}
	return endMin
}

// AssignCandidate 自動配置の対象となる1人分の情報 (承認済みスタッフ、またはマネージャー)
type AssignCandidate struct {
	ID           primitive.ObjectID
	Availability map[string][]models.UnavailableRange // 曜日 -> 勤務不可時間帯のリスト
	// マネージャーは store_staff_info を持たず勤務可能時間を設定する画面自体が無いため、
	// 曜日・時間帯を問わず常に配置対象にする
	IsManager bool
}

// isAvailable 指定曜日の [startMin, endMin) 区間が、勤務不可時間帯と重ならないかどうか
func (c AssignCandidate) isAvailable(day string, startMin, endMin int) bool {
	if c.IsManager {
		return true
	}
	for _, r := range c.Availability[day] {
		if r.AllDay {
			return false
		}
		rStart, ok1 := ParseTimeToMinutes(r.StartTime)
		rEnd, ok2 := ParseTimeToMinutes(r.EndTime)
		if !ok1 || !ok2 {
			continue
		}
		if rStart < endMin && rEnd > startMin {
			return false
		}
	}
	return true
}

// parseAvailability GetStoreStaffByStoreID が返す集計結果内の availability フィールド
// (map[string]interface{} 経由でデコードされた BSON サブドキュメント) を安全に解析する。
// ネストしたサブドキュメント/配列は primitive.M/primitive.A ではなく素の
// map[string]interface{}/[]interface{} としてデコードされるため、両方を許容する
func parseAvailability(raw interface{}) map[string][]models.UnavailableRange {
	result := map[string][]models.UnavailableRange{}

	var availMap map[string]interface{}
	switch v := raw.(type) {
	case primitive.M:
		availMap = v
	case map[string]interface{}:
		availMap = v
	default:
		return result
	}

	for day, v := range availMap {
		var arr []interface{}
		switch a := v.(type) {
		case primitive.A:
			arr = a
		case []interface{}:
			arr = a
		default:
			continue
		}
		ranges := make([]models.UnavailableRange, 0, len(arr))
		for _, item := range arr {
			var rangeMap map[string]interface{}
			switch r := item.(type) {
			case primitive.M:
				rangeMap = r
			case map[string]interface{}:
				rangeMap = r
			default:
				continue
			}
			allDay, _ := rangeMap["all_day"].(bool)
			startTime, _ := rangeMap["start_time"].(string)
			endTime, _ := rangeMap["end_time"].(string)
			ranges = append(ranges, models.UnavailableRange{
				AllDay:    allDay,
				StartTime: startTime,
				EndTime:   endTime,
			})
		}
		result[day] = ranges
	}
	return result
}

// buildAutoAssignCandidates GetStoreStaffByStoreID の結果から、承認済み(APPROVED)スタッフを抽出する。
// 項目の user_id が managerID と一致する場合(マネージャーが過去にスタッフとして参加した際の
// store_staff_info が残っているケース)は、その項目の IsManager フラグを立てて
// 勤務可能時間のチェックを不要にする (別IDで新規候補を作らないため表示名/色が一致し続ける)
func buildAutoAssignCandidates(staffList []map[string]interface{}, managerID string) []AssignCandidate {
	candidates := make([]AssignCandidate, 0, len(staffList))
	for _, entry := range staffList {
		status, _ := entry["status"].(string)
		if status != models.StaffStatusApproved {
			continue
		}
		id, ok := entry["_id"].(primitive.ObjectID)
		if !ok {
			continue
		}
		// マネージャーがスタッフとして参加した後にマネージャーになった等の経緯で
		// store_staff_info が残っているケースでは、そのまま同じ _id/氏名を使い回して
		// マネージャー扱い(勤務可能時間のチェックを不要に)する。別IDで新規候補を
		// 作ってしまうと、同一人物のシフトなのに表示名/色が一致しなくなるため避ける
		isManager := false
		if userID, ok := entry["user_id"].(primitive.ObjectID); ok && userID.Hex() == managerID {
			isManager = true
		}
		candidates = append(candidates, AssignCandidate{
			ID:           id,
			Availability: parseAvailability(entry["availability"]),
			IsManager:    isManager,
		})
	}
	return candidates
}

// appendManagerCandidateIfMissing 店舗のマネージャーがまだ候補に含まれていない場合のみ追加する。
// 通常マネージャーは store_staff_info を持たず GetStoreStaffByStoreID に含まれないため、
// その場合の最終手段として使う (勤務可能時間帯の設定画面が無いため常に配置可能として扱う)
func appendManagerCandidateIfMissing(candidates []AssignCandidate, managerID string) []AssignCandidate {
	for _, c := range candidates {
		if c.IsManager {
			return candidates
		}
	}
	objID, err := primitive.ObjectIDFromHex(managerID)
	if err != nil {
		return candidates
	}
	return append(candidates, AssignCandidate{ID: objID, IsManager: true})
}

// removeManagerCandidates 設定でマネージャーがシフト自動配置から除外されている場合に、
// 候補一覧からマネージャー分(buildAutoAssignCandidatesがIsManagerを立てた既存項目も含む)を取り除く
func removeManagerCandidates(candidates []AssignCandidate) []AssignCandidate {
	filtered := make([]AssignCandidate, 0, len(candidates))
	for _, c := range candidates {
		if c.IsManager {
			continue
		}
		filtered = append(filtered, c)
	}
	return filtered
}

// AutoAssign 必要人員設定・営業時間・定休日・スタッフの勤務可能時間をもとに、
// baseShifts に不足分のシフトを自動的に追加した新しいシフト一覧を返す。
// 各曜日は営業開始〜終了時刻を (ShiftChangeCount+1)個の等しい長さのブロックに
// 均等分割し(端数は出ない)、各ブロックごとに同時に必要な人数(Count)を満たすよう配置される。
// baseShifts が空(nilまたは長さ0)の場合は最初から全て自動生成する。
// historicalCounts は直近数週間分の実績(スタッフごとの担当シフト数)。これを公平配分の
// 初期値として使うことで、その週だけで完結しない、複数週にまたがった公平配分になる
// (nilの場合は0から、つまり従来通りその週だけで公平配分する)。
// historicalPairCounts は同様に直近数週間分の「誰と誰が一緒に組んだか」の実績で、
// ペア反復ペナルティの初期値として使う(nilの場合は0から)。
//
// 各ブロックの人員選定は、単純に担当数が少ない順に選ぶのではなく、
// 必要人数分の組み合わせを全て作って点数を付け、最も点数が低い組み合わせを選ぶ
// 2段階方式(候補生成→スコアリング)にしている。点数が低いほど良い組み合わせ:
//
//	score = 担当数合計*10 + ペア反復合計*5 + 連続勤務件数*5
//
// これにより、個人ごとの公平配分(担当数)だけでなく、同じ2人ばかりが
// 繰り返し組まされること・同じ日に隙間なく連続して働かされることも抑えられる
func AutoAssign(baseShifts []models.Shift, settings *models.Settings, candidates []AssignCandidate, historicalCounts map[primitive.ObjectID]int, historicalPairCounts map[string]int) []models.Shift {
	result := make([]models.Shift, len(baseShifts))
	copy(result, baseShifts)

	// 公平な割り当てのため、直近実績(historicalCounts)を初期値に、
	// 現時点での担当シフト数をスタッフごとに集計する
	assignedCount := make(map[primitive.ObjectID]int, len(historicalCounts))
	for id, count := range historicalCounts {
		assignedCount[id] = count
	}
	for _, s := range result {
		assignedCount[s.StaffID]++
	}

	// ペア反復ペナルティのため、直近実績(historicalPairCounts)を初期値に、
	// baseShifts に既に含まれるペア(同じ曜日で時間帯が重なる2人)も反映しておく
	pairAssignedCount := make(map[string]int, len(historicalPairCounts))
	for key, count := range historicalPairCounts {
		pairAssignedCount[key] = count
	}
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i].Day != result[j].Day || result[i].StaffID == result[j].StaffID {
				continue
			}
			if shiftsOverlap(result[i], result[j]) {
				pairAssignedCount[PairKey(result[i].StaffID, result[j].StaffID)]++
			}
		}
	}

	for _, day := range weekdayOrder {
		if IsClosedDay(day, settings.ClosedDays) {
			continue
		}

		requirement := settings.RequiredStaffCount[day]
		required := requirement.Count
		// 交代がN回発生する場合、営業時間はN+1個のブロックに区切られる
		// (柵の杭と区間の関係と同じ。交代0回=1ブロックも有効な設定)
		blockCount := requirement.ShiftChangeCount + 1
		if required <= 0 || requirement.ShiftChangeCount < 0 {
			continue
		}

		// シフトの開始時刻は指定されないため、その曜日の営業時間全体を
		// blockCount 個のブロックに均等分割する(整数分割のため端数は出ない)
		dayStart := dayStartMinutes(day, settings)
		dayEnd := dayEndMinutes(day, settings, dayStart)
		if dayEnd <= dayStart {
			// 閉店時刻が不明/不正、または日をまたぐ営業時間(シフトの日またぎは未対応)の
			// 場合は均等分割の基準にできないため、当該曜日の自動配置はスキップする
			continue
		}
		totalMinutes := dayEnd - dayStart

		for i := 0; i < blockCount; i++ {
			// (dayStart起点で totalMinutes*i/blockCount ずつ進める形にすることで、
			// 端数が最後のブロックにまとまり、ブロック同士が隙間なく接続される)
			blockStart := dayStart + totalMinutes*i/blockCount
			blockEnd := dayStart + totalMinutes*(i+1)/blockCount
			if blockEnd <= blockStart {
				// blockCountが営業時間(分)より大きい極端な設定値に対する安全策
				continue
			}

			// 既にこの曜日・時間帯に重なっているスタッフを集計 (重複割り当て防止)
			alreadyAssigned := map[primitive.ObjectID]bool{}
			current := 0
			for _, s := range result {
				if s.Day != day {
					continue
				}
				if shiftOverlapsRange(s, blockStart, blockEnd) {
					alreadyAssigned[s.StaffID] = true
					current++
				}
			}

			need := required - current
			if need <= 0 {
				continue
			}

			// 対象曜日・時間帯に勤務可能(不可時間帯と重ならない)で、
			// まだ割り当てられていない候補を抽出
			eligible := make([]AssignCandidate, 0, len(candidates))
			for _, c := range candidates {
				if alreadyAssigned[c.ID] {
					continue
				}
				if c.isAvailable(day, blockStart, blockEnd) {
					eligible = append(eligible, c)
				}
			}

			if need > len(eligible) {
				// 勤務可能な候補が必要人数に満たない場合は、いる分だけ配置する
				need = len(eligible)
			}
			if need <= 0 {
				continue
			}

			startTime := formatMinutesToTime(blockStart)
			endTime := formatMinutesToTime(blockEnd)

			// 必要人数分の組み合わせを全て作り、点数が最も低い(=良い)組み合わせを選ぶ。
			// 組み合わせ数が極端に多くなる(スタッフが非常に多い等)場合の安全策として、
			// その場合だけ従来通り「担当数が少ない順」の単純な方式にフォールバックする
			var chosen []AssignCandidate
			if combinationCountExceeds(len(eligible), need, maxAutoAssignCombinations) {
				sorted := make([]AssignCandidate, len(eligible))
				copy(sorted, eligible)
				sort.SliceStable(sorted, func(i, j int) bool {
					ci, cj := sorted[i], sorted[j]
					if assignedCount[ci.ID] != assignedCount[cj.ID] {
						return assignedCount[ci.ID] < assignedCount[cj.ID]
					}
					return ci.ID.Hex() < cj.ID.Hex()
				})
				chosen = sorted[:need]
			} else {
				var bestScore int
				var bestKey string
				for idx, combo := range generateCombinations(eligible, need) {
					score := scoreCombination(combo, day, blockStart, blockEnd, assignedCount, pairAssignedCount, result)
					key := comboKey(combo)
					if idx == 0 || score < bestScore || (score == bestScore && key < bestKey) {
						chosen = combo
						bestScore = score
						bestKey = key
					}
				}
			}

			for i := 0; i < len(chosen); i++ {
				for j := i + 1; j < len(chosen); j++ {
					pairAssignedCount[PairKey(chosen[i].ID, chosen[j].ID)]++
				}
			}

			for _, candidate := range chosen {
				result = append(result, models.Shift{
					ID:        primitive.NewObjectID(),
					StaffID:   candidate.ID,
					Day:       day,
					StartTime: startTime,
					EndTime:   endTime,
				})
				assignedCount[candidate.ID]++
			}
		}
	}

	return result
}

// ComputeShortages AutoAssign が実際に配置した結果(shifts)を、必要人員設定と
// 突き合わせて、必要人数に届かなかったブロックだけを抽出する。
// AutoAssign 内の曜日→ブロック分割・現員数カウントと同じロジックを使うが、
// こちらは新規配置は行わず「今の状態がどれだけ足りないか」を読み取るだけの読み取り専用処理
func ComputeShortages(shifts []models.Shift, settings *models.Settings) []models.ShiftShortage {
	var shortages []models.ShiftShortage

	for _, day := range weekdayOrder {
		if IsClosedDay(day, settings.ClosedDays) {
			continue
		}

		requirement := settings.RequiredStaffCount[day]
		required := requirement.Count
		blockCount := requirement.ShiftChangeCount + 1
		if required <= 0 || requirement.ShiftChangeCount < 0 {
			continue
		}

		dayStart := dayStartMinutes(day, settings)
		dayEnd := dayEndMinutes(day, settings, dayStart)
		if dayEnd <= dayStart {
			continue
		}
		totalMinutes := dayEnd - dayStart

		for i := 0; i < blockCount; i++ {
			blockStart := dayStart + totalMinutes*i/blockCount
			blockEnd := dayStart + totalMinutes*(i+1)/blockCount
			if blockEnd <= blockStart {
				continue
			}

			// 同じ曜日・時間帯に重なっているスタッフの人数(重複割り当て防止のため人単位で数える)
			assigned := map[primitive.ObjectID]bool{}
			for _, s := range shifts {
				if s.Day != day {
					continue
				}
				if shiftOverlapsRange(s, blockStart, blockEnd) {
					assigned[s.StaffID] = true
				}
			}
			filled := len(assigned)

			if filled < required {
				shortages = append(shortages, models.ShiftShortage{
					Day:        day,
					ShiftIndex: i,
					StartTime:  formatMinutesToTime(blockStart),
					EndTime:    formatMinutesToTime(blockEnd),
					Required:   required,
					Filled:     filled,
					Shortage:   required - filled,
				})
			}
		}
	}

	return shortages
}

// maxAutoAssignCombinations 1ブロックあたりに全生成してよい組み合わせ数の上限。
// スタッフ数が非常に多い店舗での組み合わせ爆発を防ぐための安全策で、これを超える
// 場合だけ組み合わせ生成をやめ、以前と同じ「担当数が少ない順」の単純な方式にフォールバックする。
// 実際の店舗規模(スタッフ数十名程度まで)では通常発動しない
const maxAutoAssignCombinations = 20000

// combinationCountExceeds nCk(nからkを選ぶ組み合わせの数)が cap を超えるかどうかを、
// 大きな数への桁あふれを避けながら判定する(計算途中でcapを超えた時点で打ち切る)
func combinationCountExceeds(n, k, cap int) bool {
	if k <= 0 || k > n {
		return false
	}
	count := 1
	for i := 0; i < k; i++ {
		count = count * (n - i) / (i + 1)
		if count > cap {
			return true
		}
	}
	return false
}

// generateCombinations items から大きさ k の組み合わせを全て生成する(辞書順)
func generateCombinations(items []AssignCandidate, k int) [][]AssignCandidate {
	n := len(items)
	if k <= 0 || k > n {
		return nil
	}

	indices := make([]int, k)
	for i := range indices {
		indices[i] = i
	}

	var combos [][]AssignCandidate
	for {
		combo := make([]AssignCandidate, k)
		for i, idx := range indices {
			combo[i] = items[idx]
		}
		combos = append(combos, combo)

		// 次の組み合わせのインデックス列へ進める。末尾から、まだ動かせる
		// (n-k個の余地がある)桁を探して+1し、その右側を詰め直す
		i := k - 1
		for i >= 0 && indices[i] == i+n-k {
			i--
		}
		if i < 0 {
			break
		}
		indices[i]++
		for j := i + 1; j < k; j++ {
			indices[j] = indices[j-1] + 1
		}
	}
	return combos
}

// scoreCombination 組み合わせ1件分の点数を計算する。低いほど良い組み合わせ:
// 担当数合計(個人の公平配分) + ペア反復合計(同じ2人の組み合わせを避ける) +
// 連続勤務件数(同じ日に隙間なく連続して働く負担を避ける)の加重合計
func scoreCombination(combo []AssignCandidate, day string, blockStart, blockEnd int,
	assignedCount map[primitive.ObjectID]int, pairAssignedCount map[string]int, result []models.Shift) int {
	workloadImbalance := 0
	for _, c := range combo {
		workloadImbalance += assignedCount[c.ID]
	}

	pairRepeat := 0
	for i := 0; i < len(combo); i++ {
		for j := i + 1; j < len(combo); j++ {
			pairRepeat += pairAssignedCount[PairKey(combo[i].ID, combo[j].ID)]
		}
	}

	consecutiveBurden := 0
	for _, c := range combo {
		if hasAdjacentShift(result, c.ID, day, blockStart, blockEnd) {
			consecutiveBurden++
		}
	}

	return workloadImbalance*10 + pairRepeat*5 + consecutiveBurden*5
}

// hasAdjacentShift staffID が同じ曜日に、このブロックと隙間なく隣接するシフト
// (終了時刻がこのブロックの開始時刻と一致、または開始時刻がこのブロックの終了時刻と一致)
// を既に持っているかどうか。連続勤務の負担を表す簡易指標として使う
func hasAdjacentShift(shifts []models.Shift, staffID primitive.ObjectID, day string, blockStart, blockEnd int) bool {
	for _, s := range shifts {
		if s.StaffID != staffID || s.Day != day {
			continue
		}
		sMin, ok1 := ParseTimeToMinutes(s.StartTime)
		eMin, ok2 := ParseTimeToMinutes(s.EndTime)
		if !ok1 || !ok2 {
			continue
		}
		if eMin == blockStart || sMin == blockEnd {
			return true
		}
	}
	return false
}

// PairKey 2人分のIDから、順序に依存しない一意なマップキーを作る("aHex|bHex"、必ず
// 小さい方が先)。過去実績の集計(data/shift_table_repo.go)と自動配置実行中の累積値の
// 両方がこの関数でキーを作ることで、両者のキーが必ず一致する
func PairKey(a, b primitive.ObjectID) string {
	aHex, bHex := a.Hex(), b.Hex()
	if aHex > bHex {
		aHex, bHex = bHex, aHex
	}
	return aHex + "|" + bHex
}

// comboKey 組み合わせの同点タイブレーク用に、メンバーIDを昇順に並べて連結した
// 文字列を作る(実行のたびに同じ入力なら同じ結果になる決定論的な選択にするため)
func comboKey(combo []AssignCandidate) string {
	ids := make([]string, len(combo))
	for i, c := range combo {
		ids[i] = c.ID.Hex()
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// BuildCandidates GetStoreStaffByStoreID の結果から自動配置の候補一覧を作る
// - excludeManager (店舗設定でマネージャーを自動配置から除外) なら、マネージャー分を取り除く
// - そうでなければ、マネージャーが候補に含まれていない場合のみ追加する
func BuildCandidates(staffList []map[string]interface{}, managerID string, excludeManager bool) []AssignCandidate {
	candidates := buildAutoAssignCandidates(staffList, managerID)
	if excludeManager {
		return removeManagerCandidates(candidates)
	}
	return appendManagerCandidateIfMissing(candidates, managerID)
}
