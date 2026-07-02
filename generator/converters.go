package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// irWrapperPbFuncName returns the wrapperspb constructor name for a wrapper FieldKind.
func irWrapperPbFuncName(kind FieldKind) string {
	switch kind {
	case FieldKindWrapperBool:
		return "Bool"
	case FieldKindWrapperInt32:
		return "Int32"
	case FieldKindWrapperInt64:
		return "Int64"
	case FieldKindWrapperUInt32:
		return "UInt32"
	case FieldKindWrapperUInt64:
		return "UInt64"
	case FieldKindWrapperFloat:
		return "Float"
	case FieldKindWrapperDouble:
		return "Double"
	case FieldKindWrapperString:
		return "String"
	case FieldKindWrapperBytes:
		return "Bytes"
	default:
		return "UNKNOWN"
	}
}

// irWrapperGoSliceType returns the Go slice type for a repeated wrapper field.
// e.g. FieldKindWrapperString → "[]*string"
func irWrapperGoSliceType(kind FieldKind) string {
	switch kind {
	case FieldKindWrapperBool:
		return "[]*bool"
	case FieldKindWrapperInt32:
		return "[]*int32"
	case FieldKindWrapperInt64:
		return "[]*int64"
	case FieldKindWrapperUInt32:
		return "[]*uint32"
	case FieldKindWrapperUInt64:
		return "[]*uint64"
	case FieldKindWrapperFloat:
		return "[]*float32"
	case FieldKindWrapperDouble:
		return "[]*float64"
	case FieldKindWrapperString:
		return "[]*string"
	case FieldKindWrapperBytes:
		return "[]*[]byte"
	default:
		return "[]any"
	}
}

// generateConverters generates ToProto() and FromProto() methods for a message struct.
// structSuffix is "" for domain, "Firestore" for Firestore, "Mongo" for Mongo, etc.
func generateConverters(g *protogen.GeneratedFile, dm *DomainMessage, structSuffix string, opts *Options) {
	generateToProto(g, dm, structSuffix, opts)
	generateFromProto(g, dm, structSuffix, opts)
}

