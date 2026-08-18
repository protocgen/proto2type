package generator

import (
	"os"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestTSFieldType_Integration(t *testing.T) {
	opts := &Options{
		Domain:       true,
		Lang:         "typescript",
		TSZodImport:  "zod",
		TSInt64Style: "string",
	}

	ir := buildIRForProto(t, "user.proto", opts)

	if len(ir.Messages) == 0 {
		t.Fatalf("expected messages")
	}
}

func TestTSEnumGeneration_Integration(t *testing.T) {
	opts := &Options{
		Domain:      true,
		Lang:        "typescript",
		TSEnumStyle: "native",
	}

	ir := buildIRForProto(t, "user.proto", opts)

	if len(ir.Enums) == 0 {
		t.Fatalf("expected enums")
	}
}

func TestTSOneofDetection_Integration(t *testing.T) {
	opts := &Options{
		Domain: true,
		Lang:   "typescript",
	}

	buildIRForProto(t, "user.proto", opts)
}

func TestTSValidateConstraints_Integration(t *testing.T) {
	opts := &Options{
		Domain:   true,
		Lang:     "typescript",
		Validate: "true",
	}

	buildIRForProto(t, "user.proto", opts)
}

func TestIsRecursive(t *testing.T) {
	tests := []struct {
		name string
		msg  *DomainMessage
		want bool
	}{
		{
			name: "false for a message with no message-typed fields",
			msg: &DomainMessage{
				Name: "Foo",
				Fields: []*DomainField{
					{Kind: FieldKindScalar},
				},
			},
			want: false,
		},
		{
			name: "false for a message referencing a different message",
			msg: &DomainMessage{
				Name: "Foo",
				Fields: []*DomainField{
					{Kind: FieldKindMessage, MessageTypeName: "Bar"},
				},
			},
			want: false,
		},
		{
			name: "true for a message with a field whose MessageTypeName == the message's Name",
			msg: &DomainMessage{
				Name: "Foo",
				Fields: []*DomainField{
					{Kind: FieldKindMessage, MessageTypeName: "Foo", NeedsBox: true},
				},
			},
			want: true,
		},
		{
			name: "true for a message with an oneof variant whose TypeName == the message's Name",
			msg: &DomainMessage{
				Name: "Foo",
				Oneofs: []*DomainOneof{
					{
						Variants: []*OneofVariant{
							{Kind: FieldKindMessage, TypeName: "Foo", NeedsBox: true},
						},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRecursive(tt.msg)
			if got != tt.want {
				t.Errorf("isRecursive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTsFieldNeedsOptional_Enum(t *testing.T) {
	opts := &Options{}
	f := &DomainField{Kind: FieldKindEnum, EnumTypeName: "Status", EnumDefaultName: "STATUS_UNSPECIFIED"}
	if tsFieldNeedsOptional(f, opts) {
		t.Error("non-optional enum field should not be optional")
	}
	f2 := &DomainField{Kind: FieldKindEnum, EnumTypeName: "Status", Optional: true}
	if !tsFieldNeedsOptional(f2, opts) {
		t.Error("optional enum field should be optional")
	}
}

func TestTsFieldZodExpr_EnumDefault(t *testing.T) {
	f := &DomainField{
		Kind:            FieldKindEnum,
		EnumTypeName:    "Status",
		EnumDefaultName: "STATUS_UNSPECIFIED",
		CamelName:       "status",
	}
	opts := &Options{}
	got := tsFieldZodExpr(f, opts)
	if !strings.Contains(got, `.default("STATUS_UNSPECIFIED")`) {
		t.Errorf("expected .default(\"STATUS_UNSPECIFIED\"), got %s", got)
	}
}

func TestCollectTSImports(t *testing.T) {
	tests := []struct {
		name string
		ir   *DomainFile
		want []tsImportEntry
	}{
		{
			name: "No cross-file references",
			ir: &DomainFile{
				SourcePath: "user.proto",
				Messages: []*DomainMessage{
					{
						Fields: []*DomainField{
							{Kind: FieldKindScalar},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "Field referencing external message",
			ir: &DomainFile{
				SourcePath: "a/user.proto",
				Messages: []*DomainMessage{
					{
						Fields: []*DomainField{
							{
								Kind:              FieldKindMessage,
								MessageTypeName:   "Address",
								MessageSourcePath: "b/address.proto",
							},
						},
					},
				},
			},
			want: []tsImportEntry{
				{
					path:  "../b/address.type.js",
					names: []string{"Address", "AddressSchema"},
				},
			},
		},
		{
			name: "Map referencing external types",
			ir: &DomainFile{
				SourcePath: "a/b/user.proto",
				Messages: []*DomainMessage{
					{
						Fields: []*DomainField{
							{
								IsMap: true,
								MapValue: &MapTypeInfo{
									Kind:            FieldKindMessage,
									MessageTypeName: "Role",
									SourcePath:      "a/role.proto",
								},
							},
						},
					},
				},
			},
			want: []tsImportEntry{
				{
					path:  "../role.type.js",
					names: []string{"Role", "RoleSchema"},
				},
			},
		},
		{
			name: "Deduplication of multiple refs",
			ir: &DomainFile{
				SourcePath: "user.proto",
				Messages: []*DomainMessage{
					{
						Fields: []*DomainField{
							{
								Kind:              FieldKindMessage,
								MessageTypeName:   "Address",
								MessageSourcePath: "common/common.proto",
							},
							{
								Kind:              FieldKindMessage,
								MessageTypeName:   "Address",
								MessageSourcePath: "common/common.proto",
							},
							{
								Kind:           FieldKindEnum,
								EnumTypeName:   "Status",
								EnumSourcePath: "common/common.proto",
							},
						},
					},
				},
			},
			want: []tsImportEntry{
				{
					path:  "./common/common.type.js",
					names: []string{"Address", "AddressSchema", "Status", "StatusSchema"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectTSImports(tt.ir, &Options{})
			if len(got) != len(tt.want) {
				t.Fatalf("got %d imports, want %d", len(got), len(tt.want))
			}
			for i, w := range tt.want {
				if got[i].path != w.path {
					t.Errorf("[%d] got path %q, want %q", i, got[i].path, w.path)
				}
				if len(got[i].names) != len(w.names) {
					t.Fatalf("[%d] got names %v, want %v", i, got[i].names, w.names)
				}
				for j, n := range w.names {
					if got[i].names[j] != n {
						t.Errorf("[%d] got name[%d] = %q, want %q", i, j, got[i].names[j], n)
					}
				}
			}
		})
	}
}

func TestTsFieldIsRequired(t *testing.T) {
	opts := &Options{Validate: "true"} // Ensure opts.ValidateEnabled() is true

	fRequired := &DomainField{
		ValidateConstraints: &ValidateConstraints{Required: true},
	}
	if !tsFieldIsRequired(fRequired, opts) {
		t.Error("expected required field to be true")
	}

	fOptional := &DomainField{
		ValidateConstraints: &ValidateConstraints{Required: false},
	}
	if tsFieldIsRequired(fOptional, opts) {
		t.Error("expected optional field to be false")
	}

	fNoConstraints := &DomainField{}
	if tsFieldIsRequired(fNoConstraints, opts) {
		t.Error("expected field without constraints to be false")
	}

	// Disabled validate options
	optsDisabled := &Options{}
	if tsFieldIsRequired(fRequired, optsDisabled) {
		t.Error("expected field to be false when validation disabled")
	}
}

func TestOneofSuperRefineOutput(t *testing.T) {
	golden, err := os.ReadFile("../testdata/golden/ts/gen/user.type.ts")
	if err != nil {
		t.Fatalf("failed to read golden file: %v", err)
	}
	content := string(golden)

	if !strings.Contains(content, ".superRefine((data, ctx) => {") {
		t.Error("expected superRefine for oneof exclusivity")
	}
	if !strings.Contains(content, "contactMethodKeys.find") {
		t.Error("expected contactMethodKeys.find in path generation")
	}
}

func TestTsPlainType(t *testing.T) {
	opts := &Options{}
	optsBigInt := &Options{TSInt64Style: "bigint"}

	tests := []struct {
		name string
		f    *DomainField
		opts *Options
		want string
	}{
		{"Scalar string", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind}, opts, "string"},
		{"Scalar int32", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Int32Kind}, opts, "number"},
		{"Scalar bool", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.BoolKind}, opts, "boolean"},
		{"Scalar int64 default", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Int64Kind}, opts, "string"},
		{"Scalar int64 bigint", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Int64Kind}, optsBigInt, "bigint"},
		{"Message", &DomainField{Kind: FieldKindMessage, MessageTypeName: "MyMessage"}, opts, "MyMessage"},
		{"Enum", &DomainField{Kind: FieldKindEnum, EnumTypeName: "MyEnum"}, opts, "MyEnum"},
		{"Repeated", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, Repeated: true}, opts, "string[]"},
		{"Map", &DomainField{IsMap: true, MapValue: &MapTypeInfo{Kind: FieldKindScalar, ScalarKind: protoreflect.Int32Kind}}, opts, "Record<string, number>"},
		{"WKT Timestamp", &DomainField{Kind: FieldKindTimestamp}, opts, "string"},
		{"WKT Struct", &DomainField{Kind: FieldKindStruct}, opts, "Record<string, unknown>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tsPlainType(tt.f, tt.opts)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateTypeScript_Negative(t *testing.T) {
	t.Run("ts_types_only with validate", func(t *testing.T) {
		opts := &Options{
			TSTypesOnly: true,
			Validate:    "true",
		}
		err := generateTypeScript(nil, nil, opts)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "cannot be used with ts_types_only") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("unsupported backend", func(t *testing.T) {
		opts := &Options{
			Backend: "firestore",
		}
		err := generateTypeScript(nil, nil, opts)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "does not support backend") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}
