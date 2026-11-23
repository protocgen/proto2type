package generator

import (
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	proto2typepb "github.com/protocgen/proto2type/proto/proto2type"
)

func stage1aMsgOpts(tbl *proto2typepb.TableOptions) *descriptorpb.MessageOptions {
	mo := &descriptorpb.MessageOptions{}
	proto.SetExtension(mo, proto2typepb.E_Message, &proto2typepb.MessageOptions{Table: tbl})
	return mo
}

func stage1aColOpts(col *proto2typepb.ColumnOptions, fld *proto2typepb.FieldOptions) *descriptorpb.FieldOptions {
	fo := &descriptorpb.FieldOptions{}
	var fExt *proto2typepb.FieldOptions
	if fld != nil {
		fExt = proto.Clone(fld).(*proto2typepb.FieldOptions)
	} else {
		fExt = &proto2typepb.FieldOptions{}
	}
	fExt.Column = col
	proto.SetExtension(fo, proto2typepb.E_Field, fExt)
	return fo
}

func stage1aRawFieldExt(extNum protowire.Number, inner []byte) *descriptorpb.FieldOptions {
	fo := &descriptorpb.FieldOptions{}
	fo.ProtoReflect().SetUnknown(buildWireExtension(extNum, inner))
	return fo
}

func stage1aScalarField(name string, num int32, typ descriptorpb.FieldDescriptorProto_Type, repeated bool, opts *descriptorpb.FieldOptions) *descriptorpb.FieldDescriptorProto {
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	if repeated {
		label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	}
	return &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(name),
		Number:   proto.Int32(num),
		Label:    label.Enum(),
		Type:     typ.Enum(),
		JsonName: proto.String(name),
		Options:  opts,
	}
}

func stage1aSingleMsgFDP(fileName, pkg, goPkg string, msg *descriptorpb.DescriptorProto) *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Syntax:      proto.String("proto3"),
		Name:        proto.String(fileName),
		Package:     proto.String(pkg),
		Options:     &descriptorpb.FileOptions{GoPackage: proto.String(goPkg)},
		MessageType: []*descriptorpb.DescriptorProto{msg},
	}
}

func stage1aGeneratedFiles(t *testing.T, gen *protogen.Plugin) map[string]string {
	t.Helper()
	resp := gen.Response()
	if resp.Error != nil && *resp.Error != "" {
		t.Fatalf("unexpected plugin response error: %s", *resp.Error)
	}
	out := make(map[string]string, len(resp.File))
	for _, f := range resp.File {
		name := f.GetName()
		if _, dup := out[name]; dup {
			t.Fatalf("duplicate generated file %q", name)
		}
		out[name] = f.GetContent()
	}
	return out
}

func stage1aFindBySuffix(t *testing.T, files map[string]string, suffix string) string {
	t.Helper()
	for name, content := range files {
		if strings.HasSuffix(name, suffix) {
			return content
		}
	}
	t.Fatalf("missing generated file ending in %q (got keys: %v)", suffix, files)
	return ""
}

// TestStage1A_HasDorisSpecificFields_SkipsPostgresOnlyTables verifies that
// messages annotated only for PostgreSQL (table_name, primary_key_fields, indexes,
// checks, singleton options, default_value: "", sql_type) are not falsely claimed
// as Doris lakehouse tables.
func TestStage1A_HasDorisSpecificFields_SkipsPostgresOnlyTables(t *testing.T) {
	pgOnlyMsg := &descriptorpb.DescriptorProto{
		Name: proto.String("PostgresOnlyEntity"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:          "postgres_only_entities",
			PrimaryKeyFields:   []string{"id"},
			IsSingleton:        true,
			SingletonCheckName: proto.String("pg_singleton"),
			SingletonCheckExpr: proto.String("id = 'singleton'"),
			Checks: []*proto2typepb.CheckDef{
				{Name: "chk_positive", Expr: "(retry_count >= 0)"},
			},
			Indexes: []*proto2typepb.IndexDef{
				{
					Name: "idx_pg_only_parts",
					Parts: []*proto2typepb.IndexDef_IndexPart{
						{Column: "label", Opclass: "gin_trgm_ops"},
					},
				},
			},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{SqlType: "UUID"}, &proto2typepb.FieldOptions{PrimaryKey: true})),
			stage1aScalarField("label", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DefaultValue: proto.String(""), SqlType: "TEXT"}, nil)),
			stage1aScalarField("retry_count", 3, descriptorpb.FieldDescriptorProto_TYPE_INT32, false, nil),
		},
	}

	dorisTableMsg := &descriptorpb.DescriptorProto{
		Name: proto.String("DorisNullableEntity"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName: "doris_nullable_entities",
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
			stage1aScalarField("evaluated_at", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DorisNullable: proto.Bool(true)}, nil)),
		},
	}

	fdp := &descriptorpb.FileDescriptorProto{
		Syntax:      proto.String("proto3"),
		Name:        proto.String("mixed.proto"),
		Package:     proto.String("stage1a.v1"),
		Options:     &descriptorpb.FileOptions{GoPackage: proto.String("example.com/stage1a/v1;stage1av1")},
		MessageType: []*descriptorpb.DescriptorProto{pgOnlyMsg, dorisTableMsg},
	}
	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"mixed.proto"})
	file := gen.Files[0]

	pgMeta := extractDorisTableMeta(file.Messages[0])
	if pgMeta != nil && pgMeta.hasDorisSpecificFields() {
		t.Fatalf("expected hasDorisSpecificFields()=false for Postgres-only table, got meta=%+v", pgMeta)
	}
	dorisMeta := extractDorisTableMeta(file.Messages[1])
	if dorisMeta == nil || dorisMeta.hasDorisSpecificFields() {
		// TableName alone has no table-level Doris field; column-level doris_nullable triggers inclusion.
		t.Logf("table-level dorisMeta=%+v", dorisMeta)
	}

	tables, err := buildDorisTablesForFile(file, &Options{Lang: "go", Backend: "doris", DorisMode: "go"})
	if err != nil {
		t.Fatalf("buildDorisTablesForFile failed: %v", err)
	}
	if len(tables) != 1 || tables[0].Descriptor.TableName != "doris_nullable_entities" {
		t.Fatalf("expected only doris_nullable_entities, got %+v", tables)
	}
	if len(tables[0].Descriptor.Columns) != 2 {
		t.Fatalf("expected unannotated sibling column 'id' to be retained alongside doris_nullable, got %+v", tables[0].Descriptor.Columns)
	}
}

