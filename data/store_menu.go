package data

import "yoyaku_mate_server/models"

// type StoreMenuItem struct {
// 	StoreID                 int     `json:"store_id"`
// 	StoreName          string  `json:"store_name"`
// 	MenuNumber         int     `json:"menu_number"`
// 	MenuName           string  `json:"menu_name"`
// 	MenuPrice          float64 `json:"menu_price"`
// 	MenuDescription    string  `json:"menu_description"`
// 	MenuImage          string  `json:"menu_image"`
// 	MenuActivationFlag bool    `json:"menu_activation_flag"`
// }

// 店メニューデータ目録
var storeMenuData = []models.StoreMenuItem{
	{StoreID: 1, StoreName: "日の丸美容室", MenuNumber: 1, MenuName: "カット", MenuPrice: 3000, MenuDescription: "カットです。", MenuImage: "https://example.com/image1.jpg", MenuActivationFlag: true},
	{StoreID: 2, StoreName: "日の丸美容室", MenuNumber: 2, MenuName: "染色", MenuPrice: 5000, MenuDescription: "染色です。", MenuImage: "https://example.com/image2.jpg", MenuActivationFlag: true},
}

// すべてのメニューデータ取得
func GetAllStoreMenu() []models.StoreMenuItem {
	return storeMenuData
}
