package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	proto2typepb "github.com/protocgen/proto2type/proto/proto2type"
)

func TestPR241_IcebergAlterSafeEvolutionAndRuntimeParity(t *testing.T) {
	baseTbl := DorisTableDescriptor{
		TableName:        "ice_evo_events",
		DatabaseScope:    "TENANT",
		SyncTier:         "ICEBERG_ONLY",
		UniqueKeyColumns: []string{"id"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "id", DorisType: "VARCHAR(64)", IcebergType: "STRING", Nullable: false, IsUniqueKey: true},
			{ColumnName: "metric_val", DorisType: "VARCHAR(64)", IcebergType: "BIGINT", Nullable: true},
			{ColumnName: "small_num", DorisType: "SMALLINT", Nullable: true},
			{ColumnName: "attrs", DorisType: "MAP<VARCHAR(32), INT>", Nullable: true},
		},
	}

	// Unchanged resolved Iceberg type (SMALLINT -> INT both resolve to INT) is a no-op.
	sameIce := baseTbl
	sameIce.Columns = append([]DorisColumnDescriptor(nil), baseTbl.Columns...)
	sameIce.Columns[2].DorisType = "INT"
	if stmts, err := baseTbl.BuildIcebergAlterTableSQL(sameIce, "acme"); err != nil || len(stmts) != 0 {
		t.Fatalf("expected no-op for unchanged resolved Iceberg type SMALLINT -> INT, got (%v, %v)", stmts, err)
	}

	// Narrowing via IcebergType override (BIGINT -> INT while DorisType is unchanged) must error.
	narrowIce := baseTbl
	narrowIce.Columns = append([]DorisColumnDescriptor(nil), baseTbl.Columns...)
	narrowIce.Columns[1].IcebergType = "INT"
	if stmts, err := baseTbl.BuildIcebergAlterTableSQL(narrowIce, "acme"); err == nil {
		t.Fatalf("expected error on IcebergType narrowing BIGINT -> INT, got stmts=%v", stmts)
	}

	// Narrowing inside MAP value type must also error.
	narrowMap := baseTbl
	narrowMap.Columns = append([]DorisColumnDescriptor(nil), baseTbl.Columns...)
	narrowMap.Columns[3].DorisType = "MAP<VARCHAR(32), SMALLINT>"
	if IsSafeDorisTypeEvolution("MAP<STRING, BIGINT>", "MAP<STRING, INT>") {
		t.Fatal("expected MAP<STRING, BIGINT> -> MAP<STRING, INT> to be unsafe")
	}

	// Widening via IcebergType override (INT -> BIGINT) emits MODIFY COLUMN.
	widenBefore := baseTbl
	widenBefore.Columns = append([]DorisColumnDescriptor(nil), baseTbl.Columns...)
	widenBefore.Columns[1].IcebergType = "INT"
	widenAfter := baseTbl
	widenAfter.Columns = append([]DorisColumnDescriptor(nil), widenBefore.Columns...)
	widenAfter.Columns[1].IcebergType = "BIGINT"
	widenStmts, err := widenBefore.BuildIcebergAlterTableSQL(widenAfter, "acme")
	if err != nil {
		t.Fatalf("expected safe IcebergType widening INT -> BIGINT to succeed, got %v", err)
	}
	wantWiden := "ALTER TABLE iceberg.tenant_acme.ice_evo_events MODIFY COLUMN metric_val BIGINT;"
	if len(widenStmts) != 1 || widenStmts[0] != wantWiden {
		t.Fatalf("unexpected widen statements: %v (want [%q])", widenStmts, wantWiden)
	}

	// BIGINT -> LARGEINT (resolves to DECIMAL(38, 0) on Iceberg) is a safe widening.
	largeAfter := baseTbl
	largeAfter.Columns = append([]DorisColumnDescriptor(nil), baseTbl.Columns...)
	largeAfter.Columns[1].IcebergType = "LARGEINT"
	if stmts, err := baseTbl.BuildIcebergAlterTableSQL(largeAfter, "acme"); err != nil || len(stmts) != 1 || !strings.Contains(stmts[0], "DECIMAL(38, 0)") {
		t.Fatalf("expected BIGINT -> LARGEINT (DECIMAL(38, 0)) widening to succeed, got (%v, %v)", stmts, err)
	}

	// Compiled runtime parity check for BuildIcebergAlterTableSQL narrowing & map evolution.
	tmpRuntimeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpRuntimeDir, "go.mod"), []byte("module example.com/rt\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	dummyFDP := stage1aSingleMsgFDP("rt_dummy.proto", "rt.v1", "example.com/rt/v1;rtv1", &descriptorpb.DescriptorProto{
		Name:    proto.String("RtDummy"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: "rt_dummy"}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
		},
	})
	rtGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{dummyFDP}}, []string{"rt_dummy.proto"})
	rtFile := rtGen.NewGeneratedFile("runtime_only.go", "example.com/rt")
	rtFile.P("package rt")
	rtFile.P()
	emitDorisSharedRuntimeTypes(rtFile)
	rtSrc := stage1aGeneratedFiles(t, rtGen)["runtime_only.go"]
	if err := os.WriteFile(filepath.Join(tmpRuntimeDir, "runtime_only.go"), []byte(rtSrc), 0o644); err != nil {
		t.Fatalf("write runtime_only.go: %v", err)
	}
	rtTest := `package rt
import "testing"
func TestRuntimeIcebergAlterEvolution(t *testing.T) {
	if !IsSafeDorisTypeEvolution("MAP<VARCHAR(32), INT>", "MAP<VARCHAR(64), BIGINT>") {
		t.Fatal("expected MAP widening to pass")
	}
	if IsSafeDorisTypeEvolution("MAP<STRING, BIGINT>", "MAP<STRING, INT>") {
		t.Fatal("expected MAP narrowing to fail")
	}
	before := DorisTableDescriptor{
		TableName: "t1", DatabaseScope: "TENANT", SyncTier: "ICEBERG_ONLY", UniqueKeyColumns: []string{"id"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "id", DorisType: "VARCHAR(64)", Nullable: false, IsUniqueKey: true},
			{ColumnName: "m", DorisType: "VARCHAR(64)", IcebergType: "BIGINT", Nullable: true},
		},
	}
	afterNarrow := before
	afterNarrow.Columns = []DorisColumnDescriptor{
		before.Columns[0],
		{ColumnName: "m", DorisType: "VARCHAR(64)", IcebergType: "INT", Nullable: true},
	}
	if _, err := before.BuildIcebergAlterTableSQL(afterNarrow, "acme"); err == nil {
		t.Fatal("expected runtime BuildIcebergAlterTableSQL to reject IcebergType narrowing BIGINT -> INT")
	}
	afterWiden := afterNarrow
	afterWiden.Columns[1].IcebergType = "INT"
	afterWidenToBig := before
	if stmts, err := afterWiden.BuildIcebergAlterTableSQL(afterWidenToBig, "acme"); err != nil || len(stmts) != 1 {
		t.Fatalf("expected runtime IcebergType widening INT -> BIGINT to emit 1 stmt, got (%v, %v)", stmts, err)
	}
}
`
	if err := os.WriteFile(filepath.Join(tmpRuntimeDir, "runtime_only_test.go"), []byte(rtTest), 0o644); err != nil {
		t.Fatalf("write runtime_only_test.go: %v", err)
	}
	cmd := exec.Command("go", "test", "-v", ".")
	cmd.Dir = tmpRuntimeDir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runtime iceberg evolution test failed: %v\n%s", err, string(out))
	}
}

