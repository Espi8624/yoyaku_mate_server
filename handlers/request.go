package handlers

// valueOr ポインタが nil でなければその値を、nil なら既定値を返す
// - リクエストの任意項目(ポインタ型)に既定値を補うために使う
func valueOr[T any](ptr *T, defaultValue T) T {
	if ptr != nil {
		return *ptr
	}
	return defaultValue
}

// filterAllowedFields クライアントから受け取った汎用$set用updateマップから、
// 許可されたフィールド名(allowed)に含まれるキーだけを残した新しいマップを返す。
// 汎用PUTエンドポイント(map[string]interface{}をそのまま$setする実装)が
// role・store_id・firebase_uidなど本来クライアントが書き換えてはいけない
// フィールドまで無検証で受け入れてしまうマスアサインメント脆弱性への対策
func filterAllowedFields(update map[string]interface{}, allowed map[string]bool) map[string]interface{} {
	filtered := make(map[string]interface{}, len(update))
	for key, value := range update {
		if allowed[key] {
			filtered[key] = value
		}
	}
	return filtered
}
