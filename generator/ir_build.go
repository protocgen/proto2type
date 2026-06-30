package generator

import (
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// BuildDomainFile builds the IR for a single proto source file.
// It consolidates the scanning/walking logic that was previously duplicated
// across go_domain.go and rust_domain.go.
func BuildDomainFile(file *protogen.File, opts *Options) *DomainFile {
	df := &DomainFile{
		SourcePath: file.Desc.Path(),
		Package:    string(file.Desc.Package()),
	}

	// Top-level enums.
	for _, enum := range file.Enums {
		df.Enums = append(df.Enums, buildDomainEnum(enum, ""))
	}

	// Top-level messages.
	for _, msg := range file.Messages {
		dm := buildDomainMessage(msg, "", opts)
		if dm != nil {
			df.Messages = append(df.Messages, dm)
		}
	}

	return df
}

// buildDomainMessage recursively builds the IR for a message and its children.
// parentName is empty for top-level messages; for nested messages it is the
// flattened parent name (e.g. "User" → nested "Settings" becomes "User_Settings").
// Returns nil if the message is skipped.
func buildDomainMessage(msg *protogen.Message, parentName string, opts *Options) *DomainMessage {
	if isMessageSkipped(msg) {
		return &DomainMessage{
			Name:     irMessageName(msg, parentName),
			FullName: string(msg.Desc.FullName()),
			Skip:     true,
		}
	}

	name := irMessageName(msg, parentName)

	dm := &DomainMessage{
		Name:     name,
		FullName: string(msg.Desc.FullName()),
		Comment:  cleanComment(string(msg.Comments.Leading)),
	}

	dm.ProtoGoIdent = msg.GoIdent
	// Detect non-synthetic oneofs for converter warning comments.
	for _, o := range msg.Oneofs {
		if !o.Desc.IsSynthetic() {
			dm.HasNonSyntheticOneof = true
			break
		}
	}

	// Nested enums (before fields, so enum type names are available).
	for _, enum := range msg.Enums {
		dm.NestedEnums = append(dm.NestedEnums, buildDomainEnum(enum, name))
	}

	// Track oneofs we've already processed.
	seenOneofs := map[string]bool{}

	for _, field := range msg.Fields {
		// Oneof members: collect into DomainOneof instead of Fields.
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			oneofName := string(field.Oneof.Desc.Name())
			if !seenOneofs[oneofName] {
				seenOneofs[oneofName] = true
				do := buildDomainOneof(msg, field.Oneof, name, opts)
				dm.Oneofs = append(dm.Oneofs, do)
				// Insert a collapsed oneof placeholder into Fields at the
				// correct proto declaration position.
				dm.Fields = append(dm.Fields, &DomainField{
					Name:          oneofName,
					PascalName:    toPascalCase(oneofName),
					CamelName:     toCamelCase(oneofName),
					IsOneof:       true,
					OneofTypeName: do.Name,
				})
			}
			continue
		}

		if isFieldSkipped(field) {
			continue
		}

		df := buildDomainField(field, opts)
		if df.DocID {
			dm.HasDocID = true
		}
		dm.Fields = append(dm.Fields, df)
	}

	// Nested messages (skip synthetic map-entry messages).
	for _, nested := range msg.Messages {
		if nested.Desc.IsMapEntry() {
			continue
		}
		child := buildDomainMessage(nested, name, opts)
		if child != nil {
			dm.NestedMessages = append(dm.NestedMessages, child)
		}
	}

	return dm
}

// buildDomainField builds the IR for a single field.
func buildDomainField(field *protogen.Field, opts *Options) *DomainField {
	protoName := string(field.Desc.Name())

	df := &DomainField{
		Name:       protoName,
		PascalName: toPascalCase(protoName),
		CamelName:  toCamelCase(protoName),
		Optional:   field.Desc.HasOptionalKeyword(),
		Repeated:   field.Desc.IsList(),
		IsMap:      field.Desc.IsMap(),

		// Annotations
		DocID:           isDocumentID(field),
		ServerTimestamp: isServerTimestamp(field),
		FieldSkip:       isFieldSkipped(field),
		NameOverride:    fieldNameOverride(field),
		Inline:          isInline(field),
		EnumAsString:    isEnumAsString(field, opts),
		Omitempty:       shouldOmitempty(field, opts),
	}

	df.ProtoGoName = field.GoName
	if field.Enum != nil {
		df.ProtoEnumGoIdent = field.Enum.GoIdent
	}
	if field.Message != nil {
		df.ProtoMessageGoIdent = field.Message.GoIdent
	}

	// Map fields.
	if field.Desc.IsMap() {
		df.MapKey = classifyMapPart(field.Desc.MapKey(), nil, opts)
		df.MapValue = classifyMapValue(field, opts)
		// The "outer" kind for the field itself is still Message (map entries
		// are messages), but we record FieldKindMessage to indicate it's a map.
		// Consumers check IsMap first.
		df.Kind = FieldKindMessage
		return df
	}

	// Repeated and singular fields.
	kind, scalarKind := classifyField(field)
	df.Kind = kind
	df.ScalarKind = scalarKind

	if kind == FieldKindMessage {
		df.MessageTypeName = irMessageNameFromDesc(field.Desc.Message())
	}
	if kind == FieldKindEnum {
		df.EnumTypeName = irEnumNameFromDesc(field.Desc.Enum())
		if enumDesc := field.Desc.Enum(); enumDesc.Values().Len() > 0 {
			df.EnumDefaultName = string(enumDesc.Values().Get(0).Name())
		}
	}

	return df
}

