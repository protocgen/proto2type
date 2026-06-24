package gen_string_enum

import (
	"testing"

	pb "github.com/protocgen/proto2type/testdata/golden/go/pb"
)

// TestStringEnumRoundTrip verifies that enum fields stored as strings
// correctly round-trip through ToProto/FromProto conversions.
func TestStringEnumRoundTrip(t *testing.T) {
	original := &User{
		Email:       "test@example.com",
		DisplayName: "Test User",
		Active:      true,
		Status:      "USER_STATUS_ACTIVE",
	}

	// Domain → Proto
	proto := original.ToProto()
	if proto.Status != pb.UserStatus_USER_STATUS_ACTIVE {
		t.Errorf("ToProto: Status = %v, want USER_STATUS_ACTIVE (%d)",
			proto.Status, pb.UserStatus_USER_STATUS_ACTIVE)
	}

	// Proto → Domain
	var roundTripped User
	roundTripped.FromProto(proto)
	if roundTripped.Status != "USER_STATUS_ACTIVE" {
		t.Errorf("FromProto: Status = %q, want %q",
			roundTripped.Status, "USER_STATUS_ACTIVE")
	}

	// Full field equality
	if roundTripped.Email != original.Email {
		t.Errorf("Email: got %q, want %q", roundTripped.Email, original.Email)
	}
	if roundTripped.DisplayName != original.DisplayName {
		t.Errorf("DisplayName: got %q, want %q", roundTripped.DisplayName, original.DisplayName)
	}
	if roundTripped.Active != original.Active {
		t.Errorf("Active: got %v, want %v", roundTripped.Active, original.Active)
	}
}

// TestStringEnumZeroValue verifies that zero-value enum strings
// map to the UNSPECIFIED proto enum value.
func TestStringEnumZeroValue(t *testing.T) {
	empty := &User{}

	proto := empty.ToProto()
	if proto.Status != pb.UserStatus_USER_STATUS_UNSPECIFIED {
		t.Errorf("ToProto zero value: Status = %v, want UNSPECIFIED", proto.Status)
	}

	var roundTripped User
	roundTripped.FromProto(proto)
	if roundTripped.Status != "USER_STATUS_UNSPECIFIED" {
		t.Errorf("FromProto zero value: Status = %q, want %q",
			roundTripped.Status, "USER_STATUS_UNSPECIFIED")
	}
}

// TestStringEnumClone verifies Clone preserves string enum fields.
func TestStringEnumClone(t *testing.T) {
	original := &User{
		Email:  "test@example.com",
		Status: "USER_STATUS_SUSPENDED",
	}

	cloned := original.Clone()
	if cloned.Status != original.Status {
		t.Errorf("Clone: Status = %q, want %q", cloned.Status, original.Status)
	}
}

// TestStringEnumEqual verifies Equal works with string enum fields.
func TestStringEnumEqual(t *testing.T) {
	a := &User{Status: "USER_STATUS_ACTIVE"}
	b := &User{Status: "USER_STATUS_ACTIVE"}
	c := &User{Status: "USER_STATUS_SUSPENDED"}

	if !a.Equal(b) {
		t.Error("Equal: identical statuses should be equal")
	}
	if a.Equal(c) {
		t.Error("Equal: different statuses should not be equal")
	}
}
