package generator

import "google.golang.org/protobuf/reflect/protoreflect"

// kotlinScalarType returns the Kotlin type for a proto scalar kind.
func kotlinScalarType(kind protoreflect.Kind) string {
	switch kind {
	case protoreflect.BoolKind:
		return "Boolean"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "Int"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return "Long"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "Int"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "Long"
	case protoreflect.FloatKind:
		return "Float"
	case protoreflect.DoubleKind:
		return "Double"
	case protoreflect.StringKind:
		return "String"
	case protoreflect.BytesKind:
		return "ByteArray"
	default:
		return "Any?"
	}
}

// kotlinScalarDefault returns the Kotlin default value for a proto scalar kind.
func kotlinScalarDefault(kind protoreflect.Kind) string {
	switch kind {
	case protoreflect.BoolKind:
		return "false"
	case protoreflect.StringKind:
		return `""`
	case protoreflect.FloatKind:
		return "0.0f"
	case protoreflect.DoubleKind:
		return "0.0"
	case protoreflect.BytesKind:
		return "byteArrayOf()"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "0"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "0L"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return "0L"
	default:
		return "0"
	}
}

// kotlinJSONKindType returns the Kotlin type for a JSON/WKT field kind.
// Returns (type, true) for JSON kinds, ("", false) for non-JSON kinds.
// This is the single source of truth for Struct/Value/ListValue/Empty/Any/FieldMask
// type mapping, preventing drift across the four call sites.
func kotlinJSONKindType(kind FieldKind) (string, bool) {
	switch kind {
	case FieldKindStruct:
		return "Map<String, JsonElement>", true
	case FieldKindValue:
		return "JsonElement", true
	case FieldKindListValue:
		return "JsonArray", true
	case FieldKindFieldMask:
		return "List<String>", true
	case FieldKindEmpty:
		return "JsonObject", true
	case FieldKindAny:
		return "JsonElement", true
	default:
		return "", false
	}
}

// kotlinMapTypeInfoType returns the Kotlin type for a MapTypeInfo.
func kotlinMapTypeInfoType(m *MapTypeInfo) string {
	if m == nil {
		return "Any?"
	}
	if t, ok := kotlinJSONKindType(m.Kind); ok {
		return t
	}
	switch m.Kind {
	case FieldKindScalar:
		return kotlinScalarType(m.ScalarKind)
	case FieldKindEnum:
		return m.EnumTypeName
	case FieldKindMessage:
		return m.MessageTypeName + "?"
	case FieldKindTimestamp:
		return "Instant"
	case FieldKindDuration:
		return "Duration"
	}
	if m.Kind.IsWrapper() {
		return kotlinWrapperType(m.Kind)
	}
	return "Any?"
}

// kotlinFieldType returns the Kotlin type string for a DomainField.
func kotlinFieldType(f DomainField) string {
	// Map fields.
	if f.IsMap {
		keyType := kotlinMapTypeInfoType(f.MapKey)
		valType := kotlinMapTypeInfoType(f.MapValue)
		return "Map<" + keyType + ", " + valType + ">"
	}

	// Repeated fields.
	if f.Repeated {
		return "List<" + kotlinElementType(f) + ">"
	}

	return kotlinSingularType(f)
}

// kotlinElementType returns the Kotlin element type for a repeated field.
// Unlike kotlinSingularType, messages are NOT nullable inside lists.
func kotlinElementType(f DomainField) string {
	if t, ok := kotlinJSONKindType(f.Kind); ok {
		return t
	}
	switch f.Kind {
	case FieldKindTimestamp:
		return "Instant"
	case FieldKindDuration:
		return "Duration"
	case FieldKindMessage:
		return f.MessageTypeName
	case FieldKindEnum:
		if f.EnumAsString {
			return "String"
		}
		return f.EnumTypeName
	}
	if f.Kind.IsWrapper() {
		return kotlinWrapperType(f.Kind)
	}
	return kotlinScalarType(f.ScalarKind)
}

