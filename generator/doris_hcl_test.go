package generator

import (
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"hegel.dev/go/hegel"

	proto2typepb "github.com/protocgen/proto2type/proto/proto2type"
)

// TestBuildDorisAtlasHCL_UniqueKeyMoWAndIndexes verifies deterministic Atlas Doris HCL
// generation for a TENANT-scoped UNIQUE KEY Merge-on-Write table with BM25 inverted,
// standard inverted, and NGRAM_BF indexes.
func TestBuildDorisAtlasHCL_UniqueKeyMoWAndIndexes(t *testing.T) {
	desc := DorisTableDescriptor{
		TableName:            "patient_note_chunks",
		MessageName:          "PatientNoteChunk",
		DatabaseScope:        "TENANT",
		SyncTier:             "MONTHLY_PARTITIONED",
		UniqueKeyColumns:     []string{"tenant_id", "patient_id", "chunk_id"},
		PartitionByColumns:   []string{"event_month"},
		ColocateGroup:        "patient_group",
		SequenceCol:          "updated_at",
		InvertedIndexColumns: []string{"encounter_id", "chunk_text"},
		NgramBfColumns:       []string{"section_title"},
		BloomFilterColumns:   []string{"patient_id", "encounter_id"},
		StorageVaultName:     "custom_vault",
		Columns: []DorisColumnDescriptor{
			{ColumnName: "tenant_id", DorisType: "VARCHAR(64)", Nullable: false, IsUniqueKey: true, Comment: "Tenant identifier"},
			{ColumnName: "patient_id", DorisType: "VARCHAR(64)", Nullable: false, IsUniqueKey: true},
			{ColumnName: "chunk_id", DorisType: "VARCHAR(64)", Nullable: false, IsUniqueKey: true},
			{ColumnName: "encounter_id", DorisType: "VARCHAR(64)", Nullable: true, InvertedIndex: true},
			{ColumnName: "event_month", DorisType: "VARCHAR(7)", Nullable: false, IsPartitionKey: true, DefaultValue: "1970-01"},
			{ColumnName: "section_title", DorisType: "VARCHAR(256)", Nullable: true},
			{ColumnName: "chunk_text", DorisType: "TEXT", Nullable: true, InvertedIndex: true},
			{ColumnName: "tags", DorisType: "ARRAY<VARCHAR(64)>", Nullable: false},
			{ColumnName: "updated_at", DorisType: "DATETIMEV2(6)", Nullable: false, IsSequenceCol: true},
		},
	}

	hcl := desc.BuildDorisAtlasHCL("acme-health", 16)
	if hcl == "" {
		t.Fatal("expected non-empty Doris Atlas HCL")
	}

	wantSnippets := []string{
		`table "patient_note_chunks" {`,
		`schema = schema.tenant_acme_health`,
		`comment = "Tenant identifier"`,
		`unique_key {`,
		`columns = [column.tenant_id, column.patient_id, column.chunk_id]`,
		`merge_on_write = true`,
		`sequence_col = column.updated_at`,
		`distributed_by_hash {`,
		`columns = [column.patient_id]`,
		`buckets = 16`,
		`partition_by_list {`,
		`columns = [column.event_month]`,
		`index "idx_patient_note_chunks_encounter_id" {`,
		`using = "INVERTED"`,
		`index "idx_chunk_text_bm25" {`,
		`parser = "english"`,
		`index "idx_ngram_patient_note_chunks_section_title" {`,
		`using = "NGRAM_BF"`,
		`storage_vault_name = "custom_vault"`,
		`enable_unique_key_merge_on_write = "true"`,
		`"function_column.sequence_col" = "updated_at"`,
		`colocate_with = "patient_group"`,
		`bloom_filter_columns = "patient_id, encounter_id"`,
	}
	for _, want := range wantSnippets {
		if !strings.Contains(hcl, want) {
			t.Errorf("BuildDorisAtlasHCL missing %q in:\n%s", want, hcl)
		}
	}

	// Verify auto buckets when buckets <= 0 on TENANT scope.
	autoHCL := desc.BuildDorisAtlasHCL("", 0)
	if !strings.Contains(autoHCL, `buckets = "auto"`) {
		t.Errorf("expected buckets = \"auto\" when buckets=0 on TENANT table, got:\n%s", autoHCL)
	}
}

