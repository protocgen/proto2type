package integration

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/protocgen/proto2type/testdata/golden/go/gen"
	"pgregory.net/rapid"
)

// ---------- rapid generators ----------

func rapidAddress(t *rapid.T) *gen.Address {
	return &gen.Address{
		Street:  rapid.StringN(0, 50, -1).Draw(t, "street"),
		City:    rapid.StringN(0, 30, -1).Draw(t, "city"),
		State:   rapid.StringN(0, 10, -1).Draw(t, "state"),
		Zip:     rapid.StringN(0, 10, -1).Draw(t, "zip"),
		Country: rapid.StringN(0, 20, -1).Draw(t, "country"),
	}
}

func rapidTag(t *rapid.T) *gen.Tag {
	return &gen.Tag{
		Key:   rapid.StringN(0, 20, -1).Draw(t, "key"),
		Value: rapid.StringN(0, 50, -1).Draw(t, "value"),
	}
}

func rapidUser(t *rapid.T) *gen.User {
	u := gen.User{
		ID:             rapid.StringN(0, 30, -1).Draw(t, "id"),
		Email:          rapid.StringN(0, 50, -1).Draw(t, "email"),
		DisplayName:    rapid.StringN(0, 50, -1).Draw(t, "display_name"),
		Active:         rapid.Bool().Draw(t, "active"),
		Age:            rapid.Int32Range(0, 150).Draw(t, "age"),
		Status:         rapid.Int32Range(0, 5).Draw(t, "status"),
		CreatedAt:      time.Unix(rapid.Int64Range(0, 2000000000).Draw(t, "created_at"), 0).UTC(),
		SessionTimeout: time.Duration(rapid.Int64Range(0, 3600).Draw(t, "timeout")) * time.Second,
		OldField:       rapid.StringN(0, 20, -1).Draw(t, "old_field"),
		OptionalName:   rapid.StringN(0, 20, -1).Draw(t, "optional_name"),
		BigNumber:      rapid.Int64Range(-1000000, 1000000).Draw(t, "big_number"),
		Handle:         rapid.StringN(0, 20, -1).Draw(t, "handle"),
	}

	// Repeated string
	u.Roles = rapid.SliceOfN(rapid.StringN(0, 20, -1), 0, 5).Draw(t, "roles")

	// Map field
	if rapid.Bool().Draw(t, "has_metadata") {
		n := rapid.IntRange(1, 3).Draw(t, "n_meta")
		u.Metadata = make(map[string]string, n)
		for i := 0; i < n; i++ {
			k := rapid.StringN(1, 10, -1).Draw(t, "meta_key")
			v := rapid.StringN(0, 20, -1).Draw(t, "meta_val")
			u.Metadata[k] = v
		}
	}

	// Optional nested message
	if rapid.Bool().Draw(t, "has_address") {
		u.Address = rapidAddress(t)
	}

	// Optional string pointers
	if rapid.Bool().Draw(t, "has_phone") {
		s := rapid.StringN(0, 20, -1).Draw(t, "phone")
		u.Phone = &s
	}
	if rapid.Bool().Draw(t, "has_nickname") {
		s := rapid.StringN(0, 20, -1).Draw(t, "nickname")
		u.Nickname = &s
	}

	// Bytes
	if rapid.Bool().Draw(t, "has_avatar") {
		u.Avatar = rapid.SliceOfN(rapid.Byte(), 0, 32).Draw(t, "avatar")
	}

	// Optional *[]byte
	if rapid.Bool().Draw(t, "has_thumb") {
		b := rapid.SliceOfN(rapid.Byte(), 0, 16).Draw(t, "thumb")
		u.AvatarThumbnail = &b
	}

	// Repeated messages
	nTags := rapid.IntRange(0, 3).Draw(t, "n_tags")
	if nTags > 0 {
		u.Tags = make([]*gen.Tag, nTags)
		for i := range u.Tags {
			u.Tags[i] = rapidTag(t)
		}
	}

	// Optional timestamp
	if rapid.Bool().Draw(t, "has_deleted_at") {
		ts := time.Unix(rapid.Int64Range(1, 2000000000).Draw(t, "deleted_at"), 0).UTC()
		u.DeletedAt = &ts
	}

	// Optional enum
	if rapid.Bool().Draw(t, "has_prev_status") {
		s := rapid.Int32Range(1, 5).Draw(t, "prev_status")
		u.PreviousStatus = &s
	}

	// Update mask ([]string)
	u.UpdateMask = rapid.SliceOfN(rapid.StringN(0, 20, -1), 0, 3).Draw(t, "update_mask")

	// EventTimes (map[string]time.Time)
	if rapid.Bool().Draw(t, "has_event_times") {
		n := rapid.IntRange(1, 3).Draw(t, "n_events")
		u.EventTimes = make(map[string]time.Time, n)
		for i := 0; i < n; i++ {
			k := rapid.StringN(1, 10, -1).Draw(t, "et_key")
			ts := time.Unix(rapid.Int64Range(0, 2000000000).Draw(t, "et_val"), 0).UTC()
			u.EventTimes[k] = ts
		}
	}

	// Oneof: ContactEmail or ContactPhone (not both)
	switch rapid.IntRange(0, 2).Draw(t, "oneof_contact") {
	case 1:
		s := rapid.StringN(0, 30, -1).Draw(t, "contact_email")
		u.ContactEmail = &s
	case 2:
		s := rapid.StringN(0, 20, -1).Draw(t, "contact_phone")
		u.ContactPhone = &s
	}

	// Wrapper map fields
	if rapid.Bool().Draw(t, "has_labels") {
		n := rapid.IntRange(1, 3).Draw(t, "n_labels")
		u.Labels = make(map[string]*string, n)
		for i := 0; i < n; i++ {
			k := rapid.StringN(1, 10, -1).Draw(t, "label_key")
			v := rapid.StringN(0, 20, -1).Draw(t, "label_val")
			u.Labels[k] = &v
		}
	}
	if rapid.Bool().Draw(t, "has_scores") {
		n := rapid.IntRange(1, 3).Draw(t, "n_scores")
		u.Scores = make(map[string]*int64, n)
		for i := 0; i < n; i++ {
			k := rapid.StringN(1, 10, -1).Draw(t, "score_key")
			v := rapid.Int64Range(-100, 100).Draw(t, "score_val")
			u.Scores[k] = &v
		}
	}

	return &u
}

