package generator

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// pythonScalarType maps proto scalar kinds to Python types.
func pythonScalarType(kind protoreflect.Kind) string {
	switch kind {
	case protoreflect.BoolKind:
		return "bool"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "int"
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return "float"
	case protoreflect.StringKind:
		return "str"
	case protoreflect.BytesKind:
		return "bytes"
	default:
		return "Any"
	}
}

// pythonScalarDefault returns the Python default value for a scalar kind.
func pythonScalarDefault(kind protoreflect.Kind) string {
	switch kind {
	case protoreflect.BoolKind:
		return "False"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "0"
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return "0.0"
	case protoreflect.StringKind:
		return "''"
	case protoreflect.BytesKind:
		return "b''"
	default:
		return "None"
	}
}

// pythonFieldType returns the full Python type annotation for a field,
// including list/dict wrapping and optionality.
func pythonFieldType(f *DomainField, opts *Options) string {
	if f.IsMap {
		keyType := pythonMapKeyType(f.MapKey)
		valType := pythonMapValueType(f.MapValue, opts)
		return fmt.Sprintf("dict[%s, %s]", keyType, valType)
	}

	baseType := pythonSingularType(f, opts)

	if f.Repeated {
		return fmt.Sprintf("list[%s]", baseType)
	}

	return baseType
}

// wktPythonType returns the Python type for a well-known type or wrapper kind.
// Returns (type, true) if the kind is a WKT/wrapper, ("", false) otherwise.
// This is the single source of truth for WKT→Python type mapping, used by both
// pythonSingularType and pythonMapValueType.
func wktPythonType(kind FieldKind) (string, bool) {
	switch kind {
	case FieldKindTimestamp:
		return "datetime", true
	case FieldKindDuration:
		return "timedelta", true
	case FieldKindStruct:
		return "dict[str, Any]", true
	case FieldKindValue, FieldKindAny:
		return "Any", true
	case FieldKindListValue:
		return "list[Any]", true
	case FieldKindEmpty:
		return "None", true
	case FieldKindFieldMask:
		return "list[str]", true
	default:
		if kind.IsWrapper() {
			return pythonWrapperType(kind), true
		}
		return "", false
	}
}

// pythonSingularType returns the Python type for a non-repeated, non-map field.
func pythonSingularType(f *DomainField, opts *Options) string {
	// WKT and wrapper types.
	if t, ok := wktPythonType(f.Kind); ok {
		return t
	}
	switch f.Kind {
	case FieldKindMessage:
		return f.MessageTypeName
	case FieldKindEnum:
		return f.EnumTypeName
	case FieldKindScalar:
		return pythonScalarType(f.ScalarKind)
	}
	return "Any"
}

// pythonWrapperType maps WKT wrapper kinds to nullable Python types.
func pythonWrapperType(kind FieldKind) string {
	switch kind {
	case FieldKindWrapperBool:
		return "bool | None"
	case FieldKindWrapperInt32, FieldKindWrapperInt64,
		FieldKindWrapperUInt32, FieldKindWrapperUInt64:
		return "int | None"
	case FieldKindWrapperFloat, FieldKindWrapperDouble:
		return "float | None"
	case FieldKindWrapperString:
		return "str | None"
	case FieldKindWrapperBytes:
		return "bytes | None"
	default:
		return "Any"
	}
}

// pythonMapKeyType returns the Python type for a map key.
func pythonMapKeyType(info *MapTypeInfo) string {
	if info == nil {
		return "str"
	}
	return pythonScalarType(info.ScalarKind)
}

// pythonMapValueType returns the Python type for a map value.
func pythonMapValueType(info *MapTypeInfo, opts *Options) string {
	if info == nil {
		return "Any"
	}
	// WKT and wrapper types.
	if t, ok := wktPythonType(info.Kind); ok {
		return t
	}
	switch info.Kind {
	case FieldKindMessage:
		return info.MessageTypeName
	case FieldKindEnum:
		return info.EnumTypeName
	case FieldKindScalar:
		return pythonScalarType(info.ScalarKind)
	default:
		return "Any"
	}
}

