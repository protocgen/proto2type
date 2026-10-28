package generator

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var updateRustPostgresGolden = flag.Bool("update-rust-postgres-golden", false, "update rust postgres golden files")

func TestRustPostgresFieldTypeFromIR(t *testing.T) {
	tests := []struct {
		name  string
		field *DomainField
		want  string
	}{
		// Native 1D PostgreSQL arrays
		{"repeated string", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, Repeated: true}, "Vec<String>"},
		{"repeated int32", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Int32Kind, Repeated: true}, "Vec<i32>"},
		{"repeated int64", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Int64Kind, Repeated: true}, "Vec<i64>"},
		{"repeated uint32", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Uint32Kind, Repeated: true}, "Vec<i64>"},
		{"repeated bool", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.BoolKind, Repeated: true}, "Vec<bool>"},
		{"repeated float", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.FloatKind, Repeated: true}, "Vec<f32>"},
		{"repeated double", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.DoubleKind, Repeated: true}, "Vec<f64>"},
		{"repeated bytes", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.BytesKind, Repeated: true}, "Vec<Vec<u8>>"},
		{"repeated enum int", &DomainField{Kind: FieldKindEnum, EnumTypeName: "UserStatus", Repeated: true}, "Vec<i32>"},
		{"repeated enum string", &DomainField{Kind: FieldKindEnum, EnumTypeName: "UserStatus", EnumAsString: true, Repeated: true}, "Vec<String>"},
		{"repeated timestamp", &DomainField{Kind: FieldKindTimestamp, Repeated: true}, "Vec<DateTime<Utc>>"},
		{"repeated duration", &DomainField{Kind: FieldKindDuration, Repeated: true}, "Vec<i64>"},
		{"field mask singular", &DomainField{Kind: FieldKindFieldMask}, "Vec<String>"},

		// Complex repeated / map -> Option<String> (TEXT, never JSONB)
		{"repeated message", &DomainField{Kind: FieldKindMessage, MessageTypeName: "Tag", Repeated: true}, "Option<String>"},
		{"repeated field_mask", &DomainField{Kind: FieldKindFieldMask, Repeated: true}, "Option<String>"},
		{"map field", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, IsMap: true}, "Option<String>"},

		// Timestamps and durations
		{"timestamp singular", &DomainField{Kind: FieldKindTimestamp}, "DateTime<Utc>"},
		{"timestamp optional", &DomainField{Kind: FieldKindTimestamp, Optional: true}, "Option<DateTime<Utc>>"},
		{"duration singular", &DomainField{Kind: FieldKindDuration}, "i64"},
		{"duration optional", &DomainField{Kind: FieldKindDuration, Optional: true}, "Option<i64>"},

		// WKT dynamic types -> String (TEXT, never JSONB)
		{"struct", &DomainField{Kind: FieldKindStruct}, "String"},
		{"value", &DomainField{Kind: FieldKindValue}, "String"},
		{"list_value", &DomainField{Kind: FieldKindListValue}, "String"},
		{"empty", &DomainField{Kind: FieldKindEmpty}, "String"},
		{"any", &DomainField{Kind: FieldKindAny}, "String"},

		// Singular non-inlined message -> Option<String> (TEXT, never JSONB)
		{"message singular", &DomainField{Kind: FieldKindMessage, MessageTypeName: "Address"}, "Option<String>"},

		// Enums
		{"enum singular int", &DomainField{Kind: FieldKindEnum, EnumTypeName: "UserStatus"}, "i32"},
		{"enum optional int", &DomainField{Kind: FieldKindEnum, EnumTypeName: "UserStatus", Optional: true}, "Option<i32>"},
		{"enum singular string", &DomainField{Kind: FieldKindEnum, EnumTypeName: "UserStatus", EnumAsString: true}, "String"},
		{"enum optional string", &DomainField{Kind: FieldKindEnum, EnumTypeName: "UserStatus", EnumAsString: true, Optional: true}, "Option<String>"},

		// Wrappers
		{"wrapper string", &DomainField{Kind: FieldKindWrapperString}, "Option<String>"},
		{"wrapper int64", &DomainField{Kind: FieldKindWrapperInt64}, "Option<i64>"},
		{"wrapper bytes", &DomainField{Kind: FieldKindWrapperBytes}, "Option<Vec<u8>>"},

		// Scalars
		{"scalar string", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind}, "String"},
		{"scalar optional string", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, Optional: true}, "Option<String>"},
		{"scalar int32", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Int32Kind}, "i32"},
		{"scalar uint64", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Uint64Kind}, "i64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rustPostgresFieldTypeFromIR(tt.field)
			if got != tt.want {
				t.Errorf("rustPostgresFieldTypeFromIR(%s) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestGenerateRustPostgres_GoldenAndNoJSONB(t *testing.T) {
	fds := buildFileDescriptorSet(t)

	goldenCases := []struct {
		protoFile  string
		genFile    string
		goldenPath string
	}{
		{"user.proto", "user_postgres.type.rs", "../testdata/golden/rust/gen/user_postgres.type.rs"},
		{"complex.proto", "complex_postgres.type.rs", "../testdata/golden/rust/gen/complex_postgres.type.rs"},
		{"catalog.proto", "catalog_postgres.type.rs", "../testdata/golden/rust/gen/catalog_postgres.type.rs"},
		{"edge_cases.proto", "edge_cases_postgres.type.rs", "../testdata/golden/rust/gen/edge_cases_postgres.type.rs"},
	}

	for _, tc := range goldenCases {
		t.Run(tc.protoFile, func(t *testing.T) {
			gen := newPlugin(t, fds, []string{tc.protoFile})
			runner := NewRunner()
			opts := &Options{
				Lang:    "rust",
				Backend: "postgres",
			}
			for _, f := range gen.Files {
				if f.Generate && f.Desc.Path() == tc.protoFile {
					if err := runner.GenerateFile(gen, f, opts); err != nil {
						t.Fatalf("GenerateFile(%s): %v", tc.protoFile, err)
					}
				}
			}

			got := extractPluginOutput(t, gen, tc.genFile)

			// Ensure no JSONB / sqlx::types::Json or broken conversions appear
			forbiddenPatterns := []string{
				"sqlx::types::Json",
				".map(|j| j.0)",
				"::try_from(self.",
				"::try_from(v)",
				"epoch_ms_to_datetime",
				"datetime_to_epoch_ms",
				"InvalidTimestamp",
			}
			for _, pat := range forbiddenPatterns {
				if strings.Contains(got, pat) {
					t.Errorf("%s contains forbidden pattern %q:\n%s", tc.genFile, pat, got)
				}
			}

			goldenAbs, err := filepath.Abs(tc.goldenPath)
			if err != nil {
				t.Fatalf("filepath.Abs(%s): %v", tc.goldenPath, err)
			}
			if os.Getenv("UPDATE_GOLDEN") == "1" || *updateRustPostgresGolden {
				if err := os.WriteFile(goldenAbs, []byte(got), 0o644); err != nil {
					t.Fatalf("WriteFile(%s): %v", goldenAbs, err)
				}
			}
			wantBytes, err := os.ReadFile(goldenAbs)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", goldenAbs, err)
			}
			if got != string(wantBytes) {
				t.Errorf("%s does not match golden file %s.\nRun with UPDATE_GOLDEN=1 to update.", tc.genFile, tc.goldenPath)
			}
		})
	}
}