// TestBuildDorisAtlasHCL_DuplicateKeyAndANNIndex verifies DUPLICATE KEY and ANN HNSW
// vector index rendering in Atlas Doris HCL.
func TestBuildDorisAtlasHCL_DuplicateKeyAndANNIndex(t *testing.T) {
	desc := DorisTableDescriptor{
		TableName:           "clinical_concept_search",
		DatabaseScope:       "GLOBAL_TERMINOLOGY_RAG",
		SyncTier:            "WHOLE_TABLE",
		DuplicateKeyColumns: []string{"concept_id", "code_system"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "concept_id", DorisType: "VARCHAR(64)", Nullable: false},
			{ColumnName: "code_system", DorisType: "VARCHAR(32)", Nullable: false},
			{ColumnName: "display_name", DorisType: "TEXT", Nullable: true, InvertedIndex: true},
			{ColumnName: "embedding", DorisType: "ARRAY<FLOAT>", Nullable: false},
		},
	}

	hcl := desc.BuildDorisAtlasHCL("", 0)
	wantSnippets := []string{
		`table "clinical_concept_search" {`,
		`schema = schema.terminology_rag`,
		`duplicate_key {`,
		`columns = [column.concept_id, column.code_system]`,
		`distributed_by_hash {`,
		`columns = [column.concept_id]`,
		`buckets = 8`,
		`index "idx_embedding_hnsw" {`,
		`columns = [column.embedding]`,
		`using = "ANN"`,
		`index_type = "hnsw"`,
		`metric_type = "l2_distance"`,
		`dim = "768"`,
	}
	for _, want := range wantSnippets {
		if !strings.Contains(hcl, want) {
			t.Errorf("clinical_concept_search HCL missing %q in:\n%s", want, hcl)
		}
	}
	if strings.Contains(hcl, "storage_vault_name") {
		t.Errorf("expected storage_vault_name to be omitted when StorageVaultName is empty, got:\n%s", hcl)
	}
	if strings.Contains(hcl, "replication_num") {
		t.Errorf("expected replication_num to be omitted, got:\n%s", hcl)
	}

	// Verify explicit EmbeddingDimension override.
	desc.EmbeddingDimension = 384
	hcl384 := desc.BuildDorisAtlasHCL("", 0)
	if !strings.Contains(hcl384, `dim = "384"`) {
		t.Errorf("expected dim = \"384\" when EmbeddingDimension=384, got:\n%s", hcl384)
	}
}

