package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
)

// generateGoFieldMask generates an ApplyFieldMask function for a domain struct.
func generateGoFieldMask(g *protogen.GeneratedFile, msg *protogen.Message) {
	name := msg.GoIdent.GoName

	g.P("// ApplyFieldMask", name, " copies fields from src to dst based on the given paths.")
	g.P("func ApplyFieldMask", name, "(dst, src *", name, ", paths []string) {")
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
		g.P("\t\t\tdst.", fieldName, " = src.", fieldName)
	}

	g.P("\t\t}")
	g.P("\t}")
	g.P("}")
	g.P()
}
