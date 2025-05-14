package services

import (
	"log"
	"sort"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"
)

func GetFrequentStores(userID string) ([]models.FrequentStoreItem, error) {
	// 予約情報取得
	reservations, err := data.GetReservationsInfoData(userID)
	if err != nil {
		log.Printf("Failed to fetch reservations for user_id %s: %v", userID, err)
		return nil, err
	}

	// 店別訪問回数及び最終訪問日時集計
	storeVisitMap := make(map[string]*models.FrequentStoreItem)

	for _, reservation := range reservations {
		storeID := reservation.StoreID
		storeName := reservation.StoreName
		reservationDate := reservation.ReservationDate

		// 訪問記録がない場合初期化
		if _, exists := storeVisitMap[storeID]; !exists {
			storeVisitMap[storeID] = &models.FrequentStoreItem{
				StoreID:     storeID,
				StoreName:   storeName,
				LastVisited: reservationDate,
				VisitCount:  0,
			}
		}

		// 訪問回数増加
		storeVisitMap[storeID].VisitCount++

		// 最後訪問日時を更新
		if reservationDate > storeVisitMap[storeID].LastVisited {
			storeVisitMap[storeID].LastVisited = reservationDate
		}
	}

	// 結果をスライスに変換
	var frequentStores []models.FrequentStoreItem
	for _, storeItem := range storeVisitMap {
		frequentStores = append(frequentStores, *storeItem)
	}

	// 訪問回数を基準にソート
	sort.Slice(frequentStores, func(i, j int) bool {
		return frequentStores[i].VisitCount > frequentStores[j].VisitCount
	})

	return frequentStores, nil
}
