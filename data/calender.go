package data

import "yoyaku_mate_server/models"

// 全ての予約状況データ取得
func GetAllCalender() []models.CalenderItem {
	reservationInfoData := GetAllReservationInfo()

	var calender []models.CalenderItem
	for _, reservation := range reservationInfoData {
		calender = append(calender, models.CalenderItem{
			ReservationID: reservation.ReservationID,
			StoreName:     reservation.StoreName,
			Details:       reservation.Details,
			ReservedDate:  reservation.ReservedDate,
			ReservedTime:  reservation.ReservedTime,
		})
	}

	return calender
}
