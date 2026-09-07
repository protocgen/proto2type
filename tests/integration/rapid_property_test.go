package integration

import (
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
