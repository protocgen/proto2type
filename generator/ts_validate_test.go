package generator

import (
	"google.golang.org/protobuf/reflect/protoreflect"
	"testing"
)

func uint64Ptr(v uint64) *uint64 { return &v }
func strPtr(v string) *string    { return &v }

func TestTsZodConstraints_StringField(t *testing.T) {
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.StringKind,
		ValidateConstraints: &ValidateConstraints{
			MinLength: uint64Ptr(3),
			MaxLength: uint64Ptr(100),
			Email:     true,
			URI:       true,
			UUID:      true,
			Pattern:   "^[a-z]+$",
		},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	expected := `.min(3).max(100).email().url().uuid().regex(new RegExp("^[a-z]+$"))`
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestTsZodConstraints_NumericField(t *testing.T) {
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.Int32Kind,
		ValidateConstraints: &ValidateConstraints{
			Gt:  strPtr("0"),
			Gte: strPtr("1"),
			Lt:  strPtr("10"),
			Lte: strPtr("9"),
		},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	expected := `.gt(0).gte(1).lt(10).lte(9)`
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestTsZodConstraints_Int64String(t *testing.T) {
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.Int64Kind,
		ValidateConstraints: &ValidateConstraints{
			Gt:  strPtr("0"),
			Gte: strPtr("1"),
			Lt:  strPtr("10"),
			Lte: strPtr("9"),
		},
	}
	opts := &Options{TSInt64Style: "string"}
	got := tsZodConstraints(f, opts)
	expected := `.refine(v => BigInt(v) > 0n, { message: "must be > 0" }).refine(v => BigInt(v) >= 1n, { message: "must be >= 1" }).refine(v => BigInt(v) < 10n, { message: "must be < 10" }).refine(v => BigInt(v) <= 9n, { message: "must be <= 9" })`
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestTsZodConstraints_Int64BigInt(t *testing.T) {
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.Int64Kind,
		ValidateConstraints: &ValidateConstraints{
			Gt:  strPtr("0"),
			Gte: strPtr("1"),
		},
	}
	opts := &Options{TSInt64Style: "bigint"}
	got := tsZodConstraints(f, opts)
	expected := `.gt(0n).gte(1n)`
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestTsZodConstraints_BytesLength(t *testing.T) {
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.BytesKind,
		ValidateConstraints: &ValidateConstraints{
			MinLength: uint64Ptr(5),
			MaxLength: uint64Ptr(10),
		},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	expected := `.refine(v => { if (!/^[A-Za-z0-9+/\-_]*={0,2}$/.test(v) || v.length % 4 !== 0) return false; const p = v.endsWith("==") ? 2 : v.endsWith("=") ? 1 : 0; return (v.length * 3 / 4) - p >= 5; }, { message: "bytes must be valid base64 and at least 5 bytes" }).refine(v => { if (!/^[A-Za-z0-9+/\-_]*={0,2}$/.test(v) || v.length % 4 !== 0) return false; const p = v.endsWith("==") ? 2 : v.endsWith("=") ? 1 : 0; return (v.length * 3 / 4) - p <= 10; }, { message: "bytes must be valid base64 and at most 10 bytes" })`
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestTsZodConstraints_NoConstraints(t *testing.T) {
	f := &DomainField{
		Kind:                FieldKindScalar,
		ScalarKind:          protoreflect.StringKind,
		ValidateConstraints: nil,
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}

	f.ValidateConstraints = &ValidateConstraints{}
	got = tsZodConstraints(f, opts)
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestTsOneofVariantZodConstraints(t *testing.T) {
	v := &OneofVariant{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.StringKind,
		ValidateConstraints: &ValidateConstraints{
			MinLength: uint64Ptr(3),
		},
	}
	opts := &Options{}
	got := tsOneofVariantZodConstraints(v, opts)
	expected := `.min(3)`
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}
