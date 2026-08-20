package generator

import (
	"fmt"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestTSScalarZodType(t *testing.T) {
	tests := []struct {
		kind protoreflect.Kind
		want string
	}{
		{protoreflect.BoolKind, "z.boolean()"},
		{protoreflect.Int32Kind, "z.number().int()"},
		{protoreflect.Sint32Kind, "z.number().int()"},
		{protoreflect.Sfixed32Kind, "z.number().int()"},
		{protoreflect.Uint32Kind, "z.number().int().nonnegative()"},
		{protoreflect.Fixed32Kind, "z.number().int().nonnegative()"},
		{protoreflect.Int64Kind, `z.union([z.string().max(100).regex(/^-?\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" })]).pipe(z.coerce.string())`},
		{protoreflect.Uint64Kind, `z.union([z.string().max(100).regex(/^\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" }).refine(n => n >= 0, { message: "must be non-negative" })]).pipe(z.coerce.string())`},
		{protoreflect.FloatKind, `z.union([z.number(), z.enum(["NaN", "Infinity", "-Infinity"])])`},
		{protoreflect.DoubleKind, `z.union([z.number(), z.enum(["NaN", "Infinity", "-Infinity"])])`},
		{protoreflect.StringKind, "z.string()"},
		{protoreflect.BytesKind, `z.string().regex(/^[A-Za-z0-9+\/\-_]*={0,2}$/, { message: "must be valid base64" })`},
	}

	opts := &Options{TSInt64Style: "string"}
	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			got := tsScalarZodType(tt.kind, opts)
			if got != tt.want {
				t.Errorf("tsScalarZodType(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestTSWKTZodType(t *testing.T) {
	tests := []struct {
		kind FieldKind
		want string
	}{
		{FieldKindTimestamp, "z.string().datetime({ offset: true })"},
		{FieldKindDuration, `z.string().regex(new RegExp("^-?[0-9]+(\\.[0-9]+)?s$"), { message: "must be a valid Duration (e.g. '1.5s')" })`},
		{FieldKindWrapperBool, "z.boolean().nullable()"},
		{FieldKindWrapperInt32, "z.number().int().nullable()"},
		{FieldKindWrapperInt64, `z.union([z.string().max(100).regex(/^-?\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" })]).pipe(z.coerce.string()).nullable()`},
		{FieldKindWrapperUInt32, "z.number().int().nonnegative().nullable()"},
		{FieldKindWrapperUInt64, `z.union([z.string().max(100).regex(/^\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" }).refine(n => n >= 0, { message: "must be non-negative" })]).pipe(z.coerce.string()).nullable()`},
		{FieldKindWrapperFloat, `z.union([z.number(), z.enum(["NaN", "Infinity", "-Infinity"])]).nullable()`},
		{FieldKindWrapperDouble, `z.union([z.number(), z.enum(["NaN", "Infinity", "-Infinity"])]).nullable()`},
		{FieldKindWrapperString, "z.string().nullable()"},
		{FieldKindWrapperBytes, `z.string().regex(/^[A-Za-z0-9+\/\-_]*={0,2}$/, { message: "must be valid base64" }).nullable()`},
		{FieldKindStruct, "z.record(z.string().refine(k => k !== '__proto__' && k !== 'constructor' && k !== 'prototype'), z.unknown())"},
		{FieldKindValue, "z.unknown()"},
		{FieldKindListValue, "z.array(z.unknown())"},
		{FieldKindFieldMask, `z.string().regex(new RegExp("^[a-zA-Z_][a-zA-Z0-9_]*(\\.[a-zA-Z_][a-zA-Z0-9_]*)*(,[a-zA-Z_][a-zA-Z0-9_]*(\\.[a-zA-Z_][a-zA-Z0-9_]*)*)*$"), { message: "must be a valid FieldMask (comma-separated field paths)" })`},
		{FieldKindEmpty, "z.record(z.string(), z.never())"},
		{FieldKindAny, `z.object({ "@type": z.string() }).passthrough()`},
	}

	opts := &Options{TSInt64Style: "string"}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v", tt.kind), func(t *testing.T) {
			got, ok := tsWKTZodType(tt.kind, opts)
			if !ok {
				t.Errorf("tsWKTZodType(%v) returned false", tt.kind)
			}
			if got != tt.want {
				t.Errorf("tsWKTZodType(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestTSScalarZodType_BigInt(t *testing.T) {
	opts := &Options{TSInt64Style: "bigint"}

	expBigInt := `z.union([z.string().max(100).regex(/^-?\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" }), z.bigint()]).pipe(z.coerce.bigint()).refine(v => v >= -9223372036854775808n && v <= 9223372036854775807n, { message: "int64 out of range" })`
	expBigIntNN := `z.union([z.string().max(100).regex(/^-?\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" }), z.bigint()]).pipe(z.coerce.bigint()).refine(v => v >= 0n && v <= 18446744073709551615n, { message: "uint64 out of range" })`
	expWrapBigInt := `z.union([z.string().max(100).regex(/^-?\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" }), z.bigint()]).pipe(z.coerce.bigint()).refine(v => v >= -9223372036854775808n && v <= 9223372036854775807n, { message: "int64 out of range" }).nullable()`
	expWrapBigIntNN := `z.union([z.string().max(100).regex(/^-?\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" }), z.bigint()]).pipe(z.coerce.bigint()).refine(v => v >= 0n && v <= 18446744073709551615n, { message: "uint64 out of range" }).nullable()`

	if got := tsScalarZodType(protoreflect.Int64Kind, opts); got != expBigInt {
		t.Errorf("Int64 = %q, want %s", got, expBigInt)
	}
	if got := tsScalarZodType(protoreflect.Uint64Kind, opts); got != expBigIntNN {
		t.Errorf("Uint64 = %q, want %s", got, expBigIntNN)
	}

	gotWkt, _ := tsWKTZodType(FieldKindWrapperInt64, opts)
	if gotWkt != expWrapBigInt {
		t.Errorf("WrapperInt64 = %q, want %s", gotWkt, expWrapBigInt)
	}
	gotWktU, _ := tsWKTZodType(FieldKindWrapperUInt64, opts)
	if gotWktU != expWrapBigIntNN {
		t.Errorf("WrapperUInt64 = %q, want %s", gotWktU, expWrapBigIntNN)
	}
}

func TestTSOutputFilename(t *testing.T) {
	opts := &Options{}
	got, err := tsOutputFilename("path/to/my_file.proto", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "path/to/my_file.type.ts" {
		t.Errorf("got %q, want path/to/my_file.type.ts", got)
	}

	opts.OutputFile = "custom.ts"
	got, err = tsOutputFilename("path/to/my_file.proto", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "custom.ts" {
		t.Errorf("got %q, want custom.ts", got)
	}

	// Error case: path traversal should be rejected.
	opts.OutputFile = ""
	_, err = tsOutputFilename("../../etc/passwd.proto", opts)
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}

func TestTsMapValueZodType(t *testing.T) {
	opts := &Options{}
	tests := []struct {
		name string
		info *MapTypeInfo
		want string
	}{
		{"nil MapTypeInfo", nil, "z.unknown()"},
		{"FieldKindScalar String", &MapTypeInfo{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind}, "z.string()"},
		{"FieldKindScalar Int32", &MapTypeInfo{Kind: FieldKindScalar, ScalarKind: protoreflect.Int32Kind}, "z.number().int()"},
		{"FieldKindTimestamp", &MapTypeInfo{Kind: FieldKindTimestamp}, "z.string().datetime({ offset: true })"},
		{"FieldKindMessage Foo", &MapTypeInfo{Kind: FieldKindMessage, MessageTypeName: "Foo"}, "FooSchema"},
		{"FieldKindEnum Bar", &MapTypeInfo{Kind: FieldKindEnum, EnumTypeName: "Bar"}, "BarSchema"},
		{"FieldKindStruct", &MapTypeInfo{Kind: FieldKindStruct}, "z.record(z.string().refine(k => k !== '__proto__' && k !== 'constructor' && k !== 'prototype'), z.unknown())"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tsMapValueZodType(tt.info, opts)
			if got != tt.want {
				t.Errorf("tsMapValueZodType() = %q, want %q", got, tt.want)
			}
		})
	}
}
