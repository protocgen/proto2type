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
		{protoreflect.Int64Kind, "z.string()"},
		{protoreflect.Uint64Kind, "z.string()"},
		{protoreflect.FloatKind, "z.number()"},
		{protoreflect.DoubleKind, "z.number()"},
		{protoreflect.StringKind, "z.string()"},
		{protoreflect.BytesKind, "z.string()"},
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
		{FieldKindTimestamp, "z.string().datetime()"},
		{FieldKindDuration, "z.string()"},
		{FieldKindWrapperBool, "z.boolean().nullable()"},
		{FieldKindWrapperInt32, "z.number().int().nullable()"},
		{FieldKindWrapperInt64, "z.string().nullable()"},
		{FieldKindWrapperUInt32, "z.number().int().nonnegative().nullable()"},
		{FieldKindWrapperUInt64, "z.string().nullable()"},
		{FieldKindWrapperFloat, "z.number().nullable()"},
		{FieldKindWrapperDouble, "z.number().nullable()"},
		{FieldKindWrapperString, "z.string().nullable()"},
		{FieldKindWrapperBytes, "z.string().nullable()"},
		{FieldKindStruct, "z.record(z.string(), z.unknown())"},
		{FieldKindValue, "z.unknown()"},
		{FieldKindListValue, "z.array(z.unknown())"},
		{FieldKindFieldMask, "z.array(z.string())"},
		{FieldKindEmpty, "z.object({})"},
		{FieldKindAny, "z.unknown()"},
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

	if got := tsScalarZodType(protoreflect.Int64Kind, opts); got != "z.bigint()" {
		t.Errorf("Int64 = %q, want z.bigint()", got)
	}
	if got := tsScalarZodType(protoreflect.Uint64Kind, opts); got != "z.bigint().nonnegative()" {
		t.Errorf("Uint64 = %q, want z.bigint().nonnegative()", got)
	}

	gotWkt, _ := tsWKTZodType(FieldKindWrapperInt64, opts)
	if gotWkt != "z.bigint().nullable()" {
		t.Errorf("WrapperInt64 = %q, want z.bigint().nullable()", gotWkt)
	}
}

func TestTSOutputFilename(t *testing.T) {
	opts := &Options{}
	if got := tsOutputFilename("path/to/my_file.proto", opts); got != "my_file.type.ts" {
		t.Errorf("got %q, want my_file.type.ts", got)
	}

	opts.OutputFile = "custom.ts"
	if got := tsOutputFilename("path/to/my_file.proto", opts); got != "custom.ts" {
		t.Errorf("got %q, want custom.ts", got)
	}
}
