package generator

import (
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	proto2typepb "github.com/protocgen/proto2type/proto/proto2type"
)

func TestParsePostgresMode_ComposableVariations(t *testing.T) {
	cases := []struct {
		mode string
		want PostgresModeTargets
	}{
		{"", PostgresModeTargets{EmitTypes: true}},
		{"tables", PostgresModeTargets{EmitTypes: true}},
		{"types", PostgresModeTargets{EmitTypes: true}},
		{"go", PostgresModeTargets{EmitTypes: true}},
		{"ddl", PostgresModeTargets{EmitDDL: true}},
		{"hcl", PostgresModeTargets{EmitHCL: true}},
		{"both", PostgresModeTargets{EmitTypes: true, EmitDDL: true}},
		{"all", PostgresModeTargets{EmitTypes: true, EmitDDL: true, EmitHCL: true}},
		{"types+hcl", PostgresModeTargets{EmitTypes: true, EmitHCL: true}},
		{"ddl+hcl", PostgresModeTargets{EmitDDL: true, EmitHCL: true}},
		{"search_index", PostgresModeTargets{EmitSearchIndexGo: true}},
		{"search_index+hcl", PostgresModeTargets{EmitSearchIndexGo: true, EmitSearchIndexHCL: true}},
		{"search_index+ddl", PostgresModeTargets{EmitSearchIndexGo: true, EmitSearchIndexDDL: true}},
		{"search_index_hcl", PostgresModeTargets{EmitSearchIndexHCL: true}},
		{"search_index_ddl", PostgresModeTargets{EmitSearchIndexDDL: true}},
		{"types+search_index+hcl", PostgresModeTargets{EmitTypes: true, EmitHCL: true, EmitSearchIndexGo: true, EmitSearchIndexHCL: true}},
		{"all+search_index", PostgresModeTargets{EmitTypes: true, EmitDDL: true, EmitHCL: true, EmitSearchIndexGo: true, EmitSearchIndexDDL: true, EmitSearchIndexHCL: true}},
	}
	for _, tc := range cases {
		opts := &Options{Backend: "postgres", PostgresMode: tc.mode}
		got, err := opts.ParsePostgresMode()
		if err != nil {
			t.Fatalf("ParsePostgresMode(%q) unexpected error: %v", tc.mode, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParsePostgresMode(%q) = %+v, want %+v", tc.mode, got, tc.want)
		}
	}

	invalidOpts := &Options{Backend: "postgres", PostgresMode: "invalid_mode"}
	if _, err := invalidOpts.ParsePostgresMode(); err == nil {
		t.Fatal("expected error for invalid postgres_mode")
	}
	noneComboOpts := &Options{Backend: "postgres", PostgresMode: "none+ddl"}
	if _, err := noneComboOpts.ParsePostgresMode(); err == nil {
		t.Fatal("expected error for 'none+ddl' postgres_mode")
	}
}

func TestParseDorisMode_ComposableVariations(t *testing.T) {
	cases := []struct {
		backend string
		mode    string
		want    DorisModeTargets
	}{
		{"doris", "", DorisModeTargets{EmitGo: true}},
		{"starrocks", "", DorisModeTargets{EmitGo: true, EmitStarRocksDDL: true, EmitStarRocksHCL: true}},
		{"doris", "go+hcl", DorisModeTargets{EmitGo: true, EmitDorisHCL: true}},
		{"doris", "ddl+hcl", DorisModeTargets{EmitDorisDDL: true, EmitDorisHCL: true}},
		{"doris", "go+starrocks_hcl", DorisModeTargets{EmitGo: true, EmitStarRocksHCL: true}},
		{"doris", "all", DorisModeTargets{EmitGo: true, EmitDorisDDL: true, EmitDorisHCL: true, EmitStarRocksDDL: true, EmitStarRocksHCL: true}},
	}
	for _, tc := range cases {
		opts := &Options{Backend: tc.backend, DorisMode: tc.mode}
		got, err := opts.ParseDorisMode()
		if err != nil {
			t.Fatalf("ParseDorisMode(%q, %q) unexpected error: %v", tc.backend, tc.mode, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseDorisMode(%q, %q) = %+v, want %+v", tc.backend, tc.mode, got, tc.want)
		}
	}
}

func TestPostgresDDLAndHCL_LakehouseProto(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"lakehouse.proto"})
	runner := NewRunner()

	opts := &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "ddl+hcl",
		Domain:       true, // Should skip domain collision automatically in schema-only mode
	}

	for _, f := range gen.Files {
		if f.Generate && f.Desc.Path() == "lakehouse.proto" {
			if err := runner.GenerateFile(gen, f, opts); err != nil {
				t.Fatalf("GenerateFile(lakehouse.proto, postgres ddl+hcl) failed: %v", err)
			}
		}
	}

	sqlOut := extractPluginOutput(t, gen, "lakehouse_postgres.sql")
	hclOut := extractPluginOutput(t, gen, "lakehouse_postgres.hcl")
	if sqlOut == "" || hclOut == "" {
		t.Fatalf("expected both lakehouse_postgres.sql and lakehouse_postgres.hcl, got sql=%d bytes, hcl=%d bytes", len(sqlOut), len(hclOut))
	}

	// Verify strict relational types (no JSON/JSONB), flattened inline_nested fields, arrays, and pgvector
	for _, wantSQL := range []string{
		"CREATE TABLE IF NOT EXISTS tenant_${TENANT_SLUG}.eligibility_criterion_results (",
		"patient_id VARCHAR(64) NOT NULL",
		"all_mrns VARCHAR(64)[] NOT NULL DEFAULT '{}'",
		"fingerprint_hash NUMERIC(38, 0) NOT NULL",
		"event_month DATE NOT NULL",
		"verified_at TIMESTAMPTZ",
		"citation_page_number INTEGER NOT NULL",
		"citation_bbox_coords REAL[] NOT NULL DEFAULT '{}'",
		"PRIMARY KEY (patient_id, criterion_id, event_month)",
		"PARTITION BY RANGE (event_month);",
		"CREATE INDEX IF NOT EXISTS idx_eligibility_criterion_results_all_mrns ON tenant_${TENANT_SLUG}.eligibility_criterion_results USING gin (all_mrns);",
		"CREATE INDEX IF NOT EXISTS idx_ngram_eligibility_criterion_results_criterion_id ON tenant_${TENANT_SLUG}.eligibility_criterion_results USING gin (criterion_id);",
		"CREATE TABLE IF NOT EXISTS terminology_rag.concept_embeddings (",
		"embedding VECTOR(768) NOT NULL",
		"CREATE INDEX IF NOT EXISTS idx_embedding_hnsw ON terminology_rag.concept_embeddings USING hnsw (embedding vector_l2_ops);",
	} {
		if !strings.Contains(sqlOut, wantSQL) {
			t.Errorf("lakehouse_postgres.sql missing %q\nFull SQL:\n%s", wantSQL, sqlOut)
		}
	}
	if strings.Contains(strings.ToUpper(sqlOut), "JSON") {
		t.Errorf("lakehouse_postgres.sql must not contain JSON/JSONB:\n%s", sqlOut)
	}

	// Parse generated lakehouse_postgres.hcl and verify zero migration drift against source descriptors
	parsedTables, err := ParsePostgresAtlasHCL(hclOut)
	if err != nil {
		t.Fatalf("ParsePostgresAtlasHCL(lakehouse_postgres.hcl) failed: %v\nHCL:\n%s", err, hclOut)
	}
	if len(parsedTables) != 2 {
		t.Fatalf("expected 2 parsed tables from lakehouse_postgres.hcl, got %d", len(parsedTables))
	}
	var wantDescs []PostgresTableDescriptor
	for _, f := range gen.Files {
		if f.Generate {
			tbls, err := buildPostgresTablesForFile(f, opts)
			if err != nil {
				t.Fatalf("buildPostgresTablesForFile failed: %v", err)
			}
			for _, tbl := range tbls {
				wantDescs = append(wantDescs, tbl.Descriptor)
			}
		}
	}
	stmts, err := BuildPostgresSchemaMigrationSQL(parsedTables, wantDescs, "acme")
	if err != nil || len(stmts) != 0 {
		t.Fatalf("expected zero migration drift between parsed lakehouse_postgres.hcl and source descriptors, got (%v, %v)", stmts, err)
	}
}