// TestStage1A_PostgresEmptyStringDefault_HasDefaultValue covers default_value: ""
// via extension 1307 and legacy wire extension 50200 across DDL, Atlas HCL,
// ParsePostgresAtlasHCL round-trips, schema evolution linting, and validation.
func TestStage1A_PostgresEmptyStringDefault_HasDefaultValue(t *testing.T) {
	var legacyEmptyWire []byte
	legacyEmptyWire = protowire.AppendTag(legacyEmptyWire, 10, protowire.BytesType)
	legacyEmptyWire = protowire.AppendString(legacyEmptyWire, "")

	msg := &descriptorpb.DescriptorProto{
		Name: proto.String("EmptyDefaults"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName: "empty_defaults",
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
			stage1aScalarField("typed_empty", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DefaultValue: proto.String("")}, nil)),
			stage1aScalarField("wire_empty", 3, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aRawFieldExt(50200, legacyEmptyWire)),
			stage1aScalarField("no_default", 4, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{}, nil)),
		},
	}

	fdp := stage1aSingleMsgFDP("empty_defaults.proto", "stage1a.v1", "example.com/stage1a/v1;stage1av1", msg)
	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"empty_defaults.proto"})
	opts := &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl+hcl"}
	if err := NewRunner().GenerateFile(gen, gen.Files[0], opts); err != nil {
		t.Fatalf("GenerateFile(ddl+hcl) failed: %v", err)
	}

	files := stage1aGeneratedFiles(t, gen)
	sqlOut := stage1aFindBySuffix(t, files, "empty_defaults_postgres.sql")
	hclOut := stage1aFindBySuffix(t, files, "empty_defaults_postgres.hcl")

	for _, col := range []string{"typed_empty", "wire_empty"} {
		wantDDL := `"` + col + `" TEXT NOT NULL DEFAULT ''`
		if !strings.Contains(sqlOut, wantDDL) {
			t.Errorf("DDL missing %q in:\n%s", wantDDL, sqlOut)
		}
		wantHCLBlock := `column "` + col + `" {`
		if !strings.Contains(hclOut, wantHCLBlock) || !strings.Contains(hclOut, `default = ""`) {
			t.Errorf("HCL missing default = \"\" for %s in:\n%s", col, hclOut)
		}
	}
	if strings.Contains(sqlOut, `"no_default" TEXT NOT NULL DEFAULT`) {
		t.Errorf("no_default must not emit DEFAULT in DDL:\n%s", sqlOut)
	}

	// Unannotated ColumnOptions{} must keep HasDefaultValue == false.
	noDefMeta := extractColumnMeta(gen.Files[0].Messages[0].Fields[3])
	if noDefMeta != nil && noDefMeta.HasDefaultValue {
		t.Errorf("expected HasDefaultValue=false for nil DefaultValue, got true")
	}

	// Round-trip HCL -> ParsePostgresAtlasHCL preserves HasDefaultValue=true, DefaultValue="".
	parsed, err := ParsePostgresAtlasHCL(hclOut)
	if err != nil {
		t.Fatalf("ParsePostgresAtlasHCL failed: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 parsed table, got %d", len(parsed))
	}
	colByName := make(map[string]PostgresColumnDescriptor)
	for _, c := range parsed[0].Columns {
		colByName[c.ColumnName] = c
	}
	for _, col := range []string{"typed_empty", "wire_empty"} {
		got := colByName[col]
		if !got.HasDefaultValue || got.DefaultValue != "" {
			t.Errorf("parsed %s = {HasDefaultValue:%v, DefaultValue:%q}, want {true, \"\"}", col, got.HasDefaultValue, got.DefaultValue)
		}
	}
	if got := colByName["no_default"]; got.HasDefaultValue {
		t.Errorf("parsed no_default unexpectedly had HasDefaultValue=true: %+v", got)
	}

	// Lakehouse HCL + DDL + PG005 + ALTER TABLE round-trip.
	lhDesc := parsed[0]
	lhHCL := lhDesc.BuildPostgresAtlasHCL("")
	if strings.Count(lhHCL, `default = ""`) != 2 {
		t.Errorf("expected 2 `default = \"\"` lines in lakehouse HCL:\n%s", lhHCL)
	}
	if stmts, err := BuildPostgresSchemaMigrationSQL(parsed, parsed, ""); err != nil || len(stmts) != 0 {
		t.Errorf("expected zero migration drift on round-tripped empty defaults, got (%v, %v)", stmts, err)
	}

	before := PostgresTableDescriptor{
		TableName:         "empty_defaults",
		DatabaseScope:     "PUBLIC",
		PrimaryKeyColumns: []string{"id"},
		Columns: []PostgresColumnDescriptor{
			{ColumnName: "id", PostgresType: "TEXT", Nullable: false, IsPrimaryKey: true},
			{ColumnName: "typed_empty", PostgresType: "TEXT", Nullable: false},
		},
	}
	afterAddColWithEmptyDefault := before
	afterAddColWithEmptyDefault.Columns = append([]PostgresColumnDescriptor(nil), before.Columns...)
	afterAddColWithEmptyDefault.Columns = append(afterAddColWithEmptyDefault.Columns, PostgresColumnDescriptor{
		ColumnName:      "added_note",
		PostgresType:    "TEXT",
		Nullable:        false,
		HasDefaultValue: true,
		DefaultValue:    "",
	})
	if v := LintPostgresTableEvolution(before, afterAddColWithEmptyDefault); len(v) != 0 {
		t.Errorf("expected 0 violations when adding NOT NULL column with HasDefaultValue=true, got %+v", v)
	}
	afterSetDefault := before
	afterSetDefault.Columns = []PostgresColumnDescriptor{
		before.Columns[0],
		{ColumnName: "typed_empty", PostgresType: "TEXT", Nullable: false, HasDefaultValue: true, DefaultValue: ""},
	}
	alters, err := before.BuildPostgresAlterTableSQL(afterSetDefault, "")
	if err != nil {
		t.Fatalf("BuildPostgresAlterTableSQL(set empty default) failed: %v", err)
	}
	if len(alters) != 1 || alters[0] != "ALTER TABLE public.empty_defaults ALTER COLUMN typed_empty SET DEFAULT '';" {
		t.Errorf("unexpected SET DEFAULT alters: %v", alters)
	}
	dropAlters, err := afterSetDefault.BuildPostgresAlterTableSQL(before, "")
	if err != nil {
		t.Fatalf("BuildPostgresAlterTableSQL(drop empty default) failed: %v", err)
	}
	if len(dropAlters) != 1 || dropAlters[0] != "ALTER TABLE public.empty_defaults ALTER COLUMN typed_empty DROP DEFAULT;" {
		t.Errorf("unexpected DROP DEFAULT alters: %v", dropAlters)
	}

	// Reject empty default_value on integer, boolean, and identity primary key columns.
	invalidCases := []struct {
		name string
		fld  *descriptorpb.FieldDescriptorProto
	}{
		{
			name: "integer column",
			fld: stage1aScalarField("count", 2, descriptorpb.FieldDescriptorProto_TYPE_INT32, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DefaultValue: proto.String("")}, nil)),
		},
		{
			name: "boolean column",
			fld: stage1aScalarField("active", 2, descriptorpb.FieldDescriptorProto_TYPE_BOOL, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DefaultValue: proto.String("")}, nil)),
		},
		{
			name: "identity primary key",
			fld: stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_INT64, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DefaultValue: proto.String(""), Identity: true, PrimaryKey: true}, &proto2typepb.FieldOptions{PrimaryKey: true})),
		},
	}
	for _, tc := range invalidCases {
		badFields := []*descriptorpb.FieldDescriptorProto{tc.fld}
		if tc.name != "identity primary key" {
			badFields = append([]*descriptorpb.FieldDescriptorProto{
				stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
					stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
			}, badFields...)
		}
		badMsg := &descriptorpb.DescriptorProto{
			Name:    proto.String("BadDefault"),
			Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: "bad_defaults"}),
			Field:   badFields,
		}
		badFDP := stage1aSingleMsgFDP("bad.proto", "stage1a.v1", "example.com/stage1a/v1;stage1av1", badMsg)
		badGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{badFDP}}, []string{"bad.proto"})
		if err := NewRunner().GenerateFile(badGen, badGen.Files[0], opts); err == nil {
			t.Errorf("expected error for empty default_value on %s, got nil", tc.name)
		}
	}
}

