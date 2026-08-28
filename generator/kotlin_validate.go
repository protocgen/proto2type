package generator

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// generateKotlinValidate generates a validate() method on the Kotlin data class.
//
// When opts.Validate is true, generates a fun validate(): List<String> method that
// returns a list of validation error messages. An empty list means valid.
//
// For nullable/optional fields, constraint checks are wrapped in ?.let { } blocks
// to satisfy Kotlin null safety.
//
// NOTE: length validation counts characters (Unicode scalar values), not bytes.
// This differs from proto's min_len/max_len which count bytes, but matches
// user expectations and is consistent with Python/Pydantic and Rust/validator.
func generateKotlinValidate(g *protogen.GeneratedFile, df *DomainFile, msg *DomainMessage, opts *Options) {
	if !opts.ValidateEnabled() {
		return
	}

	kotlinEmitRegexConstants(g, msg)

	// Always generate validate() when opts.Validate is true, even if this
	// message has no direct constraints. This ensures nested validate() calls
	// (from parent messages) always resolve. Messages without constraints will
	// have a validate() that just returns emptyList().

	// Extension function: fun ClassName.validate(): List<String>
	g.P("/** Validates constraints from buf.validate annotations. Returns a list of error messages (empty = valid). */")
	g.P("fun ", msg.Name, ".validate(): List<String> {")
	g.P("    val errors = mutableListOf<String>()")

	for _, f := range msg.Fields {
		vc := f.ValidateConstraints
		if vc == nil || !vc.HasConstraints() {
			continue
		}

		fieldName := escapeKotlinKeyword(toCamelCase(f.Name))

		// Determine if this is a nullable/optional field that needs safe access.
		hasNonRequiredConstraints := vc.Email || vc.URI || vc.UUID || vc.Pattern != "" ||
			vc.MinLength != nil || vc.MaxLength != nil ||
			vc.Gte != nil || vc.Gt != nil || vc.Lte != nil || vc.Lt != nil ||
			vc.MinItems != nil || vc.MaxItems != nil ||
			(vc.DefinedOnly && f.Kind == FieldKindEnum && f.EnumAsString)

		indent := "    "

		// Required (for nullable fields) — always at top level.
		if vc.Required && f.Optional {
			g.P("    if (", fieldName, " == null) errors.add(\"", f.Name, " is required\")")
		}

		// For optional fields, wrap remaining checks in ?.let { } for null safety.
		if f.Optional && hasNonRequiredConstraints {
			g.P("    ", fieldName, "?.let { ", fieldName, " ->")
			indent = "        "
		}

		// IgnoreEmpty: skip non-required constraints when field is zero-value.
		emittedIgnoreGuard := false
		if vc.IgnoreEmpty && hasNonRequiredConstraints {
			if f.Optional {
				// Inside ?.let block — null is already handled, but zero-value (e.g. "") should also skip.
				switch {
				case f.Kind == FieldKindScalar && (f.ScalarKind == protoreflect.StringKind || f.ScalarKind == protoreflect.BytesKind):
					g.P(indent, "if (", fieldName, ".isNotEmpty()) {")
					emittedIgnoreGuard = true
				case f.Kind == FieldKindEnum && f.EnumAsString:
					g.P(indent, "if (", fieldName, ".isNotEmpty()) {")
					emittedIgnoreGuard = true
				}
				// Numeric/bool optionals: zero-value is semantically valid when explicitly set, skip guard.
			} else {
				emittedIgnoreGuard = true
				switch {
				case f.Kind == FieldKindScalar && (f.ScalarKind == protoreflect.StringKind || f.ScalarKind == protoreflect.BytesKind):
					g.P(indent, "if (", fieldName, ".isNotEmpty()) {")
				case f.Kind == FieldKindEnum && f.EnumAsString:
					g.P(indent, "if (", fieldName, ".isNotEmpty()) {")
				case f.Kind == FieldKindScalar && (f.ScalarKind == protoreflect.Int32Kind || f.ScalarKind == protoreflect.Sint32Kind || f.ScalarKind == protoreflect.Sfixed32Kind || f.ScalarKind == protoreflect.Uint32Kind || f.ScalarKind == protoreflect.Fixed32Kind):
					g.P(indent, "if (", fieldName, " != 0) {")
				case f.Kind == FieldKindScalar && (f.ScalarKind == protoreflect.Int64Kind || f.ScalarKind == protoreflect.Sint64Kind || f.ScalarKind == protoreflect.Sfixed64Kind || f.ScalarKind == protoreflect.Uint64Kind || f.ScalarKind == protoreflect.Fixed64Kind):
					g.P(indent, "if (", fieldName, " != 0L) {")
				case f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.FloatKind:
					g.P(indent, "if (", fieldName, " != 0.0f) {")
				case f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.DoubleKind:
					g.P(indent, "if (", fieldName, " != 0.0) {")
				case f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BoolKind:
					g.P(indent, "if (", fieldName, ") {")
				case f.Repeated:
					g.P(indent, "if (", fieldName, ".isNotEmpty()) {")
				default:
					emittedIgnoreGuard = false
				}
				if emittedIgnoreGuard {
					indent = indent + "    "
				}
			} // else (non-optional)
		}

		// Email
		if vc.Email {
			g.P(indent, "if (", fieldName, ".isNotEmpty() && !", fieldName, ".matches(RE_", strings.ToUpper(msg.Name), "_", strings.ToUpper(f.Name), "_EMAIL)) errors.add(\"", f.Name, " must be a valid email\")")
		}

		// URI
		if vc.URI {
			g.P(indent, "if (", fieldName, ".isNotEmpty() && !", fieldName, ".matches(RE_", strings.ToUpper(msg.Name), "_", strings.ToUpper(f.Name), "_URI)) errors.add(\"", f.Name, " must be a valid URI\")")
		}

		// UUID
		if vc.UUID {
			g.P(indent, "if (", fieldName, ".isNotEmpty() && !", fieldName, ".matches(RE_", strings.ToUpper(msg.Name), "_", strings.ToUpper(f.Name), "_UUID)) errors.add(\"", f.Name, " must be a valid UUID\")")
		}

		// Pattern
		if vc.Pattern != "" {
			escaped := escapeKotlinStringLiteral(vc.Pattern)
			g.P(indent, "if (!", fieldName, ".matches(RE_", strings.ToUpper(msg.Name), "_", strings.ToUpper(f.Name), "_PATTERN)) errors.add(\"", f.Name, " must match pattern: ", escaped, "\")")
		}

		// String length (min_len / max_len)
		// NOTE: counts characters, not bytes.
		if vc.MinLength != nil {
			g.P(indent, "if (", fieldName, ".codePointCount(0, ", fieldName, ".length) < ", *vc.MinLength, ") errors.add(\"", f.Name, " must be at least ", *vc.MinLength, " characters\")")
		}
		if vc.MaxLength != nil {
			g.P(indent, "if (", fieldName, ".codePointCount(0, ", fieldName, ".length) > ", *vc.MaxLength, ") errors.add(\"", f.Name, " must be at most ", *vc.MaxLength, " characters\")")
		}

		if vc.Prefix != "" {
			g.P(indent, "if (!", fieldName, ".startsWith(\"", vc.Prefix, "\")) errors.add(\"", f.Name, " must start with ", vc.Prefix, "\")")
		}
		if vc.Suffix != "" {
			g.P(indent, "if (!", fieldName, ".endsWith(\"", vc.Suffix, "\")) errors.add(\"", f.Name, " must end with ", vc.Suffix, "\")")
		}
		if vc.Contains != "" {
			g.P(indent, "if (!", fieldName, ".contains(\"", vc.Contains, "\")) errors.add(\"", f.Name, " must contain ", vc.Contains, "\")")
		}
		if vc.Const != nil {
			g.P(indent, "if (", fieldName, " != ", *vc.Const, ") errors.add(\"", f.Name, " must be exactly ", *vc.Const, "\")")
		}
		if len(vc.In) > 0 {
			vals := strings.Join(vc.In, ", ")
			g.P(indent, "if (", fieldName, " !in listOf(", vals, ")) errors.add(\"", f.Name, " must be one of [", vals, "]\")")
		}
		if len(vc.NotIn) > 0 {
			vals := strings.Join(vc.NotIn, ", ")
			g.P(indent, "if (", fieldName, " in listOf(", vals, ")) errors.add(\"", f.Name, " must not be one of [", vals, "]\")")
		}

		// Numeric range
		if vc.Gte != nil {
			if f.Kind == FieldKindTimestamp {
				g.P(indent, "if (", fieldName, ".compareTo(Instant.parse(\"", *vc.Gte, "\")) < 0) errors.add(\"", f.Name, " must be >= ", *vc.Gte, "\")")
			} else {
				val := *vc.Gte
				if f.Kind == FieldKindDuration {
					val = strings.TrimSuffix(val, "s")
				}
				g.P(indent, "if (", fieldName, " < ", val, ") errors.add(\"", f.Name, " must be >= ", *vc.Gte, "\")")
			}
		}
		if vc.Gt != nil {
			if f.Kind == FieldKindTimestamp {
				g.P(indent, "if (", fieldName, ".compareTo(Instant.parse(\"", *vc.Gt, "\")) <= 0) errors.add(\"", f.Name, " must be > ", *vc.Gt, "\")")
			} else {
				val := *vc.Gt
				if f.Kind == FieldKindDuration {
					val = strings.TrimSuffix(val, "s")
				}
				g.P(indent, "if (", fieldName, " <= ", val, ") errors.add(\"", f.Name, " must be > ", *vc.Gt, "\")")
			}
		}
		if vc.Lte != nil {
			if f.Kind == FieldKindTimestamp {
				g.P(indent, "if (", fieldName, ".compareTo(Instant.parse(\"", *vc.Lte, "\")) > 0) errors.add(\"", f.Name, " must be <= ", *vc.Lte, "\")")
			} else {
				val := *vc.Lte
				if f.Kind == FieldKindDuration {
					val = strings.TrimSuffix(val, "s")
				}
				g.P(indent, "if (", fieldName, " > ", val, ") errors.add(\"", f.Name, " must be <= ", *vc.Lte, "\")")
			}
		}
		if vc.Lt != nil {
			if f.Kind == FieldKindTimestamp {
				g.P(indent, "if (", fieldName, ".compareTo(Instant.parse(\"", *vc.Lt, "\")) >= 0) errors.add(\"", f.Name, " must be < ", *vc.Lt, "\")")
			} else {
				val := *vc.Lt
				if f.Kind == FieldKindDuration {
					val = strings.TrimSuffix(val, "s")
				}
				g.P(indent, "if (", fieldName, " >= ", val, ") errors.add(\"", f.Name, " must be < ", *vc.Lt, "\")")
			}
		}

		// Repeated min/max items
		if f.Repeated {
			if vc.MinItems != nil {
				g.P(indent, "if (", fieldName, ".size < ", *vc.MinItems, ") errors.add(\"", f.Name, " must have at least ", *vc.MinItems, " items\")")
			}
			if vc.MaxItems != nil {
				g.P(indent, "if (", fieldName, ".size > ", *vc.MaxItems, ") errors.add(\"", f.Name, " must have at most ", *vc.MaxItems, " items\")")
			}
		}

		// DefinedOnly: enum value must be a defined constant.
		if vc.DefinedOnly && f.Kind == FieldKindEnum {
			if f.EnumAsString {
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
					vals := strings.Join(names, ", ")
					g.P(indent, "if (", fieldName, " !in listOf(", vals, ")) errors.add(\"", f.Name, " must be a defined enum value\")")
				} else {
					g.P(indent, "// NOTE: defined_only check for ", f.Name, " skipped (enum ", f.EnumTypeName, " not in this file)")
				}
			}
			// Typed enum classes are guaranteed by Kotlin's type system — no runtime check needed.
		}

		// Close IgnoreEmpty guard.
		if emittedIgnoreGuard {
			// Remove the extra indent added above
			g.P(strings.TrimSuffix(indent, "    "), "}")
		}

		// Close ?.let block for optional fields.
		if f.Optional && hasNonRequiredConstraints {
			g.P("    }")
		}
	}

	// Nested message validation — propagate validate() to message-typed fields.
	// All proto3 message fields are nullable in Kotlin (Type? = null), so use safe calls.
	// Consistent with Rust's #[validate(nested)].
	for _, f := range msg.Fields {
		if f.Kind != FieldKindMessage || f.IsMap || f.IsOneof {
			continue
		}
		fieldName := escapeKotlinKeyword(toCamelCase(f.Name))
		if f.Repeated {
			g.P("    ", fieldName, ".forEachIndexed { i, v -> v.validate().forEach { e -> errors.add(\"", f.Name, "[$i].$e\") } }")
		} else {
			// Singular message fields are always nullable in Kotlin (Type? = null).
			g.P("    ", fieldName, "?.validate()?.let { errors.addAll(it.map { e -> \"", f.Name, ".$e\" }) }")
		}
	}
	// Oneof validation delegation — propagate validate() to oneof sealed classes.
	for _, o := range msg.Oneofs {
		fieldName := escapeKotlinKeyword(toCamelCase(o.FieldName))
		g.P("    ", fieldName, "?.validate()?.let { errors.addAll(it) }")
	}

	g.P("    return errors")
	g.P("}")
	g.P()

	// Also generate a throwing convenience method.
	g.P("/** Validates constraints and throws [IllegalStateException] if any fail. */")
	g.P("fun ", msg.Name, ".validateOrThrow() {")
	g.P("    val errors = validate()")
	g.P("    if (errors.isNotEmpty()) {")
	g.P(fmt.Sprintf("        throw IllegalStateException(%q + errors.joinToString(\"; \"))", msg.Name+": validation failed: "))
	g.P("    }")
	g.P("}")
	g.P()

	// Recursively generate validation for nested messages.
	for _, nested := range msg.NestedMessages {
		if !nested.Skip {
			generateKotlinValidate(g, df, nested, opts)
		}
	}

	// Generate validation for oneof sealed classes.
	for _, o := range msg.Oneofs {
		generateKotlinOneofValidate(g, df, o, opts)
	}
}

