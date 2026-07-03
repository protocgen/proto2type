package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
)

// generateGoActiveField generates per-oneof Active<Name>() string methods
// that return the proto field name of the currently set variant, or "" if none.
func generateGoActiveField(g *protogen.GeneratedFile, dm *DomainMessage) {
	if !dm.HasNonSyntheticOneof {
		return
	}

	recv := receiverName(dm.Name)

	for _, oneof := range dm.Oneofs {
		methodName := "Active" + toPascalCase(oneof.FieldName)

		g.P("// ", methodName, " returns the proto field name of the set ", oneof.FieldName)
		g.P("// variant, or \"\" if none is set.")
		g.P("func (", recv, " *", dm.Name, ") ", methodName, "() string {")
		g.P("\tif ", recv, " == nil {")
		g.P("\t\treturn \"\"")
		g.P("\t}")

		for _, v := range oneof.Variants {
			g.P("\tif ", recv, ".", v.Name, " != nil {")
			g.P("\t\treturn \"", v.ProtoName, "\"")
			g.P("\t}")
		}

		g.P("\treturn \"\"")
		g.P("}")
		g.P()
	}
}