// ---------- P1: Clone/Equal consistency (rapid) ----------

func TestRapid_CloneEqualConsistency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		u := rapidUser(t)
		clone := u.Clone()

		if !clone.Equal(u) {
			t.Fatalf("Clone not Equal to original")
		}
		if !u.Equal(clone) {
			t.Fatalf("Equal is not symmetric: u.Equal(clone) is false")
		}
	})
}

// ---------- P1b: Equal reflexivity ----------

func TestRapid_EqualReflexive(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		u := rapidUser(t)
		if !u.Equal(u) {
			t.Fatalf("Equal is not reflexive")
		}
	})
}

// ---------- P1c: Equal symmetry after mutation ----------

func TestRapid_EqualSymmetric(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := rapidUser(t)
		b := rapidUser(t)

		ab := a.Equal(b)
		ba := b.Equal(a)
		if ab != ba {
			t.Fatalf("Equal is not symmetric: a.Equal(b)=%v, b.Equal(a)=%v", ab, ba)
		}
	})
}

// ---------- P2: Proto roundtrip (rapid) ----------

func TestRapid_ProtoRoundtrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		u := rapidUser(t)
		pb := u.ToProto()

		restored := &gen.User{}
		restored.FromProto(pb)

		if !restored.Equal(u) {
			t.Fatalf("Proto roundtrip mismatch:\n  original: %+v\n  restored: %+v", u, restored)
		}
	})
}

