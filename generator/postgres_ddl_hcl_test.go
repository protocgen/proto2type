package generator

import (
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

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