// classifyField returns the FieldKind and (for scalars) the ScalarKind.
func classifyField(field *protogen.Field) (FieldKind, protoreflect.Kind) {
	if isWellKnownTimestamp(field) {
		return FieldKindTimestamp, 0
	}
	if isWellKnownDuration(field) {
		return FieldKindDuration, 0
	}
	if isWellKnownWrapper(field) {
		return classifyWrapper(field), 0
	}
	if isWellKnownStruct(field) {
		return FieldKindStruct, 0
	}
	if isWellKnownValue(field) {
		return FieldKindValue, 0
	}
	if isWellKnownListValue(field) {
		return FieldKindListValue, 0
	}
	if isWellKnownFieldMask(field) {
		return FieldKindFieldMask, 0
	}
	if isWellKnownEmpty(field) {
		return FieldKindEmpty, 0
	}
	if isWellKnownAny(field) {
		return FieldKindAny, 0
	}
	if field.Desc.Kind() == protoreflect.MessageKind {
		return FieldKindMessage, 0
	}
	if field.Desc.Kind() == protoreflect.EnumKind {
		return FieldKindEnum, 0
	}
	return FieldKindScalar, field.Desc.Kind()
}

// classifyWrapper returns the specific wrapper FieldKind.
func classifyWrapper(field *protogen.Field) FieldKind {
	switch string(field.Desc.Message().FullName()) {
	case "google.protobuf.BoolValue":
		return FieldKindWrapperBool
	case "google.protobuf.Int32Value":
		return FieldKindWrapperInt32
	case "google.protobuf.Int64Value":
		return FieldKindWrapperInt64
	case "google.protobuf.UInt32Value":
		return FieldKindWrapperUInt32
	case "google.protobuf.UInt64Value":
		return FieldKindWrapperUInt64
	case "google.protobuf.FloatValue":
		return FieldKindWrapperFloat
	case "google.protobuf.DoubleValue":
		return FieldKindWrapperDouble
	case "google.protobuf.StringValue":
		return FieldKindWrapperString
	case "google.protobuf.BytesValue":
		return FieldKindWrapperBytes
	default:
		return FieldKindWrapperString // fallback
	}
}

// classifyMapPart classifies a map key descriptor.
func classifyMapPart(fd protoreflect.FieldDescriptor, _ *protogen.Field, _ *Options) *MapTypeInfo {
	return &MapTypeInfo{
		Kind:       FieldKindScalar,
		ScalarKind: fd.Kind(),
	}
}

// classifyMapValue classifies the value part of a map field.
func classifyMapValue(field *protogen.Field, opts *Options) *MapTypeInfo {
	valDesc := field.Desc.MapValue()
	mi := &MapTypeInfo{}

	if valDesc.Kind() == protoreflect.MessageKind {
		fullName := string(valDesc.Message().FullName())
		switch fullName {
		case "google.protobuf.Timestamp":
			mi.Kind = FieldKindTimestamp
		case "google.protobuf.Duration":
			mi.Kind = FieldKindDuration
		case "google.protobuf.Struct":
			mi.Kind = FieldKindStruct
		case "google.protobuf.Value":
			mi.Kind = FieldKindValue
		case "google.protobuf.ListValue":
			mi.Kind = FieldKindListValue
		case "google.protobuf.FieldMask":
			mi.Kind = FieldKindFieldMask
		case "google.protobuf.Empty":
			mi.Kind = FieldKindEmpty
		case "google.protobuf.Any":
			mi.Kind = FieldKindAny
		case "google.protobuf.StringValue":
			mi.Kind = FieldKindWrapperString
		case "google.protobuf.BoolValue":
			mi.Kind = FieldKindWrapperBool
		case "google.protobuf.Int32Value":
			mi.Kind = FieldKindWrapperInt32
		case "google.protobuf.Int64Value":
			mi.Kind = FieldKindWrapperInt64
		case "google.protobuf.UInt32Value":
			mi.Kind = FieldKindWrapperUInt32
		case "google.protobuf.UInt64Value":
			mi.Kind = FieldKindWrapperUInt64
		case "google.protobuf.FloatValue":
			mi.Kind = FieldKindWrapperFloat
		case "google.protobuf.DoubleValue":
			mi.Kind = FieldKindWrapperDouble
		case "google.protobuf.BytesValue":
			mi.Kind = FieldKindWrapperBytes
		default:
			mi.Kind = FieldKindMessage
			mi.MessageTypeName = irMessageNameFromDesc(valDesc.Message())
		}
		return mi
	}

	if valDesc.Kind() == protoreflect.EnumKind {
		mi.Kind = FieldKindEnum
		mi.EnumTypeName = irEnumNameFromDesc(valDesc.Enum())
		return mi
	}

	mi.Kind = FieldKindScalar
	mi.ScalarKind = valDesc.Kind()
	return mi
}

