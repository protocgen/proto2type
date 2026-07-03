package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
)

// generateGoValidate generates a Validate() error method on the domain type.
//
// Two parts:
//   - Oneof mutual exclusion: always generated. Ensures at most one variant
//     is set per oneof group (the Go flattened representation allows multiple).
//   - protovalidate delegation: only when opts.Validate is true. Calls
//     protovalidate.Validate(d.ToProto()) for buf.validate constraint checking.
func generateGoValidate(g *protogen.GeneratedFile, dm *DomainMessage, opts *Options) {
	recv := receiverName(dm.Name)
	hasOneofs := dm.HasNonSyntheticOneof
	useProtovalidate := opts.Validate

	// Skip if nothing to validate.
	if !hasOneofs && !useProtovalidate {
		return
	}

	g.P("// Validate checks domain invariants on ", dm.Name, ".")
	if hasOneofs {
		g.P("// It ensures at most one variant is set per oneof group.")
	}
	if useProtovalidate {
		g.P("// It also runs buf.validate constraints via protovalidate.")
	}
	g.P("func (", recv, " *", dm.Name, ") Validate() error {")
	g.P("\tif ", recv, " == nil {")
	g.P("\t\treturn nil")
	g.P("\t}")

	// Part 1: Oneof mutual exclusion.
	if hasOneofs {
		fmtErrorf := g.QualifiedGoIdent(protogen.GoIdent{
			GoImportPath: "fmt",
			GoName:       "Errorf",
		})

		for _, oneof := range dm.Oneofs {
			g.P("\t{")
			g.P("\t\t_oneofCount := 0")
			for _, v := range oneof.Variants {
				g.P("\t\tif ", recv, ".", v.Name, " != nil { _oneofCount++ }")
			}
			g.P("\t\tif _oneofCount > 1 {")
			g.P("\t\t\treturn ", fmtErrorf, "(\"oneof ", oneof.FieldName, ": %d variants set, expected at most 1\", _oneofCount)")
			g.P("\t\t}")
			g.P("\t}")
		}
	}

	// Part 2: protovalidate delegation.
	if useProtovalidate {
		protovalidateValidate := g.QualifiedGoIdent(protogen.GoIdent{
			GoImportPath: "buf.build/go/protovalidate",
			GoName:       "Validate",
		})
		g.P("\tif err := ", protovalidateValidate, "(", recv, ".ToProto()); err != nil {")
		g.P("\t\treturn err")
		g.P("\t}")
	}

	g.P("\treturn nil")
	g.P("}")
	g.P()
}
