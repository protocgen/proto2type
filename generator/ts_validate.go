package generator

import (
	"fmt"
	"google.golang.org/protobuf/reflect/protoreflect"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
)

// reHasNamedGroup detects RE2-style named capture groups (?P<name>...)
// which are not supported by JavaScript's RegExp engine.
var reHasNamedGroup = regexp.MustCompile(`\(\?P<`)

// hasDangerousPattern detects patterns with quantifiers nested inside other
// quantifiers (e.g. (a+)+, (a*){2,}, (x+y+)*) or overlapping alternations. These are safe in RE2 (which
// guarantees linear time) but cause catastrophic backtracking in JavaScript's
// NFA-based RegExp engine. Returns true if the pattern contains dangerous
// constructs that could cause ReDoS.
func hasDangerousPattern(pattern string) bool {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return false // Can't parse — let the regexp.Compile check handle it.
	}
	// Do NOT call re.Simplify() — it collapses (?:a+)+ to a+,
	// hiding dangerous nested quantifiers from detection.
	return walkForDangerousPattern(re, false)
}

// isUnboundedQuantifier returns true for quantifiers that can match
// a variable number of repetitions (Star, Plus, unbounded Repeat).
// Fixed repeats like a{2} or a{2,3} are not dangerous because they
// expand to a fixed number of alternatives and cannot cause backtracking.
func isUnboundedQuantifier(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpStar, syntax.OpPlus:
		return true
	case syntax.OpRepeat:
		// re.Max == -1 means unbounded (e.g. a{2,}). Fixed bounds
		// like a{2} or a{2,3} are safe.
		return re.Max == -1
	}
	return false
}

// canMatchAnyChar returns true if a quantified regex sub-expression can match
// arbitrary characters (dot, character class, negated class). Simple literals
// like 'a' or 'b' return false — adjacent quantifiers on distinct literals
// (e.g. a+b+) are safe because they can't overlap.
func canMatchAnyChar(re *syntax.Regexp) bool {
	// Unwrap the quantifier to get the inner expression.
	inner := re
	if isUnboundedQuantifier(re) && len(re.Sub) > 0 {
		inner = re.Sub[0]
	}
	switch inner.Op {
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return true // . or (?s:.)
	case syntax.OpCharClass:
		return true // [a-z], \w, \d, etc.
	case syntax.OpAlternate, syntax.OpCapture, syntax.OpConcat:
		return true // complex sub-expressions may overlap
	}
	return false
}

