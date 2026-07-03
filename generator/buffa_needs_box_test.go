package generator

import (
	"strings"
	"testing"
)

// TestBuffaNeedsBox_Integration verifies that the buffa backend respects
// NeedsBox for singular message fields. Non-boxed fields should NOT use
// as_ref().into() or Box::new(), while boxed fields SHOULD.
func TestBuffaNeedsBox_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)

	// user.proto has Address — required, non-recursive, NeedsBox=false
	t.Run("non_boxed_required_message_field", func(t *testing.T) {
		gen := newPlugin(t, fds, []string{"user.proto"})
		opts := &Options{
			Lang:      "rust",
			Backend:   "buffa",
			BufModule: "crate::proto",
		}

		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			if err := generateRustBuffa(gen, f, opts); err != nil {
				t.Fatalf("generateRustBuffa: %v", err)
			}
		}

		src := extractBuffaOutput(t, gen)

		// Domain → Buffa: should use (&d.address).into(), NOT (&*d.address).into()
		if !strings.Contains(src, `(&d.address).into()`) {
			t.Error("expected non-boxed d→b conversion: (&d.address).into()")
		}
		if strings.Contains(src, `(&*d.address).into()`) {
			t.Error("should NOT contain boxed d→b conversion: (&*d.address).into()")
		}

		// Buffa → Domain: should NOT use Box::new() for address
		if strings.Contains(src, `Box::new(b.address`) {
			t.Error("should NOT contain Box::new(b.address...) for non-boxed field")
		}
		// Should use ok_or(MissingRequiredField) without Box
		if !strings.Contains(src, `b.address.as_option().ok_or(ConversionError::MissingRequiredField("address"))?.try_into()?`) {
			t.Error("expected non-boxed b→d conversion with MissingRequiredField for address")
		}
	})
}

// TestBuffaNeedsBox_ExprFunctions directly tests the expression generators
// with synthetic DomainField values to cover both NeedsBox=true and
// NeedsBox=false for optional and required singular message fields.
func TestBuffaNeedsBox_ExprFunctions(t *testing.T) {
	// Helper to build a minimal message field.
	makeField := func(optional, needsBox bool) *DomainField {
		return &DomainField{
			Name:     "payload",
			Kind:     FieldKindMessage,
			Optional: optional,
			Repeated: false,
			NeedsBox: needsBox,
		}
	}

	t.Run("domain_to_buffa", func(t *testing.T) {
		t.Run("required_not_boxed", func(t *testing.T) {
			f := makeField(false, false)
			expr := rustBuffaDomainToBufExpr(f, "payload")
			if strings.Contains(expr, "&*d.") {
				t.Errorf("required non-boxed should NOT deref Box: got %q", expr)
			}
			if !strings.Contains(expr, "(&d.payload).into()") {
				t.Errorf("expected (&d.payload).into(), got %q", expr)
			}
		})

		t.Run("required_boxed", func(t *testing.T) {
			f := makeField(false, true)
			expr := rustBuffaDomainToBufExpr(f, "payload")
			if !strings.Contains(expr, "(&*d.payload).into()") {
				t.Errorf("expected (&*d.payload).into() for boxed required, got %q", expr)
			}
		})

		t.Run("optional_not_boxed", func(t *testing.T) {
			f := makeField(true, false)
			expr := rustBuffaDomainToBufExpr(f, "payload")
			if strings.Contains(expr, "as_ref().into()") {
				t.Errorf("optional non-boxed should NOT use as_ref(): got %q", expr)
			}
			if !strings.Contains(expr, "v.into()") {
				t.Errorf("expected v.into() for non-boxed optional, got %q", expr)
			}
		})

		t.Run("optional_boxed", func(t *testing.T) {
			f := makeField(true, true)
			expr := rustBuffaDomainToBufExpr(f, "payload")
			if !strings.Contains(expr, "v.as_ref().into()") {
				t.Errorf("expected v.as_ref().into() for boxed optional, got %q", expr)
			}
		})
	})

	t.Run("buffa_to_domain", func(t *testing.T) {
		t.Run("required_not_boxed", func(t *testing.T) {
			f := makeField(false, false)
			expr := rustBuffaBufToDomainExpr(f, "payload")
			if strings.Contains(expr, "Box::new") {
				t.Errorf("required non-boxed should NOT use Box::new: got %q", expr)
			}
			if !strings.Contains(expr, `ok_or(ConversionError::MissingRequiredField("payload"))`) {
				t.Errorf("expected MissingRequiredField error, got %q", expr)
			}
		})

		t.Run("required_boxed", func(t *testing.T) {
			f := makeField(false, true)
			expr := rustBuffaBufToDomainExpr(f, "payload")
			if !strings.Contains(expr, "Box::new") {
				t.Errorf("expected Box::new for boxed required, got %q", expr)
			}
			if !strings.Contains(expr, `ok_or(ConversionError::MissingRequiredField("payload"))`) {
				t.Errorf("expected MissingRequiredField error, got %q", expr)
			}
		})

		t.Run("optional_not_boxed", func(t *testing.T) {
			f := makeField(true, false)
			expr := rustBuffaBufToDomainExpr(f, "payload")
			if strings.Contains(expr, "Box::new") {
				t.Errorf("optional non-boxed should NOT use Box::new: got %q", expr)
			}
			if !strings.Contains(expr, "Some(v.try_into()?)") {
				t.Errorf("expected Some(v.try_into()?) for non-boxed optional, got %q", expr)
			}
		})

		t.Run("optional_boxed", func(t *testing.T) {
			f := makeField(true, true)
			expr := rustBuffaBufToDomainExpr(f, "payload")
			if !strings.Contains(expr, "Box::new(v.try_into()?)") {
				t.Errorf("expected Box::new(v.try_into()?) for boxed optional, got %q", expr)
			}
		})
	})
}