func TestPR241_DualAnnotatedInferredDorisMetaAndNonLeak(t *testing.T) {
	runner := NewRunner()

	// 1. Inferred Doris table (no Doris column types) must preserve ColumnName & DefaultValue
	// on dual-annotated columns and ignore Postgres-only JSONB map fields cleanly.
	inferredMsg := &descriptorpb.DescriptorProto{
		Name: proto.String("InferredDualRecord"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:        "inferred_dual_records",
			DatabaseScope:    "TENANT",
			SyncTier:         "WHOLE_TABLE",
			UniqueKeyColumns: []string{"record_uuid"},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{
					SqlType:      "UUID",
					ColumnName:   "record_uuid",
					PrimaryKey:   true,
					DefaultValue: proto.String("0000"),
				}, nil)),
			stage1aScalarField("extra_attrs", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
		},
	}
	fdpInf := stage1aSingleMsgFDP("inferred_dual.proto", "review.v1", "example.com/review/v1;reviewv1", inferredMsg)
	genInf := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdpInf}}, []string{"inferred_dual.proto"})
	if err := runner.Generate(genInf, &Options{Lang: "go", Backend: "doris", DorisMode: "ddl"}); err != nil {
		t.Fatalf("inferred dual-annotated Doris generation failed: %v", err)
	}
	infSQL := stage1aFindBySuffix(t, stage1aGeneratedFiles(t, genInf), "_doris.sql")
	if !strings.Contains(infSQL, `record_uuid`) || !strings.Contains(infSQL, `DEFAULT "0000"`) {
		t.Errorf("expected ColumnName=record_uuid and DefaultValue=0000 in inferred Doris DDL:\n%s", infSQL)
	}

	// 2. Annotated Doris table (at least one DorisType column) must NOT leak Postgres-only sibling fields.
	annotatedMsg := &descriptorpb.DescriptorProto{
		Name: proto.String("AnnotatedDualRecord"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:        "annotated_dual_records",
			DatabaseScope:    "TENANT",
			SyncTier:         "WHOLE_TABLE",
			UniqueKeyColumns: []string{"id"},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"}, nil)),
			stage1aScalarField("pg_secret", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{SqlType: "TEXT", ColumnName: "pg_secret_col", DefaultValue: proto.String("pg")}, nil)),
		},
	}
	fdpAnn := stage1aSingleMsgFDP("annotated_dual.proto", "review.v1", "example.com/review/v1;reviewv1", annotatedMsg)
	genAnn := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdpAnn}}, []string{"annotated_dual.proto"})
	if err := runner.Generate(genAnn, &Options{Lang: "go", Backend: "doris", DorisMode: "ddl"}); err != nil {
		t.Fatalf("annotated dual Doris generation failed: %v", err)
	}
	annSQL := stage1aFindBySuffix(t, stage1aGeneratedFiles(t, genAnn), "_doris.sql")
	if strings.Contains(annSQL, "pg_secret") || strings.Contains(annSQL, "pg_secret_col") {
		t.Errorf("Postgres-only column leaked into annotated Doris DDL:\n%s", annSQL)
	}
}

