package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// generatePythonFieldMask generates an apply_field_mask function for a Python domain class.
func generatePythonFieldMask(g *protogen.GeneratedFile, dm *DomainMessage) {
	name := dm.Name
	funcName := "apply_field_mask_" + toSnakeCase(name)

	g.P("def ", funcName, "(dst: ", name, ", src: ", name, ", paths: list[str]) -> None:")
	g.P(`    """Copies fields from src to dst based on the given paths."""`)
	g.P("    import copy")
	g.P("    for path in paths:")

	hasFields := false

	for _, f := range dm.Fields {
		if f.Computed != nil {
			continue
		}

		if f.IsOneof {
			oneof := findOneof(dm, f.OneofTypeName)
			pythonAttrName, _ := escapePythonKeyword(toSnakeCase(oneof.FieldName))
			for _, v := range oneof.Variants {
				if !hasFields {
					g.P("        if path == \"", v.ProtoName, "\":")
					hasFields = true
				} else {
					g.P("        elif path == \"", v.ProtoName, "\":")
				}

				if isPythonScalarFast(v.Kind, v.ScalarKind) {
					g.P("            dst.", pythonAttrName, " = src.", pythonAttrName)
				} else {
					g.P("            dst.", pythonAttrName, " = copy.deepcopy(src.", pythonAttrName, ")")
				}
			}
			continue
		}

		if !hasFields {
			g.P("        if path == \"", f.Name, "\":")
			hasFields = true
		} else {
			g.P("        elif path == \"", f.Name, "\":")
		}

		pythonAttrName, _ := escapePythonKeyword(f.Name)
		if isPythonScalarFast(f.Kind, f.ScalarKind) && !f.Repeated && !f.IsMap {
			g.P("            dst.", pythonAttrName, " = src.", pythonAttrName)
		} else {
			g.P("            dst.", pythonAttrName, " = copy.deepcopy(src.", pythonAttrName, ")")
		}
	}

	if !hasFields {
		g.P("        pass")
	}
	g.P()
}

func isPythonScalarFast(kind FieldKind, scalar protoreflect.Kind) bool {
	if kind == FieldKindEnum {
		return true
	}
	// All proto scalars (including bytes) are immutable in Python,
	// so direct assignment is safe without copy.deepcopy.
	if kind == FieldKindScalar {
		return true
	}
	return false
}
