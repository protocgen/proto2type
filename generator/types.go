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
		return "interface{}"
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
