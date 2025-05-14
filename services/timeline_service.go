package services

import (
	"log"
	"yoyaku_mate_server/data"
	"yoyaku_mate_server/models"
)

func GetUserTimeline(userID string) ([]models.TimelineItem, error) {
	// Get Reservations
	reservations, err := data.GetReservationsInfoData(userID)
	if err != nil {
		log.Printf("Failed to fetch reservations for user_id %s: %v", userID, err)
		return nil, err
	}

	// Make Timeline data
	var timeline []models.TimelineItem
	for _, reservation := range reservations {
		timeline = append(timeline, models.TimelineItem{
			ReservationID:      reservation.ReservationID,
			StoreName:          reservation.StoreName,
			ReservationDetails: reservation.ReservationDetails,
			ReservedDate:       reservation.ReservationDate,
			ReservedTime:       reservation.ReservationTime,
		})
	}
	return timeline, nil
}