// ---------- P2b: Proto roundtrip isolation (rapid) ----------

func TestRapid_ProtoRoundtripIsolation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		u := rapidUser(t)
		pb := u.ToProto()
		snapshot := u.Clone()

		restored := &gen.User{}
		restored.FromProto(pb)

		// Mutate proto after FromProto
		pb.Email = "hacked"
		pb.Age = 999
		if len(pb.Roles) > 0 {
			pb.Roles = []string{"hacked_role"}
		}
		if len(pb.Metadata) > 0 {
			pb.Metadata = map[string]string{"hacked": "meta"}
		}
		if len(pb.Avatar) > 0 {
			pb.Avatar[0] ^= 0xFF
		}
		if len(pb.Tags) > 0 && pb.Tags[0] != nil {
			pb.Tags[0].Key = "hacked_tag"
		}

		if !restored.Equal(snapshot) {
			t.Fatal("FromProto result aliased the source proto")
		}
	})
}

// ---------- P3: Clone independence (rapid) ----------

func TestRapid_CloneIndependence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		u := rapidUser(t)
		clone := u.Clone()
		snapshot := u.Clone()

		// Mutate scalars
		clone.Email = "mutated@test.com"
		clone.Age = 999

		// Mutate slices
		if len(clone.Roles) > 0 {
			clone.Roles[0] = "mutated_role"
		} else {
			clone.Roles = append(clone.Roles, "added_role")
		}
		if len(clone.Avatar) > 0 {
			clone.Avatar[0] ^= 0xFF
		}

		// Mutate maps
		if len(clone.Metadata) > 0 {
			for k := range clone.Metadata {
				clone.Metadata[k] = "mutated_val"
				break
			}
		}

		// Mutate nested messages
		if clone.Address != nil {
			clone.Address.Street = "mutated street"
		}
		if len(clone.Tags) > 0 && clone.Tags[0] != nil {
			clone.Tags[0].Key = "mutated_tag"
		}
		if clone.Phone != nil {
			*clone.Phone = "999-9999"
		}

		// Mutate wrapper maps
		if len(clone.Labels) > 0 {
			for k := range clone.Labels {
				v := "mutated"
				clone.Labels[k] = &v
				break
			}
		}
		if len(clone.Scores) > 0 {
			for k := range clone.Scores {
				v := int64(999999)
				clone.Scores[k] = &v
				break
			}
		}

		if !u.Equal(snapshot) {
			t.Fatal("Original was mutated by clone modification")
		}
	})
}

// ---------- Nil safety ----------

func TestRapid_NilSafety(t *testing.T) {
	t.Run("Clone_nil", func(t *testing.T) {
		var u *gen.User
		if clone := u.Clone(); clone != nil {
			t.Fatalf("Clone(nil) should return nil, got %+v", clone)
		}
	})

	t.Run("Equal_nil", func(t *testing.T) {
		var u *gen.User
		if u.Equal(nil) != true {
			t.Fatal("nil.Equal(nil) should be true")
		}
	})

	t.Run("Equal_nil_other", func(t *testing.T) {
		u := &gen.User{Email: "test@example.com"}
		if u.Equal(nil) {
			t.Fatal("non-nil.Equal(nil) should be false")
		}
	})

	t.Run("ToProto_nil", func(t *testing.T) {
		var u *gen.User
		if pb := u.ToProto(); pb != nil {
			t.Fatalf("ToProto(nil) should return nil, got %+v", pb)
		}
	})

	t.Run("FromProto_nil", func(t *testing.T) {
		u := &gen.User{}
		u.FromProto(nil) // should not panic
	})

	t.Run("DoubleFromProto", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			a := rapidUser(t)
			b := rapidUser(t)

			pbA := a.ToProto()
			pbB := b.ToProto()

			u := &gen.User{}
			u.FromProto(pbA)
			u.FromProto(pbB) // re-use receiver

			expected := &gen.User{}
			expected.FromProto(pbB)

			if !u.Equal(expected) {
				t.Fatalf("FromProto receiver-reuse left stale state:\n  got:      %+v\n  expected: %+v", u, expected)
			}
		})
	})
}