func TestPostgresSearchIndex_ComposableHCLAndDDL(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"fhir_patient.proto"})
	runner := NewRunner()

	opts := &Options{
		Lang:                 "go",
		Backend:              "postgres",
		PostgresMode:         "search_index+ddl+hcl",
		PostgresSearchParams: "../testdata/proto/fhir_search_params.json",
		Domain:               false,
	}

	for _, f := range gen.Files {
		if f.Generate {
			if err := runner.GenerateFile(gen, f, opts); err != nil {
				t.Fatalf("GenerateFile(fhir_patient.proto, search_index+ddl+hcl) failed: %v", err)
			}
		}
	}

	goOut := extractPluginOutput(t, gen, "_search_index.go")
	sqlOut := extractPluginOutput(t, gen, "_search_index.sql")
	hclOut := extractPluginOutput(t, gen, "_search_index.hcl")
	if goOut == "" || sqlOut == "" || hclOut == "" {
		t.Fatalf("expected _search_index.go, _search_index.sql, and _search_index.hcl; got go=%d, sql=%d, hcl=%d", len(goOut), len(sqlOut), len(hclOut))
	}

	parsedTables, err := ParsePostgresAtlasHCL(hclOut)
	if err != nil {
		t.Fatalf("ParsePostgresAtlasHCL(_search_index.hcl) failed: %v", err)
	}
	wantTables := PostgresSearchIndexTables()
	if len(parsedTables) != len(wantTables) {
		t.Fatalf("got %d search index tables, want %d", len(parsedTables), len(wantTables))
	}
	if stmts, err := BuildPostgresSchemaMigrationSQL(parsedTables, wantTables, ""); err != nil || len(stmts) != 0 {
		t.Fatalf("expected zero migration drift between _search_index.hcl and PostgresSearchIndexTables(), got (%v, %v)", stmts, err)
	}
}

