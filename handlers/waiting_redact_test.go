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