// ---------- valid User field names for FieldMask ----------

var userFieldNames = []string{
	"id", "email", "display_name", "active", "age", "roles", "metadata",
	"address", "created_at", "session_timeout", "phone", "avatar", "nickname",
	"status", "tags", "deleted_at", "previous_status", "update_mask",
	"avatar_thumbnail", "event_times", "labels", "scores",
	"old_field", "optional_name", "big_number", "handle",
}

func rapidFieldPaths(t *rapid.T) []string {
	n := rapid.IntRange(0, len(userFieldNames)).Draw(t, "n_paths")
	if n == 0 {
		return nil
	}
	// Shuffle a copy and take the first n elements.
	shuffled := make([]string, len(userFieldNames))
	copy(shuffled, userFieldNames)
	// Fisher-Yates shuffle using rapid draws for deterministic shrinking.
	for i := len(shuffled) - 1; i > 0; i-- {
		j := rapid.IntRange(0, i).Draw(t, "shuffle")
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	return shuffled[:n]
}

// ---------- P4: FieldMask properties ----------

func TestRapid_FieldMaskIdempotency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dst := rapidUser(t)
		src := rapidUser(t)
		paths := rapidFieldPaths(t)
		if len(paths) == 0 {
			return // empty mask is a no-op
		}

		// Apply once.
		dst1 := dst.Clone()
		gen.ApplyFieldMaskUser(dst1, src, paths)

		// Apply twice.
		dst2 := dst.Clone()
		gen.ApplyFieldMaskUser(dst2, src, paths)
		gen.ApplyFieldMaskUser(dst2, src, paths)

		if !dst1.Equal(dst2) {
			t.Fatalf("FieldMask is not idempotent for paths %v", paths)
		}
	})
}

func TestRapid_FieldMaskFrameRule(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dst := rapidUser(t)
		src := rapidUser(t)

		snapshot := dst.Clone()

		// Apply with empty paths — dst must be unchanged.
		gen.ApplyFieldMaskUser(dst, src, nil)
		if !dst.Equal(snapshot) {
			t.Fatal("FieldMask with nil paths modified dst")
		}

		gen.ApplyFieldMaskUser(dst, src, []string{})
		if !dst.Equal(snapshot) {
			t.Fatal("FieldMask with empty paths modified dst")
		}
	})
}

// ---------- P5: Storage roundtrip ----------

func TestRapid_FirestoreRoundtrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		u := rapidUser(t)

		var fs gen.UserFirestore
		fs.FromDomain(u)
		restored := fs.ToDomain()

		if !restored.Equal(u) {
			t.Fatalf("Firestore roundtrip mismatch:\n  orig:     %+v\n  restored: %+v", u, restored)
		}
	})
}

func TestRapid_MongoRoundtrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		u := rapidUser(t)

		var m gen.UserMongo
		m.FromDomain(u)
		restored := m.ToDomain()

		if !restored.Equal(u) {
			t.Fatalf("Mongo roundtrip mismatch:\n  orig:     %+v\n  restored: %+v", u, restored)
		}
	})
}

// ---------- P6: Encrypt/Decrypt roundtrip ----------

// xorEncryptor is a simple reversible encryptor for property testing.
// It XORs plaintext with a key derived from scope+fieldName, then base64-encodes.
type xorEncryptor struct{}

func (xorEncryptor) Encrypt(plaintext, scope, fieldName string) (string, error) {
	key := deriveKey(scope, fieldName)
	ct := xorBytes([]byte(plaintext), key)
	return base64.StdEncoding.EncodeToString(ct), nil
}

func (xorEncryptor) Decrypt(ciphertext, scope, fieldName string) (string, error) {
	ct, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt base64: %w", err)
	}
	key := deriveKey(scope, fieldName)
	return string(xorBytes(ct, key)), nil
}

