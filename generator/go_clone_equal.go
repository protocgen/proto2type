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

	// Deep copy slices (repeated fields)
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
		g.P("\t\tcopy(c.", fieldName, ", ", recv, ".", fieldName, ")")
		g.P("\t}")
	}

	// Deep copy bytes fields
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		if field.Desc.Kind() != protoreflect.BytesKind {
			continue
		}
		fieldName := toPascalCase(string(field.Desc.Name()))
		g.P("\tif ", recv, ".", fieldName, " != nil {")
		g.P("\t\tc.", fieldName, " = make([]byte, len(", recv, ".", fieldName, "))")
		g.P("\t\tcopy(c.", fieldName, ", ", recv, ".", fieldName, ")")
		g.P("\t}")
	}

	// Deep copy maps
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
		g.P("\t\t\tc.", fieldName, "[k] = v")
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
			// Nested message: recursive Equal
			g.P("\tif !", recv, ".", fieldName, ".Equal(other.", fieldName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
		} else if field.Desc.Kind() == protoreflect.BytesKind {
			// Bytes: use bytes.Equal — but to avoid an import for a simple check:
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
			g.P("\t\tif ", recv, ".", fieldName, "[i] != other.", fieldName, "[i] {")
			g.P("\t\t\treturn false")
			g.P("\t\t}")
			g.P("\t}")
		} else if field.Desc.IsMap() {
			// Map: compare length then key-value pairs
			g.P("\tif len(", recv, ".", fieldName, ") != len(other.", fieldName, ") {")
			g.P("\t\treturn false")
			g.P("\t}")
			g.P("\tfor k, v := range ", recv, ".", fieldName, " {")
			g.P("\t\tov, ok := other.", fieldName, "[k]")
			g.P("\t\tif !ok || v != ov {")
			g.P("\t\t\treturn false")
			g.P("\t\t}")
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
