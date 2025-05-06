package data

import (
	"sort"
	"yoyaku_mate_server/models"
)

func GetAllFrequentPlaces() []models.FrequentPlaceItem {
	reservationInfoData := GetAllReservationInfo()
	storeVisitCount := make(map[int]*models.FrequentPlaceItem)

	for _, reservation := range reservationInfoData {
		if _, exist := storeVisitCount[reservation.StoreID]; !exist {
			storeVisitCount[reservation.StoreID] = &models.FrequentPlaceItem{
				StoreID:     reservation.StoreID,
				StoreName:   reservation.StoreName,
				LastVisited: reservation.ReservedDate,
				VisitCount:  0,
			}
		}
		// visit count, last visited データ更新
		storeVisitCount[reservation.StoreID].VisitCount++
		if reservation.ReservedDate > storeVisitCount[reservation.StoreID].LastVisited {
			storeVisitCount[reservation.StoreID].LastVisited = reservation.ReservedDate
		}
	}

	// map を slice に変換
	var frequentPlaces []models.FrequentPlaceItem
	for _, place := range storeVisitCount {
		frequentPlaces = append(frequentPlaces, *place)
	}

	// visit count の降順、visit date の降順でソート
	sort.Slice(frequentPlaces, func(i, j int) bool {
		if frequentPlaces[i].VisitCount == frequentPlaces[j].VisitCount {
			return frequentPlaces[i].LastVisited > frequentPlaces[j].LastVisited
		}
		return frequentPlaces[i].VisitCount > frequentPlaces[j].VisitCount
	})

	return frequentPlaces
}
