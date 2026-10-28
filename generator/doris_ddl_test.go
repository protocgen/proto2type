package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// TestBuildStarRocksCreateTableSQL_RuntimeVsPluginParity verifies parity between plugin-side
// and emitted runtime-side Doris/StarRocks DDL and HCL builders.
func TestBuildStarRocksCreateTableSQL_RuntimeVsPluginParity(t *testing.T) {
	fdp := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("runtime_parity.proto"),
		Package: proto.String("parity.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/parity/v1;parityv1"),
		},
	}
	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"runtime_parity.proto"})
	g := gen.NewGeneratedFile("runtime_helpers.go", "example.com/parity/v1")
	g.P("package parityv1")
	g.P()
	emitDorisSharedRuntimeTypes(g)

	var runtimeCode string
	for _, gf := range gen.Response().File {
		if gf.GetName() == "runtime_helpers.go" {
			runtimeCode = gf.GetContent()
			break
		}
	}
	if runtimeCode == "" {
		t.Fatal("failed to emit runtime_helpers.go")
	}

	tmpPkgDir, err := os.MkdirTemp(".", "doris_sr_parity_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpPkgDir)
	}()

	if err := os.WriteFile(filepath.Join(tmpPkgDir, "runtime_helpers.go"), []byte(runtimeCode), 0o644); err != nil {
		t.Fatalf("write runtime_helpers.go: %v", err)
	}

	parityTestSrc := `package parityv1

import (
	"reflect"
	"strings"
	"testing"

	genpkg "github.com/protocgen/proto2type/generator"
)

func toRuntimeDesc(p genpkg.DorisTableDescriptor) DorisTableDescriptor {
	cols := make([]DorisColumnDescriptor, len(p.Columns))
	for i, c := range p.Columns {
		cols[i] = DorisColumnDescriptor{
			ColumnName:      c.ColumnName,
			ProtoFieldName:  c.ProtoFieldName,
			DorisType:       c.DorisType,
			IcebergType:     c.IcebergType,
			Nullable:        c.Nullable,
			InvertedIndex:   c.InvertedIndex,
			IsUniqueKey:     c.IsUniqueKey,
			IsSequenceCol:   c.IsSequenceCol,
			IsPartitionKey:  c.IsPartitionKey,
			StripEnumPrefix: c.StripEnumPrefix,
			DefaultValue:    c.DefaultValue,
			Comment:         c.Comment,
		}
	}
	idxs := make([]DorisIndexDescriptor, len(p.Indexes))
	for i, idx := range p.Indexes {
		var props map[string]string
		if idx.Properties != nil {
			props = make(map[string]string, len(idx.Properties))
			for k, v := range idx.Properties {
				props[k] = v
			}
		}
		idxs[i] = DorisIndexDescriptor{
			Name:       idx.Name,
			Columns:    append([]string(nil), idx.Columns...),
			Using:      idx.Using,
			Properties: props,
		}
	}
	return DorisTableDescriptor{
		TableName:            p.TableName,
		MessageName:          p.MessageName,
		DatabaseScope:        p.DatabaseScope,
		SyncTier:             p.SyncTier,
		UniqueKeyColumns:     append([]string(nil), p.UniqueKeyColumns...),
		PartitionByColumns:   append([]string(nil), p.PartitionByColumns...),
		ColocateGroup:        p.ColocateGroup,
		SequenceCol:          p.SequenceCol,
		SequenceColumn:       p.SequenceColumn,
		InvertedIndexColumns: append([]string(nil), p.InvertedIndexColumns...),
		NgramBfColumns:       append([]string(nil), p.NgramBfColumns...),
		BloomFilterColumns:   append([]string(nil), p.BloomFilterColumns...),
		DistributedByColumn:  p.DistributedByColumn,
		DistributionColumns:  append([]string(nil), p.DistributionColumns...),
		DuplicateKeyColumns:  append([]string(nil), p.DuplicateKeyColumns...),
		FileCacheTTLSeconds:  p.FileCacheTTLSeconds,
		EmbeddingDimension:   p.EmbeddingDimension,
		StorageVaultName:     p.StorageVaultName,
		IcebergCatalog:       p.IcebergCatalog,
		IcebergPartitionSpec: p.IcebergPartitionSpec,
		WriterService:        p.WriterService,
		ReaderServices:       append([]string(nil), p.ReaderServices...),
		Buckets:              p.Buckets,
		AutoBuckets:          p.AutoBuckets,
		EnableMoW:            p.EnableMoW,
		Indexes:              idxs,
		Columns:              cols,
	}
}

func TestRuntimeVsPlugin_StarRocksAndDorisParity(t *testing.T) {
	cases := []struct {
		name      string
		desc      genpkg.DorisTableDescriptor
		slug      string
		buckets   int
		wantEmpty bool
		wantInSR  []string
	}{
		{
			name: "DATETIMEV2(6) primary key with NgramBfColumns, InvertedIndexColumns, DistributionColumns",
			desc: genpkg.DorisTableDescriptor{
				TableName:            "clinical_events",
				DatabaseScope:        "TENANT",
				SyncTier:             "WHOLE_TABLE",
				UniqueKeyColumns:     []string{"tenant_id", "event_ts"},
				DistributionColumns:  []string{"event_ts"},
				NgramBfColumns:       []string{"summary_text"},
				InvertedIndexColumns: []string{"status_code", "raw_tag", "clinical_note"},
				BloomFilterColumns:   []string{"status_code"},
				StorageVaultName:     "s3_shared_vol",
				ColocateGroup:        "cg_events",
				SequenceCol:          "seq_num",
				Columns: []genpkg.DorisColumnDescriptor{
					{ColumnName: "tenant_id", DorisType: "VARCHAR(64)", Nullable: false, IsUniqueKey: true},
					{ColumnName: "event_ts", DorisType: "DATETIMEV2(6)", Nullable: false, IsUniqueKey: true},
					{ColumnName: "status_code", DorisType: "VARCHAR(32)", Nullable: false, InvertedIndex: true},
					{ColumnName: "raw_tag", DorisType: "STRING", Nullable: false, InvertedIndex: true},
					{ColumnName: "clinical_note", DorisType: "TEXT", Nullable: false, InvertedIndex: true},
					{ColumnName: "summary_text", DorisType: "VARCHAR(256)", Nullable: false},
					{ColumnName: "tags", DorisType: "ARRAY<TEXT>", Nullable: false},
					{ColumnName: "seq_num", DorisType: "BIGINT", Nullable: false, IsSequenceCol: true},
				},
			},
			slug:    "acme",
			buckets: 8,
			wantInSR: []string{
				"event_ts DATETIME NOT NULL",
				"tags ARRAY<STRING> NOT NULL DEFAULT \"[]\"",
				"INDEX idx_clinical_events_status_code (status_code) USING BITMAP",
				"INDEX idx_raw_tag_bm25 (raw_tag) USING GIN(\"parser\" = \"english\")",
				"INDEX idx_clinical_note_bm25 (clinical_note) USING GIN(\"parser\" = \"english\")",
				"INDEX idx_ngram_clinical_events_summary_text (summary_text) USING NGRAMBF (\"gram_num\" = \"3\", \"bloom_filter_fpp\" = \"0.05\")",
				"PRIMARY KEY(tenant_id, event_ts)",
				"DISTRIBUTED BY HASH(event_ts) BUCKETS 8",
			},
		},
		{
			name: "DATETIMEV2(6) fallback key (no explicit UK/DK) with DistributionColumns fallback",
			desc: genpkg.DorisTableDescriptor{
				TableName:           "fallback_dt_table",
				DatabaseScope:       "TENANT",
				SyncTier:            "WHOLE_TABLE",
				DistributionColumns: []string{"shard_id"},
				Columns: []genpkg.DorisColumnDescriptor{
					{ColumnName: "created_at", DorisType: "DATETIMEV2(6)", Nullable: true},
					{ColumnName: "shard_id", DorisType: "VARCHAR(64)", Nullable: false},
				},
			},
			slug:    "acme",
			buckets: 4,
			wantInSR: []string{
				"created_at DATETIME NOT NULL",
				"DUPLICATE KEY(created_at)",
				"DISTRIBUTED BY HASH(shard_id) BUCKETS 4",
			},
		},
		{
			name: "TEXT primary key is rejected in both plugin and runtime",
			desc: genpkg.DorisTableDescriptor{
				TableName:        "bad_text_pk",
				DatabaseScope:    "TENANT",
				SyncTier:         "WHOLE_TABLE",
				UniqueKeyColumns: []string{"bad_key"},
				Columns: []genpkg.DorisColumnDescriptor{
					{ColumnName: "bad_key", DorisType: "TEXT", Nullable: false, IsUniqueKey: true},
				},
			},
			slug:      "acme",
			buckets:   4,
			wantEmpty: true,
		},
		{
			name: "TEXT fallback key (no explicit UK/DK) is rejected in both plugin and runtime",
			desc: genpkg.DorisTableDescriptor{
				TableName:     "bad_text_fallback",
				DatabaseScope: "TENANT",
				SyncTier:      "WHOLE_TABLE",
				Columns: []genpkg.DorisColumnDescriptor{
					{ColumnName: "note", DorisType: "TEXT", Nullable: false},
					{ColumnName: "id", DorisType: "VARCHAR(64)", Nullable: false},
				},
			},
			slug:      "acme",
			buckets:   4,
			wantEmpty: true,
		},
		{
			name: "Ineligible DistributionColumns (TEXT) is rejected in both plugin and runtime",
			desc: genpkg.DorisTableDescriptor{
				TableName:           "bad_dist_col",
				DatabaseScope:       "TENANT",
				SyncTier:            "WHOLE_TABLE",
				UniqueKeyColumns:    []string{"id"},
				DistributionColumns: []string{"note"},
				Columns: []genpkg.DorisColumnDescriptor{
					{ColumnName: "id", DorisType: "VARCHAR(64)", Nullable: false, IsUniqueKey: true},
					{ColumnName: "note", DorisType: "TEXT", Nullable: false},
				},
			},
			slug:      "acme",
			buckets:   4,
			wantEmpty: true,
		},
		{
			name: "DUPLICATE KEY with vector index and DistributionColumns fallback",
			desc: genpkg.DorisTableDescriptor{
				TableName:           "rag_embeddings",
				DatabaseScope:       "GLOBAL_TERMINOLOGY_RAG",
				SyncTier:            "WHOLE_TABLE",
				DuplicateKeyColumns: []string{"doc_id", "chunk_id"},
				DistributionColumns: []string{"chunk_id"},
				EmbeddingDimension:  768,
				Columns: []genpkg.DorisColumnDescriptor{
					{ColumnName: "doc_id", DorisType: "VARCHAR(64)", Nullable: false},
					{ColumnName: "chunk_id", DorisType: "INT", Nullable: false},
					{ColumnName: "embedding", DorisType: "ARRAY<FLOAT>", Nullable: false},
				},
			},
			slug:    "",
			buckets: 0,
			wantInSR: []string{
				"INDEX idx_embedding_hnsw (embedding) USING VECTOR (\"index_type\" = \"hnsw\", \"metric_type\" = \"l2_distance\", \"dim\" = \"768\")",
				"DUPLICATE KEY(doc_id, chunk_id)",
				"DISTRIBUTED BY HASH(chunk_id) BUCKETS 8",
			},
		},
		{
			name: "with_writer_and_reader_services",
			desc: genpkg.DorisTableDescriptor{
				TableName:        "patient_events",
				DatabaseScope:    "TENANT",
				SyncTier:         "WHOLE_TABLE",
				UniqueKeyColumns: []string{"tenant_id", "id"},
				WriterService:    "patient-service",
				ReaderServices:   []string{"analytics-service", "*"},
				Columns: []genpkg.DorisColumnDescriptor{
					{ColumnName: "tenant_id", DorisType: "VARCHAR(64)", Nullable: false},
					{ColumnName: "id", DorisType: "VARCHAR(64)", Nullable: false},
				},
			},
			slug:    "acme",
			buckets: 4,
			wantInSR: []string{
				"PRIMARY KEY(tenant_id, id)",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rtDesc := toRuntimeDesc(tc.desc)

			pluginSRSQL := tc.desc.BuildStarRocksCreateTableSQL(tc.slug, tc.buckets)
			rtSRSQL := rtDesc.BuildStarRocksCreateTableSQL(tc.slug, tc.buckets)
			if pluginSRSQL != rtSRSQL {
				t.Errorf("BuildStarRocksCreateTableSQL mismatch:\n--- plugin ---\n%s\n--- runtime ---\n%s", pluginSRSQL, rtSRSQL)
			}
			if tc.wantEmpty && pluginSRSQL != "" {
				t.Errorf("expected empty StarRocks SQL, got:\n%s", pluginSRSQL)
			}
			for _, want := range tc.wantInSR {
				if !strings.Contains(rtSRSQL, want) {
					t.Errorf("runtime StarRocks SQL missing %q in:\n%s", want, rtSRSQL)
				}
			}

			pluginSRHCL := tc.desc.BuildStarRocksAtlasHCL(tc.slug, tc.buckets)
			rtSRHCL := rtDesc.BuildStarRocksAtlasHCL(tc.slug, tc.buckets)
			if pluginSRHCL != rtSRHCL {
				t.Errorf("BuildStarRocksAtlasHCL mismatch:\n--- plugin ---\n%s\n--- runtime ---\n%s", pluginSRHCL, rtSRHCL)
			}
			if tc.wantEmpty && pluginSRHCL != "" {
				t.Errorf("expected empty StarRocks HCL, got:\n%s", pluginSRHCL)
			}

			pluginDorisSQL := tc.desc.BuildDorisCreateTableSQL(tc.slug, tc.buckets)
			rtDorisSQL := rtDesc.BuildDorisCreateTableSQL(tc.slug, tc.buckets)
			if pluginDorisSQL != rtDorisSQL {
				t.Errorf("BuildDorisCreateTableSQL mismatch:\n--- plugin ---\n%s\n--- runtime ---\n%s", pluginDorisSQL, rtDorisSQL)
			}

			pluginDorisHCL := tc.desc.BuildDorisAtlasHCL(tc.slug, tc.buckets)
			rtDorisHCL := rtDesc.BuildDorisAtlasHCL(tc.slug, tc.buckets)
			if pluginDorisHCL != rtDorisHCL {
				t.Errorf("BuildDorisAtlasHCL mismatch:\n--- plugin ---\n%s\n--- runtime ---\n%s", pluginDorisHCL, rtDorisHCL)
			}

			if !tc.wantEmpty && rtSRHCL != "" {
				parsedRT, err := ParseDorisAtlasHCL(rtSRHCL)
				if err != nil || len(parsedRT) != 1 {
					t.Fatalf("runtime ParseDorisAtlasHCL failed: err=%v, len=%d", err, len(parsedRT))
				}
				if !reflect.DeepEqual(parsedRT[0].NgramBfColumns, tc.desc.NgramBfColumns) {
					t.Errorf("parsedRT[0].NgramBfColumns = %v, want %v", parsedRT[0].NgramBfColumns, tc.desc.NgramBfColumns)
				}
			}
		})
	}

	evolutionCases := []struct {
		before string
		after  string
		want   bool
	}{
		{"VARCHAR(32)", "VARCHAR(64)", true},
		{"VARCHAR(64)", "STRING", true},
		{"VARCHAR(64)", "TEXT", true},
		{"ARRAY<VARCHAR(64)>", "ARRAY<STRING>", true},
		{"TINYINT", "LARGEINT", true},
		{"SMALLINT", "LARGEINT", true},
		{"INT", "LARGEINT", true},
		{"BIGINT", "LARGEINT", true},
		{"LARGEINT", "BIGINT", false},
		{"DATE", "DATETIME", true},
		{"DATEV2", "DATETIMEV2(6)", true},
		{"DATETIME", "DATE", false},
		{"DATETIMEV2(6)", "DATEV2", false},
		{"DATETIME", "DATETIMEV2(6)", true},
		{"DATETIMEV2(6)", "DATETIME", false},
		{"DATETIMEV2(6)", "DATETIMEV2(3)", false},
		{"ARRAY<DATETIMEV2(6)>", "ARRAY<DATETIME>", false},
		{"DECIMALV3(10, 2)", "DECIMAL(18, 2)", true},
		{"DECIMALV3(10, 2)", "DECIMAL(8, 2)", false},
	}
	for _, ec := range evolutionCases {
		gotRT := IsSafeDorisTypeEvolution(ec.before, ec.after)
		gotPlugin := genpkg.IsSafeDorisTypeEvolution(ec.before, ec.after)
		if gotRT != gotPlugin {
			t.Errorf("IsSafeDorisTypeEvolution(%q, %q) runtime=%v != plugin=%v", ec.before, ec.after, gotRT, gotPlugin)
		}
		if gotRT != ec.want {
			t.Errorf("IsSafeDorisTypeEvolution(%q, %q) = %v, want %v", ec.before, ec.after, gotRT, ec.want)
		}
	}
}
`
	if err := os.WriteFile(filepath.Join(tmpPkgDir, "parity_test.go"), []byte(parityTestSrc), 0o644); err != nil {
		t.Fatalf("write parity_test.go: %v", err)
	}

	testCmd := exec.Command("go", "test", "-v", "./"+filepath.Base(tmpPkgDir))
	testCmd.Env = append(os.Environ(), "GOWORK=off")
	if out, err := testCmd.CombinedOutput(); err != nil {
		t.Fatalf("compiled runtime parity go test failed: %v\n%s", err, strings.TrimSpace(string(out)))
	}
}
