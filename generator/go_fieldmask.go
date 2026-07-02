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
			// Emit a case for each oneof variant
			oneof := findOneof(dm, f.OneofTypeName)
			for _, v := range oneof.Variants {
				g.P("\t\tcase \"", v.ProtoName, "\":")
				switch v.Kind {
				case FieldKindMessage:
					g.P("\t\t\tif src.", v.Name, " != nil {")
					g.P("\t\t\t\tdst.", v.Name, " = src.", v.Name, ".Clone()")
					g.P("\t\t\t} else {")
					g.P("\t\t\t\tdst.", v.Name, " = nil")
					g.P("\t\t\t}")
				default:
					g.P("\t\t\tdst.", v.Name, " = src.", v.Name)
				}
			}
			continue
		}
		g.P("\t\tcase \"", f.Name, "\":")
		if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind && f.Optional {
			// Deep copy optional bytes (*[]byte): deref, copy, re-ref
			g.P("\t\t\tif src.", f.PascalName, " != nil {")
			g.P("\t\t\t\tb := make([]byte, len(*src.", f.PascalName, "))")
			g.P("\t\t\t\tcopy(b, *src.", f.PascalName, ")")
			g.P("\t\t\t\tdst.", f.PascalName, " = &b")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tdst.", f.PascalName, " = nil")
			g.P("\t\t\t}")
		} else if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind {
			// Deep copy bytes fields (SEC-3)
			g.P("\t\t\tif src.", f.PascalName, " != nil {")
			g.P("\t\t\t\tdst.", f.PascalName, " = make([]byte, len(src.", f.PascalName, "))")
			g.P("\t\t\t\tcopy(dst.", f.PascalName, ", src.", f.PascalName, ")")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tdst.", f.PascalName, " = nil")
			g.P("\t\t\t}")
		} else if f.Kind == FieldKindMessage && !f.Repeated && !f.IsMap {
			// Deep copy user-defined message pointer fields via Clone() (GO-2).
			// Clone() is nil-safe (returns nil for nil receiver), so no guard needed.
			g.P("\t\t\tdst.", f.PascalName, " = src.", f.PascalName, ".Clone()")
		} else if f.Kind == FieldKindFieldMask && !f.Repeated && !f.IsMap {
			// Deep copy singular FieldMask ([]string)
			g.P("\t\t\tif src.", f.PascalName, " != nil {")
			g.P("\t\t\t\tdst.", f.PascalName, " = make([]string, len(src.", f.PascalName, "))")
			g.P("\t\t\t\tcopy(dst.", f.PascalName, ", src.", f.PascalName, ")")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tdst.", f.PascalName, " = nil")
			g.P("\t\t\t}")
		} else if f.Kind == FieldKindListValue && !f.Repeated && !f.IsMap {
			// Deep copy singular ListValue ([]any) via deepCopyValue
			g.P("\t\t\tif src.", f.PascalName, " != nil {")
			g.P("\t\t\t\tdst.", f.PascalName, " = deepCopyValue(src.", f.PascalName, ").([]any)")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tdst.", f.PascalName, " = nil")
			g.P("\t\t\t}")
		} else if f.Kind == FieldKindStruct && !f.Repeated && !f.IsMap {
			// Deep copy singular Struct (map[string]any) via deepCopyValue
			g.P("\t\t\tif src.", f.PascalName, " != nil {")
			g.P("\t\t\t\tdst.", f.PascalName, " = deepCopyValue(src.", f.PascalName, ").(map[string]any)")
			g.P("\t\t\t} else {")
			g.P("\t\t\t\tdst.", f.PascalName, " = nil")
			g.P("\t\t\t}")
		} else if f.Kind == FieldKindValue && !f.Repeated && !f.IsMap {
			// Deep copy singular Value (any) via deepCopyValue
			g.P("\t\t\tdst.", f.PascalName, " = deepCopyValue(src.", f.PascalName, ")")
		} else if f.Kind == FieldKindAny && !f.Repeated && !f.IsMap {
			// Deep copy singular Any (proto.Message) via proto.Clone
			protoClone := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/proto", GoName: "Clone"})
			protoMessage := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/proto", GoName: "Message"})
			g.P("\t\t\tif src.", f.PascalName, " != nil {")
			g.P("\t\t\t\tif m, ok := src.", f.PascalName, ".(", protoMessage, "); ok {")
			g.P("\t\t\t\t\tdst.", f.PascalName, " = ", protoClone, "(m)")
			g.P("\t\t\t\t} else {")
			g.P("\t\t\t\t\tdst.", f.PascalName, " = src.", f.PascalName)
			g.P("\t\t\t\t}")
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