func TestPR241_DorisRegistryStemNormalizationAndNullableValidation(t *testing.T) {
	runner := NewRunner()

	// 1. Unique single hyphenated file name "encounter-log.proto" -> EncounterLogDorisTables.
	hyphenFDP := stage1aSingleMsgFDP("encounter-log.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("EncounterLog"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:        "encounter_logs",
			DatabaseScope:    "TENANT",
			SyncTier:         "WHOLE_TABLE",
			UniqueKeyColumns: []string{"id"},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"}, nil)),
		},
	})
	hyphenGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{hyphenFDP}}, []string{"encounter-log.proto"})
	if err := runner.Generate(hyphenGen, &Options{Lang: "go", Backend: "doris", DorisMode: "go", OutputFile: "consolidated_doris.go"}); err != nil {
		t.Fatalf("consolidated doris Go generation failed for hyphenated stem: %v", err)
	}
	hyphenOut := stage1aGeneratedFiles(t, hyphenGen)["consolidated_doris.go"]
	if !strings.Contains(hyphenOut, "var EncounterLogDorisTables = []DorisTableDescriptor{") {
		t.Errorf("expected EncounterLogDorisTables for encounter-log.proto:\n%s", hyphenOut)
	}

	auditFDP := stage1aSingleMsgFDP("sub/audit-entry.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("AuditEntry"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:        "audit_entries",
			DatabaseScope:    "TENANT",
			SyncTier:         "WHOLE_TABLE",
			UniqueKeyColumns: []string{"id"},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"}, nil)),
		},
	})
	multiHyphenGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{hyphenFDP, auditFDP}}, []string{"encounter-log.proto", "sub/audit-entry.proto"})
	if err := runner.Generate(multiHyphenGen, &Options{Lang: "go", Backend: "doris", DorisMode: "go", OutputFile: "consolidated_multi_doris.go"}); err != nil {
		t.Fatalf("multi consolidated doris Go generation failed for hyphenated stems: %v", err)
	}
	multiHyphenOut := stage1aGeneratedFiles(t, multiHyphenGen)["consolidated_multi_doris.go"]
	for _, want := range []string{"var EncounterLogDorisTables = []DorisTableDescriptor{", "var AuditEntryDorisTables = []DorisTableDescriptor{"} {
		if !strings.Contains(multiHyphenOut, want) {
			t.Errorf("expected %q in multi consolidated doris Go:\n%s", want, multiHyphenOut)
		}
	}

	// 2. Keyless GLOBAL_TERMINOLOGY + RELEASE_PARTITIONED emits only Iceberg output, so
	// doris_nullable=true on partition key must succeed and produce a nullable Iceberg column.
	keylessTermFDP := stage1aSingleMsgFDP("keyless_term.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("KeylessTerm"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:          "keyless_terms",
			DatabaseScope:      "GLOBAL_TERMINOLOGY",
			SyncTier:           "RELEASE_PARTITIONED",
			PartitionByColumns: []string{"release_id"},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("code", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"}, nil)),
			stage1aScalarField("release_id", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)", DorisNullable: proto.Bool(true)}, nil)),
		},
	})
	keylessGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{keylessTermFDP}}, []string{"keyless_term.proto"})
	if err := runner.Generate(keylessGen, &Options{Lang: "go", Backend: "doris", DorisMode: "ddl"}); err != nil {
		t.Fatalf("keyless GLOBAL_TERMINOLOGY + RELEASE_PARTITIONED failed: %v", err)
	}
	iceFiles := stage1aGeneratedFiles(t, keylessGen)
	iceSQL := stage1aFindBySuffix(t, iceFiles, "_iceberg.sql")
	if !strings.Contains(iceSQL, "release_id VARCHAR(64)\n") || strings.Contains(iceSQL, "release_id VARCHAR(64) NOT NULL") {
		t.Errorf("expected nullable release_id in Iceberg DDL:\n%s", iceSQL)
	}

	// 3. Fallback inferred DUPLICATE KEY column with doris_nullable=true must be rejected.
	fallbackDupFDP := stage1aSingleMsgFDP("fallback_dup_null.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("FallbackDupNull"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:     "fallback_dup_null",
			DatabaseScope: "TENANT",
			SyncTier:      "WHOLE_TABLE",
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("row_id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)", DorisNullable: proto.Bool(true)}, nil)),
		},
	})
	fallbackGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fallbackDupFDP}}, []string{"fallback_dup_null.proto"})
	if err := runner.Generate(fallbackGen, &Options{Lang: "go", Backend: "doris", DorisMode: "ddl"}); err == nil {
		t.Errorf("expected error for doris_nullable=true on fallback inferred DUPLICATE KEY column")
	}
}

