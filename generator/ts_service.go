package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
)

// generateTSServices emits TypeScript interface declarations for each
// proto service in the file.
func generateTSServices(g *protogen.GeneratedFile, df *DomainFile) {
	for _, svc := range df.Services {
		if svc.Skip {
			continue
		}

		handlerName := svc.Name + "Handler"

		// Emit JSDoc comment.
		if svc.Comment != "" {
			g.P("/**")
			g.P(" * ", handlerName, " — ", sanitizeBlockComment(svc.Comment))
			g.P(" *")
		} else {
			g.P("/**")
			g.P(" * ", handlerName, " defines the domain-level interface for ", svc.Name, ".")
			g.P(" *")
		}
		g.P(" * All methods use domain types rather than proto types for clean")
		g.P(" * separation between the transport layer and business logic.")
		g.P(" */")
		g.P("export interface ", handlerName, " {")

		for _, m := range svc.Methods {
			if m.Skip {
				continue
			}

			if m.Comment != "" {
				g.P("  /** ", m.Name, " — ", sanitizeBlockComment(m.Comment), " */")
			}

			if m.ClientStreaming || m.ServerStreaming {
				g.P("  // ", m.Name, " is a streaming RPC and requires a streaming adapter.")
				g.P("  // ", m.Name, "(req: ", m.InputType, "): Promise<", m.OutputType, ">;")
			} else {
				g.P("  ", m.Name, "(req: ", m.InputType, "): Promise<", m.OutputType, ">;")
			}
		}

		g.P("}")
		g.P()
	}
}
