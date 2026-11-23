package generator

import (
	"go/parser"
	"go/token"
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

	// Narrowing inside MAP value type and any MAP key type change must also error.
	narrowMap := baseTbl
	narrowMap.Columns = append([]DorisColumnDescriptor(nil), baseTbl.Columns...)
	narrowMap.Columns[3].DorisType = "MAP<VARCHAR(32), SMALLINT>"
	if stmts, err := baseTbl.BuildIcebergAlterTableSQL(narrowMap, "acme"); err == nil {
		t.Fatalf("expected error on MAP value narrowing INT -> SMALLINT, got stmts=%v", stmts)
	}
	if IsSafeDorisTypeEvolution("MAP<STRING, BIGINT>", "MAP<STRING, INT>") {
		t.Fatal("expected MAP<STRING, BIGINT> -> MAP<STRING, INT> to be unsafe")
	}
	if IsSafeDorisTypeEvolution("MAP<INT, INT>", "MAP<BIGINT, BIGINT>") {
		t.Fatal("expected MAP<INT, INT> -> MAP<BIGINT, BIGINT> key widening to be unsafe")
	}
	if !IsSafeDorisTypeEvolution("MAP<INT, INT>", "MAP<INT, BIGINT>") {
		t.Fatal("expected MAP<INT, INT> -> MAP<INT, BIGINT> value widening to be safe")
	}
	keyChangeMap := baseTbl
	keyChangeMap.Columns = append([]DorisColumnDescriptor(nil), baseTbl.Columns...)
	keyChangeMap.Columns[3].DorisType = "MAP<INT, INT>"
	keyChangeMapAfter := baseTbl
	keyChangeMapAfter.Columns = append([]DorisColumnDescriptor(nil), baseTbl.Columns...)
	keyChangeMapAfter.Columns[3].DorisType = "MAP<BIGINT, BIGINT>"
	if stmts, err := keyChangeMap.BuildIcebergAlterTableSQL(keyChangeMapAfter, "acme"); err == nil {
		t.Fatalf("expected BuildIcebergAlterTableSQL to reject MAP key change MAP<INT, INT> -> MAP<BIGINT, BIGINT>, got stmts=%v", stmts)
	}
	valWidenMapAfter := baseTbl
	valWidenMapAfter.Columns = append([]DorisColumnDescriptor(nil), baseTbl.Columns...)
	valWidenMapAfter.Columns[3].DorisType = "MAP<INT, BIGINT>"
	if stmts, err := keyChangeMap.BuildIcebergAlterTableSQL(valWidenMapAfter, "acme"); err != nil || len(stmts) != 1 || !strings.Contains(stmts[0], "MODIFY COLUMN attrs MAP<INT, BIGINT>;") {
		t.Fatalf("expected MAP value widening MAP<INT, INT> -> MAP<INT, BIGINT> to emit MODIFY COLUMN, got (%v, %v)", stmts, err)
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
	if IsSafeDorisTypeEvolution("MAP<VARCHAR(32), INT>", "MAP<VARCHAR(64), BIGINT>") {
		t.Fatal("expected MAP key widening to fail")
	}
	if !IsSafeDorisTypeEvolution("MAP<VARCHAR(32), INT>", "MAP<VARCHAR(32), BIGINT>") {
		t.Fatal("expected MAP value-only widening to pass")
	}
	if IsSafeDorisTypeEvolution("MAP<INT, INT>", "MAP<BIGINT, BIGINT>") {
		t.Fatal("expected MAP<INT, INT> -> MAP<BIGINT, BIGINT> key widening to fail")
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
	if len(diffStmts) != 2 ||
		diffStmts[0] != "DROP INDEX IF EXISTS public.idx_email_dyn;" ||
		diffStmts[1] != "CREATE UNIQUE INDEX IF NOT EXISTS idx_email_dyn ON public.idx_diff_tbl (email) WHERE email <> '';" {
		t.Errorf("expected exact [DROP, CREATE] for modified index definition, got:\n%v", diffStmts)
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
	parsedArrayOps, err := ParsePostgresAtlasHCL(`table "t1" {
  column "tags" {
    type = sql("text[]")
    null = false
  }
  index "idx_ok" {
    on {
      column = column.tags
      ops    = array_ops
    }
  }
}`)
	if err != nil || len(parsedArrayOps) != 1 || len(parsedArrayOps[0].Indexes) != 1 || len(parsedArrayOps[0].Indexes[0].Parts) != 1 || parsedArrayOps[0].Indexes[0].Parts[0].Opclass != "array_ops" {
		t.Errorf("expected ParsePostgresAtlasHCL to accept array_ops with implicit BTREE, got (%+v, %v)", parsedArrayOps, err)
	}
	if _, err := ParsePostgresAtlasHCL(`table "t1" {
  column "title" {
    type = text
    null = false
  }
  index "idx_bad" {
    on {
      column = column.title
      ops    = gin_trgm_ops
    }
  }
}`); err == nil {
		t.Errorf("expected ParsePostgresAtlasHCL to reject gin_trgm_ops with implicit BTREE")
	}
}

func TestPR241_AdditionalCodeRabbitFixes(t *testing.T) {
	// 1 & 3 & 4 & 5: Host + emitted Go runtime lockstep for Iceberg HCL/DDL types,
	// StarRocks AutoBuckets omission, and HCL template escape decoding before strconv.Unquote.
	if got := dorisColumnIcebergHCLType("TEXT", "VARCHAR(64)"); got != "varchar(64)" {
		t.Errorf("dorisColumnIcebergHCLType(TEXT, VARCHAR(64)) = %q, want varchar(64)", got)
	}
	if got := dorisColumnIcebergHCLType("JSONB", "VARCHAR(128)"); got != "varchar(128)" {
		t.Errorf("dorisColumnIcebergHCLType(JSONB, VARCHAR(128)) = %q, want varchar(128)", got)
	}
	if got := dorisColumnIcebergHCLType("TEXT", ""); got != "string" {
		t.Errorf("dorisColumnIcebergHCLType(TEXT, '') = %q, want string", got)
	}
	if got := dorisColumnIcebergHCLType("TEXT", "STRING"); got != "string" {
		t.Errorf("dorisColumnIcebergHCLType(TEXT, STRING) = %q, want string", got)
	}
	if got := dorisColumnIcebergHCLType("TEXT", "ARRAY<LONG>"); got != "array<bigint>" {
		t.Errorf("dorisColumnIcebergHCLType(TEXT, ARRAY<LONG>) = %q, want array<bigint>", got)
	}
	if got := dorisColumnIcebergDDLType("", "ARRAY<LONG>"); got != "ARRAY<BIGINT>" {
		t.Errorf("dorisColumnIcebergDDLType('', ARRAY<LONG>) = %q, want ARRAY<BIGINT>", got)
	}
	if got := dorisColumnIcebergDDLType("", "ARRAY<SMALLINT>"); got != "ARRAY<INT>" {
		t.Errorf("dorisColumnIcebergDDLType('', ARRAY<SMALLINT>) = %q, want ARRAY<INT>", got)
	}
	if got := dorisColumnIcebergDDLType("", "ARRAY<VARCHAR(64)>"); got != "ARRAY<VARCHAR(64)>" {
		t.Errorf("dorisColumnIcebergDDLType('', ARRAY<VARCHAR(64)>) = %q, want ARRAY<VARCHAR(64)>", got)
	}
	for _, tc := range []struct {
		doris   string
		iceberg string
		want    string
	}{
		{"ARRAY<TEXT>", "", "ARRAY<STRING>"},
		{"ARRAY<TEXT>", "ARRAY<STRING>", "ARRAY<STRING>"},
		{"MAP<TEXT, BIGINT>", "MAP<STRING, BIGINT>", "MAP<STRING, BIGINT>"},
		{"", "ARRAY<LONG>", "ARRAY<BIGINT>"},
		{"", "LIST<STRING>", "ARRAY<VARCHAR(64)>"},
		{"", "STRING", "VARCHAR(256)"},
	} {
		if got := dorisColumnIcebergDDLType(tc.doris, tc.iceberg); got != tc.want {
			t.Errorf("dorisColumnIcebergDDLType(%q, %q) = %q, want %q", tc.doris, tc.iceberg, got, tc.want)
		}
	}

	if got := unquoteHCLString(`"$${tenant}"`); got != "${tenant}" {
		t.Errorf("unquoteHCLString raw $${tenant} = %q, want ${tenant}", got)
	}
	if got := unquoteHCLString(`"\u0024\u0024{tenant}"`); got != "$${tenant}" {
		t.Errorf("unquoteHCLString \\u0024\\u0024{tenant} = %q, want $${tenant}", got)
	}
	if got := unquoteHCLString(`"%%{directive}"`); got != "%{directive}" {
		t.Errorf("unquoteHCLString raw %%{directive} = %q, want %%{directive}", got)
	}
	if got := unquoteHCLString(`"\u0025\u0025{directive}"`); got != "%%{directive}" {
		t.Errorf("unquoteHCLString \\u0025\\u0025{directive} = %q, want %%%% {directive}", got)
	}
	if got := unquoteHCLString(`"$${a}\n\u0024\u0024{b}"`); got != "${a}\n$${b}" {
		t.Errorf("unquoteHCLString mixed = %q, want ${a}\\n$${b}", got)
	}
	parsedEscPG, err := ParsePostgresAtlasHCL(`table "esc_tbl" {
  schema = schema.public
  column "id" {
    type = uuid
    null = false
  }
  column "u_col" {
    type    = text
    null    = false
    default = "\u0024\u0024{tenant}"
  }
  column "raw_col" {
    type    = text
    null    = false
    default = "$${tenant}"
  }
  primary_key {
    columns = [column.id]
  }
}`)
	if err != nil {
		t.Fatalf("ParsePostgresAtlasHCL failed: %v", err)
	}
	if parsedEscPG[0].Columns[1].DefaultValue != "$${tenant}" || parsedEscPG[0].Columns[2].DefaultValue != "${tenant}" {
		t.Errorf("unexpected Postgres HCL unquoted defaults: u_col=%q raw_col=%q", parsedEscPG[0].Columns[1].DefaultValue, parsedEscPG[0].Columns[2].DefaultValue)
	}

	// Compiled Go runtime parity check for items 1, 3, 4, 5.
	tmpRT := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpRT, "go.mod"), []byte("module example.com/rt2\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	rtFDP := stage1aSingleMsgFDP("rt2.proto", "rt2.v1", "example.com/rt2/v1;rt2v1", &descriptorpb.DescriptorProto{
		Name:    proto.String("Rt2Msg"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: "rt2_tbl"}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
		},
	})
	rtGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{rtFDP}}, []string{"rt2.proto"})
	rtG := rtGen.NewGeneratedFile("rt_all.go", "example.com/rt2")
	rtG.P("package rt2")
	rtG.P()
	emitDorisSharedRuntimeTypes(rtG)
	rtSrc := stage1aGeneratedFiles(t, rtGen)["rt_all.go"]
	if err := os.WriteFile(filepath.Join(tmpRT, "rt_all.go"), []byte(rtSrc), 0o644); err != nil {
		t.Fatalf("write rt_all.go: %v", err)
	}
	rtTest := `package rt2
import (
	"strings"
	"testing"
)
func TestRuntimeLockstepFixes(t *testing.T) {
	if got := dorisColumnIcebergHCLType("TEXT", "VARCHAR(64)"); got != "varchar(64)" {
		t.Fatalf("runtime dorisColumnIcebergHCLType(TEXT, VARCHAR(64)) = %q", got)
	}
	if got := dorisColumnIcebergHCLType("TEXT", ""); got != "string" {
		t.Fatalf("runtime dorisColumnIcebergHCLType(TEXT, '') = %q", got)
	}
	if got := dorisColumnIcebergDDLType("", "ARRAY<LONG>"); got != "ARRAY<BIGINT>" {
		t.Fatalf("runtime dorisColumnIcebergDDLType('', ARRAY<LONG>) = %q", got)
	}
	if got := dorisColumnIcebergDDLType("", "ARRAY<VARCHAR(64)>"); got != "ARRAY<VARCHAR(64)>" {
		t.Fatalf("runtime dorisColumnIcebergDDLType('', ARRAY<VARCHAR(64)>) = %q", got)
	}
	for _, tc := range []struct {
		doris, iceberg, want string
	}{
		{"ARRAY<TEXT>", "", "ARRAY<STRING>"},
		{"ARRAY<TEXT>", "ARRAY<STRING>", "ARRAY<STRING>"},
		{"MAP<TEXT, BIGINT>", "MAP<STRING, BIGINT>", "MAP<STRING, BIGINT>"},
		{"", "ARRAY<LONG>", "ARRAY<BIGINT>"},
		{"", "LIST<STRING>", "ARRAY<VARCHAR(64)>"},
		{"", "STRING", "VARCHAR(256)"},
	} {
		if got := dorisColumnIcebergDDLType(tc.doris, tc.iceberg); got != tc.want {
			t.Fatalf("runtime dorisColumnIcebergDDLType(%q, %q) = %q, want %q", tc.doris, tc.iceberg, got, tc.want)
		}
	}
	if IsSafeDorisTypeEvolution("MAP<INT, INT>", "MAP<BIGINT, BIGINT>") {
		t.Fatal("runtime IsSafeDorisTypeEvolution should reject MAP<INT, INT> -> MAP<BIGINT, BIGINT>")
	}
	if !IsSafeDorisTypeEvolution("MAP<INT, INT>", "MAP<INT, BIGINT>") {
		t.Fatal("runtime IsSafeDorisTypeEvolution should accept MAP<INT, INT> -> MAP<INT, BIGINT>")
	}
	if got := unquoteHCLString(` + "`" + `"\u0024\u0024{tenant}"` + "`" + `); got != "$${tenant}" {
		t.Fatalf("runtime unquoteHCLString unicode = %q", got)
	}
	if got := unquoteHCLString(` + "`" + `"$${tenant}"` + "`" + `); got != "${tenant}" {
		t.Fatalf("runtime unquoteHCLString raw = %q", got)
	}
	sr := DorisTableDescriptor{
		TableName: "sr_auto", DatabaseScope: "TENANT", SyncTier: "WHOLE_TABLE",
		UniqueKeyColumns: []string{"id"}, AutoBuckets: true,
		Columns: []DorisColumnDescriptor{
			{ColumnName: "id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
		},
	}
	sqlAuto := sr.BuildStarRocksCreateTableSQL("acme", 0)
	if !strings.Contains(sqlAuto, "DISTRIBUTED BY HASH(id)\n") || strings.Contains(sqlAuto, "BUCKETS") {
		t.Fatalf("runtime StarRocks AutoBuckets should omit BUCKETS clause:\n%s", sqlAuto)
	}
	sqlNum := sr.BuildStarRocksCreateTableSQL("acme", 8)
	if !strings.Contains(sqlNum, "DISTRIBUTED BY HASH(id) BUCKETS 8\n") {
		t.Fatalf("runtime StarRocks explicit buckets=8 failed:\n%s", sqlNum)
	}
}
`
	if err := os.WriteFile(filepath.Join(tmpRT, "rt_all_test.go"), []byte(rtTest), 0o644); err != nil {
		t.Fatalf("write rt_all_test.go: %v", err)
	}
	cmd := exec.Command("go", "test", "-v", ".")
	cmd.Dir = tmpRT
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compiled runtime parity test failed: %v\n%s", err, string(out))
	}

	// 2: generateGoDomain honors opts.OutputFile and skips adjustSubdirFilename when OutputFile is set.
	runner := NewRunner()
	domFDP := stage1aSingleMsgFDP("pkg/item.proto", "dom.v1", "example.com/pkg;pkgv1", &descriptorpb.DescriptorProto{
		Name: proto.String("Item"),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
		},
	})
	domGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{domFDP}}, []string{"pkg/item.proto"})
	if err := runner.Generate(domGen, &Options{
		Lang:       "go",
		Domain:     true,
		GoPackage:  "example.com/pkg/domain;domain",
		OutputFile: "custom/domain_items.go",
	}); err != nil {
		t.Fatalf("Runner.Generate Go domain with OutputFile failed: %v", err)
	}
	domFiles := stage1aGeneratedFiles(t, domGen)
	if _, ok := domFiles["custom/domain_items.go"]; !ok || len(domFiles) != 1 {
		t.Fatalf("expected single file custom/domain_items.go, got %v", domFiles)
	}
	domDefaultGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{domFDP}}, []string{"pkg/item.proto"})
	if err := runner.Generate(domDefaultGen, &Options{
		Lang:      "go",
		Domain:    true,
		GoPackage: "example.com/pkg/domain;domain",
	}); err != nil {
		t.Fatalf("Runner.Generate Go domain default filename failed: %v", err)
	}
	domDefaultFiles := stage1aGeneratedFiles(t, domDefaultGen)
	if _, ok := domDefaultFiles["example.com/pkg/domain/item.type.go"]; !ok {
		t.Fatalf("expected subdir-adjusted example.com/pkg/domain/item.type.go when OutputFile is empty, got %v", domDefaultFiles)
	}

	// 6: Runner.Generate resets per-compilation r.goGen state across calls (even after errors).
	fdsSearch := buildFileDescriptorSet(t)
	searchOpts := &Options{
		Lang:                 "go",
		Backend:              "postgres",
		PostgresMode:         "search_index",
		PostgresSearchParams: "../testdata/proto/fhir_search_params.json",
		Domain:               false,
	}
	searchGen1 := newPlugin(t, fdsSearch, []string{"fhir_patient.proto"})
	if err := runner.Generate(searchGen1, searchOpts); err != nil {
		t.Fatalf("first Runner.Generate search_index failed: %v", err)
	}
	searchOut1 := extractPluginOutput(t, searchGen1, "_search_index.go")
	if !strings.Contains(searchOut1, "type SearchIndexes struct") {
		t.Fatalf("first search_index run missing SearchIndexes struct")
	}
	searchGen2 := newPlugin(t, fdsSearch, []string{"fhir_patient.proto"})
	if err := runner.Generate(searchGen2, searchOpts); err != nil {
		t.Fatalf("second Runner.Generate search_index failed: %v", err)
	}
	searchOut2 := extractPluginOutput(t, searchGen2, "_search_index.go")
	if !strings.Contains(searchOut2, "type SearchIndexes struct") {
		t.Fatalf("reused Runner omitted SearchIndexes struct on second Generate call")
	}
	// Error during partial compilation must not poison subsequent Runner.Generate calls.
	errPart1 := stage1aSingleMsgFDP("err1.proto", "err.v1", "example.com/err/v1;errv1", &descriptorpb.DescriptorProto{
		Name:    proto.String("GoodFirst"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: "reused_tbl"}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
		},
	})
	errPart2 := stage1aSingleMsgFDP("err2.proto", "err.v1", "example.com/err/v1;errv1", &descriptorpb.DescriptorProto{
		Name:    proto.String("BadSecond"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: "bad;tbl"}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
		},
	})
	errGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{errPart1, errPart2}}, []string{"err1.proto", "err2.proto"})
	if err := runner.Generate(errGen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"}); err == nil {
		t.Fatalf("expected error on second file with invalid table name")
	}
	retryGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{errPart1}}, []string{"err1.proto"})
	if err := runner.Generate(retryGen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"}); err != nil {
		t.Fatalf("expected clean Runner.Generate after earlier failed Generate, got: %v", err)
	}

	// 7: Consolidated Doris/StarRocks collision ignores IcebergCatalog while Iceberg-only keeps IcebergCatalog.
	mkDorisCatFDP := func(fileName, msgName, syncTier, catalog string) *descriptorpb.FileDescriptorProto {
		return stage1aSingleMsgFDP(fileName, "cat.v1", "example.com/cat/v1;catv1", &descriptorpb.DescriptorProto{
			Name: proto.String(msgName),
			Options: stage1aMsgOpts(&proto2typepb.TableOptions{
				TableName:        "shared_events",
				DatabaseScope:    "TENANT",
				SyncTier:         syncTier,
				UniqueKeyColumns: []string{"id"},
				IcebergCatalog:   catalog,
			}),
			Field: []*descriptorpb.FieldDescriptorProto{
				stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"}, nil)),
			},
		})
	}
	dorisCatFDS := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			mkDorisCatFDP("cat_a.proto", "EventA", "WHOLE_TABLE", "cat_a"),
			mkDorisCatFDP("cat_b.proto", "EventB", "WHOLE_TABLE", "cat_b"),
		},
	}
	if err := runner.Generate(newPlugin(t, dorisCatFDS, []string{"cat_a.proto", "cat_b.proto"}), &Options{
		Lang: "go", Backend: "doris", DorisMode: "starrocks_ddl", Domain: false, OutputFile: "sr.sql",
	}); err == nil {
		t.Errorf("expected StarRocks consolidated collision across different IcebergCatalogs")
	}
	if err := runner.Generate(newPlugin(t, dorisCatFDS, []string{"cat_a.proto", "cat_b.proto"}), &Options{
		Lang: "go", Backend: "doris", DorisMode: "go", Domain: false, OutputFile: "doris_all.go",
	}); err == nil {
		t.Errorf("expected Doris consolidated Go collision across different IcebergCatalogs")
	}
	iceOnlyCatFDS := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			mkDorisCatFDP("ice_a.proto", "IceEventA", "ICEBERG_ONLY", "cat_a"),
			mkDorisCatFDP("ice_b.proto", "IceEventB", "ICEBERG_ONLY", "cat_b"),
		},
	}
	iceCatGen := newPlugin(t, iceOnlyCatFDS, []string{"ice_a.proto", "ice_b.proto"})
	if err := runner.Generate(iceCatGen, &Options{
		Lang: "go", Backend: "doris", DorisMode: "ddl", Domain: false, OutputFile: "ice.sql",
	}); err != nil {
		t.Fatalf("expected ICEBERG_ONLY tables across different IcebergCatalogs to succeed, got: %v", err)
	}
	iceCatSQL := stage1aGeneratedFiles(t, iceCatGen)["ice.sql"]
	if !strings.Contains(iceCatSQL, "cat_a.tenant_${TENANT_SLUG}.shared_events") || !strings.Contains(iceCatSQL, "cat_b.tenant_${TENANT_SLUG}.shared_events") {
		t.Errorf("expected both Iceberg catalogs in consolidated ice.sql:\n%s", iceCatSQL)
	}

	// 8: Lakehouse Postgres table collisions checked by (resolvedSchema, tableName).
	mkPGScopeFDP := func(fileName, msgName, scope, syncTier string) *descriptorpb.FileDescriptorProto {
		return stage1aSingleMsgFDP(fileName, "pgs.v1", "example.com/pgs/v1;pgsv1", &descriptorpb.DescriptorProto{
			Name: proto.String(msgName),
			Options: stage1aMsgOpts(&proto2typepb.TableOptions{
				TableName:        "shared_pg_events",
				DatabaseScope:    scope,
				SyncTier:         syncTier,
				UniqueKeyColumns: []string{"id"},
			}),
			Field: []*descriptorpb.FieldDescriptorProto{
				stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{SqlType: "UUID"}, nil)),
			},
		})
	}
	pgDiffScopeFDS := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			mkPGScopeFDP("pg_tenant.proto", "TenantEvents", "TENANT", "WHOLE_TABLE"),
			mkPGScopeFDP("pg_term.proto", "TerminologyEvents", "GLOBAL_TERMINOLOGY", "WHOLE_TABLE"),
		},
	}
	pgDiffGen := newPlugin(t, pgDiffScopeFDS, []string{"pg_tenant.proto", "pg_term.proto"})
	if err := runner.Generate(pgDiffGen, &Options{
		Lang: "go", Backend: "postgres", PostgresMode: "hcl", Domain: false, OutputFile: "scopes.hcl",
	}); err != nil {
		t.Fatalf("expected same table name in TENANT vs GLOBAL_TERMINOLOGY to succeed, got: %v", err)
	}
	scopesHCL := stage1aGeneratedFiles(t, pgDiffGen)["scopes.hcl"]
	if !strings.Contains(scopesHCL, `schema = "schema.tenant_${TENANT_SLUG}"`) || !strings.Contains(scopesHCL, `schema = schema.terminology`) {
		t.Errorf("expected both tenant and terminology schemas in scopes.hcl:\n%s", scopesHCL)
	}
	pgSameScopeFDS := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			mkPGScopeFDP("pg_t1.proto", "TenantEventsA", "TENANT", "WHOLE_TABLE"),
			mkPGScopeFDP("pg_t2.proto", "TenantEventsB", "", "WHOLE_TABLE"),
		},
	}
	if err := runner.Generate(newPlugin(t, pgSameScopeFDS, []string{"pg_t1.proto", "pg_t2.proto"}), &Options{
		Lang: "go", Backend: "postgres", PostgresMode: "hcl", Domain: false, OutputFile: "collide.hcl",
	}); err == nil {
		t.Errorf("expected same resolved schema collision (TENANT vs empty lakehouse scope) to fail")
	}

	// 9: Cross-file inline targets across selected lakehouse files excluded from public.<msg>,
	// while inline targets with explicit TableOptions are preserved.
	bProto := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("b_defs.proto"),
		Package: proto.String("cross.v1"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String("example.com/cross/v1;crossv1")},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Clinic"),
				Options: stage1aMsgOpts(&proto2typepb.TableOptions{
					TableName:     "clinics",
					DatabaseScope: "TENANT",
					SyncTier:      "WHOLE_TABLE",
				}),
				Field: []*descriptorpb.FieldDescriptorProto{
					stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{PrimaryKey: true}, nil)),
				},
			},
			{
				Name: proto.String("EmbeddedAddress"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stage1aScalarField("street", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
				},
			},
			{
				Name: proto.String("ExplicitInlineTable"),
				Options: stage1aMsgOpts(&proto2typepb.TableOptions{
					TableName:     "explicit_inline_tables",
					DatabaseScope: "PUBLIC",
				}),
				Field: []*descriptorpb.FieldDescriptorProto{
					stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{PrimaryKey: true}, nil)),
					stage1aScalarField("label", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
				},
			},
		},
	}
	aPatient := &descriptorpb.DescriptorProto{
		Name: proto.String("PatientRec"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:     "patients",
			DatabaseScope: "TENANT",
			SyncTier:      "WHOLE_TABLE",
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(&proto2typepb.ColumnOptions{PrimaryKey: true}, nil)),
		},
	}
	reviewAppendMsgField(aPatient, "home_addr", 2, ".cross.v1.EmbeddedAddress", false, stage1aColOpts(&proto2typepb.ColumnOptions{InlineNested: true}, nil))
	reviewAppendMsgField(aPatient, "inline_cfg", 3, ".cross.v1.ExplicitInlineTable", false, stage1aColOpts(&proto2typepb.ColumnOptions{InlineNested: true}, nil))
	aProto := &descriptorpb.FileDescriptorProto{
		Syntax:      proto.String("proto3"),
		Name:        proto.String("a_patient.proto"),
		Package:     proto.String("cross.v1"),
		Dependency:  []string{"b_defs.proto"},
		Options:     &descriptorpb.FileOptions{GoPackage: proto.String("example.com/cross/v1;crossv1")},
		MessageType: []*descriptorpb.DescriptorProto{aPatient},
	}
	crossGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{bProto, aProto}}, []string{"b_defs.proto", "a_patient.proto"})
	if err := runner.Generate(crossGen, &Options{
		Lang: "go", Backend: "postgres", PostgresMode: "ddl", Domain: false, OutputFile: "cross_lake.sql",
	}); err != nil {
		t.Fatalf("cross-file lakehouse postgres failed: %v", err)
	}
	crossSQL := stage1aGeneratedFiles(t, crossGen)["cross_lake.sql"]
	if strings.Contains(crossSQL, "public.embedded_address") {
		t.Errorf("unannotated cross-file inline target leaked as standalone public.embedded_address:\n%s", crossSQL)
	}
	for _, want := range []string{
		"tenant_${TENANT_SLUG}.patients",
		"home_addr_street",
		"tenant_${TENANT_SLUG}.clinics",
		"public.explicit_inline_tables",
	} {
		if !strings.Contains(crossSQL, want) {
			t.Errorf("cross_lake.sql missing %q:\n%s", want, crossSQL)
		}
	}

	// 10: ValidateOutputFile blocks drive letters revealed after path.Clean.
	for _, bad := range []string{"./C:foo.sql", "./c:/foo.sql", "./d:bar.go"} {
		if _, err := ValidateOutputFile(bad); err == nil {
			t.Errorf("expected ValidateOutputFile(%q) to fail after path.Clean", bad)
		}
	}
	if got, err := ValidateOutputFile("./valid/out.sql"); err != nil || got != "valid/out.sql" {
		t.Errorf("ValidateOutputFile(./valid/out.sql) = (%q, %v), want (valid/out.sql, nil)", got, err)
	}

	// 11 & 12: Reject EmitSearchIndexGo for lang=rust, allow search_index_ddl/hcl, honor OutputFile in generateRustPostgres.
	for _, tc := range []struct {
		name string
		opts *Options
	}{
		{"no_output_file", &Options{Lang: "rust", Backend: "postgres", PostgresMode: "search_index", PostgresSearchParams: "../testdata/proto/fhir_search_params.json"}},
		{"with_output_file", &Options{Lang: "rust", Backend: "postgres", PostgresMode: "search_index", PostgresSearchParams: "../testdata/proto/fhir_search_params.json", Domain: false, OutputFile: "out.go"}},
		{"combo_search_index_ddl", &Options{Lang: "rust", Backend: "postgres", PostgresMode: "search_index+ddl", PostgresSearchParams: "../testdata/proto/fhir_search_params.json"}},
	} {
		g := newPlugin(t, fdsSearch, []string{"fhir_patient.proto"})
		if err := runner.Generate(g, tc.opts); err == nil {
			t.Errorf("%s: expected error for EmitSearchIndexGo with lang=rust", tc.name)
		}
	}
	rustSpidxDDLGen := newPlugin(t, fdsSearch, []string{"fhir_patient.proto"})
	if err := runner.Generate(rustSpidxDDLGen, &Options{
		Lang: "rust", Backend: "postgres", PostgresMode: "search_index_ddl", PostgresSearchParams: "../testdata/proto/fhir_search_params.json", Domain: false, OutputFile: "rust_spidx.sql",
	}); err != nil {
		t.Fatalf("lang=rust search_index_ddl failed: %v", err)
	}
	if _, ok := stage1aGeneratedFiles(t, rustSpidxDDLGen)["rust_spidx.sql"]; !ok {
		t.Errorf("expected rust_spidx.sql output for lang=rust search_index_ddl")
	}
	rustSpidxHCLGen := newPlugin(t, fdsSearch, []string{"fhir_patient.proto"})
	if err := runner.Generate(rustSpidxHCLGen, &Options{
		Lang: "rust", Backend: "postgres", PostgresMode: "search_index_hcl", PostgresSearchParams: "../testdata/proto/fhir_search_params.json", Domain: false, OutputFile: "rust_spidx.hcl",
	}); err != nil {
		t.Fatalf("lang=rust search_index_hcl failed: %v", err)
	}
	if _, ok := stage1aGeneratedFiles(t, rustSpidxHCLGen)["rust_spidx.hcl"]; !ok {
		t.Errorf("expected rust_spidx.hcl output for lang=rust search_index_hcl")
	}

	rustPGFDS := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{domFDP}}
	rustPGCustom := newPlugin(t, rustPGFDS, []string{"pkg/item.proto"})
	if err := runner.Generate(rustPGCustom, &Options{
		Lang: "rust", Backend: "postgres", PostgresMode: "types", Domain: false, OutputFile: "custom/pg_models.rs",
	}); err != nil {
		t.Fatalf("lang=rust postgres OutputFile failed: %v", err)
	}
	rustPGFiles := stage1aGeneratedFiles(t, rustPGCustom)
	if _, ok := rustPGFiles["custom/pg_models.rs"]; !ok || len(rustPGFiles) != 1 {
		t.Errorf("expected only custom/pg_models.rs, got %v", rustPGFiles)
	}
	rustPGDefault := newPlugin(t, rustPGFDS, []string{"pkg/item.proto"})
	if err := runner.Generate(rustPGDefault, &Options{
		Lang: "rust", Backend: "postgres", Domain: false,
	}); err != nil {
		t.Fatalf("lang=rust postgres default filename failed: %v", err)
	}
	rustPGDefaultFiles := stage1aGeneratedFiles(t, rustPGDefault)
	wantDefaultRS := rustPGDefault.Files[0].GeneratedFilenamePrefix + "_postgres.type.rs"
	if _, ok := rustPGDefaultFiles[wantDefaultRS]; !ok {
		t.Errorf("expected default %s, got %v", wantDefaultRS, rustPGDefaultFiles)
	}

	// 14: Consolidated postgres_mode=go requires domain=false; vc.In backslash error + single-quote escape;
	// typed SQL default cast validation.
	pgGoFDS := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			stage1aSingleMsgFDP("m1.proto", "m.v1", "example.com/m/v1;mv1", &descriptorpb.DescriptorProto{
				Name:    proto.String("ModelOne"),
				Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: "model_ones"}),
				Field: []*descriptorpb.FieldDescriptorProto{
					stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
				},
			}),
			stage1aSingleMsgFDP("m2.proto", "m.v1", "example.com/m/v1;mv1", &descriptorpb.DescriptorProto{
				Name:    proto.String("ModelTwo"),
				Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: "model_twos"}),
				Field: []*descriptorpb.FieldDescriptorProto{
					stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
				},
			}),
		},
	}
	if err := runner.Generate(newPlugin(t, pgGoFDS, []string{"m1.proto", "m2.proto"}), &Options{
		Lang: "go", Backend: "postgres", PostgresMode: "go", Domain: true, OutputFile: "pg_bad.go",
	}); err == nil {
		t.Errorf("expected consolidated postgres_mode=go with domain=true to be rejected")
	}
	pgGoGen := newPlugin(t, pgGoFDS, []string{"m1.proto", "m2.proto"})
	if err := runner.Generate(pgGoGen, &Options{
		Lang: "go", Backend: "postgres", PostgresMode: "go", Domain: false, OutputFile: "pg_consolidated.go",
	}); err != nil {
		t.Fatalf("consolidated postgres_mode=go with domain=false failed: %v", err)
	}
	pgGoOut := stage1aGeneratedFiles(t, pgGoGen)["pg_consolidated.go"]
	if !strings.Contains(pgGoOut, "type ModelOnePostgres struct") || !strings.Contains(pgGoOut, "type ModelTwoPostgres struct") {
		t.Errorf("consolidated postgres Go missing structs:\n%s", pgGoOut)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "pg_consolidated.go", pgGoOut, parser.AllErrors); err != nil {
		t.Fatalf("consolidated postgres Go syntax error: %v\n%s", err, pgGoOut)
	}

	backslashInOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(backslashInOpts, validate.E_Field, &validate.FieldRules{
		Type: &validate.FieldRules_String_{
			String_: &validate.StringRules{
				In: []string{"OK", `BAD\ESC`},
			},
		},
	})
	backslashFDP := stage1aSingleMsgFDP("bs_in.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name:    proto.String("BsInCheck"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: "bs_in_check"}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
			stage1aScalarField("status", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, backslashInOpts),
		},
	})
	if err := runner.Generate(newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{backslashFDP}}, []string{"bs_in.proto"}), &Options{
		Lang: "go", Backend: "postgres", PostgresMode: "ddl",
	}); err == nil {
		t.Errorf("expected error for backslash in vc.In CHECK constraint")
	}

	quoteInOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(quoteInOpts, validate.E_Field, &validate.FieldRules{
		Type: &validate.FieldRules_String_{
			String_: &validate.StringRules{
				In: []string{"O'Brien", "ACTIVE"},
			},
		},
	})
	quoteInFDP := stage1aSingleMsgFDP("quote_in.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name:    proto.String("QuoteInCheck"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{TableName: "quote_in_check"}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
			stage1aScalarField("status", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, quoteInOpts),
		},
	})
	quoteInGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{quoteInFDP}}, []string{"quote_in.proto"})
	if err := runner.Generate(quoteInGen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"}); err != nil {
		t.Fatalf("vc.In with single quote failed: %v", err)
	}
	if sql := stage1aFindBySuffix(t, stage1aGeneratedFiles(t, quoteInGen), "_postgres.sql"); !strings.Contains(sql, `'O''Brien'::text`) {
		t.Errorf("expected escaped single quote in CHECK constraint:\n%s", sql)
	}

	for _, badCastHCL := range []string{
		`table "t1" {
  column "flag" {
    type = boolean
    null = false
    default = sql("'notabool'::boolean")
  }
}`,
		`table "t1" {
  column "cnt" {
    type = integer
    null = false
    default = sql("'abc'::integer")
  }
}`,
		`table "t1" {
  column "cnt" {
    type = integer
    null = false
    default = sql("''::integer")
  }
}`,
	} {
		if _, err := ParsePostgresAtlasHCL(badCastHCL); err == nil {
			t.Errorf("expected ParsePostgresAtlasHCL to reject invalid typed SQL default:\n%s", badCastHCL)
		}
	}
	if parsedValid, err := ParsePostgresAtlasHCL(`table "t1" {
  schema = schema.public
  column "flag" {
    type = boolean
    null = false
    default = sql("'true'::boolean")
  }
  column "cnt" {
    type = integer
    null = false
    default = sql("'42'::integer")
  }
  column "note" {
    type = text
    null = false
    default = sql("''::text")
  }
}`); err != nil || !parsedValid[0].Columns[2].HasDefaultValue || parsedValid[0].Columns[2].DefaultValue != "" {
		t.Errorf("expected valid typed SQL defaults to parse cleanly, got (%+v, %v)", parsedValid, err)
	}
}

