package handlers

import (
	"testing"
	"yoyaku_mate_server/models"
)

func strPtr(s string) *string { return &s }

// TestRedactForPublic は匿名購読者へ個人情報が渡らないことを検証する。
//
// /waiting-list/stream は無認証の公開エンドポイントで、store_id さえ分かれば
// 誰でも購読できる。ここが緩むと他の客の連絡先や自由記述がそのまま配信される
func TestRedactForPublic(t *testing.T) {
	original := []models.WaitingList{
		{
			WaitingID:   "w-1",
			StoreID:     "store-1",
			QueueNumber: 3,
			PartySize:   2,
			Status:      "waiting",
			Contact:     strPtr("090-1234-5678"),
			Notes:       strPtr("アレルギーあり、窓際希望"),
			MenuItems:   []models.MenuItem{{MenuID: "m-1", Name: "ラーメン", Quantity: 2}},
		},
	}

	redacted := redactForPublic(original)

	if redacted[0].Contact != nil {
		t.Error("contact(電話番号)が残っている")
	}
	if redacted[0].Notes != nil {
		t.Error("notes(客の自由記述)が残っている")
	}
	if redacted[0].MenuItems != nil {
		t.Error("menu_items(注文内容)が残っている")
	}

	// - モニターボードが使う3フィールドは残っていなければ表示が壊れる
	if redacted[0].WaitingID != "w-1" || redacted[0].QueueNumber != 3 || redacted[0].Status != "waiting" {
		t.Errorf("ボード表示に必要なフィールドが失われている: %+v", redacted[0])
	}
}

// TestRedactForPublicDoesNotMutateOriginal は元スライスが変更されないことを検証する。
//
// 元を書き換えてしまうと、同じリストを使い回すスタッフ向けの配信からも
// 情報が消える (notifyStore は1回の取得結果を両方へ配る)
func TestRedactForPublicDoesNotMutateOriginal(t *testing.T) {
	original := []models.WaitingList{
		{
			WaitingID: "w-1",
			Contact:   strPtr("090-1234-5678"),
			Notes:     strPtr("窓際希望"),
			MenuItems: []models.MenuItem{{MenuID: "m-1", Name: "ラーメン", Quantity: 1}},
		},
	}

	_ = redactForPublic(original)

	if original[0].Contact == nil {
		t.Error("元スライスのcontactが消えている")
	}
	if original[0].Notes == nil {
		t.Error("元スライスのnotesが消えている")
	}
	if original[0].MenuItems == nil {
		t.Error("元スライスのmenu_itemsが消えている")
	}
}

// TestRedactItemForPublic は登録応答(1件)の伏せ字処理を検証する。
//
// 登録は冪等なので、既に存在する waiting_id を送ると「既存レコード」がそのまま返る。
// つまり waiting_id を推測して投げるだけで他の客の contact が読めてしまう経路があり、
// ゲストからの登録応答はここを通してから返す必要がある。
// 時刻ベースのIDは推測しやすく、旧形式は秒までしか無いため総当たりが現実的だった
func TestRedactItemForPublic(t *testing.T) {
	original := models.WaitingList{
		WaitingID:         "w-1",
		StoreID:           "store-1",
		QueueNumber:       7,
		PartySize:         2,
		Status:            "waiting",
		EstimatedWaitTime: 20,
		Contact:           strPtr("090-1234-5678"),
		Notes:             strPtr("アレルギーあり"),
		MenuItems:         []models.MenuItem{{MenuID: "m-1", Name: "ラーメン", Quantity: 2}},
	}

	redacted := redactItemForPublic(original)

	if redacted.Contact != nil {
		t.Error("contact(電話番号)が残っている")
	}
	if redacted.Notes != nil {
		t.Error("notes(客の自由記述)が残っている")
	}
	if redacted.MenuItems != nil {
		t.Error("menu_items(注文内容)が残っている")
	}

	// - 客が登録後に必要とするのはこの3つ。落とすと登録完了画面が壊れる
	if redacted.WaitingID != "w-1" || redacted.QueueNumber != 7 || redacted.EstimatedWaitTime != 20 {
		t.Errorf("登録応答に必要なフィールドが失われている: %+v", redacted)
	}

	// - 値渡しとはいえポインタ/スライスは共有されるため、元が壊れないことを確認する
	if original.Contact == nil || original.Notes == nil || original.MenuItems == nil {
		t.Error("元のアイテムが書き換えられている")
	}
}

// TestRedactFunctionsStayInSync は2つの伏せ字関数が同じフィールドを落とすことを検証する。
//
// 片方にだけ除去対象を足すと、そちらの経路だけ塞がって他方が穴として残る。
// スライス版が単体版を呼ぶ実装になっている前提を、実装が変わっても崩させないためのテスト
func TestRedactFunctionsStayInSync(t *testing.T) {
	item := models.WaitingList{
		WaitingID: "w-1",
		Contact:   strPtr("090-1234-5678"),
		Notes:     strPtr("窓際希望"),
		MenuItems: []models.MenuItem{{MenuID: "m-1", Name: "ラーメン", Quantity: 1}},
	}

	fromSlice := redactForPublic([]models.WaitingList{item})[0]
	fromItem := redactItemForPublic(item)

	if fromSlice.Contact != fromItem.Contact ||
		fromSlice.Notes != fromItem.Notes ||
		(fromSlice.MenuItems == nil) != (fromItem.MenuItems == nil) {
		t.Errorf("2つの伏せ字関数の結果が食い違っている:\n slice=%+v\n item =%+v", fromSlice, fromItem)
	}
}