// TestStage1A_PostgresArrayDefaults_HCLAndDDLRoundTrip verifies unquoted sql("'<val>'::<type>[]")
// formatting in Atlas HCL and clean round-tripping through ParsePostgresAtlasHCL.
func TestStage1A_PostgresArrayDefaults_HCLAndDDLRoundTrip(t *testing.T) {
	msg := &descriptorpb.DescriptorProto{
		Name: proto.String("ArrayDefaultTable"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName: "array_default_tables",
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{SqlType: "UUID"}, &proto2typepb.FieldOptions{PrimaryKey: true})),
			stage1aScalarField("tags", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, true,
				stage1aColOpts(&proto2typepb.ColumnOptions{DefaultValue: proto.String("{}")}, nil)),
			stage1aScalarField("ref_ids", 3, descriptorpb.FieldDescriptorProto_TYPE_STRING, true,
				stage1aColOpts(&proto2typepb.ColumnOptions{SqlType: "UUID", DefaultValue: proto.String("{}")}, nil)),
		},
	}

	fdp := stage1aSingleMsgFDP("array_defaults.proto", "stage1a.v1", "example.com/stage1a/v1;stage1av1", msg)
	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"array_defaults.proto"})
	opts := &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl+hcl"}
	if err := NewRunner().GenerateFile(gen, gen.Files[0], opts); err != nil {
		t.Fatalf("GenerateFile failed: %v", err)
	}
	files := stage1aGeneratedFiles(t, gen)
	sqlOut := stage1aFindBySuffix(t, files, "array_defaults_postgres.sql")
	hclOut := stage1aFindBySuffix(t, files, "array_defaults_postgres.hcl")

	for _, wantSQL := range []string{
		`"tags" TEXT[] NOT NULL DEFAULT '{}'`,
		`"ref_ids" UUID[] NOT NULL DEFAULT '{}'`,
	} {
		if !strings.Contains(sqlOut, wantSQL) {
			t.Errorf("DDL missing %q in:\n%s", wantSQL, sqlOut)
		}
	}
	for _, wantHCL := range []string{
		`default = sql("'{}'::text[]")`,
		`default = sql("'{}'::uuid[]")`,
	} {
		if !strings.Contains(hclOut, wantHCL) {
			t.Errorf("HCL missing %q in:\n%s", wantHCL, hclOut)
		}
	}

	parsed, err := ParsePostgresAtlasHCL(hclOut)
	if err != nil {
		t.Fatalf("ParsePostgresAtlasHCL failed: %v", err)
	}
	df, err := BuildDomainFile(gen.Files[0], opts)
	if err != nil {
		t.Fatalf("BuildDomainFile failed: %v", err)
	}
	relSchemas, err := collectPostgresTableSchemas(df, opts)
	if err != nil {
		t.Fatalf("collectPostgresTableSchemas failed: %v", err)
	}
	wantDescs := make([]PostgresTableDescriptor, 0, len(relSchemas))
	for _, rs := range relSchemas {
		wantDescs = append(wantDescs, PostgresTableDescriptor{
			TableName:         rs.TableName,
			MessageName:       rs.GoName,
			DatabaseScope:     "TENANT",
			WriterService:     rs.WriterService,
			ReaderServices:    rs.ReaderServices,
			PrimaryKeyColumns: rs.PrimaryKeys,
			Columns:           rs.Columns,
			Indexes:           rs.Indexes,
			ForeignKeys:       rs.ForeignKeys,
			Checks:            rs.Checks,
			IsSingleton:       rs.IsSingleton,
		})
	}
	if stmts, err := BuildPostgresSchemaMigrationSQL(parsed, wantDescs, "acme"); err != nil || len(stmts) != 0 {
		t.Fatalf("expected zero migration drift against proto-derived descriptor for array sql(...) defaults, got (%v, %v)", stmts, err)
	}
	if stmts, err := BuildPostgresSchemaMigrationSQL(wantDescs, parsed, "acme"); err != nil || len(stmts) != 0 {
		t.Fatalf("expected zero reverse migration drift against proto-derived descriptor for array sql(...) defaults, got (%v, %v)", stmts, err)
	}
	lhHCL := parsed[0].BuildPostgresAtlasHCL("")
	if !strings.Contains(lhHCL, `default = sql("'{}'::text[]")`) || !strings.Contains(lhHCL, `default = sql("'{}'::uuid[]")`) {
		t.Errorf("lakehouse BuildPostgresAtlasHCL lost sql(...) array default formatting:\n%s", lhHCL)
	}
}

