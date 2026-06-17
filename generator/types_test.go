package generator

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestGoType(t *testing.T) {
	tests := []struct {
		kind protoreflect.Kind
		want string
	}{
		{protoreflect.BoolKind, "bool"},
		{protoreflect.StringKind, "string"},
		{protoreflect.Int32Kind, "int32"},
		{protoreflect.Int64Kind, "int64"},
		{protoreflect.Uint32Kind, "uint32"},
		{protoreflect.Uint64Kind, "uint64"},
		{protoreflect.FloatKind, "float32"},
		{protoreflect.DoubleKind, "float64"},
		{protoreflect.BytesKind, "[]byte"},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			got := goType(tt.kind)
			if got != tt.want {
				t.Errorf("goType(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestGoZeroValue(t *testing.T) {
	tests := []struct {
		kind protoreflect.Kind
		want string
	}{
		{protoreflect.BoolKind, "false"},
		{protoreflect.StringKind, `""`},
		{protoreflect.Int32Kind, "0"},
		{protoreflect.FloatKind, "0"},
		{protoreflect.BytesKind, "nil"},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			got := goZeroValue(tt.kind)
			if got != tt.want {
				t.Errorf("goZeroValue(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}
