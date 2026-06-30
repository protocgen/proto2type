package generator

import (
	"fmt"
	"strings"

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

// rustMessageName returns the Rust struct name for a message.
// For top-level messages, it returns the PascalCase name.
// For nested messages, it prefixes with parent names to avoid collisions.
func rustMessageName(msg *protogen.Message) string {
	parent, ok := msg.Desc.Parent().(protoreflect.MessageDescriptor)
	if !ok {
		// Top-level message (parent is FileDescriptor)
		return toPascalCase(string(msg.Desc.Name()))
	}
	// Nested message: prefix with all ancestor names, underscore-separated
	return qualifiedMessageName(parent) + "_" + toPascalCase(string(msg.Desc.Name()))
}

// qualifiedMessageName builds the full qualified Rust name from a MessageDescriptor chain.
func qualifiedMessageName(md protoreflect.MessageDescriptor) string {
	parent, ok := md.Parent().(protoreflect.MessageDescriptor)
	if !ok {
		return toPascalCase(string(md.Name()))
	}
	return qualifiedMessageName(parent) + "_" + toPascalCase(string(md.Name()))
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
			valFullName := string(field.Desc.MapValue().Message().FullName())
			switch valFullName {
			case "google.protobuf.Timestamp":
				valType = "DateTime<Utc>"
			case "google.protobuf.Duration":
				valType = "i64"
			case "google.protobuf.Struct":
				valType = "serde_json::Map<String, serde_json::Value>"
			case "google.protobuf.Value":
				valType = "serde_json::Value"
			case "google.protobuf.ListValue":
				valType = "Vec<serde_json::Value>"
			case "google.protobuf.FieldMask":
				valType = "Vec<String>"
			case "google.protobuf.Empty":
				valType = "()"
			default:
				// Check wrapper types
				switch valFullName {
				case "google.protobuf.StringValue":
					valType = "Option<String>"
				case "google.protobuf.BoolValue":
					valType = "Option<bool>"
				case "google.protobuf.Int32Value":
					valType = "Option<i32>"
				case "google.protobuf.Int64Value":
					valType = "Option<i64>"
				case "google.protobuf.UInt32Value":
					valType = "Option<u32>"
				case "google.protobuf.UInt64Value":
					valType = "Option<u64>"
				case "google.protobuf.FloatValue":
					valType = "Option<f32>"
				case "google.protobuf.DoubleValue":
					valType = "Option<f64>"
				case "google.protobuf.BytesValue":
					valType = "Option<Vec<u8>>"
				default:
					valType = qualifiedMessageName(field.Desc.MapValue().Message())
				}
			}
		} else if field.Desc.MapValue().Kind() == protoreflect.EnumKind {
			if isEnumAsString(field, opts) {
				valType = "String"
			} else {
				valType = rustEnumTypeName(field)
			}
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
		return "i64" // Duration in milliseconds
	}

	// Well-known wrapper types (e.g. google.protobuf.StringValue -> Option<String>)
	if isWellKnownWrapper(field) {
		return rustWrapperType(field)
	}

	// Well-known JSON types
	if isWellKnownStruct(field) {
		return "serde_json::Map<String, serde_json::Value>"
	}
	if isWellKnownValue(field) {
		return "serde_json::Value"
	}
	if isWellKnownListValue(field) {
		return "Vec<serde_json::Value>"
	}
	if isWellKnownFieldMask(field) {
		return "Vec<String>"
	}
	if isWellKnownEmpty(field) {
		return "()"
	}
	if isWellKnownAny(field) {
		// TODO: google.protobuf.Any is too complex for auto-mapping; skipping.
		return "serde_json::Value"
	}

	// Message types (nested) -> Option<Box<T>>
	// TODO(P1-11): Box<T> is used for all nested messages but is only necessary
	// for recursive types. Implementing recursion detection is deferred.
	if field.Desc.Kind() == protoreflect.MessageKind {
		return "Option<Box<" + rustMessageName(field.Message) + ">>"
	}

	// Enum types
	if field.Desc.Kind() == protoreflect.EnumKind {
		if isEnumAsString(field, opts) {
			return "String"
		}
		return rustEnumTypeName(field)
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
		return "DateTime<Utc>"
	}
	if isWellKnownDuration(field) {
		return "i64" // Duration in milliseconds
	}
	if isWellKnownWrapper(field) {
		return rustWrapperType(field)
	}
	// Well-known JSON types
	if isWellKnownStruct(field) {
		return "serde_json::Map<String, serde_json::Value>"
	}
	if isWellKnownValue(field) {
		return "serde_json::Value"
	}
	if isWellKnownListValue(field) {
		return "Vec<serde_json::Value>"
	}
	if isWellKnownFieldMask(field) {
		return "Vec<String>"
	}
	if isWellKnownEmpty(field) {
		return "()"
	}
	if isWellKnownAny(field) {
		return "serde_json::Value"
	}
	// Message types: just the struct name, no Option<Box<>>
	if field.Desc.Kind() == protoreflect.MessageKind {
		return rustMessageName(field.Message)
	}
	// Enum types
	if field.Desc.Kind() == protoreflect.EnumKind {
		if isEnumAsString(field, opts) {
			return "String"
		}
		return rustEnumTypeName(field)
	}
	return rustType(field.Desc.Kind())
}

