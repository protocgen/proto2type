package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// irNeedsDeepCopyHelper returns true if any message has FieldKindStruct, FieldKindListValue,
// or FieldKindValue fields that need the deepCopyValue helper for correct deep copying.
func irNeedsDeepCopyHelper(msgs []*DomainMessage) bool {
	for _, m := range msgs {
		for _, f := range m.Fields {
			if f.Kind == FieldKindStruct || f.Kind == FieldKindListValue || f.Kind == FieldKindValue {
				return true
			}
			if f.IsMap && f.MapValue != nil && (f.MapValue.Kind == FieldKindStruct || f.MapValue.Kind == FieldKindListValue || f.MapValue.Kind == FieldKindValue) {
				return true
			}
		}
		if irNeedsDeepCopyHelper(m.NestedMessages) {
			return true
		}
	}
	return false
}

// generateGoDeepCopyHelper emits the deepCopyValue helper function used by Clone
// to recursively deep-copy map[string]any, []any, and any values from structpb.
func generateGoDeepCopyHelper(g *protogen.GeneratedFile) {
	g.P("// deepCopyValue recursively deep-copies values that originate from structpb")
	g.P("// (map[string]any, []any, and scalar types like string, float64, bool, nil).")
	g.P("func deepCopyValue(v any) any {")
	g.P("\tswitch val := v.(type) {")
	g.P("\tcase map[string]any:")
	g.P("\t\tm := make(map[string]any, len(val))")
	g.P("\t\tfor k, v := range val {")
	g.P("\t\t\tm[k] = deepCopyValue(v)")
	g.P("\t\t}")
	g.P("\t\treturn m")
	g.P("\tcase []any:")
	g.P("\t\ts := make([]any, len(val))")
	g.P("\t\tfor i, v := range val {")
	g.P("\t\t\ts[i] = deepCopyValue(v)")
	g.P("\t\t}")
	g.P("\t\treturn s")
	g.P("\tdefault:")
	g.P("\t\treturn v // scalars (string, float64, bool, nil) are immutable")
	g.P("\t}")
	g.P("}")
	g.P()
}

