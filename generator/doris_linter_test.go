package generator

import (
	"reflect"
	"strings"
	"testing"

	"hegel.dev/go/hegel"
)

// baseLinterTestTable constructs a canonical DorisTableDescriptor fixture for schema evolution linter tests.
func baseLinterTestTable() DorisTableDescriptor {
	return DorisTableDescriptor{
		TableName:           "eligibility_criterion_results",
		MessageName:         "EligibilityCriterionResult",
		DatabaseScope:       "TENANT",
		SyncTier:            "MONTHLY_PARTITIONED",
		UniqueKeyColumns:    []string{"patient_id", "criterion_id"},
		PartitionByColumns:  []string{"event_month"},
		ColocateGroup:       "azra_patient_group",
		SequenceCol:         "publication_generation",
		DistributedByColumn: "patient_id",
		Columns: []DorisColumnDescriptor{
			{ColumnName: "patient_id", DorisType: "VARCHAR(64)", IcebergType: "STRING", Nullable: false, IsUniqueKey: true},
			{ColumnName: "criterion_id", DorisType: "VARCHAR(64)", IcebergType: "STRING", Nullable: false, IsUniqueKey: true},
			{ColumnName: "display_order", DorisType: "SMALLINT", IcebergType: "INT", Nullable: false, DefaultValue: "0"},
			{ColumnName: "score", DorisType: "FLOAT", IcebergType: "FLOAT", Nullable: true},
			{ColumnName: "publication_generation", DorisType: "BIGINT", IcebergType: "LONG", Nullable: false, IsSequenceCol: true, DefaultValue: "0"},
		},
	}
}

// TestLintDorisTableEvolution_ValidEvolution verifies that allowed schema widenings and safe column additions produce zero violations.
func TestLintDorisTableEvolution_ValidEvolution(t *testing.T) {
	before := baseLinterTestTable()
	after := baseLinterTestTable()

	// Safe widenings:
	// - criterion_id: VARCHAR(64) -> VARCHAR(128)
	// - display_order: SMALLINT -> INT
	// - score: FLOAT -> DOUBLE
	after.Columns[1].DorisType = "VARCHAR(128)"
	after.Columns[2].DorisType = "INT"
	after.Columns[3].DorisType = "DOUBLE"

	// Safe column additions:
	// 1. Nullable scalar without DefaultValue
	// 2. NOT NULL scalar with explicit DefaultValue
	// 3. NOT NULL ARRAY<VARCHAR(64)> / JSON / VARIANT (non-scalar exemptions)
	after.Columns = append(after.Columns,
		DorisColumnDescriptor{ColumnName: "optional_note", DorisType: "VARCHAR(256)", Nullable: true},
		DorisColumnDescriptor{ColumnName: "record_status", DorisType: "VARCHAR(32)", Nullable: false, DefaultValue: "ACTIVE"},
		DorisColumnDescriptor{ColumnName: "tags", DorisType: "ARRAY<VARCHAR(64)>", Nullable: false},
		DorisColumnDescriptor{ColumnName: "raw_json", DorisType: "JSON", Nullable: false},
		DorisColumnDescriptor{ColumnName: "raw_variant", DorisType: "VARIANT", Nullable: false},
	)

	violations := LintDorisTableEvolution(before, after)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations for valid evolution, got %v", violations)
	}
	if err := ValidateDorisTableEvolution(before, after); err != nil {
		t.Fatalf("expected nil error from ValidateDorisTableEvolution, got %v", err)
	}
	if got := before.LintEvolution(after); len(got) != 0 {
		t.Fatalf("expected 0 violations from before.LintEvolution(after), got %v", got)
	}
}