// walkForDangerousPattern recursively walks the regex AST. The inQuantifier
// flag tracks whether we're already inside a quantified sub-expression.
func walkForDangerousPattern(re *syntax.Regexp, inQuantifier bool) bool {
	unbounded := isUnboundedQuantifier(re)
	if unbounded && inQuantifier {
		return true
	}
	// Alternation under a quantifier is only dangerous when branches share
	// overlapping character sets (e.g. (a|a)* causes backtracking). Single-char
	// alternations are folded to OpCharClass by the parser, so multi-char
	// alternations like (ab|cd)+ are safe. We skip this check to avoid
	// false positives — the nested quantifier and adjacent checks catch
	// the truly dangerous patterns.

	// Check for adjacent unbounded quantifiers with overlapping character classes.
	// a+b+ is safe (different literals), but .*\d+ or \w+\d+ are dangerous.
	if re.Op == syntax.OpConcat {
		for i := 0; i < len(re.Sub)-1; i++ {
			if isUnboundedQuantifier(re.Sub[i]) && isUnboundedQuantifier(re.Sub[i+1]) {
				// Only dangerous if at least one can match arbitrary characters.
				if canMatchAnyChar(re.Sub[i]) || canMatchAnyChar(re.Sub[i+1]) {
					return true
				}
			}
		}
	}

	// Recurse into sub-expressions. If this node is an unbounded quantifier,
	// mark children as being inside a quantifier.
	childInQ := inQuantifier || unbounded
	for _, sub := range re.Sub {
		if walkForDangerousPattern(sub, childInQ) {
			return true
		}
	}
	return false
}

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
	// Split into two groups: ZodString-native methods (which must come first
	// since they return ZodString), then .refine() methods (which return ZodEffects).
	var nativeParts []string // .email(), .uuid(), .regex(), .startsWith(), etc.
	var parts []string       // .refine(...) calls — accumulated as before

	isBytesField := f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind || f.Kind == FieldKindWrapperBytes
	// Decoded byte length for base64: strip trailing '=' then Math.floor(len * 3 / 4).
	// This correctly handles both padded ("Zg==") and unpadded ("Zg") base64.
	const b64DecodedLen = `Math.floor(v.replace(/=+$/, "").length * 3 / 4)`
	if vc.MinLength != nil {
		if isBytesField {
			parts = append(parts, fmt.Sprintf(`.refine(v => /^[A-Za-z0-9+\/\-_]*={0,2}$/.test(v) && %s >= %d, { message: "bytes must be at least %d bytes" })`, b64DecodedLen, *vc.MinLength, *vc.MinLength))
		} else {
			parts = append(parts, fmt.Sprintf(`.refine(v => [...v].length >= %d, { message: "must be at least %d characters" })`, *vc.MinLength, *vc.MinLength))
		}
	}
	if vc.MaxLength != nil {
		if isBytesField {
			parts = append(parts, fmt.Sprintf(`.refine(v => /^[A-Za-z0-9+\/\-_]*={0,2}$/.test(v) && %s <= %d, { message: "bytes must be at most %d bytes" })`, b64DecodedLen, *vc.MaxLength, *vc.MaxLength))
		} else {
			parts = append(parts, fmt.Sprintf(`.refine(v => [...v].length <= %d, { message: "must be at most %d characters" })`, *vc.MaxLength, *vc.MaxLength))
		}
	}
	if vc.Email {
		// proto3: skip format check on zero-value (empty string) to match Go/Kotlin behavior.
		parts = append(parts, `.refine(v => v === "" || z.string().email().safeParse(v).success, { message: "must be a valid email" })`)
	}
	if vc.URI {
		// proto3: skip format check on zero-value (empty string) to match Go/Kotlin behavior.
		// Restrict to http/https schemes to prevent XSS (javascript:) and SSRF (file:, data:).
		parts = append(parts, `.refine(v => v === "" || (z.string().url().safeParse(v).success && /^https?:\/\//i.test(v)), { message: "must be a valid http(s) URL" })`)
	}
	if vc.UUID {
		// proto3: skip format check on zero-value (empty string) to match Go/Kotlin behavior.
		parts = append(parts, `.refine(v => v === "" || z.string().uuid().safeParse(v).success, { message: "must be a valid uuid" })`)
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
		} else if hasDangerousPattern(vc.Pattern) {
			// Nested quantifiers (e.g. (a+)+) are safe in RE2 but cause catastrophic
			// backtracking (ReDoS) in JavaScript's NFA-based RegExp engine.
			safe := strings.ReplaceAll(vc.Pattern, "*/", "* /")
			parts = append(parts, fmt.Sprintf(` /* WARN: pattern %q has dangerous quantifiers (potential ReDoS in JS), skipped */`, safe))
		} else {
			escaped := strconv.Quote(vc.Pattern)
			if vc.IgnoreEmpty {
				parts = append(parts, fmt.Sprintf(`.refine(((re) => (v) => v === "" || re.test(v))(new RegExp(%s, "u")), { message: "must match pattern" })`, escaped))
			} else {
				nativeParts = append(nativeParts, fmt.Sprintf(`.regex(new RegExp(%s, "u"), { message: "must match pattern" })`, escaped))
			}
		}
	}
	if vc.Len != nil {
		if isBytesField {
			parts = append(parts, fmt.Sprintf(`.refine(v => /^[A-Za-z0-9+\/\-_]*={0,2}$/.test(v) && %s === %d, { message: "bytes must be exactly %d bytes" })`, b64DecodedLen, *vc.Len, *vc.Len))
		} else {
			parts = append(parts, fmt.Sprintf(`.refine(v => [...v].length === %d, { message: "must be exactly %d characters" })`, *vc.Len, *vc.Len))
		}
	}
	if vc.Const != nil && !isBytesField {
		// *vc.Const is pre-quoted (e.g. "\"hello\""), safe for JS comparison.
		// For the message string, strip outer quotes to avoid nested double-quote breakage.
		msgVal := strings.Trim(*vc.Const, `"`)
		if msgVal == "" {
			msgVal = "<empty string>"
		}
		parts = append(parts, fmt.Sprintf(`.refine(v => v === %s, { message: "must be exactly %s" })`, *vc.Const, msgVal))
	}
	if vc.Prefix != "" {
		nativeParts = append(nativeParts, fmt.Sprintf(".startsWith(%s)", strconv.Quote(vc.Prefix)))
	}
	if vc.Suffix != "" {
		nativeParts = append(nativeParts, fmt.Sprintf(".endsWith(%s)", strconv.Quote(vc.Suffix)))
	}
	if vc.Contains != "" {
		nativeParts = append(nativeParts, fmt.Sprintf(".includes(%s)", strconv.Quote(vc.Contains)))
	}
	if vc.Hostname {
		// proto3: skip format check on zero-value (empty string) to match Go/Kotlin behavior.
		parts = append(parts, `.refine(((re) => (v) => v === "" || re.test(v))(/^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/), { message: "must be a valid hostname" })`)
	}
	if vc.IP {
		// proto3: skip format check on zero-value (empty string) to match Go/Kotlin behavior.
		parts = append(parts, `.refine(v => v === "" || z.string().ip().safeParse(v).success, { message: "must be a valid ip" })`)
	}
	if len(vc.In) > 0 {
		vals := strings.Join(vc.In, ", ")
		// Strip outer quotes from each value for human-readable message
		var msgVals []string
		for _, v := range vc.In {
			msgVals = append(msgVals, strings.Trim(v, `"`))
		}
		msgStr := strings.Join(msgVals, ", ")
		parts = append(parts, fmt.Sprintf(`.refine(v => [%s].includes(v), { message: "must be one of [%s]" })`, vals, msgStr))
	}
	if len(vc.NotIn) > 0 {
		vals := strings.Join(vc.NotIn, ", ")
		var msgVals []string
		for _, v := range vc.NotIn {
			msgVals = append(msgVals, strings.Trim(v, `"`))
		}
		msgStr := strings.Join(msgVals, ", ")
		parts = append(parts, fmt.Sprintf(`.refine(v => ![%s].includes(v), { message: "must not be one of [%s]" })`, vals, msgStr))
	}

	// Emit native ZodString methods first, then .refine() methods.
	return strings.Join(nativeParts, "") + strings.Join(parts, "")
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
			parts = append(parts, fmt.Sprintf(`.refine(((limit) => (v) => { const m = v.match(/^(-?[0-9]+(?:\.[0-9]+)?)s$/); if (!m) return false; return parseFloat(m[1]) %s limit; })(parseFloat(%q.replace("s", ""))), { message: "must be %s %s" })`, op, *val, opName, *val))
		} else if isTimestamp {
			parts = append(parts, fmt.Sprintf(`.refine(((limit) => (v) => new Date(v).getTime() %s limit)(new Date(%q).getTime()), { message: "must be %s %s" })`, op, *val, opName, *val))
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
