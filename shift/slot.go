// Package shift はシフト表のドメインロジック(検証・自動配置・修正依頼の適用判定)を扱う。
// HTTP や DB には依存しない純粋な計算だけを置き、ハンドラはここを呼び出して結果を永続化する
package shift

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// FairnessLookbackWeeks 自動配置の公平配分で過去実績を遡って参照する週数。
// これが無いと「同数なら常にID順」で毎回・毎週同じ人が優先されてしまうため、
// 直近の実績を加味することで長期的な偏りを抑える
const FairnessLookbackWeeks = 12

// validShiftDays シフトの曜日として許容される値 (スタッフの勤務可能日と同じキー体系)
var validShiftDays = map[string]bool{
	"monday":    true,
	"tuesday":   true,
	"wednesday": true,
	"thursday":  true,
	"friday":    true,
	"saturday":  true,
	"sunday":    true,
}

// timeFormatPattern "HH:MM" (00:00〜23:59) 形式の検証用正規表現
var timeFormatPattern = regexp.MustCompile(`^([01]\d|2[0-3]):([0-5]\d)$`)

// IsValidTime "HH:MM" (00:00〜23:59) 形式の時刻かどうか
func IsValidTime(t string) bool {
	return timeFormatPattern.MatchString(t)
}

// IsValidWeekStartDate 週開始日が "YYYY-MM-DD" 形式かつ実際の月曜日かどうかを検証
func IsValidWeekStartDate(weekStartDate string) bool {
	parsed, err := time.Parse("2006-01-02", weekStartDate)
	if err != nil {
		return false
	}
	return parsed.Weekday() == time.Monday
}

// IsValidSlot シフトの曜日・開始/終了時刻の妥当性を検証
func IsValidSlot(day, startTime, endTime string) bool {
	if !validShiftDays[day] {
		return false
	}
	if !timeFormatPattern.MatchString(startTime) || !timeFormatPattern.MatchString(endTime) {
		return false
	}
	return startTime < endTime
}

// ContentEqual 2つのシフト一覧が「中身として」同じかを判定する。
// _id は比較しない。削除して同じ内容を作り直すとIDだけが変わるが、シフト表としては
// 何も変わっていないため、それを未確定の変更として扱わないようにする
func ContentEqual(a, b []models.Shift) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(s models.Shift) string {
		return s.StaffID.Hex() + "|" + s.Day + "|" + s.StartTime + "|" + s.EndTime
	}
	keysA := make([]string, 0, len(a))
	for _, s := range a {
		keysA = append(keysA, key(s))
	}
	keysB := make([]string, 0, len(b))
	for _, s := range b {
		keysB = append(keysB, key(s))
	}
	sort.Strings(keysA)
	sort.Strings(keysB)
	for i := range keysA {
		if keysA[i] != keysB[i] {
			return false
		}
	}
	return true
}

// ParseTimeToMinutes "HH:MM" を 0〜1439 の分に変換
func ParseTimeToMinutes(t string) (int, bool) {
	parts := strings.Split(t, ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, errH := strconv.Atoi(parts[0])
	m, errM := strconv.Atoi(parts[1])
	if errH != nil || errM != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// formatMinutesToTime 分を "HH:MM" 形式に変換
func formatMinutesToTime(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// shiftOverlapsRange 既存シフトが指定の分単位の時間範囲と重なっているかどうか
func shiftOverlapsRange(shift models.Shift, startMin, endMin int) bool {
	sMin, ok1 := ParseTimeToMinutes(shift.StartTime)
	eMin, ok2 := ParseTimeToMinutes(shift.EndTime)
	if !ok1 || !ok2 {
		return false
	}
	return sMin < endMin && eMin > startMin
}

// shiftsOverlap 2つのシフトの [start_time, end_time) が重なるかどうか
func shiftsOverlap(a, b models.Shift) bool {
	aStart, ok1 := ParseTimeToMinutes(a.StartTime)
	aEnd, ok2 := ParseTimeToMinutes(a.EndTime)
	bStart, ok3 := ParseTimeToMinutes(b.StartTime)
	bEnd, ok4 := ParseTimeToMinutes(b.EndTime)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return false
	}
	return aStart < bEnd && bStart < aEnd
}

func containsObjectID(ids []primitive.ObjectID, target primitive.ObjectID) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

// timeRangesOverlap 2つの [start, end) 時刻区間("HH:MM")が重なるかどうか
func timeRangesOverlap(aStart, aEnd, bStart, bEnd string) bool {
	aStartMin, ok1 := ParseTimeToMinutes(aStart)
	aEndMin, ok2 := ParseTimeToMinutes(aEnd)
	bStartMin, ok3 := ParseTimeToMinutes(bStart)
	bEndMin, ok4 := ParseTimeToMinutes(bEnd)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return false
	}
	return aStartMin < bEndMin && bStartMin < aEndMin
}
