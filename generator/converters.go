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

	// Oneof warning (PROTO-4): check for non-synthetic oneofs and emit a warning comment.
	for _, oneof := range msg.Oneofs {
		if !oneof.Desc.IsSynthetic() {
			g.P("// WARNING: oneof fields in ", msg.GoIdent.GoName, " are not yet supported by proto2type.")
			g.P()
			break
		}
	}

	generateToProto(g, msg, structName, protoType, structSuffix)
	generateFromProto(g, msg, structName, protoType, structSuffix)
}

// generateToProto generates the ToProto method.
func generateToProto(g *protogen.GeneratedFile, msg *protogen.Message, structName, protoType, structSuffix string) {
	recv := receiverName(structName)
	g.P("// ToProto converts to the protobuf message.")
	g.P("func (", recv, " *", structName, ") ToProto() *", protoType, " {")
	g.P("\tif ", recv, " == nil {")
	g.P("\t\treturn nil")
	g.P("\t}")

	// Open the proto struct literal with non-special fields.
	g.P("\tpb := &", protoType, "{")
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		// Skip document_id fields for Firestore (not in the struct)
		if isDocumentID(field) && structSuffix == "Firestore" {
			continue
		}
		if isWellKnownTimestamp(field) || isWellKnownDuration(field) {
			continue
		}
		// Skip well-known wrapper types — handle below
		if isWellKnownWrapper(field) {
			continue
		}
		// Skip non-WKT message fields from struct literal — handle recursively below.
		if field.Desc.Kind() == protoreflect.MessageKind && !field.Desc.IsList() && !field.Desc.IsMap() {
			continue
		}
		// Skip bytes fields — handle with copy below (SEC-3)
		if field.Desc.Kind() == protoreflect.BytesKind {
			continue
		}
		// Skip optional scalars — handle nil pointer below (PROTO-3)
		if field.Desc.HasOptionalKeyword() {
			continue
		}

		domainFieldName := toPascalCase(string(field.Desc.Name()))
		protoFieldName := field.GoName

		g.P("\t\t", protoFieldName, ": ", recv, ".", domainFieldName, ",")
	}
	g.P("\t}")

	// Handle well-known types and special fields outside the struct literal.
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		// Skip document_id fields for Firestore (not in the struct)
		if isDocumentID(field) && structSuffix == "Firestore" {
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
			g.P("\tif !", recv, ".", domainFieldName, ".IsZero() {")
			g.P("\t\tpb.", protoFieldName, " = ", tsNew, "(", recv, ".", domainFieldName, ")")
			g.P("\t}")
		} else if isWellKnownDuration(field) {
			// durationpb.New
			durNew := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "google.golang.org/protobuf/types/known/durationpb",
				GoName:       "New",
			})
			g.P("\tpb.", protoFieldName, " = ", durNew, "(", recv, ".", domainFieldName, ")")
		} else if isWellKnownWrapper(field) {
			// Wrapper type: if d.Phone != nil { pb.Phone = wrapperspb.String(*d.Phone) }
			funcName := wrapperPbFuncName(field)
			wrapperFunc := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "google.golang.org/protobuf/types/known/wrapperspb",
				GoName:       funcName,
			})
			g.P("\tif ", recv, ".", domainFieldName, " != nil {")
			g.P("\t\tpb.", protoFieldName, " = ", wrapperFunc, "(*", recv, ".", domainFieldName, ")")
			g.P("\t}")
		} else if field.Desc.Kind() == protoreflect.MessageKind && !field.Desc.IsList() && !field.Desc.IsMap() {
			// Nested message: recursive conversion via ToProto()
			g.P("\tif ", recv, ".", domainFieldName, " != nil {")
			g.P("\t\tpb.", protoFieldName, " = ", recv, ".", domainFieldName, ".ToProto()")
			g.P("\t}")
		} else if field.Desc.Kind() == protoreflect.BytesKind {
			// Bytes field: defensive copy (SEC-3)
			g.P("\tif ", recv, ".", domainFieldName, " != nil {")
			g.P("\t\tpb.", protoFieldName, " = make([]byte, len(", recv, ".", domainFieldName, "))")
			g.P("\t\tcopy(pb.", protoFieldName, ", ", recv, ".", domainFieldName, ")")
			g.P("\t}")
		} else if field.Desc.HasOptionalKeyword() {
			// Optional scalar: both domain and proto use *T, assign directly (PROTO-3)
			g.P("\tpb.", protoFieldName, " = ", recv, ".", domainFieldName)
		}
	}

	g.P("\treturn pb")
	g.P("}")
	g.P()
}

