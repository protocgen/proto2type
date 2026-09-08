package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
)

// generateGoServices generates a domain-level Handler interface for each
// proto service in the file. Streaming RPCs are emitted with a comment
// noting they require a streaming adapter; only unary methods get full
// domain-typed signatures.
func generateGoServices(g *protogen.GeneratedFile, df *DomainFile) {
	for _, svc := range df.Services {
		if svc.Skip {
			continue
		}

		handlerName := svc.Name + "Handler"

		// Emit comment.
		if svc.Comment != "" {
			g.P("// ", handlerName, " — ", svc.Comment)
		} else {
			g.P("// ", handlerName, " defines the domain-level interface for ", svc.Name, ".")
		}
		g.P("//")
		g.P("// All methods use domain types rather than proto types for clean")
		g.P("// separation between the transport layer and business logic.")
		g.P("type ", handlerName, " interface {")

		contextIdent := g.QualifiedGoIdent(protogen.GoIdent{
			GoImportPath: "context",
			GoName:       "Context",
		})

		for _, m := range svc.Methods {
			if m.Skip {
				continue
			}

			if m.Comment != "" {
				g.P("\t// ", m.Name, " — ", m.Comment)
			}

			if m.ClientStreaming || m.ServerStreaming {
				// Streaming methods get a placeholder comment.
				g.P("\t// ", m.Name, " is a streaming RPC and requires a streaming adapter.")
				g.P("\t// ", m.Name, "(ctx ", contextIdent, ", ...) error")
			} else {
				g.P("\t", m.Name, "(ctx ", contextIdent, ", req *", m.InputType, ") (*", m.OutputType, ", error)")
			}
		}

		g.P("}")
		g.P()
	}
}