func TestDorisNullableScalarOverridesCompiledRuntime(t *testing.T) {
	if _, err := exec.LookPath("protoc"); err != nil {
		t.Skip("protoc not found in PATH; skipping compile-and-run test")
	}
	if _, err := exec.LookPath("protoc-gen-go"); err != nil {
		t.Skip("protoc-gen-go not found in PATH; skipping compile-and-run test")
	}

	tmpPkgDir, err := os.MkdirTemp(".", "doris_nullable_compile_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpPkgDir)
	}()

	pkgName := filepath.Base(tmpPkgDir)
	goPkg := "github.com/protocgen/proto2type/generator/" + pkgName + ";nullablev1"
	protoSrc := `syntax = "proto3";
package nullabletest.v1;
option go_package = "` + goPkg + `";

enum NullableStatus {
  NULLABLE_STATUS_UNSPECIFIED = 0;
  NULLABLE_STATUS_ACTIVE = 1;
  NULLABLE_STATUS_ARCHIVED = 2;
}

message OcrBoundingBox {
  float x0 = 1;
  float y0 = 2;
}

message NullableNestedEnumBox {
  NullableStatus kind = 1;
}

message NullableRecord {
  string id = 1;
  string label = 2;
  int32 count = 3;
  float score = 4;
  uint64 hash_val = 5;
  bool active = 6;
  bytes payload = 7;
  NullableStatus status_str = 8;
  NullableStatus status_strip = 9;
  NullableStatus status_int = 10;
  OcrBoundingBox bbox = 11;
  NullableNestedEnumBox nested_enum = 12;
}
`
	if err := os.WriteFile(filepath.Join(tmpPkgDir, "nullable_scalars.proto"), []byte(protoSrc), 0o644); err != nil {
		t.Fatalf("write proto: %v", err)
	}
	fdsPath := filepath.Join(tmpPkgDir, "nullable_scalars.fds")
	protocCmd := exec.Command("protoc",
		"-I", tmpPkgDir,
		"--go_out="+tmpPkgDir,
		"--go_opt=paths=source_relative",
		"--descriptor_set_out="+fdsPath,
		"nullable_scalars.proto",
	)
	if out, err := protocCmd.CombinedOutput(); err != nil {
		t.Fatalf("protoc failed: %v\n%s", err, out)
	}

	fdsBytes, err := os.ReadFile(fdsPath)
	if err != nil {
		t.Fatalf("read fds: %v", err)
	}
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(fdsBytes, &fds); err != nil {
		t.Fatalf("unmarshal fds: %v", err)
	}
	fdp := fds.File[0]

	findDP := func(name string) *descriptorpb.DescriptorProto {
		for _, m := range fdp.MessageType {
			if m.GetName() == name {
				return m
			}
		}
		t.Fatalf("message %q not found", name)
		return nil
	}
	findField := func(dp *descriptorpb.DescriptorProto, name string) *descriptorpb.FieldDescriptorProto {
		for _, f := range dp.Field {
			if f.GetName() == name {
				return f
			}
		}
		t.Fatalf("field %q not found on %q", name, dp.GetName())
		return nil
	}

	bboxMsg := findDP("OcrBoundingBox")
	findField(bboxMsg, "x0").Options = stage1aColOpts(&proto2typepb.ColumnOptions{DorisNullable: proto.Bool(true)}, nil)
	findField(bboxMsg, "y0").Options = stage1aColOpts(&proto2typepb.ColumnOptions{DorisNullable: proto.Bool(true)}, nil)

	nestedEnumMsg := findDP("NullableNestedEnumBox")
	findField(nestedEnumMsg, "kind").Options = stage1aColOpts(
		&proto2typepb.ColumnOptions{
			DorisNullable: proto.Bool(true),
			DorisType:     "TEXT",
			IcebergType:   "VARCHAR(64)",
		},
		&proto2typepb.FieldOptions{
			EnumAsString: proto2typepb.OptionalBool_OPTIONAL_BOOL_TRUE,
		},
	)

	recMsg := findDP("NullableRecord")
	recMsg.Options = stage1aMsgOpts(&proto2typepb.TableOptions{
		TableName:        "nullable_records",
		DatabaseScope:    "TENANT",
		SyncTier:         "WHOLE_TABLE",
		UniqueKeyColumns: []string{"id"},
	})
	findField(recMsg, "id").Options = stage1aColOpts(&proto2typepb.ColumnOptions{IsUniqueKey: true}, nil)
	for _, fn := range []string{"label", "count", "score", "hash_val", "active", "payload"} {
		findField(recMsg, fn).Options = stage1aColOpts(&proto2typepb.ColumnOptions{DorisNullable: proto.Bool(true)}, nil)
	}
	findField(recMsg, "status_str").Options = stage1aColOpts(
		&proto2typepb.ColumnOptions{
			DorisNullable: proto.Bool(true),
			DorisType:     "TEXT",
			IcebergType:   "VARCHAR(64)",
		},
		&proto2typepb.FieldOptions{
			EnumAsString: proto2typepb.OptionalBool_OPTIONAL_BOOL_TRUE,
		},
	)
	findField(recMsg, "status_strip").Options = stage1aColOpts(&proto2typepb.ColumnOptions{
		DorisNullable:   proto.Bool(true),
		DorisType:       "VARCHAR(64)",
		StripEnumPrefix: true,
	}, nil)
	findField(recMsg, "status_int").Options = stage1aColOpts(&proto2typepb.ColumnOptions{
		DorisNullable: proto.Bool(true),
		DorisType:     "INT",
	}, nil)
	findField(recMsg, "bbox").Options = stage1aColOpts(&proto2typepb.ColumnOptions{InlineNested: true}, nil)
	findField(recMsg, "nested_enum").Options = stage1aColOpts(&proto2typepb.ColumnOptions{InlineNested: true}, nil)

	gen := newPlugin(t, &fds, []string{"nullable_scalars.proto"})
	runner := NewRunner()
	if err := runner.Generate(gen, &Options{
		Lang:       "go",
		Backend:    "doris",
		DorisMode:  "go",
		OutputFile: "nullable_doris.type.go",
	}); err != nil {
		t.Fatalf("Runner.Generate Doris Go failed: %v", err)
	}
	genFiles := stage1aGeneratedFiles(t, gen)
	dorisCode := genFiles["nullable_doris.type.go"]
	if dorisCode == "" {
		t.Fatalf("missing generated nullable_doris.type.go (got %v)", genFiles)
	}
	normCode := strings.Join(strings.Fields(dorisCode), " ")
	for _, wantField := range []string{
		"ID string",
		"Label *string",
		"Count *int32",
		"Score *float32",
		"HashVal *uint64",
		"Active *bool",
		"Payload []byte",
		"StatusStr *string",
		"StatusStrip *string",
		"StatusInt *int32",
		"X0 *float32",
		"Y0 *float32",
		"Kind *string",
	} {
		if !strings.Contains(normCode, wantField) {
			t.Fatalf("expected generated struct field %q in:\n%s", wantField, dorisCode)
		}
	}
	if err := os.WriteFile(filepath.Join(tmpPkgDir, "nullable_doris.type.go"), []byte(dorisCode), 0o644); err != nil {
		t.Fatalf("write nullable_doris.type.go: %v", err)
	}

	rtTestSrc := `package nullablev1

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestNullableScalarsRuntime(t *testing.T) {
	// 1. Scanning SQL NULL preserves nil for all doris_nullable scalar/enum/bytes/inline fields,
	//    and Values() -> ScanRow() round-trips SQL NULL without materializing zero values.
	var nullRow NullableRecordDoris
	nullInput := []any{"rec-null", nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil}
	if err := nullRow.ScanRow(NewDorisValueScanner(nullInput)); err != nil {
		t.Fatalf("ScanRow SQL NULL failed: %v", err)
	}
	if nullRow.ID != "rec-null" ||
		nullRow.Label != nil || nullRow.Count != nil || nullRow.Score != nil ||
		nullRow.HashVal != nil || nullRow.Active != nil || nullRow.Payload != nil ||
		nullRow.StatusStr != nil || nullRow.StatusStrip != nil || nullRow.StatusInt != nil ||
		nullRow.X0 != nil || nullRow.Y0 != nil || nullRow.Kind != nil {
		t.Fatalf("expected all nullable fields to stay nil on SQL NULL scan, got %+v", nullRow)
	}

	var roundTripNull NullableRecordDoris
	if err := roundTripNull.ScanRow(NewDorisValueScanner(nullRow.Values())); err != nil {
		t.Fatalf("ScanRow(nullRow.Values()) failed: %v", err)
	}
	if roundTripNull.Label != nil || roundTripNull.Count != nil || roundTripNull.Score != nil ||
		roundTripNull.HashVal != nil || roundTripNull.Active != nil || roundTripNull.Payload != nil ||
		roundTripNull.StatusStr != nil || roundTripNull.StatusStrip != nil || roundTripNull.StatusInt != nil ||
		roundTripNull.X0 != nil || roundTripNull.Y0 != nil || roundTripNull.Kind != nil {
		t.Fatalf("expected Values() -> ScanRow() to keep SQL NULL fields nil, got %+v", roundTripNull)
	}
	if gotNullProto := roundTripNull.ToProto(); gotNullProto.Bbox != nil || gotNullProto.NestedEnum != nil || gotNullProto.Payload != nil {
		t.Fatalf("expected nil inline nested messages & bytes after NULL round-trip, got %+v", gotNullProto)
	}

	// 2. FromProto with explicit zero values preserves non-nil pointers and []byte{}
	//    and round-trips through Values() -> ScanRow() -> ToProto().
	zeroMsg := &NullableRecord{
		Id:          "rec-zeros",
		Label:       "",
		Count:       0,
		Score:       0,
		HashVal:     0,
		Active:      false,
		Payload:     []byte{},
		StatusStr:   NullableStatus_NULLABLE_STATUS_UNSPECIFIED,
		StatusStrip: NullableStatus_NULLABLE_STATUS_UNSPECIFIED,
		StatusInt:   NullableStatus_NULLABLE_STATUS_UNSPECIFIED,
		Bbox:        &OcrBoundingBox{X0: 0, Y0: 0},
		NestedEnum:  &NullableNestedEnumBox{Kind: NullableStatus_NULLABLE_STATUS_UNSPECIFIED},
	}
	var zeroRow NullableRecordDoris
	zeroRow.FromProto(zeroMsg)
	if zeroRow.Label == nil || *zeroRow.Label != "" {
		t.Fatalf("expected non-nil &\"\" for Label, got %#v", zeroRow.Label)
	}
	if zeroRow.Count == nil || *zeroRow.Count != 0 {
		t.Fatalf("expected non-nil &0 for Count, got %#v", zeroRow.Count)
	}
	if zeroRow.Score == nil || *zeroRow.Score != 0 {
		t.Fatalf("expected non-nil &0.0 for Score, got %#v", zeroRow.Score)
	}
	if zeroRow.HashVal == nil || *zeroRow.HashVal != 0 {
		t.Fatalf("expected non-nil &0 for HashVal, got %#v", zeroRow.HashVal)
	}
	if zeroRow.Active == nil || *zeroRow.Active != false {
		t.Fatalf("expected non-nil &false for Active, got %#v", zeroRow.Active)
	}
	if zeroRow.Payload == nil || len(zeroRow.Payload) != 0 {
		t.Fatalf("expected non-nil empty []byte{} for Payload, got %#v", zeroRow.Payload)
	}
	if zeroRow.StatusStr == nil || *zeroRow.StatusStr != "NULLABLE_STATUS_UNSPECIFIED" {
		t.Fatalf("expected non-nil unstripped UNSPECIFIED for StatusStr, got %#v", zeroRow.StatusStr)
	}
	if zeroRow.StatusStrip != nil {
		t.Fatalf("expected nil for stripped enum zero value StatusStrip, got %#v", zeroRow.StatusStrip)
	}
	if zeroRow.StatusInt == nil || *zeroRow.StatusInt != 0 {
		t.Fatalf("expected non-nil &0 for StatusInt, got %#v", zeroRow.StatusInt)
	}
	if zeroRow.X0 == nil || *zeroRow.X0 != 0 || zeroRow.Y0 == nil || *zeroRow.Y0 != 0 {
		t.Fatalf("expected non-nil &0 for X0/Y0 when Bbox is present, got (%#v, %#v)", zeroRow.X0, zeroRow.Y0)
	}
	if zeroRow.Kind == nil || *zeroRow.Kind != "NULLABLE_STATUS_UNSPECIFIED" {
		t.Fatalf("expected non-nil &\"NULLABLE_STATUS_UNSPECIFIED\" for Kind, got %#v", zeroRow.Kind)
	}

	gotZeroProto, err := ScanNullableRecordProto(NewDorisValueScanner(zeroRow.Values()))
	if err != nil {
		t.Fatalf("ScanNullableRecordProto(zeroRow) failed: %v", err)
	}
	if !proto.Equal(zeroMsg, gotZeroProto) {
		t.Fatalf("zeroMsg round-trip mismatch:\nwant: %+v\ngot:  %+v", zeroMsg, gotZeroProto)
	}
	if gotZeroProto.Bbox == nil || gotZeroProto.Bbox.X0 != 0 || gotZeroProto.Bbox.Y0 != 0 {
		t.Fatalf("expected non-nil Bbox with zeros after round-trip, got %+v", gotZeroProto.Bbox)
	}
	if gotZeroProto.NestedEnum == nil || gotZeroProto.NestedEnum.Kind != 0 {
		t.Fatalf("expected non-nil NestedEnum with UNSPECIFIED after round-trip, got %+v", gotZeroProto.NestedEnum)
	}
	if gotZeroProto.Payload == nil || len(gotZeroProto.Payload) != 0 {
		t.Fatalf("expected non-nil empty []byte{} Payload after round-trip, got %#v", gotZeroProto.Payload)
	}

	// 3. Inline nested message with nil parent produces nil columns and round-trips to nil,
	//    while present parent with X0=0 produces &0 and round-trips to non-nil &OcrBoundingBox{}.
	nilParentMsg := &NullableRecord{Id: "rec-nil-parent"}
	var nilParentRow NullableRecordDoris
	nilParentRow.FromProto(nilParentMsg)
	if nilParentRow.X0 != nil || nilParentRow.Y0 != nil || nilParentRow.Kind != nil || nilParentRow.Payload != nil {
		t.Fatalf("expected nil inline nested & bytes fields when parents/bytes are nil, got %+v", nilParentRow)
	}
	gotNilParent, err := ScanNullableRecordProto(NewDorisValueScanner(nilParentRow.Values()))
	if err != nil {
		t.Fatalf("ScanNullableRecordProto(nilParent) failed: %v", err)
	}
	if gotNilParent.Bbox != nil || gotNilParent.NestedEnum != nil || gotNilParent.Payload != nil {
		t.Fatalf("expected nil Bbox/NestedEnum/Payload after nil parent round-trip, got %+v", gotNilParent)
	}

	onlyBboxZero := &NullableRecord{
		Id:   "rec-bbox-zero",
		Bbox: &OcrBoundingBox{X0: 0, Y0: 0},
	}
	var onlyBboxRow NullableRecordDoris
	onlyBboxRow.FromProto(onlyBboxZero)
	if onlyBboxRow.X0 == nil || *onlyBboxRow.X0 != 0 {
		t.Fatalf("expected non-nil &0 X0 when Bbox present, got %#v", onlyBboxRow.X0)
	}
	gotOnlyBbox, err := ScanNullableRecordProto(NewDorisValueScanner(onlyBboxRow.Values()))
	if err != nil {
		t.Fatalf("ScanNullableRecordProto(onlyBboxZero) failed: %v", err)
	}
	if gotOnlyBbox.Bbox == nil || gotOnlyBbox.Bbox.X0 != 0 || gotOnlyBbox.Bbox.Y0 != 0 {
		t.Fatalf("expected non-nil Bbox with X0=0,Y0=0 after round-trip, got %+v", gotOnlyBbox.Bbox)
	}
}
`
	if err := os.WriteFile(filepath.Join(tmpPkgDir, "nullable_rt_test.go"), []byte(rtTestSrc), 0o644); err != nil {
		t.Fatalf("write nullable_rt_test.go: %v", err)
	}

	testCmd := exec.Command("go", "test", "-v", "./"+pkgName)
	testCmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := testCmd.CombinedOutput(); err != nil {
		t.Fatalf("compiled nullable scalar runtime test failed: %v\n%s", err, out)
	}
}