// buildDomainEnum builds the IR for a proto enum.
func buildDomainEnum(enum *protogen.Enum, parentName string) *DomainEnum {
	enumName := toPascalCase(string(enum.Desc.Name()))
	if parentName != "" {
		enumName = parentName + enumName
	}

	de := &DomainEnum{
		Name:    enumName,
		Comment: cleanComment(string(enum.Comments.Leading)),
	}

	for i, val := range enum.Values {
		num := val.Desc.Number()
		de.Values = append(de.Values, &DomainEnumValue{
			Name:      stripEnumPrefix(enumName, string(val.Desc.Name())),
			ProtoName: string(val.Desc.Name()),
			Number:    int32(num),
			IsDefault: i == 0 && num == 0,
			Comment:   cleanComment(string(val.Comments.Leading)),
		})
	}

	return de
}

// buildDomainOneof builds the IR for a oneof group.
func buildDomainOneof(msg *protogen.Message, oneof *protogen.Oneof, msgIRName string, opts *Options) *DomainOneof {
	oneofPascal := toPascalCase(string(oneof.Desc.Name()))

	do := &DomainOneof{
		Name:      msgIRName + oneofPascal,
		FieldName: string(oneof.Desc.Name()),
	}

	for _, field := range oneof.Fields {
		kind, scalarKind := classifyField(field)
		variant := &OneofVariant{
			Name:       toPascalCase(string(field.Desc.Name())),
			ProtoName:  string(field.Desc.Name()),
			Kind:       kind,
			ScalarKind: scalarKind,
		}

		switch kind {
		case FieldKindMessage:
			variant.TypeName = irMessageNameFromDesc(field.Desc.Message())
		case FieldKindEnum:
			if isEnumAsString(field, opts) {
				variant.EnumAsString = true
			} else {
				variant.TypeName = irEnumNameFromDesc(field.Desc.Enum())
			}
		}

		do.Variants = append(do.Variants, variant)
	}

	return do
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// irMessageName returns the flattened IR name for a message.
// Top-level: PascalCase(name). Nested: ParentChild (PascalCase concat).
func irMessageName(msg *protogen.Message, parentName string) string {
	name := toPascalCase(string(msg.Desc.Name()))
	if parentName != "" {
		return parentName + name
	}
	return name
}

// irMessageNameFromDesc builds the IR name from a MessageDescriptor,
// using PascalCase concatenation for nested types (e.g. OrganizationDepartment).
func irMessageNameFromDesc(md protoreflect.MessageDescriptor) string {
	parent, ok := md.Parent().(protoreflect.MessageDescriptor)
	if !ok {
		return toPascalCase(string(md.Name()))
	}
	return irMessageNameFromDesc(parent) + toPascalCase(string(md.Name()))
}

// cleanComment trims whitespace from a proto comment string and sanitises
// sequences that could break block-comment syntax (e.g. Kotlin's /** */).
func cleanComment(s string) string {
	s = strings.TrimSpace(s)
	// Prevent a proto comment containing "*/" from prematurely closing
	// a generated block comment (SEC-1).
	s = strings.ReplaceAll(s, "*/", "* /")
	return s
}

// irEnumNameFromDesc builds the IR name from an EnumDescriptor,
// using PascalCase concatenation for nested types.
func irEnumNameFromDesc(ed protoreflect.EnumDescriptor) string {
	parent, ok := ed.Parent().(protoreflect.MessageDescriptor)
	if !ok {
		return toPascalCase(string(ed.Name()))
	}
	return irMessageNameFromDesc(parent) + toPascalCase(string(ed.Name()))
}
