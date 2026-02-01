package models

// CounterID defines the composite key for the counter
type CounterID struct {
	StoreID string `bson:"store_id"`
	Date    string `bson:"date"` // YYYYMMDD format
}

// Counter represents a sequence number for a specific store and date
type Counter struct {
	ID  CounterID `bson:"_id"`
	Seq int       `bson:"seq"`
}
