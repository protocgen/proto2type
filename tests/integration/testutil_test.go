//go:build integration

package integration

import (
	"context"
	"os"
	"testing"

	"cloud.google.com/go/firestore"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func firestoreClient(t *testing.T) *firestore.Client {
	t.Helper()
	// FIRESTORE_EMULATOR_HOST must be set
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set, skipping Firestore integration test")
	}
	ctx := context.Background()
	client, err := firestore.NewClient(ctx, "demo-test")
	if err != nil {
		t.Fatalf("Failed to create Firestore client: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func mongoClient(t *testing.T) *mongo.Client {
	t.Helper()
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		t.Skip("MONGO_URI not set, skipping MongoDB integration test")
	}
	ctx := context.Background()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	// Verify connection
	if err := client.Ping(ctx, nil); err != nil {
		t.Fatalf("Failed to ping MongoDB: %v", err)
	}
	t.Cleanup(func() { client.Disconnect(ctx) })
	return client
}

func strPtr(s string) *string {
	return &s
}
