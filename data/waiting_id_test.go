package data

import (
	"strings"
	"testing"
)

// TestGenerateWaitingID はサーバー生成の待機IDが衝突しない形であることを検証する。
//
// (store_id, waiting_id) にユニークインデックスを張ったため、衝突はもはや
// 「静かな二重登録」ではなく「登録の失敗」として客に見える。
// 以前の実装はミリ秒までの時刻のみ (YYYYMMDD-HHMMSS-mmm) で、同一ミリ秒に
// 登録が重なれば衝突した
func TestGenerateWaitingID(t *testing.T) {
	t.Run("形式は YYYYMMDD-HHMMSS-xxxxxx", func(t *testing.T) {
		id, err := generateWaitingID()
		if err != nil {
			t.Fatalf("生成に失敗: %v", err)
		}

		// - 点主アプリが以前使っていた形式に揃えてある
		if len(id) != 22 {
			t.Errorf("長さが22でない: %q (%d文字)", id, len(id))
		}
		parts := strings.Split(id, "-")
		if len(parts) != 3 {
			t.Fatalf("区切りが3つでない: %q", id)
		}
		if len(parts[0]) != 8 || len(parts[1]) != 6 || len(parts[2]) != 6 {
			t.Errorf("各部の長さが 8-6-6 でない: %q", id)
		}

		// - handlers 側の長さ上限に収まっていること
		if len(id) > 64 {
			t.Errorf("maxWaitingIDLength(64)を超えている: %d文字", len(id))
		}
	})

	t.Run("同一ミリ秒でも衝突しない", func(t *testing.T) {
		// - ループで連続生成すれば秒どころかミリ秒も揃う。
		//   乱数接尾辞が無ければここで必ず重複が出る
		const n = 2000
		seen := make(map[string]bool, n)
		for i := 0; i < n; i++ {
			id, err := generateWaitingID()
			if err != nil {
				t.Fatalf("生成に失敗: %v", err)
			}
			if seen[id] {
				t.Fatalf("%d回目で重複が発生した: %q", i+1, id)
			}
			seen[id] = true
		}
	})
}