// rustEnumTypeName returns the Rust enum type name for an enum field.
func rustEnumTypeName(field *protogen.Field) string {
	return toPascalCase(string(field.Desc.Enum().Name()))
}

// stripEnumPrefix strips the common enum name prefix from a proto enum value name
// and converts to PascalCase. For example, USER_STATUS_ACTIVE with enum name
// UserStatus strips USER_STATUS_ prefix, giving "ACTIVE" → "Active".
func stripEnumPrefix(enumName, valueName string) string {
	// Convert PascalCase enum name to UPPER_SNAKE prefix
	// e.g., "UserStatus" → "USER_STATUS_"
	var b strings.Builder
	for i, r := range enumName {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteRune('_')
		}
		b.WriteRune(r)
	}
	prefix := strings.ToUpper(b.String()) + "_"

	stripped := valueName
	if strings.HasPrefix(valueName, prefix) {
		stripped = strings.TrimPrefix(valueName, prefix)
	}
	if stripped == "" {
		stripped = valueName
	}
	// Convert UPPER_SNAKE to PascalCase
	return toPascalCase(strings.ToLower(stripped))
}

// hasOneofFields returns true if the message has real (non-synthetic) oneof groups.
func hasOneofFields(msg *protogen.Message) bool {
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			return true
		}
	}
	return false
}

// scanRustImports scans a message (and its nested messages) for types that require imports.
func scanRustImports(msg *protogen.Message, needsChrono, needsHashMap, needsSerdeJSON *bool) {
	if isMessageSkipped(msg) {
		return
	}
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}
		if isFieldSkipped(field) {
			continue
		}
		if isWellKnownTimestamp(field) {
			*needsChrono = true
		}
		if field.Desc.IsMap() {
			*needsHashMap = true
		}
		if isWellKnownStruct(field) || isWellKnownValue(field) || isWellKnownListValue(field) || isWellKnownAny(field) {
			*needsSerdeJSON = true
		}
	}
	for _, nested := range msg.Messages {
		if nested.Desc.IsMapEntry() {
			continue
		}
		scanRustImports(nested, needsChrono, needsHashMap, needsSerdeJSON)
	}
}

// rustOneofVariantType returns the Rust type for a oneof variant field.
func rustOneofVariantType(field *protogen.Field, opts *Options) string {
	if isWellKnownTimestamp(field) {
		return "DateTime<Utc>"
	}
	if isWellKnownDuration(field) {
		return "i64"
	}
	if isWellKnownWrapper(field) {
		return rustWrapperType(field)
	}
	if isWellKnownStruct(field) {
		return "serde_json::Map<String, serde_json::Value>"
	}
	if isWellKnownValue(field) {
		return "serde_json::Value"
	}
	if isWellKnownListValue(field) {
		return "Vec<serde_json::Value>"
	}
	if isWellKnownFieldMask(field) {
		return "Vec<String>"
	}
	if isWellKnownEmpty(field) {
		return "()"
	}
	if field.Desc.Kind() == protoreflect.MessageKind {
		return "Box<" + rustMessageName(field.Message) + ">"
	}
	if field.Desc.Kind() == protoreflect.EnumKind {
		if isEnumAsString(field, opts) {
			return "String"
		}
		return rustEnumTypeName(field)
	}
	return rustType(field.Desc.Kind())
}

