package generator

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// DorisLintRule identifies a compile-time Apache Doris / Apache Iceberg
// schema evolution safety rule.
type DorisLintRule string

const (
	// DORIS001_IMMUTABLE_KEY blocks mutations to UniqueKeyColumns or DuplicateKeyColumns on an existing table.
	DORIS001_IMMUTABLE_KEY DorisLintRule = "DORIS001_IMMUTABLE_KEY"
	// DORIS002_IMMUTABLE_SEQUENCE blocks mutations to SequenceCol on an existing table.
	DORIS002_IMMUTABLE_SEQUENCE DorisLintRule = "DORIS002_IMMUTABLE_SEQUENCE"
	// DORIS003_IMMUTABLE_DISTRIBUTION blocks mutations to DistributedByColumn or ColocateGroup on an existing table.
	DORIS003_IMMUTABLE_DISTRIBUTION DorisLintRule = "DORIS003_IMMUTABLE_DISTRIBUTION"
	// DORIS004_NO_COLUMN_DROP blocks dropping existing columns from an existing table.
	DORIS004_NO_COLUMN_DROP DorisLintRule = "DORIS004_NO_COLUMN_DROP"
	// DORIS005_NON_NULL_SCALAR_DEFAULT requires an explicit DefaultValue on newly added NOT NULL scalar columns.
	DORIS005_NON_NULL_SCALAR_DEFAULT DorisLintRule = "DORIS005_NON_NULL_SCALAR_DEFAULT"
	// DORIS006_SAFE_TYPE_EVOLUTION allows only safe widening type evolutions supported by both Doris 4.x LSC and Iceberg v2.
	DORIS006_SAFE_TYPE_EVOLUTION DorisLintRule = "DORIS006_SAFE_TYPE_EVOLUTION"

	DorisRuleImmutableKey          = DORIS001_IMMUTABLE_KEY
	DorisRuleImmutableSequence     = DORIS002_IMMUTABLE_SEQUENCE
	DorisRuleImmutableDistribution = DORIS003_IMMUTABLE_DISTRIBUTION
	DorisRuleNoColumnDrop          = DORIS004_NO_COLUMN_DROP
	DorisRuleNonNullScalarDefault  = DORIS005_NON_NULL_SCALAR_DEFAULT
	DorisRuleSafeTypeEvolution     = DORIS006_SAFE_TYPE_EVOLUTION
)

// DorisEvolutionViolation describes a single schema evolution rule violation
// detected between a before and after DorisTableDescriptor.
type DorisEvolutionViolation struct {
	Rule       DorisLintRule
	TableName  string
	ColumnName string
	Message    string
}

// String formats the violation as a deterministic human-readable diagnostic.
func (v DorisEvolutionViolation) String() string {
	if v.ColumnName != "" {
		return fmt.Sprintf("[%s] table %s column %s: %s", v.Rule, v.TableName, v.ColumnName, v.Message)
	}
	return fmt.Sprintf("[%s] table %s: %s", v.Rule, v.TableName, v.Message)
}

// Error implements the error interface.
func (v DorisEvolutionViolation) Error() string {
	return v.String()
}

// LintEvolution validates evolving this table descriptor (before) into after.
func (t DorisTableDescriptor) LintEvolution(after DorisTableDescriptor) []DorisEvolutionViolation {
	return LintDorisTableEvolution(t, after)
}