// generateGoClone generates a Clone method that returns a deep copy of the domain struct.
func generateGoClone(g *protogen.GeneratedFile, dm *DomainMessage) {
	name := dm.Name
	recv := receiverName(name)

	g.P("// Clone returns a deep copy of ", name, ".")
	g.P("func (", recv, " *", name, ") Clone() *", name, " {")
	g.P("\tif ", recv, " == nil {")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tc := &", name, "{")

	// Direct-copy fields in the struct literal
	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}

		// Skip fields that need deep copy — handle below
		if f.Repeated || f.IsMap || (f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind) || (f.Kind == FieldKindMessage && !f.Repeated && !f.IsMap) {
			continue
		}

		// Skip FieldKindAny and FieldKindValue — need deep copy below
		if f.Kind == FieldKindAny || f.Kind == FieldKindValue {
			continue
		}

		// Skip WKTs that map to reference types (slices/maps)
		if f.Kind == FieldKindStruct || f.Kind == FieldKindListValue || f.Kind == FieldKindFieldMask {
			continue
		}

		// Optional scalars and wrapper pointers: need to copy value, not pointer
		if f.Optional || f.Kind.IsWrapper() {
			continue
		}

		g.P("\t\t", f.PascalName, ": ", recv, ".", f.PascalName, ",")
	}
	g.P("\t}")

	// Deep copy pointer fields (optional scalars, wrapper types)
	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		if f.Optional || f.Kind.IsWrapper() {
			// Special case: optional bytes needs deep copy of the underlying slice
			if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind {
				g.P("\tif ", recv, ".", f.PascalName, " != nil {")
				g.P("\t\tb := make([]byte, len(*", recv, ".", f.PascalName, "))")
				g.P("\t\tcopy(b, *", recv, ".", f.PascalName, ")")
				g.P("\t\tc.", f.PascalName, " = &b")
				g.P("\t}")
			} else if f.Kind == FieldKindWrapperBytes {
				// *[]byte wrapper: deep copy the underlying bytes
				g.P("\tif ", recv, ".", f.PascalName, " != nil {")
				g.P("\t\tb := make([]byte, len(*", recv, ".", f.PascalName, "))")
				g.P("\t\tcopy(b, *", recv, ".", f.PascalName, ")")
				g.P("\t\tc.", f.PascalName, " = &b")
				g.P("\t}")
			} else {
				g.P("\tif ", recv, ".", f.PascalName, " != nil {")
				g.P("\t\tv := *", recv, ".", f.PascalName)
				g.P("\t\tc.", f.PascalName, " = &v")
				g.P("\t}")
			}
		}
	}

	// Deep copy slices (repeated fields) — with special handling for message/bytes elements
	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		if !f.Repeated {
			continue
		}
		g.P("\tif ", recv, ".", f.PascalName, " != nil {")
		g.P("\t\tc.", f.PascalName, " = make(", goDomainFieldTypeFromIR(f), ", len(", recv, ".", f.PascalName, "))")
		if f.Kind == FieldKindMessage {
			// Repeated messages: clone each element
			g.P("\t\tfor i, v := range ", recv, ".", f.PascalName, " {")
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\tc.", f.PascalName, "[i] = v.Clone()")
			g.P("\t\t\t}")
			g.P("\t\t}")
		} else if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind {
			// Repeated bytes: deep copy each element
			g.P("\t\tfor i, v := range ", recv, ".", f.PascalName, " {")
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\tc.", f.PascalName, "[i] = make([]byte, len(v))")
			g.P("\t\t\t\tcopy(c.", f.PascalName, "[i], v)")
			g.P("\t\t\t}")
			g.P("\t\t}")
		} else if f.Kind == FieldKindFieldMask {
			// Repeated FieldMask ([][]string): deep copy each element
			g.P("\t\tfor i, v := range ", recv, ".", f.PascalName, " {")
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\tc.", f.PascalName, "[i] = make([]string, len(v))")
			g.P("\t\t\t\tcopy(c.", f.PascalName, "[i], v)")
			g.P("\t\t\t}")
			g.P("\t\t}")
		} else if f.Kind == FieldKindStruct {
			// Repeated Struct ([]map[string]any): deep copy each element
			g.P("\t\tfor i, v := range ", recv, ".", f.PascalName, " {")
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\tc.", f.PascalName, "[i] = deepCopyValue(v).(map[string]any)")
			g.P("\t\t\t}")
			g.P("\t\t}")
		} else if f.Kind == FieldKindListValue {
			// Repeated ListValue ([][]any): deep copy each element
			g.P("\t\tfor i, v := range ", recv, ".", f.PascalName, " {")
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\tc.", f.PascalName, "[i] = deepCopyValue(v).([]any)")
			g.P("\t\t\t}")
			g.P("\t\t}")
		} else if f.Kind == FieldKindAny {
			// Repeated Any ([]any): deep copy via proto.Clone for proto.Message values
			protoClone := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/proto", GoName: "Clone"})
			protoMessage := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/proto", GoName: "Message"})
			g.P("\t\tfor i, v := range ", recv, ".", f.PascalName, " {")
			g.P("\t\t\tif m, ok := v.(", protoMessage, "); ok {")
			g.P("\t\t\t\tc.", f.PascalName, "[i] = ", protoClone, "(m)")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tc.", f.PascalName, "[i] = v")
			g.P("\t\t\t}")
			g.P("\t\t}")
		} else if f.Kind == FieldKindValue {
			// Repeated Value ([]any): deep copy each element
			g.P("\t\tfor i, v := range ", recv, ".", f.PascalName, " {")
			g.P("\t\t\tc.", f.PascalName, "[i] = deepCopyValue(v)")
			g.P("\t\t}")
		} else if f.Kind.IsWrapper() {
			// Repeated wrapper (e.g. []*string): deep copy each pointer
			g.P("\t\tfor i, v := range ", recv, ".", f.PascalName, " {")
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\tcpy := *v")
			g.P("\t\t\t\tc.", f.PascalName, "[i] = &cpy")
			g.P("\t\t\t}")
			g.P("\t\t}")
		} else {
			// Scalar slices: copy is sufficient
			g.P("\t\tcopy(c.", f.PascalName, ", ", recv, ".", f.PascalName, ")")
		}
		g.P("\t}")
	}

	// Deep copy singular bytes fields (not repeated or optional — those handled above)
	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		if f.Kind != FieldKindScalar || f.ScalarKind != protoreflect.BytesKind || f.Repeated || f.Optional {
			continue
		}
		g.P("\tif ", recv, ".", f.PascalName, " != nil {")
		g.P("\t\tc.", f.PascalName, " = make([]byte, len(", recv, ".", f.PascalName, "))")
		g.P("\t\tcopy(c.", f.PascalName, ", ", recv, ".", f.PascalName, ")")
		g.P("\t}")
	}

	// Deep copy maps — with special handling for message/bytes values
	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		if !f.IsMap {
			continue
		}
		keyType := goType(f.MapKey.ScalarKind)
		valType := goMapValueTypeFromIR(f.MapValue)
		g.P("\tif ", recv, ".", f.PascalName, " != nil {")
		g.P("\t\tc.", f.PascalName, " = make(map[", keyType, "]", valType, ", len(", recv, ".", f.PascalName, "))")
		g.P("\t\tfor k, v := range ", recv, ".", f.PascalName, " {")
		if f.MapValue != nil && f.MapValue.Kind == FieldKindMessage {
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\tc.", f.PascalName, "[k] = v.Clone()")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tc.", f.PascalName, "[k] = nil")
			g.P("\t\t\t}")
		} else if f.MapValue != nil && f.MapValue.ScalarKind == protoreflect.BytesKind {
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\tbuf := make([]byte, len(v))")
			g.P("\t\t\t\tcopy(buf, v)")
			g.P("\t\t\t\tc.", f.PascalName, "[k] = buf")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tc.", f.PascalName, "[k] = nil")
			g.P("\t\t\t}")
		} else if f.MapValue != nil && f.MapValue.Kind == FieldKindStruct {
			// map value is map[string]any: deep copy via deepCopyValue
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\tc.", f.PascalName, "[k] = deepCopyValue(v).(map[string]any)")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tc.", f.PascalName, "[k] = nil")
			g.P("\t\t\t}")
		} else if f.MapValue != nil && f.MapValue.Kind == FieldKindListValue {
			// map value is []any: deep copy via deepCopyValue
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\tc.", f.PascalName, "[k] = deepCopyValue(v).([]any)")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tc.", f.PascalName, "[k] = nil")
			g.P("\t\t\t}")
		} else if f.MapValue != nil && f.MapValue.Kind == FieldKindValue {
			// map value is any: deep copy via deepCopyValue
			g.P("\t\t\tc.", f.PascalName, "[k] = deepCopyValue(v)")
		} else if f.MapValue != nil && f.MapValue.Kind == FieldKindAny {
			// map value is any holding proto.Message: deep copy via proto.Clone
			protoClone := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/proto", GoName: "Clone"})
			protoMessage := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/proto", GoName: "Message"})
			g.P("\t\t\tif m, ok := v.(", protoMessage, "); ok {")
			g.P("\t\t\t\tc.", f.PascalName, "[k] = ", protoClone, "(m)")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tc.", f.PascalName, "[k] = v")
			g.P("\t\t\t}")
		} else if f.MapValue != nil && f.MapValue.Kind == FieldKindFieldMask {
			// map value is []string: copy the slice
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\ts := make([]string, len(v))")
			g.P("\t\t\t\tcopy(s, v)")
			g.P("\t\t\t\tc.", f.PascalName, "[k] = s")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tc.", f.PascalName, "[k] = nil")
			g.P("\t\t\t}")
		} else {
			g.P("\t\t\tc.", f.PascalName, "[k] = v")
		}
		g.P("\t\t}")
		g.P("\t}")
	}

	// Deep copy nested messages
	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		if f.Kind != FieldKindMessage || f.Repeated || f.IsMap {
			continue
		}
		g.P("\tif ", recv, ".", f.PascalName, " != nil {")
		g.P("\t\tc.", f.PascalName, " = ", recv, ".", f.PascalName, ".Clone()")
		g.P("\t}")
	}

	// Deep copy WKT reference types (slices and maps)
	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		switch f.Kind {
		case FieldKindFieldMask:
			if f.Repeated || f.IsMap {
				continue // handled by repeated/map loops
			}
			// []string: make + copy
			g.P("\tif ", recv, ".", f.PascalName, " != nil {")
			g.P("\t\tc.", f.PascalName, " = make([]string, len(", recv, ".", f.PascalName, "))")
			g.P("\t\tcopy(c.", f.PascalName, ", ", recv, ".", f.PascalName, ")")
			g.P("\t}")
		case FieldKindListValue:
			if f.Repeated || f.IsMap {
				continue
			}
			// []any: deep copy via deepCopyValue
			g.P("\tif ", recv, ".", f.PascalName, " != nil {")
			g.P("\t\tc.", f.PascalName, " = deepCopyValue(", recv, ".", f.PascalName, ").([]any)")
			g.P("\t}")
		case FieldKindStruct:
			if f.Repeated || f.IsMap {
				continue
			}
			// map[string]any: deep copy via deepCopyValue
			g.P("\tif ", recv, ".", f.PascalName, " != nil {")
			g.P("\t\tc.", f.PascalName, " = deepCopyValue(", recv, ".", f.PascalName, ").(map[string]any)")
			g.P("\t}")
		case FieldKindValue:
			if f.Repeated || f.IsMap {
				continue
			}
			// any: deep copy via deepCopyValue
			g.P("\tc.", f.PascalName, " = deepCopyValue(", recv, ".", f.PascalName, ")")
		case FieldKindAny:
			if f.Repeated || f.IsMap {
				continue
			}
			// any holding proto.Message: deep copy via proto.Clone
			protoClone := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/proto", GoName: "Clone"})
			protoMessage := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/proto", GoName: "Message"})
			g.P("\tif ", recv, ".", f.PascalName, " != nil {")
			g.P("\t\tif m, ok := ", recv, ".", f.PascalName, ".(", protoMessage, "); ok {")
			g.P("\t\t\tc.", f.PascalName, " = ", protoClone, "(m)")
			g.P("\t\t} else {")
			g.P("\t\t\tc.", f.PascalName, " = ", recv, ".", f.PascalName)
			g.P("\t\t}")
			g.P("\t}")
		}
	}

	// Deep copy oneof variant pointer fields
	for _, f := range dm.Fields {
		if !f.IsOneof {
			continue
		}
		oneof := findOneof(dm, f.OneofTypeName)
		for _, v := range oneof.Variants {
			switch v.Kind {
			case FieldKindMessage:
				g.P("\tif ", recv, ".", v.Name, " != nil {")
				g.P("\t\tc.", v.Name, " = ", recv, ".", v.Name, ".Clone()")
				g.P("\t}")
			case FieldKindScalar:
				g.P("\tif ", recv, ".", v.Name, " != nil {")
				g.P("\t\tv := *", recv, ".", v.Name)
				g.P("\t\tc.", v.Name, " = &v")
				g.P("\t}")
			case FieldKindStruct:
				// *map[string]any: deep copy via deepCopyValue
				g.P("\tif ", recv, ".", v.Name, " != nil {")
				g.P("\t\tval := deepCopyValue(*", recv, ".", v.Name, ").(map[string]any)")
				g.P("\t\tc.", v.Name, " = &val")
				g.P("\t}")
			case FieldKindValue:
				// *any: deep copy via deepCopyValue
				g.P("\tif ", recv, ".", v.Name, " != nil {")
				g.P("\t\tval := deepCopyValue(*", recv, ".", v.Name, ")")
				g.P("\t\tc.", v.Name, " = &val")
				g.P("\t}")
			case FieldKindListValue:
				// *[]any: deep copy via deepCopyValue
				g.P("\tif ", recv, ".", v.Name, " != nil {")
				g.P("\t\tval := deepCopyValue(*", recv, ".", v.Name, ").([]any)")
				g.P("\t\tc.", v.Name, " = &val")
				g.P("\t}")
			case FieldKindFieldMask:
				// *[]string: make + copy the slice
				g.P("\tif ", recv, ".", v.Name, " != nil {")
				g.P("\t\ts := make([]string, len(*", recv, ".", v.Name, "))")
				g.P("\t\tcopy(s, *", recv, ".", v.Name, ")")
				g.P("\t\tc.", v.Name, " = &s")
				g.P("\t}")
			case FieldKindAny:
				// *any: if proto.Message, proto.Clone; else deepCopyValue
				protoClone := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/proto", GoName: "Clone"})
				protoMessage := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/proto", GoName: "Message"})
				g.P("\tif ", recv, ".", v.Name, " != nil {")
				g.P("\t\tif m, ok := (*", recv, ".", v.Name, ").(" , protoMessage, "); ok {")
				g.P("\t\t\tval := any(", protoClone, "(m))")
				g.P("\t\t\tc.", v.Name, " = &val")
				g.P("\t\t} else {")
				g.P("\t\t\tval := deepCopyValue(*", recv, ".", v.Name, ")")
				g.P("\t\t\tc.", v.Name, " = &val")
				g.P("\t\t}")
				g.P("\t}")
			default:
				// Enum, Timestamp, Duration, etc. — all pointer types, copy value
				g.P("\tif ", recv, ".", v.Name, " != nil {")
				g.P("\t\tv := *", recv, ".", v.Name)
				g.P("\t\tc.", v.Name, " = &v")
				g.P("\t}")
			}
		}
	}

	g.P("\treturn c")
	g.P("}")
	g.P()
}

