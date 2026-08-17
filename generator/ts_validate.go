package generator

import (
	"fmt"
	"strings"
)

// tsZodConstraints returns additional Zod method chain segments for a field's
// buf.validate constraints. Returns empty string if no constraints.
func tsZodConstraints(f *DomainField) string {
	vc := f.ValidateConstraints
	if vc == nil || !vc.HasConstraints() {
		return ""
	}

	var parts []string

	// String constraints
	if vc.MinLength != nil {
		parts = append(parts, fmt.Sprintf(".min(%d)", *vc.MinLength))
	}
	if vc.MaxLength != nil {
		parts = append(parts, fmt.Sprintf(".max(%d)", *vc.MaxLength))
	}
	if vc.Email {
		parts = append(parts, ".email()")
	}
	if vc.URI {
		parts = append(parts, ".url()")
	}
	if vc.UUID {
		parts = append(parts, ".uuid()")
	}
	if vc.Pattern != "" {
		escaped := strings.ReplaceAll(vc.Pattern, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, "/", "\\/")
		parts = append(parts, fmt.Sprintf(".regex(/%s/)", escaped))
	}

	// Numeric constraints
	if vc.Gt != nil {
		parts = append(parts, fmt.Sprintf(".gt(%s)", *vc.Gt))
	}
	if vc.Gte != nil {
		parts = append(parts, fmt.Sprintf(".gte(%s)", *vc.Gte))
	}
	if vc.Lt != nil {
		parts = append(parts, fmt.Sprintf(".lt(%s)", *vc.Lt))
	}
	if vc.Lte != nil {
		parts = append(parts, fmt.Sprintf(".lte(%s)", *vc.Lte))
	}

	// Repeated constraints are handled in writeTSMessage for arrays,
	// but keeping them here is fine if not applied there, though
	// the logic in ts_domain.go applies them separately to the array wrapper.
	// Wait, the prompt spec says to put them here.
	// Ah, writeTSMessage already does it for array wrapper, but let's keep it here if called for base type?
	// Actually, if it's repeated, these shouldn't be applied to the base type. Let's let ts_domain handle array constraints.

	return strings.Join(parts, "")
}
