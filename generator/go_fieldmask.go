package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// generateGoFieldMask generates an ApplyFieldMask function for a domain struct.
func generateGoFieldMask(g *protogen.GeneratedFile, dm *DomainMessage) {
	name := dm.Name

	g.P("// ApplyFieldMask", name, " copies fields from src to dst based on the given paths.")
	g.P("func ApplyFieldMask", name, "(dst, src *", name, ", paths []string) {")
	g.P("\tif dst == nil || src == nil {")
	g.P("\t\treturn")
	g.P("\t}")
	g.P("\tfor _, path := range paths {")
	g.P("\t\tswitch path {")

	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		g.P("\t\tcase \"", f.Name, "\":")
		if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind {
			// Deep copy bytes fields (SEC-3)
			g.P("\t\t\tif src.", f.PascalName, " != nil {")
			g.P("\t\t\t\tdst.", f.PascalName, " = make([]byte, len(src.", f.PascalName, "))")
			g.P("\t\t\t\tcopy(dst.", f.PascalName, ", src.", f.PascalName, ")")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tdst.", f.PascalName, " = nil")
			g.P("\t\t\t}")
		} else if f.Kind == FieldKindMessage && !f.Repeated && !f.IsMap {
			// Deep copy user-defined message pointer fields (GO-2).
			// WKTs (Timestamp, Duration, wrappers) map to Go value types
			// and don't need pointer deep copy.
			g.P("\t\t\tif src.", f.PascalName, " != nil {")
			g.P("\t\t\t\tclone := *src.", f.PascalName)
			g.P("\t\t\t\tdst.", f.PascalName, " = &clone")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tdst.", f.PascalName, " = nil")
			g.P("\t\t\t}")
		} else {
			g.P("\t\t\tdst.", f.PascalName, " = src.", f.PascalName)
		}
	}

	g.P("\t\t}")
	g.P("\t}")
	g.P("}")
	g.P()
}
