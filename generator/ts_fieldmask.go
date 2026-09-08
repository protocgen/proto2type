package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// generateTsFieldMask generates an applyFieldMask function for a TS domain message.
func generateTsFieldMask(g *protogen.GeneratedFile, dm *DomainMessage) {
	hasFields := false
	for _, f := range dm.Fields {
		if f.Computed == nil {
			hasFields = true
			break
		}
	}
	if !hasFields {
		return
	}

	name := dm.Name

	g.P("export function applyFieldMask", name, "(dst: ", name, ", src: ", name, ", paths: string[]): void {")
	g.P("  for (const path of paths) {")
	g.P("    switch (path) {")

	for _, f := range dm.Fields {
		if f.Computed != nil {
			continue
		}
		if f.IsOneof {
			oneof := findOneof(dm, f.OneofTypeName)
			for _, v := range oneof.Variants {
				g.P("      case \"", v.ProtoName, "\":")
				emitTsFieldAssign(g, toCamelCase(v.ProtoName), v.Kind, false, false, v.ScalarKind)
				g.P("        break;")
			}
			continue
		}
		g.P("      case \"", f.Name, "\":")
		emitTsFieldAssign(g, f.CamelName, f.Kind, f.Repeated, f.IsMap, f.ScalarKind)
		g.P("        break;")
	}

	g.P("    }")
	g.P("  }")
	g.P("}")
}

func emitTsFieldAssign(g *protogen.GeneratedFile, camelName string, kind FieldKind, repeated, isMap bool, scalarKind protoreflect.Kind) {
	if isMap {
		g.P("        dst.", camelName, " = src.", camelName, " ? structuredClone(src.", camelName, ") : {};")
	} else if repeated {
		if kind == FieldKindMessage || kind == FieldKindStruct || kind == FieldKindListValue || kind == FieldKindValue || kind == FieldKindAny || kind == FieldKindEmpty {
			g.P("        dst.", camelName, " = src.", camelName, " ? structuredClone(src.", camelName, ") : [];")
		} else {
			g.P("        dst.", camelName, " = src.", camelName, " ? [...src.", camelName, "] : [];")
		}
	} else if kind == FieldKindMessage || kind == FieldKindStruct || kind == FieldKindListValue || kind == FieldKindValue || kind == FieldKindAny || kind == FieldKindEmpty {
		g.P("        dst.", camelName, " = src.", camelName, " ? structuredClone(src.", camelName, ") : undefined;")
	} else {
		// Scalars (including bytes, which are base64 strings in TS) are directly assigned.
		g.P("        dst.", camelName, " = src.", camelName, ";")
	}
}