// generateToProto generates the ToProto method.
func generateToProto(g *protogen.GeneratedFile, dm *DomainMessage, structSuffix string, opts *Options) {
	structName := dm.Name + structSuffix
	protoType := g.QualifiedGoIdent(dm.ProtoGoIdent)
	recv := receiverName(structName)
	g.P("// ToProto converts to the protobuf message.")
	g.P("func (", recv, " *", structName, ") ToProto() *", protoType, " {")
	g.P("\tif ", recv, " == nil {")
	g.P("\t\treturn nil")
	g.P("\t}")

	// Open the proto struct literal with non-special fields.
	// Use "out" instead of "pb" to avoid shadowing the proto package import
	// when QualifiedGoIdent resolves types like pb.Tag.
	g.P("\tout := &", protoType, "{")
	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		// Skip document_id fields for Firestore (not in the struct)
		if f.DocID && structSuffix == "Firestore" {
			continue
		}
		if f.Kind == FieldKindTimestamp || f.Kind == FieldKindDuration {
			continue
		}
		// Skip well-known wrapper types — handle below
		if f.Kind.IsWrapper() {
			continue
		}
		// Skip message fields from struct literal — handle recursively below.
		// This covers both singular messages and repeated messages.
		if f.Kind == FieldKindMessage && !f.IsMap {
			continue
		}
		// Skip WKT reference types — handled below (FieldMask, Struct, ListValue)
		if f.Kind == FieldKindFieldMask || f.Kind == FieldKindStruct || f.Kind == FieldKindListValue {
			continue
		}
		// Skip map fields with WKT values — need conversion, handled below.
		if f.IsMap && f.MapValue != nil {
			switch f.MapValue.Kind {
			case FieldKindTimestamp, FieldKindDuration, FieldKindStruct, FieldKindListValue, FieldKindFieldMask:
				continue
			}
		}
		// Skip bytes fields — handle with copy below (SEC-3)
		if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind {
			continue
		}
		// Skip optional scalars — handle nil pointer below (PROTO-3)
		// Note: optional enums are also skipped here and handled below.
		if f.Optional {
			continue
		}

		domainFieldName := f.PascalName
		protoFieldName := f.ProtoGoName

		if f.Kind == FieldKindEnum {
			enumIdent := g.QualifiedGoIdent(f.ProtoEnumGoIdent)
			if f.EnumAsString {
				// String enum: look up the numeric value from the enum's _value map
				g.P("\t\t", protoFieldName, ": ", enumIdent, "(", enumIdent, "_value[", recv, ".", domainFieldName, "]),")
			} else {
				// Int32 enum: direct cast
				g.P("\t\t", protoFieldName, ": ", enumIdent, "(", recv, ".", domainFieldName, "),")
			}
		} else {
			g.P("\t\t", protoFieldName, ": ", recv, ".", domainFieldName, ",")
		}
	}
	g.P("\t}")

	// Handle well-known types and special fields outside the struct literal.
	for _, f := range dm.Fields {
		if f.IsOneof {
			oneof := findOneof(dm, f.OneofTypeName)
			for _, v := range oneof.Variants {
				g.P("\tif ", recv, ".", v.Name, " != nil {")
				switch v.Kind {
				case FieldKindScalar:
					wrapperIdent := g.QualifiedGoIdent(v.ProtoGoIdent)
					g.P("\t\tout.", oneof.ProtoGoName, " = &", wrapperIdent,
						"{", v.ProtoGoName, ": *", recv, ".", v.Name, "}")
				case FieldKindMessage:
					wrapperIdent := g.QualifiedGoIdent(v.ProtoGoIdent)
					g.P("\t\tout.", oneof.ProtoGoName, " = &", wrapperIdent,
						"{", v.ProtoGoName, ": ", recv, ".", v.Name, ".ToProto()}")
				case FieldKindEnum:
					wrapperIdent := g.QualifiedGoIdent(v.ProtoGoIdent)
					enumIdent := g.QualifiedGoIdent(v.ProtoEnumGoIdent)
					if v.EnumAsString {
						g.P("\t\tout.", oneof.ProtoGoName, " = &", wrapperIdent,
							"{", v.ProtoGoName, ": ", enumIdent, "(", enumIdent, "_value[*", recv, ".", v.Name, "])}")
					} else {
						g.P("\t\tout.", oneof.ProtoGoName, " = &", wrapperIdent,
							"{", v.ProtoGoName, ": ", enumIdent, "(*", recv, ".", v.Name, ")}")
					}
				case FieldKindTimestamp:
					wrapperIdent := g.QualifiedGoIdent(v.ProtoGoIdent)
					tsNew := g.QualifiedGoIdent(protogen.GoIdent{
						GoImportPath: "google.golang.org/protobuf/types/known/timestamppb",
						GoName:       "New",
					})
					g.P("\t\tout.", oneof.ProtoGoName, " = &", wrapperIdent,
						"{", v.ProtoGoName, ": ", tsNew, "(*", recv, ".", v.Name, ")}")
				case FieldKindDuration:
					wrapperIdent := g.QualifiedGoIdent(v.ProtoGoIdent)
					durNew := g.QualifiedGoIdent(protogen.GoIdent{
						GoImportPath: "google.golang.org/protobuf/types/known/durationpb",
						GoName:       "New",
					})
					g.P("\t\tout.", oneof.ProtoGoName, " = &", wrapperIdent,
						"{", v.ProtoGoName, ": ", durNew, "(*", recv, ".", v.Name, ")}")
				}
				g.P("\t}")
			}
			continue
		}
		// Skip document_id fields for Firestore (not in the struct)
		if f.DocID && structSuffix == "Firestore" {
			continue
		}

		domainFieldName := f.PascalName
		protoFieldName := f.ProtoGoName

		// Handle repeated WKT types with loop-based conversion.
		if f.Repeated && (f.Kind == FieldKindTimestamp || f.Kind == FieldKindDuration || f.Kind == FieldKindFieldMask || f.Kind == FieldKindStruct || f.Kind == FieldKindListValue || f.Kind.IsWrapper()) {
			switch f.Kind {
			case FieldKindTimestamp:
				tsNew := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/timestamppb", GoName: "New"})
				tsType := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/timestamppb", GoName: "Timestamp"})
				g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
				g.P("\t\tout.", protoFieldName, " = make([]*", tsType, ", len(", recv, ".", domainFieldName, "))")
				g.P("\t\tfor i, v := range ", recv, ".", domainFieldName, " {")
				g.P("\t\t\tout.", protoFieldName, "[i] = ", tsNew, "(v)")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindDuration:
				durNew := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/durationpb", GoName: "New"})
				durType := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/durationpb", GoName: "Duration"})
				g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
				g.P("\t\tout.", protoFieldName, " = make([]*", durType, ", len(", recv, ".", domainFieldName, "))")
				g.P("\t\tfor i, v := range ", recv, ".", domainFieldName, " {")
				g.P("\t\t\tout.", protoFieldName, "[i] = ", durNew, "(v)")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindFieldMask:
				fmType := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/fieldmaskpb", GoName: "FieldMask"})
				g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
				g.P("\t\tout.", protoFieldName, " = make([]*", fmType, ", len(", recv, ".", domainFieldName, "))")
				g.P("\t\tfor i, v := range ", recv, ".", domainFieldName, " {")
				g.P("\t\t\tpaths := make([]string, len(v))")
				g.P("\t\t\tcopy(paths, v)")
				g.P("\t\t\tout.", protoFieldName, "[i] = &", fmType, "{Paths: paths}")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindStruct:
				structNew := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/structpb", GoName: "NewStruct"})
				spbStruct := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/structpb", GoName: "Struct"})
				logPrintf := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "log", GoName: "Printf"})
				g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
				g.P("\t\tout.", protoFieldName, " = make([]*", spbStruct, ", len(", recv, ".", domainFieldName, "))")
				g.P("\t\tfor i, v := range ", recv, ".", domainFieldName, " {")
				g.P("\t\t\ts, err := ", structNew, "(v)")
				g.P("\t\t\tif err != nil {")
				g.P("\t\t\t\t", logPrintf, "(\"proto2type: failed to convert %s.", domainFieldName, "[%d] to Struct: %v\", \"", structName, "\", i, err)")
				g.P("\t\t\t\tcontinue")
				g.P("\t\t\t}")
				g.P("\t\t\tout.", protoFieldName, "[i] = s")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindListValue:
				listNew := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/structpb", GoName: "NewList"})
				spbListValue := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/structpb", GoName: "ListValue"})
				logPrintf := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "log", GoName: "Printf"})
				g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
				g.P("\t\tout.", protoFieldName, " = make([]*", spbListValue, ", len(", recv, ".", domainFieldName, "))")
				g.P("\t\tfor i, v := range ", recv, ".", domainFieldName, " {")
				g.P("\t\t\tl, err := ", listNew, "(v)")
				g.P("\t\t\tif err != nil {")
				g.P("\t\t\t\t", logPrintf, "(\"proto2type: failed to convert %s.", domainFieldName, "[%d] to ListValue: %v\", \"", structName, "\", i, err)")
				g.P("\t\t\t\tcontinue")
				g.P("\t\t\t}")
				g.P("\t\t\tout.", protoFieldName, "[i] = l")
				g.P("\t\t}")
				g.P("\t}")
			default:
				if f.Kind == FieldKindWrapperBytes {
					// Repeated BytesValue wrapper: deep copy to prevent aliasing (SEC-3)
					wrapperFunc := g.QualifiedGoIdent(protogen.GoIdent{
						GoImportPath: "google.golang.org/protobuf/types/known/wrapperspb",
						GoName:       "Bytes",
					})
					wrapperType := g.QualifiedGoIdent(protogen.GoIdent{
						GoImportPath: "google.golang.org/protobuf/types/known/wrapperspb",
						GoName:       "BytesValue",
					})
					g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
					g.P("\t\tout.", protoFieldName, " = make([]*", wrapperType, ", len(", recv, ".", domainFieldName, "))")
					g.P("\t\tfor i, v := range ", recv, ".", domainFieldName, " {")
					g.P("\t\t\tif v != nil {")
					g.P("\t\t\t\tb := make([]byte, len(*v))")
					g.P("\t\t\t\tcopy(b, *v)")
					g.P("\t\t\t\tout.", protoFieldName, "[i] = ", wrapperFunc, "(b)")
					g.P("\t\t\t}")
					g.P("\t\t}")
					g.P("\t}")
				} else if f.Kind.IsWrapper() {
					// Repeated wrapper: domain []*T → proto []*wrapperspb.T
					funcName := irWrapperPbFuncName(f.Kind)
					wrapperFunc := g.QualifiedGoIdent(protogen.GoIdent{
						GoImportPath: "google.golang.org/protobuf/types/known/wrapperspb",
						GoName:       funcName,
					})
					wrapperType := g.QualifiedGoIdent(protogen.GoIdent{
						GoImportPath: "google.golang.org/protobuf/types/known/wrapperspb",
						GoName:       funcName + "Value",
					})
					g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
					g.P("\t\tout.", protoFieldName, " = make([]*", wrapperType, ", len(", recv, ".", domainFieldName, "))")
					g.P("\t\tfor i, v := range ", recv, ".", domainFieldName, " {")
					g.P("\t\t\tif v != nil {")
					g.P("\t\t\t\tout.", protoFieldName, "[i] = ", wrapperFunc, "(*v)")
					g.P("\t\t\t}")
					g.P("\t\t}")
					g.P("\t}")
				}
			}
			continue
		}

		// Handle map fields with WKT values.
		if f.IsMap && f.MapValue != nil && (f.MapValue.Kind == FieldKindTimestamp || f.MapValue.Kind == FieldKindDuration || f.MapValue.Kind == FieldKindFieldMask || f.MapValue.Kind == FieldKindStruct || f.MapValue.Kind == FieldKindListValue) {
			switch f.MapValue.Kind {
			case FieldKindTimestamp:
				tsNew := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/timestamppb", GoName: "New"})
				tsType := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/timestamppb", GoName: "Timestamp"})
				g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
				g.P("\t\tout.", protoFieldName, " = make(map[string]*", tsType, ", len(", recv, ".", domainFieldName, "))")
				g.P("\t\tfor k, v := range ", recv, ".", domainFieldName, " {")
				g.P("\t\t\tout.", protoFieldName, "[k] = ", tsNew, "(v)")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindDuration:
				durNew := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/durationpb", GoName: "New"})
				durType := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/durationpb", GoName: "Duration"})
				g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
				g.P("\t\tout.", protoFieldName, " = make(map[string]*", durType, ", len(", recv, ".", domainFieldName, "))")
				g.P("\t\tfor k, v := range ", recv, ".", domainFieldName, " {")
				g.P("\t\t\tout.", protoFieldName, "[k] = ", durNew, "(v)")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindFieldMask:
				fmType := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/fieldmaskpb", GoName: "FieldMask"})
				g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
				g.P("\t\tout.", protoFieldName, " = make(map[string]*", fmType, ", len(", recv, ".", domainFieldName, "))")
				g.P("\t\tfor k, v := range ", recv, ".", domainFieldName, " {")
				g.P("\t\t\tpaths := make([]string, len(v))")
				g.P("\t\t\tcopy(paths, v)")
				g.P("\t\t\tout.", protoFieldName, "[k] = &", fmType, "{Paths: paths}")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindStruct:
				structNew := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/structpb", GoName: "NewStruct"})
				spbStruct := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/structpb", GoName: "Struct"})
				logPrintf := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "log", GoName: "Printf"})
				g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
				g.P("\t\tout.", protoFieldName, " = make(map[string]*", spbStruct, ", len(", recv, ".", domainFieldName, "))")
				g.P("\t\tfor k, v := range ", recv, ".", domainFieldName, " {")
				g.P("\t\t\ts, err := ", structNew, "(v)")
				g.P("\t\t\tif err != nil {")
				g.P("\t\t\t\t", logPrintf, "(\"proto2type: failed to convert %s.", domainFieldName, "[%s] to Struct: %v\", \"", structName, "\", k, err)")
				g.P("\t\t\t\tcontinue")
				g.P("\t\t\t}")
				g.P("\t\t\tout.", protoFieldName, "[k] = s")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindListValue:
				listNew := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/structpb", GoName: "NewList"})
				spbListValue := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "google.golang.org/protobuf/types/known/structpb", GoName: "ListValue"})
				logPrintf := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "log", GoName: "Printf"})
				g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
				g.P("\t\tout.", protoFieldName, " = make(map[string]*", spbListValue, ", len(", recv, ".", domainFieldName, "))")
				g.P("\t\tfor k, v := range ", recv, ".", domainFieldName, " {")
				g.P("\t\t\tl, err := ", listNew, "(v)")
				g.P("\t\t\tif err != nil {")
				g.P("\t\t\t\t", logPrintf, "(\"proto2type: failed to convert %s.", domainFieldName, "[%s] to ListValue: %v\", \"", structName, "\", k, err)")
				g.P("\t\t\t\tcontinue")
				g.P("\t\t\t}")
				g.P("\t\t\tout.", protoFieldName, "[k] = l")
				g.P("\t\t}")
				g.P("\t}")
			}
			continue
		}

		if f.Kind == FieldKindTimestamp {
			// timestamppb.New
			tsNew := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "google.golang.org/protobuf/types/known/timestamppb",
				GoName:       "New",
			})
			if f.Optional && structSuffix == "" {
				// Domain struct: optional timestamp is *time.Time
				g.P("\tif ", recv, ".", domainFieldName, " != nil {")
				g.P("\t\tout.", protoFieldName, " = ", tsNew, "(*", recv, ".", domainFieldName, ")")
				g.P("\t}")
			} else {
				g.P("\tif !", recv, ".", domainFieldName, ".IsZero() {")
				g.P("\t\tout.", protoFieldName, " = ", tsNew, "(", recv, ".", domainFieldName, ")")
				g.P("\t}")
			}
		} else if f.Kind == FieldKindDuration {
			// durationpb.New
			durNew := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "google.golang.org/protobuf/types/known/durationpb",
				GoName:       "New",
			})
			if f.Optional && structSuffix == "" {
				// Domain struct: optional duration is *time.Duration
				g.P("\tif ", recv, ".", domainFieldName, " != nil {")
				g.P("\t\tout.", protoFieldName, " = ", durNew, "(*", recv, ".", domainFieldName, ")")
				g.P("\t}")
			} else {
				g.P("\tout.", protoFieldName, " = ", durNew, "(", recv, ".", domainFieldName, ")")
			}
		} else if f.Kind == FieldKindWrapperBytes {
			// BytesValue wrapper: deep copy to prevent aliasing (SEC-3)
			wrapperFunc := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "google.golang.org/protobuf/types/known/wrapperspb",
				GoName:       "Bytes",
			})
			g.P("\tif ", recv, ".", domainFieldName, " != nil {")
			g.P("\t\tb := make([]byte, len(*", recv, ".", domainFieldName, "))")
			g.P("\t\tcopy(b, *", recv, ".", domainFieldName, ")")
			g.P("\t\tout.", protoFieldName, " = ", wrapperFunc, "(b)")
			g.P("\t}")
		} else if f.Kind.IsWrapper() {
			// Wrapper type: if d.Phone != nil { out.Phone = wrapperspb.String(*d.Phone) }
			funcName := irWrapperPbFuncName(f.Kind)
			wrapperFunc := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "google.golang.org/protobuf/types/known/wrapperspb",
				GoName:       funcName,
			})
			g.P("\tif ", recv, ".", domainFieldName, " != nil {")
			g.P("\t\tout.", protoFieldName, " = ", wrapperFunc, "(*", recv, ".", domainFieldName, ")")
			g.P("\t}")
		} else if f.Kind == FieldKindFieldMask {
			// FieldMask: domain []string → proto *fieldmaskpb.FieldMask (defensive copy per SEC-3)
			fmIdent := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "google.golang.org/protobuf/types/known/fieldmaskpb",
				GoName:       "FieldMask",
			})
			g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
			g.P("\t\tpaths := make([]string, len(", recv, ".", domainFieldName, "))")
			g.P("\t\tcopy(paths, ", recv, ".", domainFieldName, ")")
			g.P("\t\tout.", protoFieldName, " = &", fmIdent, "{Paths: paths}")
			g.P("\t}")
		} else if f.Kind == FieldKindStruct {
			// Struct: domain map[string]any → proto *structpb.Struct
			structNew := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "google.golang.org/protobuf/types/known/structpb",
				GoName:       "NewStruct",
			})
			logPrintf := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "log",
				GoName:       "Printf",
			})
			g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
			g.P("\t\tvar err error")
			g.P("\t\tout.", protoFieldName, ", err = ", structNew, "(", recv, ".", domainFieldName, ")")
			g.P("\t\tif err != nil {")
			g.P("\t\t\t", logPrintf, "(\"proto2type: failed to convert %s.", domainFieldName, " to Struct: %v\", \"", structName, "\", err)")
			g.P("\t\t\tout.", protoFieldName, " = nil")
			g.P("\t\t}")
			g.P("\t}")
		} else if f.Kind == FieldKindListValue {
			// ListValue: domain []any → proto *structpb.ListValue
			listNew := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "google.golang.org/protobuf/types/known/structpb",
				GoName:       "NewList",
			})
			logPrintf2 := g.QualifiedGoIdent(protogen.GoIdent{
				GoImportPath: "log",
				GoName:       "Printf",
			})
			g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
			g.P("\t\tvar err error")
			g.P("\t\tout.", protoFieldName, ", err = ", listNew, "(", recv, ".", domainFieldName, ")")
			g.P("\t\tif err != nil {")
			g.P("\t\t\t", logPrintf2, "(\"proto2type: failed to convert %s.", domainFieldName, " to ListValue: %v\", \"", structName, "\", err)")
			g.P("\t\t\tout.", protoFieldName, " = nil")
			g.P("\t\t}")
			g.P("\t}")
		} else if f.Kind == FieldKindMessage && !f.Repeated && !f.IsMap {
			// Singular nested message: recursive conversion via ToProto()
			g.P("\tif ", recv, ".", domainFieldName, " != nil {")
			g.P("\t\tout.", protoFieldName, " = ", recv, ".", domainFieldName, ".ToProto()")
			g.P("\t}")
		} else if f.Kind == FieldKindMessage && f.Repeated {
			// Repeated message: loop-based element-wise conversion
			protoElemType := g.QualifiedGoIdent(f.ProtoMessageGoIdent)
			g.P("\tif len(", recv, ".", domainFieldName, ") > 0 {")
			g.P("\t\tout.", protoFieldName, " = make([]*", protoElemType, ", len(", recv, ".", domainFieldName, "))")
			g.P("\t\tfor i, v := range ", recv, ".", domainFieldName, " {")
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\tout.", protoFieldName, "[i] = v.ToProto()")
			g.P("\t\t\t}")
			g.P("\t\t}")
			g.P("\t}")
		} else if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind && f.Optional {
			// Optional bytes: dereference then copy
			g.P("\tif ", recv, ".", domainFieldName, " != nil {")
			g.P("\t\tout.", protoFieldName, " = make([]byte, len(*", recv, ".", domainFieldName, "))")
			g.P("\t\tcopy(out.", protoFieldName, ", *", recv, ".", domainFieldName, ")")
			g.P("\t}")
		} else if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind {
			// Bytes field: defensive copy (SEC-3)
			g.P("\tif ", recv, ".", domainFieldName, " != nil {")
			g.P("\t\tout.", protoFieldName, " = make([]byte, len(", recv, ".", domainFieldName, "))")
			g.P("\t\tcopy(out.", protoFieldName, ", ", recv, ".", domainFieldName, ")")
			g.P("\t}")
		} else if f.Optional && f.Kind == FieldKindEnum {
			// Optional enum: proto uses *EnumType, domain uses *string or *int32.
			enumIdent := g.QualifiedGoIdent(f.ProtoEnumGoIdent)
			if structSuffix == "" {
				// Domain struct: optional enum is pointer type
				g.P("\tif ", recv, ".", domainFieldName, " != nil {")
				if f.EnumAsString {
					g.P("\t\tv := ", enumIdent, "(", enumIdent, "_value[*", recv, ".", domainFieldName, "])")
				} else {
					g.P("\t\tv := ", enumIdent, "(*", recv, ".", domainFieldName, ")")
				}
			} else {
				// Storage struct: optional enum is non-pointer
				if f.EnumAsString {
					g.P("\tif ", recv, ".", domainFieldName, " != \"\" {")
					g.P("\t\tv := ", enumIdent, "(", enumIdent, "_value[", recv, ".", domainFieldName, "])")
				} else {
					g.P("\tif ", recv, ".", domainFieldName, " != 0 {")
					g.P("\t\tv := ", enumIdent, "(", recv, ".", domainFieldName, ")")
				}
			}
			g.P("\t\tout.", protoFieldName, " = &v")
			g.P("\t}")
		} else if f.Optional {
			// Optional scalar: both domain and proto use *T, assign directly (PROTO-3)
			g.P("\tout.", protoFieldName, " = ", recv, ".", domainFieldName, "")
		}
	}

	g.P("\treturn out")
	g.P("}")
	g.P()
}

