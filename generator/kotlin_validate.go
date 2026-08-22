package generator

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
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
func generateKotlinValidate(g *protogen.GeneratedFile, msg *DomainMessage, opts *Options) {
	if !opts.ValidateEnabled() {
		return
	}

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
			vc.MinItems != nil || vc.MaxItems != nil

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

		// Email
		if vc.Email {
			g.P(indent, "if (", fieldName, ".isNotEmpty() && !", fieldName, ".matches(Regex(\"^[^@\\\\s]+@[^@\\\\s]+\\\\.[^@\\\\s]+$\"))) errors.add(\"", f.Name, " must be a valid email\")")
		}

		// URI
		if vc.URI {
			g.P(indent, "if (", fieldName, ".isNotEmpty() && !", fieldName, ".matches(Regex(\"^https?://.*\"))) errors.add(\"", f.Name, " must be a valid URI\")")
		}

		// UUID
		if vc.UUID {
			g.P(indent, "if (", fieldName, ".isNotEmpty() && !", fieldName, ".matches(Regex(\"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$\"))) errors.add(\"", f.Name, " must be a valid UUID\")")
		}

		// Pattern
		if vc.Pattern != "" {
			escaped := escapeKotlinStringLiteral(vc.Pattern)
			g.P(indent, "if (!", fieldName, ".matches(Regex(\"", escaped, "\"))) errors.add(\"", f.Name, " must match pattern: ", escaped, "\")")
		}

		// String length (min_len / max_len)
		// NOTE: counts characters, not bytes.
		if vc.MinLength != nil {
			g.P(indent, "if (", fieldName, ".length < ", *vc.MinLength, ") errors.add(\"", f.Name, " must be at least ", *vc.MinLength, " characters\")")
		}
		if vc.MaxLength != nil {
			g.P(indent, "if (", fieldName, ".length > ", *vc.MaxLength, ") errors.add(\"", f.Name, " must be at most ", *vc.MaxLength, " characters\")")
		}

		// Numeric range
		if vc.Gte != nil {
			g.P(indent, "if (", fieldName, " < ", *vc.Gte, ") errors.add(\"", f.Name, " must be >= ", *vc.Gte, "\")")
		}
		if vc.Gt != nil {
			g.P(indent, "if (", fieldName, " <= ", *vc.Gt, ") errors.add(\"", f.Name, " must be > ", *vc.Gt, "\")")
		}
		if vc.Lte != nil {
			g.P(indent, "if (", fieldName, " > ", *vc.Lte, ") errors.add(\"", f.Name, " must be <= ", *vc.Lte, "\")")
		}
		if vc.Lt != nil {
			g.P(indent, "if (", fieldName, " >= ", *vc.Lt, ") errors.add(\"", f.Name, " must be < ", *vc.Lt, "\")")
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
			generateKotlinValidate(g, nested, opts)
		}
	}

	// Generate validation for oneof sealed classes.
	for _, o := range msg.Oneofs {
		generateKotlinOneofValidate(g, o, opts)
	}
}

// generateKotlinOneofValidate generates validate() and validateOrThrow() extension
// functions for a oneof sealed class. For message variants, it propagates validation
// to the inner message. For scalar variants with constraints, it emits constraint checks.
func generateKotlinOneofValidate(g *protogen.GeneratedFile, o *DomainOneof, opts *Options) {
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
			emitKotlinOneofVariantConstraints(g, v)
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
func emitKotlinOneofVariantConstraints(g *protogen.GeneratedFile, v *OneofVariant) {
	vc := v.ValidateConstraints
	if vc == nil {
		return
	}
	if vc.Email {
		g.P("            if (value.isNotEmpty() && !value.matches(Regex(\"^[^@\\\\s]+@[^@\\\\s]+\\\\.[^@\\\\s]+$\"))) errors.add(\"", v.ProtoName, " must be a valid email\")")
	}
	if vc.URI {
		g.P("            if (value.isNotEmpty() && !value.matches(Regex(\"^https?://.*\"))) errors.add(\"", v.ProtoName, " must be a valid URI\")")
	}
	if vc.UUID {
		g.P("            if (value.isNotEmpty() && !value.matches(Regex(\"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$\"))) errors.add(\"", v.ProtoName, " must be a valid UUID\")")
	}
	if vc.Pattern != "" {
		escaped := escapeKotlinStringLiteral(vc.Pattern)
		g.P("            if (!value.matches(Regex(\"", escaped, "\"))) errors.add(\"", v.ProtoName, " must match pattern: ", escaped, "\")")
	}
	if vc.MinLength != nil {
		g.P("            if (value.length < ", *vc.MinLength, ") errors.add(\"", v.ProtoName, " must be at least ", *vc.MinLength, " characters\")")
	}
	if vc.MaxLength != nil {
		g.P("            if (value.length > ", *vc.MaxLength, ") errors.add(\"", v.ProtoName, " must be at most ", *vc.MaxLength, " characters\")")
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