// TestBuildIcebergAtlasHCL_TypeMappingsAndPartitions verifies deterministic Atlas Iceberg HCL
// generation including JSON/JSONB/VARIANT -> string mapping, LARGEINT -> decimal(38, 0),
// catalog selection, and partition spec rendering.
func TestBuildIcebergAtlasHCL_TypeMappingsAndPartitions(t *testing.T) {
	desc := DorisTableDescriptor{
		TableName:          "patient_encounters",
		DatabaseScope:      "TENANT",
		SyncTier:           "MONTHLY_PARTITIONED",
		UniqueKeyColumns:   []string{"patient_id", "encounter_id"},
		PartitionByColumns: []string{"event_month"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "patient_id", DorisType: "VARCHAR(64)", Nullable: false, IsUniqueKey: true},
			{ColumnName: "encounter_id", DorisType: "VARCHAR(64)", Nullable: false, IsUniqueKey: true},
			{ColumnName: "seq_hash", DorisType: "LARGEINT", Nullable: false},
			{ColumnName: "raw_json", DorisType: "JSON", Nullable: true},
			{ColumnName: "raw_jsonb", DorisType: "JSONB", Nullable: true},
			{ColumnName: "raw_variant", DorisType: "VARIANT", Nullable: true},
			{ColumnName: "event_month", DorisType: "VARCHAR(7)", Nullable: false, IsPartitionKey: true},
		},
	}

	hcl := desc.BuildIcebergAtlasHCL("acme")
	wantSnippets := []string{
		`table "patient_encounters" {`,
		`catalog = "iceberg"`,
		`schema = schema.tenant_acme`,
		"column \"seq_hash\" {\n    type = decimal(38, 0)\n    null = false\n  }",
		"column \"raw_json\" {\n    type = string\n    null = true\n  }",
		"column \"raw_jsonb\" {\n    type = string\n    null = true\n  }",
		"column \"raw_variant\" {\n    type = string\n    null = true\n  }",
		"partition_by {\n    spec = \"event_month\"\n  }",
		`engine = "iceberg"`,
		`"write.format.default" = "parquet"`,
		`"write.parquet.compression-codec" = "zstd"`,
		`"write.parquet.bloom-filter-enabled.column.patient_id" = "true"`,
	}
	for _, want := range wantSnippets {
		if !strings.Contains(hcl, want) {
			t.Errorf("BuildIcebergAtlasHCL missing %q in:\n%s", want, hcl)
		}
	}

	customCatalog := desc
	customCatalog.IcebergCatalog = "azra_lake"
	if got := customCatalog.BuildIcebergAtlasHCL("acme"); !strings.Contains(got, `catalog = "azra_lake"`) {
		t.Errorf("expected custom catalog = \"azra_lake\", got:\n%s", got)
	}

	// Verify INTERNAL_ONLY tables return empty Iceberg HCL, and ICEBERG_ONLY tables return empty Doris HCL.
	internalOnly := desc
	internalOnly.SyncTier = "INTERNAL_ONLY"
	if got := internalOnly.BuildIcebergAtlasHCL("acme"); got != "" {
		t.Errorf("expected empty Iceberg HCL for INTERNAL_ONLY table, got:\n%s", got)
	}
	icebergOnly := desc
	icebergOnly.SyncTier = "ICEBERG_ONLY"
	if got := icebergOnly.BuildDorisAtlasHCL("acme", 8); got != "" {
		t.Errorf("expected empty Doris HCL for ICEBERG_ONLY table, got:\n%s", got)
	}
}