// LintDorisTableEvolution validates evolving an existing table descriptor from
// before to after and returns all detected violations in deterministic rule order
// (DORIS001 through DORIS006).
func LintDorisTableEvolution(before, after DorisTableDescriptor) []DorisEvolutionViolation {
	tableName := strings.TrimSpace(after.TableName)
	if tableName == "" {
		tableName = strings.TrimSpace(before.TableName)
	}

	var violations []DorisEvolutionViolation

	// DORIS001_IMMUTABLE_KEY: Block changes to UniqueKeyColumns or DuplicateKeyColumns.
	beforeUKs := effectiveDorisUniqueKeys(before)
	afterUKs := effectiveDorisUniqueKeys(after)
	if !dorisStringSlicesEqual(beforeUKs, afterUKs) {
		violations = append(violations, DorisEvolutionViolation{
			Rule:      DORIS001_IMMUTABLE_KEY,
			TableName: tableName,
			Message:   fmt.Sprintf("UNIQUE KEY columns cannot be mutated on existing table (before=%v, after=%v)", beforeUKs, afterUKs),
		})
	}
	beforeDKs := effectiveDorisDuplicateKeys(before)
	afterDKs := effectiveDorisDuplicateKeys(after)
	if !dorisStringSlicesEqual(beforeDKs, afterDKs) {
		violations = append(violations, DorisEvolutionViolation{
			Rule:      DORIS001_IMMUTABLE_KEY,
			TableName: tableName,
			Message:   fmt.Sprintf("DUPLICATE KEY columns cannot be mutated on existing table (before=%v, after=%v)", beforeDKs, afterDKs),
		})
	}

	// DORIS002_IMMUTABLE_SEQUENCE: Block changes to SequenceCol (e.g. publication_generation).
	beforeSeq := effectiveDorisSequenceCol(before)
	afterSeq := effectiveDorisSequenceCol(after)
	if beforeSeq != afterSeq {
		seqCol := afterSeq
		if seqCol == "" {
			seqCol = beforeSeq
		}
		violations = append(violations, DorisEvolutionViolation{
			Rule:       DORIS002_IMMUTABLE_SEQUENCE,
			TableName:  tableName,
			ColumnName: seqCol,
			Message:    fmt.Sprintf("sequence_col cannot be mutated on existing table (before=%q, after=%q)", beforeSeq, afterSeq),
		})
	}

	// DORIS003_IMMUTABLE_DISTRIBUTION: Block changes to DistributedByColumn or ColocateGroup.
	beforeDist := strings.TrimSpace(before.DistributedByColumn)
	afterDist := strings.TrimSpace(after.DistributedByColumn)
	if beforeDist != afterDist {
		distCol := afterDist
		if distCol == "" {
			distCol = beforeDist
		}
		violations = append(violations, DorisEvolutionViolation{
			Rule:       DORIS003_IMMUTABLE_DISTRIBUTION,
			TableName:  tableName,
			ColumnName: distCol,
			Message:    fmt.Sprintf("distributed_by_column cannot be mutated on existing table (before=%q, after=%q)", beforeDist, afterDist),
		})
	}
	beforeColocate := strings.TrimSpace(before.ColocateGroup)
	afterColocate := strings.TrimSpace(after.ColocateGroup)
	if beforeColocate != afterColocate {
		violations = append(violations, DorisEvolutionViolation{
			Rule:      DORIS003_IMMUTABLE_DISTRIBUTION,
			TableName: tableName,
			Message:   fmt.Sprintf("colocate_with group cannot be mutated on existing table (before=%q, after=%q)", beforeColocate, afterColocate),
		})
	}

	beforeColByName := make(map[string]DorisColumnDescriptor, len(before.Columns))
	for _, c := range before.Columns {
		beforeColByName[c.ColumnName] = c
	}
	afterColByName := make(map[string]DorisColumnDescriptor, len(after.Columns))
	for _, c := range after.Columns {
		afterColByName[c.ColumnName] = c
	}

	// DORIS004_NO_COLUMN_DROP: Block removing existing DorisColumnDescriptor entries.
	for _, bc := range before.Columns {
		if _, ok := afterColByName[bc.ColumnName]; !ok {
			violations = append(violations, DorisEvolutionViolation{
				Rule:       DORIS004_NO_COLUMN_DROP,
				TableName:  tableName,
				ColumnName: bc.ColumnName,
				Message:    fmt.Sprintf("dropping existing column %q is forbidden", bc.ColumnName),
			})
		}
	}

	// DORIS005_NON_NULL_SCALAR_DEFAULT & DORIS006_SAFE_TYPE_EVOLUTION
	for _, ac := range after.Columns {
		bc, existed := beforeColByName[ac.ColumnName]
		if !existed {
			// Newly added column: check DORIS005_NON_NULL_SCALAR_DEFAULT.
			if !ac.Nullable && !isDorisNonScalarColumnType(ac.DorisType) && strings.TrimSpace(ac.DefaultValue) == "" {
				violations = append(violations, DorisEvolutionViolation{
					Rule:       DORIS005_NON_NULL_SCALAR_DEFAULT,
					TableName:  tableName,
					ColumnName: ac.ColumnName,
					Message:    fmt.Sprintf("newly added NOT NULL scalar column %q (%s) requires an explicit DefaultValue", ac.ColumnName, ac.DorisType),
				})
			}
			continue
		}

		// Existing column: check DORIS006_SAFE_TYPE_EVOLUTION.
		beforeType := strings.TrimSpace(bc.DorisType)
		afterType := strings.TrimSpace(ac.DorisType)
		if beforeType == "" && afterType == "" {
			beforeType = strings.TrimSpace(bc.IcebergType)
			afterType = strings.TrimSpace(ac.IcebergType)
		}
		if !IsSafeDorisTypeEvolution(beforeType, afterType) {
			violations = append(violations, DorisEvolutionViolation{
				Rule:       DORIS006_SAFE_TYPE_EVOLUTION,
				TableName:  tableName,
				ColumnName: ac.ColumnName,
				Message:    fmt.Sprintf("unsafe type evolution on column %q from %q to %q", ac.ColumnName, beforeType, afterType),
			})
		}
	}

	return violations
}

// ValidateDorisTableEvolution returns an error joining all violations if evolving
// before into after violates any Doris schema evolution rule, or nil if safe.
func ValidateDorisTableEvolution(before, after DorisTableDescriptor) error {
	violations := LintDorisTableEvolution(before, after)
	if len(violations) == 0 {
		return nil
	}
	errs := make([]error, len(violations))
	for i, v := range violations {
		errs[i] = v
	}
	return errors.Join(errs...)
}