// TestStage1A_RepeatedExplicitSQLType verifies that repeated fields with an
// explicit sql_type append [] once unless already [] or VECTOR(...), while still rejecting JSONB.
func TestStage1A_RepeatedExplicitSQLType(t *testing.T) {
	msg := &descriptorpb.DescriptorProto{
		Name:    proto.String("RepeatedTypes"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: "repeated_types"}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{SqlType: "UUID"}, &proto2typepb.FieldOptions{PrimaryKey: true})),
			stage1aScalarField("tag_ids", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, true,
				stage1aColOpts(&proto2typepb.ColumnOptions{SqlType: "UUID"}, nil)),
			stage1aScalarField("codes", 3, descriptorpb.FieldDescriptorProto_TYPE_STRING, true,
				stage1aColOpts(&proto2typepb.ColumnOptions{SqlType: "TEXT[]"}, nil)),
			stage1aScalarField("embedding", 4, descriptorpb.FieldDescriptorProto_TYPE_FLOAT, true,
				stage1aColOpts(&proto2typepb.ColumnOptions{SqlType: "VECTOR(768)"}, nil)),
		},
	}
	fdp := stage1aSingleMsgFDP("repeated_types.proto", "stage1a.v1", "example.com/stage1a/v1;stage1av1", msg)
	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"repeated_types.proto"})
	opts := &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl+hcl"}
	if err := NewRunner().GenerateFile(gen, gen.Files[0], opts); err != nil {
		t.Fatalf("GenerateFile failed: %v", err)
	}
	files := stage1aGeneratedFiles(t, gen)
	sqlOut := stage1aFindBySuffix(t, files, "repeated_types_postgres.sql")
	hclOut := stage1aFindBySuffix(t, files, "repeated_types_postgres.hcl")

	for _, wantSQL := range []string{
		`"tag_ids" UUID[] NOT NULL`,
		`"codes" TEXT[] NOT NULL`,
		`"embedding" VECTOR(768) NOT NULL`,
	} {
		if !strings.Contains(sqlOut, wantSQL) {
			t.Errorf("DDL missing %q in:\n%s", wantSQL, sqlOut)
		}
	}
	if strings.Contains(sqlOut, "TEXT[][]") || strings.Contains(sqlOut, "VECTOR(768)[]") {
		t.Errorf("DDL unexpectedly double-arrayed type:\n%s", sqlOut)
	}
	for _, wantHCL := range []string{`type = sql("uuid[]")`, `type = sql("text[]")`, `type = sql("vector(768)")`} {
		if !strings.Contains(hclOut, wantHCL) {
			t.Errorf("HCL missing %q in:\n%s", wantHCL, hclOut)
		}
	}

	// JSONB on a repeated field must remain forbidden.
	badMsg := &descriptorpb.DescriptorProto{
		Name:    proto.String("BadRepeatedJSONB"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: "bad_repeated_jsonb"}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
			stage1aScalarField("blobs", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, true,
				stage1aColOpts(&proto2typepb.ColumnOptions{SqlType: "JSONB"}, nil)),
		},
	}
	badFDP := stage1aSingleMsgFDP("bad_jsonb.proto", "stage1a.v1", "example.com/stage1a/v1;stage1av1", badMsg)
	badGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{badFDP}}, []string{"bad_jsonb.proto"})
	if err := NewRunner().GenerateFile(badGen, badGen.Files[0], opts); err == nil {
		t.Errorf("expected error for repeated field with sql_type=JSONB, got nil")
	}
}

