package generator

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

// rustRawStringDelimiter returns the appropriate number of # characters for a Rust raw string.
// It counts the maximum consecutive '#' characters in the pattern and returns that count + 1.
func rustRawStringDelimiter(pattern string) string {
	maxHash := 0
	currentHash := 0
	for _, c := range pattern {
		if c == '#' {
			currentHash++
			if currentHash > maxHash {
				maxHash = currentHash
			}
		} else {
			currentHash = 0
		}
	}
	return strings.Repeat("#", maxHash+1)
}

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

	// TODO: prefix/suffix/contains/const/in/not_in constraints not yet emittable via validator derive

	// Numeric bounds: gt/gte/lt/lte
	if vc.Gt != nil || vc.Gte != nil || vc.Lt != nil || vc.Lte != nil {
		if f.Kind == FieldKindTimestamp {
			// Skip for now, comment emitted in rust_domain.go
		} else {
			var parts []string
			// formatBound converts proto duration/timestamp bound values to Rust numeric literals.
			// Duration bounds come as "1.5s" — we strip the "s" suffix since Rust's validator
			// range attribute accepts numeric literals directly.
			formatBound := func(val string) string {
				if f.Kind == FieldKindDuration && strings.HasSuffix(val, "s") {
					return strings.TrimSuffix(val, "s")
				}
				return val
			}

			if vc.Gt != nil {
				parts = append(parts, fmt.Sprintf("exclusive_min = %s", formatBound(*vc.Gt)))
			}
			if vc.Gte != nil {
				parts = append(parts, fmt.Sprintf("min = %s", formatBound(*vc.Gte)))
			}
			if vc.Lt != nil {
				parts = append(parts, fmt.Sprintf("exclusive_max = %s", formatBound(*vc.Lt)))
			}
			if vc.Lte != nil {
				parts = append(parts, fmt.Sprintf("max = %s", formatBound(*vc.Lte)))
			}
			attrs = append(attrs, fmt.Sprintf("range(%s)", strings.Join(parts, ", ")))
		}
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

	for _, do := range dm.Oneofs {
		for _, v := range do.Variants {
			vc := v.ValidateConstraints
			if vc == nil {
				continue
			}
			if vc.Pattern != "" {
				regexes = append(regexes, struct {
					name    string
					pattern string
				}{
					name:    fmt.Sprintf("RE_%s_%s_%s", strings.ToUpper(toSnakeCase(do.Name)), strings.ToUpper(toSnakeCase(v.ProtoName)), "PATTERN"),
					pattern: vc.Pattern,
				})
			}
			if vc.UUID {
				regexes = append(regexes, struct {
					name    string
					pattern string
				}{
					name:    fmt.Sprintf("RE_%s_%s_%s", strings.ToUpper(toSnakeCase(do.Name)), strings.ToUpper(toSnakeCase(v.ProtoName)), "UUID"),
					pattern: `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
				})
			}
		}
	}

	if len(regexes) == 0 {
		return
	}

	g.P("lazy_static! {")
	for _, r := range regexes {
		delim := rustRawStringDelimiter(r.pattern)
		g.P("    static ref ", r.name, ": Regex = Regex::new(r", delim, `"`, r.pattern, `"`, delim, ").unwrap();")
	}
	g.P("}")
	g.P()
}
