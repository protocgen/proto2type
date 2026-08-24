package generator

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// generateGoValidate generates a Validate() error method on the domain type.
//
// Three parts:
//   - Oneof mutual exclusion: always generated. Ensures at most one variant
//     is set per oneof group (the Go flattened representation allows multiple).
//   - protovalidate delegation: when opts.Validate is "true". Calls
//     protovalidate.Validate(d.ToProto()) for buf.validate constraint checking.
//   - native validation: when opts.Validate is "native". Emits pure Go checks
//     from buf.validate constraints with zero external dependencies.
func generateGoValidate(g *protogen.GeneratedFile, dm *DomainMessage, opts *Options) {
	recv := receiverName(dm.Name)
	hasOneofs := dm.HasNonSyntheticOneof
	useProtovalidate := opts.ValidateEnabled() && !opts.ValidateNative()
	useNative := opts.ValidateNative()

	// Skip if nothing to validate.
	if !hasOneofs && !useProtovalidate && !useNative {
		return
	}

	// Emit package-level regex vars for native validation.
	if useNative {
		goEmitRegexVars(g, dm)
	}

	g.P("// Validate checks domain invariants on ", dm.Name, ".")
	if hasOneofs {
		g.P("// It ensures at most one variant is set per oneof group.")
	}
	if useProtovalidate {
		g.P("// It also runs buf.validate constraints via protovalidate.")
	}
	if useNative {
		g.P("// It also runs buf.validate constraints as native Go checks.")
	}
	g.P("func (", recv, " *", dm.Name, ") Validate() error {")
	g.P("\tif ", recv, " == nil {")
	g.P("\t\treturn nil")
	g.P("\t}")

	// Part 1: Oneof mutual exclusion.
	if hasOneofs {
		fmtErrorf := g.QualifiedGoIdent(protogen.GoIdent{
			GoImportPath: "fmt",
			GoName:       "Errorf",
		})

		for _, oneof := range dm.Oneofs {
			g.P("\t{")
			g.P("\t\t_oneofCount := 0")
			for _, v := range oneof.Variants {
				g.P("\t\tif ", recv, ".", v.Name, " != nil { _oneofCount++ }")
			}
			g.P("\t\tif _oneofCount > 1 {")
			g.P("\t\t\treturn ", fmtErrorf, "(\"oneof ", oneof.FieldName, ": %d variants set, expected at most 1\", _oneofCount)")
			g.P("\t\t}")
			g.P("\t}")
		}
	}

	// Part 2: protovalidate delegation.
	if useProtovalidate {
		protovalidateValidate := g.QualifiedGoIdent(protogen.GoIdent{
			GoImportPath: "buf.build/go/protovalidate",
			GoName:       "Validate",
		})
		g.P("\tif err := ", protovalidateValidate, "(", recv, ".ToProto()); err != nil {")
		g.P("\t\treturn err")
		g.P("\t}")
	}

	// Part 3: Native validation checks.
	if useNative {
		goEmitNativeFieldChecks(g, dm, recv)
		goEmitNativeNestedChecks(g, dm, recv)
	}

	g.P("\treturn nil")
	g.P("}")
	g.P()
}

// goEmitRegexVars emits package-level compiled regexp variables for pattern/email/uuid/uri checks.
func goEmitRegexVars(g *protogen.GeneratedFile, dm *DomainMessage) {
	regexpMustCompile := g.QualifiedGoIdent(protogen.GoIdent{
		GoImportPath: "regexp",
		GoName:       "MustCompile",
	})

	for _, f := range dm.Fields {
		vc := f.ValidateConstraints
		if vc == nil {
			continue
		}
		prefix := fmt.Sprintf("_re%s%s", dm.Name, f.PascalName)

		if vc.Email {
			g.P("var ", prefix, "Email = ", regexpMustCompile, "(`^[^@\\s]+@[^@\\s]+\\.[^@\\s]+$`)")
		}
		if vc.UUID {
			g.P("var ", prefix, "UUID = ", regexpMustCompile, "(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)")
		}
		if vc.URI {
			g.P("var ", prefix, "URI = ", regexpMustCompile, "(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)")
		}
		if vc.Hostname {
			g.P("var ", prefix, "Hostname = ", regexpMustCompile, "(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)")
		}
		if vc.IP {
			g.P("var ", prefix, "IP = ", regexpMustCompile, "(`^((25[0-5]|2[0-4]\\d|[01]?\\d\\d?)\\.){3}(25[0-5]|2[0-4]\\d|[01]?\\d\\d?)$|^([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}$`)")
		}
		// F10: Use strconv.Quote for pattern to handle backticks safely.
		if vc.Pattern != "" {
			g.P("var ", prefix, "Pattern = ", regexpMustCompile, "(", strconv.Quote(vc.Pattern), ")")
		}
	}
}

