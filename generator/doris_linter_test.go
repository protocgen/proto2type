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
		{"VARCHAR(64)", "STRING"},
		{"VARCHAR(64)", "TEXT"},
		{"TINYINT", "SMALLINT"},
		{"TINYINT", "INT"},
		{"TINYINT", "BIGINT"},
		{"TINYINT", "LARGEINT"},
		{"SMALLINT", "INT"},
		{"SMALLINT", "BIGINT"},
		{"SMALLINT", "LARGEINT"},
		{"INT", "BIGINT"},
		{"INT", "LARGEINT"},
		{"BIGINT", "LARGEINT"},
		{"FLOAT", "DOUBLE"},
		{"DATEV2", "DATEV2"},
		{"DATEV2", "DATE"},
		{"DATE", "DATEV2"},
		{"DATE", "DATETIME"},
		{"DATEV2", "DATETIMEV2(6)"},
		{"DATETIME", "DATETIMEV2"},
		{"DATETIMEV2(0)", "DATETIME"},
		{"DATETIME", "DATETIMEV2(6)"},
		{"DATETIMEV2(3)", "DATETIMEV2(6)"},
		{"DATETIMEV2(6)", "DATETIMEV2(6)"},
		{"DATETIME(6)", "DATETIMEV2(6)"},
		{"DATETIMEV2(6)", "DATETIME(6)"},
		{"ARRAY<DATETIME>", "ARRAY<DATETIMEV2(6)>"},
		{"DECIMALV3(10, 2)", "DECIMAL(10, 2)"},
		{"DECIMAL(10, 2)", "DECIMALV3(10, 2)"},
		{"DECIMAL(10, 2)", "DECIMAL(18, 2)"},
		{"DECIMALV3(10, 2)", "DECIMAL(18, 2)"},
		{"DECIMALV3(10, 2)", "DECIMALV3(18, 2)"},
		{"TEXT", "STRING"},
		{"STRING", "TEXT"},
		{"ARRAY<FLOAT>", "ARRAY<FLOAT>"},
		{"ARRAY<VARCHAR(32)>", "ARRAY<VARCHAR(64)>"},
		{"ARRAY<VARCHAR(64)>", "ARRAY<STRING>"},
		{"ARRAY<VARCHAR(64)>", "ARRAY<TEXT>"},
		{"ARRAY<TEXT>", "ARRAY<STRING>"},
		{"ARRAY<STRING>", "ARRAY<TEXT>"},
		{"ARRAY<DATEV2>", "ARRAY<DATE>"},
		{"ARRAY<DATE>", "ARRAY<DATETIME>"},
		{"ARRAY<DATEV2>", "ARRAY<DATETIMEV2(6)>"},
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
		{"STRING", "VARCHAR(64)"},                    // Narrowing STRING to VARCHAR
		{"TEXT", "VARCHAR(64)"},                      // Narrowing TEXT to VARCHAR
		{"VARCHAR(64)", "INT"},                       // Incompatible type family
		{"LARGEINT", "BIGINT"},                       // Narrowing integer
		{"LARGEINT", "INT"},                          // Narrowing integer
		{"LARGEINT", "SMALLINT"},                     // Narrowing integer
		{"LARGEINT", "TINYINT"},                      // Narrowing integer
		{"BIGINT", "INT"},                            // Narrowing integer
		{"BIGINT", "SMALLINT"},                       // Narrowing integer
		{"BIGINT", "TINYINT"},                        // Narrowing integer
		{"INT", "SMALLINT"},                          // Narrowing integer
		{"INT", "TINYINT"},                           // Narrowing integer
		{"SMALLINT", "TINYINT"},                      // Narrowing integer
		{"DOUBLE", "FLOAT"},                          // Narrowing float
		{"FLOAT", "INT"},                             // Float to int
		{"INT", "DOUBLE"},                            // Int to double
		{"DATETIME", "DATE"},                         // Datetime to date narrowing
		{"DATETIMEV2(6)", "DATEV2"},                  // Datetime to date narrowing
		{"DATE", "DATETIMEV2(99)"},                   // Invalid datetime precision
		{"DATETIMEV2(6)", "DATETIME"},                // Datetime precision narrowing (6 -> 0)
		{"DATETIMEV2(6)", "DATETIMEV2(0)"},           // Datetime precision narrowing (6 -> 0)
		{"DATETIMEV2(6)", "DATETIMEV2(3)"},           // Datetime precision narrowing (6 -> 3)
		{"ARRAY<DATETIMEV2(6)>", "ARRAY<DATETIME>"},  // Array datetime precision narrowing
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

	// Verify that tables sharing the same TableName across different DatabaseScopes
	// (e.g., TENANT vs GLOBAL_TELEMETRY) or IcebergCatalogs are paired by full physical
	// table identity and do not overwrite or cross-pair each other.
	tenantEventsBefore := DorisTableDescriptor{
		TableName:        "audit_events",
		DatabaseScope:    "TENANT",
		UniqueKeyColumns: []string{"tenant_id", "event_id"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "tenant_id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
			{ColumnName: "event_id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
		},
	}
	globalEventsBefore := DorisTableDescriptor{
		TableName:        "audit_events",
		DatabaseScope:    "GLOBAL_TELEMETRY",
		UniqueKeyColumns: []string{"event_id"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "event_id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
		},
	}
	tenantEventsAfter := tenantEventsBefore
	tenantEventsAfter.Columns = append([]DorisColumnDescriptor(nil), tenantEventsBefore.Columns...)
	tenantEventsAfter.Columns[1].DorisType = "VARCHAR(128)" // Safe widening

	globalEventsAfter := globalEventsBefore
	globalEventsAfter.UniqueKeyColumns = []string{"event_id", "extra_key"} // Violates DORIS001 on GLOBAL_TELEMETRY only

	scopedViolations := LintDorisSchemaEvolution(
		[]DorisTableDescriptor{tenantEventsBefore, globalEventsBefore},
		[]DorisTableDescriptor{tenantEventsAfter, globalEventsAfter},
	)
	if len(scopedViolations) != 1 || scopedViolations[0].Rule != DORIS001_IMMUTABLE_KEY {
		t.Fatalf("expected 1 DORIS001 violation on GLOBAL_TELEMETRY audit_events, got %v", scopedViolations)
	}

	// Verify deterministic rule ordering DORIS001..DORIS006 even when an existing narrowed
	// column (DORIS006) precedes a newly added NOT NULL scalar column without DefaultValue (DORIS005).
	orderBefore := baseLinterTestTable()
	orderAfter := baseLinterTestTable()
	orderAfter.UniqueKeyColumns = []string{"patient_id"}
	orderAfter.SequenceCol = "other_seq"
	orderAfter.DistributedByColumn = "other_dist"
	orderAfter.Columns = orderAfter.Columns[1:] // drop patient_id -> DORIS004
	orderAfter.Columns[0].DorisType = "INT"     // narrow criterion_id -> DORIS006
	orderAfter.Columns = append(orderAfter.Columns, DorisColumnDescriptor{
		ColumnName: "added_required_col",
		DorisType:  "VARCHAR(32)",
		Nullable:   false,
	}) // -> DORIS005
	orderViolations := LintDorisTableEvolution(orderBefore, orderAfter)
	wantOrder := []DorisLintRule{
		DORIS001_IMMUTABLE_KEY,
		DORIS002_IMMUTABLE_SEQUENCE,
		DORIS003_IMMUTABLE_DISTRIBUTION,
		DORIS004_NO_COLUMN_DROP,
		DORIS005_NON_NULL_SCALAR_DEFAULT,
		DORIS006_SAFE_TYPE_EVOLUTION,
	}
	if len(orderViolations) != len(wantOrder) {
		t.Fatalf("expected %d violations in rule order, got %d: %v", len(wantOrder), len(orderViolations), orderViolations)
	}
	for i, wantRule := range wantOrder {
		if orderViolations[i].Rule != wantRule {
			t.Errorf("orderViolations[%d].Rule = %s, want %s", i, orderViolations[i].Rule, wantRule)
		}
	}
}

