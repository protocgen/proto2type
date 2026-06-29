package generator

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// rustType returns the Rust type string for a proto field kind.
func rustType(kind protoreflect.Kind) string {
	switch kind {
	case protoreflect.BoolKind:
		return "bool"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "i32"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return "i64"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "u32"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "u64"
	case protoreflect.FloatKind:
		return "f32"
	case protoreflect.DoubleKind:
		return "f64"
	case protoreflect.StringKind:
		return "String"
	case protoreflect.BytesKind:
		return "Vec<u8>"
	default:
		return "serde_json::Value"
	}
}

// rustZeroValue returns the Rust default/zero value for a proto field kind.
func rustZeroValue(kind protoreflect.Kind) string {
	switch kind {
	case protoreflect.BoolKind:
		return "false"
	case protoreflect.StringKind:
		return "String::new()"
	case protoreflect.BytesKind:
		return "Vec::new()"
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return "0.0"
	default:
		return "0"
	}
}

// rustWrapperType returns the Rust Option type for a well-known wrapper field.
func rustWrapperType(field *protogen.Field) string {
	switch string(field.Desc.Message().FullName()) {
	case "google.protobuf.StringValue":
		return "Option<String>"
	case "google.protobuf.BoolValue":
		return "Option<bool>"
	case "google.protobuf.Int32Value":
		return "Option<i32>"
	case "google.protobuf.Int64Value":
		return "Option<i64>"
	case "google.protobuf.UInt32Value":
		return "Option<u32>"
	case "google.protobuf.UInt64Value":
		return "Option<u64>"
	case "google.protobuf.FloatValue":
		return "Option<f32>"
	case "google.protobuf.DoubleValue":
		return "Option<f64>"
	case "google.protobuf.BytesValue":
		return "Option<Vec<u8>>"
	default:
		return "Option<serde_json::Value>"
	}
}

// rustDomainFieldType returns the Rust type for a proto field in a domain struct.
func rustDomainFieldType(field *protogen.Field, opts *Options) string {
	// Handle repeated fields
	if field.Desc.IsList() {
		return "Vec<" + rustDomainListElementType(field, opts) + ">"
	}

	// Handle map fields
	if field.Desc.IsMap() {
		keyType := rustType(field.Desc.MapKey().Kind())
		valType := rustType(field.Desc.MapValue().Kind())
		if field.Desc.MapValue().Kind() == protoreflect.MessageKind {
			valType = toPascalCase(string(field.Desc.MapValue().Message().Name()))
		}
		return fmt.Sprintf("HashMap<%s, %s>", keyType, valType)
	}

	return rustDomainSingularType(field, opts)
}

// rustDomainSingularType returns the Rust type for a singular (non-repeated, non-map) field.
func rustDomainSingularType(field *protogen.Field, opts *Options) string {
	// Well-known types
	if isWellKnownTimestamp(field) {
		return "DateTime<Utc>"
	}
	if isWellKnownDuration(field) {
		return "chrono::Duration"
	}

	// Well-known wrapper types (e.g. google.protobuf.StringValue -> Option<String>)
	if isWellKnownWrapper(field) {
		return rustWrapperType(field)
	}

	// Message types (nested) -> Option<Box<T>>
	if field.Desc.Kind() == protoreflect.MessageKind {
		return "Option<Box<" + toPascalCase(string(field.Desc.Message().Name())) + ">>"
	}

	// Enum types
	if field.Desc.Kind() == protoreflect.EnumKind {
		if isEnumAsString(field, opts) {
			return "String"
		}
		return "i32"
	}

	// proto3 optional scalars -> Option<T>
	if field.Desc.HasOptionalKeyword() {
		return "Option<" + rustType(field.Desc.Kind()) + ">"
	}

	// Scalar types
	return rustType(field.Desc.Kind())
}

// rustDomainListElementType returns the Rust element type for repeated fields.
// Unlike rustDomainSingularType, messages are NOT wrapped in Option<Box<>> —
// they are stored directly in the Vec.
func rustDomainListElementType(field *protogen.Field, opts *Options) string {
	// WKTs
	if isWellKnownTimestamp(field) {
		return "chrono::DateTime<chrono::Utc>"
	}
	if isWellKnownDuration(field) {
		return "chrono::Duration"
	}
	if isWellKnownWrapper(field) {
		return rustWrapperType(field)
	}
	// Message types: just the struct name, no Option<Box<>>
	if field.Desc.Kind() == protoreflect.MessageKind {
		return toPascalCase(string(field.Desc.Message().Name()))
	}
	// Enum types
	if field.Desc.Kind() == protoreflect.EnumKind {
		if isEnumAsString(field, opts) {
			return "String"
		}
		return "i32"
	}
	return rustType(field.Desc.Kind())
}