// generateKotlinOneofValidate generates validate() and validateOrThrow() extension
// functions for a oneof sealed class. For message variants, it propagates validation
// to the inner message. For scalar variants with constraints, it emits constraint checks.
func generateKotlinOneofValidate(g *protogen.GeneratedFile, df *DomainFile, o *DomainOneof, opts *Options) {
	g.P("/** Validates constraints on [", o.Name, "] variants. Returns a list of error messages (empty = valid). */")
	g.P("fun ", o.Name, ".validate(): List<String> {")
	g.P("    val errors = mutableListOf<String>()")
	g.P("    when (this) {")

	for _, v := range o.Variants {
		hasConstraints := v.ValidateConstraints != nil && v.ValidateConstraints.HasConstraints()
		if v.Kind == FieldKindMessage {
			g.P("        is ", o.Name, ".", v.Name, " -> {")
			g.P("            value.validate().let { errors.addAll(it.map { e -> \"", v.ProtoName, ".$e\" }) }")
			g.P("        }")
		} else if hasConstraints {
			g.P("        is ", o.Name, ".", v.Name, " -> {")
			emitKotlinOneofVariantConstraints(g, df, o, v)
			g.P("        }")
		}
	}
	// Add else branch for variants without constraints (Kotlin requires exhaustive when on sealed classes).
	g.P("        else -> {}")
	g.P("    }")
	g.P("    return errors")
	g.P("}")
	g.P()

	g.P("/** Validates constraints and throws [IllegalStateException] if any fail. */")
	g.P("fun ", o.Name, ".validateOrThrow() {")
	g.P("    val errors = validate()")
	g.P("    if (errors.isNotEmpty()) {")
	g.P(fmt.Sprintf("        throw IllegalStateException(%q + errors.joinToString(\"; \"))", o.Name+": validation failed: "))
	g.P("    }")
	g.P("}")
	g.P()
}