func deriveKey(scope, fieldName string) []byte {
	// Simple deterministic key derivation for testing.
	raw := []byte(scope + ":" + fieldName)
	if len(raw) == 0 {
		raw = []byte{0x42}
	}
	return raw
}

func xorBytes(data, key []byte) []byte {
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ key[i%len(key)]
	}
	return out
}

func TestRapid_EncryptDecryptRoundtrip(t *testing.T) {
	enc := xorEncryptor{}
	rapid.Check(t, func(t *rapid.T) {
		u := rapidUser(t)
		scope := rapid.StringN(1, 20, -1).Draw(t, "scope")

		var fs gen.UserFirestore
		fs.FromDomain(u)

		// Snapshot the phone before encrypt.
		var origPhone *string
		if fs.Phone != nil {
			s := *fs.Phone
			origPhone = &s
		}

		if err := fs.EncryptFields(enc, scope); err != nil {
			t.Fatalf("EncryptFields: %v", err)
		}

		// If phone was non-empty, ciphertext should differ from plaintext.
		if origPhone != nil && *origPhone != "" && fs.Phone != nil && *fs.Phone == *origPhone {
			t.Fatal("EncryptFields did not change non-empty phone")
		}

		if err := fs.DecryptFields(enc, scope); err != nil {
			t.Fatalf("DecryptFields: %v", err)
		}

		// After decrypt, phone must match original.
		if origPhone == nil {
			if fs.Phone != nil {
				t.Fatal("phone should be nil after decrypt")
			}
		} else {
			if fs.Phone == nil || *fs.Phone != *origPhone {
				t.Fatalf("phone mismatch: got %v, want %v", fs.Phone, *origPhone)
			}
		}
	})
}

// ---------- Additional properties ----------

func TestRapid_EqualTransitivity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := rapidUser(t)
		b := a.Clone()
		c := b.Clone()

		if !a.Equal(b) || !b.Equal(c) {
			t.Fatal("Clone should produce Equal structs")
		}
		if !a.Equal(c) {
			t.Fatal("Equal is not transitive: a==b && b==c but a!=c")
		}
	})
}

func TestRapid_ValidateCloneConsistency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		u := rapidUser(t)
		clone := u.Clone()

		origErr := u.Validate()
		cloneErr := clone.Validate()

		if (origErr == nil) != (cloneErr == nil) {
			t.Fatalf("Validate inconsistency: orig=%v, clone=%v", origErr, cloneErr)
		}
	})
}

// ---------- rapidDocument generator ----------

func rapidSettings(t *rapid.T) *gen.Settings {
	return &gen.Settings{
		Theme:  rapid.Int32Range(0, 10).Draw(t, "theme"),
		Locale: rapid.StringN(0, 10, -1).Draw(t, "locale"),
	}
}

func rapidDocument(t *rapid.T) *gen.Document {
	d := gen.Document{
		ID: rapid.StringN(0, 20, -1).Draw(t, "doc_id"),
	}

	// Message-valued map (BUG-4 regression surface).
	if rapid.Bool().Draw(t, "has_settings") {
		n := rapid.IntRange(1, 3).Draw(t, "n_settings")
		d.SettingsMap = make(map[string]*gen.Settings, n)
		for i := 0; i < n; i++ {
			k := rapid.StringN(1, 10, -1).Draw(t, "setting_key")
			d.SettingsMap[k] = rapidSettings(t)
		}
	}

	// Scalar map.
	if rapid.Bool().Draw(t, "has_codenames") {
		n := rapid.IntRange(1, 3).Draw(t, "n_codes")
		d.CodeNames = make(map[int32]string, n)
		for i := 0; i < n; i++ {
			k := rapid.Int32Range(0, 100).Draw(t, "code_key")
			d.CodeNames[k] = rapid.StringN(0, 10, -1).Draw(t, "code_val")
		}
	}

	// Update mask.
	d.UpdateMask = rapid.SliceOfN(rapid.StringN(0, 10, -1), 0, 3).Draw(t, "doc_mask")

	// Optional wrappers.
	if rapid.Bool().Draw(t, "has_archived") {
		b := rapid.Bool().Draw(t, "archived")
		d.Archived = &b
	}
	if rapid.Bool().Draw(t, "has_viewcount") {
		v := rapid.Int64Range(0, 1000000).Draw(t, "view_count")
		d.ViewCount = &v
	}

	return &d
}