// TestLintDorisTableEvolution_DORIS001_ImmutableKey verifies that mutating UNIQUE KEY or DUPLICATE KEY column tuples triggers DORIS001_IMMUTABLE_KEY.
func TestLintDorisTableEvolution_DORIS001_ImmutableKey(t *testing.T) {
	// 1. Mutating UniqueKeyColumns
	beforeUK := baseLinterTestTable()
	afterUK := baseLinterTestTable()
	afterUK.UniqueKeyColumns = []string{"patient_id", "criterion_id", "display_order"}

	vUK := LintDorisTableEvolution(beforeUK, afterUK)
	if len(vUK) != 1 || vUK[0].Rule != DORIS001_IMMUTABLE_KEY {
		t.Fatalf("expected [DORIS001_IMMUTABLE_KEY] on UniqueKeyColumns change, got %v", vUK)
	}

	// 2. Mutating DuplicateKeyColumns
	beforeDK := DorisTableDescriptor{
		TableName:           "clinical_concept_search",
		DuplicateKeyColumns: []string{"concept_id", "code_system"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "concept_id", DorisType: "VARCHAR(64)"},
			{ColumnName: "code_system", DorisType: "VARCHAR(32)"},
		},
	}
	afterDK := DorisTableDescriptor{
		TableName:           "clinical_concept_search",
		DuplicateKeyColumns: []string{"concept_id"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "concept_id", DorisType: "VARCHAR(64)"},
			{ColumnName: "code_system", DorisType: "VARCHAR(32)"},
		},
	}
	vDK := LintDorisTableEvolution(beforeDK, afterDK)
	if len(vDK) != 1 || vDK[0].Rule != DORIS001_IMMUTABLE_KEY {
		t.Fatalf("expected [DORIS001_IMMUTABLE_KEY] on DuplicateKeyColumns change, got %v", vDK)
	}

	// 3. Mutating inferred IsUniqueKey on columns when UniqueKeyColumns slice is empty
	beforeInferred := DorisTableDescriptor{
		TableName: "patients",
		Columns: []DorisColumnDescriptor{
			{ColumnName: "patient_id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
			{ColumnName: "mrn", DorisType: "VARCHAR(64)"},
		},
	}
	afterInferred := DorisTableDescriptor{
		TableName: "patients",
		Columns: []DorisColumnDescriptor{
			{ColumnName: "patient_id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
			{ColumnName: "mrn", DorisType: "VARCHAR(64)", IsUniqueKey: true},
		},
	}
	vInf := LintDorisTableEvolution(beforeInferred, afterInferred)
	if len(vInf) != 1 || vInf[0].Rule != DORIS001_IMMUTABLE_KEY {
		t.Fatalf("expected [DORIS001_IMMUTABLE_KEY] on inferred IsUniqueKey change, got %v", vInf)
	}
}

// TestLintDorisTableEvolution_DORIS002_ImmutableSequence verifies that mutating or removing function_column.sequence_col triggers DORIS002_IMMUTABLE_SEQUENCE.
func TestLintDorisTableEvolution_DORIS002_ImmutableSequence(t *testing.T) {
	before := baseLinterTestTable()
	after := baseLinterTestTable()
	after.SequenceCol = "updated_generation"

	violations := LintDorisTableEvolution(before, after)
	if len(violations) != 1 || violations[0].Rule != DORIS002_IMMUTABLE_SEQUENCE {
		t.Fatalf("expected [DORIS002_IMMUTABLE_SEQUENCE], got %v", violations)
	}
	if !strings.Contains(violations[0].Error(), "DORIS002_IMMUTABLE_SEQUENCE") {
		t.Errorf("unexpected error string: %s", violations[0].Error())
	}

	// Also verify dropping SequenceCol triggers DORIS002_IMMUTABLE_SEQUENCE
	afterDroppedSeq := baseLinterTestTable()
	afterDroppedSeq.SequenceCol = ""
	afterDroppedSeq.Columns[4].IsSequenceCol = false
	vDrop := LintDorisTableEvolution(before, afterDroppedSeq)
	if len(vDrop) != 1 || vDrop[0].Rule != DORIS002_IMMUTABLE_SEQUENCE {
		t.Fatalf("expected [DORIS002_IMMUTABLE_SEQUENCE] when clearing SequenceCol, got %v", vDrop)
	}
}

// TestLintDorisTableEvolution_DORIS003_ImmutableDistribution verifies that mutating DISTRIBUTED BY HASH or colocate_with triggers DORIS003_IMMUTABLE_DISTRIBUTION.
func TestLintDorisTableEvolution_DORIS003_ImmutableDistribution(t *testing.T) {
	// 1. Mutating DistributedByColumn
	before := baseLinterTestTable()
	afterDist := baseLinterTestTable()
	afterDist.DistributedByColumn = "criterion_id"

	vDist := LintDorisTableEvolution(before, afterDist)
	if len(vDist) != 1 || vDist[0].Rule != DORIS003_IMMUTABLE_DISTRIBUTION {
		t.Fatalf("expected [DORIS003_IMMUTABLE_DISTRIBUTION] on DistributedByColumn change, got %v", vDist)
	}

	// 2. Mutating ColocateGroup (colocate_with)
	afterColocate := baseLinterTestTable()
	afterColocate.ColocateGroup = "other_group"

	vColocate := LintDorisTableEvolution(before, afterColocate)
	if len(vColocate) != 1 || vColocate[0].Rule != DORIS003_IMMUTABLE_DISTRIBUTION {
		t.Fatalf("expected [DORIS003_IMMUTABLE_DISTRIBUTION] on ColocateGroup change, got %v", vColocate)
	}

	// 3. Mutating both DistributedByColumn and ColocateGroup emits 2 violations
	afterBoth := baseLinterTestTable()
	afterBoth.DistributedByColumn = "criterion_id"
	afterBoth.ColocateGroup = ""
	vBoth := LintDorisTableEvolution(before, afterBoth)
	if len(vBoth) != 2 || vBoth[0].Rule != DORIS003_IMMUTABLE_DISTRIBUTION || vBoth[1].Rule != DORIS003_IMMUTABLE_DISTRIBUTION {
		t.Fatalf("expected 2 DORIS003_IMMUTABLE_DISTRIBUTION violations, got %v", vBoth)
	}
}

// TestLintDorisTableEvolution_DORIS004_NoColumnDrop verifies that dropping an existing column triggers DORIS004_NO_COLUMN_DROP.
func TestLintDorisTableEvolution_DORIS004_NoColumnDrop(t *testing.T) {
	before := baseLinterTestTable()
	after := baseLinterTestTable()
	// Drop "score" (index 3)
	after.Columns = append([]DorisColumnDescriptor(nil), before.Columns[:3]...)
	after.Columns = append(after.Columns, before.Columns[4])

	violations := LintDorisTableEvolution(before, after)
	if len(violations) != 1 || violations[0].Rule != DORIS004_NO_COLUMN_DROP || violations[0].ColumnName != "score" {
		t.Fatalf("expected [DORIS004_NO_COLUMN_DROP] for column 'score', got %v", violations)
	}
}

// TestLintDorisTableEvolution_DORIS005_NonNullScalarDefault verifies that adding a non-nullable scalar column without an explicit default value triggers DORIS005_NON_NULL_SCALAR_DEFAULT.
func TestLintDorisTableEvolution_DORIS005_NonNullScalarDefault(t *testing.T) {
	before := baseLinterTestTable()
	after := baseLinterTestTable()
	after.Columns = append(after.Columns,
		// Missing DefaultValue on NOT NULL scalar -> violation
		DorisColumnDescriptor{ColumnName: "new_required_col", DorisType: "VARCHAR(64)", Nullable: false, DefaultValue: ""},
		// Explicit DefaultValue on NOT NULL scalar -> allowed
		DorisColumnDescriptor{ColumnName: "new_defaulted_col", DorisType: "INT", Nullable: false, DefaultValue: "0"},
		// Nullable scalar without DefaultValue -> allowed
		DorisColumnDescriptor{ColumnName: "new_nullable_col", DorisType: "VARCHAR(64)", Nullable: true},
		// Non-scalar columns (ARRAY, MAP, STRUCT, JSON, JSONB, VARIANT) -> exempt
		DorisColumnDescriptor{ColumnName: "new_arr", DorisType: "ARRAY<STRING>", Nullable: false},
		DorisColumnDescriptor{ColumnName: "new_map", DorisType: "MAP<VARCHAR(32), INT>", Nullable: false},
		DorisColumnDescriptor{ColumnName: "new_struct", DorisType: "STRUCT<a:INT>", Nullable: false},
		DorisColumnDescriptor{ColumnName: "new_json", DorisType: "JSON", Nullable: false},
		DorisColumnDescriptor{ColumnName: "new_jsonb", DorisType: "JSONB", Nullable: false},
		DorisColumnDescriptor{ColumnName: "new_variant", DorisType: "VARIANT", Nullable: false},
	)

	violations := LintDorisTableEvolution(before, after)
	if len(violations) != 1 || violations[0].Rule != DORIS005_NON_NULL_SCALAR_DEFAULT || violations[0].ColumnName != "new_required_col" {
		t.Fatalf("expected single DORIS005_NON_NULL_SCALAR_DEFAULT violation on new_required_col, got %v", violations)
	}
}

// TestLintDorisTableEvolution_DORIS006_SafeTypeEvolution verifies safe widening transitions and rejects narrowing or incompatible type changes under DORIS006_SAFE_TYPE_EVOLUTION.
func TestLintDorisTableEvolution_DORIS006_SafeTypeEvolution(t *testing.T) {
	allowedCases := []struct {
		before string
		after  string
	}{
		{"", ""},
		{"VARCHAR(32)", "VARCHAR(32)"},
		{"VARCHAR(32)", "VARCHAR(64)"},
		{"varchar(64)", "VARCHAR(256)"},
		{"TINYINT", "SMALLINT"},
		{"TINYINT", "INT"},
		{"TINYINT", "BIGINT"},
		{"SMALLINT", "INT"},
		{"SMALLINT", "BIGINT"},
		{"INT", "BIGINT"},
		{"FLOAT", "DOUBLE"},
		{"DATEV2", "DATEV2"},
		{"DATEV2", "DATE"},
		{"DATE", "DATEV2"},
		{"DATETIMEV2(6)", "DATETIMEV2(6)"},
		{"DATETIMEV2(6)", "DATETIME"},
		{"DATETIME", "DATETIMEV2(6)"},
		{"DECIMALV3(10, 2)", "DECIMAL(10, 2)"},
		{"DECIMAL(10, 2)", "DECIMALV3(10, 2)"},
		{"DECIMAL(10, 2)", "DECIMAL(18, 2)"},
		{"DECIMALV3(10, 2)", "DECIMAL(18, 2)"},
		{"DECIMALV3(10, 2)", "DECIMALV3(18, 2)"},
		{"TEXT", "STRING"},
		{"STRING", "TEXT"},
		{"ARRAY<FLOAT>", "ARRAY<FLOAT>"},
		{"ARRAY<VARCHAR(32)>", "ARRAY<VARCHAR(64)>"},
		{"ARRAY<TEXT>", "ARRAY<STRING>"},
		{"ARRAY<STRING>", "ARRAY<TEXT>"},
		{"ARRAY<DATEV2>", "ARRAY<DATE>"},
	}
	for _, tc := range allowedCases {
		if !IsSafeDorisTypeEvolution(tc.before, tc.after) {
			t.Errorf("expected IsSafeDorisTypeEvolution(%q, %q) = true, got false", tc.before, tc.after)
		}
	}

	rejectedCases := []struct {
		before string
		after  string
	}{
		{"VARCHAR(64)", "VARCHAR(32)"},               // Narrowing VARCHAR
		{"VARCHAR(64)", "TEXT"},                      // Incompatible with LSC / key rules
		{"VARCHAR(64)", "INT"},                       // Incompatible type family
		{"BIGINT", "INT"},                            // Narrowing integer
		{"BIGINT", "SMALLINT"},                       // Narrowing integer
		{"BIGINT", "TINYINT"},                        // Narrowing integer
		{"BIGINT", "LARGEINT"},                       // Incompatible in Iceberg v2 (LONG -> DECIMAL(38,0))
		{"INT", "SMALLINT"},                          // Narrowing integer
		{"INT", "TINYINT"},                           // Narrowing integer
		{"SMALLINT", "TINYINT"},                      // Narrowing integer
		{"DOUBLE", "FLOAT"},                          // Narrowing float
		{"FLOAT", "INT"},                             // Float to int
		{"INT", "DOUBLE"},                            // Int to double
		{"DATEV2", "DATETIMEV2(6)"},                  // Date to datetime
		{"DECIMAL(10, 2)", "DECIMAL(10, 4)"},         // Scale change
		{"DECIMALV3(10, 2)", "DECIMAL(18, 4)"},       // Scale change
		{"DECIMAL(10, 2)", "DECIMAL(8, 2)"},          // Precision narrowing
		{"DECIMALV3(10, 2)", "DECIMAL(8, 2)"},        // Precision narrowing
		{"ARRAY<VARCHAR(64)>", "ARRAY<VARCHAR(32)>"}, // Array element narrowing
		{"BOOLEAN", "INT"},                           // Boolean to int
		{"", "INT"},                                  // Empty before type
		{"INT", ""},                                  // Empty after type
	}
	for _, tc := range rejectedCases {
		if IsSafeDorisTypeEvolution(tc.before, tc.after) {
			t.Errorf("expected IsSafeDorisTypeEvolution(%q, %q) = false, got true", tc.before, tc.after)
		}
	}

	// Verify table-level DORIS006 violation emission
	before := baseLinterTestTable()
	after := baseLinterTestTable()
	after.Columns[0].DorisType = "VARCHAR(32)" // Narrowing from VARCHAR(64) -> VARCHAR(32)
	after.Columns[2].DorisType = "TINYINT"     // Narrowing from SMALLINT -> TINYINT

	violations := LintDorisTableEvolution(before, after)
	if len(violations) != 2 {
		t.Fatalf("expected 2 DORIS006_SAFE_TYPE_EVOLUTION violations, got %v", violations)
	}
	for _, v := range violations {
		if v.Rule != DORIS006_SAFE_TYPE_EVOLUTION {
			t.Errorf("expected rule DORIS006_SAFE_TYPE_EVOLUTION, got %s", v.Rule)
		}
	}
}

// TestLintDorisSchemaEvolution_MultiTableSlice verifies multi-table schema evolution linting and error aggregation across table slices.
func TestLintDorisSchemaEvolution_MultiTableSlice(t *testing.T) {
	t1Before := baseLinterTestTable()
	t2Before := DorisTableDescriptor{
		TableName:           "clinical_concept_search",
		DuplicateKeyColumns: []string{"concept_id", "code_system"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "concept_id", DorisType: "VARCHAR(64)"},
			{ColumnName: "code_system", DorisType: "VARCHAR(32)"},
		},
	}

	t1After := baseLinterTestTable()
	t1After.Columns[1].DorisType = "VARCHAR(128)" // Valid widening

	t2After := t2Before
	t2After.DuplicateKeyColumns = []string{"concept_id"} // Violates DORIS001

	// Newly added table t3After has no 'before' counterpart -> allowed without evolution violations
	t3After := DorisTableDescriptor{
		TableName:        "brand_new_table",
		UniqueKeyColumns: []string{"id"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
		},
	}

	violations := LintDorisSchemaEvolution(
		[]DorisTableDescriptor{t1Before, t2Before},
		[]DorisTableDescriptor{t1After, t2After, t3After},
	)
	if len(violations) != 1 || violations[0].TableName != "clinical_concept_search" || violations[0].Rule != DORIS001_IMMUTABLE_KEY {
		t.Fatalf("unexpected schema evolution violations: %v", violations)
	}
	if err := ValidateDorisSchemaEvolution(
		[]DorisTableDescriptor{t1Before, t2Before},
		[]DorisTableDescriptor{t1After, t2After, t3After},
	); err == nil {
		t.Fatal("expected non-nil error from ValidateDorisSchemaEvolution")
	}
	if err := ValidateDorisSchemaEvolution(
		[]DorisTableDescriptor{t1Before},
		[]DorisTableDescriptor{t1After, t3After},
	); err != nil {
		t.Fatalf("expected nil error for safe multi-table evolution, got %v", err)
	}
}