// emitKotlinOneofVariantConstraints emits constraint checks for a scalar oneof variant.
func emitKotlinOneofVariantConstraints(g *protogen.GeneratedFile, df *DomainFile, o *DomainOneof, v *OneofVariant) {
	vc := v.ValidateConstraints
	if vc == nil {
		return
	}
	if vc.Email {
		g.P("            if (value.isNotEmpty() && !value.matches(RE_", strings.ToUpper(o.Name), "_", strings.ToUpper(v.Name), "_EMAIL)) errors.add(\"", v.ProtoName, " must be a valid email\")")
	}
	if vc.URI {
		g.P("            if (value.isNotEmpty() && !value.matches(RE_", strings.ToUpper(o.Name), "_", strings.ToUpper(v.Name), "_URI)) errors.add(\"", v.ProtoName, " must be a valid URI\")")
	}
	if vc.UUID {
		g.P("            if (value.isNotEmpty() && !value.matches(RE_", strings.ToUpper(o.Name), "_", strings.ToUpper(v.Name), "_UUID)) errors.add(\"", v.ProtoName, " must be a valid UUID\")")
	}
	if vc.Pattern != "" {
		escaped := escapeKotlinStringLiteral(vc.Pattern)
		g.P("            if (!value.matches(RE_", strings.ToUpper(o.Name), "_", strings.ToUpper(v.Name), "_PATTERN)) errors.add(\"", v.ProtoName, " must match pattern: ", escaped, "\")")
	}
	if vc.MinLength != nil {
		g.P("            if (value.codePointCount(0, value.length) < ", *vc.MinLength, ") errors.add(\"", v.ProtoName, " must be at least ", *vc.MinLength, " characters\")")
	}
	if vc.MaxLength != nil {
		g.P("            if (value.codePointCount(0, value.length) > ", *vc.MaxLength, ") errors.add(\"", v.ProtoName, " must be at most ", *vc.MaxLength, " characters\")")
	}

	if vc.Prefix != "" {
		g.P("            if (!value.startsWith(\"", vc.Prefix, "\")) errors.add(\"", v.ProtoName, " must start with ", vc.Prefix, "\")")
	}
	if vc.Suffix != "" {
		g.P("            if (!value.endsWith(\"", vc.Suffix, "\")) errors.add(\"", v.ProtoName, " must end with ", vc.Suffix, "\")")
	}
	if vc.Contains != "" {
		g.P("            if (!value.contains(\"", vc.Contains, "\")) errors.add(\"", v.ProtoName, " must contain ", vc.Contains, "\")")
	}
	if vc.Const != nil {
		g.P("            if (value != ", *vc.Const, ") errors.add(\"", v.ProtoName, " must be exactly ", *vc.Const, "\")")
	}
	if len(vc.In) > 0 {
		vals := strings.Join(vc.In, ", ")
		g.P("            if (value !in listOf(", vals, ")) errors.add(\"", v.ProtoName, " must be one of [", vals, "]\")")
	}
	if len(vc.NotIn) > 0 {
		vals := strings.Join(vc.NotIn, ", ")
		g.P("            if (value in listOf(", vals, ")) errors.add(\"", v.ProtoName, " must not be one of [", vals, "]\")")
	}

	if vc.DefinedOnly && v.Kind == FieldKindEnum && v.EnumAsString {
		enumDef := findEnumByName(df, v.TypeName)
		if enumDef != nil {
			seen := make(map[string]bool)
			var names []string
			for _, ev := range enumDef.Values {
				if !seen[ev.Name] {
					seen[ev.Name] = true
					names = append(names, `"`+ev.Name+`"`)
				}
			}
			vals := strings.Join(names, ", ")
			g.P("            if (value !in listOf(", vals, ")) errors.add(\"", v.ProtoName, " must be a defined enum value\")")
		}
	}
}

