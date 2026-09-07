package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// generateKotlinFieldMask generates an applyFieldMask function for a Kotlin domain class.
func generateKotlinFieldMask(g *protogen.GeneratedFile, dm *DomainMessage) {
	name := dm.Name

	g.P("/**")
	g.P(" * Copies fields from [src] to [dst] based on the given [paths], returning a new instance.")
	g.P(" *")
	g.P(" * Only top-level field names are supported.")
	g.P(" */")
	g.P("fun applyFieldMask", name, "(dst: ", name, ", src: ", name, ", paths: List<String>): ", name, " {")
	g.P("    var result = dst")
	g.P("    for (path in paths) {")
	g.P("        result = when (path) {")

	for _, f := range dm.Fields {
		if f.Computed != nil {
			continue
		}

		if f.IsOneof {
			oneof := findOneof(dm, f.OneofTypeName)
			camel := escapeKotlinKeyword(toCamelCase(f.Name))
			for _, v := range oneof.Variants {
				variantType := oneof.Name + "." + v.Name

				g.P("            \"", v.ProtoName, "\" -> if (src.", camel, " is ", variantType, ") {")

				valExpr := "src." + camel + ".value"
				if v.Kind == FieldKindScalar && v.ScalarKind == protoreflect.BytesKind {
					valExpr += ".copyOf()"
				}

				g.P("                result.copy(", camel, " = ", variantType, "(", valExpr, "))")
				g.P("            } else if (result.", camel, " is ", variantType, ") {")
				g.P("                result.copy(", camel, " = null)")
				g.P("            } else {")
				g.P("                result")
				g.P("            }")
			}
			continue
		}

		camel := escapeKotlinKeyword(toCamelCase(f.Name))
		expr := "src." + camel

		if f.IsMap {
			expr += ".toMap()"
		} else if f.Repeated {
			expr += ".toList()"
		} else if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind {
			// ByteArray is mutable; copy to avoid aliasing.
			if f.Optional {
				expr += "?.copyOf()"
			} else {
				expr += ".copyOf()"
			}
		}
		// Messages: data class fields are val (immutable), so direct reference is safe.

		g.P("            \"", f.Name, "\" -> result.copy(", camel, " = ", expr, ")")
	}

	g.P("            else -> result")
	g.P("        }")
	g.P("    }")
	g.P("    return result")
	g.P("}")
	g.P()
}