// goEmitNativeFieldChecks emits native Go constraint checks for each field.
func goEmitNativeFieldChecks(g *protogen.GeneratedFile, dm *DomainMessage, recv string) {
	fmtErrorf := g.QualifiedGoIdent(protogen.GoIdent{
		GoImportPath: "fmt",
		GoName:       "Errorf",
	})

	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		vc := f.ValidateConstraints
		if vc == nil || !vc.HasConstraints() {
			continue
		}

		fieldAccess := recv + "." + f.PascalName
		isString := f.ScalarKind == protoreflect.StringKind
		// F11: Repeated fields are slices, not pointers. Map fields are also not pointers.
		isPointer := (f.Optional || f.Kind == FieldKindMessage) && !f.Repeated && !f.IsMap

		// Required check for pointer fields.
		if vc.Required && isPointer {
			g.P("\tif ", fieldAccess, " == nil {")
			g.P("\t\treturn ", fmtErrorf, "(\"", f.Name, " is required\")")
			g.P("\t}")
		}

		// For pointer fields, dereference into a local and guard with nil check.
		indent := "\t"
		localVar := fieldAccess
		if isPointer && hasNonRequiredGoConstraints(vc) {
			g.P("\tif ", fieldAccess, " != nil {")
			indent = "\t\t"
			// F12: Parenthesize dereference for correct method calls on pointer types.
			localVar = fmt.Sprintf("(*%s)", fieldAccess)
		}

		// String length (counts runes, not bytes — consistent with all other backends).
		if isString && vc.MinLength != nil {
			utf8RuneCountInString := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "unicode/utf8",
				GoName:       "RuneCountInString",
			})
			g.P(indent, "if ", utf8RuneCountInString, "(", localVar, ") < ", *vc.MinLength, " {")
			g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must be at least %d characters\", ", *vc.MinLength, ")")
			g.P(indent, "}")
		}
		if isString && vc.MaxLength != nil {
			utf8RuneCountInString := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "unicode/utf8",
				GoName:       "RuneCountInString",
			})
			g.P(indent, "if ", utf8RuneCountInString, "(", localVar, ") > ", *vc.MaxLength, " {")
			g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must be at most %d characters\", ", *vc.MaxLength, ")")
			g.P(indent, "}")
		}
		if isString && vc.Len != nil {
			utf8RuneCountInString := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "unicode/utf8",
				GoName:       "RuneCountInString",
			})
			g.P(indent, "if ", utf8RuneCountInString, "(", localVar, ") != ", *vc.Len, " {")
			g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must be exactly %d characters\", ", *vc.Len, ")")
			g.P(indent, "}")
		}

		// Pattern (regex) — F10: use strconv.Quote for the error message too.
		if vc.Pattern != "" {
			prefix := fmt.Sprintf("_re%s%s", dm.Name, f.PascalName)
			g.P(indent, "if !", prefix, "Pattern.MatchString(", localVar, ") {")
			g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must match pattern %s\", ", strconv.Quote(vc.Pattern), ")")
			g.P(indent, "}")
		}

		// Email.
		if vc.Email {
			prefix := fmt.Sprintf("_re%s%s", dm.Name, f.PascalName)
			g.P(indent, "if ", localVar, " != \"\" && !", prefix, "Email.MatchString(", localVar, ") {")
			g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must be a valid email address\")")
			g.P(indent, "}")
		}

		// UUID.
		if vc.UUID {
			prefix := fmt.Sprintf("_re%s%s", dm.Name, f.PascalName)
			g.P(indent, "if ", localVar, " != \"\" && !", prefix, "UUID.MatchString(", localVar, ") {")
			g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must be a valid UUID\")")
			g.P(indent, "}")
		}

		// URI.
		if vc.URI {
			prefix := fmt.Sprintf("_re%s%s", dm.Name, f.PascalName)
			g.P(indent, "if ", localVar, " != \"\" && !", prefix, "URI.MatchString(", localVar, ") {")
			g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must be a valid URI\")")
			g.P(indent, "}")
		}

		// Hostname.
		if vc.Hostname {
			prefix := fmt.Sprintf("_re%s%s", dm.Name, f.PascalName)
			g.P(indent, "if ", localVar, " != \"\" && !", prefix, "Hostname.MatchString(", localVar, ") {")
			g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must be a valid hostname\")")
			g.P(indent, "}")
		}

		// F16: IP validation.
		if vc.IP {
			prefix := fmt.Sprintf("_re%s%s", dm.Name, f.PascalName)
			g.P(indent, "if ", localVar, " != \"\" && !", prefix, "IP.MatchString(", localVar, ") {")
			g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must be a valid IP address\")")
			g.P(indent, "}")
		}

		// String prefix/suffix/contains.
		if isString && vc.Prefix != "" {
			stringsHasPrefix := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "strings",
				GoName:       "HasPrefix",
			})
			g.P(indent, "if !", stringsHasPrefix, "(", localVar, ", ", strconv.Quote(vc.Prefix), ") {")
			g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must start with %s\", ", strconv.Quote(vc.Prefix), ")")
			g.P(indent, "}")
		}
		if isString && vc.Suffix != "" {
			stringsSuffix := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "strings",
				GoName:       "HasSuffix",
			})
			g.P(indent, "if !", stringsSuffix, "(", localVar, ", ", strconv.Quote(vc.Suffix), ") {")
			g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must end with %s\", ", strconv.Quote(vc.Suffix), ")")
			g.P(indent, "}")
		}
		if isString && vc.Contains != "" {
			stringsContains := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "strings",
				GoName:       "Contains",
			})
			g.P(indent, "if !", stringsContains, "(", localVar, ", ", strconv.Quote(vc.Contains), ") {")
			g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must contain %s\", ", strconv.Quote(vc.Contains), ")")
			g.P(indent, "}")
		}

		// Const.
		if vc.Const != nil {
			g.P(indent, "if ", localVar, " != ", *vc.Const, " {")
			g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must be exactly %v\", ", *vc.Const, ")")
			g.P(indent, "}")
		}

		// In.
		if len(vc.In) > 0 {
			goEmitInCheck(g, indent, localVar, f.Name, vc.In, fmtErrorf)
		}

		// NotIn.
		if len(vc.NotIn) > 0 {
			goEmitNotInCheck(g, indent, localVar, f.Name, vc.NotIn, fmtErrorf)
		}

		// Numeric/temporal range bounds.
		if vc.Gte != nil {
			goEmitBound(g, indent, localVar, f, ">=", "<", *vc.Gte, fmtErrorf)
		}
		if vc.Gt != nil {
			goEmitBound(g, indent, localVar, f, ">", "<=", *vc.Gt, fmtErrorf)
		}
		if vc.Lte != nil {
			goEmitBound(g, indent, localVar, f, "<=", ">", *vc.Lte, fmtErrorf)
		}
		if vc.Lt != nil {
			goEmitBound(g, indent, localVar, f, "<", ">=", *vc.Lt, fmtErrorf)
		}

		// Repeated constraints.
		if f.Repeated {
			if vc.MinItems != nil {
				g.P(indent, "if len(", fieldAccess, ") < ", *vc.MinItems, " {")
				g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must have at least %d items\", ", *vc.MinItems, ")")
				g.P(indent, "}")
			}
			if vc.MaxItems != nil {
				g.P(indent, "if len(", fieldAccess, ") > ", *vc.MaxItems, " {")
				g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must have at most %d items\", ", *vc.MaxItems, ")")
				g.P(indent, "}")
			}
			// F11: unique only makes sense for comparable types (scalars, strings).
			if vc.Unique && f.Kind != FieldKindMessage {
				g.P(indent, "{")
				g.P(indent, "\t_seen := make(map[any]bool, len(", fieldAccess, "))")
				g.P(indent, "\tfor _, _v := range ", fieldAccess, " {")
				g.P(indent, "\t\tif _seen[_v] {")
				g.P(indent, "\t\t\treturn ", fmtErrorf, "(\"", f.Name, ": items must be unique\")")
				g.P(indent, "\t\t}")
				g.P(indent, "\t\t_seen[_v] = true")
				g.P(indent, "\t}")
				g.P(indent, "}")
			}
		}

		// F13: Map constraints (min_pairs / max_pairs).
		if f.IsMap {
			if vc.MinItems != nil {
				g.P(indent, "if len(", fieldAccess, ") < ", *vc.MinItems, " {")
				g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must have at least %d entries\", ", *vc.MinItems, ")")
				g.P(indent, "}")
			}
			if vc.MaxItems != nil {
				g.P(indent, "if len(", fieldAccess, ") > ", *vc.MaxItems, " {")
				g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must have at most %d entries\", ", *vc.MaxItems, ")")
				g.P(indent, "}")
			}
		}

		// Close pointer nil-guard block.
		if isPointer && hasNonRequiredGoConstraints(vc) {
			g.P("\t}")
		}
	}
}

