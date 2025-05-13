package data

import (
	"context"
	"log"
	"time"

	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
)

func GetReservationsInfoData(userID string) ([]models.NewReservationInfoItem, error) {
	collection := db.GetCollection("yoyaku_mate_db", "reservation_info")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var reservationsInfoData []models.NewReservationInfoItem
	filter := bson.M{"user_id": userID}

	// log.Printf("Querying store_info with filter: %v", filter)

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		log.Printf("Failed to fetch reservation info: %v", err)
		return nil, err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var reservationInfoItem models.NewReservationInfoItem
		if err := cursor.Decode(&reservationInfoItem); err != nil {
			log.Printf("Failed to decode reservation info item: %v", err)
			continue
		}
		reservationsInfoData = append(reservationsInfoData, reservationInfoItem)
	}

	if err := cursor.Err(); err != nil {
		log.Printf("Cursor error: %v", err)
		return nil, err
	}

	return reservationsInfoData, nil
}
