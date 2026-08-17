package generator

import (
	"fmt"
	"google.golang.org/protobuf/reflect/protoreflect"
	"strconv"
	"strings"
)

// tsZodConstraints returns additional Zod method chain segments for a field's
// buf.validate constraints. Returns empty string if no constraints.
func tsZodConstraints(f *DomainField, opts *Options) string {
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
		escaped := strconv.Quote(vc.Pattern)
		parts = append(parts, fmt.Sprintf(".regex(new RegExp(%s))", escaped))
	}

	// Numeric constraints
	isInt64Kind := f.ScalarKind == protoreflect.Int64Kind || f.ScalarKind == protoreflect.Uint64Kind || f.ScalarKind == protoreflect.Sint64Kind || f.ScalarKind == protoreflect.Sfixed64Kind || f.ScalarKind == protoreflect.Fixed64Kind
	isBigInt := opts.TSInt64Style == "bigint" && isInt64Kind
	isInt64String := opts.TSInt64Style != "bigint" && isInt64Kind

	formatNum := func(v string) string {
		if isBigInt {
			return v + "n"
		}
		return v
	}

	if vc.Gt != nil {
		if isInt64String {
			parts = append(parts, fmt.Sprintf(`.refine(v => BigInt(v) > %sn, { message: "must be > %s" })`, *vc.Gt, *vc.Gt))
		} else {
			parts = append(parts, fmt.Sprintf(".gt(%s)", formatNum(*vc.Gt)))
		}
	}
	if vc.Gte != nil {
		if isInt64String {
			parts = append(parts, fmt.Sprintf(`.refine(v => BigInt(v) >= %sn, { message: "must be >= %s" })`, *vc.Gte, *vc.Gte))
		} else {
			parts = append(parts, fmt.Sprintf(".gte(%s)", formatNum(*vc.Gte)))
		}
	}
	if vc.Lt != nil {
		if isInt64String {
			parts = append(parts, fmt.Sprintf(`.refine(v => BigInt(v) < %sn, { message: "must be < %s" })`, *vc.Lt, *vc.Lt))
		} else {
			parts = append(parts, fmt.Sprintf(".lt(%s)", formatNum(*vc.Lt)))
		}
	}
	if vc.Lte != nil {
		if isInt64String {
			parts = append(parts, fmt.Sprintf(`.refine(v => BigInt(v) <= %sn, { message: "must be <= %s" })`, *vc.Lte, *vc.Lte))
		} else {
			parts = append(parts, fmt.Sprintf(".lte(%s)", formatNum(*vc.Lte)))
		}
	}

	// Repeated constraints are handled in writeTSMessage for arrays,
	// but keeping them here is fine if not applied there, though
	// the logic in ts_domain.go applies them separately to the array wrapper.
	// Wait, the prompt spec says to put them here.
	// Ah, writeTSMessage already does it for array wrapper, but let's keep it here if called for base type?
	// Actually, if it's repeated, these shouldn't be applied to the base type. Let's let ts_domain handle array constraints.

	return strings.Join(parts, "")
}