// TestParseDorisAtlasHCL_RoundTripAndEvolutionLinting verifies that emitted Atlas HCL
// parses back into []DorisTableDescriptor and integrates directly with LintDorisEvolution
// (DORIS001..006).
func TestParseDorisAtlasHCL_RoundTripAndEvolutionLinting(t *testing.T) {
	t1 := baseLinterTestTable()
	t1.StorageVaultName = "azra_lakehouse_vault"
	t1.Columns = append(t1.Columns, DorisColumnDescriptor{
		ColumnName:     "event_month",
		DorisType:      "VARCHAR(7)",
		IcebergType:    "STRING",
		Nullable:       false,
		IsPartitionKey: true,
		DefaultValue:   "1970-01",
	})
	t2 := DorisTableDescriptor{
		TableName:           "clinical_concept_search",
		DatabaseScope:       "GLOBAL_TERMINOLOGY_RAG",
		SyncTier:            "WHOLE_TABLE",
		DuplicateKeyColumns: []string{"concept_id", "code_system"},
		EmbeddingDimension:  768,
		Columns: []DorisColumnDescriptor{
			{ColumnName: "concept_id", DorisType: "VARCHAR(64)", Nullable: false},
			{ColumnName: "code_system", DorisType: "VARCHAR(32)", Nullable: false},
			{ColumnName: "embedding", DorisType: "ARRAY<FLOAT>", Nullable: false},
		},
	}

	combinedHCL := "# Header comment\nschema \"tenant_acme\" {}\n\n" +
		t1.BuildDorisAtlasHCL("acme", 16) + "\n\n" +
		t2.BuildDorisAtlasHCL("acme", 8)

	parsed, err := ParseDorisAtlasHCL(combinedHCL)
	if err != nil {
		t.Fatalf("ParseDorisAtlasHCL failed: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 parsed tables, got %d", len(parsed))
	}

	p1 := parsed[0]
	if p1.TableName != "eligibility_criterion_results" ||
		p1.DatabaseScope != "TENANT" ||
		p1.SyncTier != "MONTHLY_PARTITIONED" ||
		!reflect.DeepEqual(p1.UniqueKeyColumns, []string{"patient_id", "criterion_id"}) ||
		!reflect.DeepEqual(p1.DistributionColumns, []string{"patient_id"}) ||
		p1.DistributedByColumn != "patient_id" ||
		p1.Buckets != 16 ||
		p1.AutoBuckets ||
		p1.SequenceColumn != "publication_generation" ||
		p1.SequenceCol != "publication_generation" ||
		!p1.EnableMoW ||
		p1.ColocateGroup != "azra_patient_group" ||
		p1.StorageVaultName != "azra_lakehouse_vault" {
		t.Fatalf("unexpected parsed descriptor p1: %+v", p1)
	}

	p2 := parsed[1]
	if p2.TableName != "clinical_concept_search" ||
		p2.DatabaseScope != "GLOBAL_TERMINOLOGY_RAG" ||
		!reflect.DeepEqual(p2.DuplicateKeyColumns, []string{"concept_id", "code_system"}) ||
		p2.EmbeddingDimension != 768 ||
		len(p2.Indexes) != 1 ||
		p2.Indexes[0].Using != "ANN" {
		t.Fatalf("unexpected parsed descriptor p2: %+v", p2)
	}

	// Reflexive evolution between original descriptors and HCL-parsed descriptors must have 0 violations.
	if v := LintDorisEvolution([]DorisTableDescriptor{t1, t2}, parsed); len(v) != 0 {
		t.Fatalf("expected 0 violations between original and parsed HCL descriptors, got %v", v)
	}
	if err := ValidateDorisEvolution(parsed, parsed); err != nil {
		t.Fatalf("expected nil error from ValidateDorisEvolution on identical HCL, got %v", err)
	}

	// Verify round-trip with empty tenantSlug (${TENANT_SLUG} placeholder in schema reference).
	placeholderHCL := t1.BuildDorisAtlasHCL("", 0)
	if !strings.Contains(placeholderHCL, `schema = "schema.tenant_${TENANT_SLUG}"`) {
		t.Fatalf("expected quoted ${TENANT_SLUG} schema reference, got:\n%s", placeholderHCL)
	}
	placeholderParsed, err := ParseDorisAtlasHCL(placeholderHCL)
	if err != nil || len(placeholderParsed) != 1 || placeholderParsed[0].DatabaseScope != "TENANT" {
		t.Fatalf("ParseDorisAtlasHCL(placeholderHCL) failed: err=%v, parsed=%+v", err, placeholderParsed)
	}
	if v := LintDorisEvolution([]DorisTableDescriptor{t1}, placeholderParsed); len(v) != 0 {
		t.Fatalf("expected 0 violations on ${TENANT_SLUG} round-trip, got %v", v)
	}

	// Mutate HCL to trigger DORIS001..006 and verify LintDorisEvolution catches every rule!
	mutatedT1 := t1
	mutatedT1.UniqueKeyColumns = []string{"patient_id"}                         // DORIS001
	mutatedT1.SequenceCol = "other_seq"                                         // DORIS002
	mutatedT1.DistributedByColumn = "criterion_id"                              // DORIS003
	mutatedT1.ColocateGroup = "other_group"                                     // DORIS003
	mutatedT1.Columns = append([]DorisColumnDescriptor(nil), t1.Columns[:3]...) // Drop "score" -> DORIS004
	mutatedT1.Columns = append(mutatedT1.Columns, t1.Columns[4], t1.Columns[5]) // Keep publication_generation & event_month
	mutatedT1.Columns[0].DorisType = "VARCHAR(32)"                              // Narrowing VARCHAR(64)->32 -> DORIS006
	mutatedT1.Columns = append(mutatedT1.Columns,
		DorisColumnDescriptor{ColumnName: "other_seq", DorisType: "BIGINT", Nullable: false}, // Missing default -> DORIS005
	)

	mutatedHCL := mutatedT1.BuildDorisAtlasHCL("acme", 16)
	mutatedParsed, err := ParseDorisAtlasHCL(mutatedHCL)
	if err != nil {
		t.Fatalf("ParseDorisAtlasHCL(mutatedHCL) failed: %v", err)
	}
	// Clear inferred sequence default "0" on other_seq so DORIS005 triggers on the parsed descriptor.
	for i := range mutatedParsed[0].Columns {
		if mutatedParsed[0].Columns[i].ColumnName == "other_seq" {
			mutatedParsed[0].Columns[i].DefaultValue = ""
		}
	}

	violations := LintDorisEvolution([]DorisTableDescriptor{p1}, mutatedParsed)
	seenRules := make(map[DorisLintRule]bool)
	for _, v := range violations {
		seenRules[v.Rule] = true
	}
	for _, wantRule := range []DorisLintRule{
		DORIS001_IMMUTABLE_KEY,
		DORIS002_IMMUTABLE_SEQUENCE,
		DORIS003_IMMUTABLE_DISTRIBUTION,
		DORIS004_NO_COLUMN_DROP,
		DORIS005_NON_NULL_SCALAR_DEFAULT,
		DORIS006_SAFE_TYPE_EVOLUTION,
	} {
		if !seenRules[wantRule] {
			t.Errorf("expected rule %s to trigger on mutated HCL diff, got violations: %v", wantRule, violations)
		}
	}
}

