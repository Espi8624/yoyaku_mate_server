package data

import "yoyaku_mate_server/models"

// type StoreInfoItem struct {
// 	StoreID                   int    `json:"store_id"`
// 	StoreName            string `json:"store_name"`
// 	StoreAddress         string `json:"store_address"`
// 	StoreTelNumber       string `json:"store_tel_number"`
// 	StoreEmail           string `json:"store_email"`
// 	StoreOfficialWebSite string `json:"store_official_web_site"`
// 	StoreOpenTime        string `json:"store_open_time"`
// 	StoreCloseTime       string `json:"store_close_time"`
// }

// 店情報データ目録
var storeInfoData = models.StoreInfoItem{
	StoreID:              1,
	StoreName:            "Test Store",
	StoreAddress:         "Tokyo, Japan",
	StoreTelNumber:       "03-1234-5678",
	StoreEmail:           "test@example.com",
	StoreOfficialWebSite: "https://example.com",
	StoreOpenTime:        "09:00",
	StoreCloseTime:       "18:00",
}

// 店情報データ取得
func GetStoreInfoData() models.StoreInfoItem {
	return storeInfoData
}
