package models

import (
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// waiting_list モデル
type WaitingList struct {
	ID               primitive.ObjectID `json:"id,omitempty" bson:"_id, omitempty"`
	StoreID          string             `json:"store_id" bson:"store_id"`
	WaitingID        string             `json:"waiting_id" bson:"waiting_id"`
	QueueNumber      int                `json:"queue_number" bson:"queue_number"`
	PartySize        int                `json:"party_size" bson:"party_size"`
	Nationality      string             `json:"nationality" bson:"nationality"`
	RegistrationTime string             `json:"registration_time" bson:"registration_time"`
	Contact          *string            `json:"contact" bson:"contact,omitempty"`
	Status           string             `json:"status" bson:"status"` // "waiting", "notified", "called", "cancelled", "completed", "no_show"
	CalledTime       *string            `json:"called_time,omitempty" bson:"called_time,omitempty"`
	EntryTime        *string            `json:"entry_time,omitempty" bson:"entry_time,omitempty"`
	Notes            *string            `json:"notes,omitempty" bson:"notes,omitempty"`
}

type AverageWaitingTimeResponse struct {
	AverageSeconds int    `json:"average_seconds"`
	AverageText    string `json:"average_text"`
}
