package generator

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

// rustValidateAttrs returns the validator attribute arguments for a field.
// Returns nil if the field has no validation constraints.
//
// NOTE: length validation counts characters (Unicode scalar values), not bytes.
// This differs from proto's min_len/max_len which count bytes, but matches
// user expectations and is consistent with Python/Pydantic.
func rustValidateAttrs(f *DomainField) []string {
	var attrs []string

	// Nested message validation (recursive)
	if f.Kind == FieldKindMessage && !f.IsMap && !f.IsOneof {
		attrs = append(attrs, "nested")
	}

	vc := f.ValidateConstraints
	if vc == nil {
		return attrs
	}

	// Required (for Option<T> fields)
	if vc.Required && f.Optional {
		attrs = append(attrs, "required")
	}

	// Email
	if vc.Email {
		attrs = append(attrs, "email")
	}

	// URI → url validator
	if vc.URI {
		attrs = append(attrs, "url")
	}

	// String length: min_len / max_len
	if vc.MinLength != nil || vc.MaxLength != nil {
		var parts []string
		if vc.MinLength != nil {
			parts = append(parts, fmt.Sprintf("min = %d", *vc.MinLength))
		}
		if vc.MaxLength != nil {
			parts = append(parts, fmt.Sprintf("max = %d", *vc.MaxLength))
		}
		attrs = append(attrs, fmt.Sprintf("length(%s)", strings.Join(parts, ", ")))
	}

	// Pattern → regex validator with lazy_static path
	if vc.Pattern != "" {
		constName := rustRegexConstName(f, "PATTERN")
		attrs = append(attrs, fmt.Sprintf("regex(path = *%s)", constName))
	}

	// UUID → regex validator
	if vc.UUID {
		constName := rustRegexConstName(f, "UUID")
		attrs = append(attrs, fmt.Sprintf("regex(path = *%s)", constName))
	}

	// Numeric bounds: gt/gte/lt/lte
	if vc.Gt != nil || vc.Gte != nil || vc.Lt != nil || vc.Lte != nil {
		var parts []string
		if vc.Gt != nil {
			parts = append(parts, fmt.Sprintf("exclusive_min = %s", *vc.Gt))
		}
		if vc.Gte != nil {
			parts = append(parts, fmt.Sprintf("min = %s", *vc.Gte))
		}
		if vc.Lt != nil {
			parts = append(parts, fmt.Sprintf("exclusive_max = %s", *vc.Lt))
		}
		if vc.Lte != nil {
			parts = append(parts, fmt.Sprintf("max = %s", *vc.Lte))
		}
		attrs = append(attrs, fmt.Sprintf("range(%s)", strings.Join(parts, ", ")))
	}

	// Repeated min/max items
	if vc.MinItems != nil || vc.MaxItems != nil {
		var parts []string
		if vc.MinItems != nil {
			parts = append(parts, fmt.Sprintf("min = %d", *vc.MinItems))
		}
		if vc.MaxItems != nil {
			parts = append(parts, fmt.Sprintf("max = %d", *vc.MaxItems))
		}
		attrs = append(attrs, fmt.Sprintf("length(%s)", strings.Join(parts, ", ")))
	}

	return attrs
}

// rustRegexConstName generates a unique constant name for a regex pattern.
func rustRegexConstName(f *DomainField, suffix string) string {
	return fmt.Sprintf("RE_%s_%s", strings.ToUpper(toSnakeCase(f.Name)), suffix)
}

// rustEmitRegexConstants emits lazy_static! blocks for any fields in the message
// that need compiled regex patterns (pattern constraints or UUID validation).
func rustEmitRegexConstants(g *protogen.GeneratedFile, dm *DomainMessage) {
	var regexes []struct {
		name    string
		pattern string
	}

	for _, f := range dm.Fields {
		vc := f.ValidateConstraints
		if vc == nil {
			continue
		}
		if vc.Pattern != "" {
			regexes = append(regexes, struct {
				name    string
				pattern string
			}{
				name:    rustRegexConstName(f, "PATTERN"),
				pattern: vc.Pattern,
			})
		}
		if vc.UUID {
			regexes = append(regexes, struct {
				name    string
				pattern string
			}{
				name:    rustRegexConstName(f, "UUID"),
				pattern: `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
			})
		}
	}

	if len(regexes) == 0 {
		return
	}

	g.P("lazy_static! {")
	for _, r := range regexes {
		g.P("    static ref ", r.name, `: Regex = Regex::new(r"`, r.pattern, `").unwrap();`)
	}
	g.P("}")
	g.P()
}
