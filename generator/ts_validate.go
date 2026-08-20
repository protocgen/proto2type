package generator

import (
	"fmt"
	"google.golang.org/protobuf/reflect/protoreflect"
	"regexp"
	"strconv"
	"strings"
)

// reHasNamedGroup detects RE2-style named capture groups (?P<name>...)
// which are not supported by JavaScript's RegExp engine.
var reHasNamedGroup = regexp.MustCompile(`\(\?P<`)

func tsZodConstraints(f *DomainField, opts *Options) string {
	vc := f.ValidateConstraints
	if vc == nil || !vc.HasConstraints() {
		return ""
	}

	var parts []string
	parts = append(parts, tsStringConstraints(f, vc))
	parts = append(parts, tsNumericConstraints(f, opts, vc))
	// Repeated and Map constraints are applied at the collection level in ts_domain.go

	return strings.Join(parts, "")
}

func tsStringConstraints(f *DomainField, vc *ValidateConstraints) string {
	var parts []string

	isBytesField := f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind || f.Kind == FieldKindWrapperBytes
	// Decoded byte length for base64: strip trailing '=' then Math.floor(len * 3 / 4).
	// This correctly handles both padded ("Zg==") and unpadded ("Zg") base64.
	const b64DecodedLen = `Math.floor(v.replace(/=+$/, "").length * 3 / 4)`
	if vc.MinLength != nil {
		if isBytesField {
			parts = append(parts, fmt.Sprintf(`.refine(v => /^[A-Za-z0-9+\/\-_]*={0,2}$/.test(v) && %s >= %d, { message: "bytes must be at least %d bytes" })`, b64DecodedLen, *vc.MinLength, *vc.MinLength))
		} else {
			parts = append(parts, fmt.Sprintf(".min(%d)", *vc.MinLength))
		}
	}
	if vc.MaxLength != nil {
		if isBytesField {
			parts = append(parts, fmt.Sprintf(`.refine(v => /^[A-Za-z0-9+\/\-_]*={0,2}$/.test(v) && %s <= %d, { message: "bytes must be at most %d bytes" })`, b64DecodedLen, *vc.MaxLength, *vc.MaxLength))
		} else {
			parts = append(parts, fmt.Sprintf(".max(%d)", *vc.MaxLength))
		}
	}
	if vc.Email {
		if vc.IgnoreEmpty {
			parts = append(parts, `.refine(v => v === "" || z.string().email().safeParse(v).success, { message: "must be a valid email" })`)
		} else {
			parts = append(parts, ".email()")
		}
	}
	if vc.URI {
		if vc.IgnoreEmpty {
			parts = append(parts, `.refine(v => v === "" || z.string().url().safeParse(v).success, { message: "must be a valid url" })`)
		} else {
			parts = append(parts, ".url()")
		}
	}
	if vc.UUID {
		if vc.IgnoreEmpty {
			parts = append(parts, `.refine(v => v === "" || z.string().uuid().safeParse(v).success, { message: "must be a valid uuid" })`)
		} else {
			parts = append(parts, ".uuid()")
		}
	}
	if vc.Pattern != "" {
		// Validate pattern with Go's RE2 engine (linear-time guarantee) to prevent ReDoS.
		// buf.validate specifies RE2 syntax, so valid patterns always pass this check.
		if _, err := regexp.Compile(vc.Pattern); err != nil {
			// Sanitize pattern to prevent comment injection (closing */ in pattern).
			safe := strings.ReplaceAll(vc.Pattern, "*/", "* /")
			parts = append(parts, fmt.Sprintf(` /* WARN: pattern %q is not RE2-safe, skipped */`, safe))
		} else if reHasNamedGroup.MatchString(vc.Pattern) {
			// RE2 named groups (?P<name>...) are not valid in JS RegExp.
			safe := strings.ReplaceAll(vc.Pattern, "*/", "* /")
			parts = append(parts, fmt.Sprintf(` /* WARN: pattern %q uses RE2 named groups unsupported by JS, skipped */`, safe))
		} else {
			escaped := strconv.Quote(vc.Pattern)
			if vc.IgnoreEmpty {
				parts = append(parts, fmt.Sprintf(`.refine(((re) => (v) => v === "" || re.test(v))(new RegExp(%s)), { message: "must match pattern" })`, escaped))
			} else {
				parts = append(parts, fmt.Sprintf(`.regex(new RegExp(%s), { message: "must match pattern" })`, escaped))
			}
		}
	}
	if vc.Len != nil {
		if isBytesField {
			parts = append(parts, fmt.Sprintf(`.refine(v => /^[A-Za-z0-9+\/\-_]*={0,2}$/.test(v) && %s === %d, { message: "bytes must be exactly %d bytes" })`, b64DecodedLen, *vc.Len, *vc.Len))
		} else {
			parts = append(parts, fmt.Sprintf(".length(%d)", *vc.Len))
		}
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
		if vc.IgnoreEmpty {
			parts = append(parts, `.refine(((re) => (v) => v === "" || re.test(v))(/^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/), { message: "must be a valid hostname" })`)
		} else {
			parts = append(parts, `.regex(/^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/, { message: "must be a valid hostname" })`)
		}
	}
	if vc.IP {
		if vc.IgnoreEmpty {
			parts = append(parts, `.refine(v => v === "" || z.string().ip().safeParse(v).success, { message: "must be a valid ip" })`)
		} else {
			parts = append(parts, `.ip()`)
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

	return strings.Join(parts, "")
}

func tsNumericConstraints(f *DomainField, opts *Options, vc *ValidateConstraints) string {
	var parts []string

	isNumericScalar := f.Kind == FieldKindScalar && (f.ScalarKind == protoreflect.Int32Kind || f.ScalarKind == protoreflect.Sint32Kind || f.ScalarKind == protoreflect.Sfixed32Kind ||
		f.ScalarKind == protoreflect.Uint32Kind || f.ScalarKind == protoreflect.Fixed32Kind ||
		f.ScalarKind == protoreflect.FloatKind || f.ScalarKind == protoreflect.DoubleKind ||
		f.ScalarKind == protoreflect.Int64Kind || f.ScalarKind == protoreflect.Sint64Kind || f.ScalarKind == protoreflect.Sfixed64Kind ||
		f.ScalarKind == protoreflect.Uint64Kind || f.ScalarKind == protoreflect.Fixed64Kind)

	isNumericWrapper := f.Kind == FieldKindWrapperInt32 || f.Kind == FieldKindWrapperUInt32 || f.Kind == FieldKindWrapperFloat || f.Kind == FieldKindWrapperDouble || f.Kind == FieldKindWrapperInt64 || f.Kind == FieldKindWrapperUInt64

	isNumeric := isNumericScalar || isNumericWrapper
	isTimestamp := f.Kind == FieldKindTimestamp
	isDuration := f.Kind == FieldKindDuration
	isWktTime := isTimestamp || isDuration

	if !isNumeric && !isWktTime {
		return ""
	}

	isInt64Kind := f.ScalarKind == protoreflect.Int64Kind || f.ScalarKind == protoreflect.Uint64Kind || f.ScalarKind == protoreflect.Sint64Kind || f.ScalarKind == protoreflect.Sfixed64Kind || f.ScalarKind == protoreflect.Fixed64Kind
	isWrapperInt64Kind := f.Kind == FieldKindWrapperInt64 || f.Kind == FieldKindWrapperUInt64

	isBigInt := opts.TSInt64Style == "bigint" && (isInt64Kind || isWrapperInt64Kind)
	isInt64String := opts.TSInt64Style != "bigint" && (isInt64Kind || isWrapperInt64Kind)

	numConstraint := func(op string, val *string, opName string) {
		if val == nil {
			return
		}
		if isDuration {
			parts = append(parts, fmt.Sprintf(`.refine(v => { const m = v.match(/^(-?[0-9]+(?:\.[0-9]+)?)s$/); if (!m) return false; return parseFloat(m[1]) %s parseFloat(%q.replace("s", "")); }, { message: "must be %s %s" })`, op, *val, opName, *val))
		} else if isTimestamp {
			parts = append(parts, fmt.Sprintf(`.refine(v => new Date(v).getTime() %s new Date(%q).getTime(), { message: "must be %s %s" })`, op, *val, opName, *val))
		} else if isInt64String {
			parts = append(parts, fmt.Sprintf(`.refine(v => { try { return BigInt(v) %s %sn; } catch { return false; } }, { message: "must be %s %s" })`, op, *val, opName, *val))
		} else if isBigInt {
			parts = append(parts, fmt.Sprintf(`.refine(v => v %s %sn, { message: "must be %s %s" })`, op, *val, opName, *val))
		} else {
			if op == "===" {
				parts = append(parts, fmt.Sprintf(`.refine(v => v === %s, { message: "must be %s %s" })`, *val, opName, *val))
			} else {
				// use zod native gt/gte/lt/lte for normal numbers
				zodOp := ""
				switch op {
				case ">":
					zodOp = "gt"
				case ">=":
					zodOp = "gte"
				case "<":
					zodOp = "lt"
				case "<=":
					zodOp = "lte"
				}
				parts = append(parts, fmt.Sprintf(".%s(%s)", zodOp, *val))
			}
		}
	}

	numConstraint(">", vc.Gt, ">")
	numConstraint(">=", vc.Gte, ">=")
	numConstraint("<", vc.Lt, "<")
	numConstraint("<=", vc.Lte, "<=")
	numConstraint("===", vc.Const, "equal to")

	return strings.Join(parts, "")
}

func tsRepeatedConstraints(vc *ValidateConstraints) string {
	if vc == nil || !vc.HasConstraints() {
		return ""
	}
	var parts []string
	if vc.MinItems != nil {
		parts = append(parts, fmt.Sprintf(".min(%d)", *vc.MinItems))
	}
	if vc.MaxItems != nil {
		parts = append(parts, fmt.Sprintf(".max(%d)", *vc.MaxItems))
	}
	if vc.Unique {
		// Document that unique constraint uses reference equality
		parts = append(parts, `.refine(v => new Set(v).size === v.length, { message: "items must be unique (checked by reference equality)" })`)
	}
	return strings.Join(parts, "")
}

func tsMapConstraints(vc *ValidateConstraints) string {
	if vc == nil || !vc.HasConstraints() {
		return ""
	}
	var parts []string
	if vc.MinItems != nil {
		parts = append(parts, fmt.Sprintf(".refine(v => Object.keys(v).length >= %d, { message: 'Map must have at least %d entries' })", *vc.MinItems, *vc.MinItems))
	}
	if vc.MaxItems != nil {
		parts = append(parts, fmt.Sprintf(".refine(v => Object.keys(v).length <= %d, { message: 'Map must have at most %d entries' })", *vc.MaxItems, *vc.MaxItems))
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
