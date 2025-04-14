package models

// 가게 데이터 구조체
type StoreInfoItem struct {
	ID                   int    `json:"store_id"`
	StoreName            string `json:"store_name"`
	StoreAddress         string `json:"store_address"`
	StoreTelNumber       string `json:"store_tel_number"`
	StoreEmail           string `json:"store_email"`
	StoreOfficialWebSite string `json:"store_official_web_site"`
	StoreOpenTime        string `json:"store_open_time"`
	StoreCloseTime       string `json:"store_close_time"`
}
