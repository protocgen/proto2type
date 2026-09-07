package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
)

// generateRustFieldMask generates an apply_field_mask function for a domain struct.
func generateRustFieldMask(g *protogen.GeneratedFile, dm *DomainMessage) {
	if dm.Skip {
		return
	}

	name := dm.Name

	g.P("impl ", name, " {")
	g.P("    /// Copies fields from `src` to `self` based on the given paths.")
	g.P("    ///")
	g.P("    /// Only top-level field names are supported.")
	g.P("    pub fn apply_field_mask(&mut self, src: &Self, paths: &[&str]) {")
	g.P("        for path in paths {")
	g.P("            match *path {")

	for _, f := range dm.Fields {
		if f.Computed != nil {
			continue
		}
		if f.IsOneof {
			// Emit a case for each oneof variant
			oneof := findOneof(dm, f.OneofTypeName)
			rustFieldName := escapeRustKeyword(toSnakeCase(f.Name))
			for _, v := range oneof.Variants {
				g.P("                \"", v.ProtoName, "\" => self.", rustFieldName, " = src.", rustFieldName, ".clone(),")
			}
			continue
		}

		rustFieldName := escapeRustKeyword(toSnakeCase(f.Name))
		g.P("                \"", f.Name, "\" => self.", rustFieldName, " = src.", rustFieldName, ".clone(),")
	}

	g.P("                _ => {} // ignore unknown paths")
	g.P("            }")
	g.P("        }")
	g.P("    }")
	g.P("}")
	g.P()
}