// TestStage1A_PostgresIndexParts_OrderedExprColumnOpclassDesc covers ordered
// IndexDef.parts across typed ext 1307, raw wire ext 50201, DDL, Atlas HCL,
// ParsePostgresAtlasHCL round-trip, and validation.
func TestStage1A_PostgresIndexParts_OrderedExprColumnOpclassDesc(t *testing.T) {
	msg := &descriptorpb.DescriptorProto{
		Name: proto.String("OrderedIndexEntity"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName: "ordered_index_entities",
			Indexes: []*proto2typepb.IndexDef{
				{
					Name:  "idx_ordered_mixed",
					Where: "deleted_at IS NULL",
					Parts: []*proto2typepb.IndexDef_IndexPart{
						{Expr: "lower(title)", Opclass: "text_pattern_ops"},
						{Column: "created_at", Desc: true},
						{Column: "tenant_id"},
					},
				},
			},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
			stage1aScalarField("tenant_id", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
			stage1aScalarField("title", 3, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
			stage1aScalarField("created_at", 4, descriptorpb.FieldDescriptorProto_TYPE_INT64, false, nil),
			stage1aScalarField("deleted_at", 5, descriptorpb.FieldDescriptorProto_TYPE_INT64, false, nil),
		},
	}

	fdp := stage1aSingleMsgFDP("idx_parts.proto", "stage1a.v1", "example.com/stage1a/v1;stage1av1", msg)
	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"idx_parts.proto"})
	opts := &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl+hcl"}
	if err := NewRunner().GenerateFile(gen, gen.Files[0], opts); err != nil {
		t.Fatalf("GenerateFile failed: %v", err)
	}
	files := stage1aGeneratedFiles(t, gen)
	sqlOut := stage1aFindBySuffix(t, files, "idx_parts_postgres.sql")
	hclOut := stage1aFindBySuffix(t, files, "idx_parts_postgres.hcl")

	wantSQL := `CREATE INDEX IF NOT EXISTS "idx_ordered_mixed" ON "ordered_index_entities" ((lower(title)) text_pattern_ops, "created_at" DESC, "tenant_id") WHERE deleted_at IS NULL;`
	if !strings.Contains(sqlOut, wantSQL) {
		t.Errorf("DDL missing ordered IndexPart clause %q in:\n%s", wantSQL, sqlOut)
	}

	// Verify HCL has three ordered on blocks with ops = text_pattern_ops and desc = true.
	for _, wantHCL := range []string{
		`expr = "lower(title)"`,
		`ops    = text_pattern_ops`,
		`column = column.created_at`,
		`desc   = true`,
		`column = column.tenant_id`,
	} {
		if !strings.Contains(hclOut, wantHCL) {
			t.Errorf("HCL missing %q in:\n%s", wantHCL, hclOut)
		}
	}

	// Parse back and verify Parts order and zero migration drift (also accept `opclass = ...` alias).
	parsed, err := ParsePostgresAtlasHCL(hclOut)
	if err != nil {
		t.Fatalf("ParsePostgresAtlasHCL failed: %v", err)
	}
	if len(parsed) != 1 || len(parsed[0].Indexes) != 1 {
		t.Fatalf("expected 1 table with 1 index, got %+v", parsed)
	}
	wantParts := []PostgresIndexPartDescriptor{
		{Expr: "lower(title)", Opclass: "text_pattern_ops"},
		{Column: "created_at", Desc: true},
		{Column: "tenant_id"},
	}
	if !reflect.DeepEqual(parsed[0].Indexes[0].Parts, wantParts) {
		t.Fatalf("parsed Index.Parts = %+v, want %+v", parsed[0].Indexes[0].Parts, wantParts)
	}
	if stmts, err := BuildPostgresSchemaMigrationSQL(parsed, parsed, ""); err != nil || len(stmts) != 0 {
		t.Fatalf("expected zero migration drift for ordered Index.Parts, got (%v, %v)", stmts, err)
	}

	// Test raw wire parser on ext 1307 (MessageOptions.table tag 2) -> IndexDef tag 20 (IndexPart tag 7, tags 1..4).
	var part1Wire, part2Wire, idxWire, tblWire, msgOptsWire []byte
	part1Wire = protowire.AppendTag(part1Wire, 2, protowire.BytesType)
	part1Wire = protowire.AppendString(part1Wire, "lower(title)")
	part1Wire = protowire.AppendTag(part1Wire, 3, protowire.BytesType)
	part1Wire = protowire.AppendString(part1Wire, "text_pattern_ops")
	part2Wire = protowire.AppendTag(part2Wire, 1, protowire.BytesType)
	part2Wire = protowire.AppendString(part2Wire, "created_at")
	part2Wire = protowire.AppendTag(part2Wire, 4, protowire.VarintType)
	part2Wire = protowire.AppendVarint(part2Wire, 1)
	idxWire = protowire.AppendTag(idxWire, 1, protowire.BytesType)
	idxWire = protowire.AppendString(idxWire, "idx_wire_parts")
	idxWire = protowire.AppendTag(idxWire, 7, protowire.BytesType)
	idxWire = protowire.AppendBytes(idxWire, part1Wire)
	idxWire = protowire.AppendTag(idxWire, 7, protowire.BytesType)
	idxWire = protowire.AppendBytes(idxWire, part2Wire)
	tblWire = protowire.AppendTag(tblWire, 1, protowire.BytesType)
	tblWire = protowire.AppendString(tblWire, "wire_parts_tbl")
	tblWire = protowire.AppendTag(tblWire, 20, protowire.BytesType)
	tblWire = protowire.AppendBytes(tblWire, idxWire)
	msgOptsWire = protowire.AppendTag(msgOptsWire, 2, protowire.BytesType)
	msgOptsWire = protowire.AppendBytes(msgOptsWire, tblWire)
	parsedWire := parseDorisTableMetaFromOptionsWire(buildWireExtension(1307, msgOptsWire))
	if parsedWire == nil || len(parsedWire.Indexes) != 1 || len(parsedWire.Indexes[0].Parts) != 2 {
		t.Fatalf("expected 2 parsed wire IndexPartIR entries, got %+v", parsedWire)
	}
	if parsedWire.Indexes[0].Parts[0] != (IndexPartIR{Expr: "lower(title)", Opclass: "text_pattern_ops"}) ||
		parsedWire.Indexes[0].Parts[1] != (IndexPartIR{Column: "created_at", Desc: true}) {
		t.Fatalf("unexpected wire IndexPartIR entries: %+v", parsedWire.Indexes[0].Parts)
	}

	// Reject malformed parts: both column & expr set, unknown column, injection in opclass.
	invalidParts := [][]PostgresIndexPartDescriptor{
		{{Column: "title", Expr: "lower(title)"}},
		{{}},
		{{Column: "nonexistent_col"}},
		{{Expr: "lower(title)", Opclass: "bad; DROP TABLE x"}},
	}
	for i, parts := range invalidParts {
		desc := PostgresIndexDescriptor{Name: "idx_bad", Parts: parts}
		protoParts := make([]*proto2typepb.IndexDef_IndexPart, 0, len(parts))
		for _, p := range parts {
			protoParts = append(protoParts, &proto2typepb.IndexDef_IndexPart{
				Column: p.Column, Expr: p.Expr, Opclass: p.Opclass, Desc: p.Desc,
			})
		}
		badMsg := proto.Clone(msg).(*descriptorpb.DescriptorProto)
		badMsg.Options = stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName: "ordered_index_entities",
			Indexes:   []*proto2typepb.IndexDef{{Name: desc.Name, Parts: protoParts}},
		})
		badFDP := stage1aSingleMsgFDP("bad_parts.proto", "stage1a.v1", "example.com/stage1a/v1;stage1av1", badMsg)
		badGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{badFDP}}, []string{"bad_parts.proto"})
		if err := NewRunner().GenerateFile(badGen, badGen.Files[0], opts); err == nil {
			t.Errorf("case %d: expected validation error for invalid IndexPart %+v, got nil", i, parts)
		}
	}
}