// generateFromProto generates the FromProto method.
func generateFromProto(g *protogen.GeneratedFile, dm *DomainMessage, structSuffix string, opts *Options) {
	structName := dm.Name + structSuffix
	protoType := g.QualifiedGoIdent(dm.ProtoGoIdent)
	recv := receiverName(structName)
	g.P("// FromProto populates from a protobuf message.")
	g.P("func (", recv, " *", structName, ") FromProto(msg *", protoType, ") {")
	g.P("\tif msg == nil {")
	g.P("\t\treturn")
	g.P("\t}")

	for _, f := range dm.Fields {
		if f.IsOneof {
			oneof := findOneof(dm, f.OneofTypeName)
			// Clear all variants so a reused receiver doesn't retain stale state.
			for _, v := range oneof.Variants {
				g.P("\t", recv, ".", v.Name, " = nil")
			}
			g.P("\tswitch v := msg.Get", oneof.ProtoGoName, "().(type) {")
			for _, v := range oneof.Variants {
				wrapperIdent := g.QualifiedGoIdent(v.ProtoGoIdent)
				g.P("\tcase *", wrapperIdent, ":")
				switch v.Kind {
				case FieldKindScalar:
					g.P("\t\t", recv, ".", v.Name, " = &v.", v.ProtoGoName)
				case FieldKindMessage:
					nestedType := v.TypeName + structSuffix
					g.P("\t\t", recv, ".", v.Name, " = &", nestedType, "{}")
					g.P("\t\t", recv, ".", v.Name, ".FromProto(v.", v.ProtoGoName, ")")
				case FieldKindEnum:
					if v.EnumAsString {
						g.P("\t\tenumVal := v.", v.ProtoGoName, ".String()")
						g.P("\t\t", recv, ".", v.Name, " = &enumVal")
					} else {
						g.P("\t\tenumVal := int32(v.", v.ProtoGoName, ")")
						g.P("\t\t", recv, ".", v.Name, " = &enumVal")
					}
				case FieldKindTimestamp:
					g.P("\t\tif v.", v.ProtoGoName, " != nil {")
					g.P("\t\t\tt := v.", v.ProtoGoName, ".AsTime()")
					g.P("\t\t\t", recv, ".", v.Name, " = &t")
					g.P("\t\t}")
				case FieldKindDuration:
					g.P("\t\tif v.", v.ProtoGoName, " != nil {")
					g.P("\t\t\tdur := v.", v.ProtoGoName, ".AsDuration()")
					g.P("\t\t\t", recv, ".", v.Name, " = &dur")
					g.P("\t\t}")
				}
			}
			g.P("\t}")
			continue
		}
		if f.DocID && structSuffix == "Firestore" {
			continue
		}

		domainFieldName := f.PascalName
		protoFieldName := f.ProtoGoName

		// Handle repeated WKT types with loop-based conversion.
		if f.Repeated && (f.Kind == FieldKindTimestamp || f.Kind == FieldKindDuration || f.Kind == FieldKindFieldMask || f.Kind == FieldKindStruct || f.Kind == FieldKindListValue || f.Kind.IsWrapper()) {
			switch f.Kind {
			case FieldKindTimestamp:
				g.P("\tif len(msg.", protoFieldName, ") > 0 {")
				g.P("\t\t", recv, ".", domainFieldName, " = make([]time.Time, len(msg.", protoFieldName, "))")
				g.P("\t\tfor i, v := range msg.", protoFieldName, " {")
				g.P("\t\t\tif v != nil {")
				g.P("\t\t\t\t", recv, ".", domainFieldName, "[i] = v.AsTime()")
				g.P("\t\t\t}")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindDuration:
				g.P("\tif len(msg.", protoFieldName, ") > 0 {")
				g.P("\t\t", recv, ".", domainFieldName, " = make([]time.Duration, len(msg.", protoFieldName, "))")
				g.P("\t\tfor i, v := range msg.", protoFieldName, " {")
				g.P("\t\t\tif v != nil {")
				g.P("\t\t\t\t", recv, ".", domainFieldName, "[i] = v.AsDuration()")
				g.P("\t\t\t}")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindFieldMask:
				g.P("\tif len(msg.", protoFieldName, ") > 0 {")
				g.P("\t\t", recv, ".", domainFieldName, " = make([][]string, len(msg.", protoFieldName, "))")
				g.P("\t\tfor i, v := range msg.", protoFieldName, " {")
				g.P("\t\t\tif v != nil {")
				g.P("\t\t\t\tsrc := v.GetPaths()")
				g.P("\t\t\t\t", recv, ".", domainFieldName, "[i] = make([]string, len(src))")
				g.P("\t\t\t\tcopy(", recv, ".", domainFieldName, "[i], src)")
				g.P("\t\t\t}")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindStruct:
				g.P("\tif len(msg.", protoFieldName, ") > 0 {")
				g.P("\t\t", recv, ".", domainFieldName, " = make([]map[string]any, len(msg.", protoFieldName, "))")
				g.P("\t\tfor i, v := range msg.", protoFieldName, " {")
				g.P("\t\t\tif v != nil {")
				g.P("\t\t\t\t", recv, ".", domainFieldName, "[i] = v.AsMap()")
				g.P("\t\t\t}")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindListValue:
				g.P("\tif len(msg.", protoFieldName, ") > 0 {")
				g.P("\t\t", recv, ".", domainFieldName, " = make([][]any, len(msg.", protoFieldName, "))")
				g.P("\t\tfor i, v := range msg.", protoFieldName, " {")
				g.P("\t\t\tif v != nil {")
				g.P("\t\t\t\t", recv, ".", domainFieldName, "[i] = v.AsSlice()")
				g.P("\t\t\t}")
				g.P("\t\t}")
				g.P("\t}")
			default:
				if f.Kind == FieldKindWrapperBytes {
					// Repeated BytesValue wrapper: deep copy to prevent aliasing (SEC-3)
					g.P("\tif len(msg.", protoFieldName, ") > 0 {")
					g.P("\t\t", recv, ".", domainFieldName, " = make([]*[]byte, len(msg.", protoFieldName, "))")
					g.P("\t\tfor i, v := range msg.", protoFieldName, " {")
					g.P("\t\t\tif v != nil {")
					g.P("\t\t\t\tb := make([]byte, len(v.GetValue()))")
					g.P("\t\t\t\tcopy(b, v.GetValue())")
					g.P("\t\t\t\t", recv, ".", domainFieldName, "[i] = &b")
					g.P("\t\t\t}")
					g.P("\t\t}")
					g.P("\t}")
				} else if f.Kind.IsWrapper() {
					// Repeated wrapper: proto []*wrapperspb.T → domain []*T
					g.P("\tif len(msg.", protoFieldName, ") > 0 {")
					g.P("\t\t", recv, ".", domainFieldName, " = make(", irWrapperGoSliceType(f.Kind), ", len(msg.", protoFieldName, "))")
					g.P("\t\tfor i, v := range msg.", protoFieldName, " {")
					g.P("\t\t\tif v != nil {")
					g.P("\t\t\t\tval := v.GetValue()")
					g.P("\t\t\t\t", recv, ".", domainFieldName, "[i] = &val")
					g.P("\t\t\t}")
					g.P("\t\t}")
					g.P("\t}")
				}
			}
			continue
		}

		if f.Kind == FieldKindTimestamp {
			g.P("\tif msg.", protoFieldName, " != nil {")
			if f.Optional && structSuffix == "" {
				// Domain struct: optional timestamp is *time.Time
				g.P("\t\tv := msg.", protoFieldName, ".AsTime()")
				g.P("\t\t", recv, ".", domainFieldName, " = &v")
			} else {
				g.P("\t\t", recv, ".", domainFieldName, " = msg.", protoFieldName, ".AsTime()")
			}
			g.P("\t}")
		} else if f.Kind == FieldKindDuration {
			g.P("\tif msg.", protoFieldName, " != nil {")
			if f.Optional && structSuffix == "" {
				// Domain struct: optional duration is *time.Duration
				g.P("\t\tv := msg.", protoFieldName, ".AsDuration()")
				g.P("\t\t", recv, ".", domainFieldName, " = &v")
			} else {
				g.P("\t\t", recv, ".", domainFieldName, " = msg.", protoFieldName, ".AsDuration()")
			}
			g.P("\t}")
		} else if f.Kind == FieldKindWrapperBytes {
			// BytesValue wrapper: deep copy to prevent aliasing (SEC-3)
			g.P("\tif msg.", protoFieldName, " != nil {")
			g.P("\t\tsrc := msg.", protoFieldName, ".GetValue()")
			g.P("\t\tb := make([]byte, len(src))")
			g.P("\t\tcopy(b, src)")
			g.P("\t\t", recv, ".", domainFieldName, " = &b")
			g.P("\t}")
		} else if f.Kind.IsWrapper() {
			// Wrapper type: if msg.Phone != nil { v := msg.Phone.GetValue(); d.Phone = &v }
			g.P("\tif msg.", protoFieldName, " != nil {")
			g.P("\t\tv := msg.", protoFieldName, ".GetValue()")
			g.P("\t\t", recv, ".", domainFieldName, " = &v")
			g.P("\t}")
		} else if f.Kind == FieldKindFieldMask {
			// FieldMask: proto *fieldmaskpb.FieldMask → domain []string (defensive copy per SEC-3)
			g.P("\tif msg.", protoFieldName, " != nil {")
			g.P("\t\tsrc := msg.", protoFieldName, ".GetPaths()")
			g.P("\t\t", recv, ".", domainFieldName, " = make([]string, len(src))")
			g.P("\t\tcopy(", recv, ".", domainFieldName, ", src)")
			g.P("\t}")
		} else if f.Kind == FieldKindStruct {
			// Struct: proto *structpb.Struct → domain map[string]any
			g.P("\tif msg.", protoFieldName, " != nil {")
			g.P("\t\t", recv, ".", domainFieldName, " = msg.", protoFieldName, ".AsMap()")
			g.P("\t}")
		} else if f.Kind == FieldKindListValue {
			// ListValue: proto *structpb.ListValue → domain []any
			g.P("\tif msg.", protoFieldName, " != nil {")
			g.P("\t\t", recv, ".", domainFieldName, " = msg.", protoFieldName, ".AsSlice()")
			g.P("\t}")
		} else if f.Kind == FieldKindMessage && !f.Repeated && !f.IsMap {
			// Singular nested message: recursive conversion via FromProto()
			nestedType := f.MessageTypeName + structSuffix
			g.P("\tif msg.", protoFieldName, " != nil {")
			g.P("\t\t", recv, ".", domainFieldName, " = &", nestedType, "{}")
			g.P("\t\t", recv, ".", domainFieldName, ".FromProto(msg.", protoFieldName, ")")
			g.P("\t}")
		} else if f.Kind == FieldKindMessage && f.Repeated {
			// Repeated message: loop-based element-wise conversion
			nestedType := f.MessageTypeName + structSuffix
			g.P("\tif len(msg.", protoFieldName, ") > 0 {")
			g.P("\t\t", recv, ".", domainFieldName, " = make([]*", nestedType, ", len(msg.", protoFieldName, "))")
			g.P("\t\tfor i, v := range msg.", protoFieldName, " {")
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\telem := &", nestedType, "{}")
			g.P("\t\t\t\telem.FromProto(v)")
			g.P("\t\t\t\t", recv, ".", domainFieldName, "[i] = elem")
			g.P("\t\t\t}")
			g.P("\t\t}")
			g.P("\t}")
		} else if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind && f.Optional {
			// Optional bytes: copy into pointer
			g.P("\tif msg.", protoFieldName, " != nil {")
			g.P("\t\tb := make([]byte, len(msg.", protoFieldName, "))")
			g.P("\t\tcopy(b, msg.", protoFieldName, ")")
			g.P("\t\t", recv, ".", domainFieldName, " = &b")
			g.P("\t}")
		} else if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind {
			// Bytes field: defensive copy (SEC-3)
			g.P("\tif msg.", protoFieldName, " != nil {")
			g.P("\t\t", recv, ".", domainFieldName, " = make([]byte, len(msg.", protoFieldName, "))")
			g.P("\t\tcopy(", recv, ".", domainFieldName, ", msg.", protoFieldName, ")")
			g.P("\t}")
		} else if f.Optional && f.Kind == FieldKindEnum {
			// Optional enum: proto uses *EnumType, domain uses *string or *int32.
			g.P("\tif msg.", protoFieldName, " != nil {")
			if structSuffix == "" {
				// Domain struct: optional enum is pointer type
				if f.EnumAsString {
					g.P("\t\tv := msg.Get", protoFieldName, "().String()")
					g.P("\t\t", recv, ".", domainFieldName, " = &v")
				} else {
					g.P("\t\tv := int32(msg.Get", protoFieldName, "())")
					g.P("\t\t", recv, ".", domainFieldName, " = &v")
				}
			} else {
				// Storage struct: optional enum is non-pointer
				if f.EnumAsString {
					g.P("\t\t", recv, ".", domainFieldName, " = msg.Get", protoFieldName, "().String()")
				} else {
					g.P("\t\t", recv, ".", domainFieldName, " = int32(msg.Get", protoFieldName, "())")
				}
			}
			g.P("\t}")
		} else if f.Optional {
			// Optional scalar: both proto and domain use *T, assign directly (PROTO-3)
			g.P("\t", recv, ".", domainFieldName, " = msg.", protoFieldName)
		} else if f.Kind == FieldKindEnum {
			if f.EnumAsString {
				// String enum: convert proto enum to its string name
				g.P("\t", recv, ".", domainFieldName, " = msg.", protoFieldName, ".String()")
			} else {
				// Int32 enum: direct cast
				g.P("\t", recv, ".", domainFieldName, " = int32(msg.", protoFieldName, ")")
			}
		} else if f.IsMap && f.MapValue != nil {
			switch f.MapValue.Kind {
			case FieldKindTimestamp:
				g.P("\tif len(msg.", protoFieldName, ") > 0 {")
				g.P("\t\t", recv, ".", domainFieldName, " = make(map[string]time.Time, len(msg.", protoFieldName, "))")
				g.P("\t\tfor k, v := range msg.", protoFieldName, " {")
				g.P("\t\t\tif v != nil {")
				g.P("\t\t\t\t", recv, ".", domainFieldName, "[k] = v.AsTime()")
				g.P("\t\t\t}")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindDuration:
				g.P("\tif len(msg.", protoFieldName, ") > 0 {")
				g.P("\t\t", recv, ".", domainFieldName, " = make(map[string]time.Duration, len(msg.", protoFieldName, "))")
				g.P("\t\tfor k, v := range msg.", protoFieldName, " {")
				g.P("\t\t\tif v != nil {")
				g.P("\t\t\t\t", recv, ".", domainFieldName, "[k] = v.AsDuration()")
				g.P("\t\t\t}")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindStruct:
				g.P("\tif len(msg.", protoFieldName, ") > 0 {")
				g.P("\t\t", recv, ".", domainFieldName, " = make(map[string]map[string]any, len(msg.", protoFieldName, "))")
				g.P("\t\tfor k, v := range msg.", protoFieldName, " {")
				g.P("\t\t\tif v != nil {")
				g.P("\t\t\t\t", recv, ".", domainFieldName, "[k] = v.AsMap()")
				g.P("\t\t\t}")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindListValue:
				g.P("\tif len(msg.", protoFieldName, ") > 0 {")
				g.P("\t\t", recv, ".", domainFieldName, " = make(map[string][]any, len(msg.", protoFieldName, "))")
				g.P("\t\tfor k, v := range msg.", protoFieldName, " {")
				g.P("\t\t\tif v != nil {")
				g.P("\t\t\t\t", recv, ".", domainFieldName, "[k] = v.AsSlice()")
				g.P("\t\t\t}")
				g.P("\t\t}")
				g.P("\t}")
			case FieldKindFieldMask:
				g.P("\tif len(msg.", protoFieldName, ") > 0 {")
				g.P("\t\t", recv, ".", domainFieldName, " = make(map[string][]string, len(msg.", protoFieldName, "))")
				g.P("\t\tfor k, v := range msg.", protoFieldName, " {")
				g.P("\t\t\tif v != nil {")
				g.P("\t\t\t\tsrc := v.GetPaths()")
				g.P("\t\t\t\tdst := make([]string, len(src))")
				g.P("\t\t\t\tcopy(dst, src)")
				g.P("\t\t\t\t", recv, ".", domainFieldName, "[k] = dst")
				g.P("\t\t\t}")
				g.P("\t\t}")
				g.P("\t}")
			default:
				// Non-WKT map values: direct assignment
				g.P("\t", recv, ".", domainFieldName, " = msg.", protoFieldName)
			}
		} else {
			// Scalars, repeated, maps: direct assignment
			g.P("\t", recv, ".", domainFieldName, " = msg.", protoFieldName)
		}
	}

	g.P("}")
	g.P()
}

