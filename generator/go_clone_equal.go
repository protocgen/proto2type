package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

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
			g.P("\tif ", recv, ".", f.PascalName, " != nil {")
			g.P("\t\tv := *", recv, ".", f.PascalName)
			g.P("\t\tc.", f.PascalName, " = &v")
			g.P("\t}")
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
		} else {
			// Scalar slices: copy is sufficient
			g.P("\t\tcopy(c.", f.PascalName, ", ", recv, ".", f.PascalName, ")")
		}
		g.P("\t}")
	}

	// Deep copy singular bytes fields (not repeated — those handled above)
	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		if f.Kind != FieldKindScalar || f.ScalarKind != protoreflect.BytesKind || f.Repeated {
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
			// []string: make + copy
			g.P("\tif ", recv, ".", f.PascalName, " != nil {")
			g.P("\t\tc.", f.PascalName, " = make([]string, len(", recv, ".", f.PascalName, "))")
			g.P("\t\tcopy(c.", f.PascalName, ", ", recv, ".", f.PascalName, ")")
			g.P("\t}")
		case FieldKindListValue:
			// []any: make + copy
			g.P("\tif ", recv, ".", f.PascalName, " != nil {")
			g.P("\t\tc.", f.PascalName, " = make([]any, len(", recv, ".", f.PascalName, "))")
			g.P("\t\tcopy(c.", f.PascalName, ", ", recv, ".", f.PascalName, ")")
			g.P("\t}")
		case FieldKindStruct:
			// map[string]any: iterate and copy
			g.P("\tif ", recv, ".", f.PascalName, " != nil {")
			g.P("\t\tc.", f.PascalName, " = make(map[string]any, len(", recv, ".", f.PascalName, "))")
			g.P("\t\tfor k, v := range ", recv, ".", f.PascalName, " {")
			g.P("\t\t\tc.", f.PascalName, "[k] = v")
			g.P("\t\t}")
			g.P("\t}")
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
		} else if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind && !f.Repeated {
			// Singular bytes: length + element comparison
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
			g.P("\tif ", recv, ".", f.PascalName, " != nil && *", recv, ".", f.PascalName, " != *other.", f.PascalName, " {")
			g.P("\t\treturn false")
			g.P("\t}")
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
			// []any: compare length then elements
			g.P("\tif len(", recv, ".", f.PascalName, ") != len(other.", f.PascalName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
			g.P("\tfor i := range ", recv, ".", f.PascalName, " {")
			g.P("\t\tif ", recv, ".", f.PascalName, "[i] != other.", f.PascalName, "[i] {")
			g.P("\t\t\treturn false")
			g.P("\t\t}")
			g.P("\t}")
		} else if f.Kind == FieldKindStruct {
			// map[string]any: compare length then key-value pairs
			g.P("\tif len(", recv, ".", f.PascalName, ") != len(other.", f.PascalName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
			g.P("\tfor k, v := range ", recv, ".", f.PascalName, " {")
			g.P("\t\tov, ok := other.", f.PascalName, "[k]")
			g.P("\t\tif !ok {")
			g.P("\t\t\treturn false")
			g.P("\t\t}")
			g.P("\t\tif v != ov {")
			g.P("\t\t\treturn false")
			g.P("\t\t}")
			g.P("\t}")
		} else {
			// Scalars, enums, durations: ==
			g.P("\tif ", recv, ".", f.PascalName, " != other.", f.PascalName, " {")
			g.P("\t\treturn false")
			g.P("\t}")
		}
	}

	g.P("\treturn true")
	g.P("}")
	g.P()
}