// TestStage1A_SingletonCheckAndNamedChecks covers singleton_check_name/expr
// (ext 1307 & wire 50201 tags 24/25) and preserving explicit non-empty CheckDef.Name.
func TestStage1A_SingletonCheckAndNamedChecks(t *testing.T) {
	msg := &descriptorpb.DescriptorProto{
		Name: proto.String("CustomSingleton"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:          "custom_singleton",
			IsSingleton:        true,
			SingletonCheckName: proto.String("tenant_cfg_singleton"),
			SingletonCheckExpr: proto.String("id = 42"),
			Checks: []*proto2typepb.CheckDef{
				{Name: "valid_status_range", Expr: "(status >= 0)"},
			},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_INT32, false,
				stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
			stage1aScalarField("status", 2, descriptorpb.FieldDescriptorProto_TYPE_INT32, false, nil),
		},
	}

	fdp := stage1aSingleMsgFDP("singleton_chk.proto", "stage1a.v1", "example.com/stage1a/v1;stage1av1", msg)
	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"singleton_chk.proto"})
	opts := &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl+hcl"}
	if err := NewRunner().GenerateFile(gen, gen.Files[0], opts); err != nil {
		t.Fatalf("GenerateFile failed: %v", err)
	}
	files := stage1aGeneratedFiles(t, gen)
	sqlOut := stage1aFindBySuffix(t, files, "singleton_chk_postgres.sql")
	hclOut := stage1aFindBySuffix(t, files, "singleton_chk_postgres.hcl")

	for _, wantSQL := range []string{
		`CONSTRAINT "tenant_cfg_singleton" CHECK (id = 42)`,
		`CONSTRAINT "valid_status_range" CHECK ((status >= 0))`,
	} {
		if !strings.Contains(sqlOut, wantSQL) {
			t.Errorf("DDL missing %q in:\n%s", wantSQL, sqlOut)
		}
	}
	if strings.Contains(sqlOut, `"chk_valid_status_range"`) {
		t.Errorf("explicit check name should not be prefixed with chk_:\n%s", sqlOut)
	}
	for _, wantHCL := range []string{
		`check "tenant_cfg_singleton"`,
		`expr = "id = 42"`,
		`check "valid_status_range"`,
		`expr = "(status >= 0)"`,
	} {
		if !strings.Contains(hclOut, wantHCL) {
			t.Errorf("HCL missing %q in:\n%s", wantHCL, hclOut)
		}
	}

	// Verify wire parsing for tags 24 & 25 on ext 1307 (MessageOptions.table tag 2 -> TableOptions).
	var tblWire, msgOptsWire []byte
	tblWire = protowire.AppendTag(tblWire, 1, protowire.BytesType)
	tblWire = protowire.AppendString(tblWire, "wire_singleton")
	tblWire = protowire.AppendTag(tblWire, 24, protowire.BytesType)
	tblWire = protowire.AppendString(tblWire, "wire_singleton_chk")
	tblWire = protowire.AppendTag(tblWire, 25, protowire.BytesType)
	tblWire = protowire.AppendString(tblWire, "id = 99")
	msgOptsWire = protowire.AppendTag(msgOptsWire, 2, protowire.BytesType)
	msgOptsWire = protowire.AppendBytes(msgOptsWire, tblWire)
	wireMeta := parseDorisTableMetaFromOptionsWire(buildWireExtension(1307, msgOptsWire))
	if wireMeta == nil || wireMeta.SingletonCheckName != "wire_singleton_chk" || wireMeta.SingletonCheckExpr != "id = 99" {
		t.Fatalf("unexpected wire singleton check metadata: %+v", wireMeta)
	}
}