// ---------- P3 extension: Document clone independence (BUG-4 surface) ----------

func TestRapid_DocumentCloneIndependence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		doc := rapidDocument(t)
		snapshot := doc.Clone()
		clone := doc.Clone()

		// Mutate map values deeply on the clone.
		for k, s := range clone.SettingsMap {
			if s != nil {
				s.Theme = 999
				s.Locale = "mutated"
			}
			_ = k
		}
		// Mutate scalar map.
		for k := range clone.CodeNames {
			clone.CodeNames[k] = "mutated"
		}
		// Mutate optional pointer.
		if clone.Archived != nil {
			b := !(*clone.Archived)
			clone.Archived = &b
		}

		// Original must be completely untouched.
		if !doc.Equal(snapshot) {
			t.Fatalf("mutating clone changed original:\n  orig:     %+v\n  snapshot: %+v", doc, snapshot)
		}
	})
}

// ---------- P4a: FieldMask isolation (mutation after apply) ----------

func TestRapid_FieldMaskIsolation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		dst := rapidUser(t)
		src := rapidUser(t)

		// Pick a non-empty subset of reference-type paths (slices, maps, bytes).
		refPaths := []string{"roles", "metadata", "avatar", "tags", "update_mask", "event_times", "avatar_thumbnail"}
		n := rapid.IntRange(1, len(refPaths)).Draw(t, "n_ref_paths")
		paths := refPaths[:n]

		gen.ApplyFieldMaskUser(dst, src, paths)
		snapshot := dst.Clone()

		// Mutate src's reference-type fields.
		if len(src.Roles) > 0 {
			src.Roles[0] = "MUTATED"
		}
		if src.Metadata != nil {
			src.Metadata["INJECTED"] = "hack"
		}
		if len(src.Avatar) > 0 {
			src.Avatar[0] ^= 0xFF
		}
		if len(src.Tags) > 0 && src.Tags[0] != nil {
			src.Tags[0].Key = "MUTATED"
		}
		if len(src.UpdateMask) > 0 {
			src.UpdateMask[0] = "MUTATED"
		}
		if src.EventTimes != nil {
			src.EventTimes["INJECTED"] = time.Now()
		}
		if src.AvatarThumbnail != nil && len(*src.AvatarThumbnail) > 0 {
			(*src.AvatarThumbnail)[0] ^= 0xFF
		}

		// dst must be unaffected by src mutations.
		if !dst.Equal(snapshot) {
			t.Fatalf("FieldMask isolation violated: mutating src changed dst for paths %v", paths)
		}
	})
}

// ---------- TryToProto error path ----------

func TestTryToProtoErrorPath(t *testing.T) {
	// ExtraMetadata with an unsupported type should cause TryToProto to return an error.
	u := &gen.User{
		ID:    "test",
		Email: "test@test.com",
		ExtraMetadata: map[string]any{
			"bad": func() {}, // functions are not supported by structpb
		},
	}

	pb, err := u.TryToProto()
	if err == nil {
		t.Fatal("TryToProto should return error for unsupported structpb type")
	}
	if pb != nil {
		t.Fatal("TryToProto should return nil proto on error")
	}

	// Verify ToProto doesn't panic (it logs instead).
	result := u.ToProto()
	if result == nil {
		t.Fatal("ToProto should not return nil")
	}
	// ExtraMetadata should be nil because of the conversion failure.
	if result.ExtraMetadata != nil {
		t.Fatal("ToProto should set ExtraMetadata to nil on conversion failure")
	}
}
