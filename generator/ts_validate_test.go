package generator

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
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
	expected := `.min(3).max(100).email().url().uuid().regex(new RegExp("^[a-z]+$"), { message: "must match pattern" })`
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
	expected := `.refine(v => { try { return BigInt(v) > 0n; } catch { return false; } }, { message: "must be > 0" }).refine(v => { try { return BigInt(v) >= 1n; } catch { return false; } }, { message: "must be >= 1" }).refine(v => { try { return BigInt(v) < 10n; } catch { return false; } }, { message: "must be < 10" }).refine(v => { try { return BigInt(v) <= 9n; } catch { return false; } }, { message: "must be <= 9" })`
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
	expected := `.refine(v => v > 0n, { message: "must be > 0" }).refine(v => v >= 1n, { message: "must be >= 1" })`
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
	expected := `.refine(v => /^[A-Za-z0-9+\/\-_]*={0,2}$/.test(v) && Math.floor(v.replace(/=+$/, "").length * 3 / 4) >= 5, { message: "bytes must be at least 5 bytes" }).refine(v => /^[A-Za-z0-9+\/\-_]*={0,2}$/.test(v) && Math.floor(v.replace(/=+$/, "").length * 3 / 4) <= 10, { message: "bytes must be at most 10 bytes" })`
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

func TestTsZodConstraints_NewStringConstraints(t *testing.T) {
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.StringKind,
		ValidateConstraints: &ValidateConstraints{
			Len:      uint64Ptr(10),
			Prefix:   "hello",
			Suffix:   "world",
			Contains: "foo",
			Hostname: true,
		},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	for _, want := range []string{".length(10)", `.startsWith("hello")`, `.endsWith("world")`, `.includes("foo")`, `hostname`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output: %s", want, got)
		}
	}
}

func TestTsZodConstraints_IP(t *testing.T) {
	f := &DomainField{
		Kind:                FieldKindScalar,
		ScalarKind:          protoreflect.StringKind,
		ValidateConstraints: &ValidateConstraints{IP: true},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	if !strings.Contains(got, ".ip()") {
		t.Errorf("missing .ip() in output: %s", got)
	}
}

func TestTsZodConstraints_NumericConst(t *testing.T) {
	constVal := "42"
	f := &DomainField{
		Kind:                FieldKindScalar,
		ScalarKind:          protoreflect.Int32Kind,
		ValidateConstraints: &ValidateConstraints{Const: &constVal},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	if !strings.Contains(got, "v === 42") {
		t.Errorf("missing const check in output: %s", got)
	}
}

func TestTsZodConstraints_InNotIn(t *testing.T) {
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.Int32Kind,
		ValidateConstraints: &ValidateConstraints{
			In:    []string{"1", "2", "3"},
			NotIn: []string{"10", "20"},
		},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	if !strings.Contains(got, "[1, 2, 3].includes") {
		t.Errorf("missing In check: %s", got)
	}
	if !strings.Contains(got, "![10, 20].includes") {
		t.Errorf("missing NotIn check: %s", got)
	}
}

func TestTsZodConstraints_Unique(t *testing.T) {
	vc := &ValidateConstraints{Unique: true}
	got := tsRepeatedConstraints(vc)
	if !strings.Contains(got, "new Set(v).size === v.length") {
		t.Errorf("missing unique check: %s", got)
	}
}