// goEmitNativeNestedChecks emits recursive Validate() calls for nested message fields.
func goEmitNativeNestedChecks(g *protogen.GeneratedFile, dm *DomainMessage, recv string) {
	fmtErrorf := g.QualifiedGoIdent(protogen.GoIdent{
		GoImportPath: "fmt",
		GoName:       "Errorf",
	})

	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		// Only recurse into actual message fields (not WKTs like Timestamp → time.Time).
		if f.Kind != FieldKindMessage {
			continue
		}

		fieldAccess := recv + "." + f.PascalName
		if f.Repeated {
			g.P("\tfor _i, _v := range ", fieldAccess, " {")
			g.P("\t\tif _v != nil {")
			g.P("\t\t\tif err := _v.Validate(); err != nil {")
			g.P("\t\t\t\treturn ", fmtErrorf, "(\"", f.Name, "[%d]: %w\", _i, err)")
			g.P("\t\t\t}")
			g.P("\t\t}")
			g.P("\t}")
		} else {
			// Singular message field (always a pointer in Go).
			g.P("\tif ", fieldAccess, " != nil {")
			g.P("\t\tif err := ", fieldAccess, ".Validate(); err != nil {")
			g.P("\t\t\treturn ", fmtErrorf, "(\"", f.Name, ": %w\", err)")
			g.P("\t\t}")
			g.P("\t}")
		}
	}
}

