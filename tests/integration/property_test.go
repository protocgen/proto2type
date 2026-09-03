package integration

import (
	"testing"
	"time"

	"github.com/protocgen/proto2type/testdata/golden/go/gen"
	"hegel.dev/go/hegel"
)

// genAddress generates a random Address.
func genAddress(ht *hegel.T) *gen.Address {
	return &gen.Address{
		Street:  hegel.Draw(ht, hegel.Text().MaxSize(50)),
		City:    hegel.Draw(ht, hegel.Text().MaxSize(30)),
		State:   hegel.Draw(ht, hegel.Text().MaxSize(10)),
		Zip:     hegel.Draw(ht, hegel.Text().MaxSize(10)),
		Country: hegel.Draw(ht, hegel.Text().MaxSize(20)),
	}
}

// genTag generates a random Tag.
func genTag(ht *hegel.T) *gen.Tag {
	return &gen.Tag{
		Key:   hegel.Draw(ht, hegel.Text().MaxSize(20)),
		Value: hegel.Draw(ht, hegel.Text().MaxSize(50)),
	}
}

// genUser generates a random User with all field types exercised.
func genUser(ht *hegel.T) *gen.User {
	u := gen.User{
		ID:          hegel.Draw(ht, hegel.Text().MaxSize(30)),
		Email:       hegel.Draw(ht, hegel.Text().MaxSize(50)),
		DisplayName: hegel.Draw(ht, hegel.Text().MaxSize(50)),
		Active:      hegel.Draw(ht, hegel.Booleans()),
		Age:         int32(hegel.Draw(ht, hegel.Integers(0, 150))),
		Status:      int32(hegel.Draw(ht, hegel.Integers(0, 5))),
		// UTC and truncate to second precision for proto roundtrip compatibility.
		CreatedAt:      time.Unix(int64(hegel.Draw(ht, hegel.Integers(0, 2000000000))), 0).UTC(),
		SessionTimeout: time.Duration(hegel.Draw(ht, hegel.Integers(0, 3600))) * time.Second,
		OldField:       hegel.Draw(ht, hegel.Text().MaxSize(20)),
		OptionalName:   hegel.Draw(ht, hegel.Text().MaxSize(20)),
		BigNumber:      int64(hegel.Draw(ht, hegel.Integers(-1000000, 1000000))),
		Handle:         hegel.Draw(ht, hegel.Text().MaxSize(20)),
	}

	// Repeated string field
	nRoles := hegel.Draw(ht, hegel.Integers(0, 5))
	if nRoles > 0 {
		u.Roles = make([]string, nRoles)
		for i := range u.Roles {
			u.Roles[i] = hegel.Draw(ht, hegel.Text().MaxSize(20))
		}
	}

	// Map field
	nMeta := hegel.Draw(ht, hegel.Integers(0, 3))
	if nMeta > 0 {
		u.Metadata = make(map[string]string, nMeta)
		for i := 0; i < nMeta; i++ {
			k := hegel.Draw(ht, hegel.Text().MinSize(1).MaxSize(10))
			v := hegel.Draw(ht, hegel.Text().MaxSize(20))
			u.Metadata[k] = v
		}
	}

	// Optional nested message
	if hegel.Draw(ht, hegel.Booleans()) {
		u.Address = genAddress(ht)
	}

	// Optional string pointers
	if hegel.Draw(ht, hegel.Booleans()) {
		s := hegel.Draw(ht, hegel.Text().MaxSize(20))
		u.Phone = &s
	}
	if hegel.Draw(ht, hegel.Booleans()) {
		s := hegel.Draw(ht, hegel.Text().MaxSize(20))
		u.Nickname = &s
	}

	// Bytes
	if hegel.Draw(ht, hegel.Booleans()) {
		nBytes := hegel.Draw(ht, hegel.Integers(0, 32))
		u.Avatar = make([]byte, nBytes)
		for i := range u.Avatar {
			u.Avatar[i] = byte(hegel.Draw(ht, hegel.Integers(0, 255)))
		}
	}

	// Optional *[]byte
	if hegel.Draw(ht, hegel.Booleans()) {
		nBytes := hegel.Draw(ht, hegel.Integers(0, 16))
		b := make([]byte, nBytes)
		for i := range b {
			b[i] = byte(hegel.Draw(ht, hegel.Integers(0, 255)))
		}
		u.AvatarThumbnail = &b
	}

	// Repeated messages
	nTags := hegel.Draw(ht, hegel.Integers(0, 3))
	if nTags > 0 {
		u.Tags = make([]*gen.Tag, nTags)
		for i := range u.Tags {
			u.Tags[i] = genTag(ht)
		}
	}

	// Optional timestamp
	if hegel.Draw(ht, hegel.Booleans()) {
		t := time.Unix(int64(hegel.Draw(ht, hegel.Integers(1, 2000000000))), 0).UTC()
		u.DeletedAt = &t
	}

	// Optional enum (int32)
	if hegel.Draw(ht, hegel.Booleans()) {
		s := int32(hegel.Draw(ht, hegel.Integers(1, 5)))
		u.PreviousStatus = &s
	}

	// Update mask ([]string)
	nMask := hegel.Draw(ht, hegel.Integers(0, 3))
	if nMask > 0 {
		u.UpdateMask = make([]string, nMask)
		for i := range u.UpdateMask {
			u.UpdateMask[i] = hegel.Draw(ht, hegel.Text().MaxSize(20))
		}
	}

	// EventTimes (map[string]time.Time)
	nEvents := hegel.Draw(ht, hegel.Integers(0, 3))
	if nEvents > 0 {
		u.EventTimes = make(map[string]time.Time, nEvents)
		for i := 0; i < nEvents; i++ {
			k := hegel.Draw(ht, hegel.Text().MinSize(1).MaxSize(10))
			t := time.Unix(int64(hegel.Draw(ht, hegel.Integers(0, 2000000000))), 0).UTC()
			u.EventTimes[k] = t
		}
	}

	// Oneof: ContactEmail or ContactPhone (not both)
	switch hegel.Draw(ht, hegel.Integers(0, 2)) {
	case 1:
		s := hegel.Draw(ht, hegel.Text().MaxSize(30))
		u.ContactEmail = &s
	case 2:
		s := hegel.Draw(ht, hegel.Text().MaxSize(20))
		u.ContactPhone = &s
	}

	return &u
}

