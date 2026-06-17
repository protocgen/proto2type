package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// generateGoClone generates a Clone method that returns a deep copy of the domain struct.
func generateGoClone(g *protogen.GeneratedFile, msg *protogen.Message) {
	name := msg.GoIdent.GoName
	recv := receiverName(name)

	g.P("// Clone returns a deep copy of ", name, ".")
	g.P("func (", recv, " *", name, ") Clone() *", name, " {")
	g.P("\tif ", recv, " == nil {")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tc := &", name, "{")

	// Direct-copy fields in the struct literal
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		fieldName := toPascalCase(string(field.Desc.Name()))

		// Skip fields that need deep copy — handle below
		if field.Desc.IsList() || field.Desc.IsMap() || field.Desc.Kind() == protoreflect.BytesKind || isNestedMessage(field) {
			continue
		}

		// Optional scalars and wrapper pointers: need to copy value, not pointer
		if field.Desc.HasOptionalKeyword() || isWellKnownWrapper(field) {
			continue
		}

		g.P("\t\t", fieldName, ": ", recv, ".", fieldName, ",")
	}
	g.P("\t}")

	// Deep copy pointer fields (optional scalars, wrapper types)
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		if field.Desc.HasOptionalKeyword() || isWellKnownWrapper(field) {
			fieldName := toPascalCase(string(field.Desc.Name()))
			g.P("\tif ", recv, ".", fieldName, " != nil {")
			g.P("\t\tv := *", recv, ".", fieldName)
			g.P("\t\tc.", fieldName, " = &v")
			g.P("\t}")
		}
	}

	// Deep copy slices (repeated fields) — with special handling for message/bytes elements
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		if !field.Desc.IsList() {
			continue
		}
		fieldName := toPascalCase(string(field.Desc.Name()))
		g.P("\tif ", recv, ".", fieldName, " != nil {")
		g.P("\t\tc.", fieldName, " = make(", goDomainFieldType(field), ", len(", recv, ".", fieldName, "))")
		if field.Desc.Kind() == protoreflect.MessageKind {
			// Repeated messages: clone each element
			g.P("\t\tfor i, v := range ", recv, ".", fieldName, " {")
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\tc.", fieldName, "[i] = v.Clone()")
			g.P("\t\t\t}")
			g.P("\t\t}")
		} else if field.Desc.Kind() == protoreflect.BytesKind {
			// Repeated bytes: deep copy each element
			g.P("\t\tfor i, v := range ", recv, ".", fieldName, " {")
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\tc.", fieldName, "[i] = make([]byte, len(v))")
			g.P("\t\t\t\tcopy(c.", fieldName, "[i], v)")
			g.P("\t\t\t}")
			g.P("\t\t}")
		} else {
			// Scalar slices: copy is sufficient
			g.P("\t\tcopy(c.", fieldName, ", ", recv, ".", fieldName, ")")
		}
		g.P("\t}")
	}

	// Deep copy singular bytes fields (not repeated — those handled above)
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		if field.Desc.Kind() != protoreflect.BytesKind || field.Desc.IsList() {
			continue
		}
		fieldName := toPascalCase(string(field.Desc.Name()))
		g.P("\tif ", recv, ".", fieldName, " != nil {")
		g.P("\t\tc.", fieldName, " = make([]byte, len(", recv, ".", fieldName, "))")
		g.P("\t\tcopy(c.", fieldName, ", ", recv, ".", fieldName, ")")
		g.P("\t}")
	}

	// Deep copy maps — with special handling for message/bytes values
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		if !field.Desc.IsMap() {
			continue
		}
		fieldName := toPascalCase(string(field.Desc.Name()))
		keyType := goType(field.Desc.MapKey().Kind())
		valType := goType(field.Desc.MapValue().Kind())
		if field.Desc.MapValue().Kind() == protoreflect.MessageKind {
			valType = "*" + toPascalCase(string(field.Desc.MapValue().Message().Name()))
		}
		g.P("\tif ", recv, ".", fieldName, " != nil {")
		g.P("\t\tc.", fieldName, " = make(map[", keyType, "]", valType, ", len(", recv, ".", fieldName, "))")
		g.P("\t\tfor k, v := range ", recv, ".", fieldName, " {")
		if field.Desc.MapValue().Kind() == protoreflect.MessageKind {
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\tc.", fieldName, "[k] = v.Clone()")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tc.", fieldName, "[k] = nil")
			g.P("\t\t\t}")
		} else if field.Desc.MapValue().Kind() == protoreflect.BytesKind {
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\tbuf := make([]byte, len(v))")
			g.P("\t\t\t\tcopy(buf, v)")
			g.P("\t\t\t\tc.", fieldName, "[k] = buf")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tc.", fieldName, "[k] = nil")
			g.P("\t\t\t}")
		} else {
			g.P("\t\t\tc.", fieldName, "[k] = v")
		}
		g.P("\t\t}")
		g.P("\t}")
	}

	// Deep copy nested messages
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		if !isNestedMessage(field) {
			continue
		}
		fieldName := toPascalCase(string(field.Desc.Name()))
		g.P("\tif ", recv, ".", fieldName, " != nil {")
		g.P("\t\tc.", fieldName, " = ", recv, ".", fieldName, ".Clone()")
		g.P("\t}")
	}

	g.P("\treturn c")
	g.P("}")
	g.P()
}

