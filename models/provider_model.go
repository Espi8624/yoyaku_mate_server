package models

// 店情報データ構造体
type StoreInfoItem struct {
	StoreID              int    `json:"store_id"`
	StoreName            string `json:"store_name"`
	StoreAddress         string `json:"store_address"`
	StoreTelNumber       string `json:"store_tel_number"`
	StoreEmail           string `json:"store_email"`
	StoreOfficialWebSite string `json:"store_official_web_site"`
	StoreOpenTime        string `json:"store_open_time"`
	StoreCloseTime       string `json:"store_close_time"`
}

// 店メニューデータ構造体
type StoreMenuItem struct {
	StoreID            int     `json:"store_id"`
	StoreName          string  `json:"store_name"`
	MenuNumber         int     `json:"menu_number"`
	MenuName           string  `json:"menu_name"`
	MenuPrice          float64 `json:"menu_price"`
	MenuDescription    string  `json:"menu_description"`
	MenuImage          string  `json:"menu_image"`
	MenuActivationFlag bool    `json:"menu_activation_flag"`
}

// 店予約状況データ構造体
type StoreReservationItem struct {
	StoreID      int    `json:"store_id"`
	StoreName    string `json:"store_name"`
	ClientName   string `json:"client_name"`
	Details      string `json:"details"`
	ReservedDate string `json:"reserved_date"`
	ReservedTime string `json:"reserved_time"`
	TimeStamp    string `json:"time_stamp"`
}
