package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// generateConverters generates ToProto() and FromProto() methods for a message struct.
// structSuffix is "" for domain, "Firestore" for Firestore, "Mongo" for Mongo, etc.
func generateConverters(g *protogen.GeneratedFile, msg *protogen.Message, structSuffix string) {
	structName := msg.GoIdent.GoName + structSuffix
	protoType := g.QualifiedGoIdent(msg.GoIdent)

	generateToProto(g, msg, structName, protoType)
	generateFromProto(g, msg, structName, protoType)
}

// generateToProto generates the ToProto method.
func generateToProto(g *protogen.GeneratedFile, msg *protogen.Message, structName, protoType string) {
	g.P("// ToProto converts to the protobuf message.")
	g.P("func (d *", structName, ") ToProto() *", protoType, " {")
	g.P("\tif d == nil {")
	g.P("\t\treturn nil")
	g.P("\t}")

	// Open the proto struct literal with non-special fields.
	g.P("\tpb := &", protoType, "{")
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isWellKnownTimestamp(field) || isWellKnownDuration(field) {
			continue
		}

		domainFieldName := toPascalCase(string(field.Desc.Name()))
		protoFieldName := field.GoName

		g.P("\t\t", protoFieldName, ": d.", domainFieldName, ",")
	}
	g.P("\t}")

	// Handle well-known types that need special conversion outside the struct literal.
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}

		domainFieldName := toPascalCase(string(field.Desc.Name()))
		protoFieldName := field.GoName

		if isWellKnownTimestamp(field) {
			// timestamppb.New
			tsNew := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "google.golang.org/protobuf/types/known/timestamppb",
				GoName:       "New",
			})
			g.P("\tif !d.", domainFieldName, ".IsZero() {")
			g.P("\t\tpb.", protoFieldName, " = ", tsNew, "(d.", domainFieldName, ")")
			g.P("\t}")
		} else if isWellKnownDuration(field) {
			// durationpb.New
			durNew := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "google.golang.org/protobuf/types/known/durationpb",
				GoName:       "New",
			})
			g.P("\tpb.", protoFieldName, " = ", durNew, "(d.", domainFieldName, ")")
		}
	}

	g.P("\treturn pb")
	g.P("}")
	g.P()
}

// generateFromProto generates the FromProto method.
func generateFromProto(g *protogen.GeneratedFile, msg *protogen.Message, structName, protoType string) {
	g.P("// FromProto populates from a protobuf message.")
	g.P("func (d *", structName, ") FromProto(pb *", protoType, ") {")
	g.P("\tif pb == nil {")
	g.P("\t\treturn")
	g.P("\t}")

	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}

		domainFieldName := toPascalCase(string(field.Desc.Name()))
		protoFieldName := field.GoName

		if isWellKnownTimestamp(field) {
			g.P("\tif pb.", protoFieldName, " != nil {")
			g.P("\t\td.", domainFieldName, " = pb.", protoFieldName, ".AsTime()")
			g.P("\t}")
		} else if isWellKnownDuration(field) {
			g.P("\tif pb.", protoFieldName, " != nil {")
			g.P("\t\td.", domainFieldName, " = pb.", protoFieldName, ".AsDuration()")
			g.P("\t}")
		} else if field.Desc.Kind() == protoreflect.MessageKind && !field.Desc.IsList() && !field.Desc.IsMap() {
			// Nested message: assign as pointer (for now)
			g.P("\td.", domainFieldName, " = pb.", protoFieldName)
		} else {
			// Scalars, repeated, maps: direct assignment
			g.P("\td.", domainFieldName, " = pb.", protoFieldName)
		}
	}

	g.P("}")
	g.P()
}
