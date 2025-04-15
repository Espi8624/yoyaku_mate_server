package data

import "yoyaku_mate_server/models"

// type StoreReservationItem struct {
// 	StoreID           int    `json:"store_id"`
// 	StoreName    string `json:"store_name"`
// 	ClientName   string `json:"client_name"`
// 	Details      string `json:"details"`
// 	ReservedDate string `json:"reserved_date"`
// 	ReservedTime string `json:"reserved_time"`
// 	TimeStamp    string `json:"time_stamp"`
// }

// 店予約状況データ目録
var storeReservationsData = []models.StoreReservationItem{
	{StoreID: 1, StoreName: "provider1", ClientName: "山田", Details: "ランチ", ReservedDate: "2025-03-20", ReservedTime: "13:00", TimeStamp: "2025-03-20 19:00"},
	{StoreID: 2, StoreName: "provider1", ClientName: "佐藤", Details: "ディナー", ReservedDate: "2025-03-23", ReservedTime: "19:00", TimeStamp: "2025-03-23 17:00"},
	{StoreID: 3, StoreName: "provider1", ClientName: "鈴木", Details: "ランチ", ReservedDate: "2025-03-23", ReservedTime: "12:00", TimeStamp: "2025-03-23 21:00"},
	{StoreID: 4, StoreName: "provider1", ClientName: "佐藤", Details: "ランチ", ReservedDate: "2025-03-24", ReservedTime: "14:00", TimeStamp: "2025-03-24 20:00"},
	{StoreID: 5, StoreName: "provider1", ClientName: "鈴木", Details: "ディナー", ReservedDate: "2025-03-25", ReservedTime: "20:00", TimeStamp: "2025-03-25 18:00"},
	{StoreID: 6, StoreName: "provider1", ClientName: "山田", Details: "ディナー", ReservedDate: "2025-03-25", ReservedTime: "21:00", TimeStamp: "2025-03-25 12:00"},
}

// 全ての予約状況データ取得
func GetAllStoreReservationsData() []models.StoreReservationItem {
	return storeReservationsData
}