// generateFromProto generates the FromProto method.
func generateFromProto(g *protogen.GeneratedFile, msg *protogen.Message, structName, protoType, structSuffix string) {
	recv := receiverName(structName)
	g.P("// FromProto populates from a protobuf message.")
	g.P("func (", recv, " *", structName, ") FromProto(pb *", protoType, ") {")
	g.P("\tif pb == nil {")
	g.P("\t\treturn")
	g.P("\t}")

	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		// Skip document_id fields for Firestore (not in the struct)
		if isDocumentID(field) && structSuffix == "Firestore" {
			continue
		}

		domainFieldName := toPascalCase(string(field.Desc.Name()))
		protoFieldName := field.GoName

		if isWellKnownTimestamp(field) {
			g.P("\tif pb.", protoFieldName, " != nil {")
			g.P("\t\t", recv, ".", domainFieldName, " = pb.", protoFieldName, ".AsTime()")
			g.P("\t}")
		} else if isWellKnownDuration(field) {
			g.P("\tif pb.", protoFieldName, " != nil {")
			g.P("\t\t", recv, ".", domainFieldName, " = pb.", protoFieldName, ".AsDuration()")
			g.P("\t}")
		} else if isWellKnownWrapper(field) {
			// Wrapper type: if pb.Phone != nil { v := pb.Phone.GetValue(); d.Phone = &v }
			g.P("\tif pb.", protoFieldName, " != nil {")
			g.P("\t\tv := pb.", protoFieldName, ".GetValue()")
			g.P("\t\t", recv, ".", domainFieldName, " = &v")
			g.P("\t}")
		} else if field.Desc.Kind() == protoreflect.MessageKind && !field.Desc.IsList() && !field.Desc.IsMap() {
			// Nested message: recursive conversion via FromProto()
			nestedType := toPascalCase(string(field.Desc.Message().Name())) + structSuffix
			g.P("\tif pb.", protoFieldName, " != nil {")
			g.P("\t\t", recv, ".", domainFieldName, " = &", nestedType, "{}")
			g.P("\t\t", recv, ".", domainFieldName, ".FromProto(pb.", protoFieldName, ")")
			g.P("\t}")
		} else if field.Desc.Kind() == protoreflect.BytesKind {
			// Bytes field: defensive copy (SEC-3)
			g.P("\tif pb.", protoFieldName, " != nil {")
			g.P("\t\t", recv, ".", domainFieldName, " = make([]byte, len(pb.", protoFieldName, "))")
			g.P("\t\tcopy(", recv, ".", domainFieldName, ", pb.", protoFieldName, ")")
			g.P("\t}")
		} else if field.Desc.HasOptionalKeyword() {
			// Optional scalar: both proto and domain use *T, assign directly (PROTO-3)
			g.P("\t", recv, ".", domainFieldName, " = pb.", protoFieldName)
		} else {
			// Scalars, repeated, maps: direct assignment
			g.P("\t", recv, ".", domainFieldName, " = pb.", protoFieldName)
		}
	}

	g.P("}")
	g.P()
}

