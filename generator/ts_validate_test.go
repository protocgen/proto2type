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
	expected := `.email().url().uuid().regex(new RegExp("^[a-z]+$", "u"), { message: "must match pattern" }).refine(v => [...v].length >= 3, { message: "must be at least 3 characters" }).refine(v => [...v].length <= 100, { message: "must be at most 100 characters" }).refine(v => /^https?:\/\//i.test(v), { message: "must use http or https scheme" })`
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
	expected := `.refine(v => [...v].length >= 3, { message: "must be at least 3 characters" })`
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
	for _, want := range []string{`[...v].length === 10`, `.startsWith("hello")`, `.endsWith("world")`, `.includes("foo")`, `hostname`} {
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

func TestTsZodConstraints_BytesExactLen(t *testing.T) {
	v := uint64(3)
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.BytesKind,
		ValidateConstraints: &ValidateConstraints{
			Len: &v,
		},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	if !strings.Contains(got, "=== 3") {
		t.Errorf("missing exact length check: %s", got)
	}
	if !strings.Contains(got, "Math.floor") {
		t.Errorf("should use decoded byte length: %s", got)
	}
}

func TestTsZodConstraints_PatternInvalidRE2(t *testing.T) {
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.StringKind,
		ValidateConstraints: &ValidateConstraints{
			Pattern: "(?!lookahead)", // negative lookahead: not RE2-safe
		},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	if !strings.Contains(got, "WARN") {
		t.Errorf("should emit WARN for non-RE2 pattern: %s", got)
	}
	if strings.Contains(got, "new RegExp") {
		t.Errorf("should not emit RegExp for non-RE2 pattern: %s", got)
	}
}

func TestTsZodConstraints_PatternNamedGroupRejected(t *testing.T) {
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.StringKind,
		ValidateConstraints: &ValidateConstraints{
			Pattern: `(?P<name>[a-z]+)`, // RE2 named group, invalid JS
		},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	if !strings.Contains(got, "WARN") {
		t.Errorf("should emit WARN for RE2 named group: %s", got)
	}
	if !strings.Contains(got, "named groups") {
		t.Errorf("should mention named groups: %s", got)
	}
}

func TestTsZodConstraints_PatternCommentInjection(t *testing.T) {
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.StringKind,
		ValidateConstraints: &ValidateConstraints{
			Pattern: "(?!evil*/inject)", // contains */ to break comment
		},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	// Must not contain raw */ that would close the block comment
	if strings.Contains(got, "*/inject") {
		t.Errorf("comment injection not sanitized: %s", got)
	}
}

func TestTsZodConstraints_PatternValid(t *testing.T) {
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.StringKind,
		ValidateConstraints: &ValidateConstraints{
			Pattern: `^[a-z0-9]+$`,
		},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	if !strings.Contains(got, "new RegExp") {
		t.Errorf("should emit RegExp for valid pattern: %s", got)
	}
}

func TestTsMapConstraints_MinMax(t *testing.T) {
	min := uint64(2)
	max := uint64(10)
	vc := &ValidateConstraints{MinItems: &min, MaxItems: &max}
	got := tsMapConstraints(vc)
	if !strings.Contains(got, "Object.keys(v).length >= 2") {
		t.Errorf("missing MinItems check: %s", got)
	}
	if !strings.Contains(got, "Object.keys(v).length <= 10") {
		t.Errorf("missing MaxItems check: %s", got)
	}
}

func TestTsMapConstraints_Empty(t *testing.T) {
	got := tsMapConstraints(nil)
	if got != "" {
		t.Errorf("expected empty for nil vc, got %q", got)
	}
	got = tsMapConstraints(&ValidateConstraints{})
	if got != "" {
		t.Errorf("expected empty for empty vc, got %q", got)
	}
}

func TestTsScalarDefault(t *testing.T) {
	tests := []struct {
		kind    protoreflect.Kind
		int64   string
		want    string
		wantBig string
	}{
		{protoreflect.BoolKind, "string", "false", "false"},
		{protoreflect.Int32Kind, "string", "0", "0"},
		{protoreflect.StringKind, "string", `""`, `""`},
		{protoreflect.BytesKind, "string", `""`, `""`},
		{protoreflect.Int64Kind, "string", `"0"`, "0n"},
		{protoreflect.Uint64Kind, "string", `"0"`, "0n"},
	}
	for _, tt := range tests {
		opts := &Options{TSInt64Style: "string"}
		got := tsScalarDefault(tt.kind, opts)
		if got != tt.want {
			t.Errorf("tsScalarDefault(%v, string) = %q, want %q", tt.kind, got, tt.want)
		}
		if tt.kind == protoreflect.Int64Kind || tt.kind == protoreflect.Uint64Kind {
			opts.TSInt64Style = "bigint"
			got = tsScalarDefault(tt.kind, opts)
			if got != tt.wantBig {
				t.Errorf("tsScalarDefault(%v, bigint) = %q, want %q", tt.kind, got, tt.wantBig)
			}
		}
	}
}

func TestTsZodConstraints_StringConst(t *testing.T) {
	c := `"hello"`
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.StringKind,
		ValidateConstraints: &ValidateConstraints{
			Const: &c,
		},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	if !strings.Contains(got, `v === "hello"`) {
		t.Errorf("missing const check: %s", got)
	}
}

func TestTsZodConstraints_StringInNotIn(t *testing.T) {
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.StringKind,
		ValidateConstraints: &ValidateConstraints{
			In:    []string{`"A"`, `"B"`},
			NotIn: []string{`"X"`},
		},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	if !strings.Contains(got, `"A"`) {
		t.Errorf("missing In check: %s", got)
	}
	if !strings.Contains(got, `"X"`) {
		t.Errorf("missing NotIn check: %s", got)
	}
}

func TestTsZodConstraints_URI_SchemeRestriction(t *testing.T) {
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.StringKind,
		ValidateConstraints: &ValidateConstraints{
			URI: true,
		},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	if !strings.Contains(got, "https?") {
		t.Errorf("URI validation should restrict to http(s) scheme: %s", got)
	}
}

func TestTsMapValueZodType_NullValue(t *testing.T) {
	info := &MapTypeInfo{
		Kind:         FieldKindEnum,
		EnumTypeName: "NullValue",
		EnumFullName: "google.protobuf.NullValue",
	}
	opts := &Options{}
	got := tsMapValueZodType(info, opts)
	if got != "z.null()" {
		t.Errorf("NullValue map value should be z.null(), got %q", got)
	}
}

func TestTsOneofVariantZodType_NullValue(t *testing.T) {
	v := &OneofVariant{
		Kind:         FieldKindEnum,
		TypeName:     "NullValue",
		EnumFullName: "google.protobuf.NullValue",
	}
	opts := &Options{}
	got := tsOneofVariantZodType(v, opts)
	if got != "z.null()" {
		t.Errorf("NullValue oneof variant should be z.null(), got %q", got)
	}
}

func TestTsRepeatedConstraints_MinMax(t *testing.T) {
	min := uint64(1)
	max := uint64(5)
	vc := &ValidateConstraints{MinItems: &min, MaxItems: &max}
	got := tsRepeatedConstraints(vc)
	if !strings.Contains(got, ".min(1)") {
		t.Errorf("missing min check: %s", got)
	}
	if !strings.Contains(got, ".max(5)") {
		t.Errorf("missing max check: %s", got)
	}
}

func TestHasNestedQuantifiers(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		// Dangerous: nested quantifiers cause ReDoS in JS.
		{`(a+)+`, true},
		{`(a*)*`, true},
		{`(a+)*`, true},
		{`(a*)+`, true},
		{`(a+b+)+`, true},
		{`([a-z]+)+`, true},
		{`(a{2,})+`, true},
		{`((a+))+`, true},
		{`(?:a+)+`, true}, // Non-capturing group — Simplify() would hide this.

		// Safe: single-level quantifiers.
		{`a+`, false},
		{`[a-z]+`, false},
		{`(abc)+`, false}, // No inner quantifier.
		{`a+b+c+`, false}, // Siblings, not nested.
		{`^[a-z]+$`, false},
		{`\d{3}-\d{4}`, false},
		{`(a|b)+`, false},                    // Alternation under quantifier, no nested quantifier.
		{`^[a-zA-Z0-9+/\-_]*={0,2}$`, false}, // base64 regex.
		{`(a{2})+`, false},                   // Fixed repeat inside quantifier — not dangerous.
		{`(a{2,3})+`, false},                 // Bounded repeat — finite expansion, safe.
		// Adjacent unbounded quantifiers.
		{`.*.+`, true},    // Dot-star + dot-plus — overlapping.
		{`\w+\d+`, true},  // Overlapping char classes.
		{`a+b+`, false},   // Distinct literals — safe.
		{`a+b+c+`, false}, // Multiple distinct literals — safe.
		// Alternation under quantifier.
		{`(ab|cd)+`, false}, // Disjoint multi-char branches — safe.
	}
	for _, tt := range tests {
		got := hasDangerousPattern(tt.pattern)
		if got != tt.want {
			t.Errorf("hasDangerousPattern(%q) = %v, want %v", tt.pattern, got, tt.want)
		}
	}
}

func TestTsZodConstraints_PatternNestedQuantifier(t *testing.T) {
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.StringKind,
		ValidateConstraints: &ValidateConstraints{
			Pattern: "(a+)+",
		},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	if !strings.Contains(got, "WARN") || !strings.Contains(got, "dangerous quantifiers") {
		t.Errorf("dangerous pattern should emit WARN, got: %s", got)
	}
	if strings.Contains(got, "new RegExp") {
		t.Error("nested quantifier pattern should NOT emit RegExp")
	}
}

func TestTsZodConstraints_PatternSafeQuantifier(t *testing.T) {
	f := &DomainField{
		Kind:       FieldKindScalar,
		ScalarKind: protoreflect.StringKind,
		ValidateConstraints: &ValidateConstraints{
			Pattern: `^[a-z]+$`,
		},
	}
	opts := &Options{}
	got := tsZodConstraints(f, opts)
	if !strings.Contains(got, "new RegExp") {
		t.Errorf("safe pattern should emit RegExp, got: %s", got)
	}
	if strings.Contains(got, "WARN") {
		t.Error("safe pattern should NOT emit WARN")
	}
}
