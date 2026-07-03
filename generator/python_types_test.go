package generator

import (
	"testing"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestPythonScalarType(t *testing.T) {
	tests := []struct {
		kind protoreflect.Kind
		want string
	}{
		{protoreflect.BoolKind, "bool"},
		{protoreflect.Int32Kind, "int"},
		{protoreflect.Sint32Kind, "int"},
		{protoreflect.Sfixed32Kind, "int"},
		{protoreflect.Int64Kind, "int"},
		{protoreflect.Sint64Kind, "int"},
		{protoreflect.Sfixed64Kind, "int"},
		{protoreflect.Uint32Kind, "int"},
		{protoreflect.Fixed32Kind, "int"},
		{protoreflect.Uint64Kind, "int"},
		{protoreflect.Fixed64Kind, "int"},
		{protoreflect.FloatKind, "float"},
		{protoreflect.DoubleKind, "float"},
		{protoreflect.StringKind, "str"},
		{protoreflect.BytesKind, "bytes"},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			got := pythonScalarType(tt.kind)
			if got != tt.want {
				t.Errorf("pythonScalarType(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestPythonScalarDefault(t *testing.T) {
	tests := []struct {
		kind protoreflect.Kind
		want string
	}{
		{protoreflect.BoolKind, "False"},
		{protoreflect.Int32Kind, "0"},
		{protoreflect.Int64Kind, "0"},
		{protoreflect.Uint32Kind, "0"},
		{protoreflect.FloatKind, "0.0"},
		{protoreflect.DoubleKind, "0.0"},
		{protoreflect.StringKind, "''"},
		{protoreflect.BytesKind, "b''"},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			got := pythonScalarDefault(tt.kind)
			if got != tt.want {
				t.Errorf("pythonScalarDefault(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestPythonFieldType(t *testing.T) {
	opts := &Options{Lang: "python"}
	tests := []struct {
		name string
		f    DomainField
		want string
	}{
		{
			name: "scalar string",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind},
			want: "str",
		},
		{
			name: "repeated string",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, Repeated: true},
			want: "list[str]",
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
			want: "dict[str, str]",
		},
		{
			name: "message",
			f:    DomainField{Kind: FieldKindMessage, MessageTypeName: "Address"},
			want: "Address",
		},
		{
			name: "timestamp",
			f:    DomainField{Kind: FieldKindTimestamp},
			want: "datetime",
		},
		{
			name: "duration",
			f:    DomainField{Kind: FieldKindDuration},
			want: "timedelta",
		},
		{
			name: "struct",
			f:    DomainField{Kind: FieldKindStruct},
			want: "dict[str, Any]",
		},
		{
			name: "enum",
			f:    DomainField{Kind: FieldKindEnum, EnumTypeName: "UserStatus"},
			want: "UserStatus",
		},
		{
			name: "repeated message",
			f:    DomainField{Kind: FieldKindMessage, MessageTypeName: "Tag", Repeated: true},
			want: "list[Tag]",
		},
		{
			name: "value",
			f:    DomainField{Kind: FieldKindValue},
			want: "Any",
		},
		{
			name: "list value",
			f:    DomainField{Kind: FieldKindListValue},
			want: "list[Any]",
		},
		{
			name: "any",
			f:    DomainField{Kind: FieldKindAny},
			want: "Any",
		},
		{
			name: "field mask",
			f:    DomainField{Kind: FieldKindFieldMask},
			want: "list[str]",
		},
		{
			name: "empty",
			f:    DomainField{Kind: FieldKindEmpty},
			want: "None",
		},
		{
			name: "map int->message",
			f: DomainField{
				IsMap: true,
				MapKey: &MapTypeInfo{
					Kind:       FieldKindScalar,
					ScalarKind: protoreflect.Int32Kind,
				},
				MapValue: &MapTypeInfo{
					Kind:            FieldKindMessage,
					MessageTypeName: "Settings",
				},
			},
			want: "dict[int, Settings]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pythonFieldType(&tt.f, opts)
			if got != tt.want {
				t.Errorf("pythonFieldType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPythonDefaultValue(t *testing.T) {
	opts := &Options{Lang: "python"}
	optsRaw := &Options{Lang: "python", PythonEnumStyle: "raw"}

	tests := []struct {
		name string
		f    DomainField
		opts *Options
		want string
	}{
		{
			name: "required field no default",
			f: DomainField{
				Kind:           FieldKindScalar,
				ScalarKind:     protoreflect.StringKind,
				FieldBehaviors: []annotations.FieldBehavior{annotations.FieldBehavior_REQUIRED},
			},
			opts: opts,
			want: "",
		},
		{
			name: "optional scalar",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, Optional: true},
			opts: opts,
			want: "None",
		},
		{
			name: "scalar string",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind},
			opts: opts,
			want: "''",
		},
		{
			name: "scalar bool",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.BoolKind},
			opts: opts,
			want: "False",
		},
		{
			name: "scalar int32",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Int32Kind},
			opts: opts,
			want: "0",
		},
		{
			name: "scalar float",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.FloatKind},
			opts: opts,
			want: "0.0",
		},
		{
			name: "repeated",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, Repeated: true},
			opts: opts,
			want: "None",
		},
		{
			name: "map",
			f:    DomainField{IsMap: true},
			opts: opts,
			want: "None",
		},
		{
			name: "message",
			f:    DomainField{Kind: FieldKindMessage, MessageTypeName: "Address"},
			opts: opts,
			want: "None",
		},
		{
			name: "timestamp",
			f:    DomainField{Kind: FieldKindTimestamp},
			opts: opts,
			want: "None",
		},
		{
			name: "duration",
			f:    DomainField{Kind: FieldKindDuration},
			opts: opts,
			want: "None",
		},
		{
			name: "enum with UNSPECIFIED default style",
			f: DomainField{
				Kind:            FieldKindEnum,
				EnumTypeName:    "UserStatus",
				EnumDefaultName: "USER_STATUS_UNSPECIFIED",
			},
			opts: opts,
			want: "None",
		},
		{
			name: "enum with UNSPECIFIED raw style",
			f: DomainField{
				Kind:            FieldKindEnum,
				EnumTypeName:    "UserStatus",
				EnumDefaultName: "USER_STATUS_UNSPECIFIED",
			},
			opts: optsRaw,
			want: "UserStatus.USER_STATUS_UNSPECIFIED",
		},
		{
			name: "enum with non-UNSPECIFIED default",
			f: DomainField{
				Kind:            FieldKindEnum,
				EnumTypeName:    "Priority",
				EnumDefaultName: "PRIORITY_LOW",
			},
			opts: opts,
			want: "Priority.PRIORITY_LOW",
		},
		{
			name: "wrapper bool",
			f:    DomainField{Kind: FieldKindWrapperBool},
			opts: opts,
			want: "None",
		},
		{
			name: "wrapper string",
			f:    DomainField{Kind: FieldKindWrapperString},
			opts: opts,
			want: "None",
		},
		{
			name: "struct",
			f:    DomainField{Kind: FieldKindStruct},
			opts: opts,
			want: "None",
		},
		{
			name: "validate required",
			f: DomainField{
				Kind:                FieldKindScalar,
				ScalarKind:          protoreflect.StringKind,
				ValidateConstraints: &ValidateConstraints{Required: true},
			},
			opts: opts,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pythonDefaultValue(&tt.f, tt.opts)
			if got != tt.want {
				t.Errorf("pythonDefaultValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPythonWrapperType(t *testing.T) {
	tests := []struct {
		kind FieldKind
		want string
	}{
		{FieldKindWrapperBool, "bool | None"},
		{FieldKindWrapperString, "str | None"},
		{FieldKindWrapperInt32, "int | None"},
		{FieldKindWrapperInt64, "int | None"},
		{FieldKindWrapperUInt32, "int | None"},
		{FieldKindWrapperUInt64, "int | None"},
		{FieldKindWrapperFloat, "float | None"},
		{FieldKindWrapperDouble, "float | None"},
		{FieldKindWrapperBytes, "bytes | None"},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			got := pythonWrapperType(tt.kind)
			if got != tt.want {
				t.Errorf("pythonWrapperType(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

func TestPythonTypeNeedsOptional(t *testing.T) {
	tests := []struct {
		name string
		f    DomainField
		want bool
	}{
		{
			name: "optional field",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, Optional: true},
			want: true,
		},
		{
			name: "repeated",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, Repeated: true},
			want: true,
		},
		{
			name: "map",
			f:    DomainField{IsMap: true},
			want: true,
		},
		{
			name: "message",
			f:    DomainField{Kind: FieldKindMessage, MessageTypeName: "Address"},
			want: true,
		},
		{
			name: "scalar non-optional",
			f:    DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind},
			want: false,
		},
		{
			name: "enum non-optional",
			f:    DomainField{Kind: FieldKindEnum, EnumTypeName: "Status"},
			want: false,
		},
		{
			name: "timestamp",
			f:    DomainField{Kind: FieldKindTimestamp},
			want: true,
		},
		{
			name: "duration",
			f:    DomainField{Kind: FieldKindDuration},
			want: true,
		},
		{
			name: "struct",
			f:    DomainField{Kind: FieldKindStruct},
			want: true,
		},
		{
			name: "wrapper",
			f:    DomainField{Kind: FieldKindWrapperBool},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pythonTypeNeedsOptional(&tt.f)
			if got != tt.want {
				t.Errorf("pythonTypeNeedsOptional() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEscapePythonKeyword(t *testing.T) {
	tests := []struct {
		input     string
		wantName  string
		wantAlias string
	}{
		{"type", "type_", "type"},
		{"class", "class_", "class"},
		{"name", "name", ""},
		{"list", "list_", "list"},
		{"int", "int_", "int"},
		{"dict", "dict_", "dict"},
		{"str", "str_", "str"},
		{"bool", "bool_", "bool"},
		{"email", "email", ""},
		{"return", "return_", "return"},
		{"yield", "yield_", "yield"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotName, gotAlias := escapePythonKeyword(tt.input)
			if gotName != tt.wantName {
				t.Errorf("escapePythonKeyword(%q) name = %q, want %q", tt.input, gotName, tt.wantName)
			}
			if gotAlias != tt.wantAlias {
				t.Errorf("escapePythonKeyword(%q) alias = %q, want %q", tt.input, gotAlias, tt.wantAlias)
			}
		})
	}
}

func TestPythonOutputFilename(t *testing.T) {
	tests := []struct {
		name  string
		proto string
		opts  Options
		want  string
	}{
		{
			name:  "default suffix",
			proto: "user.proto",
			opts:  Options{},
			want:  "user_pb2_pydantic.py",
		},
		{
			name:  "strip proto suffix",
			proto: "user.proto",
			opts:  Options{StripProtoSuffix: true},
			want:  "user.py",
		},
		{
			name:  "path stripped to basename",
			proto: "path/to/user.proto",
			opts:  Options{},
			want:  "user_pb2_pydantic.py",
		},
		{
			name:  "output file override",
			proto: "user.proto",
			opts:  Options{OutputFile: "custom_output.py"},
			want:  "custom_output.py",
		},
		{
			name:  "strip proto suffix with path",
			proto: "some/nested/service.proto",
			opts:  Options{StripProtoSuffix: true},
			want:  "service.py",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pythonOutputFilename(tt.proto, &tt.opts)
			if got != tt.want {
				t.Errorf("pythonOutputFilename(%q) = %q, want %q", tt.proto, got, tt.want)
			}
		})
	}
}
