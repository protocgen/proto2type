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

	// DORIS003_IMMUTABLE_DISTRIBUTION: Block changes to DistributedByColumn/DistributionColumns or ColocateGroup.
	beforeDist := effectiveDorisDistributionCols(before)
	afterDist := effectiveDorisDistributionCols(after)
	if !dorisStringSlicesEqual(beforeDist, afterDist) {
		beforeDistStr := strings.Join(beforeDist, ", ")
		afterDistStr := strings.Join(afterDist, ", ")
		distCol := afterDistStr
		if distCol == "" {
			distCol = beforeDistStr
		}
		violations = append(violations, DorisEvolutionViolation{
			Rule:       DORIS003_IMMUTABLE_DISTRIBUTION,
			TableName:  tableName,
			ColumnName: distCol,
			Message:    fmt.Sprintf("distributed_by_column cannot be mutated on existing table (before=%q, after=%q)", beforeDistStr, afterDistStr),
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

	// DORIS005_NON_NULL_SCALAR_DEFAULT: Newly added NOT NULL scalar columns require an explicit DefaultValue.
	for _, ac := range after.Columns {
		if _, existed := beforeColByName[ac.ColumnName]; existed {
			continue
		}
		if !ac.Nullable && !isDorisNonScalarColumnType(ac.DorisType) && strings.TrimSpace(ac.DefaultValue) == "" {
			violations = append(violations, DorisEvolutionViolation{
				Rule:       DORIS005_NON_NULL_SCALAR_DEFAULT,
				TableName:  tableName,
				ColumnName: ac.ColumnName,
				Message:    fmt.Sprintf("newly added NOT NULL scalar column %q (%s) requires an explicit DefaultValue", ac.ColumnName, ac.DorisType),
			})
		}
	}

	// DORIS006_SAFE_TYPE_EVOLUTION: Existing columns may only undergo safe widening type evolutions.
	for _, ac := range after.Columns {
		bc, existed := beforeColByName[ac.ColumnName]
		if !existed {
			continue
		}
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

// dorisScopeTableIdentity computes the (DatabaseScope, TableName) identity of t.
func dorisScopeTableIdentity(t DorisTableDescriptor) string {
	tableName := strings.TrimSpace(t.TableName)
	if tableName == "" {
		return ""
	}
	scope := strings.TrimSpace(t.DatabaseScope)
	if scope == "" || scope == "UNSPECIFIED" {
		scope = "TENANT"
	}
	return scope + "\x00" + tableName
}

// dorisSameDorisPhysicalTable reports whether before and after refer to the same
// physical Doris/StarRocks table (matching DatabaseScope and TableName, and matching
// IcebergCatalog when explicitly set on both descriptors).
func dorisSameDorisPhysicalTable(before, after DorisTableDescriptor) bool {
	sBefore := dorisScopeTableIdentity(before)
	sAfter := dorisScopeTableIdentity(after)
	if sBefore == "" || sBefore != sAfter {
		return false
	}
	cBefore := strings.TrimSpace(before.IcebergCatalog)
	cAfter := strings.TrimSpace(after.IcebergCatalog)
	if cBefore != "" && cAfter != "" && cBefore != cAfter {
		return false
	}
	return true
}

// dorisTableEvolutionIdentity computes the canonical physical table identity
// (DatabaseScope, IcebergCatalog, TableName) used to pair before and after
// table descriptors in LintDorisSchemaEvolution.
func dorisTableEvolutionIdentity(t DorisTableDescriptor) string {
	tableName := strings.TrimSpace(t.TableName)
	if tableName == "" {
		return ""
	}
	scope := strings.TrimSpace(t.DatabaseScope)
	if scope == "" || scope == "UNSPECIFIED" {
		scope = "TENANT"
	}
	catalog := strings.TrimSpace(t.IcebergCatalog)
	if catalog == "" {
		catalog = "iceberg"
	}
	return scope + "\x00" + catalog + "\x00" + tableName
}

func matchBeforeDorisTable(
	afterTbl DorisTableDescriptor,
	beforeByKey map[string]DorisTableDescriptor,
	beforeByScope map[string]DorisTableDescriptor,
	beforeScopeCount map[string]int,
	afterScopeCount map[string]int,
) (DorisTableDescriptor, string, bool) {
	key := dorisTableEvolutionIdentity(afterTbl)
	if beforeTbl, ok := beforeByKey[key]; ok {
		return beforeTbl, key, true
	}
	scopeKey := dorisScopeTableIdentity(afterTbl)
	if scopeKey != "" && beforeScopeCount[scopeKey] == 1 && afterScopeCount[scopeKey] == 1 {
		if beforeTbl, ok := beforeByScope[scopeKey]; ok && dorisSameDorisPhysicalTable(beforeTbl, afterTbl) {
			return beforeTbl, dorisTableEvolutionIdentity(beforeTbl), true
		}
	}
	return DorisTableDescriptor{}, "", false
}

// LintDorisSchemaEvolution validates evolving a slice of before table descriptors
// into after table descriptors. Tables present in both slices (matched by physical
// table identity: DatabaseScope, IcebergCatalog, and TableName) are validated in
// deterministic order.
func LintDorisSchemaEvolution(before, after []DorisTableDescriptor) []DorisEvolutionViolation {
	beforeByKey := make(map[string]DorisTableDescriptor, len(before))
	beforeByScope := make(map[string]DorisTableDescriptor, len(before))
	beforeScopeCount := make(map[string]int, len(before))
	for _, t := range before {
		if key := dorisTableEvolutionIdentity(t); key != "" {
			beforeByKey[key] = t
		}
		if scopeKey := dorisScopeTableIdentity(t); scopeKey != "" {
			beforeByScope[scopeKey] = t
			beforeScopeCount[scopeKey]++
		}
	}
	afterScopeCount := make(map[string]int, len(after))
	for _, t := range after {
		if scopeKey := dorisScopeTableIdentity(t); scopeKey != "" {
			afterScopeCount[scopeKey]++
		}
	}
	var violations []DorisEvolutionViolation
	seen := make(map[string]bool, len(after))
	for _, afterTbl := range after {
		key := dorisTableEvolutionIdentity(afterTbl)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if beforeTbl, _, ok := matchBeforeDorisTable(afterTbl, beforeByKey, beforeByScope, beforeScopeCount, afterScopeCount); ok {
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

// LintDorisEvolution is an alias for LintDorisSchemaEvolution that validates
// evolving a slice of before table descriptors into after table descriptors.
func LintDorisEvolution(before, after []DorisTableDescriptor) []DorisEvolutionViolation {
	return LintDorisSchemaEvolution(before, after)
}

// ValidateDorisEvolution is an alias for ValidateDorisSchemaEvolution that returns
// an error joining all schema evolution violations, or nil if safe.
func ValidateDorisEvolution(before, after []DorisTableDescriptor) error {
	return ValidateDorisSchemaEvolution(before, after)
}

// IsSafeDorisTypeEvolution reports whether evolving a column from beforeType to
// afterType is either a no-op (including Doris <-> StarRocks type aliases such as
// DATEV2 <-> DATE, DECIMALV3(p,s) <-> DECIMAL(p,s), and TEXT <-> STRING) or a
// safe widening type change supported by Apache Doris 4.x Light Schema Change (LSC),
// StarRocks 4.x, and Apache Iceberg v2:
//   - VARCHAR(m) -> VARCHAR(n) (where n >= m), STRING, or TEXT
//   - DATE / DATEV2 -> DATETIME / DATETIMEV2(p)
//   - DATETIME(p1) / DATETIMEV2(p1) -> DATETIME(p2) / DATETIMEV2(p2) where p2 >= p1 (bare DATETIME/DATETIMEV2 has precision 0)
//   - DECIMAL(p1, s) / DECIMALV3(p1, s) -> DECIMAL(p2, s) / DECIMALV3(p2, s) where p2 >= p1
//   - ARRAY<T1> -> ARRAY<T2> where T1 -> T2 is a safe type evolution
//   - TINYINT -> SMALLINT, INT, BIGINT, or LARGEINT
//   - SMALLINT -> INT, BIGINT, or LARGEINT
//   - INT -> BIGINT or LARGEINT
//   - BIGINT -> LARGEINT
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

	if strings.HasPrefix(bt, "ARRAY<") && strings.HasSuffix(bt, ">") {
		if strings.HasPrefix(at, "ARRAY<") && strings.HasSuffix(at, ">") {
			innerB := strings.TrimSuffix(strings.TrimPrefix(bt, "ARRAY<"), ">")
			innerA := strings.TrimSuffix(strings.TrimPrefix(at, "ARRAY<"), ">")
			return IsSafeDorisTypeEvolution(innerB, innerA)
		}
		return false
	}

	if p1, okB := parseDorisDatetimePrecision(bt); okB {
		if p2, okA := parseDorisDatetimePrecision(at); okA {
			return p2 >= p1
		}
		return false
	}

	canonBefore := dorisColumnStarRocksDDLType(bt, "")
	canonAfter := dorisColumnStarRocksDDLType(at, "")
	if canonBefore == canonAfter {
		return true
	}

	if m, okB := parseDorisVarcharLength(canonBefore); okB {
		if n, okA := parseDorisVarcharLength(canonAfter); okA {
			return n >= m
		}
		return canonAfter == "STRING"
	}

	if p1, s1, okB := parseDorisDecimalParams(canonBefore); okB {
		if p2, s2, okA := parseDorisDecimalParams(canonAfter); okA {
			return p2 >= p1 && s1 == s2
		}
		return false
	}

	switch canonBefore {
	case "TINYINT":
		return canonAfter == "SMALLINT" || canonAfter == "INT" || canonAfter == "BIGINT" || canonAfter == "LARGEINT"
	case "SMALLINT":
		return canonAfter == "INT" || canonAfter == "BIGINT" || canonAfter == "LARGEINT"
	case "INT":
		return canonAfter == "BIGINT" || canonAfter == "LARGEINT"
	case "BIGINT":
		return canonAfter == "LARGEINT"
	case "FLOAT":
		return canonAfter == "DOUBLE"
	case "DATE":
		_, okA := parseDorisDatetimePrecision(at)
		return okA
	default:
		return false
	}
}

// parseDorisDatetimePrecision extracts precision P (0 <= P <= 6) from "DATETIME", "DATETIMEV2",
// "DATETIME(P)", or "DATETIMEV2(P)". Bare "DATETIME" or "DATETIMEV2" has precision 0.
func parseDorisDatetimePrecision(upperType string) (int, bool) {
	switch upperType {
	case "DATETIME", "DATETIMEV2":
		return 0, true
	}
	var inner string
	switch {
	case strings.HasPrefix(upperType, "DATETIME(") && strings.HasSuffix(upperType, ")"):
		inner = strings.TrimSuffix(strings.TrimPrefix(upperType, "DATETIME("), ")")
	case strings.HasPrefix(upperType, "DATETIMEV2(") && strings.HasSuffix(upperType, ")"):
		inner = strings.TrimSuffix(strings.TrimPrefix(upperType, "DATETIMEV2("), ")")
	default:
		return 0, false
	}
	p, err := strconv.Atoi(strings.TrimSpace(inner))
	if err != nil || p < 0 || p > 6 {
		return 0, false
	}
	return p, true
}

// parseDorisDecimalParams extracts precision P and scale S from "DECIMAL(P, S)" or "DECIMALV3(P, S)".
func parseDorisDecimalParams(upperType string) (int, int, bool) {
	var inner string
	switch {
	case strings.HasPrefix(upperType, "DECIMAL(") && strings.HasSuffix(upperType, ")"):
		inner = strings.TrimSuffix(strings.TrimPrefix(upperType, "DECIMAL("), ")")
	case strings.HasPrefix(upperType, "DECIMALV3(") && strings.HasSuffix(upperType, ")"):
		inner = strings.TrimSuffix(strings.TrimPrefix(upperType, "DECIMALV3("), ")")
	default:
		return 0, 0, false
	}
	parts := strings.Split(inner, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	p, errP := strconv.Atoi(strings.TrimSpace(parts[0]))
	s, errS := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errP != nil || errS != nil || p <= 0 || s < 0 || s > p {
		return 0, 0, false
	}
	return p, s, true
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
	if len(t.DuplicateKeyColumns) > 0 {
		return dedupDorisColumnList(t.DuplicateKeyColumns)
	}
	if len(effectiveDorisUniqueKeys(t)) == 0 && len(t.Columns) > 0 && t.Columns[0].ColumnName != "" {
		return []string{t.Columns[0].ColumnName}
	}
	return nil
}

// effectiveDorisDistributionCols resolves the hash distribution column list for t.
func effectiveDorisDistributionCols(t DorisTableDescriptor) []string {
	if len(t.DistributionColumns) > 0 {
		return dedupDorisColumnList(t.DistributionColumns)
	}
	if s := strings.TrimSpace(t.DistributedByColumn); s != "" {
		return []string{s}
	}
	if uks := effectiveDorisUniqueKeys(t); len(uks) > 0 {
		if len(uks) > 1 && uks[0] == "tenant_id" {
			return []string{uks[1]}
		}
		return []string{uks[0]}
	}
	if dks := effectiveDorisDuplicateKeys(t); len(dks) > 0 {
		return []string{dks[0]}
	}
	return nil
}

// effectiveDorisSequenceCol resolves the sequence column name for t.
func effectiveDorisSequenceCol(t DorisTableDescriptor) string {
	if s := strings.TrimSpace(t.SequenceCol); s != "" {
		return s
	}
	if s := strings.TrimSpace(t.SequenceColumn); s != "" {
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

// hasDorisTargetTable reports whether t materializes a physical table in
// Apache Doris or StarRocks (as opposed to living exclusively in Iceberg).
func hasDorisTargetTable(t DorisTableDescriptor) bool {
	if t.SyncTier == "ICEBERG_ONLY" {
		return false
	}
	uks := effectiveDorisUniqueKeys(t)
	dks := dedupDorisColumnList(t.DuplicateKeyColumns)
	if t.DatabaseScope == "GLOBAL_TERMINOLOGY" && t.SyncTier == "RELEASE_PARTITIONED" && len(uks) == 0 && len(dks) == 0 {
		return false
	}
	return true
}

// hasIcebergTargetTable reports whether t materializes a physical table in
// the Apache Iceberg catalog (as opposed to INTERNAL_ONLY tables).
func hasIcebergTargetTable(t DorisTableDescriptor) bool {
	return t.SyncTier != "INTERNAL_ONLY"
}

// effectiveDorisPartitionCols resolves the deduplicated partition column list for t.
func effectiveDorisPartitionCols(t DorisTableDescriptor) []string {
	if len(t.PartitionByColumns) > 0 {
		return dedupDorisColumnList(t.PartitionByColumns)
	}
	var cols []string
	for _, c := range t.Columns {
		if c.IsPartitionKey {
			cols = append(cols, c.ColumnName)
		}
	}
	cols = dedupDorisColumnList(cols)
	if len(cols) == 0 && t.SyncTier == "MONTHLY_PARTITIONED" {
		for _, c := range t.Columns {
			if c.ColumnName == "event_month" {
				return []string{"event_month"}
			}
		}
	}
	if len(cols) == 0 && t.SyncTier == "RELEASE_PARTITIONED" {
		for _, c := range t.Columns {
			if c.ColumnName == "release_id" {
				return []string{"release_id"}
			}
		}
	}
	return cols
}

// BuildDorisAlterTableSQL validates evolving this table descriptor (before) into after
// and renders deterministic Apache Doris 4.x ALTER TABLE statements (MODIFY COLUMN for
// safe type widenings followed by ADD COLUMN for newly added columns).
func (t DorisTableDescriptor) BuildDorisAlterTableSQL(after DorisTableDescriptor, tenantSlug string) ([]string, error) {
	return BuildDorisAlterTableSQL(t, after, tenantSlug)
}

// BuildStarRocksAlterTableSQL validates evolving this table descriptor (before) into after
// and renders deterministic StarRocks 4.x ALTER TABLE statements.
func (t DorisTableDescriptor) BuildStarRocksAlterTableSQL(after DorisTableDescriptor, tenantSlug string) ([]string, error) {
	return BuildStarRocksAlterTableSQL(t, after, tenantSlug)
}

// BuildIcebergAlterTableSQL validates evolving this table descriptor (before) into after
// and renders deterministic Apache Iceberg ALTER TABLE statements.
func (t DorisTableDescriptor) BuildIcebergAlterTableSQL(after DorisTableDescriptor, tenantSlug string) ([]string, error) {
	return BuildIcebergAlterTableSQL(t, after, tenantSlug)
}

// BuildDorisAlterTableSQL validates evolving before into after and returns the ordered
// Apache Doris 4.x ALTER TABLE statements (MODIFY COLUMN for widened columns, then
// ADD COLUMN for new columns). Returns (nil, nil) when no changes are required or when
// the table is Iceberg-only in both before and after.
func BuildDorisAlterTableSQL(before, after DorisTableDescriptor, tenantSlug string) ([]string, error) {
	if !validateDescriptorIdentifiers(before) || !validateDescriptorIdentifiers(after) {
		return nil, fmt.Errorf("invalid DorisTableDescriptor identifiers in BuildDorisAlterTableSQL")
	}
	if !dorisSameDorisPhysicalTable(before, after) {
		return nil, fmt.Errorf("cannot generate ALTER TABLE across different physical tables (%q vs %q)", before.TableName, after.TableName)
	}
	slug, ok := sanitizeDorisTenantSlug(tenantSlug)
	if !ok {
		return nil, fmt.Errorf("invalid tenantSlug %q in BuildDorisAlterTableSQL", tenantSlug)
	}
	if err := ValidateDorisTableEvolution(before, after); err != nil {
		return nil, err
	}
	beforeHasDoris := hasDorisTargetTable(before)
	afterHasDoris := hasDorisTargetTable(after)
	if !beforeHasDoris && !afterHasDoris {
		return nil, nil
	}
	if beforeHasDoris != afterHasDoris {
		return nil, fmt.Errorf("cannot generate Doris ALTER TABLE across sync_tier target transition (%q -> %q) for table %q", before.SyncTier, after.SyncTier, after.TableName)
	}
	beforeParts := effectiveDorisPartitionCols(before)
	afterParts := effectiveDorisPartitionCols(after)
	if !dorisStringSlicesEqual(beforeParts, afterParts) {
		return nil, fmt.Errorf("cannot mutate partition_by_columns on existing table %q (before=%v, after=%v)", after.TableName, beforeParts, afterParts)
	}

	fullTableName := dorisQualifiedTableName(after, slug)
	beforeColByName := make(map[string]DorisColumnDescriptor, len(before.Columns))
	for _, bc := range before.Columns {
		beforeColByName[bc.ColumnName] = bc
	}

	var stmts []string
	// 1. Safe type widenings or nullability changes on existing columns (MODIFY COLUMN).
	for _, ac := range after.Columns {
		bc, existed := beforeColByName[ac.ColumnName]
		if !existed {
			continue
		}
		beforeType := resolveDorisAlterColumnType(bc)
		afterType := resolveDorisAlterColumnType(ac)
		if beforeType == "" || afterType == "" {
			return nil, fmt.Errorf("column %q in table %q has empty DorisType and IcebergType", ac.ColumnName, after.TableName)
		}
		if beforeType != afterType || bc.Nullable != ac.Nullable {
			stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s;", fullTableName, formatDorisAlterColumnClause(ac, afterType, true)))
		}
	}
	// 2. Newly added columns (ADD COLUMN).
	for _, ac := range after.Columns {
		if _, existed := beforeColByName[ac.ColumnName]; existed {
			continue
		}
		afterType := resolveDorisAlterColumnType(ac)
		if afterType == "" {
			return nil, fmt.Errorf("column %q in table %q has empty DorisType and IcebergType", ac.ColumnName, after.TableName)
		}
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;", fullTableName, formatDorisAlterColumnClause(ac, afterType, true)))
	}
	return stmts, nil
}

// BuildStarRocksAlterTableSQL validates evolving before into after and returns the ordered
// StarRocks 4.x ALTER TABLE statements (MODIFY COLUMN for widened columns, then
// ADD COLUMN for new columns). Returns (nil, nil) when no changes are required or when
// the table is Iceberg-only in both before and after.
func BuildStarRocksAlterTableSQL(before, after DorisTableDescriptor, tenantSlug string) ([]string, error) {
	if !validateDescriptorIdentifiers(before) || !validateDescriptorIdentifiers(after) {
		return nil, fmt.Errorf("invalid DorisTableDescriptor identifiers in BuildStarRocksAlterTableSQL")
	}
	if !dorisSameDorisPhysicalTable(before, after) {
		return nil, fmt.Errorf("cannot generate ALTER TABLE across different physical tables (%q vs %q)", before.TableName, after.TableName)
	}
	slug, ok := sanitizeDorisTenantSlug(tenantSlug)
	if !ok {
		return nil, fmt.Errorf("invalid tenantSlug %q in BuildStarRocksAlterTableSQL", tenantSlug)
	}
	if err := ValidateDorisTableEvolution(before, after); err != nil {
		return nil, err
	}
	beforeHasSR := hasDorisTargetTable(before)
	afterHasSR := hasDorisTargetTable(after)
	if !beforeHasSR && !afterHasSR {
		return nil, nil
	}
	if beforeHasSR != afterHasSR {
		return nil, fmt.Errorf("cannot generate StarRocks ALTER TABLE across sync_tier target transition (%q -> %q) for table %q", before.SyncTier, after.SyncTier, after.TableName)
	}
	beforeParts := effectiveDorisPartitionCols(before)
	afterParts := effectiveDorisPartitionCols(after)
	if !dorisStringSlicesEqual(beforeParts, afterParts) {
		return nil, fmt.Errorf("cannot mutate partition_by_columns on existing table %q (before=%v, after=%v)", after.TableName, beforeParts, afterParts)
	}

	fullTableName := dorisQualifiedTableName(after, slug)
	beforeColByName := make(map[string]DorisColumnDescriptor, len(before.Columns))
	for _, bc := range before.Columns {
		beforeColByName[bc.ColumnName] = bc
	}

	var stmts []string
	for _, ac := range after.Columns {
		bc, existed := beforeColByName[ac.ColumnName]
		if !existed {
			continue
		}
		beforeSR := dorisColumnStarRocksDDLType(bc.DorisType, bc.IcebergType)
		afterSR := dorisColumnStarRocksDDLType(ac.DorisType, ac.IcebergType)
		if beforeSR == "" || afterSR == "" {
			return nil, fmt.Errorf("column %q in table %q has empty DorisType and IcebergType", ac.ColumnName, after.TableName)
		}
		if beforeSR != afterSR || bc.Nullable != ac.Nullable {
			stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s;", fullTableName, formatDorisAlterColumnClause(ac, afterSR, true)))
		}
	}
	for _, ac := range after.Columns {
		if _, existed := beforeColByName[ac.ColumnName]; existed {
			continue
		}
		afterSR := dorisColumnStarRocksDDLType(ac.DorisType, ac.IcebergType)
		if afterSR == "" {
			return nil, fmt.Errorf("column %q in table %q has empty DorisType and IcebergType", ac.ColumnName, after.TableName)
		}
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;", fullTableName, formatDorisAlterColumnClause(ac, afterSR, true)))
	}
	return stmts, nil
}

// BuildIcebergAlterTableSQL validates evolving before into after and returns the ordered
// Apache Iceberg ALTER TABLE statements (MODIFY COLUMN for widened columns, then
// ADD COLUMN for new columns). Returns (nil, nil) when no changes are required or when
// SyncTier == "INTERNAL_ONLY" in both before and after.
func BuildIcebergAlterTableSQL(before, after DorisTableDescriptor, tenantSlug string) ([]string, error) {
	if !validateDescriptorIdentifiers(before) || !validateDescriptorIdentifiers(after) {
		return nil, fmt.Errorf("invalid DorisTableDescriptor identifiers in BuildIcebergAlterTableSQL")
	}
	if dorisTableEvolutionIdentity(before) != dorisTableEvolutionIdentity(after) {
		return nil, fmt.Errorf("cannot generate ALTER TABLE across different physical tables (%q vs %q)", before.TableName, after.TableName)
	}
	slug, ok := sanitizeDorisTenantSlug(tenantSlug)
	if !ok {
		return nil, fmt.Errorf("invalid tenantSlug %q in BuildIcebergAlterTableSQL", tenantSlug)
	}
	if err := ValidateDorisTableEvolution(before, after); err != nil {
		return nil, err
	}
	beforeHasIce := hasIcebergTargetTable(before)
	afterHasIce := hasIcebergTargetTable(after)
	if !beforeHasIce && !afterHasIce {
		return nil, nil
	}
	if beforeHasIce != afterHasIce {
		return nil, fmt.Errorf("cannot generate Iceberg ALTER TABLE across sync_tier target transition (%q -> %q) for table %q", before.SyncTier, after.SyncTier, after.TableName)
	}
	beforeParts := effectiveDorisPartitionCols(before)
	afterParts := effectiveDorisPartitionCols(after)
	if !dorisStringSlicesEqual(beforeParts, afterParts) || strings.TrimSpace(before.IcebergPartitionSpec) != strings.TrimSpace(after.IcebergPartitionSpec) {
		return nil, fmt.Errorf("cannot mutate partition_by_columns on existing Iceberg table %q (before=%v, after=%v)", after.TableName, beforeParts, afterParts)
	}

	fullTableName := icebergQualifiedTableName(after, slug)
	beforeColByName := make(map[string]DorisColumnDescriptor, len(before.Columns))
	for _, bc := range before.Columns {
		beforeColByName[bc.ColumnName] = bc
	}

	var stmts []string
	for _, ac := range after.Columns {
		bc, existed := beforeColByName[ac.ColumnName]
		if !existed {
			continue
		}
		beforeIce := dorisColumnIcebergDDLType(bc.DorisType, bc.IcebergType)
		afterIce := dorisColumnIcebergDDLType(ac.DorisType, ac.IcebergType)
		if beforeIce == "" || afterIce == "" {
			return nil, fmt.Errorf("column %q in table %q has empty DorisType and IcebergType", ac.ColumnName, after.TableName)
		}
		if beforeIce != afterIce || bc.Nullable != ac.Nullable {
			stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s;", fullTableName, formatDorisAlterColumnClause(ac, afterIce, false)))
		}
	}
	for _, ac := range after.Columns {
		if _, existed := beforeColByName[ac.ColumnName]; existed {
			continue
		}
		afterIce := dorisColumnIcebergDDLType(ac.DorisType, ac.IcebergType)
		if afterIce == "" {
			return nil, fmt.Errorf("column %q in table %q has empty DorisType and IcebergType", ac.ColumnName, after.TableName)
		}
		if !ac.Nullable && !isDorisNonScalarColumnType(afterIce) {
			return nil, fmt.Errorf("cannot add NOT NULL scalar column %q to existing Iceberg table %q: Iceberg ALTER TABLE ADD COLUMN does not support initial DEFAULT values", ac.ColumnName, after.TableName)
		}
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;", fullTableName, formatDorisAlterColumnClause(ac, afterIce, false)))
	}
	return stmts, nil
}

func indexDorisSchemaEvolutionTables(
	before, after []DorisTableDescriptor,
	caller string,
) (map[string]DorisTableDescriptor, map[string]DorisTableDescriptor, map[string]int, map[string]int, error) {
	beforeByKey := make(map[string]DorisTableDescriptor, len(before))
	beforeByScope := make(map[string]DorisTableDescriptor, len(before))
	beforeScopeCount := make(map[string]int, len(before))
	for _, t := range before {
		if !validateDescriptorIdentifiers(t) {
			return nil, nil, nil, nil, fmt.Errorf("invalid DorisTableDescriptor identifiers for table %q in %s", t.TableName, caller)
		}
		key := dorisTableEvolutionIdentity(t)
		if _, dup := beforeByKey[key]; dup {
			return nil, nil, nil, nil, fmt.Errorf("duplicate table identity for table %q in %s", t.TableName, caller)
		}
		beforeByKey[key] = t
		scopeKey := dorisScopeTableIdentity(t)
		beforeByScope[scopeKey] = t
		beforeScopeCount[scopeKey]++
	}
	afterSeenKey := make(map[string]bool, len(after))
	afterScopeCount := make(map[string]int, len(after))
	for _, t := range after {
		if !validateDescriptorIdentifiers(t) {
			return nil, nil, nil, nil, fmt.Errorf("invalid DorisTableDescriptor identifiers for table %q in %s", t.TableName, caller)
		}
		key := dorisTableEvolutionIdentity(t)
		if afterSeenKey[key] {
			return nil, nil, nil, nil, fmt.Errorf("duplicate table identity for table %q in %s", t.TableName, caller)
		}
		afterSeenKey[key] = true
		scopeKey := dorisScopeTableIdentity(t)
		afterScopeCount[scopeKey]++
	}
	return beforeByKey, beforeByScope, beforeScopeCount, afterScopeCount, nil
}

// rejectDuplicateDorisTargetScope rejects two after descriptors that materialize
// Doris or StarRocks tables with the same database scope and table name. The
// CREATE TABLE name omits IcebergCatalog, so both would target one physical table.
func rejectDuplicateDorisTargetScope(after []DorisTableDescriptor, engine, caller string) error {
	seen := make(map[string]bool, len(after))
	for _, t := range after {
		if !hasDorisTargetTable(t) {
			continue
		}
		scopeKey := dorisScopeTableIdentity(t)
		if seen[scopeKey] {
			return fmt.Errorf("duplicate %s table name %q in scope in %s", engine, t.TableName, caller)
		}
		seen[scopeKey] = true
	}
	return nil
}

func validateDorisNewTableColumns(t DorisTableDescriptor, engine string) error {
	if len(t.Columns) == 0 {
		return fmt.Errorf("failed to build %s CREATE TABLE SQL for new table %q: no columns", engine, t.TableName)
	}
	for _, c := range t.Columns {
		if strings.TrimSpace(c.DorisType) == "" && strings.TrimSpace(c.IcebergType) == "" {
			return fmt.Errorf("column %q in table %q has empty DorisType and IcebergType", c.ColumnName, t.TableName)
		}
	}
	return nil
}

// hasAmbiguousDorisCatalogOmission reports whether beforeTbl could not be paired by
// matchBeforeDorisTable because an omitted IcebergCatalog shared its (DatabaseScope, TableName)
// with multiple descriptors.
func hasAmbiguousDorisCatalogOmission(
	beforeTbl DorisTableDescriptor,
	after []DorisTableDescriptor,
	beforeScopeCount, afterScopeCount map[string]int,
) bool {
	scopeKey := dorisScopeTableIdentity(beforeTbl)
	if scopeKey == "" || (beforeScopeCount[scopeKey] <= 1 && afterScopeCount[scopeKey] <= 1) {
		return false
	}
	for _, afterTbl := range after {
		if dorisSameDorisPhysicalTable(beforeTbl, afterTbl) {
			return true
		}
	}
	return false
}

// BuildDorisSchemaMigrationSQL validates evolving a slice of before table descriptors
// into after table descriptors and returns the ordered Apache Doris 4.x DDL statements:
// ALTER TABLE statements for evolved existing tables and CREATE TABLE IF NOT EXISTS
// statements for newly added tables. Rejects dropping existing Doris tables.
func BuildDorisSchemaMigrationSQL(before, after []DorisTableDescriptor, tenantSlug string, buckets int) ([]string, error) {
	if _, ok := sanitizeDorisTenantSlug(tenantSlug); !ok {
		return nil, fmt.Errorf("invalid tenantSlug %q in BuildDorisSchemaMigrationSQL", tenantSlug)
	}
	beforeByKey, beforeByScope, beforeScopeCount, afterScopeCount, err := indexDorisSchemaEvolutionTables(before, after, "BuildDorisSchemaMigrationSQL")
	if err != nil {
		return nil, err
	}
	if err := ValidateDorisSchemaEvolution(before, after); err != nil {
		return nil, err
	}
	if err := rejectDuplicateDorisTargetScope(after, "Doris", "BuildDorisSchemaMigrationSQL"); err != nil {
		return nil, err
	}
	matchedBeforeKeys := make(map[string]bool, len(before))
	var stmts []string
	for _, afterTbl := range after {
		if beforeTbl, matchedKey, ok := matchBeforeDorisTable(afterTbl, beforeByKey, beforeByScope, beforeScopeCount, afterScopeCount); ok {
			matchedBeforeKeys[matchedKey] = true
			alterStmts, err := BuildDorisAlterTableSQL(beforeTbl, afterTbl, tenantSlug)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, alterStmts...)
			continue
		}
		if !hasDorisTargetTable(afterTbl) {
			continue
		}
		if err := validateDorisNewTableColumns(afterTbl, "Doris"); err != nil {
			return nil, err
		}
		createSQL := afterTbl.BuildDorisCreateTableSQL(tenantSlug, buckets)
		if createSQL == "" {
			return nil, fmt.Errorf("failed to build Doris CREATE TABLE SQL for new table %q", afterTbl.TableName)
		}
		stmts = append(stmts, createSQL)
	}
	for _, beforeTbl := range before {
		key := dorisTableEvolutionIdentity(beforeTbl)
		if !matchedBeforeKeys[key] && hasDorisTargetTable(beforeTbl) {
			if hasAmbiguousDorisCatalogOmission(beforeTbl, after, beforeScopeCount, afterScopeCount) {
				return nil, fmt.Errorf("ambiguous IcebergCatalog omission for Doris table %q in BuildDorisSchemaMigrationSQL", beforeTbl.TableName)
			}
			return nil, fmt.Errorf("dropping existing Doris table %q is forbidden in BuildDorisSchemaMigrationSQL", beforeTbl.TableName)
		}
	}
	return stmts, nil
}

// BuildStarRocksSchemaMigrationSQL validates evolving a slice of before table descriptors
// into after table descriptors and returns the ordered StarRocks 4.x DDL statements:
// ALTER TABLE statements for evolved existing tables and CREATE TABLE IF NOT EXISTS
// statements for newly added tables. Rejects dropping existing StarRocks tables.
func BuildStarRocksSchemaMigrationSQL(before, after []DorisTableDescriptor, tenantSlug string, buckets int) ([]string, error) {
	if _, ok := sanitizeDorisTenantSlug(tenantSlug); !ok {
		return nil, fmt.Errorf("invalid tenantSlug %q in BuildStarRocksSchemaMigrationSQL", tenantSlug)
	}
	beforeByKey, beforeByScope, beforeScopeCount, afterScopeCount, err := indexDorisSchemaEvolutionTables(before, after, "BuildStarRocksSchemaMigrationSQL")
	if err != nil {
		return nil, err
	}
	if err := ValidateDorisSchemaEvolution(before, after); err != nil {
		return nil, err
	}
	if err := rejectDuplicateDorisTargetScope(after, "StarRocks", "BuildStarRocksSchemaMigrationSQL"); err != nil {
		return nil, err
	}
	matchedBeforeKeys := make(map[string]bool, len(before))
	var stmts []string
	for _, afterTbl := range after {
		if beforeTbl, matchedKey, ok := matchBeforeDorisTable(afterTbl, beforeByKey, beforeByScope, beforeScopeCount, afterScopeCount); ok {
			matchedBeforeKeys[matchedKey] = true
			alterStmts, err := BuildStarRocksAlterTableSQL(beforeTbl, afterTbl, tenantSlug)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, alterStmts...)
			continue
		}
		if !hasDorisTargetTable(afterTbl) {
			continue
		}
		if err := validateDorisNewTableColumns(afterTbl, "StarRocks"); err != nil {
			return nil, err
		}
		createSQL := afterTbl.BuildStarRocksCreateTableSQL(tenantSlug, buckets)
		if createSQL == "" {
			return nil, fmt.Errorf("failed to build StarRocks CREATE TABLE SQL for new table %q", afterTbl.TableName)
		}
		stmts = append(stmts, createSQL)
	}
	for _, beforeTbl := range before {
		key := dorisTableEvolutionIdentity(beforeTbl)
		if !matchedBeforeKeys[key] && hasDorisTargetTable(beforeTbl) {
			if hasAmbiguousDorisCatalogOmission(beforeTbl, after, beforeScopeCount, afterScopeCount) {
				return nil, fmt.Errorf("ambiguous IcebergCatalog omission for StarRocks table %q in BuildStarRocksSchemaMigrationSQL", beforeTbl.TableName)
			}
			return nil, fmt.Errorf("dropping existing StarRocks table %q is forbidden in BuildStarRocksSchemaMigrationSQL", beforeTbl.TableName)
		}
	}
	return stmts, nil
}

// BuildIcebergSchemaMigrationSQL validates evolving a slice of before table descriptors
// into after table descriptors and returns the ordered Apache Iceberg DDL statements:
// ALTER TABLE statements for evolved existing tables and CREATE TABLE IF NOT EXISTS
// statements for newly added tables. Rejects dropping existing Iceberg tables.
func BuildIcebergSchemaMigrationSQL(before, after []DorisTableDescriptor, tenantSlug string) ([]string, error) {
	if _, ok := sanitizeDorisTenantSlug(tenantSlug); !ok {
		return nil, fmt.Errorf("invalid tenantSlug %q in BuildIcebergSchemaMigrationSQL", tenantSlug)
	}
	beforeByKey, _, _, _, err := indexDorisSchemaEvolutionTables(before, after, "BuildIcebergSchemaMigrationSQL")
	if err != nil {
		return nil, err
	}
	if err := ValidateDorisSchemaEvolution(before, after); err != nil {
		return nil, err
	}
	matchedBeforeKeys := make(map[string]bool, len(before))
	var stmts []string
	for _, afterTbl := range after {
		key := dorisTableEvolutionIdentity(afterTbl)
		if beforeTbl, ok := beforeByKey[key]; ok {
			matchedBeforeKeys[key] = true
			alterStmts, err := BuildIcebergAlterTableSQL(beforeTbl, afterTbl, tenantSlug)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, alterStmts...)
			continue
		}
		if !hasIcebergTargetTable(afterTbl) {
			continue
		}
		if err := validateDorisNewTableColumns(afterTbl, "Iceberg"); err != nil {
			return nil, err
		}
		createSQL := afterTbl.BuildIcebergCreateTableSQL(tenantSlug)
		if createSQL == "" {
			return nil, fmt.Errorf("failed to build Iceberg CREATE TABLE SQL for new table %q", afterTbl.TableName)
		}
		stmts = append(stmts, createSQL)
	}
	for _, beforeTbl := range before {
		key := dorisTableEvolutionIdentity(beforeTbl)
		if !matchedBeforeKeys[key] && hasIcebergTargetTable(beforeTbl) {
			return nil, fmt.Errorf("dropping existing Iceberg table %q is forbidden in BuildIcebergSchemaMigrationSQL", beforeTbl.TableName)
		}
	}
	return stmts, nil
}

func resolveDorisAlterColumnType(c DorisColumnDescriptor) string {
	dt := strings.ToUpper(strings.TrimSpace(c.DorisType))
	if dt != "" {
		return dt
	}
	return dorisColumnIcebergDDLType(c.DorisType, c.IcebergType)
}

func dorisQualifiedTableName(t DorisTableDescriptor, slug string) string {
	switch t.DatabaseScope {
	case "GLOBAL_TERMINOLOGY":
		return "internal.terminology." + t.TableName
	case "GLOBAL_TERMINOLOGY_RAG":
		return "internal.terminology_rag." + t.TableName
	case "GLOBAL_TELEMETRY":
		return "internal.telemetry." + t.TableName
	default:
		return "internal.tenant_" + slug + "." + t.TableName
	}
}

func icebergQualifiedTableName(t DorisTableDescriptor, slug string) string {
	catalog := strings.TrimSpace(t.IcebergCatalog)
	if catalog == "" {
		catalog = "iceberg"
	}
	switch t.DatabaseScope {
	case "GLOBAL_TERMINOLOGY":
		return catalog + ".terminology." + t.TableName
	case "GLOBAL_TELEMETRY":
		return catalog + ".telemetry." + t.TableName
	case "GLOBAL_TERMINOLOGY_RAG":
		return catalog + ".terminology_rag." + t.TableName
	default:
		return catalog + ".tenant_" + slug + "." + t.TableName
	}
}

func formatDorisAlterColumnClause(c DorisColumnDescriptor, colType string, includeDefaults bool) string {
	colIdent := formatDorisDDLColumnIdent(c.ColumnName)
	clause := fmt.Sprintf("%s %s", colIdent, colType)
	if !c.Nullable {
		clause += " NOT NULL"
		if includeDefaults {
			cWithType := c
			cWithType.DorisType = colType
			if defVal, hasDef := resolveDorisColumnDefaultValue(cWithType); hasDef {
				clause += fmt.Sprintf(" DEFAULT %q", defVal)
			}
		}
	} else if includeDefaults && c.DefaultValue != "" {
		clause += fmt.Sprintf(" DEFAULT %q", c.DefaultValue)
	}
	return clause
}
