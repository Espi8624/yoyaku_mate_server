package models

import "time"

type WaitingListItem struct {
	ID               string     `json:"id" bson:"_id"`
	StoreID          string     `json:"store_id" bson:"store_id"`
	WaitingID        string     `json:"waiting_id" bson:"waiting_id"`
	QueueNumber      int        `json:"queue_number" bson:"queue_number"`
	CustomerName     string     `json:"customer_name" bson:"customer_name"`
	PartySize        int        `json:"party_size" bson:"party_size"`
	RegistrationTime time.Time  `json:"registration_time" bson:"registration_time"`
	Contact          string     `json:"contact" bson:"contact"`
	Status           string     `json:"status" bson:"status"` // "waiting", "notified", "cancelled", "fulfilled"
	CalledTime       *time.Time `json:"called_time,omitempty" bson:"called_time,omitempty"`
	EntryTime        *time.Time `json:"entry_time,omitempty" bson:"entry_time,omitempty"`
	Notes            string     `json:"notes,omitempty" bson:"notes,omitempty"`
}