// TestBuildDorisAlterTableSQL_AllDialects verifies deterministic ALTER TABLE DDL generation
// across Apache Doris 4.x, StarRocks 4.x, and Apache Iceberg v2 for safe evolutions and error
// rejection on unsafe evolutions or mismatched table identities.
func TestBuildDorisAlterTableSQL_AllDialects(t *testing.T) {
	before := baseLinterTestTable()

	// 1. Reflexive / no-op evolution returns empty slice and nil error.
	if stmts, err := before.BuildDorisAlterTableSQL(before, "acme"); err != nil || len(stmts) != 0 {
		t.Fatalf("expected (nil, nil) for reflexive Doris ALTER TABLE, got (%v, %v)", stmts, err)
	}
	if stmts, err := before.BuildStarRocksAlterTableSQL(before, "acme"); err != nil || len(stmts) != 0 {
		t.Fatalf("expected (nil, nil) for reflexive StarRocks ALTER TABLE, got (%v, %v)", stmts, err)
	}
	if stmts, err := before.BuildIcebergAlterTableSQL(before, "acme"); err != nil || len(stmts) != 0 {
		t.Fatalf("expected (nil, nil) for reflexive Iceberg ALTER TABLE, got (%v, %v)", stmts, err)
	}

	// 2. Safe widening + column additions:
	after := baseLinterTestTable()
	after.Columns[1].DorisType = "VARCHAR(128)" // criterion_id: VARCHAR(64) -> VARCHAR(128)
	after.Columns[2].DorisType = "INT"          // display_order: SMALLINT -> INT
	after.Columns = append(after.Columns,
		DorisColumnDescriptor{ColumnName: "optional_note", DorisType: "VARCHAR(256)", IcebergType: "STRING", Nullable: true},
		DorisColumnDescriptor{ColumnName: "record_status", DorisType: "VARCHAR(32)", IcebergType: "STRING", Nullable: false, DefaultValue: "ACTIVE"},
		DorisColumnDescriptor{ColumnName: "tags", DorisType: "ARRAY<VARCHAR(64)>", IcebergType: "LIST<STRING>", Nullable: false},
	)

	dorisStmts, err := before.BuildDorisAlterTableSQL(after, "acme")
	if err != nil {
		t.Fatalf("BuildDorisAlterTableSQL failed: %v", err)
	}
	wantDoris := []string{
		`ALTER TABLE internal.tenant_acme.eligibility_criterion_results MODIFY COLUMN criterion_id VARCHAR(128) NOT NULL;`,
		`ALTER TABLE internal.tenant_acme.eligibility_criterion_results MODIFY COLUMN display_order INT NOT NULL DEFAULT "0";`,
		`ALTER TABLE internal.tenant_acme.eligibility_criterion_results ADD COLUMN optional_note VARCHAR(256);`,
		`ALTER TABLE internal.tenant_acme.eligibility_criterion_results ADD COLUMN record_status VARCHAR(32) NOT NULL DEFAULT "ACTIVE";`,
		`ALTER TABLE internal.tenant_acme.eligibility_criterion_results ADD COLUMN tags ARRAY<VARCHAR(64)> NOT NULL DEFAULT "[]";`,
	}
	if !reflect.DeepEqual(dorisStmts, wantDoris) {
		t.Fatalf("BuildDorisAlterTableSQL mismatch:\ngot:  %#v\nwant: %#v", dorisStmts, wantDoris)
	}

	srStmts, err := before.BuildStarRocksAlterTableSQL(after, "acme")
	if err != nil {
		t.Fatalf("BuildStarRocksAlterTableSQL failed: %v", err)
	}
	if !reflect.DeepEqual(srStmts, wantDoris) {
		t.Fatalf("BuildStarRocksAlterTableSQL mismatch:\ngot:  %#v\nwant: %#v", srStmts, wantDoris)
	}

	// Iceberg rejects adding a NOT NULL scalar column (`record_status`) because Iceberg SQL
	// ALTER TABLE ADD COLUMN does not support initial DEFAULT values.
	if _, err := before.BuildIcebergAlterTableSQL(after, "acme"); err == nil {
		t.Fatal("expected BuildIcebergAlterTableSQL to reject adding NOT NULL scalar column without Iceberg default support")
	}

	iceAfter := baseLinterTestTable()
	iceAfter.Columns[1].DorisType = "VARCHAR(128)" // criterion_id: VARCHAR(64) -> VARCHAR(128)
	iceAfter.Columns[2].DorisType = "INT"          // display_order: SMALLINT -> INT
	iceAfter.Columns = append(iceAfter.Columns,
		DorisColumnDescriptor{ColumnName: "optional_note", DorisType: "VARCHAR(256)", IcebergType: "STRING", Nullable: true},
		DorisColumnDescriptor{ColumnName: "tags", DorisType: "ARRAY<VARCHAR(64)>", IcebergType: "LIST<STRING>", Nullable: false},
	)
	iceStmts, err := before.BuildIcebergAlterTableSQL(iceAfter, "acme")
	if err != nil {
		t.Fatalf("BuildIcebergAlterTableSQL failed: %v", err)
	}
	wantIce := []string{
		`ALTER TABLE iceberg.tenant_acme.eligibility_criterion_results MODIFY COLUMN criterion_id VARCHAR(128) NOT NULL;`,
		`ALTER TABLE iceberg.tenant_acme.eligibility_criterion_results MODIFY COLUMN display_order INT NOT NULL;`,
		`ALTER TABLE iceberg.tenant_acme.eligibility_criterion_results ADD COLUMN optional_note VARCHAR(256);`,
		`ALTER TABLE iceberg.tenant_acme.eligibility_criterion_results ADD COLUMN tags ARRAY<VARCHAR(64)> NOT NULL;`,
	}
	if !reflect.DeepEqual(iceStmts, wantIce) {
		t.Fatalf("BuildIcebergAlterTableSQL mismatch:\ngot:  %#v\nwant: %#v", iceStmts, wantIce)
	}

	// 2b. Nullability change on an existing column emits MODIFY COLUMN across all dialects:
	nullableAfter := baseLinterTestTable()
	nullableAfter.Columns[3].Nullable = false // score: Nullable=true -> Nullable=false
	nullableAfter.Columns[3].DefaultValue = "0.0"
	if stmts, err := before.BuildDorisAlterTableSQL(nullableAfter, "acme"); err != nil || len(stmts) != 1 || stmts[0] != `ALTER TABLE internal.tenant_acme.eligibility_criterion_results MODIFY COLUMN score FLOAT NOT NULL DEFAULT "0.0";` {
		t.Fatalf("expected Doris MODIFY COLUMN for nullability change, got (%v, %v)", stmts, err)
	}
	if stmts, err := before.BuildStarRocksAlterTableSQL(nullableAfter, "acme"); err != nil || len(stmts) != 1 || stmts[0] != `ALTER TABLE internal.tenant_acme.eligibility_criterion_results MODIFY COLUMN score FLOAT NOT NULL DEFAULT "0.0";` {
		t.Fatalf("expected StarRocks MODIFY COLUMN for nullability change, got (%v, %v)", stmts, err)
	}
	if stmts, err := before.BuildIcebergAlterTableSQL(nullableAfter, "acme"); err != nil || len(stmts) != 1 || stmts[0] != `ALTER TABLE iceberg.tenant_acme.eligibility_criterion_results MODIFY COLUMN score FLOAT NOT NULL;` {
		t.Fatalf("expected Iceberg MODIFY COLUMN for nullability change, got (%v, %v)", stmts, err)
	}

	// 3. Engine-specific skip tiers (ICEBERG_ONLY vs INTERNAL_ONLY) when both before and after match:
	iceOnlyBefore := baseLinterTestTable()
	iceOnlyBefore.SyncTier = "ICEBERG_ONLY"
	iceOnlyAfter := after
	iceOnlyAfter.SyncTier = "ICEBERG_ONLY"
	if stmts, err := iceOnlyBefore.BuildDorisAlterTableSQL(iceOnlyAfter, "acme"); err != nil || len(stmts) != 0 {
		t.Fatalf("expected ICEBERG_ONLY table to return empty Doris ALTER TABLE, got (%v, %v)", stmts, err)
	}
	if stmts, err := iceOnlyBefore.BuildStarRocksAlterTableSQL(iceOnlyAfter, "acme"); err != nil || len(stmts) != 0 {
		t.Fatalf("expected ICEBERG_ONLY table to return empty StarRocks ALTER TABLE, got (%v, %v)", stmts, err)
	}

	internalBefore := baseLinterTestTable()
	internalBefore.SyncTier = "INTERNAL_ONLY"
	internalAfter := after
	internalAfter.SyncTier = "INTERNAL_ONLY"
	if stmts, err := internalBefore.BuildIcebergAlterTableSQL(internalAfter, "acme"); err != nil || len(stmts) != 0 {
		t.Fatalf("expected INTERNAL_ONLY table to return empty Iceberg ALTER TABLE, got (%v, %v)", stmts, err)
	}

	// 3b. Cross-tier target transitions (ICEBERG_ONLY <-> MONTHLY_PARTITIONED or INTERNAL_ONLY <-> MONTHLY_PARTITIONED) must error:
	if _, err := iceOnlyBefore.BuildDorisAlterTableSQL(after, "acme"); err == nil {
		t.Fatal("expected BuildDorisAlterTableSQL to reject ICEBERG_ONLY -> MONTHLY_PARTITIONED transition")
	}
	if _, err := iceOnlyBefore.BuildStarRocksAlterTableSQL(after, "acme"); err == nil {
		t.Fatal("expected BuildStarRocksAlterTableSQL to reject ICEBERG_ONLY -> MONTHLY_PARTITIONED transition")
	}
	if _, err := internalBefore.BuildIcebergAlterTableSQL(iceAfter, "acme"); err == nil {
		t.Fatal("expected BuildIcebergAlterTableSQL to reject INTERNAL_ONLY -> MONTHLY_PARTITIONED transition")
	}

	// 4. Reject unsafe evolutions, partition mutations, empty column types, mismatched table identities, and invalid tenant slugs across all dialects:
	unsafeAfter := baseLinterTestTable()
	unsafeAfter.UniqueKeyColumns = []string{"patient_id"}
	if _, err := before.BuildDorisAlterTableSQL(unsafeAfter, "acme"); err == nil {
		t.Fatal("expected BuildDorisAlterTableSQL to return error on DORIS001 violation")
	}
	if _, err := before.BuildStarRocksAlterTableSQL(unsafeAfter, "acme"); err == nil {
		t.Fatal("expected BuildStarRocksAlterTableSQL to return error on DORIS001 violation")
	}
	if _, err := before.BuildIcebergAlterTableSQL(unsafeAfter, "acme"); err == nil {
		t.Fatal("expected BuildIcebergAlterTableSQL to return error on DORIS001 violation")
	}

	partMutated := baseLinterTestTable()
	partMutated.PartitionByColumns = []string{"patient_id"}
	if _, err := before.BuildDorisAlterTableSQL(partMutated, "acme"); err == nil {
		t.Fatal("expected BuildDorisAlterTableSQL to reject partition_by_columns mutation")
	}
	if _, err := before.BuildStarRocksAlterTableSQL(partMutated, "acme"); err == nil {
		t.Fatal("expected BuildStarRocksAlterTableSQL to reject partition_by_columns mutation")
	}
	if _, err := before.BuildIcebergAlterTableSQL(partMutated, "acme"); err == nil {
		t.Fatal("expected BuildIcebergAlterTableSQL to reject partition_by_columns mutation")
	}

	emptyTypeAfter := baseLinterTestTable()
	emptyTypeAfter.Columns = append(emptyTypeAfter.Columns, DorisColumnDescriptor{ColumnName: "untyped_col", Nullable: true})
	if _, err := before.BuildDorisAlterTableSQL(emptyTypeAfter, "acme"); err == nil {
		t.Fatal("expected BuildDorisAlterTableSQL to reject column with empty DorisType and IcebergType")
	}
	if _, err := before.BuildStarRocksAlterTableSQL(emptyTypeAfter, "acme"); err == nil {
		t.Fatal("expected BuildStarRocksAlterTableSQL to reject column with empty DorisType and IcebergType")
	}
	if _, err := before.BuildIcebergAlterTableSQL(emptyTypeAfter, "acme"); err == nil {
		t.Fatal("expected BuildIcebergAlterTableSQL to reject column with empty DorisType and IcebergType")
	}

	otherTable := iceAfter
	otherTable.TableName = "other_table"
	if _, err := before.BuildDorisAlterTableSQL(otherTable, "acme"); err == nil {
		t.Fatal("expected BuildDorisAlterTableSQL to reject mismatched table identity")
	}
	if _, err := before.BuildStarRocksAlterTableSQL(otherTable, "acme"); err == nil {
		t.Fatal("expected BuildStarRocksAlterTableSQL to reject mismatched table identity")
	}
	if _, err := before.BuildIcebergAlterTableSQL(otherTable, "acme"); err == nil {
		t.Fatal("expected BuildIcebergAlterTableSQL to reject mismatched table identity")
	}

	if _, err := before.BuildDorisAlterTableSQL(after, "bad;slug"); err == nil {
		t.Fatal("expected BuildDorisAlterTableSQL to reject invalid tenantSlug")
	}
	if _, err := before.BuildStarRocksAlterTableSQL(after, "bad;slug"); err == nil {
		t.Fatal("expected BuildStarRocksAlterTableSQL to reject invalid tenantSlug")
	}
	if _, err := before.BuildIcebergAlterTableSQL(iceAfter, "bad;slug"); err == nil {
		t.Fatal("expected BuildIcebergAlterTableSQL to reject invalid tenantSlug")
	}
}

