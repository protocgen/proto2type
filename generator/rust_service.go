package generator

import (
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

// generateRustServices emits Rust async trait definitions for each
// proto service in the file.
func generateRustServices(g *protogen.GeneratedFile, df *DomainFile) {
	for _, svc := range df.Services {
		if svc.Skip {
			continue
		}

		traitName := svc.Name + "Handler"

		// Emit doc comment (handling multiline).
		if svc.Comment != "" {
			for i, line := range strings.Split(svc.Comment, "\n") {
				if i == 0 {
					g.P("/// ", traitName, " — ", line)
				} else {
					g.P("/// ", line)
				}
			}
		} else {
			g.P("/// ", traitName, " defines the domain-level interface for ", svc.Name, ".")
		}
		g.P("///")
		g.P("/// All methods use domain types rather than proto types for clean")
		g.P("/// separation between the transport layer and business logic.")
		g.P("#[async_trait::async_trait]")
		g.P("pub trait ", traitName, " {")

		for _, m := range svc.Methods {
			if m.Skip {
				continue
			}

			methodName := escapeRustKeyword(toSnakeCase(m.Name))

			if m.Comment != "" {
				for _, line := range strings.Split(m.Comment, "\n") {
					g.P("    /// ", line)
				}
			}

			if m.ClientStreaming || m.ServerStreaming {
				g.P("    // ", m.Name, " is a streaming RPC and requires a streaming adapter.")
				g.P("    // async fn ", methodName, "(&self, req: ", m.InputType, ") -> Result<", m.OutputType, ", Box<dyn std::error::Error>>;")
			} else {
				g.P("    async fn ", methodName, "(&self, req: ", m.InputType, ") -> Result<", m.OutputType, ", Box<dyn std::error::Error>>;")
			}
		}

		g.P("}")
		g.P()
	}
}
