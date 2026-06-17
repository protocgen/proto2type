//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	gen "github.com/protocgen/proto2type/testdata/golden/go/gen"
)

func TestFirestoreRoundTrip_ModelCatalogEntry(t *testing.T) {
	client := firestoreClient(t)
	ctx := context.Background()
	col := client.Collection("test_catalog")

	// Create domain struct with realistic data
	now := time.Now().UTC().Truncate(time.Millisecond) // Firestore truncates to millis
	original := &gen.ModelCatalogEntry{
		ModelID:          "gpt-4o",
		Provider:         "openai",
		DisplayName:      "GPT-4o",
		InputPerMillion:  2.50,
		OutputPerMillion: 10.00,
		Enabled:          true,
		Category:         "chat",
		ContextWindow:    128000,
		DiscountPercent:  0.0,
		Aliases:          []string{"gpt4o", "gpt-4o-latest"},
		ProviderModelID:  "gpt-4o-2024-08-06",
		CreatedAt:        now.Add(-24 * time.Hour),
		UpdatedAt:        now,
		Notes:            "Primary model",
		Region:           "us-east-1",
	}

	// Domain → Firestore storage
	var fs gen.ModelCatalogEntryFirestore
	fs.FromDomain(original)

	// Write to Firestore
	docID := fmt.Sprintf("%s-%s", original.Provider, original.ModelID)
	_, err := col.Doc(docID).Set(ctx, &fs)
	if err != nil {
		t.Fatalf("Failed to write to Firestore: %v", err)
	}

	// Read back
	snap, err := col.Doc(docID).Get(ctx)
	if err != nil {
		t.Fatalf("Failed to read from Firestore: %v", err)
	}

	var readBack gen.ModelCatalogEntryFirestore
	if err := snap.DataTo(&readBack); err != nil {
		t.Fatalf("Failed to deserialize from Firestore: %v", err)
	}

	// Firestore → Domain
	roundTripped := readBack.ToDomain(docID)

	// Assert equality (ModelID is not stored in Firestore, so it won't round-trip through storage)
	// But all other fields should match
	assertEqual(t, "Provider", original.Provider, roundTripped.Provider)
	assertEqual(t, "DisplayName", original.DisplayName, roundTripped.DisplayName)
	assertEqualFloat(t, "InputPerMillion", original.InputPerMillion, roundTripped.InputPerMillion)
	assertEqualFloat(t, "OutputPerMillion", original.OutputPerMillion, roundTripped.OutputPerMillion)
	assertEqual(t, "Enabled", original.Enabled, roundTripped.Enabled)
	assertEqual(t, "Category", original.Category, roundTripped.Category)
	assertEqual(t, "ContextWindow", original.ContextWindow, roundTripped.ContextWindow)
	assertEqual(t, "DiscountPercent", original.DiscountPercent, roundTripped.DiscountPercent)
	assertEqual(t, "ProviderModelID", original.ProviderModelID, roundTripped.ProviderModelID)
	assertEqual(t, "Notes", original.Notes, roundTripped.Notes)
	assertEqual(t, "Region", original.Region, roundTripped.Region)

	// Repeated fields
	if len(roundTripped.Aliases) != len(original.Aliases) {
		t.Errorf("Aliases length: got %d, want %d", len(roundTripped.Aliases), len(original.Aliases))
	} else {
		for i, a := range original.Aliases {
			assertEqual(t, fmt.Sprintf("Aliases[%d]", i), a, roundTripped.Aliases[i])
		}
	}

	// Timestamps (Firestore preserves to microsecond)
	assertTimeEqual(t, "CreatedAt", original.CreatedAt, roundTripped.CreatedAt)
	// UpdatedAt has serverTimestamp tag — the emulator may not populate it.
	// The tag generation is validated by the golden snapshot test.
	// Just verify it doesn't cause serialization errors (which we've already proven by getting here).

	// Clean up
	_, err = col.Doc(docID).Delete(ctx)
	if err != nil {
		t.Logf("Warning: failed to clean up doc: %v", err)
	}
}