// Property P1: Clone/Equal Consistency
// For any User u, u.Clone() should be Equal to u, and vice versa.
func TestProperty_CloneEqualConsistency(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		u := genUser(ht)
		clone := u.Clone()

		if !clone.Equal(u) {
			ht.Fatalf("Clone not Equal to original:\n  original: %+v\n  clone:    %+v", u, clone)
		}
		if !u.Equal(clone) {
			ht.Fatalf("Original not Equal to clone (asymmetric)")
		}
	})
}

// Property P1b: Clone independence — mutating a clone doesn't affect the original.
func TestProperty_CloneIndependence(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		u := genUser(ht)
		clone := u.Clone()
		snapshot := u.Clone()

		// Mutate the clone
		clone.Email = "mutated@test.com"
		clone.Age = 999
		clone.Roles = []string{"mutated"}
		clone.Metadata = map[string]string{"mutated": "true"}

		// Original should be unchanged
		if !u.Equal(snapshot) {
			ht.Fatal("Original was mutated by clone modification")
		}
	})
}

// Property P2: Proto Roundtrip
// For any User u, ToProto then FromProto should produce an Equal User.
func TestProperty_ProtoRoundtrip(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		u := genUser(ht)
		pb := u.ToProto()

		restored := &gen.User{}
		restored.FromProto(pb)

		if !restored.Equal(u) {
			ht.Fatalf("Proto roundtrip mismatch:\n  original: %+v\n  restored: %+v", u, restored)
		}
	})
}

// Property P2b: Proto roundtrip preserves isolation — source proto is not aliased.
func TestProperty_ProtoRoundtripIsolation(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		u := genUser(ht)
		pb := u.ToProto()
		snapshot := u.Clone()

		restored := &gen.User{}
		restored.FromProto(pb)

		// Mutate proto after FromProto
		pb.Email = "hacked"
		pb.Age = 999

		// Restored should be unaffected
		if !restored.Equal(snapshot) {
			ht.Fatal("FromProto result aliased the source proto")
		}
	})
}
