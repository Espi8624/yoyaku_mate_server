package data

import "yoyaku_mate_server/models"

// type StoreInfoItem struct {
// 	ID                   int    `json:"store_id"`
// 	StoreName            string `json:"store_name"`
// 	StoreAddress         string `json:"store_address"`
// 	StoreTelNumber       string `json:"store_tel_number"`
// 	StoreEmail           string `json:"store_email"`
// 	StoreOfficialWebSite string `json:"store_official_web_site"`
// 	StoreOpenTime        string `json:"store_open_time"`
// 	StoreCloseTime       string `json:"store_close_time"`
// }

// 타임라인 데이터 목록
var storeInfo = models.StoreInfoItem{
	ID:                   1,
	StoreName:            "Test Store",
	StoreAddress:         "Tokyo, Japan",
	StoreTelNumber:       "03-1234-5678",
	StoreEmail:           "test@example.com",
	StoreOfficialWebSite: "https://example.com",
	StoreOpenTime:        "09:00",
	StoreCloseTime:       "18:00",
}

// 모든 알림 반환
func GetStoreInfoData() models.StoreInfoItem {
	return storeInfo
}