func TestGenerateRustPostgres_InlineNestedFlattening(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})

	var userFile *protogen.File
	for _, f := range gen.Files {
		if f.Desc.Path() == "user.proto" {
			userFile = f
			break
		}
	}
	if userFile == nil {
		t.Fatal("user.proto not found")
	}

	opts := &Options{
		Lang:    "rust",
		Backend: "postgres",
	}

	df, err := BuildDomainFile(userFile, opts)
	if err != nil {
		t.Fatalf("BuildDomainFile: %v", err)
	}
	for _, dm := range df.Messages {
		if dm.Name == "User" {
			for _, f := range dm.Fields {
				if f.Name == "address" {
					f.Inline = true
				}
			}
		}
	}

	g := gen.NewGeneratedFile("inline_user_postgres.type.rs", "")
	msgByName := indexDomainMessages(df.Messages)
	for _, dm := range df.Messages {
		generateRustPostgresMessage(g, dm, msgByName)
	}

	out := extractPluginOutput(t, gen, "inline_user_postgres.type.rs")
	for _, want := range []string{
		"pub address_street: String,",
		"pub address_city: String,",
		"pub address_state: String,",
		"pub address_zip: String,",
		"pub address_country: String,",
		"address: Some(Address {",
		"street: self.address_street.clone(),",
		"street: self.address_street,",
		"address_street: match &d.address { Some(v) => v.street.clone(), None => Default::default() },",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("inline_user_postgres.type.rs missing %q:\n%s", want, out)
		}
	}
}
