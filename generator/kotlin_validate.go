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
// NOTE: length validation counts characters (Unicode scalar values), not bytes.
// This differs from proto's min_len/max_len which count bytes, but matches
// user expectations and is consistent with Python/Pydantic and Rust/validator.
func generateKotlinValidate(g *protogen.GeneratedFile, msg *DomainMessage, opts *Options) {
	if !opts.Validate {
		return
	}

	// Check if this message has any constraints to validate.
	hasConstraints := false
	for _, f := range msg.Fields {
		if f.ValidateConstraints != nil && f.ValidateConstraints.HasConstraints() {
			hasConstraints = true
			break
		}
	}
	if !hasConstraints {
		return
	}

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

		// Required (for nullable fields)
		if vc.Required && f.Optional {
			g.P("    if (", fieldName, " == null) errors.add(\"", f.Name, " is required\")")
		}

		// Email
		if vc.Email {
			g.P("    if (", fieldName, ".isNotEmpty() && !", fieldName, ".matches(Regex(\"^[^@\\\\s]+@[^@\\\\s]+\\\\.[^@\\\\s]+$\"))) errors.add(\"", f.Name, " must be a valid email\")")
		}

		// URI
		if vc.URI {
			g.P("    if (", fieldName, ".isNotEmpty() && !", fieldName, ".matches(Regex(\"^https?://.*\"))) errors.add(\"", f.Name, " must be a valid URI\")")
		}

		// UUID
		if vc.UUID {
			g.P("    if (", fieldName, ".isNotEmpty() && !", fieldName, ".matches(Regex(\"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$\"))) errors.add(\"", f.Name, " must be a valid UUID\")")
		}

		// Pattern
		if vc.Pattern != "" {
			escaped := strings.ReplaceAll(vc.Pattern, "\\", "\\\\")
			escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
			g.P("    if (!", fieldName, ".matches(Regex(\"", escaped, "\"))) errors.add(\"", f.Name, " must match pattern: ", vc.Pattern, "\")")
		}

		// String length (min_len / max_len)
		// NOTE: counts characters, not bytes.
		if vc.MinLength != nil {
			g.P("    if (", fieldName, ".length < ", *vc.MinLength, ") errors.add(\"", f.Name, " must be at least ", *vc.MinLength, " characters\")")
		}
		if vc.MaxLength != nil {
			g.P("    if (", fieldName, ".length > ", *vc.MaxLength, ") errors.add(\"", f.Name, " must be at most ", *vc.MaxLength, " characters\")")
		}

		// Numeric range
		if vc.Gte != nil {
			g.P("    if (", fieldName, " < ", *vc.Gte, ") errors.add(\"", f.Name, " must be >= ", *vc.Gte, "\")")
		}
		if vc.Gt != nil {
			g.P("    if (", fieldName, " <= ", *vc.Gt, ") errors.add(\"", f.Name, " must be > ", *vc.Gt, "\")")
		}
		if vc.Lte != nil {
			g.P("    if (", fieldName, " > ", *vc.Lte, ") errors.add(\"", f.Name, " must be <= ", *vc.Lte, "\")")
		}
		if vc.Lt != nil {
			g.P("    if (", fieldName, " >= ", *vc.Lt, ") errors.add(\"", f.Name, " must be < ", *vc.Lt, "\")")
		}

		// Repeated min/max items
		if f.Repeated {
			if vc.MinItems != nil {
				g.P("    if (", fieldName, ".size < ", *vc.MinItems, ") errors.add(\"", f.Name, " must have at least ", *vc.MinItems, " items\")")
			}
			if vc.MaxItems != nil {
				g.P("    if (", fieldName, ".size > ", *vc.MaxItems, ") errors.add(\"", f.Name, " must have at most ", *vc.MaxItems, " items\")")
			}
		}
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
}
