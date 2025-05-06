package data

import "yoyaku_mate_server/models"

var reservationInfoData = []models.ReservationInfoItem{
	{ReservationID: 1, UserName: "佐藤太郎", StoreID: 1, StoreName: "日の丸美容室", Details: "カット", ReservedDate: "2025-03-20", ReservedTime: "19:00", TimeStamp: "2025-03-20 19:00"},
	{ReservationID: 2, UserName: "佐藤太郎", StoreID: 2, StoreName: "川崎食堂", Details: "カット", ReservedDate: "2025-03-23", ReservedTime: "17:00", TimeStamp: "2025-03-23 17:00"},
	{ReservationID: 3, UserName: "佐藤太郎", StoreID: 2, StoreName: "川崎食堂", Details: "カット", ReservedDate: "2025-05-23", ReservedTime: "17:00", TimeStamp: "2025-03-23 17:00"},
	{ReservationID: 4, UserName: "佐藤太郎", StoreID: 4, StoreName: "品川食堂", Details: "カット", ReservedDate: "2025-03-23", ReservedTime: "21:00", TimeStamp: "2025-03-23 21:00"},
	{ReservationID: 5, UserName: "佐藤太郎", StoreID: 5, StoreName: "日本橋皮膚科", Details: "カット", ReservedDate: "2025-03-24", ReservedTime: "20:00", TimeStamp: "2025-03-24 20:00"},
}

// 全ての予約状況データ取得
func GetAllReservationInfo() []models.ReservationInfoItem {
	return reservationInfoData
}