func TestPostgresLinter_RulesAndForbiddenJSONB(t *testing.T) {
	before := PostgresTableDescriptor{
		TableName:          "eligibility_criterion_results",
		DatabaseScope:      "TENANT",
		PrimaryKeyColumns:  []string{"patient_id", "criterion_id", "event_month"},
		PartitionByColumns: []string{"event_month"},
		Columns: []PostgresColumnDescriptor{
			{ColumnName: "patient_id", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
			{ColumnName: "criterion_id", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
			{ColumnName: "event_month", PostgresType: "DATE", Nullable: false, IsPrimaryKey: true, IsPartitionKey: true},
			{ColumnName: "score", PostgresType: "REAL", Nullable: true},
		},
	}

	// 1. Safe widening + safe column addition
	afterSafe := before
	afterSafe.Columns = append([]PostgresColumnDescriptor(nil), before.Columns...)
	afterSafe.Columns[1].PostgresType = "VARCHAR(128)"
	afterSafe.Columns[3].PostgresType = "DOUBLE PRECISION"
	afterSafe.Columns = append(afterSafe.Columns, PostgresColumnDescriptor{
		ColumnName:   "review_note",
		PostgresType: "TEXT",
		Nullable:     true,
	})
	if err := ValidatePostgresTableEvolution(before, afterSafe); err != nil {
		t.Fatalf("ValidatePostgresTableEvolution failed on safe evolution: %v", err)
	}
	alterStmts, err := before.BuildPostgresAlterTableSQL(afterSafe, "acme")
	if err != nil {
		t.Fatalf("BuildPostgresAlterTableSQL failed: %v", err)
	}
	wantAlter := []string{
		"ALTER TABLE tenant_acme.eligibility_criterion_results ALTER COLUMN criterion_id TYPE VARCHAR(128);",
		"ALTER TABLE tenant_acme.eligibility_criterion_results ALTER COLUMN score TYPE DOUBLE PRECISION;",
		"ALTER TABLE tenant_acme.eligibility_criterion_results ADD COLUMN review_note TEXT;",
	}
	if !reflect.DeepEqual(alterStmts, wantAlter) {
		t.Fatalf("BuildPostgresAlterTableSQL = %#v, want %#v", alterStmts, wantAlter)
	}

	// 2. Unsafe evolution triggering PG001, PG002, PG004, PG005, PG006, PG007 in order
	afterUnsafe := PostgresTableDescriptor{
		TableName:          "eligibility_criterion_results",
		DatabaseScope:      "TENANT",
		PrimaryKeyColumns:  []string{"patient_id"},
		PartitionByColumns: []string{"criterion_id"},
		Columns: []PostgresColumnDescriptor{
			{ColumnName: "patient_id", PostgresType: "VARCHAR(32)", Nullable: false, IsPrimaryKey: true}, // PG006 narrowing
			{ColumnName: "criterion_id", PostgresType: "VARCHAR(64)", Nullable: false},
			{ColumnName: "event_month", PostgresType: "DATE", Nullable: false},
			// score dropped -> PG004
			{ColumnName: "required_no_default", PostgresType: "INTEGER", Nullable: false}, // PG005
			{ColumnName: "forbidden_blob", PostgresType: "JSONB", Nullable: true},         // PG007
		},
	}
	violations := LintPostgresTableEvolution(before, afterUnsafe)
	wantRules := []PostgresLintRule{
		PG001_IMMUTABLE_PK,
		PG002_IMMUTABLE_PARTITION,
		PG004_NO_COLUMN_DROP,
		PG005_NON_NULL_SCALAR_DEFAULT,
		PG006_SAFE_TYPE_EVOLUTION,
		PG007_FORBIDDEN_TYPE,
	}
	if len(violations) != len(wantRules) {
		t.Fatalf("got %d violations, want %d: %v", len(violations), len(wantRules), violations)
	}
	for i, wantRule := range wantRules {
		if violations[i].Rule != wantRule {
			t.Errorf("violations[%d].Rule = %s, want %s", i, violations[i].Rule, wantRule)
		}
	}
}

func TestGoPostgres_InlineNestedFlattening(t *testing.T) {
	inlineFieldOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(inlineFieldOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		Inline: true,
	})
	docIDFieldOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(docIDFieldOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		DocumentId: true,
	})

	tsFileProto := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("google/protobuf/timestamp.proto"),
		Package: proto.String("google.protobuf"),
		Syntax:  proto.String("proto3"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("google.golang.org/protobuf/types/known/timestamppb"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Timestamp"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("seconds"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum()},
					{Name: proto.String("nanos"), Number: proto.Int32(2), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum()},
				},
			},
		},
	}

	fileProto := &descriptorpb.FileDescriptorProto{
		Name:       proto.String("inline_pg.proto"),
		Package:    proto.String("test.v1"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/protobuf/timestamp.proto"},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/protocgen/proto2type/testdata/golden/go/pb;pb"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("GeoPoint"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   proto.String("lat"),
						Number: proto.Int32(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum(),
					},
					{
						Name:   proto.String("lng"),
						Number: proto.Int32(2),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum(),
					},
					{
						Name:           proto.String("recorded_at"),
						Number:         proto.Int32(3),
						Label:          descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:           descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName:       proto.String(".google.protobuf.Timestamp"),
						Proto3Optional: proto.Bool(true),
						OneofIndex:     proto.Int32(0),
					},
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: proto.String("_recorded_at")},
				},
			},
			{
				Name: proto.String("ClinicLocation"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:    proto.String("clinic_id"),
						Number:  proto.Int32(1),
						Label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:    descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						Options: docIDFieldOpts,
					},
					{
						Name:     proto.String("coords"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".test.v1.GeoPoint"),
						Options:  inlineFieldOpts,
					},
				},
			},
		},
	}

	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{tsFileProto, fileProto}}
	gen := newPlugin(t, fds, []string{"inline_pg.proto"})
	runner := NewRunner()
	opts := &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "all",
		Domain:       true,
		GoPackage:    "github.com/protocgen/proto2type/testdata/golden/go/gen;gen",
	}

	for _, f := range gen.Files {
		if f.Generate {
			if err := runner.GenerateFile(gen, f, opts); err != nil {
				t.Fatalf("GenerateFile(inline_pg.proto) failed: %v", err)
			}
		}
	}

	domainContent := extractPluginOutput(t, gen, "inline_pg.type.go")
	goContent := extractPluginOutput(t, gen, "inline_pg_postgres.type.go")
	sqlContent := extractPluginOutput(t, gen, "inline_pg_postgres.sql")
	hclContent := extractPluginOutput(t, gen, "inline_pg_postgres.hcl")

	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "inline_pg.type.go", domainContent, parser.AllErrors); err != nil {
		t.Fatalf("generated inline_pg.type.go has invalid Go syntax: %v\n%s", err, domainContent)
	}
	if _, err := parser.ParseFile(fset, "inline_pg_postgres.type.go", goContent, parser.AllErrors); err != nil {
		t.Fatalf("generated inline_pg_postgres.type.go has invalid Go syntax: %v\n%s", err, goContent)
	}

	for _, wantGo := range []string{
		"`db:\"coords_lat\"`",
		"`db:\"coords_lng\"`",
		`CoordsRecordedAt time.Time ` + "`db:\"coords_recorded_at\"`",
		`vCoordsRecordedAt := c.CoordsRecordedAt`,
		`d.Coords.RecordedAt = &vCoordsRecordedAt`,
		`c.CoordsRecordedAt = *d.Coords.RecordedAt`,
	} {
		if !strings.Contains(goContent, wantGo) {
			t.Errorf("inline_pg_postgres.type.go missing %q\nContent:\n%s", wantGo, goContent)
		}
	}
	for _, wantSQL := range []string{
		`"coords_lat" DOUBLE PRECISION NOT NULL`,
		`"coords_lng" DOUBLE PRECISION NOT NULL`,
		`"coords_recorded_at" TIMESTAMPTZ`,
	} {
		if !strings.Contains(sqlContent, wantSQL) {
			t.Errorf("inline_pg_postgres.sql missing %q\nSQL:\n%s", wantSQL, sqlContent)
		}
	}
	if _, err := ParsePostgresAtlasHCL(hclContent); err != nil {
		t.Fatalf("ParsePostgresAtlasHCL(inline_pg_postgres.hcl) failed: %v\n%s", err, hclContent)
	}
}