// TestStage1A_DorisIcebergParityFixes verifies UNIQUE KEY partition omission,
// Iceberg type normalization (DATETIME/TINYINT/SMALLINT/ARRAY<VARCHAR(N)>),
// ${TENANT_BUCKETS} placeholder emission, and doris_nullable overrides.
func TestStage1A_DorisIcebergParityFixes(t *testing.T) {
	// 1. dorisColumnIcebergDDLType mappings
	iceCases := []struct {
		dorisType   string
		icebergType string
		want        string
	}{
		{"DATETIME(6)", "", "DATETIME"},
		{"DATETIMEV2(3)", "", "DATETIME"},
		{"TINYINT", "", "INT"},
		{"SMALLINT", "", "INT"},
		{"ARRAY<TINYINT>", "", "ARRAY<INT>"},
		{"ARRAY<SMALLINT>", "", "ARRAY<INT>"},
		{"ARRAY<VARCHAR(64)>", "ARRAY<VARCHAR(64)>", "ARRAY<VARCHAR(64)>"},
		{"ARRAY<VARCHAR(64)>", "", "ARRAY<VARCHAR(64)>"},
	}
	for _, tc := range iceCases {
		if got := dorisColumnIcebergDDLType(tc.dorisType, tc.icebergType); got != tc.want {
			t.Errorf("dorisColumnIcebergDDLType(%q, %q) = %q, want %q", tc.dorisType, tc.icebergType, got, tc.want)
		}
	}

	// 2. Partition omission on UNIQUE KEY when partition col != UK subset + doris_nullable + ${TENANT_BUCKETS}
	var legacyNullableFalse []byte
	legacyNullableFalse = protowire.AppendTag(legacyNullableFalse, 11, protowire.VarintType)
	legacyNullableFalse = protowire.AppendVarint(legacyNullableFalse, 0)

	firstSeenField := stage1aScalarField("first_seen_at", 5, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
		stage1aRawFieldExt(50200, legacyNullableFalse))
	firstSeenField.Proto3Optional = proto.Bool(true)
	firstSeenField.OneofIndex = proto.Int32(0)

	plainOptField := stage1aScalarField("plain_optional", 7, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
		stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(256)"}, nil))
	plainOptField.Proto3Optional = proto.Bool(true)
	plainOptField.OneofIndex = proto.Int32(1)

	ukMsg := &descriptorpb.DescriptorProto{
		Name: proto.String("UniquePartitionGap"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:          "uk_partition_gap",
			DatabaseScope:      "TENANT",
			SyncTier:           "MONTHLY_PARTITIONED",
			UniqueKeyColumns:   []string{"patient_id", "item_id"},
			PartitionByColumns: []string{"event_month"},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("patient_id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)", IsUniqueKey: true}, nil)),
			stage1aScalarField("item_id", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)", IsUniqueKey: true}, nil)),
			stage1aScalarField("event_month", 3, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(7)", IsPartitionKey: true, DefaultValue: proto.String("1970-01")}, nil)),
			stage1aScalarField("evaluated_at", 4, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "DATETIMEV2(3)", DorisNullable: proto.Bool(true)}, nil)),
			firstSeenField,
			stage1aScalarField("tiny_rank", 6, descriptorpb.FieldDescriptorProto_TYPE_INT32, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "TINYINT"}, nil)),
			plainOptField,
		},
		OneofDecl: []*descriptorpb.OneofDescriptorProto{
			{Name: proto.String("_first_seen_at")},
			{Name: proto.String("_plain_optional")},
		},
	}

	fdp := stage1aSingleMsgFDP("doris_parity.proto", "stage1a.v1", "example.com/stage1a/v1;stage1av1", ukMsg)
	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"doris_parity.proto"})
	opts := &Options{Lang: "go", Backend: "doris", DorisMode: "all"}
	if err := NewRunner().GenerateFile(gen, gen.Files[0], opts); err != nil {
		t.Fatalf("GenerateFile(doris, all) failed: %v", err)
	}
	files := stage1aGeneratedFiles(t, gen)
	dorisSQL := stage1aFindBySuffix(t, files, "doris_parity_doris.sql")
	dorisHCL := stage1aFindBySuffix(t, files, "doris_parity_doris.hcl")
	iceSQL := stage1aFindBySuffix(t, files, "doris_parity_iceberg.sql")
	srHCL := stage1aFindBySuffix(t, files, "doris_parity_starrocks.hcl")

	if strings.Contains(dorisSQL, "PARTITION BY") {
		t.Errorf("Doris DDL must omit PARTITION BY when event_month is not in UNIQUE KEY:\n%s", dorisSQL)
	}
	if strings.Contains(dorisHCL, "partition_by_") {
		t.Errorf("Doris HCL must omit partition_by_* when event_month is not in UNIQUE KEY:\n%s", dorisHCL)
	}
	if !strings.Contains(iceSQL, "PARTITION BY LIST (event_month)") {
		t.Errorf("Iceberg DDL must retain PARTITION BY LIST (event_month):\n%s", iceSQL)
	}
	if !strings.Contains(iceSQL, "tiny_rank INT NOT NULL") {
		t.Errorf("Iceberg DDL should map TINYINT -> INT:\n%s", iceSQL)
	}
	if !strings.Contains(iceSQL, "evaluated_at DATETIME,") {
		t.Errorf("Iceberg DDL should map DATETIMEV2(3) -> DATETIME and preserve doris_nullable=true:\n%s", iceSQL)
	}
	if !strings.Contains(dorisHCL, `buckets = "${TENANT_BUCKETS}"`) {
		t.Errorf("Doris tenant HCL should emit ${TENANT_BUCKETS}:\n%s", dorisHCL)
	}
	if !strings.Contains(srHCL, `buckets = "${TENANT_BUCKETS}"`) {
		t.Errorf("StarRocks tenant HCL should emit ${TENANT_BUCKETS}:\n%s", srHCL)
	}
	if !strings.Contains(dorisSQL, "evaluated_at DATETIMEV2(3),") {
		t.Errorf("expected evaluated_at nullable via doris_nullable=true:\n%s", dorisSQL)
	}
	if !strings.Contains(dorisSQL, "first_seen_at VARCHAR(256) NOT NULL,") {
		t.Errorf("expected optional first_seen_at NOT NULL via legacy wire nullable=false:\n%s", dorisSQL)
	}
	if !strings.Contains(dorisSQL, "plain_optional VARCHAR(256)\n") || strings.Contains(dorisSQL, "plain_optional VARCHAR(256) NOT NULL") {
		t.Errorf("expected control optional field plain_optional to remain nullable:\n%s", dorisSQL)
	}
	// Verify partition IS emitted when partition col is a subset of unique_key_columns.
	ukSubsetMsg := proto.Clone(ukMsg).(*descriptorpb.DescriptorProto)
	ukSubsetMsg.Options = stage1aMsgOpts(&proto2typepb.TableOptions{
		TableName:          "uk_partition_subset",
		DatabaseScope:      "TENANT",
		SyncTier:           "MONTHLY_PARTITIONED",
		UniqueKeyColumns:   []string{"patient_id", "item_id", "event_month"},
		PartitionByColumns: []string{"event_month"},
	})
	subFDP := stage1aSingleMsgFDP("doris_subset.proto", "stage1a.v1", "example.com/stage1a/v1;stage1av1", ukSubsetMsg)
	subGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{subFDP}}, []string{"doris_subset.proto"})
	if err := NewRunner().GenerateFile(subGen, subGen.Files[0], &Options{Lang: "go", Backend: "doris", DorisMode: "ddl+hcl"}); err != nil {
		t.Fatalf("GenerateFile(doris subset) failed: %v", err)
	}
	subFiles := stage1aGeneratedFiles(t, subGen)
	if !strings.Contains(stage1aFindBySuffix(t, subFiles, "doris_subset_doris.sql"), "PARTITION BY") {
		t.Errorf("expected PARTITION BY when event_month is in unique_key_columns")
	}
	if !strings.Contains(stage1aFindBySuffix(t, subFiles, "doris_subset_doris.hcl"), "partition_by_") {
		t.Errorf("expected partition_by_* when event_month is in unique_key_columns")
	}
	parsedHCL, err := ParseDorisAtlasHCL(dorisHCL)
	if err != nil || len(parsedHCL) != 1 || parsedHCL[0].Buckets != 0 {
		t.Fatalf("ParseDorisAtlasHCL failed on ${TENANT_BUCKETS}: err=%v parsed=%+v", err, parsedHCL)
	}
	tablesIR, err := buildDorisTablesForFile(gen.Files[0], opts)
	if err != nil || len(tablesIR) != 1 {
		t.Fatalf("buildDorisTablesForFile failed: err=%v len=%d", err, len(tablesIR))
	}
	if stmts, err := BuildDorisSchemaMigrationSQL(parsedHCL, []DorisTableDescriptor{tablesIR[0].Descriptor}, "acme", 8); err != nil || len(stmts) != 0 {
		t.Fatalf("expected zero Doris migration drift when UK omits partition col, got (%v, %v)", stmts, err)
	}
}

