package data

import (
	"sort"
	"yoyaku_mate_server/models"
)

// 全てのタイムラインデータ取得
func GetAllTimeLines() []models.TimeLineItem {
	reservationInfoData := GetAllReservationInfo()

	var timeLines []models.TimeLineItem
	for _, reservation := range reservationInfoData {
		timeLines = append(timeLines, models.TimeLineItem{
			ReservationID: reservation.ReservationID,
			StoreName:     reservation.StoreName,
			ReservedDate:  reservation.ReservedDate,
			ReservedTime:  reservation.ReservedTime,
		})
	}

	sort.Slice(timeLines, func(i, j int) bool {
		if timeLines[i].ReservedDate == timeLines[j].ReservedDate {
			return timeLines[i].ReservedTime < timeLines[j].ReservedTime
		}
		return timeLines[i].ReservedDate < timeLines[j].ReservedDate
	})

	return timeLines
}
