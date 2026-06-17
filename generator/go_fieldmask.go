package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// generateGoFieldMask generates an ApplyFieldMask function for a domain struct.
func generateGoFieldMask(g *protogen.GeneratedFile, msg *protogen.Message) {
	name := msg.GoIdent.GoName

	g.P("// ApplyFieldMask", name, " copies fields from src to dst based on the given paths.")
	g.P("func ApplyFieldMask", name, "(dst, src *", name, ", paths []string) {")
	g.P("\tif dst == nil || src == nil {")
	g.P("\t\treturn")
	g.P("\t}")
	g.P("\tfor _, path := range paths {")
	g.P("\t\tswitch path {")

	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		protoName := string(field.Desc.Name())
		fieldName := toPascalCase(protoName)
		g.P("\t\tcase \"", protoName, "\":")
		if field.Desc.Kind() == protoreflect.BytesKind {
			// Deep copy bytes fields (SEC-3)
			g.P("\t\t\tif src.", fieldName, " != nil {")
			g.P("\t\t\t\tdst.", fieldName, " = make([]byte, len(src.", fieldName, "))")
			g.P("\t\t\t\tcopy(dst.", fieldName, ", src.", fieldName, ")")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tdst.", fieldName, " = nil")
			g.P("\t\t\t}")
		} else {
			g.P("\t\t\tdst.", fieldName, " = src.", fieldName)
		}
	}

	g.P("\t\t}")
	g.P("\t}")
	g.P("}")
	g.P()
}