// generateDomainConverters generates ToDomain/FromDomain methods for a storage struct.
func generateDomainConverters(g *protogen.GeneratedFile, msg *protogen.Message, storageSuffix string) {
	storageType := msg.GoIdent.GoName + storageSuffix
	domainType := msg.GoIdent.GoName
	recv := receiverName(storageType)

	// Determine if this is a Firestore type and find the document_id field.
	isFirestore := storageSuffix == "Firestore"
	var docIDFieldName string
	if isFirestore {
		for _, field := range msg.Fields {
			if isDocumentID(field) {
				docIDFieldName = toPascalCase(string(field.Desc.Name()))
				break
			}
		}
	}

	// ToDomain
	g.P("// ToDomain converts to the domain type.")
	if isFirestore && docIDFieldName != "" {
		// Firestore ToDomain accepts a documentID parameter (DATA-2)
		g.P("func (", recv, " *", storageType, ") ToDomain(documentID string) *", domainType, " {")
	} else {
		g.P("func (", recv, " *", storageType, ") ToDomain() *", domainType, " {")
	}
	g.P("\tif ", recv, " == nil {")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\td := &", domainType, "{")
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		// For Firestore: document_id fields are not in the storage struct
		// They need to be passed separately — skip in direct assignment
		if isDocumentID(field) && isFirestore {
			continue
		}
		// Skip non-WKT message fields — handle recursively below
		if isNestedMessage(field) {
			continue
		}
		fieldName := toPascalCase(string(field.Desc.Name()))
		if field.Desc.Kind() == protoreflect.BytesKind {
			// Skip bytes — handled with deep copy below
			continue
		}
		g.P("\t\t", fieldName, ": ", recv, ".", fieldName, ",")
	}
	g.P("\t}")

	// Deep copy bytes fields (SEC-3)
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		if isDocumentID(field) && isFirestore {
			continue
		}
		if field.Desc.Kind() == protoreflect.BytesKind {
			fieldName := toPascalCase(string(field.Desc.Name()))
			g.P("\tif ", recv, ".", fieldName, " != nil {")
			g.P("\t\td.", fieldName, " = make([]byte, len(", recv, ".", fieldName, "))")
			g.P("\t\tcopy(d.", fieldName, ", ", recv, ".", fieldName, ")")
			g.P("\t}")
		}
	}

	// Assign document ID from parameter (Firestore only)
	if isFirestore && docIDFieldName != "" {
		g.P("\td.", docIDFieldName, " = documentID")
	}

	// Handle nested message fields with recursive conversion
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		if isDocumentID(field) && isFirestore {
			continue
		}
		if isNestedMessage(field) {
			fieldName := toPascalCase(string(field.Desc.Name()))
			g.P("\tif ", recv, ".", fieldName, " != nil {")
			g.P("\t\td.", fieldName, " = ", recv, ".", fieldName, ".ToDomain()")
			g.P("\t}")
		}
	}

	g.P("\treturn d")
	g.P("}")
	g.P()

	// FromDomain
	g.P("// FromDomain populates from the domain type.")
	g.P("func (", recv, " *", storageType, ") FromDomain(d *", domainType, ") {")
	g.P("\tif d == nil {")
	g.P("\t\treturn")
	g.P("\t}")
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		if isDocumentID(field) && isFirestore {
			continue
		}
		fieldName := toPascalCase(string(field.Desc.Name()))
		if isNestedMessage(field) {
			// Nested message: recursive conversion via FromDomain
			nestedType := toPascalCase(string(field.Desc.Message().Name())) + storageSuffix
			g.P("\tif d.", fieldName, " != nil {")
			g.P("\t\t", recv, ".", fieldName, " = &", nestedType, "{}")
			g.P("\t\t", recv, ".", fieldName, ".FromDomain(d.", fieldName, ")")
			g.P("\t}")
		} else if field.Desc.Kind() == protoreflect.BytesKind {
			// Deep copy bytes fields (SEC-3)
			g.P("\tif d.", fieldName, " != nil {")
			g.P("\t\t", recv, ".", fieldName, " = make([]byte, len(d.", fieldName, "))")
			g.P("\t\tcopy(", recv, ".", fieldName, ", d.", fieldName, ")")
			g.P("\t}")
		} else {
			g.P("\t", recv, ".", fieldName, " = d.", fieldName)
		}
	}
	g.P("}")
	g.P()
}