func TestPostgresIdentityAndExpressionIndexes_EndToEndAndRoundTrip(t *testing.T) {
	programMsgOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(programMsgOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			TableName:        "productconfigservice__programs",
			PrimaryKeyFields: []string{"program_key"},
			Indexes: []*proto2typepb.IndexDef{
				{
					Name:        "productconfigservice__programs_name_key",
					Expressions: []string{"lower(btrim(name))"},
					Unique:      true,
				},
				{
					Name:        "productconfigservice__programs_family_expr_idx",
					Columns:     []string{"family"},
					DescColumns: []string{"family"},
					Expressions: []string{"lower(btrim(name))"},
					Where:       "(status <> 'INACTIVE'::text)",
				},
				{
					Expressions: []string{"(coalesce(program_type_key, ''::text))"},
				},
			},
		},
	})

	versionMsgOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(versionMsgOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			TableName:        "productconfigservice__configuration_versions",
			PrimaryKeyFields: []string{"version"},
		},
	})

	identityFieldOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(identityFieldOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		Column: &proto2typepb.ColumnOptions{
			Identity: true,
		},
	})

	// Also build a message/field using raw wire-encoded extension 1307 to exercise parseIndexDefBytes(tag 6)
	// and parseColumnMetaBytes(tag 20) fallback paths.
	var rawIdxDef []byte
	rawIdxDef = protowire.AppendTag(rawIdxDef, 1, protowire.BytesType)
	rawIdxDef = protowire.AppendString(rawIdxDef, "wire_events_actor_expr_idx")
	rawIdxDef = protowire.AppendTag(rawIdxDef, 3, protowire.VarintType)
	rawIdxDef = protowire.AppendVarint(rawIdxDef, 1)
	rawIdxDef = protowire.AppendTag(rawIdxDef, 6, protowire.BytesType)
	rawIdxDef = protowire.AppendString(rawIdxDef, "lower(actor_subject)")

	var rawTableOpts []byte
	rawTableOpts = protowire.AppendTag(rawTableOpts, 1, protowire.BytesType)
	rawTableOpts = protowire.AppendString(rawTableOpts, "productconfigservice__change_history")
	rawTableOpts = protowire.AppendTag(rawTableOpts, 19, protowire.BytesType)
	rawTableOpts = protowire.AppendString(rawTableOpts, "event_id")
	rawTableOpts = protowire.AppendTag(rawTableOpts, 20, protowire.BytesType)
	rawTableOpts = protowire.AppendBytes(rawTableOpts, rawIdxDef)

	var rawMsgExt []byte
	rawMsgExt = protowire.AppendTag(rawMsgExt, 2, protowire.BytesType)
	rawMsgExt = protowire.AppendBytes(rawMsgExt, rawTableOpts)

	var wireMsgOptsRaw []byte
	wireMsgOptsRaw = protowire.AppendTag(wireMsgOptsRaw, proto2typeExtensionNumber, protowire.BytesType)
	wireMsgOptsRaw = protowire.AppendBytes(wireMsgOptsRaw, rawMsgExt)
	wireMsgOpts := &descriptorpb.MessageOptions{}
	wireMsgOpts.ProtoReflect().SetUnknown(wireMsgOptsRaw)

	var rawColOpts []byte
	rawColOpts = protowire.AppendTag(rawColOpts, 20, protowire.VarintType)
	rawColOpts = protowire.AppendVarint(rawColOpts, 1)
	var rawFieldExt []byte
	rawFieldExt = protowire.AppendTag(rawFieldExt, 10, protowire.BytesType)
	rawFieldExt = protowire.AppendBytes(rawFieldExt, rawColOpts)
	var wireFieldOptsRaw []byte
	wireFieldOptsRaw = protowire.AppendTag(wireFieldOptsRaw, proto2typeExtensionNumber, protowire.BytesType)
	wireFieldOptsRaw = protowire.AppendBytes(wireFieldOptsRaw, rawFieldExt)
	wireIdentityFieldOpts := &descriptorpb.FieldOptions{}
	wireIdentityFieldOpts.ProtoReflect().SetUnknown(wireFieldOptsRaw)

	fileProto := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("programs_pg.proto"),
		Package: proto.String("azra.productconfig.v1"),
		Syntax:  proto.String("proto3"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("github.com/protocgen/proto2type/testdata/golden/go/pb;pb"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("ProgramRow"),
				Options: programMsgOpts,
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("program_key"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
					{Name: proto.String("family"), Number: proto.Int32(2), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
					{Name: proto.String("program_type_key"), Number: proto.Int32(3), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
					{Name: proto.String("name"), Number: proto.Int32(4), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
					{Name: proto.String("status"), Number: proto.Int32(5), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
				},
			},
			{
				Name:    proto.String("ConfigurationVersionRow"),
				Options: versionMsgOpts,
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("version"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(), Options: identityFieldOpts},
					{Name: proto.String("program_key"), Number: proto.Int32(2), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
				},
			},
			{
				Name:    proto.String("ChangeHistoryRow"),
				Options: wireMsgOpts,
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("event_id"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(), Options: wireIdentityFieldOpts},
					{Name: proto.String("actor_subject"), Number: proto.Int32(2), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
				},
			},
		},
	}

	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fileProto}}
	gen := newPlugin(t, fds, []string{"programs_pg.proto"})
	runner := NewRunner()
	opts := &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "all",
		Domain:       true,
		GoPackage:    "github.com/protocgen/proto2type/testdata/golden/go/gen;gen",
	}

	for _, f := range gen.Files {
		if f.Generate {
			if err := runner.GenerateFile(gen, f, opts); err != nil {
				t.Fatalf("GenerateFile(programs_pg.proto) failed: %v", err)
			}
		}
	}

	sqlOut := extractPluginOutput(t, gen, "programs_pg_postgres.sql")
	hclOut := extractPluginOutput(t, gen, "programs_pg_postgres.hcl")

	for _, wantSQL := range []string{
		`"version" BIGINT NOT NULL GENERATED ALWAYS AS IDENTITY`,
		`"event_id" BIGINT NOT NULL GENERATED ALWAYS AS IDENTITY`,
		`CREATE UNIQUE INDEX IF NOT EXISTS "productconfigservice__programs_name_key" ON "productconfigservice__programs" ((lower(btrim(name))));`,
		`CREATE INDEX IF NOT EXISTS "productconfigservice__programs_family_expr_idx" ON "productconfigservice__programs" ("family" DESC, (lower(btrim(name)))) WHERE (status <> 'INACTIVE'::text);`,
		`ON "productconfigservice__programs" ((coalesce(program_type_key, ''::text)));`,
		`CREATE UNIQUE INDEX IF NOT EXISTS "wire_events_actor_expr_idx" ON "productconfigservice__change_history" ((lower(actor_subject)));`,
	} {
		if !strings.Contains(sqlOut, wantSQL) {
			t.Errorf("programs_pg_postgres.sql missing %q\nSQL:\n%s", wantSQL, sqlOut)
		}
	}

	for _, wantHCL := range []string{
		"  column \"version\" {\n    null = false\n    type = bigint\n    identity {\n      generated = ALWAYS\n    }\n  }",
		"  column \"event_id\" {\n    null = false\n    type = bigint\n    identity {\n      generated = ALWAYS\n    }\n  }",
		"  index \"productconfigservice__programs_name_key\" {\n    unique  = true\n    on {\n      expr = \"lower(btrim(name))\"\n    }\n  }",
		"  index \"productconfigservice__programs_family_expr_idx\" {\n    on {\n      desc   = true\n      column = column.family\n    }\n    on {\n      expr = \"lower(btrim(name))\"\n    }\n    where   = \"(status <> 'INACTIVE'::text)\"\n  }",
		"  index \"wire_events_actor_expr_idx\" {\n    unique  = true\n    on {\n      expr = \"lower(actor_subject)\"\n    }\n  }",
	} {
		if !strings.Contains(hclOut, wantHCL) {
			t.Errorf("programs_pg_postgres.hcl missing %q\nHCL:\n%s", wantHCL, hclOut)
		}
	}

	// Round-trip HCL -> ParsePostgresAtlasHCL -> BuildPostgresAtlasHCL -> ParsePostgresAtlasHCL
	parsed, err := ParsePostgresAtlasHCL(hclOut)
	if err != nil {
		t.Fatalf("ParsePostgresAtlasHCL failed: %v\nHCL:\n%s", err, hclOut)
	}
	if len(parsed) != 3 {
		t.Fatalf("expected 3 parsed tables, got %d", len(parsed))
	}
	if !parsed[1].Columns[0].Identity {
		t.Errorf("expected ConfigurationVersionRow.version Identity=true after HCL parse, got %+v", parsed[1].Columns[0])
	}
	if !parsed[2].Columns[0].Identity {
		t.Errorf("expected ChangeHistoryRow.event_id Identity=true after HCL parse, got %+v", parsed[2].Columns[0])
	}
	if gotExprs := parsed[0].Indexes[0].Expressions; len(gotExprs) != 1 || gotExprs[0] != "lower(btrim(name))" {
		t.Errorf("expected parsed[0].Indexes[0].Expressions = [lower(btrim(name))], got %v", gotExprs)
	}

	// Verify zero migration drift when comparing parsed descriptors to themselves
	stmts, err := BuildPostgresSchemaMigrationSQL(parsed, parsed, "acme")
	if err != nil {
		t.Fatalf("BuildPostgresSchemaMigrationSQL self-diff failed: %v", err)
	}
	if len(stmts) != 0 {
		t.Errorf("expected 0 migration statements on identical parsed schema, got %v", stmts)
	}

	// Verify Lakehouse BuildPostgresAtlasHCL & BuildPostgresCreateTableSQL parity
	lhSQL := parsed[0].BuildPostgresCreateTableSQL("acme")
	if !strings.Contains(lhSQL, "CREATE UNIQUE INDEX IF NOT EXISTS productconfigservice__programs_name_key ON tenant_acme.productconfigservice__programs ((lower(btrim(name))));") {
		t.Errorf("lakehouse SQL missing expression index:\n%s", lhSQL)
	}
	lhVerSQL := parsed[1].BuildPostgresCreateTableSQL("acme")
	if !strings.Contains(lhVerSQL, "version BIGINT NOT NULL GENERATED ALWAYS AS IDENTITY") {
		t.Errorf("lakehouse SQL missing identity clause:\n%s", lhVerSQL)
	}
	lhVerHCL := parsed[1].BuildPostgresAtlasHCL("acme")
	if !strings.Contains(lhVerHCL, "identity {\n      generated = ALWAYS\n    }") {
		t.Errorf("lakehouse HCL missing identity block:\n%s", lhVerHCL)
	}
}