// generateGoEqual generates an Equal method that compares two domain structs field-by-field.
func generateGoEqual(g *protogen.GeneratedFile, dm *DomainMessage) {
	name := dm.Name
	recv := receiverName(name)

	g.P("// Equal reports whether ", recv, " and other are equal.")
	g.P("func (", recv, " *", name, ") Equal(other *", name, ") bool {")
	g.P("\tif ", recv, " == other {")
	g.P("\t\treturn true")
	g.P("\t}")
	g.P("\tif ", recv, " == nil || other == nil {")
	g.P("\t\treturn false")
	g.P("\t}")

	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}

		if f.Kind == FieldKindMessage && !f.Repeated && !f.IsMap {
			// Nested message: nil check + recursive Equal
			g.P("\tif (", recv, ".", f.PascalName, " == nil) != (other.", f.PascalName, " == nil) {")
			g.P("\t\treturn false")
			g.P("\t}")
			g.P("\tif ", recv, ".", f.PascalName, " != nil && !", recv, ".", f.PascalName, ".Equal(other.", f.PascalName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
		} else if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind && !f.Repeated && !f.Optional {
			// Singular bytes (non-optional): length + element comparison
			g.P("\tif len(", recv, ".", f.PascalName, ") != len(other.", f.PascalName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
			g.P("\tfor i := range ", recv, ".", f.PascalName, " {")
			g.P("\t\tif ", recv, ".", f.PascalName, "[i] != other.", f.PascalName, "[i] {")
			g.P("\t\t\treturn false")
			g.P("\t\t}")
			g.P("\t}")
		} else if f.Repeated {
			// Repeated: compare length then elements
			g.P("\tif len(", recv, ".", f.PascalName, ") != len(other.", f.PascalName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
			g.P("\tfor i := range ", recv, ".", f.PascalName, " {")
			if f.Kind == FieldKindMessage {
				// Repeated messages: nil check + recursive Equal
				g.P("\t\tif (", recv, ".", f.PascalName, "[i] == nil) != (other.", f.PascalName, "[i] == nil) {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
				g.P("\t\tif ", recv, ".", f.PascalName, "[i] != nil && !", recv, ".", f.PascalName, "[i].Equal(other.", f.PascalName, "[i]) {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
			} else if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind {
				// Repeated bytes: length + element comparison for each
				g.P("\t\tif len(", recv, ".", f.PascalName, "[i]) != len(other.", f.PascalName, "[i]) {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
				g.P("\t\tfor j := range ", recv, ".", f.PascalName, "[i] {")
				g.P("\t\t\tif ", recv, ".", f.PascalName, "[i][j] != other.", f.PascalName, "[i][j] {")
				g.P("\t\t\t\treturn false")
				g.P("\t\t\t}")
				g.P("\t\t}")
			} else if f.Kind == FieldKindFieldMask || f.Kind == FieldKindStruct || f.Kind == FieldKindListValue || f.Kind == FieldKindAny || f.Kind == FieldKindValue {
				// Repeated WKT reference types: use reflect.DeepEqual for non-comparable elements
				deepEqual := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "reflect", GoName: "DeepEqual"})
				g.P("\t\tif !", deepEqual, "(", recv, ".", f.PascalName, "[i], other.", f.PascalName, "[i]) {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
			} else if f.Kind.IsWrapper() {
				// Repeated wrapper pointers: dereference-compare
				g.P("\t\ta, b := ", recv, ".", f.PascalName, "[i], other.", f.PascalName, "[i]")
				g.P("\t\tif (a == nil) != (b == nil) {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
				g.P("\t\tif a != nil && *a != *b {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
			} else {
				// Scalar elements: ==
				g.P("\t\tif ", recv, ".", f.PascalName, "[i] != other.", f.PascalName, "[i] {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
			}
			g.P("\t}")
		} else if f.IsMap {
			// Map: compare length then key-value pairs
			g.P("\tif len(", recv, ".", f.PascalName, ") != len(other.", f.PascalName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
			g.P("\tfor k, v := range ", recv, ".", f.PascalName, " {")
			g.P("\t\tov, ok := other.", f.PascalName, "[k]")
			g.P("\t\tif !ok {")
			g.P("\t\t\treturn false")
			g.P("\t\t}")
			if f.MapValue != nil && f.MapValue.Kind == FieldKindMessage {
				g.P("\t\tif (v == nil) != (ov == nil) {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
				g.P("\t\tif v != nil && !v.Equal(ov) {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
			} else if f.MapValue != nil && f.MapValue.ScalarKind == protoreflect.BytesKind {
				g.P("\t\tif len(v) != len(ov) {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
				g.P("\t\tfor i := range v {")
				g.P("\t\t\tif v[i] != ov[i] {")
				g.P("\t\t\t\treturn false")
				g.P("\t\t\t}")
				g.P("\t\t}")
			} else if f.MapValue != nil && (f.MapValue.Kind == FieldKindStruct || f.MapValue.Kind == FieldKindListValue || f.MapValue.Kind == FieldKindFieldMask || f.MapValue.Kind == FieldKindAny || f.MapValue.Kind == FieldKindValue) {
				// WKT map values with non-comparable types: use reflect.DeepEqual
				deepEqual := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "reflect", GoName: "DeepEqual"})
				g.P("\t\tif !", deepEqual, "(v, ov) {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
			} else {
				g.P("\t\tif v != ov {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
			}
			g.P("\t}")
		} else if f.Optional || f.Kind.IsWrapper() {
			// Pointer fields: compare nil-ness then deref
			g.P("\tif (", recv, ".", f.PascalName, " == nil) != (other.", f.PascalName, " == nil) {")
			g.P("\t\treturn false")
			g.P("\t}")
			if (f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind) || f.Kind == FieldKindWrapperBytes {
				// *[]byte: deref then compare bytes
				g.P("\tif ", recv, ".", f.PascalName, " != nil {")
				g.P("\t\tif len(*", recv, ".", f.PascalName, ") != len(*other.", f.PascalName, ") {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
				g.P("\t\tfor i := range *", recv, ".", f.PascalName, " {")
				g.P("\t\t\tif (*", recv, ".", f.PascalName, ")[i] != (*other.", f.PascalName, ")[i] {")
				g.P("\t\t\t\treturn false")
				g.P("\t\t\t}")
				g.P("\t\t}")
				g.P("\t}")
			} else {
				g.P("\tif ", recv, ".", f.PascalName, " != nil && *", recv, ".", f.PascalName, " != *other.", f.PascalName, " {")
				g.P("\t\treturn false")
				g.P("\t}")
			}
		} else if f.Kind == FieldKindTimestamp {
			// time.Time: use .Equal() for monotonic clock safety
			g.P("\tif !", recv, ".", f.PascalName, ".Equal(other.", f.PascalName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
		} else if f.Kind == FieldKindFieldMask {
			// []string: compare length then elements
			g.P("\tif len(", recv, ".", f.PascalName, ") != len(other.", f.PascalName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
			g.P("\tfor i := range ", recv, ".", f.PascalName, " {")
			g.P("\t\tif ", recv, ".", f.PascalName, "[i] != other.", f.PascalName, "[i] {")
			g.P("\t\t\treturn false")
			g.P("\t\t}")
			g.P("\t}")
		} else if f.Kind == FieldKindListValue {
			// []any: use reflect.DeepEqual because any values can be non-comparable
			deepEqual := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "reflect", GoName: "DeepEqual"})
			g.P("\tif !", deepEqual, "(", recv, ".", f.PascalName, ", other.", f.PascalName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
		} else if f.Kind == FieldKindStruct {
			// map[string]any: use reflect.DeepEqual because any values can be non-comparable
			deepEqual := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "reflect", GoName: "DeepEqual"})
			g.P("\tif !", deepEqual, "(", recv, ".", f.PascalName, ", other.", f.PascalName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
		} else if f.Kind == FieldKindAny || f.Kind == FieldKindValue {
			// any fields: use reflect.DeepEqual since they may hold non-comparable types
			deepEqual := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "reflect", GoName: "DeepEqual"})
			g.P("\tif !", deepEqual, "(", recv, ".", f.PascalName, ", other.", f.PascalName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
		} else {
			// Scalars, enums, durations: ==
			g.P("\tif ", recv, ".", f.PascalName, " != other.", f.PascalName, " {")
			g.P("\t\treturn false")
			g.P("\t}")
		}
	}

	// Compare oneof variant pointer fields
	for _, f := range dm.Fields {
		if !f.IsOneof {
			continue
		}
		oneof := findOneof(dm, f.OneofTypeName)
		for _, v := range oneof.Variants {
			g.P("\tif (", recv, ".", v.Name, " == nil) != (other.", v.Name, " == nil) {")
			g.P("\t\treturn false")
			g.P("\t}")
			switch v.Kind {
			case FieldKindMessage:
				g.P("\tif ", recv, ".", v.Name, " != nil && !", recv, ".", v.Name, ".Equal(other.", v.Name, ") {")
				g.P("\t\treturn false")
				g.P("\t}")
			case FieldKindStruct, FieldKindValue, FieldKindListValue, FieldKindAny, FieldKindFieldMask:
				// Non-comparable pointer types: use reflect.DeepEqual
				deepEqual := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "reflect", GoName: "DeepEqual"})
				g.P("\tif ", recv, ".", v.Name, " != nil && !", deepEqual, "(", recv, ".", v.Name, ", other.", v.Name, ") {")
				g.P("\t\treturn false")
				g.P("\t}")
			default:
				g.P("\tif ", recv, ".", v.Name, " != nil && *", recv, ".", v.Name, " != *other.", v.Name, " {")
				g.P("\t\treturn false")
				g.P("\t}")
			}
		}
	}

	g.P("\treturn true")
	g.P("}")
	g.P()
}
