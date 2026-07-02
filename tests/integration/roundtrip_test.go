package integration

import (
	"testing"
	"time"

	"github.com/protocgen/proto2type/testdata/golden/go/gen"
	"github.com/protocgen/proto2type/testdata/golden/go/pb"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestUserRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second).UTC() // Use UTC since proto timestamps normalize to UTC
	dur := 5 * time.Minute
	email := "test@example.com"
	phone := "555-1234"
	nickname := "testy"
	prevStatus := int32(2)
	thumb := []byte{0xDE, 0xAD}

	original := &gen.User{
		ID:             "user-1",
		Email:          "user@test.com",
		DisplayName:    "Test User",
		Active:         true,
		Age:            30,
		Roles:          []string{"admin", "editor"},
		Metadata:       map[string]string{"env": "prod"},
		Address:        &gen.Address{Street: "123 Main St", City: "Springfield", State: "IL", Zip: "62701", Country: "US"},
		CreatedAt:      now,
		SessionTimeout: dur,
		Phone:          &phone,
		Avatar:         []byte{0xCA, 0xFE},
		Nickname:       &nickname,
		Status:         1,
		ContactEmail:   &email,
		Tags: []*gen.Tag{
			{Key: "env", Value: "prod"},
			{Key: "team", Value: "core"},
		},
		PreviousStatus:  &prevStatus,
		UpdateMask:      []string{"email", "display_name"},
		ExtraMetadata:   map[string]any{"nested": map[string]any{"key": "value"}},
		Preferences:     []any{"dark_mode", float64(42)},
		AvatarThumbnail: &thumb,
		FieldMasks:      [][]string{{"a", "b"}, {"c"}},
		Structs:         []map[string]any{{"x": float64(1)}},
		Lists:           [][]any{{"a", float64(2)}},
		EventTimes:      map[string]time.Time{"login": now},
		Configs:         map[string]map[string]any{"main": {"theme": "dark"}},
	}

	// Convert to proto and back
	proto := original.ToProto()
	if proto == nil {
		t.Fatal("ToProto returned nil")
	}

	restored := &gen.User{}
	restored.FromProto(proto)

	if !original.Equal(restored) {
		t.Errorf("round-trip failed: original != restored\noriginal: %+v\nrestored: %+v", original, restored)
	}
}

func TestDocumentRoundTrip(t *testing.T) {
	// Create an anypb.Any for the Extension field
	ts := timestamppb.Now()
	anyVal, err := anypb.New(ts)
	if err != nil {
		t.Fatalf("failed to create anypb.Any: %v", err)
	}

	metadata, err := structpb.NewStruct(map[string]any{
		"nested": map[string]any{
			"key": "value",
		},
		"count": float64(10),
	})
	if err != nil {
		t.Fatalf("failed to create structpb.Struct: %v", err)
	}

	// Build the Document from a proto message so Extension is properly an *anypb.Any
	pbDoc := &pb.Document{
		Id: "doc-1",
		SettingsMap: map[string]*pb.Settings{
			"main": {Theme: pb.Settings_THEME_DARK, Locale: "en-US"},
		},
		CodeNames: map[int32]string{1: "alpha", 2: "beta"},
		Metadata:  metadata,
		Extension: anyVal,
		Archived:  wrapperspb.Bool(true),
		ViewCount: wrapperspb.Int64(42),
	}

	original := &gen.Document{}
	original.FromProto(pbDoc)

	// Round-trip: domain → proto → domain
	proto := original.ToProto()
	restored := &gen.Document{}
	restored.FromProto(proto)

	if !original.Equal(restored) {
		t.Errorf("Document round-trip failed: original != restored")
	}
}

func TestCloneIndependence(t *testing.T) {
	original := &gen.Document{
		ID: "doc-1",
		SettingsMap: map[string]*gen.Settings{
			"main": {Theme: 1, Locale: "en-US"},
		},
		Metadata: map[string]any{
			"nested": map[string]any{"key": "value"},
		},
	}

	cloned := original.Clone()

	// Mutate clone
	cloned.SettingsMap["main"].Theme = 2
	mustMap(t, cloned.Metadata["nested"])["key"] = "changed"

	// Verify original is unchanged
	if original.SettingsMap["main"].Theme != 1 {
		t.Error("Clone leaked: SettingsMap mutation affected original")
	}
	if mustMap(t, original.Metadata["nested"])["key"] != "value" {
		t.Error("Clone leaked: Metadata mutation affected original")
	}
}

func TestCloneIndependenceAny(t *testing.T) {
	// Verify that cloning a Document with an Any Extension produces an independent copy
	ts := timestamppb.Now()
	anyVal, err := anypb.New(ts)
	if err != nil {
		t.Fatalf("failed to create anypb.Any: %v", err)
	}

	original := &gen.Document{
		ID:        "doc-any",
		Extension: anyVal,
	}

	cloned := original.Clone()

	// Mutate the cloned Extension
	clonedAny, ok := cloned.Extension.(*anypb.Any)
	if !ok {
		t.Fatal("cloned Extension is not *anypb.Any")
	}
	clonedAny.TypeUrl = "mutated"

	// Verify original is unchanged
	originalAny, ok := original.Extension.(*anypb.Any)
	if !ok {
		t.Fatal("original Extension is not *anypb.Any")
	}
	if originalAny.GetTypeUrl() == "mutated" {
		t.Error("Clone leaked: Any Extension mutation affected original")
	}
}