// TestParseDorisAtlasHCL_InvalidSyntax verifies that malformed HCL inputs return errors.
func TestParseDorisAtlasHCL_InvalidSyntax(t *testing.T) {
	badInputs := []string{
		"",
		`schema "only_schema" {}`,
		`table "unclosed" {`,
		`table "bad-name!" {}`,
		`table "t" { column "c" { type = "unterminated } }`,
		`table "t" { /* unclosed comment }`,
		`table "t" { unknown_block {} }`,
		`table "t" { column "c" { null = false } }`,
	}
	for _, src := range badInputs {
		if _, err := ParseDorisAtlasHCL(src); err == nil {
			t.Errorf("expected error from ParseDorisAtlasHCL(%q), got nil", src)
		}
	}
}

// TestGenerateGoDoris_HCLAndAllModes verifies that doris_mode=hcl and doris_mode=all
// emit deterministic <prefix>_doris.hcl and <prefix>_iceberg.hcl artifacts.
func TestGenerateGoDoris_HCLAndAllModes(t *testing.T) {
	msgOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(msgOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			TableName:        "patients",
			DatabaseScope:    "TENANT",
			SyncTier:         "WHOLE_TABLE",
			UniqueKeyColumns: []string{"patient_id"},
		},
	})
	col1Opts := &descriptorpb.FieldOptions{}
	proto.SetExtension(col1Opts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		Column: &proto2typepb.ColumnOptions{
			DorisType:   "VARCHAR(64)",
			IcebergType: "STRING",
			IsUniqueKey: true,
		},
	})
	col2Opts := &descriptorpb.FieldOptions{}
	proto.SetExtension(col2Opts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		Column: &proto2typepb.ColumnOptions{
			DorisType:   "VARIANT",
			IcebergType: "STRING",
		},
	})

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{
				Name:    proto.String("clinical/patient.proto"),
				Package: proto.String("clinical.v1"),
				Syntax:  proto.String("proto3"),
				Options: &descriptorpb.FileOptions{
					GoPackage: proto.String("example.com/clinical/v1;clinicalv1"),
				},
				MessageType: []*descriptorpb.DescriptorProto{
					{
						Name:    proto.String("PatientRecord"),
						Options: msgOpts,
						Field: []*descriptorpb.FieldDescriptorProto{
							{
								Name:    proto.String("patient_id"),
								Number:  proto.Int32(1),
								Label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:    descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
								Options: col1Opts,
							},
							{
								Name:    proto.String("raw_fhir"),
								Number:  proto.Int32(2),
								Label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:    descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
								Options: col2Opts,
							},
						},
					},
				},
			},
		},
	}

	// 1. doris_mode=hcl emits only _doris.hcl and _iceberg.hcl (when Domain=false)
	genHCL := newPlugin(t, fds, []string{"clinical/patient.proto"})
	runnerHCL := NewRunner()
	optsHCL := &Options{
		Lang:      "go",
		Backend:   "doris",
		DorisMode: "hcl",
		Domain:    false,
	}
	for _, f := range genHCL.Files {
		if f.Generate {
			if err := runnerHCL.GenerateFile(genHCL, f, optsHCL); err != nil {
				t.Fatalf("GenerateFile(doris_mode=hcl) failed: %v", err)
			}
		}
	}
	hclFiles := make(map[string]string)
	for _, rf := range genHCL.Response().File {
		hclFiles[rf.GetName()] = rf.GetContent()
	}
	if _, ok := hclFiles["example.com/clinical/v1/patient_doris.hcl"]; !ok {
		t.Fatalf("missing example.com/clinical/v1/patient_doris.hcl; got files: %v", keysOf(hclFiles))
	}
	if _, ok := hclFiles["example.com/clinical/v1/patient_iceberg.hcl"]; !ok {
		t.Fatalf("missing example.com/clinical/v1/patient_iceberg.hcl; got files: %v", keysOf(hclFiles))
	}
	if _, ok := hclFiles["example.com/clinical/v1/patient_doris.type.go"]; ok {
		t.Errorf("unexpected _doris.type.go when doris_mode=hcl")
	}

	// 2. doris_mode=all emits Go + SQL DDL + Atlas HCL
	genAll := newPlugin(t, fds, []string{"clinical/patient.proto"})
	runnerAll := NewRunner()
	optsAll := &Options{
		Lang:      "go",
		Backend:   "doris",
		DorisMode: "all",
		Domain:    false,
	}
	for _, f := range genAll.Files {
		if f.Generate {
			if err := runnerAll.GenerateFile(genAll, f, optsAll); err != nil {
				t.Fatalf("GenerateFile(doris_mode=all) failed: %v", err)
			}
		}
	}
	allFiles := make(map[string]string)
	for _, rf := range genAll.Response().File {
		allFiles[rf.GetName()] = rf.GetContent()
	}
	for _, wantFile := range []string{
		"example.com/clinical/v1/patient_doris.type.go",
		"example.com/clinical/v1/patient_doris.sql",
		"example.com/clinical/v1/patient_iceberg.sql",
		"example.com/clinical/v1/patient_doris.hcl",
		"example.com/clinical/v1/patient_iceberg.hcl",
	} {
		if _, ok := allFiles[wantFile]; !ok {
			t.Errorf("doris_mode=all missing %s; got files: %v", wantFile, keysOf(allFiles))
		}
	}
}