func TestPR241_CodeRabbitRound2_PostgresFixes(t *testing.T) {
	runner := NewRunner()

	// Item 5: Field validation CHECK collision with singleton check must fail, non-conflicting emits both.
	titleMinLenOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(titleMinLenOpts, validate.E_Field, &validate.FieldRules{
		Type: &validate.FieldRules_String_{
			String_: &validate.StringRules{MinLen: proto.Uint64(3)},
		},
	})
	chkCollisionFDP := stage1aSingleMsgFDP("settings_chk.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("Settings"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:          "settings",
			IsSingleton:        true,
			SingletonCheckName: proto.String("chk_settings_title_min_len"),
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("title", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, titleMinLenOpts),
		},
	})
	if err := runner.Generate(
		newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{chkCollisionFDP}}, []string{"settings_chk.proto"}),
		&Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"},
	); err == nil {
		t.Errorf("expected error when singleton_check_name collides with field validation CHECK constraint name")
	}
	nonConflictFDP := stage1aSingleMsgFDP("settings_ok.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
		Name: proto.String("Settings"),
		Options: stage1aMsgOpts(&proto2typepb.TableOptions{
			TableName:          "settings",
			IsSingleton:        true,
			SingletonCheckName: proto.String("chk_settings_singleton_only"),
		}),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("title", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, titleMinLenOpts),
		},
	})
	nonConflictGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{nonConflictFDP}}, []string{"settings_ok.proto"})
	if err := runner.Generate(nonConflictGen, &Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"}); err != nil {
		t.Fatalf("unexpected error for non-conflicting singleton + field CHECK: %v", err)
	}
	nonConflictSQL := stage1aFindBySuffix(t, stage1aGeneratedFiles(t, nonConflictGen), "_postgres.sql")
	if !strings.Contains(nonConflictSQL, `"chk_settings_singleton_only"`) || !strings.Contains(nonConflictSQL, `"chk_settings_title_min_len"`) {
		t.Errorf("expected both singleton check and field min_len check, got:\n%s", nonConflictSQL)
	}

	// Item 6: Non-boolean existing singleton column rejected in both relational and lakehouse builders.
	for _, lake := range []bool{false, true} {
		tblOpts := &proto2typepb.TableOptions{TableName: "bad_singleton_type", IsSingleton: true}
		if lake {
			tblOpts.DatabaseScope = "TENANT"
			tblOpts.SyncTier = "WHOLE_TABLE"
		}
		badSingletonFDP := stage1aSingleMsgFDP("bad_singleton.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
			Name:    proto.String("BadSingleton"),
			Options: stage1aMsgOpts(tblOpts),
			Field: []*descriptorpb.FieldDescriptorProto{
				stage1aScalarField("singleton", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
			},
		})
		if err := runner.Generate(
			newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{badSingletonFDP}}, []string{"bad_singleton.proto"}),
			&Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"},
		); err == nil {
			t.Errorf("lakehouse=%v: expected error for non-boolean existing singleton column", lake)
		}

		boolOpts := &proto2typepb.TableOptions{TableName: "ok_bool_singleton", IsSingleton: true}
		if lake {
			boolOpts.DatabaseScope = "TENANT"
			boolOpts.SyncTier = "WHOLE_TABLE"
		}
		boolSingletonFDP := stage1aSingleMsgFDP("ok_bool_singleton.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
			Name:    proto.String("BoolSingleton"),
			Options: stage1aMsgOpts(boolOpts),
			Field: []*descriptorpb.FieldDescriptorProto{
				stage1aScalarField("singleton", 1, descriptorpb.FieldDescriptorProto_TYPE_BOOL, false, nil),
			},
		})
		if err := runner.Generate(
			newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{boolSingletonFDP}}, []string{"ok_bool_singleton.proto"}),
			&Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"},
		); err != nil {
			t.Errorf("lakehouse=%v: expected bool existing singleton column to succeed, got %v", lake, err)
		}

		synthOpts := &proto2typepb.TableOptions{TableName: "ok_synth_singleton", IsSingleton: true}
		if lake {
			synthOpts.DatabaseScope = "TENANT"
			synthOpts.SyncTier = "WHOLE_TABLE"
		}
		synthSingletonFDP := stage1aSingleMsgFDP("ok_synth_singleton.proto", "review.v1", "example.com/review/v1;reviewv1", &descriptorpb.DescriptorProto{
			Name:    proto.String("SynthSingleton"),
			Options: stage1aMsgOpts(synthOpts),
			Field: []*descriptorpb.FieldDescriptorProto{
				stage1aScalarField("flag", 1, descriptorpb.FieldDescriptorProto_TYPE_BOOL, false, nil),
			},
		})
		if err := runner.Generate(
			newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{synthSingletonFDP}}, []string{"ok_synth_singleton.proto"}),
			&Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"},
		); err != nil {
			t.Errorf("lakehouse=%v: expected synthetic singleton column to succeed, got %v", lake, err)
		}
	}

	// Item 7: Precision-preserving timestamp/time cast aliases.
	for _, tc := range []struct {
		in, want string
	}{
		{"timestamp(3) with time zone", "timestamptz(3)"},
		{"timestamp ( 3 ) with time zone", "timestamptz(3)"},
		{"timestamptz ( 3 )", "timestamptz(3)"},
		{"timestamp(3) without time zone", "timestamp(3)"},
		{"time(3) with time zone", "timetz(3)"},
		{"time(3) without time zone", "time(3)"},
	} {
		if got := canonicalPostgresSQLCastType(tc.in); got != tc.want {
			t.Errorf("canonicalPostgresSQLCastType(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, okCast := range []string{
		`'2026-01-01 00:00:00+00'::timestamp(3) with time zone`,
		`'2026-01-01 00:00:00+00'::timestamp ( 3 ) with time zone`,
	} {
		hcl := `table "tz_tbl" {
  schema = schema.public
  column "id" {
    type = uuid
    null = false
  }
  column "ts" {
    type    = sql("timestamptz(3)")
    null    = false
    default = sql("` + okCast + `")
  }
  primary_key {
    columns = [column.id]
  }
}`
		if parsed, err := ParsePostgresAtlasHCL(hcl); err != nil || !parsed[0].Columns[1].HasDefaultValue {
			t.Errorf("expected ParsePostgresAtlasHCL to accept %s on timestamptz(3), got (%+v, %v)", okCast, parsed, err)
		}
	}
	for _, badCast := range []string{
		`'2026-01-01 00:00:00+00'::timestamp(6) with time zone`,
		`'2026-01-01 00:00:00'::timestamp(3) without time zone`,
	} {
		hcl := `table "tz_tbl" {
  schema = schema.public
  column "id" {
    type = uuid
    null = false
  }
  column "ts" {
    type    = sql("timestamptz(3)")
    null    = false
    default = sql("` + badCast + `")
  }
}`
		if _, err := ParsePostgresAtlasHCL(hcl); err == nil {
			t.Errorf("expected ParsePostgresAtlasHCL to reject %s for timestamptz(3)", badCast)
		}
	}

	// Item 8: Case-folded PostgreSQL identifier collision checks.
	makeTwoMsgFDP := func(fileName string, opts1, opts2 *proto2typepb.TableOptions) *descriptorpb.FileDescriptorProto {
		return &descriptorpb.FileDescriptorProto{
			Name:    proto.String(fileName),
			Package: proto.String("casecol.v1"),
			Syntax:  proto.String("proto3"),
			Options: &descriptorpb.FileOptions{GoPackage: proto.String("example.com/casecol/v1;casecolv1")},
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name:    proto.String("MsgUpper"),
					Options: stage1aMsgOpts(opts1),
					Field: []*descriptorpb.FieldDescriptorProto{
						stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
					},
				},
				{
					Name:    proto.String("MsgLower"),
					Options: stage1aMsgOpts(opts2),
					Field: []*descriptorpb.FieldDescriptorProto{
						stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, stage1aColOpts(nil, &proto2typepb.FieldOptions{PrimaryKey: true})),
					},
				},
			},
		}
	}
	for _, mode := range []string{"ddl", "hcl"} {
		for _, scope := range []string{"PUBLIC", "TENANT"} {
			fdpSame := makeTwoMsgFDP("same_"+mode+"_"+scope+".proto",
				&proto2typepb.TableOptions{TableName: "Events", DatabaseScope: scope, SyncTier: "WHOLE_TABLE"},
				&proto2typepb.TableOptions{TableName: "events", DatabaseScope: scope, SyncTier: "WHOLE_TABLE"},
			)
			if err := runner.Generate(
				newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdpSame}}, []string{"same_" + mode + "_" + scope + ".proto"}),
				&Options{Lang: "go", Backend: "postgres", PostgresMode: mode},
			); err == nil {
				t.Errorf("mode=%s scope=%s: expected case-only collision Events vs events to fail", mode, scope)
			}
		}
		fdpDiff := makeTwoMsgFDP("diff_"+mode+".proto",
			&proto2typepb.TableOptions{TableName: "Events", DatabaseScope: "PUBLIC", SyncTier: "WHOLE_TABLE"},
			&proto2typepb.TableOptions{TableName: "events", DatabaseScope: "GLOBAL_TERMINOLOGY", SyncTier: "WHOLE_TABLE"},
		)
		if err := runner.Generate(
			newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdpDiff}}, []string{"diff_" + mode + ".proto"}),
			&Options{Lang: "go", Backend: "postgres", PostgresMode: mode},
		); err != nil {
			t.Errorf("mode=%s: expected Events in PUBLIC and events in GLOBAL_TERMINOLOGY to succeed, got %v", mode, err)
		}
	}
	relSame := makeTwoMsgFDP("rel_same.proto",
		&proto2typepb.TableOptions{TableName: "Events"},
		&proto2typepb.TableOptions{TableName: "events"},
	)
	if err := runner.Generate(
		newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{relSame}}, []string{"rel_same.proto"}),
		&Options{Lang: "go", Backend: "postgres", PostgresMode: "ddl"},
	); err == nil {
		t.Errorf("relational ddl: expected case-only collision Events vs events to fail")
	}

	// Item 9: Single-key idx.Ops normalization for BTREE & opclass-before-DESC without double-appending.
	idxLegacyOps := PostgresIndexDescriptor{
		Name:        "idx_email_pat",
		Columns:     []string{"email"},
		DescColumns: []string{"email"},
		Using:       "BTREE",
		Ops:         "varchar_pattern_ops",
	}
	idxPartsOps := PostgresIndexDescriptor{
		Name:  "idx_email_pat",
		Using: "BTREE",
		Parts: []PostgresIndexPartDescriptor{
			{Column: "email", Opclass: "varchar_pattern_ops", Desc: true},
		},
	}
	if !postgresIndexesEffectivelyEqual(idxLegacyOps, idxPartsOps) {
		t.Errorf("expected BTREE single-key idx.Ops + DESC to equal Parts with Opclass + DESC")
	}
	if parts := formatPostgresIndexPartsSQL(idxLegacyOps, false); len(parts) != 1 || parts[0] != "email varchar_pattern_ops DESC" {
		t.Errorf("expected opclass before DESC in formatPostgresIndexPartsSQL, got %v", parts)
	}
	hclTblOps := PostgresTableDescriptor{
		TableName: "ops_tbl", DatabaseScope: "PUBLIC",
		Columns: []PostgresColumnDescriptor{
			{ColumnName: "id", SQLType: "UUID", PostgresType: "UUID", IsPrimaryKey: true},
			{ColumnName: "email", SQLType: "TEXT", PostgresType: "TEXT"},
			{ColumnName: "title", SQLType: "TEXT", PostgresType: "TEXT"},
		},
		Indexes: []PostgresIndexDescriptor{
			idxLegacyOps,
			{Name: "idx_title_gin", Columns: []string{"title"}, Using: "GIN", Ops: "gin_trgm_ops"},
		},
	}
	hclSQL := hclTblOps.BuildPostgresCreateTableWithoutForeignKeysSQL("")
	if !strings.Contains(hclSQL, "CREATE INDEX IF NOT EXISTS idx_email_pat ON public.ops_tbl (email varchar_pattern_ops DESC);") {
		t.Errorf("expected single-key BTREE ops before DESC without duplication in HCL CREATE:\n%s", hclSQL)
	}
	if !strings.Contains(hclSQL, "CREATE INDEX IF NOT EXISTS idx_title_gin ON public.ops_tbl USING gin (title gin_trgm_ops);") {
		t.Errorf("expected single-key GIN ops without double-append in HCL CREATE:\n%s", hclSQL)
	}
	relTblOps := PostgresTableSchemaDescriptor{
		TableName: "ops_tbl",
		Columns:   hclTblOps.Columns,
		Indexes:   []PostgresIndexDescriptor{idxLegacyOps},
	}
	relSQL := relTblOps.BuildPostgresCreateTableWithoutForeignKeysSQL()
	if !strings.Contains(relSQL, `CREATE INDEX IF NOT EXISTS "idx_email_pat" ON "ops_tbl" ("email" varchar_pattern_ops DESC);`) {
		t.Errorf("expected single-key BTREE ops before DESC in relational DDL:\n%s", relSQL)
	}
	alterBefore := PostgresTableDescriptor{
		TableName: "ops_tbl", DatabaseScope: "PUBLIC",
		Columns: hclTblOps.Columns,
		Indexes: []PostgresIndexDescriptor{idxPartsOps},
	}
	alterSameStmts, err := BuildPostgresAlterTableSQL(alterBefore, hclTblOps, "")
	if err != nil {
		t.Fatalf("BuildPostgresAlterTableSQL same failed: %v", err)
	}
	for _, st := range alterSameStmts {
		if strings.Contains(st, "idx_email_pat") {
			t.Errorf("expected no DROP/CREATE churn for equivalent single-key BTREE ops index, got %v", alterSameStmts)
		}
	}
	alterEmpty := PostgresTableDescriptor{
		TableName: "ops_tbl", DatabaseScope: "PUBLIC",
		Columns: hclTblOps.Columns,
	}
	alterAddStmts, err := BuildPostgresAlterTableSQL(alterEmpty, hclTblOps, "")
	if err != nil {
		t.Fatalf("BuildPostgresAlterTableSQL add failed: %v", err)
	}
	joinedAlter := strings.Join(alterAddStmts, "\n")
	if !strings.Contains(joinedAlter, "CREATE INDEX IF NOT EXISTS idx_email_pat ON public.ops_tbl (email varchar_pattern_ops DESC);") ||
		!strings.Contains(joinedAlter, "CREATE INDEX IF NOT EXISTS idx_title_gin ON public.ops_tbl USING gin (title gin_trgm_ops);") {
		t.Errorf("expected clean non-duplicated single-key Ops in ALTER statements:\n%s", joinedAlter)
	}
}

