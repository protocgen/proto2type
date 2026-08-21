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
		if f.EnumFullName == "google.protobuf.NullValue" {
			return "z.null()"
		}
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
			return `z.union([z.string().max(100).regex(/^-?\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" }), z.bigint()]).pipe(z.coerce.bigint()).refine(v => v >= -9223372036854775808n && v <= 9223372036854775807n, { message: "int64 out of range" })`
		}
		return `z.union([z.string().max(100).regex(/^-?\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" })]).pipe(z.coerce.string())`
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		if opts.TSInt64Style == "bigint" {
			return `z.union([z.string().max(100).regex(/^-?\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" }), z.bigint()]).pipe(z.coerce.bigint()).refine(v => v >= 0n && v <= 18446744073709551615n, { message: "uint64 out of range" })`
		}
		return `z.union([z.string().max(100).regex(/^\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" }).refine(n => n >= 0, { message: "must be non-negative" })]).pipe(z.coerce.string())`
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return `z.union([z.number(), z.enum(["NaN", "Infinity", "-Infinity"])])`
	case protoreflect.StringKind:
		return "z.string()"
	case protoreflect.BytesKind:
		// ProtoJSON accepts standard base64 (+/) and base64url (-_), with or without padding.
		return `z.string().regex(/^[A-Za-z0-9+\/\-_]*={0,2}$/, { message: "must be valid base64" })`
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
		return `z.string().regex(new RegExp("^-?[0-9]+(\\.[0-9]+)?s$"), { message: "must be a valid Duration (e.g. '1.5s')" })`, true
	case FieldKindWrapperBool:
		return "z.boolean().nullable()", true
	case FieldKindWrapperInt32:
		return "z.number().int().nullable()", true
	case FieldKindWrapperUInt32:
		return "z.number().int().nonnegative().nullable()", true
	case FieldKindWrapperInt64:
		if opts.TSInt64Style == "bigint" {
			return `z.union([z.string().max(100).regex(/^-?\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" }), z.bigint()]).pipe(z.coerce.bigint()).refine(v => v >= -9223372036854775808n && v <= 9223372036854775807n, { message: "int64 out of range" }).nullable()`, true
		}
		return `z.union([z.string().max(100).regex(/^-?\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" })]).pipe(z.coerce.string()).nullable()`, true
	case FieldKindWrapperUInt64:
		if opts.TSInt64Style == "bigint" {
			return `z.union([z.string().max(100).regex(/^-?\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" }), z.bigint()]).pipe(z.coerce.bigint()).refine(v => v >= 0n && v <= 18446744073709551615n, { message: "uint64 out of range" }).nullable()`, true
		}
		return `z.union([z.string().max(100).regex(/^\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" }).refine(n => n >= 0, { message: "must be non-negative" })]).pipe(z.coerce.string()).nullable()`, true
	case FieldKindWrapperFloat, FieldKindWrapperDouble:
		return `z.union([z.number(), z.enum(["NaN", "Infinity", "-Infinity"])]).nullable()`, true
	case FieldKindWrapperString:
		return "z.string().nullable()", true
	case FieldKindWrapperBytes:
		return `z.string().regex(/^[A-Za-z0-9+\/\-_]*={0,2}$/, { message: "must be valid base64" }).nullable()`, true
	case FieldKindStruct:
		return "z.record(z.string().refine(k => k !== '__proto__' && k !== 'constructor' && k !== 'prototype', { message: 'reserved key name' }), z.unknown())", true
	case FieldKindValue:
		return "z.unknown()", true
	case FieldKindListValue:
		return "z.array(z.unknown())", true
	case FieldKindFieldMask:
		return `z.string().regex(new RegExp("^[a-zA-Z_][a-zA-Z0-9_]*(\\.[a-zA-Z_][a-zA-Z0-9_]*)*(,[a-zA-Z_][a-zA-Z0-9_]*(\\.[a-zA-Z_][a-zA-Z0-9_]*)*)*$"), { message: "must be a valid FieldMask (comma-separated field paths)" })`, true
	case FieldKindEmpty:
		return "z.record(z.string(), z.never())", true
	case FieldKindAny:
		return `z.object({ "@type": z.string() }).passthrough()`, true
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
		if info.EnumFullName == "google.protobuf.NullValue" {
			return "z.null()"
		}
		return info.EnumTypeName + "Schema"
	case FieldKindMessage:
		return info.MessageTypeName + "Schema"
	default:
		return "z.unknown()"
	}
}

// tsOutputFilename determines the output .ts filename safely.
func tsOutputFilename(protoPath string, opts *Options) (string, error) {
	if opts.OutputFile != "" {
		return outputFilename(opts.OutputFile, "")
	}
	return outputFilename(protoPath, ".type.ts")
}
