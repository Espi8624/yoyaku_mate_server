package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ReservationInfoItem represents a reservation record
type NewReservationInfoItem struct {
	ID                 primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ReservationID      string             `bson:"reservation_id" json:"reservation_id"`
	ReservationStatus  string             `bson:"reservation_status" json:"reservation_status"`
	StoreID            string             `bson:"store_id" json:"store_id"`
	StoreName          string             `bson:"store_name" json:"store_name"`
	ReservationDate    string             `bson:"reservation_date" json:"reservation_date"`
	ReservationTime    string             `bson:"reservation_time" json:"reservation_time"`
	UserID             string             `bson:"user_id" json:"user_id"`
	UserName           string             `bson:"user_name" json:"user_name"`
	UserPhoneNumber    string             `bson:"user_phone_number" json:"user_phone_number"`
	UserEmail          string             `bson:"user_email" json:"user_email"`
	NumberOfPeople     int                `bson:"number_of_people" json:"number_of_people"`
	ReservationDetails string             `bson:"reservation_details" json:"reservation_details"`
	AdditionalNotes    string             `bson:"additional_notes" json:"additional_notes"`
	Timestamp          time.Time          `bson:"timestamp" json:"timestamp"`
}