// LintDorisSchemaEvolution validates evolving a slice of before table descriptors
// into after table descriptors. Tables present in both slices (matched by TableName)
// are validated in deterministic order.
func LintDorisSchemaEvolution(before, after []DorisTableDescriptor) []DorisEvolutionViolation {
	beforeByName := make(map[string]DorisTableDescriptor, len(before))
	for _, t := range before {
		if t.TableName != "" {
			beforeByName[t.TableName] = t
		}
	}
	var violations []DorisEvolutionViolation
	seen := make(map[string]bool, len(after))
	for _, afterTbl := range after {
		if afterTbl.TableName == "" || seen[afterTbl.TableName] {
			continue
		}
		seen[afterTbl.TableName] = true
		if beforeTbl, ok := beforeByName[afterTbl.TableName]; ok {
			violations = append(violations, LintDorisTableEvolution(beforeTbl, afterTbl)...)
		}
	}
	return violations
}

// ValidateDorisSchemaEvolution returns an error joining all violations across
// the schema evolution from before to after, or nil if safe.
func ValidateDorisSchemaEvolution(before, after []DorisTableDescriptor) error {
	violations := LintDorisSchemaEvolution(before, after)
	if len(violations) == 0 {
		return nil
	}
	errs := make([]error, len(violations))
	for i, v := range violations {
		errs[i] = v
	}
	return errors.Join(errs...)
}

// IsSafeDorisTypeEvolution reports whether evolving a column from beforeType to
// afterType is either a no-op or a safe widening type change supported by both
// Apache Doris 4.x Light Schema Change (LSC) and Apache Iceberg v2:
//   - VARCHAR(m) -> VARCHAR(n) where n > m
//   - TINYINT -> SMALLINT, INT, or BIGINT
//   - SMALLINT -> INT or BIGINT
//   - INT -> BIGINT
//   - FLOAT -> DOUBLE
func IsSafeDorisTypeEvolution(beforeType, afterType string) bool {
	bt := strings.ToUpper(strings.TrimSpace(beforeType))
	at := strings.ToUpper(strings.TrimSpace(afterType))
	if bt == at {
		return true
	}
	if bt == "" || at == "" {
		return false
	}

	if m, okB := parseDorisVarcharLength(bt); okB {
		if n, okA := parseDorisVarcharLength(at); okA {
			return n > m
		}
		return false
	}

	switch bt {
	case "TINYINT":
		return at == "SMALLINT" || at == "INT" || at == "BIGINT"
	case "SMALLINT":
		return at == "INT" || at == "BIGINT"
	case "INT":
		return at == "BIGINT"
	case "FLOAT":
		return at == "DOUBLE"
	default:
		return false
	}
}

// parseDorisVarcharLength extracts the positive integer length N from "VARCHAR(N)".
func parseDorisVarcharLength(upperType string) (int, bool) {
	if !strings.HasPrefix(upperType, "VARCHAR(") || !strings.HasSuffix(upperType, ")") {
		return 0, false
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(upperType, "VARCHAR("), ")"))
	if inner == "" {
		return 0, false
	}
	n, err := strconv.Atoi(inner)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// isDorisNonScalarColumnType reports whether dorisType is a complex or semi-structured
// Doris column type (ARRAY, MAP, STRUCT, JSON, JSONB, VARIANT) exempt from scalar default rules.
func isDorisNonScalarColumnType(dorisType string) bool {
	dt := strings.ToUpper(strings.TrimSpace(dorisType))
	switch dt {
	case "JSON", "JSONB", "VARIANT":
		return true
	}
	return strings.HasPrefix(dt, "ARRAY<") ||
		strings.HasPrefix(dt, "MAP<") ||
		strings.HasPrefix(dt, "STRUCT<")
}

// effectiveDorisUniqueKeys resolves the deduplicated UNIQUE KEY column list for t.
func effectiveDorisUniqueKeys(t DorisTableDescriptor) []string {
	if len(t.UniqueKeyColumns) > 0 {
		return dedupDorisColumnList(t.UniqueKeyColumns)
	}
	var uks []string
	for _, c := range t.Columns {
		if c.IsUniqueKey {
			uks = append(uks, c.ColumnName)
		}
	}
	return dedupDorisColumnList(uks)
}

// effectiveDorisDuplicateKeys resolves the deduplicated DUPLICATE KEY column list for t.
func effectiveDorisDuplicateKeys(t DorisTableDescriptor) []string {
	return dedupDorisColumnList(t.DuplicateKeyColumns)
}

// effectiveDorisSequenceCol resolves the sequence column name for t.
func effectiveDorisSequenceCol(t DorisTableDescriptor) string {
	if s := strings.TrimSpace(t.SequenceCol); s != "" {
		return s
	}
	for _, c := range t.Columns {
		if c.IsSequenceCol {
			return c.ColumnName
		}
	}
	return ""
}

// dorisStringSlicesEqual reports whether a and b have identical elements in the same order.
func dorisStringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
