//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	gen "github.com/protocgen/proto2type/testdata/golden/go/gen"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestCrossBackendConsistency verifies that the same domain struct,
// written to both Firestore and MongoDB, produces identical domain structs when read back.
func TestCrossBackendConsistency(t *testing.T) {
	fsClient := firestoreClient(t)
	mgClient := mongoClient(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Millisecond)
	original := &gen.ModelCatalogEntry{
		ModelID:          "cross-test",
		Provider:         "test",
		DisplayName:      "Cross-Backend Test",
		InputPerMillion:  1.23,
		OutputPerMillion: 4.56,
		Enabled:          true,
		Category:         "test",
		ContextWindow:    4096,
		Aliases:          []string{"cross1", "cross2"},
		ProviderModelID:  "cross-test-v1",
		CreatedAt:        now,
		Notes:            "Cross-backend test",
		Region:           "us-central1",
	}

	// Write to Firestore
	var fs gen.ModelCatalogEntryFirestore
	fs.FromDomain(original)
	fsCol := fsClient.Collection("test_cross")
	_, err := fsCol.Doc("cross-test").Set(ctx, &fs)
	if err != nil {
		t.Fatalf("Firestore write failed: %v", err)
	}

	// Write to MongoDB
	var mg gen.ModelCatalogEntryMongo
	mg.FromDomain(original)
	mgCol := mgClient.Database("proto2type_test").Collection("cross")
	_, err = mgCol.InsertOne(ctx, &mg)
	if err != nil {
		t.Fatalf("MongoDB write failed: %v", err)
	}

	// Read from Firestore
	snap, err := fsCol.Doc("cross-test").Get(ctx)
	if err != nil {
		t.Fatalf("Firestore read failed: %v", err)
	}
	var fsRead gen.ModelCatalogEntryFirestore
	snap.DataTo(&fsRead)
	fsDomain := fsRead.ToDomain("cross-test")

	// Read from MongoDB
	var mgRead gen.ModelCatalogEntryMongo
	err = mgCol.FindOne(ctx, bson.M{"_id": original.ModelID}).Decode(&mgRead)
	if err != nil {
		t.Fatalf("MongoDB read failed: %v", err)
	}
	mgDomain := mgRead.ToDomain()

	// Compare: all non-ID fields should match between backends
	// (Firestore doesn't store ModelID, so skip that)
	assertEqual(t, "Provider", fsDomain.Provider, mgDomain.Provider)
	assertEqual(t, "DisplayName", fsDomain.DisplayName, mgDomain.DisplayName)
	assertEqualFloat(t, "InputPerMillion", fsDomain.InputPerMillion, mgDomain.InputPerMillion)
	assertEqualFloat(t, "OutputPerMillion", fsDomain.OutputPerMillion, mgDomain.OutputPerMillion)
	assertEqual(t, "Enabled", fsDomain.Enabled, mgDomain.Enabled)
	assertEqual(t, "Category", fsDomain.Category, mgDomain.Category)
	assertEqual(t, "ContextWindow", fsDomain.ContextWindow, mgDomain.ContextWindow)
	assertEqual(t, "ProviderModelID", fsDomain.ProviderModelID, mgDomain.ProviderModelID)
	assertEqual(t, "Notes", fsDomain.Notes, mgDomain.Notes)
	assertEqual(t, "Region", fsDomain.Region, mgDomain.Region)
	assertTimeEqual(t, "CreatedAt", fsDomain.CreatedAt, mgDomain.CreatedAt)

	// Aliases
	if len(fsDomain.Aliases) != len(mgDomain.Aliases) {
		t.Errorf("Aliases length mismatch: Firestore=%d, Mongo=%d",
			len(fsDomain.Aliases), len(mgDomain.Aliases))
	}

	// Clean up
	fsCol.Doc("test/cross-test").Delete(ctx)
	mgCol.Drop(ctx)
}
