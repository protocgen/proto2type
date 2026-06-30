package generator

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestGoDomainSingularTypeFromIR_OptionalTimestamp(t *testing.T) {
	f := &DomainField{Kind: FieldKindTimestamp, Optional: true}
	got := goDomainSingularTypeFromIR(f)
	want := "*time.Time"
	if got != want {
		t.Errorf("goDomainSingularTypeFromIR(optional Timestamp) = %q, want %q", got, want)
	}
}

func TestGoDomainSingularTypeFromIR_NonOptionalTimestamp(t *testing.T) {
	f := &DomainField{Kind: FieldKindTimestamp, Optional: false}
	got := goDomainSingularTypeFromIR(f)
	want := "time.Time"
	if got != want {
		t.Errorf("goDomainSingularTypeFromIR(non-optional Timestamp) = %q, want %q", got, want)
	}
}

func TestGoDomainSingularTypeFromIR_OptionalDuration(t *testing.T) {
	f := &DomainField{Kind: FieldKindDuration, Optional: true}
	got := goDomainSingularTypeFromIR(f)
	want := "*time.Duration"
	if got != want {
		t.Errorf("goDomainSingularTypeFromIR(optional Duration) = %q, want %q", got, want)
	}
}

func TestGoDomainSingularTypeFromIR_OptionalEnum(t *testing.T) {
	f := &DomainField{Kind: FieldKindEnum, Optional: true}
	got := goDomainSingularTypeFromIR(f)
	want := "*int32"
	if got != want {
		t.Errorf("goDomainSingularTypeFromIR(optional Enum) = %q, want %q", got, want)
	}
}

func TestGoDomainSingularTypeFromIR_OptionalEnumAsString(t *testing.T) {
	f := &DomainField{Kind: FieldKindEnum, Optional: true, EnumAsString: true}
	got := goDomainSingularTypeFromIR(f)
	want := "*string"
	if got != want {
		t.Errorf("goDomainSingularTypeFromIR(optional Enum as string) = %q, want %q", got, want)
	}
}

func TestGoDomainSingularTypeFromIR_OptionalScalar(t *testing.T) {
	f := &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, Optional: true}
	got := goDomainSingularTypeFromIR(f)
	want := "*string"
	if got != want {
		t.Errorf("goDomainSingularTypeFromIR(optional string) = %q, want %q", got, want)
	}
}