func TestPR241_PostgresHCLAndDDLFixes(t *testing.T) {
	runner := NewRunner()

	// 1. Long table name auto-generated index/FK names must compact <= 63 bytes BEFORE validation.
	longTableName := "very_long_clinical_workflow_orchestration_audit_event_table_x" // 61 chars (<63)
	longMsg := &descriptorpb.DescriptorProto{
		Name: proto.String("VeryLongTable"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:     longTableName,
			DatabaseScope: "TENANT",
			SyncTier:      "WHOLE_TABLE",
			Indexes: []*proto2typepb.IndexDef{
				{Columns: []string{"status_code"}},
			},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{PrimaryKey: true}, nil)),
			stage1aScalarField("parent_id", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{
					Index:      true,
					ForeignKey: &proto2typepb.ForeignKey{RefTable: "parents", RefColumn: "id"},
				}, nil)),
			stage1aScalarField("email", 3, descriptorpb.FieldDescriptorProto_TYPE_STRING, false,
				stage1aColOpts(&proto2typepb.ColumnOptions{Unique: true}, nil)),
			stage1aScalarField("status_code", 4, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
		},
	}
	longFDP := stage1aSingleMsgFDP("long_tbl.proto", "review.v1", "example.com/review/v1;reviewv1", longMsg)
	longGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{longFDP}}, []string{"long_tbl.proto"})
	if err := runner.Generate(longGen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "all"}); err != nil {
		t.Fatalf("long table Generate failed: %v", err)
	}
	longFiles := stage1aGeneratedFiles(t, longGen)
	longSQL := stage1aFindBySuffix(t, longFiles, "_postgres.sql")
	longHCL := stage1aFindBySuffix(t, longFiles, "_postgres.hcl")
	if !strings.Contains(longSQL, "CREATE TABLE IF NOT EXISTS") || !strings.Contains(longSQL, "FOREIGN KEY (parent_id)") {
		t.Fatalf("long table SQL unexpectedly empty or missing FK:\n%s", longSQL)
	}
	if _, err := ParsePostgresAtlasHCL(longHCL); err != nil {
		t.Fatalf("long table HCL failed round-trip parse: %v\n%s", err, longHCL)
	}

	// 2. Consolidated lakehouse DDL emits all CREATE TABLEs before all FOREIGN KEY constraints.
	childMsg := &descriptorpb.DescriptorProto{
		Name: proto.String("ChildFirst"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:     "child_first",
			DatabaseScope: "TENANT",
			SyncTier:      "WHOLE_TABLE",
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{PrimaryKey: true}, nil)),
			stage1aScalarField("parent_id", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{
				ForeignKey: &proto2typepb.ForeignKey{RefTable: "parent_second", RefColumn: "id"},
			}, nil)),
		},
	}
	parentMsg := &descriptorpb.DescriptorProto{
		Name: proto.String("ParentSecond"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:     "parent_second",
			DatabaseScope: "TENANT",
			SyncTier:      "WHOLE_TABLE",
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{PrimaryKey: true}, nil)),
		},
	}
	fkOrderFDP := &descriptorpb.FileDescriptorProto{
		Syntax:      proto.String("proto3"),
		Name:        proto.String("fk_order.proto"),
		Package:     proto.String("review.v1"),
		Options:     &descriptorpb.FileOptions{GoPackage: proto.String("example.com/review/v1;reviewv1")},
		MessageType: []*descriptorpb.DescriptorProto{childMsg, parentMsg},
	}
	fkOrderGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fkOrderFDP}}, []string{"fk_order.proto"})
	if err := runner.Generate(fkOrderGen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"}); err != nil {
		t.Fatalf("fkOrder Generate failed: %v", err)
	}
	fkOrderSQL := stage1aFindBySuffix(t, stage1aGeneratedFiles(t, fkOrderGen), "_postgres.sql")
	parentIdx := strings.Index(fkOrderSQL, "CREATE TABLE IF NOT EXISTS tenant_${TENANT_SLUG}.parent_second")
	fkIdx := strings.Index(fkOrderSQL, "ADD CONSTRAINT fk_child_first_parent_id FOREIGN KEY")
	if parentIdx < 0 || fkIdx < 0 || fkIdx < parentIdx {
		t.Errorf("expected FK constraint after all CREATE TABLE statements:\n%s", fkOrderSQL)
	}

	// 3. ParsePostgresAtlasHCL allows index/foreign_key/primary_key declared BEFORE column blocks,
	// and explicitly ordered GIN indexes do not emit duplicate synthetic GIN indexes.
	reorderedHCL := `schema "public" {}

table "reordered_tbl" {
  schema = schema.public
  primary_key {
    columns = [column.id]
  }
  index "idx_tags_custom" {
    on {
      column = column.tags
      ops    = array_ops
    }
    type = "GIN"
  }
  foreign_key "fk_reordered_parent" {
    columns     = [column.parent_id]
    ref_columns = [table.parents.column.id]
  }
  column "id" {
    type = uuid
    null = false
  }
  column "parent_id" {
    type = uuid
    null = false
  }
  column "tags" {
    type = sql("text[]")
    null = false
  }
  column "note" {
    type    = text
    null    = false
    default = "  keep spaces  "
  }
}`
	parsedReordered, err := ParsePostgresAtlasHCL(reorderedHCL)
	if err != nil {
		t.Fatalf("ParsePostgresAtlasHCL failed on indexes/FKs declared before columns: %v", err)
	}
	if len(parsedReordered) != 1 || len(parsedReordered[0].InvertedIndexColumns) != 0 {
		t.Fatalf("expected explicit ordered GIN index not to populate InvertedIndexColumns, got %+v", parsedReordered[0].InvertedIndexColumns)
	}
	reemittedSQL := parsedReordered[0].BuildPostgresCreateTableSQL("")
	if strings.Contains(reemittedSQL, "idx_reordered_tbl_tags") {
		t.Errorf("unexpected duplicate synthetic GIN index in:\n%s", reemittedSQL)
	}
	if !strings.Contains(reemittedSQL, "DEFAULT '  keep spaces  '") {
		t.Errorf("expected leading/trailing whitespace in default literal to be preserved:\n%s", reemittedSQL)
	}

	// 4. ServerDefault takes consistent precedence over DefaultValue across HCL, CREATE, and ALTER.
	srvTbl := PostgresTableDescriptor{
		TableName:         "srv_def_tbl",
		DatabaseScope:     "PUBLIC",
		PrimaryKeyColumns: []string{"id"},
		Columns: []PostgresColumnDescriptor{
			{ColumnName: "id", SQLType: "UUID", PostgresType: "UUID", HCLType: "uuid", IsPrimaryKey: true},
			{ColumnName: "created_at", SQLType: "TIMESTAMPTZ", PostgresType: "TIMESTAMPTZ", HCLType: "timestamptz", HasDefaultValue: true, DefaultValue: "1970-01-01", ServerDefault: "NOW()"},
		},
	}
	srvSQL := srvTbl.BuildPostgresCreateTableSQL("")
	srvHCL := srvTbl.BuildPostgresAtlasHCL("")
	if !strings.Contains(srvSQL, "created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()") || !strings.Contains(srvHCL, `default = sql("now()")`) {
		t.Errorf("expected ServerDefault precedence in SQL & HCL:\nSQL:\n%s\nHCL:\n%s", srvSQL, srvHCL)
	}
	srvAlterNoop := srvTbl
	srvAlterNoop.Columns = append([]PostgresColumnDescriptor(nil), srvTbl.Columns...)
	srvAlterNoop.Columns[1].HasDefaultValue = false
	srvAlterNoop.Columns[1].DefaultValue = ""
	if stmts, err := BuildPostgresAlterTableSQL(srvTbl, srvAlterNoop, ""); err != nil || len(stmts) != 0 {
		t.Errorf("expected no alter drift when ServerDefault wins over DefaultValue, got (%v, %v)", stmts, err)
	}

	// 5. Index diffing in BuildPostgresAlterTableSQL: modified same-named index drops and recreates.
	idxBefore := PostgresTableDescriptor{
		TableName:         "idx_diff_tbl",
		DatabaseScope:     "PUBLIC",
		PrimaryKeyColumns: []string{"id"},
		Columns: []PostgresColumnDescriptor{
			{ColumnName: "id", SQLType: "UUID", PostgresType: "UUID", IsPrimaryKey: true},
			{ColumnName: "email", SQLType: "TEXT", PostgresType: "TEXT", Nullable: false},
		},
		Indexes: []PostgresIndexDescriptor{
			{Name: "idx_email_dyn", Columns: []string{"email"}, Using: "BTREE", Unique: false},
		},
	}
	idxAfter := idxBefore
	idxAfter.Indexes = []PostgresIndexDescriptor{
		{Name: "idx_email_dyn", Columns: []string{"email"}, Using: "BTREE", Unique: true, Where: "email <> ''"},
	}
	diffStmts, err := BuildPostgresAlterTableSQL(idxBefore, idxAfter, "")
	if err != nil {
		t.Fatalf("BuildPostgresAlterTableSQL index diff failed: %v", err)
	}
	joinedDiff := strings.Join(diffStmts, "\n")
	if !strings.Contains(joinedDiff, "DROP INDEX IF EXISTS public.idx_email_dyn;") ||
		!strings.Contains(joinedDiff, "CREATE UNIQUE INDEX IF NOT EXISTS idx_email_dyn ON public.idx_diff_tbl (email) WHERE email <> '';") {
		t.Errorf("expected DROP + CREATE for modified index definition, got:\n%s", joinedDiff)
	}

	// 6. Partitioned singleton must be rejected across relational, lakehouse IR, and ValidateDescriptor.
	for _, scope := range []struct {
		name string
		lake bool
	}{
		{"relational", false},
		{"lakehouse", true},
	} {
		tblOpts := &proto2typepb.TableOptions{
			TableName:          "bad_part_singleton_" + scope.name,
			IsSingleton:        true,
			PartitionByColumns: []string{"event_month"},
		}
		if scope.lake {
			tblOpts.DatabaseScope = "TENANT"
			tblOpts.SyncTier = "MONTHLY_PARTITIONED"
		}
		badFDP := stage1aSingleMsgFDP("bad_part_singleton_"+scope.name+".proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
			Name:    proto.String("BadPartSingleton"),
			Options: stage1aMsgOpts(tblOpts),
			Field: []*descriptorpb.FieldDescriptorProto{
				stage1aScalarField("event_month", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
			},
		})
		g := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{badFDP}}, []string{"bad_part_singleton_" + scope.name + ".proto"})
		if err := runner.Generate(g, &Options{Lang: "go", Backend: "postgres", PostgresMode: "hcl"}); err == nil {
			t.Errorf("%s: expected error for is_singleton=true combined with partition_by_columns", scope.name)
		}
	}
	if validatePostgresDescriptor(PostgresTableDescriptor{
		TableName:          "bad_desc",
		IsSingleton:        true,
		PartitionByColumns: []string{"event_month"},
		Columns: []PostgresColumnDescriptor{
			{ColumnName: "singleton", PostgresType: "BOOLEAN", IsPrimaryKey: true},
			{ColumnName: "event_month", PostgresType: "TEXT", IsPartitionKey: true},
		},
	}) {
		t.Errorf("expected validatePostgresDescriptor to reject IsSingleton + PartitionByColumns")
	}

	// 7. Relational: synthetic singleton column materialized before index validation + partial unique on PK kept.
	relSingletonIdxFDP := stage1aSingleMsgFDP("rel_singleton_idx.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("RelSingletonIdx"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:   "rel_singleton_idx",
			IsSingleton: true,
			Indexes: []*proto2typepb.IndexDef{
				{Name: "idx_singleton_col", Columns: []string{"singleton"}},
			},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("val", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
		},
	})
	relSingletonGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{relSingletonIdxFDP}}, []string{"rel_singleton_idx.proto"})
	if err := runner.Generate(relSingletonGen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"}); err != nil {
		t.Fatalf("relational singleton with index on synthetic singleton failed: %v", err)
	}

	partialUniquePKFDP := stage1aSingleMsgFDP("partial_pk_uq.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("PartialPKUnique"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName: "partial_pk_uq",
			Indexes: []*proto2typepb.IndexDef{
				{Columns: []string{"id"}, Unique: true, Where: "deleted_at IS NULL"},
			},
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
			stage1aScalarField("deleted_at", 2, descriptorpb.FieldDescriptorProto_TYPE_INT64, false, nil),
		},
	})
	partialPKGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{partialUniquePKFDP}}, []string{"partial_pk_uq.proto"})
	if err := runner.Generate(partialPKGen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"}); err != nil {
		t.Fatalf("partial unique PK index failed: %v", err)
	}
	partialPKSQL := stage1aFindBySuffix(t, stage1aGeneratedFiles(t, partialPKGen), "_postgres.sql")
	if !strings.Contains(partialPKSQL, `CREATE UNIQUE INDEX IF NOT EXISTS "uq_partial_pk_uq_id" ON "partial_pk_uq" ("id") WHERE deleted_at IS NULL;`) {
		t.Errorf("expected partial unique index on single-col PK NOT to be discarded:\n%s", partialPKSQL)
	}

	// 8. Relational: unsafe vc.In CHECK expression must return error instead of silently dropping table.
	badInOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(badInOpts, validate.E_Field, &validate.FieldRules{
		Type: &validate.FieldRules_String_{
			String_: &validate.StringRules{
				In: []string{"ACTIVE", "BAD; DROP TABLE x"},
			},
		},
	})
	badInFDP := stage1aSingleMsgFDP("bad_in_chk.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name:    proto.String("BadInCheck"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: "bad_in_check"}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
			stage1aScalarField("status", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, badInOpts),
		},
	})
	badInGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{badInFDP}}, []string{"bad_in_chk.proto"})
	if err := runner.Generate(badInGen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"}); err == nil {
		t.Errorf("expected error for invalid vc.In CHECK constraint expression")
	}

	// 9. parsePostgresHCLSQLDefault must reject lossy/mismatched casts.
	for _, badHCL := range []string{
		`table "t1" {
  column "s" {
    type = text
    null = false
    default = sql("'hello world'::varchar(5)")
  }
}`,
		`table "t1" {
  column "tags" {
    type = sql("text[]")
    null = false
    default = sql("'{}'::uuid[]")
  }
}`,
	} {
		if _, err := ParsePostgresAtlasHCL(badHCL); err == nil {
			t.Errorf("expected ParsePostgresAtlasHCL to reject mismatched/lossy SQL cast:\n%s", badHCL)
		}
	}

	// 10. Reject operator classes incompatible with implicit/explicit B-tree when USING is omitted.
	for _, scope := range []bool{false, true} {
		tblOpts := &proto2typepb.TableOptions{
			TableName: "bad_btree_ops",
			Indexes: []*proto2typepb.IndexDef{
				{
					Name:  "idx_bad_ops",
					Parts: []*proto2typepb.IndexDef_IndexPart{{Column: "title", Opclass: "gin_trgm_ops"}},
				},
			},
		}
		if scope {
			tblOpts.DatabaseScope = "TENANT"
			tblOpts.SyncTier = "WHOLE_TABLE"
		}
		badOpsFDP := stage1aSingleMsgFDP("bad_ops.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
			Name:    proto.String("BadOps"),
			Options: stage1aMsgOpts(tblOpts),
			Field: []*descriptorpb.FieldDescriptorProto{
				stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
				stage1aScalarField("title", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
			},
		})
		g := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{badOpsFDP}}, []string{"bad_ops.proto"})
		if err := runner.Generate(g, &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"}); err == nil {
			t.Errorf("lakehouse=%v: expected error for gin_trgm_ops with implicit BTREE", scope)
		}
	}
	if _, err := ParsePostgresAtlasHCL(`table "t1" {
  column "tags" {
    type = sql("text[]")
    null = false
  }
  index "idx_bad" {
    on {
      column = column.tags
      ops    = array_ops
    }
  }
}`); err == nil {
		t.Errorf("expected ParsePostgresAtlasHCL to reject array_ops with implicit BTREE")
	}
}
