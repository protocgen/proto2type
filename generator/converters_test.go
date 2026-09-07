package generator

import (
	"testing"
)

func TestIrWrapperPbFuncName(t *testing.T) {
	tests := []struct {
		kind FieldKind
		want string
	}{
		{FieldKindWrapperString, "String"},
		{FieldKindWrapperBool, "Bool"},
		{FieldKindWrapperInt32, "Int32"},
		{FieldKindWrapperInt64, "Int64"},
		{FieldKindWrapperUInt32, "UInt32"},
		{FieldKindWrapperUInt64, "UInt64"},
		{FieldKindWrapperFloat, "Float"},
		{FieldKindWrapperDouble, "Double"},
		{FieldKindWrapperBytes, "Bytes"},
		{FieldKindScalar, "UNKNOWN"}, // Default case
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			if got := irWrapperPbFuncName(tt.kind); got != tt.want {
				t.Errorf("irWrapperPbFuncName(%v) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}

func TestIrWrapperGoSliceType(t *testing.T) {
	tests := []struct {
		kind FieldKind
		want string
	}{
		{FieldKindWrapperBool, "[]*bool"},
		{FieldKindWrapperInt32, "[]*int32"},
		{FieldKindWrapperInt64, "[]*int64"},
		{FieldKindWrapperUInt32, "[]*uint32"},
		{FieldKindWrapperUInt64, "[]*uint64"},
		{FieldKindWrapperFloat, "[]*float32"},
		{FieldKindWrapperDouble, "[]*float64"},
		{FieldKindWrapperString, "[]*string"},
		{FieldKindWrapperBytes, "[]*[]byte"},
		{FieldKindScalar, "[]any"}, // Default case
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			if got := irWrapperGoSliceType(tt.kind); got != tt.want {
				t.Errorf("irWrapperGoSliceType(%v) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}

func TestNeedsTryToProto(t *testing.T) {
	tests := []struct {
		name string
		dm   *DomainMessage
		want bool
	}{
		{
			name: "Empty message",
			dm:   &DomainMessage{Fields: []*DomainField{}},
			want: false,
		},
		{
			name: "Message with scalar fields only",
			dm: &DomainMessage{
				Fields: []*DomainField{
					{Kind: FieldKindScalar},
				},
			},
			want: false,
		},
		{
			name: "Message with FieldKindStruct singular",
			dm: &DomainMessage{
				Fields: []*DomainField{
					{Kind: FieldKindStruct},
				},
			},
			want: true,
		},
		{
			name: "Message with FieldKindValue singular",
			dm: &DomainMessage{
				Fields: []*DomainField{
					{Kind: FieldKindValue},
				},
			},
			want: true,
		},
		{
			name: "Message with FieldKindListValue singular",
			dm: &DomainMessage{
				Fields: []*DomainField{
					{Kind: FieldKindListValue},
				},
			},
			want: true,
		},
		{
			name: "Message with repeated Struct",
			dm: &DomainMessage{
				Fields: []*DomainField{
					{Kind: FieldKindStruct, Repeated: true},
				},
			},
			want: true,
		},
		{
			name: "Message with map containing Value",
			dm: &DomainMessage{
				Fields: []*DomainField{
					{
						IsMap: true,
						MapValue: &MapTypeInfo{
							Kind: FieldKindValue,
						},
					},
				},
			},
			want: true,
		},
		{
			name: "Message with oneof containing Struct variant",
			dm: &DomainMessage{
				Fields: []*DomainField{
					{
						IsOneof:       true,
						OneofTypeName: "MyOneof",
					},
				},
				Oneofs: []*DomainOneof{
					{
						Name: "MyOneof",
						Variants: []*OneofVariant{
							{Kind: FieldKindStruct},
						},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsTryToProto(tt.dm); got != tt.want {
				t.Errorf("needsTryToProto() = %v, want %v", got, tt.want)
			}
		})
	}
}