func TestFromProtoReusedReceiver(t *testing.T) {
	email := "test@example.com"
	phone := "555-0100"

	u := &gen.User{ContactEmail: &email}

	// Verify email is set
	if u.ContactEmail == nil || *u.ContactEmail != email {
		t.Fatalf("ContactEmail not set: got %v", u.ContactEmail)
	}

	// Create proto with phone variant instead
	pbUser := &pb.User{
		ContactMethod: &pb.User_ContactPhone{ContactPhone: phone},
	}
	u.FromProto(pbUser)

	if u.ContactEmail != nil {
		t.Errorf("FromProto did not clear stale ContactEmail, got %v", *u.ContactEmail)
	}
	if u.ContactPhone == nil || *u.ContactPhone != phone {
		t.Errorf("FromProto did not set ContactPhone, got %v", u.ContactPhone)
	}
}

func TestUserCloneIndependence(t *testing.T) {
	original := &gen.User{
		ID:            "user-clone",
		ExtraMetadata: map[string]any{"nested": map[string]any{"k": "v"}},
		Preferences:   []any{"a", map[string]any{"deep": "val"}},
		Configs:       map[string]map[string]any{"cfg": {"inner": "orig"}},
		Structs:       []map[string]any{{"s": "t"}},
		Lists:         [][]any{{"x", float64(1)}},
	}

	cloned := original.Clone()

	// Mutate clone's nested structures
	mustMap(t, cloned.ExtraMetadata["nested"])["k"] = "changed"
	mustMap(t, cloned.Preferences[1])["deep"] = "changed"
	cloned.Configs["cfg"]["inner"] = "changed"
	cloned.Structs[0]["s"] = "changed"
	cloned.Lists[0][0] = "changed"

	// Verify original is unchanged
	if mustMap(t, original.ExtraMetadata["nested"])["k"] != "v" {
		t.Error("Clone leaked: ExtraMetadata mutation affected original")
	}
	if mustMap(t, original.Preferences[1])["deep"] != "val" {
		t.Error("Clone leaked: Preferences mutation affected original")
	}
	if original.Configs["cfg"]["inner"] != "orig" {
		t.Error("Clone leaked: Configs mutation affected original")
	}
	if original.Structs[0]["s"] != "t" {
		t.Error("Clone leaked: Structs mutation affected original")
	}
	if original.Lists[0][0] != "x" {
		t.Error("Clone leaked: Lists mutation affected original")
	}
}

func TestFromProtoReusedReceiverClearsFields(t *testing.T) {
	// Create an anypb.Any for the Extension field
	ts := timestamppb.Now()
	anyVal, err := anypb.New(ts)
	if err != nil {
		t.Fatalf("failed to create anypb.Any: %v", err)
	}

	// Populate a Document with everything
	doc := &gen.Document{
		ID: "doc-1",
		SettingsMap: map[string]*gen.Settings{
			"main": {Theme: 1, Locale: "en-US"},
		},
		Extension:    anyVal,
		Placeholders: []struct{}{{}, {}},
		Metadata:     map[string]any{"key": "val"},
		UpdateMask:   []string{"a", "b"},
	}

	// FromProto with empty proto — should clear all reference-type fields
	doc.FromProto(&pb.Document{Id: "doc-2"})

	if doc.SettingsMap != nil {
		t.Error("SettingsMap not cleared")
	}
	if doc.Extension != nil {
		t.Error("Extension not cleared")
	}
	if doc.Placeholders != nil {
		t.Error("Placeholders not cleared")
	}
	if doc.Metadata != nil {
		t.Error("Metadata not cleared")
	}
	if doc.UpdateMask != nil {
		t.Error("UpdateMask not cleared")
	}
	if doc.ID != "doc-2" {
		t.Errorf("ID not updated: got %q", doc.ID)
	}
}

func TestFieldMaskDocument(t *testing.T) {
	src := &gen.Document{
		ID:         "src",
		Metadata:   map[string]any{"k": "v"},
		UpdateMask: []string{"a", "b"},
	}
	dst := &gen.Document{ID: "dst"}

	gen.ApplyFieldMaskDocument(dst, src, []string{"metadata", "update_mask"})

	if dst.ID != "dst" {
		t.Error("FieldMask modified unmasked field")
	}
	if dst.Metadata == nil {
		t.Fatal("FieldMask didn't copy Metadata")
	}
	if dst.UpdateMask == nil {
		t.Fatal("FieldMask didn't copy UpdateMask")
	}
	// Verify deep copy — mutating src should not affect dst
	src.Metadata["k"] = "changed"
	if dst.Metadata["k"] != "v" {
		t.Error("FieldMask shallow-copied Metadata")
	}
	src.UpdateMask[0] = "changed"
	if dst.UpdateMask[0] != "a" {
		t.Error("FieldMask shallow-copied UpdateMask")
	}
}

func TestEqualEdgeCases(t *testing.T) {
	a := &gen.Document{ID: "1"}
	b := &gen.Document{ID: "1"}
	if !a.Equal(b) {
		t.Error("identical docs not equal")
	}

	// Empty map vs nil — both semantically empty but structurally different
	a.Metadata = map[string]any{}
	b.Metadata = nil
	// nil Metadata vs empty Metadata
	// They differ structurally, so Equal should reflect that
	if a.Equal(b) {
		t.Log("Equal treats nil and empty map as equal (acceptable)")
	}
	// But the inverse should be symmetric
	if a.Equal(b) != b.Equal(a) {
		t.Error("Equal is not symmetric for nil vs empty map")
	}
}

func mustMap(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", v)
	}
	return m
}