// TestStage1A_OutputFileConsolidationAndFailFast verifies single-file consolidation
// across multiple proto files and fail-fast rejection of invalid multi-file combinations.
func TestStage1A_OutputFileConsolidationAndFailFast(t *testing.T) {
	msgA := &descriptorpb.DescriptorProto{
		Name: proto.String("User"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName: "users",
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{SqlType: "UUID"}, &proto2typepb.FieldOptions{PrimaryKey: true})),
		},
	}
	msgB := &descriptorpb.DescriptorProto{
		Name: proto.String("UserSession"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName: "user_sessions",
			ForeignKeys: []*proto2typepb.ForeignKeyDef{
				{Name: "fk_sessions_user", Columns: []string{"user_id"}, RefTable: "users", RefColumns: []string{"id"}},
			},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{SqlType: "UUID"}, &proto2typepb.FieldOptions{PrimaryKey: true})),
			stage1aScalarField("user_id", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{SqlType: "UUID"}, nil)),
		},
	}

	dorisMsgA := &descriptorpb.DescriptorProto{
		Name: proto.String("ConceptA"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:           "concepts_a",
			DatabaseScope:       "GLOBAL_TERMINOLOGY_RAG",
			DuplicateKeyColumns: []string{"id"},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"}, nil)),
		},
	}
	dorisMsgB := &descriptorpb.DescriptorProto{
		Name: proto.String("ConceptB"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:           "concepts_b",
			DatabaseScope:       "GLOBAL_TERMINOLOGY_RAG",
			DuplicateKeyColumns: []string{"id"},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"}, nil)),
		},
	}

	makeFDS := func(samePkg bool, doris bool) *descriptorpb.FileDescriptorSet {
		goPkgB := "example.com/multi/v1;multiv1"
		if !samePkg {
			goPkgB = "example.com/other/v1;otherv1"
		}
		if doris {
			return &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
				stage1aSingleMsgFDP("a.proto", "multi.v1", "example.com/multi/v1;multiv1", dorisMsgA),
				stage1aSingleMsgFDP("b.proto", "multi.v1", goPkgB, dorisMsgB),
			}}
		}
		return &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
			stage1aSingleMsgFDP("a.proto", "multi.v1", "example.com/multi/v1;multiv1", msgA),
			stage1aSingleMsgFDP("b.proto", "multi.v1", goPkgB, msgB),
		}}
	}

	// 1. Valid multi-proto consolidation modes
	validCases := []struct {
		name         string
		doris        bool
		opts         *Options
		wantContains []string
		verifyGo     bool
	}{
		{
			name:         "postgres hcl consolidation",
			opts:         &Options{Lang: "go", Backend: "postgres", PostgresMode: "hcl", OutputFile: "schema/all.hcl"},
			wantContains: []string{`table "users" {`, `table "user_sessions" {`, `foreign_key "fk_sessions_user"`},
		},
		{
			name:         "postgres ddl consolidation",
			opts:         &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl", OutputFile: "schema/all.sql"},
			wantContains: []string{`CREATE TABLE IF NOT EXISTS "users"`, `CREATE TABLE IF NOT EXISTS "user_sessions"`, `FOREIGN KEY ("user_id") REFERENCES "users" ("id")`},
		},
		{
			name:         "postgres go consolidation",
			opts:         &Options{Lang: "go", Backend: "postgres", PostgresMode: "go", OutputFile: "db/pg_all.go"},
			wantContains: []string{`package multiv1`, `func (x *UserPostgres) TableName() string`, `func (x *UserSessionPostgres) TableName() string`},
			verifyGo:     true,
		},
		{
			name:         "starrocks ddl consolidation",
			doris:        true,
			opts:         &Options{Lang: "go", Backend: "doris", DorisMode: "starrocks_ddl", OutputFile: "sr/all.sql"},
			wantContains: []string{`concepts_a`, `concepts_b`},
		},
		{
			name:         "starrocks hcl consolidation",
			doris:        true,
			opts:         &Options{Lang: "go", Backend: "doris", DorisMode: "starrocks_hcl", OutputFile: "sr/all.hcl"},
			wantContains: []string{`table "concepts_a"`, `table "concepts_b"`},
		},
		{
			name:         "doris go consolidation",
			doris:        true,
			opts:         &Options{Lang: "go", Backend: "doris", DorisMode: "go", OutputFile: "lake/all.go"},
			wantContains: []string{`package multiv1`, `func ConceptADorisTable() DorisTableDescriptor`, `func ConceptBDorisTable() DorisTableDescriptor`},
			verifyGo:     true,
		},
	}

	runner := NewRunner()
	for _, tc := range validCases {
		t.Run(tc.name, func(t *testing.T) {
			gen := newPlugin(t, makeFDS(true, tc.doris), []string{"a.proto", "b.proto"})
			if err := runner.Generate(gen, tc.opts); err != nil {
				t.Fatalf("Generate failed: %v", err)
			}
			files := stage1aGeneratedFiles(t, gen)
			if len(files) != 1 {
				t.Fatalf("expected 1 consolidated output file, got %d (%v)", len(files), files)
			}
			content, ok := files[tc.opts.OutputFile]
			if !ok {
				t.Fatalf("expected consolidated file %q, got keys %v", tc.opts.OutputFile, files)
			}
			for _, sub := range tc.wantContains {
				if !strings.Contains(content, sub) {
					t.Errorf("consolidated output missing %q in:\n%s", sub, content)
				}
			}
			if tc.verifyGo {
				fset := token.NewFileSet()
				if _, err := parser.ParseFile(fset, tc.opts.OutputFile, content, parser.AllErrors); err != nil {
					t.Errorf("consolidated Go file does not parse cleanly: %v\n%s", err, content)
				}
			}
		})
	}

	// 2. Fail-fast cases
	failCases := []struct {
		name    string
		samePkg bool
		doris   bool
		opts    *Options
	}{
		{
			name:    "path traversal",
			samePkg: true,
			opts:    &Options{Lang: "go", Backend: "postgres", PostgresMode: "hcl", OutputFile: "../evil.hcl"},
		},
		{
			name:    "absolute path",
			samePkg: true,
			opts:    &Options{Lang: "go", Backend: "postgres", PostgresMode: "hcl", OutputFile: "/tmp/evil.hcl"},
		},
		{
			name:    "postgres all mode",
			samePkg: true,
			opts:    &Options{Lang: "go", Backend: "postgres", PostgresMode: "all", OutputFile: "out.sql"},
		},
		{
			name:    "postgres ddl+hcl mode",
			samePkg: true,
			opts:    &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl+hcl", OutputFile: "out.sql"},
		},
		{
			name:    "domain true with postgres backend",
			samePkg: true,
			opts:    &Options{Lang: "go", Backend: "postgres", PostgresMode: "go", Domain: true, OutputFile: "out.go"},
		},
		{
			name:    "doris all mode",
			samePkg: true,
			doris:   true,
			opts:    &Options{Lang: "go", Backend: "doris", DorisMode: "all", OutputFile: "out.sql"},
		},
		{
			name:    "doris ddl emitting doris and iceberg",
			samePkg: true,
			doris:   true,
			opts:    &Options{Lang: "go", Backend: "doris", DorisMode: "ddl", OutputFile: "out.sql"},
		},
		{
			name:    "doris hcl emitting doris and iceberg",
			samePkg: true,
			doris:   true,
			opts:    &Options{Lang: "go", Backend: "doris", DorisMode: "hcl", OutputFile: "out.hcl"},
		},
		{
			name:    "mixed go_package in consolidated postgres go",
			samePkg: false,
			opts:    &Options{Lang: "go", Backend: "postgres", PostgresMode: "go", OutputFile: "out.go"},
		},
		{
			name:    "mixed go_package in consolidated doris go",
			samePkg: false,
			doris:   true,
			opts:    &Options{Lang: "go", Backend: "doris", DorisMode: "go", OutputFile: "out.go"},
		},
	}
	for _, tc := range failCases {
		t.Run("fail_fast/"+tc.name, func(t *testing.T) {
			gen := newPlugin(t, makeFDS(tc.samePkg, tc.doris), []string{"a.proto", "b.proto"})
			if err := runner.Generate(gen, tc.opts); err == nil {
				t.Errorf("expected fail-fast error for %s, got nil", tc.name)
			}
		})
	}
}
