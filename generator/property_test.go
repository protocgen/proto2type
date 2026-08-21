package generator

import (
	"fmt"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"hegel.dev/go/hegel"
)

// Property: hasNestedQuantifiers never panics on arbitrary strings.
func TestProperty_HasNestedQuantifiers_NeverPanics(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		pattern := hegel.Draw(ht, hegel.Text().MaxSize(200))
		// Must not panic — result is irrelevant.
		_ = hasNestedQuantifiers(pattern)
	})
}

// Property: hasNestedQuantifiers is deterministic (same input → same output).
func TestProperty_HasNestedQuantifiers_Deterministic(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		pattern := hegel.Draw(ht, hegel.Text().MaxSize(100))
		a := hasNestedQuantifiers(pattern)
		b := hasNestedQuantifiers(pattern)
		if a != b {
			ht.Fatalf("non-deterministic: %q returned %v then %v", pattern, a, b)
		}
	})
}

// genProtoPath builds a structured proto path like "a/b/c/file.proto"
// from random path segments.
func genProtoPath(ht *hegel.T) string {
	depth := hegel.Draw(ht, hegel.Integers(0, 4))
	parts := make([]string, 0, depth+1)
	for i := 0; i < depth; i++ {
		seg := hegel.Draw(ht, hegel.Text().MinSize(1).MaxSize(10))
		// Filter to alphanumeric segments only for valid paths.
		clean := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				return r
			}
			return -1
		}, seg)
		if clean == "" {
			clean = "dir"
		}
		parts = append(parts, clean)
	}
	file := hegel.Draw(ht, hegel.Text().MinSize(1).MaxSize(10))
	cleanFile := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, file)
	if cleanFile == "" {
		cleanFile = "msg"
	}
	parts = append(parts, cleanFile+".proto")
	return strings.Join(parts, "/")
}

// Property: tsImportPath always produces relative paths (starts with ./ or ../).
func TestProperty_TsImportPath_AlwaysRelative(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		from := genProtoPath(ht)
		to := genProtoPath(ht)
		opts := &Options{}
		result := tsImportPath(from, to, opts)
		if result == "" {
			ht.Fatalf("empty import path for from=%q to=%q", from, to)
		}
		if !strings.HasPrefix(result, "./") && !strings.HasPrefix(result, "../") {
			ht.Fatalf("import path %q does not start with ./ or ../ (from=%q to=%q)", result, from, to)
		}
	})
}

// Property: tsImportPath result always ends with the expected suffix.
func TestProperty_TsImportPath_CorrectSuffix(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		from := genProtoPath(ht)
		to := genProtoPath(ht)
		opts := &Options{}
		result := tsImportPath(from, to, opts)
		if !strings.HasSuffix(result, ".type.js") {
			ht.Fatalf("import path %q does not end with .type.js (from=%q to=%q)", result, from, to)
		}
	})
}

// Property: tsZodConstraints never panics on arbitrary constraint combos.
func TestProperty_TsZodConstraints_NeverPanics(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		minLen := uint64(hegel.Draw(ht, hegel.Integers(0, 1000)))
		maxLen := uint64(hegel.Draw(ht, hegel.Integers(0, 1000)))
		pattern := hegel.Draw(ht, hegel.Text().MaxSize(50))
		prefix := hegel.Draw(ht, hegel.Text().MaxSize(20))
		suffix := hegel.Draw(ht, hegel.Text().MaxSize(20))

		f := &DomainField{
			Kind:       FieldKindScalar,
			ScalarKind: protoreflect.StringKind,
			ValidateConstraints: &ValidateConstraints{
				MinLength: &minLen,
				MaxLength: &maxLen,
				Pattern:   pattern,
				Prefix:    prefix,
				Suffix:    suffix,
				Email:     hegel.Draw(ht, hegel.Booleans()),
				UUID:      hegel.Draw(ht, hegel.Booleans()),
				URI:       hegel.Draw(ht, hegel.Booleans()),
				Hostname:  hegel.Draw(ht, hegel.Booleans()),
				IP:        hegel.Draw(ht, hegel.Booleans()),
			},
		}
		opts := &Options{}
		// Must not panic.
		_ = tsZodConstraints(f, opts)
	})
}

// Property: tsScalarDefault never returns empty for any scalar kind.
func TestProperty_TsScalarDefault_NeverEmpty(t *testing.T) {
	kinds := []protoreflect.Kind{
		protoreflect.BoolKind,
		protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind,
		protoreflect.FloatKind, protoreflect.DoubleKind,
		protoreflect.StringKind, protoreflect.BytesKind,
	}
	hegel.Test(t, func(ht *hegel.T) {
		idx := hegel.Draw(ht, hegel.Integers(0, len(kinds)-1))
		bigint := hegel.Draw(ht, hegel.Booleans())
		style := "string"
		if bigint {
			style = "bigint"
		}
		opts := &Options{TSInt64Style: style}
		result := tsScalarDefault(kinds[idx], opts)
		if result == "" {
			ht.Fatalf("empty default for kind=%v style=%s", kinds[idx], style)
		}
	})
}

// Property: tsMapConstraints output correctly reflects each requested bound.
func TestProperty_TsMapConstraints_ConsistentOutput(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		minItems := uint64(hegel.Draw(ht, hegel.Integers(0, 100)))
		maxItems := uint64(hegel.Draw(ht, hegel.Integers(0, 100)))
		hasMin := hegel.Draw(ht, hegel.Booleans())
		hasMax := hegel.Draw(ht, hegel.Booleans())

		vc := &ValidateConstraints{}
		if hasMin {
			vc.MinItems = &minItems
		}
		if hasMax {
			vc.MaxItems = &maxItems
		}

		result := tsMapConstraints(vc)
		if !hasMin && !hasMax {
			if result != "" {
				ht.Fatalf("expected empty with no constraints, got %q", result)
			}
			return
		}
		if !strings.Contains(result, ".refine") {
			ht.Fatalf("expected .refine for min=%v max=%v, got %q", hasMin, hasMax, result)
		}
		// Assert specific bound text is present when requested.
		if hasMin {
			expected := fmt.Sprintf(">= %d", minItems)
			if !strings.Contains(result, expected) {
				ht.Fatalf("min bound %d not found in %q", minItems, result)
			}
		}
		if hasMax {
			expected := fmt.Sprintf("<= %d", maxItems)
			if !strings.Contains(result, expected) {
				ht.Fatalf("max bound %d not found in %q", maxItems, result)
			}
		}
	})
}

// Property: tsNumericConstraints never panics on arbitrary bounds.
func TestProperty_TsNumericConstraints_NeverPanics(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		gt := hegel.Draw(ht, hegel.Text().MaxSize(20))
		lt := hegel.Draw(ht, hegel.Text().MaxSize(20))

		f := &DomainField{
			Kind:       FieldKindScalar,
			ScalarKind: protoreflect.Int32Kind,
			ValidateConstraints: &ValidateConstraints{
				Gt: &gt,
				Lt: &lt,
				In: []string{
					hegel.Draw(ht, hegel.Text().MaxSize(10)),
					hegel.Draw(ht, hegel.Text().MaxSize(10)),
				},
			},
		}
		opts := &Options{}
		_ = tsZodConstraints(f, opts)
	})
}
