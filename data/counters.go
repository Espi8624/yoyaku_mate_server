package data

import (
	"context"
	"fmt"
	"log"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const CollectionCounters = "counters"

// GetNextSequence generates the next sequence number atomically.
// It uses "lazy initialization" to handle cases where the counter document does not exist yet.
func GetNextSequence(storeID string, businessDate time.Time) (int, error) {
	collection := db.GetCollection(DatabaseName, CollectionCounters)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Generate dateStr for the ID from the businessDate
	dateStr := businessDate.Format("20060102")

	filter := bson.M{
		"_id": models.CounterID{
			StoreID: storeID,
			Date:    dateStr,
		},
	}

	update := bson.M{
		"$inc": bson.M{"seq": 1},
	}

	// Strategy: Try to update. If not found, Initialize.
	var updatedCounter models.Counter
	after := options.After
	opts := options.FindOneAndUpdate().SetReturnDocument(after)

	err := collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedCounter)
	if err == nil {
		// Successful increment
		return updatedCounter.Seq, nil
	}

	if err != mongo.ErrNoDocuments {
		// Unexpected error
		return 0, fmt.Errorf("failed to get next sequence: %w", err)
	}

	// Document not found. This means it's the first registration for this store on this date (or first after deployment).
	log.Printf("[GetNextSequence] Counter not found for %s %s. Initializing from existing data.", storeID, dateStr)

	// Get current max from waiting_list using the PRECISE businessDate
	currentMax, err := getMaxQueueNumberInternal(storeID, businessDate)
	if err != nil {
		return 0, fmt.Errorf("failed to get max queue number during initialization: %w", err)
	}

	newSeq := currentMax + 1

	// Insert the new counter
	newCounter := models.Counter{
		ID: models.CounterID{
			StoreID: storeID,
			Date:    dateStr,
		},
		Seq: newSeq,
	}

	_, err = collection.InsertOne(ctx, newCounter)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			// Race condition: Someone else inserted it. Retry increment.
			log.Printf("[GetNextSequence] Race detected during insert. Retrying increment.")
			return GetNextSequence(storeID, businessDate) // Recursive retry
		}
		return 0, fmt.Errorf("failed to insert new counter: %w", err)
	}

	log.Printf("[GetNextSequence] Initialized counter for %s %s to %d", storeID, dateStr, newSeq)
	return newSeq, nil
}

// getMaxQueueNumberInternal simulates the logic of `GetNextQueueNumber` but just returns the max.
// It accepts the exact businessDate (Cutoff time) to ensure we query the correct window.
func getMaxQueueNumberInternal(storeID string, businessDate time.Time) (int, error) {
	collection := db.GetCollection(DatabaseName, CollectionWaitingList)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Window: businessDate <= time < businessDate + 24h
	// We formatting it exactly as yoyaku_mate_server expects (RFC3339 with offset)
	// Note: businessDate passed here should already be in JST (or correct location)
	startStr := businessDate.Format("2006-01-02T15:04:05.000+09:00")

	opts := options.FindOne().SetSort(bson.D{{Key: "queue_number", Value: -1}})
	filter := bson.M{
		"store_id": storeID,
		"registration_time": bson.M{
			"$gte": startStr,
		},
	}

	var lastItem models.WaitingList
	err := collection.FindOne(ctx, filter, opts).Decode(&lastItem)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return 0, nil
		}
		return 0, err
	}

	return lastItem.QueueNumber, nil
}
