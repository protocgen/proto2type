package integration

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/protocgen/proto2type/testdata/golden/go/doris"
	"github.com/protocgen/proto2type/testdata/golden/go/pb"
	"google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDorisGolden_RoundTripAndSchemaEvolution(t *testing.T) {
	verifiedAt := time.Date(2026, time.October, 27, 15, 30, 0, 123456000, time.UTC)
	orig := &pb.EligibilityCriterionResult{
		PatientId:       "pat-100",
		CriterionId:     "crit-200",
		EffectiveStatus: pb.CriterionStatus_CRITERION_STATUS_MET,
		AllMrns:         []string{"MRN-1", "MRN-2"},
		FingerprintHash: uint64(math.MaxInt64) + 99,
		EventMonth: &date.Date{
			Year:  2026,
			Month: 10,
			Day:   1,
		},
		VerifiedAt: timestamppb.New(verifiedAt),
		Citation: &pb.DocumentCitation{
			PageNumber: 7,
			BboxCoords: []float32{0.1, 0.2, 0.8, 0.9},
		},
		PublicationGeneration: 42,
	}

	// 1. Proto -> Doris struct -> Values -> ScanRow -> ToProto round-trip
	vals := doris.EligibilityCriterionResultProtoValues(orig)
	gotProto, err := doris.ScanEligibilityCriterionResultProto(doris.NewDorisValueScanner(vals))
	if err != nil {
		t.Fatalf("ScanEligibilityCriterionResultProto failed: %v", err)
	}
	if !proto.Equal(orig, gotProto) {
		t.Fatalf("EligibilityCriterionResult round-trip mismatch:\norig: %+v\ngot:  %+v", orig, gotProto)
	}

	// 2. ConceptEmbeddingRecord round-trip & EmbeddingDimension descriptor check
	embOrig := &pb.ConceptEmbeddingRecord{
		ConceptId:  "C0001",
		CodeSystem: "SNOMEDCT_US",
		Embedding:  []float32{0.25, -0.5, 0.75},
	}
	embVals := doris.ConceptEmbeddingRecordProtoValues(embOrig)
	embGot, err := doris.ScanConceptEmbeddingRecordProto(doris.NewDorisValueScanner(embVals))
	if err != nil {
		t.Fatalf("ScanConceptEmbeddingRecordProto failed: %v", err)
	}
	if !proto.Equal(embOrig, embGot) {
		t.Fatalf("ConceptEmbeddingRecord round-trip mismatch:\norig: %+v\ngot:  %+v", embOrig, embGot)
	}
	embDesc := doris.ConceptEmbeddingRecordDorisTable()
	if embDesc.EmbeddingDimension != 768 {
		t.Fatalf("ConceptEmbeddingRecordDorisTable().EmbeddingDimension = %d, want 768", embDesc.EmbeddingDimension)
	}
	if !strings.Contains(embDesc.BuildDorisCreateTableSQL("", 0), `"dim" = "768"`) {
		t.Fatalf("expected HNSW dim=768 in Doris DDL:\n%s", embDesc.BuildDorisCreateTableSQL("", 0))
	}
	if !strings.Contains(embDesc.BuildStarRocksCreateTableSQL("", 0), `"dim" = "768"`) {
		t.Fatalf("expected HNSW dim=768 in StarRocks DDL:\n%s", embDesc.BuildStarRocksCreateTableSQL("", 0))
	}

	// 3. Verify emitted schema evolution linter & ALTER TABLE builders in golden package
	beforeTbl := doris.EligibilityCriterionResultDorisTable()
	afterSafe := doris.EligibilityCriterionResultDorisTable()
	afterSafe.Columns[1].DorisType = "VARCHAR(128)" // criterion_id: VARCHAR(64) -> VARCHAR(128)
	afterSafe.Columns = append(afterSafe.Columns, doris.DorisColumnDescriptor{
		ColumnName:  "review_comment",
		DorisType:   "VARCHAR(256)",
		IcebergType: "STRING",
		Nullable:    true,
	})

	if err := doris.ValidateDorisTableEvolution(beforeTbl, afterSafe); err != nil {
		t.Fatalf("ValidateDorisTableEvolution failed on safe evolution: %v", err)
	}
	dorisAlter, err := beforeTbl.BuildDorisAlterTableSQL(afterSafe, "acme")
	if err != nil {
		t.Fatalf("BuildDorisAlterTableSQL failed: %v", err)
	}
	wantDorisAlter := []string{
		"ALTER TABLE internal.tenant_acme.eligibility_criterion_results MODIFY COLUMN criterion_id VARCHAR(128) NOT NULL;",
		"ALTER TABLE internal.tenant_acme.eligibility_criterion_results ADD COLUMN review_comment VARCHAR(256);",
	}
	if !reflect.DeepEqual(dorisAlter, wantDorisAlter) {
		t.Fatalf("BuildDorisAlterTableSQL = %#v, want %#v", dorisAlter, wantDorisAlter)
	}

	srAlter, err := beforeTbl.BuildStarRocksAlterTableSQL(afterSafe, "acme")
	if err != nil || !reflect.DeepEqual(srAlter, wantDorisAlter) {
		t.Fatalf("BuildStarRocksAlterTableSQL = (%#v, %v), want (%#v, nil)", srAlter, err, wantDorisAlter)
	}

	iceAlter, err := beforeTbl.BuildIcebergAlterTableSQL(afterSafe, "acme")
	if err != nil {
		t.Fatalf("BuildIcebergAlterTableSQL failed: %v", err)
	}
	wantIceAlter := []string{
		"ALTER TABLE lakehouse.tenant_acme.eligibility_criterion_results MODIFY COLUMN criterion_id VARCHAR(128) NOT NULL;",
		"ALTER TABLE lakehouse.tenant_acme.eligibility_criterion_results ADD COLUMN review_comment VARCHAR(256);",
	}
	if !reflect.DeepEqual(iceAlter, wantIceAlter) {
		t.Fatalf("BuildIcebergAlterTableSQL = %#v, want %#v", iceAlter, wantIceAlter)
	}

	// 4. Verify deterministic DORIS001..DORIS006 rule ordering in golden package
	afterUnsafe := doris.EligibilityCriterionResultDorisTable()
	afterUnsafe.UniqueKeyColumns = []string{"patient_id"}
	afterUnsafe.SequenceCol = "other_seq"
	afterUnsafe.DistributedByColumn = "other_dist"
	afterUnsafe.Columns = afterUnsafe.Columns[1:] // drop patient_id -> DORIS004
	afterUnsafe.Columns[0].DorisType = "INT"      // narrow criterion_id -> DORIS006
	afterUnsafe.Columns = append(afterUnsafe.Columns, doris.DorisColumnDescriptor{
		ColumnName: "added_required_no_default",
		DorisType:  "VARCHAR(32)",
		Nullable:   false,
	}) // -> DORIS005
	violations := beforeTbl.LintEvolution(afterUnsafe)
	wantRules := []doris.DorisLintRule{
		doris.DORIS001_IMMUTABLE_KEY,
		doris.DORIS002_IMMUTABLE_SEQUENCE,
		doris.DORIS003_IMMUTABLE_DISTRIBUTION,
		doris.DORIS004_NO_COLUMN_DROP,
		doris.DORIS005_NON_NULL_SCALAR_DEFAULT,
		doris.DORIS006_SAFE_TYPE_EVOLUTION,
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
