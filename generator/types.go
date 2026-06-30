package generator

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// goType returns the Go type string for a proto field kind.
func goType(kind protoreflect.Kind) string {
	switch kind {
	case protoreflect.BoolKind:
		return "bool"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "int32"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return "int64"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "uint32"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "uint64"
	case protoreflect.FloatKind:
		return "float32"
	case protoreflect.DoubleKind:
		return "float64"
	case protoreflect.StringKind:
		return "string"
	case protoreflect.BytesKind:
		return "[]byte"
	default:
		return "any"
	}
}

// goZeroValue returns the Go zero value for a proto field kind.
func goZeroValue(kind protoreflect.Kind) string {
	switch kind {
	case protoreflect.BoolKind:
		return "false"
	case protoreflect.StringKind:
		return `""`
	case protoreflect.BytesKind:
		return "nil"
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return "0"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "0"
	default:
		return "nil"
	}
}

// isWellKnownTimestamp returns true if the message is google.protobuf.Timestamp.
func isWellKnownTimestamp(field *protogen.Field) bool {
	if field.Desc.Kind() != protoreflect.MessageKind {
		return false
	}
	return string(field.Desc.Message().FullName()) == "google.protobuf.Timestamp"
}

// isWellKnownDuration returns true if the message is google.protobuf.Duration.
func isWellKnownDuration(field *protogen.Field) bool {
	if field.Desc.Kind() != protoreflect.MessageKind {
		return false
	}
	return string(field.Desc.Message().FullName()) == "google.protobuf.Duration"
}

// isWellKnownWrapper returns true if the field is a google.protobuf wrapper type
// (e.g., StringValue, BoolValue, Int32Value, etc.).
func isWellKnownWrapper(field *protogen.Field) bool {
	if field.Desc.Kind() != protoreflect.MessageKind {
		return false
	}
	switch string(field.Desc.Message().FullName()) {
	case "google.protobuf.StringValue",
		"google.protobuf.BoolValue",
		"google.protobuf.Int32Value",
		"google.protobuf.Int64Value",
		"google.protobuf.UInt32Value",
		"google.protobuf.UInt64Value",
		"google.protobuf.FloatValue",
		"google.protobuf.DoubleValue",
		"google.protobuf.BytesValue":
		return true
	}
	return false
}

// wrapperPbFuncName returns the wrapperspb constructor function name for a wrapper type.
// e.g., "google.protobuf.StringValue" -> "String"
func wrapperPbFuncName(field *protogen.Field) string {
	switch string(field.Desc.Message().FullName()) {
	case "google.protobuf.StringValue":
		return "String"
	case "google.protobuf.BoolValue":
		return "Bool"
	case "google.protobuf.Int32Value":
		return "Int32"
	case "google.protobuf.Int64Value":
		return "Int64"
	case "google.protobuf.UInt32Value":
		return "UInt32"
	case "google.protobuf.UInt64Value":
		return "UInt64"
	case "google.protobuf.FloatValue":
		return "Float"
	case "google.protobuf.DoubleValue":
		return "Double"
	case "google.protobuf.BytesValue":
		return "Bytes"
	default:
		return "String"
	}
}

// isNestedMessage returns true if the field is a non-WKT message type that is not a list or map.
func isNestedMessage(field *protogen.Field) bool {
	if field.Desc.Kind() != protoreflect.MessageKind {
		return false
	}
	if field.Desc.IsList() || field.Desc.IsMap() {
		return false
	}
	if isWellKnownTimestamp(field) || isWellKnownDuration(field) || isWellKnownWrapper(field) {
		return false
	}
	if isWellKnownStruct(field) || isWellKnownValue(field) || isWellKnownListValue(field) ||
		isWellKnownFieldMask(field) || isWellKnownEmpty(field) || isWellKnownAny(field) {
		return false
	}
	return true
}

// isWellKnownStruct returns true if the field is google.protobuf.Struct.
func isWellKnownStruct(field *protogen.Field) bool {
	return field.Desc.Kind() == protoreflect.MessageKind &&
		string(field.Desc.Message().FullName()) == "google.protobuf.Struct"
}

// isWellKnownValue returns true if the field is google.protobuf.Value.
func isWellKnownValue(field *protogen.Field) bool {
	return field.Desc.Kind() == protoreflect.MessageKind &&
		string(field.Desc.Message().FullName()) == "google.protobuf.Value"
}

// isWellKnownListValue returns true if the field is google.protobuf.ListValue.
func isWellKnownListValue(field *protogen.Field) bool {
	return field.Desc.Kind() == protoreflect.MessageKind &&
		string(field.Desc.Message().FullName()) == "google.protobuf.ListValue"
}

// isWellKnownFieldMask returns true if the field is google.protobuf.FieldMask.
func isWellKnownFieldMask(field *protogen.Field) bool {
	return field.Desc.Kind() == protoreflect.MessageKind &&
		string(field.Desc.Message().FullName()) == "google.protobuf.FieldMask"
}

// isWellKnownEmpty returns true if the field is google.protobuf.Empty.
func isWellKnownEmpty(field *protogen.Field) bool {
	return field.Desc.Kind() == protoreflect.MessageKind &&
		string(field.Desc.Message().FullName()) == "google.protobuf.Empty"
}

// isWellKnownAny returns true if the field is google.protobuf.Any.
func isWellKnownAny(field *protogen.Field) bool {
	return field.Desc.Kind() == protoreflect.MessageKind &&
		string(field.Desc.Message().FullName()) == "google.protobuf.Any"
}