// escapeKotlinStringLiteral escapes a string for use in a Kotlin double-quoted
// string literal. Handles backslashes, double quotes, and dollar signs (which
// trigger Kotlin string template interpolation).
func escapeKotlinStringLiteral(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "$", "\\$")
	return s
}

func kotlinEmitRegexConstants(g *protogen.GeneratedFile, msg *DomainMessage) {
	for _, f := range msg.Fields {
		vc := f.ValidateConstraints
		if vc == nil || !vc.HasConstraints() {
			continue
		}
		if vc.Email {
			g.P("private val RE_", strings.ToUpper(msg.Name), "_", strings.ToUpper(f.Name), "_EMAIL = Regex(\"^[^@\\\\s]+@[^@\\\\s]+\\\\.[^@\\\\s]+$\")")
		}
		if vc.URI {
			g.P("private val RE_", strings.ToUpper(msg.Name), "_", strings.ToUpper(f.Name), "_URI = Regex(\"^https?://.*\")")
		}
		if vc.UUID {
			g.P("private val RE_", strings.ToUpper(msg.Name), "_", strings.ToUpper(f.Name), "_UUID = Regex(\"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$\")")
		}
		if vc.Pattern != "" {
			escaped := escapeKotlinStringLiteral(vc.Pattern)
			g.P("private val RE_", strings.ToUpper(msg.Name), "_", strings.ToUpper(f.Name), "_PATTERN = Regex(\"", escaped, "\")")
		}
	}
	for _, o := range msg.Oneofs {
		for _, v := range o.Variants {
			vc := v.ValidateConstraints
			if vc == nil || !vc.HasConstraints() {
				continue
			}
			if vc.Email {
				g.P("private val RE_", strings.ToUpper(o.Name), "_", strings.ToUpper(v.Name), "_EMAIL = Regex(\"^[^@\\\\s]+@[^@\\\\s]+\\\\.[^@\\\\s]+$\")")
			}
			if vc.URI {
				g.P("private val RE_", strings.ToUpper(o.Name), "_", strings.ToUpper(v.Name), "_URI = Regex(\"^https?://.*\")")
			}
			if vc.UUID {
				g.P("private val RE_", strings.ToUpper(o.Name), "_", strings.ToUpper(v.Name), "_UUID = Regex(\"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$\")")
			}
			if vc.Pattern != "" {
				escaped := escapeKotlinStringLiteral(vc.Pattern)
				g.P("private val RE_", strings.ToUpper(o.Name), "_", strings.ToUpper(v.Name), "_PATTERN = Regex(\"", escaped, "\")")
			}
		}
	}
}