// goEmitBound emits a numeric/temporal comparison check.
// F15: Properly qualify time.RFC3339Nano and handle parse errors.
func goEmitBound(g *protogen.GeneratedFile, indent, localVar string, f *DomainField, op, negOp, value, fmtErrorf string) {
	if f.Kind == FieldKindTimestamp {
		// Timestamp fields are time.Time in Go.
		timeParse := g.QualifiedGoIdent(protogen.GoIdent{
			GoImportPath: "time",
			GoName:       "Parse",
		})
		timeRFC3339Nano := g.QualifiedGoIdent(protogen.GoIdent{
			GoImportPath: "time",
			GoName:       "RFC3339Nano",
		})
		g.P(indent, "{")
		g.P(indent, "\t_bound, _err := ", timeParse, "(", timeRFC3339Nano, ", ", strconv.Quote(value), ")")
		g.P(indent, "\tif _err != nil {")
		g.P(indent, "\t\treturn ", fmtErrorf, "(\"", f.Name, ": invalid bound %s: %w\", ", strconv.Quote(value), ", _err)")
		g.P(indent, "\t}")
		switch op {
		case ">=":
			g.P(indent, "\tif ", localVar, ".Before(_bound) {")
		case ">":
			g.P(indent, "\tif !", localVar, ".After(_bound) {")
		case "<=":
			g.P(indent, "\tif ", localVar, ".After(_bound) {")
		case "<":
			g.P(indent, "\tif !", localVar, ".Before(_bound) {")
		}
		g.P(indent, "\t\treturn ", fmtErrorf, "(\"", f.Name, ": must be ", op, " ", value, "\")")
		g.P(indent, "\t}")
		g.P(indent, "}")
	} else {
		// Numeric: emit direct comparison.
		g.P(indent, "if ", localVar, " ", negOp, " ", value, " {")
		g.P(indent, "\treturn ", fmtErrorf, "(\"", f.Name, ": must be ", op, " ", value, "\")")
		g.P(indent, "}")
	}
}

// goEmitInCheck emits a membership check. F14: fmtErrorf typed as string.
func goEmitInCheck(g *protogen.GeneratedFile, indent, localVar, fieldName string, values []string, fmtErrorf string) {
	g.P(indent, "switch ", localVar, " {")
	g.P(indent, "case ", strings.Join(values, ", "), ":")
	g.P(indent, "\t// valid")
	g.P(indent, "default:")
	g.P(indent, "\treturn ", fmtErrorf, "(\"", fieldName, ": must be one of [", strings.Join(values, ", "), "]\")")
	g.P(indent, "}")
}

// goEmitNotInCheck emits a not-in membership check. F14: fmtErrorf typed as string.
func goEmitNotInCheck(g *protogen.GeneratedFile, indent, localVar, fieldName string, values []string, fmtErrorf string) {
	g.P(indent, "switch ", localVar, " {")
	g.P(indent, "case ", strings.Join(values, ", "), ":")
	g.P(indent, "\treturn ", fmtErrorf, "(\"", fieldName, ": must not be one of [", strings.Join(values, ", "), "]\")")
	g.P(indent, "}")
}

// hasNonRequiredGoConstraints returns true if there are non-required constraints that need the value.
// F16: Removed IP and IgnoreEmpty/DefinedOnly from this check — IP now emits a real check.
func hasNonRequiredGoConstraints(vc *ValidateConstraints) bool {
	if vc == nil {
		return false
	}
	return vc.Email || vc.URI || vc.UUID || vc.Hostname || vc.IP ||
		vc.Pattern != "" ||
		vc.MinLength != nil || vc.MaxLength != nil || vc.Len != nil ||
		vc.Prefix != "" || vc.Suffix != "" || vc.Contains != "" ||
		vc.Const != nil || len(vc.In) > 0 || len(vc.NotIn) > 0 ||
		vc.Gte != nil || vc.Gt != nil || vc.Lte != nil || vc.Lt != nil ||
		vc.MinItems != nil || vc.MaxItems != nil || vc.Unique
}