// generateDomainConverters generates ToDomain/FromDomain methods for a storage struct.
func generateDomainConverters(g *protogen.GeneratedFile, dm *DomainMessage, storageSuffix string) {
	storageType := dm.Name + storageSuffix
	domainType := dm.Name
	recv := receiverName(storageType)
	// Avoid shadowing the domain variable "d" used in ToDomain/FromDomain.
	if recv == "d" {
		recv = "s"
	}

	// Determine if this is a Firestore type and find the document_id field.
	isFirestore := storageSuffix == "Firestore"
	var docIDFieldName string
	if isFirestore {
		for _, f := range dm.Fields {
			if f.DocID {
				docIDFieldName = f.PascalName
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
	for _, f := range dm.Fields {
		if f.IsOneof {
			// Oneof variants: copy pointer fields directly (domain and storage have same layout)
			oneof := findOneof(dm, f.OneofTypeName)
			for _, v := range oneof.Variants {
				g.P("\t\t", v.Name, ": ", recv, ".", v.Name, ",")
			}
			continue
		}
		// For Firestore: document_id fields are not in the storage struct
		// They need to be passed separately — skip in direct assignment
		if f.DocID && isFirestore {
			continue
		}
		// Skip message fields — handle recursively below
		// This covers both singular (isNestedMessage) and repeated messages.
		if f.Kind == FieldKindMessage && !f.IsMap {
			continue
		}
		fieldName := f.PascalName
		if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind {
			// Skip bytes — handled with deep copy below
			continue
		}
		// Optional timestamp/duration/enum: storage is non-pointer, domain is pointer
		if f.Optional && (f.Kind == FieldKindTimestamp || f.Kind == FieldKindDuration) {
			// Skip from struct literal — handle below with address-of
			continue
		}
		if f.Optional && f.Kind == FieldKindEnum {
			// Skip from struct literal — handle below with pointer wrap
			continue
		}
		g.P("\t\t", fieldName, ": ", recv, ".", fieldName, ",")
	}
	g.P("\t}")

	// Deep copy bytes fields (SEC-3)
	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		if f.DocID && isFirestore {
			continue
		}
		if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind {
			fieldName := f.PascalName
			if f.Optional {
				// Optional bytes: *[]byte — dereference, copy, re-ref
				g.P("\tif ", recv, ".", fieldName, " != nil {")
				g.P("\t\tb := make([]byte, len(*", recv, ".", fieldName, "))")
				g.P("\t\tcopy(b, *", recv, ".", fieldName, ")")
				g.P("\t\td.", fieldName, " = &b")
				g.P("\t}")
			} else {
				g.P("\tif ", recv, ".", fieldName, " != nil {")
				g.P("\t\td.", fieldName, " = make([]byte, len(", recv, ".", fieldName, "))")
				g.P("\t\tcopy(d.", fieldName, ", ", recv, ".", fieldName, ")")
				g.P("\t}")
			}
		}
	}

	// Assign document ID from parameter (Firestore only)
	if isFirestore && docIDFieldName != "" {
		g.P("\td.", docIDFieldName, " = documentID")
	}

	// Handle optional timestamp/duration/enum: storage T -> domain *T
	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		if f.DocID && isFirestore {
			continue
		}
		if !f.Optional {
			continue
		}
		fieldName := f.PascalName
		switch f.Kind {
		case FieldKindTimestamp, FieldKindDuration:
			// Storage has time.Time, domain has *time.Time — take address
			g.P("\tv", fieldName, " := ", recv, ".", fieldName)
			g.P("\td.", fieldName, " = &v", fieldName)
		case FieldKindEnum:
			// Storage has int32/string, domain has *int32/*string — take address
			g.P("\tv", fieldName, " := ", recv, ".", fieldName)
			g.P("\td.", fieldName, " = &v", fieldName)
		}
	}

	// Handle nested message fields with recursive conversion
	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		if f.DocID && isFirestore {
			continue
		}
		fieldName := f.PascalName
		if f.Kind == FieldKindMessage && !f.Repeated && !f.IsMap {
			// Singular nested message
			g.P("\tif ", recv, ".", fieldName, " != nil {")
			g.P("\t\td.", fieldName, " = ", recv, ".", fieldName, ".ToDomain()")
			g.P("\t}")
		} else if f.Kind == FieldKindMessage && f.Repeated {
			// Repeated message: loop-based element-wise conversion
			nestedDomainType := f.MessageTypeName
			g.P("\tif len(", recv, ".", fieldName, ") > 0 {")
			g.P("\t\td.", fieldName, " = make([]*", nestedDomainType, ", len(", recv, ".", fieldName, "))")
			g.P("\t\tfor i, v := range ", recv, ".", fieldName, " {")
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\td.", fieldName, "[i] = v.ToDomain()")
			g.P("\t\t\t}")
			g.P("\t\t}")
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
	for _, f := range dm.Fields {
		if f.IsOneof {
			// Oneof variants: copy pointer fields directly
			oneof := findOneof(dm, f.OneofTypeName)
			for _, v := range oneof.Variants {
				g.P("\t", recv, ".", v.Name, " = d.", v.Name)
			}
			continue
		}
		if f.DocID && isFirestore {
			continue
		}
		fieldName := f.PascalName
		if f.Kind == FieldKindMessage && !f.Repeated && !f.IsMap {
			// Singular nested message: recursive conversion via FromDomain
			nestedType := f.MessageTypeName + storageSuffix
			g.P("\tif d.", fieldName, " != nil {")
			g.P("\t\t", recv, ".", fieldName, " = &", nestedType, "{}")
			g.P("\t\t", recv, ".", fieldName, ".FromDomain(d.", fieldName, ")")
			g.P("\t}")
		} else if f.Kind == FieldKindMessage && f.Repeated {
			// Repeated message: loop-based element-wise conversion
			nestedType := f.MessageTypeName + storageSuffix
			g.P("\tif len(d.", fieldName, ") > 0 {")
			g.P("\t\t", recv, ".", fieldName, " = make([]*", nestedType, ", len(d.", fieldName, "))")
			g.P("\t\tfor i, v := range d.", fieldName, " {")
			g.P("\t\t\tif v != nil {")
			g.P("\t\t\t\telem := &", nestedType, "{}")
			g.P("\t\t\t\telem.FromDomain(v)")
			g.P("\t\t\t\t", recv, ".", fieldName, "[i] = elem")
			g.P("\t\t\t}")
			g.P("\t\t}")
			g.P("\t}")
		} else if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind && f.Optional {
			// Deep copy optional bytes: domain *[]byte → storage *[]byte
			g.P("\tif d.", fieldName, " != nil {")
			g.P("\t\tb := make([]byte, len(*d.", fieldName, "))")
			g.P("\t\tcopy(b, *d.", fieldName, ")")
			g.P("\t\t", recv, ".", fieldName, " = &b")
			g.P("\t}")
		} else if f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind {
			// Deep copy bytes fields (SEC-3)
			g.P("\tif d.", fieldName, " != nil {")
			g.P("\t\t", recv, ".", fieldName, " = make([]byte, len(d.", fieldName, "))")
			g.P("\t\tcopy(", recv, ".", fieldName, ", d.", fieldName, ")")
			g.P("\t}")
		} else if f.Optional && (f.Kind == FieldKindTimestamp || f.Kind == FieldKindDuration) {
			// Optional timestamp/duration: domain *T -> storage T (dereference)
			g.P("\tif d.", fieldName, " != nil {")
			g.P("\t\t", recv, ".", fieldName, " = *d.", fieldName)
			g.P("\t}")
		} else if f.Optional && f.Kind == FieldKindEnum {
			// Optional enum: domain *int32/*string -> storage int32/string (dereference)
			g.P("\tif d.", fieldName, " != nil {")
			g.P("\t\t", recv, ".", fieldName, " = *d.", fieldName)
			g.P("\t}")
		} else {
			g.P("\t", recv, ".", fieldName, " = d.", fieldName)
		}
	}
	g.P("}")
	g.P()
}
