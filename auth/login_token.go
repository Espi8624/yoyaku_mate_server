package auth

import (
	"context"
	"fmt"
	"time"
	"yoyaku_mate_server/db"
	"yoyaku_mate_server/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// VerifyLoginToken checks if the provided login token matches the one in the database.
func VerifyLoginToken(userID primitive.ObjectID, token string) (bool, error) {
	collection := db.GetCollection("saboten_provider", "user_info") // Hardcoded DB name for now matching constants
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var user models.User
	err := collection.FindOne(ctx, bson.M{"_id": userID}).Decode(&user)
	if err != nil {
		return false, err
	}

	// If DB has no token, any token is invalid (or valid if we want to allow legacy sessions - but let's be strict)
	// If DB has token, provided token must match.
	if user.LoginToken == "" {
		// Migration case: if user has no token yet, allow it? Or force relogin.
		// For security, if we enabled this feature, we should force relogin.
		// Return false to force clean login.
		return false, fmt.Errorf("no active session found")
	}

	if user.LoginToken != token {
		return false, fmt.Errorf("session expired or logged in from another device")
	}

	// fmt.Printf("[VerifyLoginToken] Success for User %s. Token: '%s'\n", userID.Hex(), token)

	return true, nil
}
