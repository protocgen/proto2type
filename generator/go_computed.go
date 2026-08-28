package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
)

// generateComputedAssignment emits the assignment for a single computed field.
// The source field is read from the domain object "d" (FromDomain parameter),
// transformed, and assigned to the storage receiver.
func generateComputedAssignment(g *protogen.GeneratedFile, recv string, f *DomainField) {
	if f.Computed == nil {
		return
	}

	src := "d." + f.Computed.SourcePascal
	dst := recv + "." + f.PascalName

	switch f.Computed.Transform {
	case "lower":
		toLower := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "ToLower"})
		g.P("\t", dst, " = ", toLower, "(", src, ")")
	case "upper":
		toUpper := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "ToUpper"})
		g.P("\t", dst, " = ", toUpper, "(", src, ")")
	}
}
