package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
)

// generatePythonServices emits Python Protocol classes for each
// proto service in the file.
func generatePythonServices(g *protogen.GeneratedFile, df *DomainFile) {
	for _, svc := range df.Services {
		if svc.Skip {
			continue
		}

		handlerName := svc.Name + "Handler"

		// Emit class docstring.
		if svc.Comment != "" {
			g.P("class ", handlerName, "(Protocol):")
			g.P(`    """`, handlerName, ` — `, svc.Comment, `"""`)
		} else {
			g.P("class ", handlerName, "(Protocol):")
			g.P(`    """Defines the domain-level interface for `, svc.Name, `."""`)
		}
		g.P()

		for _, m := range svc.Methods {
			if m.Skip {
				continue
			}

			methodName := toSnakeCase(m.Name)

			if m.Comment != "" {
				g.P("    def ", methodName, "(self, req: ", m.InputType, ") -> ", m.OutputType, ":")
				g.P(`        """`, m.Name, ` — `, m.Comment, `"""`)
				g.P("        ...")
			} else if m.ClientStreaming || m.ServerStreaming {
				g.P("    # ", m.Name, " is a streaming RPC and requires a streaming adapter.")
				g.P("    # def ", methodName, "(self, req: ", m.InputType, ") -> ", m.OutputType, ": ...")
			} else {
				g.P("    def ", methodName, "(self, req: ", m.InputType, ") -> ", m.OutputType, ":")
				g.P("        ...")
			}
			g.P()
		}
	}
}