func TestPostgresIdentityAndExpressionIndexes_EvolutionAndValidation(t *testing.T) {
	// 1. Evolution: adding a NOT NULL identity column passes PG005 and emits GENERATED ALWAYS AS IDENTITY;
	// evolving existing default column to identity drops default first then adds identity;
	// adding an expression index emits CREATE UNIQUE INDEX.
	before := PostgresTableDescriptor{
		TableName:         "events",
		DatabaseScope:     "PUBLIC",
		PrimaryKeyColumns: []string{"id"},
		Columns: []PostgresColumnDescriptor{
			{ColumnName: "id", PostgresType: "BIGINT", Nullable: false, IsPrimaryKey: true, DefaultValue: "0"},
			{ColumnName: "name", PostgresType: "TEXT", Nullable: false, DefaultValue: ""},
		},
	}
	after := PostgresTableDescriptor{
		TableName:         "events",
		DatabaseScope:     "PUBLIC",
		PrimaryKeyColumns: []string{"id"},
		Columns: []PostgresColumnDescriptor{
			{ColumnName: "id", PostgresType: "BIGINT", Nullable: false, IsPrimaryKey: true, Identity: true},
			{ColumnName: "name", PostgresType: "TEXT", Nullable: false, DefaultValue: ""},
			{ColumnName: "seq_no", PostgresType: "INTEGER", Nullable: false, Identity: true},
		},
		Indexes: []PostgresIndexDescriptor{
			{
				Name:        "events_name_norm_key",
				Unique:      true,
				Expressions: []string{"lower(btrim(name))"},
			},
		},
	}
	if violations := LintPostgresTableEvolution(before, after); len(violations) != 0 {
		t.Fatalf("expected 0 evolution violations when adding/evolving identity columns, got %v", violations)
	}
	alterStmts, err := BuildPostgresAlterTableSQL(before, after, "")
	if err != nil {
		t.Fatalf("BuildPostgresAlterTableSQL failed: %v", err)
	}
	joined := strings.Join(alterStmts, "\n")
	for _, want := range []string{
		"ALTER TABLE public.events ALTER COLUMN id DROP DEFAULT;",
		"ALTER TABLE public.events ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY;",
		"ALTER TABLE public.events ADD COLUMN seq_no INTEGER NOT NULL GENERATED ALWAYS AS IDENTITY;",
		"CREATE UNIQUE INDEX IF NOT EXISTS events_name_norm_key ON public.events ((lower(btrim(name))));",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("BuildPostgresAlterTableSQL missing %q\nGot:\n%s", want, joined)
		}
	}
	// Verify DROP DEFAULT precedes ADD GENERATED ALWAYS AS IDENTITY
	if strings.Index(joined, "DROP DEFAULT;") > strings.Index(joined, "ADD GENERATED ALWAYS AS IDENTITY;") {
		t.Errorf("expected DROP DEFAULT before ADD GENERATED ALWAYS AS IDENTITY, got:\n%s", joined)
	}

	// Dropping identity from an existing column emits DROP IDENTITY IF EXISTS
	afterDropIdentity := after
	afterDropIdentity.Columns = []PostgresColumnDescriptor{
		{ColumnName: "id", PostgresType: "BIGINT", Nullable: false, IsPrimaryKey: true},
		{ColumnName: "name", PostgresType: "TEXT", Nullable: false, DefaultValue: ""},
		{ColumnName: "seq_no", PostgresType: "INTEGER", Nullable: false, Identity: true},
	}
	dropStmts, err := BuildPostgresAlterTableSQL(after, afterDropIdentity, "")
	if err != nil {
		t.Fatalf("BuildPostgresAlterTableSQL drop identity failed: %v", err)
	}
	if !strings.Contains(strings.Join(dropStmts, "\n"), "ALTER TABLE public.events ALTER COLUMN id DROP IDENTITY IF EXISTS;") {
		t.Errorf("expected DROP IDENTITY IF EXISTS, got: %v", dropStmts)
	}

	// 2. Negative proto validation cases
	runProtoErrCase := func(name string, msgOpts *descriptorpb.MessageOptions, fldType descriptorpb.FieldDescriptorProto_Type, fldLabel descriptorpb.FieldDescriptorProto_Label, fldOpts *descriptorpb.FieldOptions, wantSubstr string) {
		t.Helper()
		fp := &descriptorpb.FileDescriptorProto{
			Name:    proto.String("bad.proto"),
			Package: proto.String("test.v1"),
			Syntax:  proto.String("proto3"),
			Options: &descriptorpb.FileOptions{GoPackage: proto.String("example.com/bad;bad")},
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name:    proto.String("BadRow"),
					Options: msgOpts,
					Field: []*descriptorpb.FieldDescriptorProto{
						{Name: proto.String("col"), Number: proto.Int32(1), Label: fldLabel.Enum(), Type: fldType.Enum(), Options: fldOpts},
					},
				},
			},
		}
		fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fp}}
		gen := newPlugin(t, fds, []string{"bad.proto"})
		err := NewRunner().GenerateFile(gen, gen.Files[0], &Options{Lang: "go", Backend: "postgres", PostgresMode: "hcl"})
		if err == nil || !strings.Contains(err.Error(), wantSubstr) {
			t.Errorf("%s: expected error containing %q, got %v", name, wantSubstr, err)
		}
	}

	idOpts := func(co *proto2typepb.ColumnOptions) *descriptorpb.FieldOptions {
		fo := &descriptorpb.FieldOptions{}
		proto.SetExtension(fo, proto2typepb.E_Field, &proto2typepb.FieldOptions{Column: co})
		return fo
	}
	tblIdxOpts := func(idx *proto2typepb.IndexDef) *descriptorpb.MessageOptions {
		mo := &descriptorpb.MessageOptions{}
		proto.SetExtension(mo, proto2typepb.E_Message, &proto2typepb.MessageOptions{
			Table: &proto2typepb.TableOptions{TableName: "bad_tbl", Indexes: []*proto2typepb.IndexDef{idx}},
		})
		return mo
	}

	runProtoErrCase("identity on string", nil, descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		idOpts(&proto2typepb.ColumnOptions{Identity: true}), "identity column requires SMALLINT, INTEGER, or BIGINT")
	runProtoErrCase("identity on bigserial", nil, descriptorpb.FieldDescriptorProto_TYPE_INT64, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		idOpts(&proto2typepb.ColumnOptions{Identity: true, SqlType: "BIGSERIAL"}), "identity column requires SMALLINT, INTEGER, or BIGINT")
	runProtoErrCase("identity on array", nil, descriptorpb.FieldDescriptorProto_TYPE_INT64, descriptorpb.FieldDescriptorProto_LABEL_REPEATED,
		idOpts(&proto2typepb.ColumnOptions{Identity: true}), "identity column requires SMALLINT, INTEGER, or BIGINT")
	runProtoErrCase("identity nullable true", nil, descriptorpb.FieldDescriptorProto_TYPE_INT64, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		idOpts(&proto2typepb.ColumnOptions{Identity: true, Nullable: proto2typepb.OptionalBool_OPTIONAL_BOOL_TRUE}), "identity column cannot be nullable")
	runProtoErrCase("identity with default_value", nil, descriptorpb.FieldDescriptorProto_TYPE_INT64, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		idOpts(&proto2typepb.ColumnOptions{Identity: true, DefaultValue: "1"}), "identity column cannot specify default_value")
	runProtoErrCase("index empty columns and expressions", tblIdxOpts(&proto2typepb.IndexDef{Name: "empty_idx", Unique: true}),
		descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL, nil, "has no columns")
	runProtoErrCase("index SQL injection in expression", tblIdxOpts(&proto2typepb.IndexDef{Name: "inj_idx", Expressions: []string{"lower(col)); DROP TABLE bad_tbl; --"}}),
		descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL, nil, "invalid index expression")
	runProtoErrCase("index unbalanced parens in expression", tblIdxOpts(&proto2typepb.IndexDef{Name: "paren_idx", Expressions: []string{"lower(btrim(col)"}}),
		descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL, nil, "invalid index expression")

	// 3. Negative HCL parser cases
	badHCLCases := []struct {
		name string
		hcl  string
	}{
		{
			name: "identity on text column",
			hcl: `table "t" {
  schema = schema.tenant
  column "id" {
    null = false
    type = text
    identity {
      generated = ALWAYS
    }
  }
}`,
		},
		{
			name: "identity on nullable column",
			hcl: `table "t" {
  schema = schema.tenant
  column "id" {
    null = true
    type = bigint
    identity {
      generated = ALWAYS
    }
  }
}`,
		},
		{
			name: "identity combined with default",
			hcl: `table "t" {
  schema = schema.tenant
  column "id" {
    null    = false
    type    = bigint
    default = 0
    identity {
      generated = ALWAYS
    }
  }
}`,
		},
		{
			name: "index on block with both column and expr",
			hcl: `table "t" {
  schema = schema.tenant
  column "name" {
    null = false
    type = text
  }
  index "idx_both" {
    on {
      column = column.name
      expr   = "lower(name)"
    }
  }
}`,
		},
		{
			name: "index on block with SQL injection in expr",
			hcl: `table "t" {
  schema = schema.tenant
  column "name" {
    null = false
    type = text
  }
  index "idx_inj" {
    on {
      expr = "lower(name)); DROP TABLE t; --"
    }
  }
}`,
		},
		{
			name: "index with neither columns nor expressions",
			hcl: `table "t" {
  schema = schema.tenant
  column "name" {
    null = false
    type = text
  }
  index "idx_empty" {
    unique = true
  }
}`,
		},
	}
	for _, tc := range badHCLCases {
		if _, err := ParsePostgresAtlasHCL(tc.hcl); err == nil {
			t.Errorf("expected ParsePostgresAtlasHCL error for %s, got nil", tc.name)
		}
	}
}