// generateGoEqual generates an Equal method that compares two domain structs field-by-field.
func generateGoEqual(g *protogen.GeneratedFile, msg *protogen.Message) {
	name := msg.GoIdent.GoName
	recv := receiverName(name)

	g.P("// Equal reports whether ", recv, " and other are equal.")
	g.P("func (", recv, " *", name, ") Equal(other *", name, ") bool {")
	g.P("\tif ", recv, " == other {")
	g.P("\t\treturn true")
	g.P("\t}")
	g.P("\tif ", recv, " == nil || other == nil {")
	g.P("\t\treturn false")
	g.P("\t}")

	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		fieldName := toPascalCase(string(field.Desc.Name()))

		if isNestedMessage(field) {
			// Nested message: nil check + recursive Equal
			g.P("\tif (", recv, ".", fieldName, " == nil) != (other.", fieldName, " == nil) {")
			g.P("\t\treturn false")
			g.P("\t}")
			g.P("\tif ", recv, ".", fieldName, " != nil && !", recv, ".", fieldName, ".Equal(other.", fieldName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
		} else if field.Desc.Kind() == protoreflect.BytesKind && !field.Desc.IsList() {
			// Singular bytes: length + element comparison
			g.P("\tif len(", recv, ".", fieldName, ") != len(other.", fieldName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
			g.P("\tfor i := range ", recv, ".", fieldName, " {")
			g.P("\t\tif ", recv, ".", fieldName, "[i] != other.", fieldName, "[i] {")
			g.P("\t\t\treturn false")
			g.P("\t\t}")
			g.P("\t}")
		} else if field.Desc.IsList() {
			// Repeated: compare length then elements
			g.P("\tif len(", recv, ".", fieldName, ") != len(other.", fieldName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
			g.P("\tfor i := range ", recv, ".", fieldName, " {")
			if field.Desc.Kind() == protoreflect.MessageKind {
				// Repeated messages: nil check + recursive Equal
				g.P("\t\tif (", recv, ".", fieldName, "[i] == nil) != (other.", fieldName, "[i] == nil) {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
				g.P("\t\tif ", recv, ".", fieldName, "[i] != nil && !", recv, ".", fieldName, "[i].Equal(other.", fieldName, "[i]) {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
			} else if field.Desc.Kind() == protoreflect.BytesKind {
				// Repeated bytes: length + element comparison for each
				g.P("\t\tif len(", recv, ".", fieldName, "[i]) != len(other.", fieldName, "[i]) {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
				g.P("\t\tfor j := range ", recv, ".", fieldName, "[i] {")
				g.P("\t\t\tif ", recv, ".", fieldName, "[i][j] != other.", fieldName, "[i][j] {")
				g.P("\t\t\t\treturn false")
				g.P("\t\t\t}")
				g.P("\t\t}")
			} else {
				// Scalar elements: ==
				g.P("\t\tif ", recv, ".", fieldName, "[i] != other.", fieldName, "[i] {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
			}
			g.P("\t}")
		} else if field.Desc.IsMap() {
			// Map: compare length then key-value pairs
			g.P("\tif len(", recv, ".", fieldName, ") != len(other.", fieldName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
			g.P("\tfor k, v := range ", recv, ".", fieldName, " {")
			g.P("\t\tov, ok := other.", fieldName, "[k]")
			g.P("\t\tif !ok {")
			g.P("\t\t\treturn false")
			g.P("\t\t}")
			if field.Desc.MapValue().Kind() == protoreflect.MessageKind {
				g.P("\t\tif (v == nil) != (ov == nil) {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
				g.P("\t\tif v != nil && !v.Equal(ov) {")
				g.P("\t\t\treturn false")
				g.P("\t\t}")
			} else if field.Desc.MapValue().Kind() == protoreflect.BytesKind {
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
		} else if field.Desc.HasOptionalKeyword() || isWellKnownWrapper(field) {
			// Pointer fields: compare nil-ness then deref
			g.P("\tif (", recv, ".", fieldName, " == nil) != (other.", fieldName, " == nil) {")
			g.P("\t\treturn false")
			g.P("\t}")
			g.P("\tif ", recv, ".", fieldName, " != nil && *", recv, ".", fieldName, " != *other.", fieldName, " {")
			g.P("\t\treturn false")
			g.P("\t}")
		} else if isWellKnownTimestamp(field) {
			// time.Time: use .Equal() for monotonic clock safety
			g.P("\tif !", recv, ".", fieldName, ".Equal(other.", fieldName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
		} else {
			// Scalars, enums, durations: ==
			g.P("\tif ", recv, ".", fieldName, " != other.", fieldName, " {")
			g.P("\t\treturn false")
			g.P("\t}")
		}
	}

	g.P("\treturn true")
	g.P("}")
	g.P()
}