// TestProperty_DorisAtlasHCL_RoundTripAndReflexivity uses property-based testing
// to verify that arbitrary valid DorisTableDescriptors round-trip through
// BuildDorisAtlasHCL -> ParseDorisAtlasHCL with zero schema evolution violations.
func TestProperty_DorisAtlasHCL_RoundTripAndReflexivity(t *testing.T) {
	scopes := []string{"TENANT", "GLOBAL_TERMINOLOGY", "GLOBAL_TERMINOLOGY_RAG", "GLOBAL_TELEMETRY"}
	varcharLens := []int{32, 64, 128, 256}

	hegel.Test(t, func(ht *hegel.T) {
		scopeIdx := hegel.Draw(ht, hegel.Integers(0, len(scopes)-1))
		vLenIdx := hegel.Draw(ht, hegel.Integers(0, len(varcharLens)-1))
		buckets := hegel.Draw(ht, hegel.Integers(0, 32))

		tbl := baseLinterTestTable()
		tbl.Columns = append(tbl.Columns, DorisColumnDescriptor{
			ColumnName:     "event_month",
			DorisType:      "VARCHAR(7)",
			IcebergType:    "STRING",
			Nullable:       false,
			IsPartitionKey: true,
			DefaultValue:   "1970-01",
		})
		tbl.DatabaseScope = scopes[scopeIdx]
		tbl.Columns[0].DorisType = "VARCHAR(" + strconvItoaTest(varcharLens[vLenIdx]) + ")"

		hcl := tbl.BuildDorisAtlasHCL("acme", buckets)
		if hcl == "" {
			ht.Fatalf("BuildDorisAtlasHCL returned empty string for %+v", tbl)
		}
		parsed, err := ParseDorisAtlasHCL(hcl)
		if err != nil || len(parsed) != 1 {
			ht.Fatalf("ParseDorisAtlasHCL failed: err=%v, len=%d", err, len(parsed))
		}
		if v := LintDorisEvolution([]DorisTableDescriptor{tbl}, parsed); len(v) != 0 {
			ht.Fatalf("round-tripped HCL produced evolution violations: %v", v)
		}
	})
}
