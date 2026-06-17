//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	gen "github.com/protocgen/proto2type/testdata/golden/go"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoRoundTrip_ModelCatalogEntry(t *testing.T) {
	client := mongoClient(t)
	ctx := context.Background()
	col := client.Database("proto2type_test").Collection("catalog")

	now := time.Now().UTC().Truncate(time.Millisecond) // Mongo truncates to millis
	original := &gen.ModelCatalogEntry{
		ModelID:          "claude-4-sonnet",
		Provider:         "anthropic",
		DisplayName:      "Claude 4 Sonnet",
		InputPerMillion:  3.00,
		OutputPerMillion: 15.00,
		Enabled:          true,
		Category:         "chat",
		ContextWindow:    200000,
		Aliases:          []string{"claude4s", "sonnet4"},
		ProviderModelID:  "claude-4-sonnet-20250601",
		CreatedAt:        now.Add(-48 * time.Hour),
		UpdatedAt:        now,
		Notes:            "Newest model",
		Region:           "us-west-2",
	}

	// Domain → Mongo storage
	var ms gen.ModelCatalogEntryMongo
	ms.FromDomain(original)

	// Write to MongoDB
	_, err := col.InsertOne(ctx, &ms)
	if err != nil {
		t.Fatalf("Failed to insert into MongoDB: %v", err)
	}

	// Read back by _id (model_id is the document ID for Mongo)
	var readBack gen.ModelCatalogEntryMongo
	err = col.FindOne(ctx, bson.M{"_id": original.ModelID}).Decode(&readBack)
	if err != nil {
		t.Fatalf("Failed to read from MongoDB: %v", err)
	}

	// Mongo → Domain
	roundTripped := readBack.ToDomain()

	// ModelID should round-trip (Mongo stores _id in struct)
	assertEqual(t, "ModelID", original.ModelID, roundTripped.ModelID)
	assertEqual(t, "Provider", original.Provider, roundTripped.Provider)
	assertEqual(t, "DisplayName", original.DisplayName, roundTripped.DisplayName)
	assertEqualFloat(t, "InputPerMillion", original.InputPerMillion, roundTripped.InputPerMillion)
	assertEqual(t, "Enabled", original.Enabled, roundTripped.Enabled)
	assertEqual(t, "Category", original.Category, roundTripped.Category)
	assertEqual(t, "ContextWindow", original.ContextWindow, roundTripped.ContextWindow)
	assertEqual(t, "ProviderModelID", original.ProviderModelID, roundTripped.ProviderModelID)
	assertEqual(t, "Notes", original.Notes, roundTripped.Notes)
	assertEqual(t, "Region", original.Region, roundTripped.Region)

	// Repeated
	if len(roundTripped.Aliases) != len(original.Aliases) {
		t.Errorf("Aliases length: got %d, want %d", len(roundTripped.Aliases), len(original.Aliases))
	}

	// Timestamps
	assertTimeEqual(t, "CreatedAt", original.CreatedAt, roundTripped.CreatedAt)
	assertTimeEqual(t, "UpdatedAt", original.UpdatedAt, roundTripped.UpdatedAt)

	// Clean up
	col.Drop(ctx)
}

func TestMongoRoundTrip_User(t *testing.T) {
	client := mongoClient(t)
	ctx := context.Background()
	col := client.Database("proto2type_test").Collection("users")

	now := time.Now().UTC().Truncate(time.Millisecond)
	original := &gen.User{
		ID:          "user-456",
		Email:       "mongo@example.com",
		DisplayName: "Mongo User",
		Active:      true,
		Age:         25,
		Roles:       []string{"viewer"},
		Metadata:    map[string]string{"source": "test"},
		CreatedAt:   now,
		Phone:       "555-9876",
	}

	var ms gen.UserMongo
	ms.FromDomain(original)

	_, err := col.InsertOne(ctx, &ms)
	if err != nil {
		t.Fatalf("Failed to insert User into MongoDB: %v", err)
	}

	var readBack gen.UserMongo
	err = col.FindOne(ctx, bson.M{"id": original.ID}).Decode(&readBack)
	if err != nil {
		t.Fatalf("Failed to read User from MongoDB: %v", err)
	}

	roundTripped := readBack.ToDomain()

	assertEqual(t, "ID", original.ID, roundTripped.ID)
	assertEqual(t, "Email", original.Email, roundTripped.Email)
	assertEqual(t, "DisplayName", original.DisplayName, roundTripped.DisplayName)
	assertEqual(t, "Active", original.Active, roundTripped.Active)
	assertEqual(t, "Age", original.Age, roundTripped.Age)
	assertEqual(t, "Phone", original.Phone, roundTripped.Phone)

	if len(roundTripped.Metadata) != len(original.Metadata) {
		t.Errorf("Metadata length: got %d, want %d", len(roundTripped.Metadata), len(original.Metadata))
	}

	col.Drop(ctx)
}

func TestMongoZeroValues(t *testing.T) {
	client := mongoClient(t)
	ctx := context.Background()
	col := client.Database("proto2type_test").Collection("zeros")

	original := &gen.ModelCatalogEntry{
		ModelID:  "zero-model",
		Provider: "test",
	}

	var ms gen.ModelCatalogEntryMongo
	ms.FromDomain(original)

	_, err := col.InsertOne(ctx, &ms)
	if err != nil {
		t.Fatalf("Failed to insert zero-value doc: %v", err)
	}

	var readBack gen.ModelCatalogEntryMongo
	err = col.FindOne(ctx, bson.M{"_id": "zero-model"}).Decode(&readBack)
	if err != nil {
		t.Fatalf("Failed to read zero-value doc: %v", err)
	}

	roundTripped := readBack.ToDomain()

	assertEqual(t, "ModelID", "zero-model", roundTripped.ModelID)
	assertEqual(t, "Provider", "test", roundTripped.Provider)
	assertEqual(t, "Enabled", false, roundTripped.Enabled)
	assertEqualFloat(t, "InputPerMillion", 0, roundTripped.InputPerMillion)

	col.Drop(ctx)
}
