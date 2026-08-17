package generator

import (
	"google.golang.org/protobuf/reflect/protoreflect"
)

// tsZodType returns the Zod schema expression for a DomainField's base type.
// e.g. "z.string()", "z.number().int()", "z.boolean()"
func tsZodType(f *DomainField, opts *Options) string {
	// Check WKTs first
	if t, ok := tsWKTZodType(f.Kind, opts); ok {
		return t
	}
	// Scalars
	if f.Kind == FieldKindScalar {
		return tsScalarZodType(f.ScalarKind, opts)
	}
	// Enum
	if f.Kind == FieldKindEnum {
		return f.EnumTypeName + "Schema"
	}
	// Message
	if f.Kind == FieldKindMessage {
		return f.MessageTypeName + "Schema"
	}
	return "z.unknown()"
}

// tsScalarZodType returns the Zod type for a scalar kind.
func tsScalarZodType(k protoreflect.Kind, opts *Options) string {
	switch k {
	case protoreflect.BoolKind:
		return "z.boolean()"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "z.number().int()"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "z.number().int().nonnegative()"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		if opts.TSInt64Style == "bigint" {
			return "z.union([z.string(), z.number(), z.bigint()]).pipe(z.coerce.bigint())"
		}
		return "z.string()"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		if opts.TSInt64Style == "bigint" {
			return "z.union([z.string(), z.number(), z.bigint()]).pipe(z.coerce.bigint()).nonnegative()"
		}
		return "z.string()"
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return "z.number()"
	case protoreflect.StringKind, protoreflect.BytesKind:
		// bytes base64 encoded as string
		return "z.string()"
	default:
		return "z.unknown()"
	}
}

// tsWKTZodType returns the Zod type for a well-known type.
func tsWKTZodType(k FieldKind, opts *Options) (string, bool) {
	switch k {
	case FieldKindTimestamp:
		return "z.string().datetime({ offset: true })", true
	case FieldKindDuration:
		return "z.string()", true
	case FieldKindWrapperBool:
		return "z.boolean().nullable()", true
	case FieldKindWrapperInt32:
		return "z.number().int().nullable()", true
	case FieldKindWrapperUInt32:
		return "z.number().int().nonnegative().nullable()", true
	case FieldKindWrapperInt64:
		if opts.TSInt64Style == "bigint" {
			return "z.union([z.string(), z.number(), z.bigint()]).pipe(z.coerce.bigint()).nullable()", true
		}
		return "z.string().nullable()", true
	case FieldKindWrapperUInt64:
		if opts.TSInt64Style == "bigint" {
			return "z.union([z.string(), z.number(), z.bigint()]).pipe(z.coerce.bigint()).nonnegative().nullable()", true
		}
		return "z.string().nullable()", true
	case FieldKindWrapperFloat, FieldKindWrapperDouble:
		return "z.number().nullable()", true
	case FieldKindWrapperString, FieldKindWrapperBytes:
		return "z.string().nullable()", true
	case FieldKindStruct:
		return "z.record(z.string().refine(k => k !== '__proto__'), z.unknown())", true
	case FieldKindValue:
		return "z.unknown()", true
	case FieldKindListValue:
		return "z.array(z.unknown())", true
	case FieldKindFieldMask:
		return "z.string()", true
	case FieldKindEmpty:
		return "z.record(z.string(), z.never())", true
	case FieldKindAny:
		return "z.unknown()", true
	}
	return "", false
}

// tsMapValueZodType returns the Zod type for a map value using MapTypeInfo.
func tsMapValueZodType(info *MapTypeInfo, opts *Options) string {
	if info == nil {
		return "z.unknown()"
	}
	// WKT types.
	if t, ok := tsWKTZodType(info.Kind, opts); ok {
		return t
	}
	switch info.Kind {
	case FieldKindScalar:
		return tsScalarZodType(info.ScalarKind, opts)
	case FieldKindEnum:
		return info.EnumTypeName + "Schema"
	case FieldKindMessage:
		return info.MessageTypeName + "Schema"
	default:
		return "z.unknown()"
	}
}

// tsOutputFilename determines the output .ts filename safely.
func tsOutputFilename(protoPath string, opts *Options) string {
	if opts.OutputFile != "" {
		return outputFilename(opts.OutputFile, "")
	}
	return outputFilename(protoPath, ".type.ts")
}