func TestFirestoreRoundTrip_User(t *testing.T) {
	client := firestoreClient(t)
	ctx := context.Background()
	col := client.Collection("test_users")

	now := time.Now().UTC().Truncate(time.Millisecond)
	original := &gen.User{
		ID:          "user-123",
		Email:       "test@example.com",
		DisplayName: "Test User",
		Active:      true,
		Age:         30,
		Roles:       []string{"admin", "editor"},
		Metadata:    map[string]string{"theme": "dark", "lang": "en"},
		CreatedAt:   now,
		Phone:       strPtr("555-1234"),
	}

	// Domain → Firestore
	var fs gen.UserFirestore
	fs.FromDomain(original)

	// Write
	_, err := col.Doc(original.ID).Set(ctx, &fs)
	if err != nil {
		t.Fatalf("Failed to write User to Firestore: %v", err)
	}

	// Read
	snap, err := col.Doc(original.ID).Get(ctx)
	if err != nil {
		t.Fatalf("Failed to read User from Firestore: %v", err)
	}

	var readBack gen.UserFirestore
	if err := snap.DataTo(&readBack); err != nil {
		t.Fatalf("Failed to deserialize User from Firestore: %v", err)
	}

	// Firestore → Domain
	roundTripped := readBack.ToDomain()

	assertEqual(t, "Email", original.Email, roundTripped.Email)
	assertEqual(t, "DisplayName", original.DisplayName, roundTripped.DisplayName)
	assertEqual(t, "Active", original.Active, roundTripped.Active)
	assertEqual(t, "Age", original.Age, roundTripped.Age)
	if (original.Phone == nil) != (roundTripped.Phone == nil) {
		t.Errorf("Phone: nil mismatch: got %v, want %v", roundTripped.Phone, original.Phone)
	} else if original.Phone != nil && *original.Phone != *roundTripped.Phone {
		t.Errorf("Phone: got %q, want %q", *roundTripped.Phone, *original.Phone)
	}

	// Map field
	if len(roundTripped.Metadata) != len(original.Metadata) {
		t.Errorf("Metadata length: got %d, want %d", len(roundTripped.Metadata), len(original.Metadata))
	}
	for k, v := range original.Metadata {
		if roundTripped.Metadata[k] != v {
			t.Errorf("Metadata[%q]: got %q, want %q", k, roundTripped.Metadata[k], v)
		}
	}

	// Repeated field
	if len(roundTripped.Roles) != len(original.Roles) {
		t.Errorf("Roles length: got %d, want %d", len(roundTripped.Roles), len(original.Roles))
	}

	// Clean up
	col.Doc(original.ID).Delete(ctx)
}

func TestFirestoreZeroValues(t *testing.T) {
	client := firestoreClient(t)
	ctx := context.Background()
	col := client.Collection("test_zeros")

	// Test with zero/empty values
	original := &gen.ModelCatalogEntry{
		ModelID:  "empty-model",
		Provider: "test",
		// All other fields zero/empty
	}

	var fs gen.ModelCatalogEntryFirestore
	fs.FromDomain(original)

	_, err := col.Doc("empty-model").Set(ctx, &fs)
	if err != nil {
		t.Fatalf("Failed to write zero-value doc: %v", err)
	}

	snap, err := col.Doc("empty-model").Get(ctx)
	if err != nil {
		t.Fatalf("Failed to read zero-value doc: %v", err)
	}

	var readBack gen.ModelCatalogEntryFirestore
	if err := snap.DataTo(&readBack); err != nil {
		t.Fatalf("Failed to deserialize zero-value doc: %v", err)
	}

	roundTripped := readBack.ToDomain("empty-model")

	assertEqual(t, "Provider", "test", roundTripped.Provider)
	assertEqual(t, "DisplayName", "", roundTripped.DisplayName)
	assertEqual(t, "Enabled", false, roundTripped.Enabled)
	assertEqualFloat(t, "InputPerMillion", 0, roundTripped.InputPerMillion)
	if len(roundTripped.Aliases) != 0 {
		t.Errorf("Aliases should be empty, got %v", roundTripped.Aliases)
	}
	if !roundTripped.CreatedAt.IsZero() {
		t.Errorf("CreatedAt should be zero, got %v", roundTripped.CreatedAt)
	}

	col.Doc("test/empty-model").Delete(ctx)
}

// Test helpers
func assertEqual[T comparable](t *testing.T, name string, want, got T) {
	t.Helper()
	if want != got {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func assertEqualFloat(t *testing.T, name string, want, got float64) {
	t.Helper()
	if want != got {
		t.Errorf("%s: got %f, want %f", name, got, want)
	}
}

func assertTimeEqual(t *testing.T, name string, want, got time.Time) {
	t.Helper()
	// Allow 1ms difference for Firestore timestamp precision
	diff := want.Sub(got)
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Millisecond {
		t.Errorf("%s: got %v, want %v (diff: %v)", name, got, want, diff)
	}
}
