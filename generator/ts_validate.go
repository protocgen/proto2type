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

	// TODO: IgnoreEmpty is extracted but not yet used in TS constraint emission.

	var parts []string

	// String/bytes length constraints.
	isBytesField := f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind
	if vc.MinLength != nil {
		if isBytesField {
			// buf.validate byte length refers to decoded bytes, not base64 string length.
			parts = append(parts, fmt.Sprintf(`.refine(v => { if (!/^[A-Za-z0-9+\/\-_]*={0,2}$/.test(v) || v.length %% 4 !== 0) return false; const p = v.endsWith("==") ? 2 : v.endsWith("=") ? 1 : 0; return (v.length * 3 / 4) - p >= %d; }, { message: "bytes must be valid base64 and at least %d bytes" })`, *vc.MinLength, *vc.MinLength))
		} else {
			parts = append(parts, fmt.Sprintf(".min(%d)", *vc.MinLength))
		}
	}
	if vc.MaxLength != nil {
		if isBytesField {
			parts = append(parts, fmt.Sprintf(`.refine(v => { if (!/^[A-Za-z0-9+\/\-_]*={0,2}$/.test(v) || v.length %% 4 !== 0) return false; const p = v.endsWith("==") ? 2 : v.endsWith("=") ? 1 : 0; return (v.length * 3 / 4) - p <= %d; }, { message: "bytes must be valid base64 and at most %d bytes" })`, *vc.MaxLength, *vc.MaxLength))
		} else {
			parts = append(parts, fmt.Sprintf(".max(%d)", *vc.MaxLength))
		}
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
		parts = append(parts, fmt.Sprintf(`.regex(new RegExp(%s), { message: "must match pattern %s" })`, escaped, vc.Pattern))
	}
	if vc.Len != nil {
		parts = append(parts, fmt.Sprintf(".length(%d)", *vc.Len))
	}
	if vc.Prefix != "" {
		parts = append(parts, fmt.Sprintf(".startsWith(%s)", strconv.Quote(vc.Prefix)))
	}
	if vc.Suffix != "" {
		parts = append(parts, fmt.Sprintf(".endsWith(%s)", strconv.Quote(vc.Suffix)))
	}
	if vc.Contains != "" {
		parts = append(parts, fmt.Sprintf(".includes(%s)", strconv.Quote(vc.Contains)))
	}
	if vc.Hostname {
		// Zod doesn't have .hostname(), use regex
		parts = append(parts, `.regex(new RegExp("^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$"), { message: "must be a valid hostname" })`)
	}
	if vc.IP {
		parts = append(parts, `.ip()`)
	}

	// Numeric constraints
	isInt64Kind := f.ScalarKind == protoreflect.Int64Kind || f.ScalarKind == protoreflect.Uint64Kind || f.ScalarKind == protoreflect.Sint64Kind || f.ScalarKind == protoreflect.Sfixed64Kind || f.ScalarKind == protoreflect.Fixed64Kind
	isBigInt := opts.TSInt64Style == "bigint" && isInt64Kind
	isInt64String := opts.TSInt64Style != "bigint" && isInt64Kind

	if vc.Gt != nil {
		if isInt64String {
			parts = append(parts, fmt.Sprintf(`.refine(v => { try { return BigInt(v) > %sn; } catch { return false; } }, { message: "must be > %s" })`, *vc.Gt, *vc.Gt))
		} else if isBigInt {
			parts = append(parts, fmt.Sprintf(`.refine(v => v > %sn, { message: "must be > %s" })`, *vc.Gt, *vc.Gt))
		} else {
			parts = append(parts, fmt.Sprintf(".gt(%s)", *vc.Gt))
		}
	}
	if vc.Gte != nil {
		if isInt64String {
			parts = append(parts, fmt.Sprintf(`.refine(v => { try { return BigInt(v) >= %sn; } catch { return false; } }, { message: "must be >= %s" })`, *vc.Gte, *vc.Gte))
		} else if isBigInt {
			parts = append(parts, fmt.Sprintf(`.refine(v => v >= %sn, { message: "must be >= %s" })`, *vc.Gte, *vc.Gte))
		} else {
			parts = append(parts, fmt.Sprintf(".gte(%s)", *vc.Gte))
		}
	}
	if vc.Lt != nil {
		if isInt64String {
			parts = append(parts, fmt.Sprintf(`.refine(v => { try { return BigInt(v) < %sn; } catch { return false; } }, { message: "must be < %s" })`, *vc.Lt, *vc.Lt))
		} else if isBigInt {
			parts = append(parts, fmt.Sprintf(`.refine(v => v < %sn, { message: "must be < %s" })`, *vc.Lt, *vc.Lt))
		} else {
			parts = append(parts, fmt.Sprintf(".lt(%s)", *vc.Lt))
		}
	}
	if vc.Lte != nil {
		if isInt64String {
			parts = append(parts, fmt.Sprintf(`.refine(v => { try { return BigInt(v) <= %sn; } catch { return false; } }, { message: "must be <= %s" })`, *vc.Lte, *vc.Lte))
		} else if isBigInt {
			parts = append(parts, fmt.Sprintf(`.refine(v => v <= %sn, { message: "must be <= %s" })`, *vc.Lte, *vc.Lte))
		} else {
			parts = append(parts, fmt.Sprintf(".lte(%s)", *vc.Lte))
		}
	}
	if vc.Const != nil {
		if isInt64String {
			parts = append(parts, fmt.Sprintf(`.refine(v => { try { return BigInt(v) === %sn; } catch { return false; } }, { message: "must equal %s" })`, *vc.Const, *vc.Const))
		} else if isBigInt {
			parts = append(parts, fmt.Sprintf(`.refine(v => v === %sn, { message: "must equal %s" })`, *vc.Const, *vc.Const))
		} else {
			parts = append(parts, fmt.Sprintf(`.refine(v => v === %s, { message: "must equal %s" })`, *vc.Const, *vc.Const))
		}
	}
	if len(vc.In) > 0 {
		vals := strings.Join(vc.In, ", ")
		parts = append(parts, fmt.Sprintf(`.refine(v => [%s].includes(v), { message: "must be one of [%s]" })`, vals, vals))
	}
	if len(vc.NotIn) > 0 {
		vals := strings.Join(vc.NotIn, ", ")
		parts = append(parts, fmt.Sprintf(`.refine(v => ![%s].includes(v), { message: "must not be one of [%s]" })`, vals, vals))
	}
	if vc.Unique {
		parts = append(parts, `.refine(v => new Set(v).size === v.length, { message: "items must be unique" })`)
	}

	return strings.Join(parts, "")
}

// tsOneofVariantZodConstraints returns Zod constraint chains for a oneof variant's
// buf.validate constraints. Returns empty string if no constraints.
func tsOneofVariantZodConstraints(v *OneofVariant, opts *Options) string {
	vc := v.ValidateConstraints
	if vc == nil || !vc.HasConstraints() {
		return ""
	}

	// Build a minimal DomainField to reuse tsZodConstraints logic.
	f := &DomainField{
		Kind:                FieldKindScalar,
		ScalarKind:          v.ScalarKind,
		ValidateConstraints: vc,
	}
	if v.Kind != FieldKindScalar {
		f.Kind = v.Kind
	}
	return tsZodConstraints(f, opts)
}