func TestPR241_CodeRabbitRound2_OutputFileHonored(t *testing.T) {
	runner := NewRunner()
	fdp := stage1aSingleMsgFDP("pkg/item.proto", "out.v1", "example.com/pkg;pkgv1", &descriptorpb.DescriptorProto{
		Name: proto.String("Item"),
		Field: []*descriptorpb.FieldDescriptorProto{
			stage1aScalarField("id", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, false, nil),
		},
	})

	// generateRustDomain honors opts.OutputFile.
	rsGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"pkg/item.proto"})
	if err := runner.Generate(rsGen, &Options{
		Lang:       "rust",
		Domain:     true,
		OutputFile: "custom/my_domain.rs",
	}); err != nil {
		t.Fatalf("Runner.Generate Rust domain with OutputFile failed: %v", err)
	}
	rsFiles := stage1aGeneratedFiles(t, rsGen)
	if _, ok := rsFiles["custom/my_domain.rs"]; !ok || len(rsFiles) != 1 {
		t.Fatalf("expected single file custom/my_domain.rs, got %v", rsFiles)
	}

	// generateGoFirestore honors opts.OutputFile and skips adjustSubdirFilename when OutputFile is set.
	fsGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"pkg/item.proto"})
	if err := runner.Generate(fsGen, &Options{
		Lang:       "go",
		Backend:    "firestore",
		GoPackage:  "example.com/pkg/fs;fs",
		OutputFile: "custom/my_firestore.go",
	}); err != nil {
		t.Fatalf("Runner.Generate Go firestore with OutputFile failed: %v", err)
	}
	fsFiles := stage1aGeneratedFiles(t, fsGen)
	if _, ok := fsFiles["custom/my_firestore.go"]; !ok || len(fsFiles) != 1 {
		t.Fatalf("expected single file custom/my_firestore.go, got %v", fsFiles)
	}

	fsDefaultGen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"pkg/item.proto"})
	if err := runner.Generate(fsDefaultGen, &Options{
		Lang:      "go",
		Backend:   "firestore",
		GoPackage: "example.com/pkg/fs;fs",
	}); err != nil {
		t.Fatalf("Runner.Generate Go firestore default filename failed: %v", err)
	}
	fsDefaultFiles := stage1aGeneratedFiles(t, fsDefaultGen)
	if _, ok := fsDefaultFiles["example.com/pkg/fs/item_firestore.type.go"]; !ok {
		t.Fatalf("expected subdir-adjusted example.com/pkg/fs/item_firestore.type.go, got %v", fsDefaultFiles)
	}
}
