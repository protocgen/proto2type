package generator

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestKotlinScalarType(t *testing.T) {
	tests := []struct {
		kind protoreflect.Kind
		want string
	}{
		{protoreflect.BoolKind, "Boolean"},
		{protoreflect.StringKind, "String"},
		{protoreflect.Int32Kind, "Int"},
		{protoreflect.Sint32Kind, "Int"},
		{protoreflect.Sfixed32Kind, "Int"},
		{protoreflect.Int64Kind, "Long"},
		{protoreflect.Sint64Kind, "Long"},
		{protoreflect.Sfixed64Kind, "Long"},
		{protoreflect.Uint32Kind, "Int"},
		{protoreflect.Fixed32Kind, "Int"},
		{protoreflect.Uint64Kind, "Long"},
		{protoreflect.Fixed64Kind, "Long"},
		{protoreflect.FloatKind, "Float"},
		{protoreflect.DoubleKind, "Double"},
		{protoreflect.BytesKind, "ByteArray"},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			got := kotlinScalarType(tt.kind)
			if got != tt.want {
				t.Errorf("kotlinScalarType(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestKotlinScalarDefault(t *testing.T) {
	tests := []struct {
		kind protoreflect.Kind
		want string
	}{
		{protoreflect.BoolKind, "false"},
		{protoreflect.StringKind, `""`},
		{protoreflect.BytesKind, "byteArrayOf()"},
		{protoreflect.FloatKind, "0.0f"},
		{protoreflect.DoubleKind, "0.0"},
		{protoreflect.Int32Kind, "0"},
		{protoreflect.Int64Kind, "0L"},
		{protoreflect.Uint32Kind, "0"},
		{protoreflect.Uint64Kind, "0L"},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			got := kotlinScalarDefault(tt.kind)
			if got != tt.want {
				t.Errorf("kotlinScalarDefault(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestKotlinFieldType(t *testing.T) {
	tests := []struct {
		name string
		f    DomainField
		want string
	}{
		{
			name: "string scalar",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind},
			want: "String",
		},
		{
			name: "bool scalar",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.BoolKind},
			want: "Boolean",
		},
		{
			name: "int32 scalar",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Int32Kind},
			want: "Int",
		},
		{
			name: "optional string",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, Optional: true},
			want: "String?",
		},
		{
			name: "repeated string",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, Repeated: true},
			want: "List<String>",
		},
		{
			name: "timestamp",
			f:    DomainField{Kind: FieldKindTimestamp},
			want: "Instant",
		},
		{
			name: "optional timestamp",
			f:    DomainField{Kind: FieldKindTimestamp, Optional: true},
			want: "Instant?",
		},
		{
			name: "duration",
			f:    DomainField{Kind: FieldKindDuration},
			want: "Duration",
		},
		{
			name: "optional duration",
			f:    DomainField{Kind: FieldKindDuration, Optional: true},
			want: "Duration?",
		},
		{
			name: "message",
			f:    DomainField{Kind: FieldKindMessage, MessageTypeName: "Address"},
			want: "Address?",
		},
		{
			name: "repeated message",
			f:    DomainField{Kind: FieldKindMessage, MessageTypeName: "Tag", Repeated: true},
			want: "List<Tag>",
		},
		{
			name: "enum",
			f:    DomainField{Kind: FieldKindEnum, EnumTypeName: "UserStatus"},
			want: "UserStatus",
		},
		{
			name: "optional enum",
			f:    DomainField{Kind: FieldKindEnum, EnumTypeName: "UserStatus", Optional: true},
			want: "UserStatus?",
		},
		{
			name: "wrapper string",
			f:    DomainField{Kind: FieldKindWrapperString},
			want: "String?",
		},
		{
			name: "wrapper bool",
			f:    DomainField{Kind: FieldKindWrapperBool},
			want: "Boolean?",
		},
		{
			name: "wrapper int32",
			f:    DomainField{Kind: FieldKindWrapperInt32},
			want: "Int?",
		},
		{
			name: "wrapper int64",
			f:    DomainField{Kind: FieldKindWrapperInt64},
			want: "Long?",
		},
		{
			name: "struct",
			f:    DomainField{Kind: FieldKindStruct},
			want: "Map<String, Any?>",
		},
		{
			name: "value",
			f:    DomainField{Kind: FieldKindValue},
			want: "Any?",
		},
		{
			name: "list value",
			f:    DomainField{Kind: FieldKindListValue},
			want: "List<Any?>",
		},
		{
			name: "any",
			f:    DomainField{Kind: FieldKindAny},
			want: "Any?",
		},
		{
			name: "bytes",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.BytesKind},
			want: "ByteArray",
		},
		{
			name: "map string->string",
			f: DomainField{
				IsMap: true,
				MapKey: &MapTypeInfo{
					Kind:       FieldKindScalar,
					ScalarKind: protoreflect.StringKind,
				},
				MapValue: &MapTypeInfo{
					Kind:       FieldKindScalar,
					ScalarKind: protoreflect.StringKind,
				},
			},
			want: "Map<String, String>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kotlinFieldType(tt.f)
			if got != tt.want {
				t.Errorf("kotlinFieldType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKotlinDefaultValue(t *testing.T) {
	tests := []struct {
		name string
		f    DomainField
		want string
	}{
		{
			name: "string",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind},
			want: `""`,
		},
		{
			name: "bool",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.BoolKind},
			want: "false",
		},
		{
			name: "int32",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Int32Kind},
			want: "0",
		},
		{
			name: "int64",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Int64Kind},
			want: "0L",
		},
		{
			name: "float",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.FloatKind},
			want: "0.0f",
		},
		{
			name: "double",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.DoubleKind},
			want: "0.0",
		},
		{
			name: "repeated",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, Repeated: true},
			want: "emptyList()",
		},
		{
			name: "map",
			f:    DomainField{IsMap: true},
			want: "emptyMap()",
		},
		{
			name: "timestamp",
			f:    DomainField{Kind: FieldKindTimestamp},
			want: "Instant.fromEpochSeconds(0)",
		},
		{
			name: "optional timestamp",
			f:    DomainField{Kind: FieldKindTimestamp, Optional: true},
			want: "null",
		},
		{
			name: "duration",
			f:    DomainField{Kind: FieldKindDuration},
			want: "Duration.ZERO",
		},
		{
			name: "optional duration",
			f:    DomainField{Kind: FieldKindDuration, Optional: true},
			want: "null",
		},
		{
			name: "message",
			f:    DomainField{Kind: FieldKindMessage, MessageTypeName: "Address"},
			want: "null",
		},
		{
			name: "optional",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, Optional: true},
			want: "null",
		},
		{
			name: "enum",
			f:    DomainField{Kind: FieldKindEnum, EnumTypeName: "UserStatus"},
			want: "UserStatus.entries.first()",
		},
		{
			name: "optional enum",
			f:    DomainField{Kind: FieldKindEnum, EnumTypeName: "UserStatus", Optional: true},
			want: "null",
		},
		{
			name: "wrapper string",
			f:    DomainField{Kind: FieldKindWrapperString},
			want: "null",
		},
		{
			name: "bytes",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.BytesKind},
			want: "byteArrayOf()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kotlinDefaultValue(tt.f)
			if got != tt.want {
				t.Errorf("kotlinDefaultValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPascalToUpperSnake(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Active", "ACTIVE"},
		{"Unspecified", "UNSPECIFIED"},
		{"Suspended", "SUSPENDED"},
		{"InProgress", "IN_PROGRESS"},
		{"", ""},
		{"A", "A"},
		{"HTMLParser", "HTML_PARSER"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := pascalToUpperSnake(tt.input)
			if got != tt.want {
				t.Errorf("pascalToUpperSnake(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