// kotlinSingularType returns the Kotlin type for a non-repeated, non-map field.
func kotlinSingularType(f DomainField) string {
	if t, ok := kotlinJSONKindType(f.Kind); ok {
		return t
	}
	switch f.Kind {
	case FieldKindTimestamp:
		if f.Optional {
			return "Instant?"
		}
		return "Instant"
	case FieldKindDuration:
		if f.Optional {
			return "Duration?"
		}
		return "Duration"
	case FieldKindMessage:
		return f.MessageTypeName + "?"
	case FieldKindEnum:
		if f.EnumAsString {
			if f.Optional {
				return "String?"
			}
			return "String"
		}
		if f.Optional {
			return f.EnumTypeName + "?"
		}
		return f.EnumTypeName
	}
	if f.Kind.IsWrapper() {
		return kotlinWrapperType(f.Kind)
	}

	// Optional scalars.
	if f.Optional {
		return kotlinScalarType(f.ScalarKind) + "?"
	}

	return kotlinScalarType(f.ScalarKind)
}

// kotlinWrapperType returns the nullable Kotlin type for a well-known wrapper kind.
func kotlinWrapperType(kind FieldKind) string {
	switch kind {
	case FieldKindWrapperBool:
		return "Boolean?"
	case FieldKindWrapperInt32:
		return "Int?"
	case FieldKindWrapperInt64:
		return "Long?"
	case FieldKindWrapperUInt32:
		return "Int?"
	case FieldKindWrapperUInt64:
		return "Long?"
	case FieldKindWrapperFloat:
		return "Float?"
	case FieldKindWrapperDouble:
		return "Double?"
	case FieldKindWrapperString:
		return "String?"
	case FieldKindWrapperBytes:
		return "ByteArray?"
	default:
		return "Any?"
	}
}

// kotlinOneofVariantType returns the Kotlin type for a oneof variant.
func kotlinOneofVariantType(v *OneofVariant) string {
	if t, ok := kotlinJSONKindType(v.Kind); ok {
		return t
	}
	switch v.Kind {
	case FieldKindTimestamp:
		return "Instant"
	case FieldKindDuration:
		return "Duration"
	case FieldKindMessage:
		return v.TypeName
	case FieldKindEnum:
		if v.EnumAsString {
			return "String"
		}
		return v.TypeName
	case FieldKindScalar:
		return kotlinScalarType(v.ScalarKind)
	}
	if v.Kind.IsWrapper() {
		return kotlinWrapperType(v.Kind)
	}
	return "Any?"
}

// kotlinDefaultValue returns the Kotlin default value expression for a DomainField.
func kotlinDefaultValue(f DomainField) string {
	// Map fields.
	if f.IsMap {
		return "emptyMap()"
	}

	// Repeated fields.
	if f.Repeated {
		return "emptyList()"
	}

	switch f.Kind {
	case FieldKindTimestamp:
		if f.Optional {
			return "null"
		}
		return "Instant.fromEpochSeconds(0)"
	case FieldKindDuration:
		if f.Optional {
			return "null"
		}
		return "Duration.ZERO"
	case FieldKindMessage:
		return "null"
	case FieldKindEnum:
		if f.Optional {
			return "null"
		}
		if f.EnumAsString {
			if f.EnumDefaultName != "" {
				return "\"" + f.EnumDefaultName + "\""
			}
			return "\"\""
		}
		return f.EnumTypeName + ".entries.first()"
	case FieldKindStruct:
		return "emptyMap()"
	case FieldKindValue, FieldKindAny:
		return "JsonNull"
	case FieldKindListValue:
		return "JsonArray(emptyList())"
	case FieldKindFieldMask:
		return "emptyList()"
	case FieldKindEmpty:
		return "JsonObject(emptyMap())"
	}

	// Wrappers are nullable.
	if f.Kind.IsWrapper() {
		return "null"
	}

	// Optional scalars.
	if f.Optional {
		return "null"
	}

	return kotlinScalarDefault(f.ScalarKind)
}

// kotlinOneofFieldType returns the type for a oneof field in a message
// (the sealed class name, nullable).
func kotlinOneofFieldType(oneofName string) string {
	return oneofName + "?"
}
