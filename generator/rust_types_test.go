package generator

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestRustType(t *testing.T) {
	tests := []struct {
		kind protoreflect.Kind
		want string
	}{
		{protoreflect.BoolKind, "bool"},
		{protoreflect.StringKind, "String"},
		{protoreflect.Int32Kind, "i32"},
		{protoreflect.Sint32Kind, "i32"},
		{protoreflect.Sfixed32Kind, "i32"},
		{protoreflect.Int64Kind, "i64"},
		{protoreflect.Sint64Kind, "i64"},
		{protoreflect.Sfixed64Kind, "i64"},
		{protoreflect.Uint32Kind, "u32"},
		{protoreflect.Fixed32Kind, "u32"},
		{protoreflect.Uint64Kind, "u64"},
		{protoreflect.Fixed64Kind, "u64"},
		{protoreflect.FloatKind, "f32"},
		{protoreflect.DoubleKind, "f64"},
		{protoreflect.BytesKind, "Vec<u8>"},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			got := rustType(tt.kind)
			if got != tt.want {
				t.Errorf("rustType(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestRustZeroValue(t *testing.T) {
	tests := []struct {
		kind protoreflect.Kind
		want string
	}{
		{protoreflect.BoolKind, "false"},
		{protoreflect.StringKind, "String::new()"},
		{protoreflect.BytesKind, "Vec::new()"},
		{protoreflect.FloatKind, "0.0"},
		{protoreflect.DoubleKind, "0.0"},
		{protoreflect.Int32Kind, "0"},
		{protoreflect.Int64Kind, "0"},
		{protoreflect.Uint32Kind, "0"},
		{protoreflect.Uint64Kind, "0"},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			got := rustZeroValue(tt.kind)
			if got != tt.want {
				t.Errorf("rustZeroValue(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}