// TestProperty_DorisLinter_DeterminismAndReflexivity verifies reflexivity, determinism, and widening partial-order properties of the Doris schema evolution linter.
func TestProperty_DorisLinter_DeterminismAndReflexivity(t *testing.T) {
	intLadder := []string{"TINYINT", "SMALLINT", "INT", "BIGINT"}

	hegel.Test(t, func(ht *hegel.T) {
		tbl := baseLinterTestTable()
		// Reflexivity: any table evolved to itself must produce 0 violations.
		if v := LintDorisTableEvolution(tbl, tbl); len(v) != 0 {
			ht.Fatalf("reflexive evolution of table to itself produced violations: %v", v)
		}

		// Integer widening order property: i <= j iff IsSafeDorisTypeEvolution(intLadder[i], intLadder[j])
		i := hegel.Draw(ht, hegel.Integers(0, len(intLadder)-1))
		j := hegel.Draw(ht, hegel.Integers(0, len(intLadder)-1))
		gotSafe := IsSafeDorisTypeEvolution(intLadder[i], intLadder[j])
		wantSafe := i <= j
		if gotSafe != wantSafe {
			ht.Fatalf("IsSafeDorisTypeEvolution(%q, %q) = %v, want %v", intLadder[i], intLadder[j], gotSafe, wantSafe)
		}

		// VARCHAR(m) -> VARCHAR(n): safe iff n >= m
		m := hegel.Draw(ht, hegel.Integers(1, 512))
		n := hegel.Draw(ht, hegel.Integers(1, 512))
		v1 := baseLinterTestTable()
		v2 := baseLinterTestTable()
		v1.Columns[0].DorisType = "VARCHAR(" + strconvItoaTest(m) + ")"
		v2.Columns[0].DorisType = "VARCHAR(" + strconvItoaTest(n) + ")"
		res1 := LintDorisTableEvolution(v1, v2)
		res2 := LintDorisTableEvolution(v1, v2)
		if !reflect.DeepEqual(res1, res2) {
			ht.Fatalf("LintDorisTableEvolution non-deterministic")
		}
		if n >= m && len(res1) != 0 {
			ht.Fatalf("expected 0 violations for VARCHAR(%d) -> VARCHAR(%d), got %v", m, n, res1)
		}
		if n < m && (len(res1) != 1 || res1[0].Rule != DORIS006_SAFE_TYPE_EVOLUTION) {
			ht.Fatalf("expected DORIS006_SAFE_TYPE_EVOLUTION for VARCHAR(%d) -> VARCHAR(%d), got %v", m, n, res1)
		}
	})
}

// strconvItoaTest formats a non-negative integer as a decimal string for property tests.
func strconvItoaTest(n int) string {
	var b strings.Builder
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	b.Write(digits[i:])
	return b.String()
}