// TestBuildDorisSchemaMigrationSQL_AllDialects verifies slice-level multi-table schema
// migration SQL generation (combining ALTER TABLE for evolved tables and CREATE TABLE
// for newly added tables, while rejecting dropped tables, duplicates, or unsafe evolutions).
func TestBuildDorisSchemaMigrationSQL_AllDialects(t *testing.T) {
	t1Before := baseLinterTestTable()
	t2New := DorisTableDescriptor{
		TableName:           "patient_cohorts",
		MessageName:         "PatientCohort",
		DatabaseScope:       "TENANT",
		SyncTier:            "WHOLE_TABLE",
		UniqueKeyColumns:    []string{"patient_id", "cohort_id"},
		DistributedByColumn: "patient_id",
		Columns: []DorisColumnDescriptor{
			{ColumnName: "patient_id", DorisType: "VARCHAR(64)", IcebergType: "STRING", Nullable: false, IsUniqueKey: true},
			{ColumnName: "cohort_id", DorisType: "VARCHAR(64)", IcebergType: "STRING", Nullable: false, IsUniqueKey: true},
		},
	}

	// 1. Reflexive multi-table slice returns empty statements and nil error.
	if stmts, err := BuildDorisSchemaMigrationSQL([]DorisTableDescriptor{t1Before, t2New}, []DorisTableDescriptor{t1Before, t2New}, "acme", 8); err != nil || len(stmts) != 0 {
		t.Fatalf("expected (nil, nil) for reflexive Doris schema migration, got (%v, %v)", stmts, err)
	}
	if stmts, err := BuildStarRocksSchemaMigrationSQL([]DorisTableDescriptor{t1Before, t2New}, []DorisTableDescriptor{t1Before, t2New}, "acme", 8); err != nil || len(stmts) != 0 {
		t.Fatalf("expected (nil, nil) for reflexive StarRocks schema migration, got (%v, %v)", stmts, err)
	}
	if stmts, err := BuildIcebergSchemaMigrationSQL([]DorisTableDescriptor{t1Before, t2New}, []DorisTableDescriptor{t1Before, t2New}, "acme"); err != nil || len(stmts) != 0 {
		t.Fatalf("expected (nil, nil) for reflexive Iceberg schema migration, got (%v, %v)", stmts, err)
	}

	// 2. Evolved t1 + newly added t2 emits ALTER TABLE for t1 and CREATE TABLE for t2:
	t1After := baseLinterTestTable()
	t1After.Columns[1].DorisType = "VARCHAR(128)" // criterion_id widened
	t1After.Columns = append(t1After.Columns, DorisColumnDescriptor{
		ColumnName:  "optional_note",
		DorisType:   "VARCHAR(256)",
		IcebergType: "STRING",
		Nullable:    true,
	})

	dorisMig, err := BuildDorisSchemaMigrationSQL([]DorisTableDescriptor{t1Before}, []DorisTableDescriptor{t1After, t2New}, "acme", 8)
	if err != nil {
		t.Fatalf("BuildDorisSchemaMigrationSQL failed: %v", err)
	}
	if len(dorisMig) != 3 ||
		dorisMig[0] != "ALTER TABLE internal.tenant_acme.eligibility_criterion_results MODIFY COLUMN criterion_id VARCHAR(128) NOT NULL;" ||
		dorisMig[1] != "ALTER TABLE internal.tenant_acme.eligibility_criterion_results ADD COLUMN optional_note VARCHAR(256);" ||
		!strings.HasPrefix(dorisMig[2], "CREATE TABLE IF NOT EXISTS internal.tenant_acme.patient_cohorts (") {
		t.Fatalf("unexpected Doris migration statements: %#v", dorisMig)
	}

	srMig, err := BuildStarRocksSchemaMigrationSQL([]DorisTableDescriptor{t1Before}, []DorisTableDescriptor{t1After, t2New}, "acme", 8)
	if err != nil {
		t.Fatalf("BuildStarRocksSchemaMigrationSQL failed: %v", err)
	}
	if len(srMig) != 3 ||
		srMig[0] != "ALTER TABLE internal.tenant_acme.eligibility_criterion_results MODIFY COLUMN criterion_id VARCHAR(128) NOT NULL;" ||
		srMig[1] != "ALTER TABLE internal.tenant_acme.eligibility_criterion_results ADD COLUMN optional_note VARCHAR(256);" ||
		!strings.HasPrefix(srMig[2], "CREATE TABLE IF NOT EXISTS internal.tenant_acme.patient_cohorts (") {
		t.Fatalf("unexpected StarRocks migration statements: %#v", srMig)
	}

	iceMig, err := BuildIcebergSchemaMigrationSQL([]DorisTableDescriptor{t1Before}, []DorisTableDescriptor{t1After, t2New}, "acme")
	if err != nil {
		t.Fatalf("BuildIcebergSchemaMigrationSQL failed: %v", err)
	}
	if len(iceMig) != 3 ||
		iceMig[0] != "ALTER TABLE iceberg.tenant_acme.eligibility_criterion_results MODIFY COLUMN criterion_id VARCHAR(128) NOT NULL;" ||
		iceMig[1] != "ALTER TABLE iceberg.tenant_acme.eligibility_criterion_results ADD COLUMN optional_note VARCHAR(256);" ||
		!strings.HasPrefix(iceMig[2], "CREATE TABLE IF NOT EXISTS iceberg.tenant_acme.patient_cohorts (") {
		t.Fatalf("unexpected Iceberg migration statements: %#v", iceMig)
	}

	// 3. Reject dropping an existing table across all dialects:
	if _, err := BuildDorisSchemaMigrationSQL([]DorisTableDescriptor{t1Before, t2New}, []DorisTableDescriptor{t1Before}, "acme", 8); err == nil {
		t.Fatal("expected BuildDorisSchemaMigrationSQL to reject dropping existing table")
	}
	if _, err := BuildStarRocksSchemaMigrationSQL([]DorisTableDescriptor{t1Before, t2New}, []DorisTableDescriptor{t1Before}, "acme", 8); err == nil {
		t.Fatal("expected BuildStarRocksSchemaMigrationSQL to reject dropping existing table")
	}
	if _, err := BuildIcebergSchemaMigrationSQL([]DorisTableDescriptor{t1Before, t2New}, []DorisTableDescriptor{t1Before}, "acme"); err == nil {
		t.Fatal("expected BuildIcebergSchemaMigrationSQL to reject dropping existing table")
	}

	// 4. Reject duplicate table identities in before or after:
	if _, err := BuildDorisSchemaMigrationSQL([]DorisTableDescriptor{t1Before, t1Before}, []DorisTableDescriptor{t1Before}, "acme", 8); err == nil {
		t.Fatal("expected BuildDorisSchemaMigrationSQL to reject duplicate table identities")
	}
	if _, err := BuildStarRocksSchemaMigrationSQL([]DorisTableDescriptor{t1Before}, []DorisTableDescriptor{t1Before, t1Before}, "acme", 8); err == nil {
		t.Fatal("expected BuildStarRocksSchemaMigrationSQL to reject duplicate table identities")
	}
	if _, err := BuildIcebergSchemaMigrationSQL([]DorisTableDescriptor{t1Before}, []DorisTableDescriptor{t1Before, t1Before}, "acme"); err == nil {
		t.Fatal("expected BuildIcebergSchemaMigrationSQL to reject duplicate table identities")
	}

	// 5. Same-name Doris/StarRocks target tables differing only by IcebergCatalog are rejected
	// by Doris and StarRocks migration planners, while Iceberg accepts them across catalogs:
	catA := t2New
	catA.IcebergCatalog = "lake_a"
	catB := t2New
	catB.IcebergCatalog = "lake_b"
	if _, err := BuildDorisSchemaMigrationSQL(nil, []DorisTableDescriptor{catA, catB}, "acme", 8); err == nil {
		t.Fatal("expected BuildDorisSchemaMigrationSQL to reject same-name Doris tables across different catalogs")
	}
	if _, err := BuildStarRocksSchemaMigrationSQL(nil, []DorisTableDescriptor{catA, catB}, "acme", 8); err == nil {
		t.Fatal("expected BuildStarRocksSchemaMigrationSQL to reject same-name StarRocks tables across different catalogs")
	}
	crossCatIce, err := BuildIcebergSchemaMigrationSQL(nil, []DorisTableDescriptor{catA, catB}, "acme")
	if err != nil {
		t.Fatalf("expected BuildIcebergSchemaMigrationSQL to accept cross-catalog Iceberg tables, got %v", err)
	}
	if len(crossCatIce) != 2 ||
		!strings.HasPrefix(crossCatIce[0], "CREATE TABLE IF NOT EXISTS lake_a.tenant_acme.patient_cohorts (") ||
		!strings.HasPrefix(crossCatIce[1], "CREATE TABLE IF NOT EXISTS lake_b.tenant_acme.patient_cohorts (") {
		t.Fatalf("unexpected cross-catalog Iceberg CREATE statements: %#v", crossCatIce)
	}

	// 6. Reject new target descriptor with zero columns:
	emptyColsTbl := DorisTableDescriptor{
		TableName:     "empty_table",
		DatabaseScope: "TENANT",
		SyncTier:      "WHOLE_TABLE",
	}
	if stmts, err := BuildDorisSchemaMigrationSQL(nil, []DorisTableDescriptor{emptyColsTbl}, "acme", 8); err == nil || len(stmts) != 0 {
		t.Fatalf("expected BuildDorisSchemaMigrationSQL to reject zero-column new table, got (%v, %v)", stmts, err)
	}
	if stmts, err := BuildStarRocksSchemaMigrationSQL(nil, []DorisTableDescriptor{emptyColsTbl}, "acme", 8); err == nil || len(stmts) != 0 {
		t.Fatalf("expected BuildStarRocksSchemaMigrationSQL to reject zero-column new table, got (%v, %v)", stmts, err)
	}
	if stmts, err := BuildIcebergSchemaMigrationSQL(nil, []DorisTableDescriptor{emptyColsTbl}, "acme"); err == nil || len(stmts) != 0 {
		t.Fatalf("expected BuildIcebergSchemaMigrationSQL to reject zero-column new table, got (%v, %v)", stmts, err)
	}

	// 7. Report ambiguous catalog omission separately from forbidden drops when an
	// ICEBERG_ONLY sibling shares (DatabaseScope, TableName) with a Doris target:
	iceOnlySibling := catB
	iceOnlySibling.SyncTier = "ICEBERG_ONLY"
	omittedCatAfter := t2New
	omittedCatAfter.IcebergCatalog = ""
	if _, err := BuildDorisSchemaMigrationSQL(
		[]DorisTableDescriptor{catA, iceOnlySibling},
		[]DorisTableDescriptor{omittedCatAfter, iceOnlySibling},
		"acme",
		8,
	); err == nil || !strings.Contains(err.Error(), "ambiguous IcebergCatalog omission") {
		t.Fatalf("expected ambiguous IcebergCatalog omission error from BuildDorisSchemaMigrationSQL, got %v", err)
	}
	if _, err := BuildStarRocksSchemaMigrationSQL(
		[]DorisTableDescriptor{catA, iceOnlySibling},
		[]DorisTableDescriptor{omittedCatAfter, iceOnlySibling},
		"acme",
		8,
	); err == nil || !strings.Contains(err.Error(), "ambiguous IcebergCatalog omission") {
		t.Fatalf("expected ambiguous IcebergCatalog omission error from BuildStarRocksSchemaMigrationSQL, got %v", err)
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
