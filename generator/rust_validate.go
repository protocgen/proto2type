package generator

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
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
func rustValidateAttrs(messageName string, f *DomainField) []string {
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

	// IgnoreEmpty: if set, format and range checks must be routed to custom
	// validation to add a zero-value guard, since validator derive attributes
	// don't support skip-on-empty.
	if vc.IgnoreEmpty {
		// Route all format/range/length checks to custom function
		if rustFieldHasIgnoreEmptyConstraints(vc) {
			funcName := rustCustomValidateFuncName(messageName, f)
			attrs = append(attrs, fmt.Sprintf(`custom(function = "%s")`, funcName))
			return attrs
		}
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

	// Prefix/suffix/contains/const/in/not_in and timestamp range → custom validation function
	if rustFieldHasCustomConstraints(f) {
		funcName := rustCustomValidateFuncName(messageName, f)
		attrs = append(attrs, fmt.Sprintf(`custom(function = "%s")`, funcName))
	}

	// Numeric bounds: gt/gte/lt/lte
	if vc.Gt != nil || vc.Gte != nil || vc.Lt != nil || vc.Lte != nil {
		if f.Kind == FieldKindTimestamp {
			// Handled by custom validation function above
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

// rustFieldHasIgnoreEmptyConstraints returns true if the field has constraints
// that need to be wrapped in a zero-value guard when IgnoreEmpty is set.
func rustFieldHasIgnoreEmptyConstraints(vc *ValidateConstraints) bool {
	return vc.Email || vc.URI || vc.UUID || vc.Hostname || vc.IP ||
		vc.Pattern != "" || vc.MinLength != nil || vc.MaxLength != nil ||
		vc.Gte != nil || vc.Gt != nil || vc.Lte != nil || vc.Lt != nil ||
		vc.MinItems != nil || vc.MaxItems != nil ||
		vc.DefinedOnly
}

// rustFieldHasCustomConstraints returns true if the field has constraints that
// can't be expressed via validator derive attributes (prefix/suffix/contains/
// const/in/not_in, or timestamp range bounds).
func rustFieldHasCustomConstraints(f *DomainField) bool {
	vc := f.ValidateConstraints
	if vc == nil {
		return false
	}
	if vc.Prefix != "" || vc.Suffix != "" || vc.Contains != "" {
		return true
	}
	if vc.Const != nil || len(vc.In) > 0 || len(vc.NotIn) > 0 {
		return true
	}
	if vc.DefinedOnly && f.Kind == FieldKindEnum && f.EnumAsString {
		return true
	}
	if vc.IgnoreEmpty && rustFieldHasIgnoreEmptyConstraints(vc) {
		return true
	}
	if f.Kind == FieldKindTimestamp && (vc.Gt != nil || vc.Gte != nil || vc.Lt != nil || vc.Lte != nil) {
		return true
	}
	return false
}

// rustCustomValidateFuncName returns the function name for a field's custom validator.
// Includes the message name to prevent collisions across messages.
func rustCustomValidateFuncName(messageName string, f *DomainField) string {
	return fmt.Sprintf("validate_%s_%s", toSnakeCase(messageName), toSnakeCase(f.Name))
}

// rustEmitCustomValidateFuncs emits custom validation functions for fields in a
// DomainMessage that have non-derive constraints. These functions are referenced
// by #[validate(custom(function = "..."))] attributes.
func rustEmitCustomValidateFuncs(g *protogen.GeneratedFile, dm *DomainMessage, df *DomainFile) {
	for _, f := range dm.Fields {
		if f.Computed != nil {
			continue
		}
		if f.IsOneof || !rustFieldHasCustomConstraints(f) {
			continue
		}
		vc := f.ValidateConstraints
		funcName := rustCustomValidateFuncName(dm.Name, f)

		// Determine the Rust type for the parameter
		isString := f.Kind == FieldKindScalar && (f.ScalarKind == protoreflect.StringKind || f.ScalarKind == protoreflect.BytesKind)
		paramType := "String"
		if f.Kind == FieldKindTimestamp {
			paramType = "chrono::DateTime<chrono::Utc>"
		} else if !isString {
			paramType = rustType(f.ScalarKind)
		}

		g.P("fn ", funcName, "(value: &", paramType, ") -> Result<(), validator::ValidationError> {")

		if vc.IgnoreEmpty {
			if isString {
				g.P("    if value.is_empty() {")
			} else if f.Kind == FieldKindTimestamp {
				// No zero-value guard for timestamps
			} else {
				g.P("    if *value == 0 {")
			}
			if f.Kind != FieldKindTimestamp {
				g.P("        return Ok(());")
				g.P("    }")
			}
		}

		if vc.IgnoreEmpty && vc.Email {
			g.P(`    if !validator::ValidateEmail::validate_email(value) {`)
			g.P(`        return Err(validator::ValidationError::new("email"));`)
			g.P("    }")
		}
		if vc.IgnoreEmpty && vc.URI {
			g.P(`    if !validator::ValidateUrl::validate_url(value) {`)
			g.P(`        return Err(validator::ValidationError::new("url"));`)
			g.P("    }")
		}

		// Re-emit constraints that were suppressed from derive attrs by IgnoreEmpty routing.
		if vc.IgnoreEmpty {
			if vc.MinLength != nil || vc.MaxLength != nil {
				if vc.MinLength != nil {
					g.P(fmt.Sprintf("    if value.chars().count() < %d {", *vc.MinLength))
					g.P(`        return Err(validator::ValidationError::new("length"));`)
					g.P("    }")
				}
				if vc.MaxLength != nil {
					g.P(fmt.Sprintf("    if value.chars().count() > %d {", *vc.MaxLength))
					g.P(`        return Err(validator::ValidationError::new("length"));`)
					g.P("    }")
				}
			}
			if vc.Pattern != "" {
				constName := rustRegexConstName(f, "PATTERN")
				g.P("    if !", constName, ".is_match(value) {")
				g.P(`        return Err(validator::ValidationError::new("regex"));`)
				g.P("    }")
			}
			if vc.UUID {
				constName := rustRegexConstName(f, "UUID")
				g.P("    if !", constName, ".is_match(value) {")
				g.P(`        return Err(validator::ValidationError::new("regex"));`)
				g.P("    }")
			}
			if f.Kind != FieldKindTimestamp && (vc.Gt != nil || vc.Gte != nil || vc.Lt != nil || vc.Lte != nil) {
				formatBound := func(val string) string {
					if f.Kind == FieldKindDuration && strings.HasSuffix(val, "s") {
						return strings.TrimSuffix(val, "s")
					}
					return val
				}
				if vc.Gt != nil {
					g.P(fmt.Sprintf("    if *value <= %s {", formatBound(*vc.Gt)))
					g.P(`        return Err(validator::ValidationError::new("range"));`)
					g.P("    }")
				}
				if vc.Gte != nil {
					g.P(fmt.Sprintf("    if *value < %s {", formatBound(*vc.Gte)))
					g.P(`        return Err(validator::ValidationError::new("range"));`)
					g.P("    }")
				}
				if vc.Lt != nil {
					g.P(fmt.Sprintf("    if *value >= %s {", formatBound(*vc.Lt)))
					g.P(`        return Err(validator::ValidationError::new("range"));`)
					g.P("    }")
				}
				if vc.Lte != nil {
					g.P(fmt.Sprintf("    if *value > %s {", formatBound(*vc.Lte)))
					g.P(`        return Err(validator::ValidationError::new("range"));`)
					g.P("    }")
				}
			}
			if vc.Hostname {
				g.P(`    let hostname_re = regex::Regex::new(r"^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$").unwrap();`)
				g.P("    if !hostname_re.is_match(value) {")
				g.P(`        return Err(validator::ValidationError::new("hostname"));`)
				g.P("    }")
			}
			if vc.IP {
				g.P(`    if value.parse::<std::net::IpAddr>().is_err() {`)
				g.P(`        return Err(validator::ValidationError::new("ip"));`)
				g.P("    }")
			}
		}

		// String constraints (only valid for string fields)
		if isString {
			if vc.Prefix != "" {
				g.P(`    if !value.starts_with("`, rustEscapeString(vc.Prefix), `") {`)
				g.P(`        return Err(validator::ValidationError::new("prefix"));`)
				g.P("    }")
			}
			if vc.Suffix != "" {
				g.P(`    if !value.ends_with("`, rustEscapeString(vc.Suffix), `") {`)
				g.P(`        return Err(validator::ValidationError::new("suffix"));`)
				g.P("    }")
			}
			if vc.Contains != "" {
				g.P(`    if !value.contains("`, rustEscapeString(vc.Contains), `") {`)
				g.P(`        return Err(validator::ValidationError::new("contains"));`)
				g.P("    }")
			}
		}

		// Const — works for both string and numeric
		if vc.Const != nil {
			if isString {
				g.P("    if value != ", *vc.Const, " {")
			} else {
				g.P("    if *value != ", *vc.Const, " {")
			}
			g.P(`        return Err(validator::ValidationError::new("const"));`)
			g.P("    }")
		}

		// In/NotIn — type-specific matching
		if len(vc.In) > 0 {
			if isString {
				g.P("    if ![", strings.Join(vc.In, ", "), "].contains(&value.as_str()) {")
			} else {
				g.P("    if ![", strings.Join(vc.In, ", "), "].contains(value) {")
			}
			g.P(`        return Err(validator::ValidationError::new("in"));`)
			g.P("    }")
		}
		if len(vc.NotIn) > 0 {
			if isString {
				g.P("    if [", strings.Join(vc.NotIn, ", "), "].contains(&value.as_str()) {")
			} else {
				g.P("    if [", strings.Join(vc.NotIn, ", "), "].contains(value) {")
			}
			g.P(`        return Err(validator::ValidationError::new("not_in"));`)
			g.P("    }")
		}

		// DefinedOnly: enum value must be a defined constant.
		if vc.DefinedOnly && f.Kind == FieldKindEnum && f.EnumAsString {
			enumDef := findEnumByName(df, f.EnumTypeName)
			if enumDef != nil {
				seen := make(map[string]bool)
				var names []string
				for _, ev := range enumDef.Values {
					if !seen[ev.Name] {
						seen[ev.Name] = true
						names = append(names, `"`+ev.Name+`"`)
					}
				}
				g.P("    if ![" + strings.Join(names, ", ") + "].contains(&value.as_str()) {")
				g.P(`        return Err(validator::ValidationError::new("defined_only"));`)
				g.P("    }")
			} else {
				g.P("    // NOTE: defined_only check for ", f.Name, " skipped (enum ", f.EnumTypeName, " not in this file)")
			}
		}

		// Timestamp range constraints
		if f.Kind == FieldKindTimestamp {
			if vc.Gte != nil {
				g.P(`    if *value < chrono::DateTime::parse_from_rfc3339("`, *vc.Gte, `").unwrap().with_timezone(&chrono::Utc) {`)
				g.P(`        return Err(validator::ValidationError::new("timestamp_range"));`)
				g.P("    }")
			}
			if vc.Gt != nil {
				g.P(`    if *value <= chrono::DateTime::parse_from_rfc3339("`, *vc.Gt, `").unwrap().with_timezone(&chrono::Utc) {`)
				g.P(`        return Err(validator::ValidationError::new("timestamp_range"));`)
				g.P("    }")
			}
			if vc.Lte != nil {
				g.P(`    if *value > chrono::DateTime::parse_from_rfc3339("`, *vc.Lte, `").unwrap().with_timezone(&chrono::Utc) {`)
				g.P(`        return Err(validator::ValidationError::new("timestamp_range"));`)
				g.P("    }")
			}
			if vc.Lt != nil {
				g.P(`    if *value >= chrono::DateTime::parse_from_rfc3339("`, *vc.Lt, `").unwrap().with_timezone(&chrono::Utc) {`)
				g.P(`        return Err(validator::ValidationError::new("timestamp_range"));`)
				g.P("    }")
			}
		}

		g.P("    Ok(())")
		g.P("}")
		g.P()
	}
}

// rustEscapeString escapes special characters for embedding in Rust string literals.
func rustEscapeString(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return r.Replace(s)
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
		if f.Computed != nil {
			continue
		}
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
