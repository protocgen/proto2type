package generator

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// writeTSExplicitInterface emits a plain TypeScript interface alongside the Zod schema.
// This can improve IDE performance compared to z.infer in large codebases.
func writeTSExplicitInterface(g *protogen.GeneratedFile, m *DomainMessage, opts *Options) {
	g.P("export interface ", m.Name, " {")
	for _, f := range m.Fields {
		if f.FieldSkip || f.IsOneof {
			continue
		}
		if f.Comment != "" {
			g.P("  /** ", sanitizeJSDoc(f.Comment), " */")
		}
		tsType := tsPlainType(f, opts)
		optMark := ""
		if tsFieldNeedsOptional(f, opts) {
			optMark = "?"
			// Optional fields use .nullish() in the Zod schema, so the interface
			// must include | null to match z.infer<> (T | null | undefined).
			if !f.Kind.IsWrapper() {
				tsType += " | null"
			}
		}
		g.P(fmt.Sprintf("  %s%s: %s;", f.CamelName, optMark, tsType))
	}
	for _, o := range m.Oneofs {
		if len(o.Variants) >= 2 {
			varNames := make([]string, 0, len(o.Variants))
			for _, v := range o.Variants {
				varNames = append(varNames, toCamelCase(v.ProtoName))
			}
			g.P(fmt.Sprintf("  /** @oneof %s — at most one of: %s */",
				o.FieldName, strings.Join(varNames, ", ")))
		}
		for _, v := range o.Variants {
			tsType := tsPlainOneofVariantType(v, opts)
			g.P(fmt.Sprintf("  %s?: %s | null;", toCamelCase(v.ProtoName), tsType))
		}
	}
	g.P("}")
}

// tsPlainType returns the plain TypeScript type for a field (used in explicit interfaces).
func tsPlainType(f *DomainField, opts *Options) string {
	base := tsPlainBaseType(f, opts)

	if f.IsMap {
		valType := "unknown"
		if f.MapValue != nil {
			valType = tsMapValuePlainType(f.MapValue, opts)
		}
		return fmt.Sprintf("Record<string, %s>", valType)
	}
	if f.Repeated {
		return base + "[]"
	}
	return base
}

// tsPlainBaseType returns the plain TS type for a field's base type.
func tsPlainBaseType(f *DomainField, opts *Options) string {
	switch {
	case f.Kind == FieldKindScalar:
		return tsPlainScalarType(f.ScalarKind, opts)
	case f.Kind == FieldKindEnum:
		return f.EnumTypeName
	case f.Kind == FieldKindMessage:
		return f.MessageTypeName
	case f.Kind == FieldKindTimestamp:
		return "string"
	case f.Kind == FieldKindDuration:
		return "string"
	case f.Kind.IsWrapper():
		return tsPlainWrapperType(f.Kind, opts) + " | null"
	case f.Kind == FieldKindStruct:
		return "Record<string, unknown>"
	case f.Kind == FieldKindValue:
		return "unknown"
	case f.Kind == FieldKindListValue:
		return "unknown[]"
	case f.Kind == FieldKindFieldMask:
		return "string"
	case f.Kind == FieldKindEmpty:
		return "Record<string, never>"
	case f.Kind == FieldKindAny:
		return `{ "@type": string; [key: string]: unknown }`
	default:
		return "unknown"
	}
}

func tsPlainScalarType(k protoreflect.Kind, opts *Options) string {
	switch k {
	case protoreflect.BoolKind:
		return "boolean"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.FloatKind, protoreflect.DoubleKind:
		return "number"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		if opts.TSInt64Style == "bigint" {
			return "bigint"
		}
		return "string"
	case protoreflect.StringKind, protoreflect.BytesKind:
		return "string"
	default:
		return "unknown"
	}
}

func tsPlainWrapperType(k FieldKind, opts *Options) string {
	switch k {
	case FieldKindWrapperBool:
		return "boolean"
	case FieldKindWrapperInt32, FieldKindWrapperUInt32,
		FieldKindWrapperFloat, FieldKindWrapperDouble:
		return "number"
	case FieldKindWrapperInt64, FieldKindWrapperUInt64:
		if opts.TSInt64Style == "bigint" {
			return "bigint"
		}
		return "string"
	case FieldKindWrapperString, FieldKindWrapperBytes:
		return "string"
	default:
		return "unknown"
	}
}

func tsMapValuePlainType(info *MapTypeInfo, opts *Options) string {
	if info == nil {
		return "unknown"
	}
	switch info.Kind {
	case FieldKindTimestamp, FieldKindDuration:
		return "string"
	case FieldKindStruct:
		return "Record<string, unknown>"
	case FieldKindValue:
		return "unknown"
	case FieldKindListValue:
		return "unknown[]"
	case FieldKindFieldMask:
		return "string"
	case FieldKindEmpty:
		return "Record<string, never>"
	case FieldKindAny:
		return `{ "@type": string; [key: string]: unknown }`
	}
	if info.Kind.IsWrapper() {
		return tsPlainWrapperType(info.Kind, opts) + " | null"
	}
	switch info.Kind {
	case FieldKindScalar:
		return tsPlainScalarType(info.ScalarKind, opts)
	case FieldKindEnum:
		return info.EnumTypeName
	case FieldKindMessage:
		return info.MessageTypeName
	default:
		return "unknown"
	}
}

// tsPlainOneofVariantType returns the plain TS type for a oneof variant.
func tsPlainOneofVariantType(v *OneofVariant, opts *Options) string {
	switch v.Kind {
	case FieldKindScalar:
		return tsPlainScalarType(v.ScalarKind, opts)
	case FieldKindMessage:
		return v.TypeName
	case FieldKindEnum:
		return v.TypeName
	case FieldKindTimestamp, FieldKindDuration:
		return "string"
	case FieldKindStruct:
		return "Record<string, unknown>"
	case FieldKindValue:
		return "unknown"
	case FieldKindListValue:
		return "unknown[]"
	case FieldKindFieldMask:
		return "string"
	case FieldKindEmpty:
		return "Record<string, never>"
	case FieldKindAny:
		return `{ "@type": string; [key: string]: unknown }`
	default:
		if v.Kind.IsWrapper() {
			return tsPlainWrapperType(v.Kind, opts) + " | null"
		}
		return "unknown"
	}
}