// pythonDefaultValue returns the Python default value expression for a field.
// Returns empty string for required fields (no default).
func pythonDefaultValue(f *DomainField, opts *Options) string {
	// Required fields (field_behavior or buf/validate) have no default.
	if f.IsRequired() || (f.ValidateConstraints != nil && f.ValidateConstraints.Required) {
		return ""
	}

	// Repeated/map fields use Field(default_factory=list/dict), no simple default.
	if f.Repeated || f.IsMap {
		return ""
	}

	// Optional (proto3 keyword), message, list, map fields default to None.
	if f.Optional {
		return "None"
	}
	if f.Kind == FieldKindMessage || f.Kind.IsWrapper() {
		return "None"
	}

	// Timestamps, durations, structs, etc.
	switch f.Kind {
	case FieldKindTimestamp, FieldKindDuration, FieldKindStruct,
		FieldKindValue, FieldKindListValue, FieldKindFieldMask,
		FieldKindEmpty, FieldKindAny:
		return "None"
	}

	// Enums default to the zero value name.
	// In default enum style, _UNSPECIFIED values are skipped from the enum class,
	// so we must not reference them as defaults — use None instead.
	if f.Kind == FieldKindEnum {
		if f.EnumDefaultName != "" {
			if opts.PythonEnumStyle != "raw" && strings.HasSuffix(f.EnumDefaultName, "_UNSPECIFIED") {
				return "None"
			}
			return fmt.Sprintf("%s.%s", f.EnumTypeName, f.EnumDefaultName)
		}
		return "None"
	}

	// Scalars.
	if f.Kind == FieldKindScalar {
		return pythonScalarDefault(f.ScalarKind)
	}

	return "None"
}

// pythonTypeNeedsOptional returns true if the field type should be wrapped with " | None".
func pythonTypeNeedsOptional(f *DomainField) bool {
	if f.Optional {
		return true
	}
	// Repeated/map fields use default_factory, not None.
	if f.Repeated || f.IsMap {
		return false
	}
	if f.Kind == FieldKindMessage || f.Kind.IsWrapper() {
		return true
	}
	switch f.Kind {
	case FieldKindTimestamp, FieldKindDuration, FieldKindStruct,
		FieldKindValue, FieldKindListValue, FieldKindFieldMask,
		FieldKindEmpty, FieldKindAny:
		return true
	}
	return false
}

// pythonOneofUnionType builds the union type string for a oneof group.
// e.g. "str | int | MyMessage | None"
func pythonOneofUnionType(oneof *DomainOneof, opts *Options) string {
	seen := make(map[string]bool)
	var types []string
	for _, v := range oneof.Variants {
		var t string
		switch v.Kind {
		case FieldKindScalar:
			t = pythonScalarType(v.ScalarKind)
		case FieldKindMessage:
			t = v.TypeName
		case FieldKindEnum:
			t = v.TypeName
		case FieldKindTimestamp:
			t = "datetime"
		case FieldKindDuration:
			t = "timedelta"
		case FieldKindStruct:
			t = "dict[str, Any]"
		case FieldKindValue:
			t = "Any"
		default:
			t = "Any"
		}
		if !seen[t] {
			seen[t] = true
			types = append(types, t)
		}
	}
	types = append(types, "None")
	return strings.Join(types, " | ")
}

// pythonOutputFilename determines the output .py filename.
func pythonOutputFilename(protoPath string, opts *Options) string {
	if opts.OutputFile != "" {
		return opts.OutputFile
	}
	// Strip .proto extension.
	base := strings.TrimSuffix(protoPath, ".proto")
	// Get just the filename, not the full path.
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	if opts.PythonStripProtoSuffix {
		return base + ".py"
	}
	return base + "_pb2_pydantic.py"
}
