package generator

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

// DorisColumnDescriptor describes a single physical column in an Apache Doris
// and Apache Iceberg lakehouse table.
type DorisColumnDescriptor struct {
	ColumnName      string
	ProtoFieldName  string
	DorisType       string
	IcebergType     string
	Nullable        bool
	InvertedIndex   bool
	IsUniqueKey     bool
	IsSequenceCol   bool
	IsPartitionKey  bool
	StripEnumPrefix bool
	DefaultValue    string
	Comment         string
}

// DorisIndexDescriptor describes a physical index on an Apache Doris table
// (INVERTED, NGRAM_BF, or ANN).
type DorisIndexDescriptor struct {
	Name       string
	Columns    []string
	Using      string
	Properties map[string]string
}

// CanonicalIcebergSpecType returns the canonical Apache Iceberg specification
// type (e.g. "STRING", "LONG", "INT", "TIMESTAMPTZ", "DATE", "LIST<STRING>")
// for this column descriptor.
func (c DorisColumnDescriptor) CanonicalIcebergSpecType() string {
	it := strings.TrimSpace(c.IcebergType)
	if it != "" {
		return it
	}
	dt := strings.ToUpper(strings.TrimSpace(c.DorisType))
	switch dt {
	case "DATEV2", "DATE":
		return "DATE"
	case "DATETIMEV2", "DATETIME":
		return "TIMESTAMPTZ"
	case "BIGINT", "LARGEINT":
		return "LONG"
	case "SMALLINT", "TINYINT", "INT":
		return "INT"
	case "BOOLEAN":
		return "BOOLEAN"
	case "FLOAT":
		return "FLOAT"
	case "DOUBLE":
		return "DOUBLE"
	}
	if (strings.HasPrefix(dt, "DATETIMEV2(") || strings.HasPrefix(dt, "DATETIME(")) && strings.HasSuffix(dt, ")") {
		return "TIMESTAMPTZ"
	}
	if strings.HasPrefix(dt, "VARCHAR") || dt == "TEXT" || dt == "STRING" || dt == "JSON" || dt == "JSONB" || dt == "VARIANT" {
		return "STRING"
	}
	if strings.HasPrefix(dt, "DECIMALV3(") {
		return "DECIMAL(" + strings.TrimPrefix(dt, "DECIMALV3(")
	}
	if strings.HasPrefix(dt, "ARRAY<") && strings.HasSuffix(dt, ">") {
		inner := strings.TrimSuffix(strings.TrimPrefix(dt, "ARRAY<"), ">")
		return "LIST<" + (DorisColumnDescriptor{DorisType: inner}).CanonicalIcebergSpecType() + ">"
	}
	return dt
}

// DorisTableDescriptor describes an Apache Doris and Apache Iceberg table
// generated from a protobuf message annotated with table_meta.
type DorisTableDescriptor struct {
	TableName            string
	MessageName          string
	DatabaseScope        string
	SyncTier             string
	UniqueKeyColumns     []string
	PartitionByColumns   []string
	ColocateGroup        string
	SequenceCol          string
	SequenceColumn       string
	InvertedIndexColumns []string
	NgramBfColumns       []string
	BloomFilterColumns   []string
	DistributedByColumn  string
	DistributionColumns  []string
	DuplicateKeyColumns  []string
	FileCacheTTLSeconds  int32
	EmbeddingDimension   int32
	StorageVaultName     string
	IcebergCatalog       string
	IcebergPartitionSpec string
	Buckets              int
	AutoBuckets          bool
	EnableMoW            bool
	Indexes              []DorisIndexDescriptor
	Columns              []DorisColumnDescriptor
}

// dorisColumnStarRocksDDLType maps a column's Doris/Iceberg type metadata to the
// SQL column type used in StarRocks 4.x CREATE TABLE DDL:
//   - DATEV2 -> DATE
//   - DATETIMEV2 / DATETIMEV2(p) -> DATETIME
//   - DECIMALV3(p, s) -> DECIMAL(p, s)
//   - TEXT -> STRING (and recursively inside ARRAY<T>)
func dorisColumnStarRocksDDLType(dorisType, icebergType string) string {
	dt := strings.ToUpper(strings.TrimSpace(dorisType))
	switch dt {
	case "DATEV2", "DATE":
		return "DATE"
	case "DATETIMEV2", "DATETIME":
		return "DATETIME"
	case "TEXT", "STRING":
		return "STRING"
	}
	if (strings.HasPrefix(dt, "DATETIMEV2(") || strings.HasPrefix(dt, "DATETIME(")) && strings.HasSuffix(dt, ")") {
		return "DATETIME"
	}
	if strings.HasPrefix(dt, "DECIMALV3(") && strings.HasSuffix(dt, ")") {
		rest := strings.TrimPrefix(dt, "DECIMALV3(")
		rest = strings.ReplaceAll(rest, ", ", ",")
		rest = strings.ReplaceAll(rest, ",", ", ")
		return "DECIMAL(" + rest
	}
	if strings.HasPrefix(dt, "DECIMAL(") && strings.HasSuffix(dt, ")") {
		rest := strings.TrimPrefix(dt, "DECIMAL(")
		rest = strings.ReplaceAll(rest, ", ", ",")
		rest = strings.ReplaceAll(rest, ",", ", ")
		return "DECIMAL(" + rest
	}
	if strings.HasPrefix(dt, "ARRAY<") && strings.HasSuffix(dt, ">") {
		inner := strings.TrimSuffix(strings.TrimPrefix(dt, "ARRAY<"), ">")
		return "ARRAY<" + dorisColumnStarRocksDDLType(inner, "") + ">"
	}
	if dt != "" {
		return dt
	}
	it := strings.TrimSpace(icebergType)
	switch strings.ToUpper(it) {
	case "STRING":
		return "VARCHAR(256)"
	case "LONG":
		return "BIGINT"
	case "TIMESTAMPTZ", "TIMESTAMP":
		return "DATETIME"
	case "LIST<STRING>":
		return "ARRAY<VARCHAR(64)>"
	case "LIST<FLOAT>":
		return "ARRAY<FLOAT>"
	case "LIST<DOUBLE>":
		return "ARRAY<DOUBLE>"
	case "LIST<INT>":
		return "ARRAY<INT>"
	case "LIST<LONG>":
		return "ARRAY<BIGINT>"
	}
	return strings.ToUpper(it)
}

// dorisColumnIcebergDDLType maps a column's Doris/Iceberg type metadata to the
// SQL column type used in Doris's Iceberg catalog CREATE TABLE DDL.
func dorisColumnIcebergDDLType(dorisType, icebergType string) string {
	dt := strings.ToUpper(strings.TrimSpace(dorisType))
	switch dt {
	case "DATEV2":
		return "DATE"
	case "DATETIMEV2":
		return "DATETIME"
	case "LARGEINT":
		return "DECIMAL(38, 0)"
	case "ARRAY<LARGEINT>":
		return "ARRAY<DECIMAL(38, 0)>"
	case "JSON", "JSONB", "VARIANT":
		return "STRING"
	}
	if strings.HasPrefix(dt, "DATETIMEV2(") && strings.HasSuffix(dt, ")") {
		return "DATETIME"
	}
	if strings.HasPrefix(dt, "DECIMALV3(") {
		rest := strings.TrimPrefix(dt, "DECIMALV3(")
		rest = strings.ReplaceAll(rest, ", ", ",")
		rest = strings.ReplaceAll(rest, ",", ", ")
		return "DECIMAL(" + rest
	}
	if strings.HasPrefix(dt, "ARRAY<") && strings.HasSuffix(dt, ">") {
		inner := strings.TrimSuffix(strings.TrimPrefix(dt, "ARRAY<"), ">")
		return "ARRAY<" + dorisColumnIcebergDDLType(inner, "") + ">"
	}
	if dt != "" {
		return dt
	}
	it := strings.TrimSpace(icebergType)
	switch strings.ToUpper(it) {
	case "STRING":
		return "VARCHAR(256)"
	case "LONG":
		return "BIGINT"
	case "TIMESTAMPTZ", "TIMESTAMP":
		return "DATETIME"
	case "LIST<STRING>":
		return "ARRAY<VARCHAR(64)>"
	case "LIST<FLOAT>":
		return "ARRAY<FLOAT>"
	case "LIST<DOUBLE>":
		return "ARRAY<DOUBLE>"
	case "LIST<INT>":
		return "ARRAY<INT>"
	case "LIST<LONG>":
		return "ARRAY<BIGINT>"
	}
	return strings.ToUpper(it)
}

// sanitizeDorisTenantSlug normalizes and validates a tenant slug for SQL DDL interpolation.
// Hyphens are normalized to underscores. Returns ("", false) if the slug contains
// any characters outside [a-zA-Z0-9_-].
func sanitizeDorisTenantSlug(tenantSlug string) (string, bool) {
	slug := strings.TrimSpace(tenantSlug)
	if slug == "" || slug == "${TENANT_SLUG}" {
		return "${TENANT_SLUG}", true
	}
	slug = strings.ReplaceAll(slug, "-", "_")
	if !isValidSQLIdentifier(slug) {
		return "", false
	}
	return slug, true
}

// validateDescriptorIdentifiers verifies that all table, column, index, default value,
// and type strings in t are safe for SQL DDL emission and that all referenced columns exist.
func validateDescriptorIdentifiers(t DorisTableDescriptor) bool {
	if !isValidSQLIdentifier(t.TableName) || !isValidDatabaseScope(t.DatabaseScope) || !isValidSyncTier(t.SyncTier) {
		return false
	}
	if t.ColocateGroup != "" && !isValidSQLIdentifier(t.ColocateGroup) {
		return false
	}
	if t.StorageVaultName != "" && !isValidSQLIdentifier(t.StorageVaultName) {
		return false
	}
	if t.IcebergCatalog != "" && !isValidSQLIdentifier(t.IcebergCatalog) {
		return false
	}
	if t.EmbeddingDimension < 0 {
		return false
	}
	colSet := make(map[string]bool, len(t.Columns))
	for _, c := range t.Columns {
		if !isValidSQLIdentifier(c.ColumnName) || colSet[c.ColumnName] {
			return false
		}
		if c.DorisType != "" && !isValidSQLTypeSyntax(c.DorisType) {
			return false
		}
		if c.IcebergType != "" && !isValidSQLTypeSyntax(c.IcebergType) {
			return false
		}
		if c.DefaultValue != "" && !isValidDorisDefaultValue(c.DefaultValue) {
			return false
		}
		colSet[c.ColumnName] = true
	}
	if t.SequenceCol != "" && (!isValidSQLIdentifier(t.SequenceCol) || !colSet[t.SequenceCol]) {
		return false
	}
	if t.SequenceColumn != "" && (!isValidSQLIdentifier(t.SequenceColumn) || !colSet[t.SequenceColumn]) {
		return false
	}
	if t.DistributedByColumn != "" && (!isValidSQLIdentifier(t.DistributedByColumn) || !colSet[t.DistributedByColumn]) {
		return false
	}
	for _, list := range [][]string{
		t.UniqueKeyColumns,
		t.PartitionByColumns,
		t.InvertedIndexColumns,
		t.NgramBfColumns,
		t.BloomFilterColumns,
		t.DuplicateKeyColumns,
		t.DistributionColumns,
	} {
		for _, id := range list {
			if !isValidSQLIdentifier(id) || !colSet[id] {
				return false
			}
		}
	}
	for _, idx := range t.Indexes {
		if idx.Name != "" && !isValidSQLIdentifier(idx.Name) {
			return false
		}
		for _, id := range idx.Columns {
			if !isValidSQLIdentifier(id) || !colSet[id] {
				return false
			}
		}
	}
	return true
}

// dedupDorisColumnList returns a copy of cols with empty strings and duplicate
// column names removed while preserving first-seen order.
func dedupDorisColumnList(cols []string) []string {
	if len(cols) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(cols))
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// reorderDorisDescriptorColumns moves UNIQUE KEY or DUPLICATE KEY columns to the
// front of cols in key order and, when forceKeyNonNull is true, marks them
// non-nullable, preserving the relative order of all remaining columns.
func reorderDorisDescriptorColumns(cols []DorisColumnDescriptor, uks, dupKeys []string, forceKeyNonNull bool) []DorisColumnDescriptor {
	keyCols := uks
	if len(keyCols) == 0 && len(dupKeys) > 0 {
		keyCols = dupKeys
	}
	if len(keyCols) == 0 {
		return cols
	}
	colByName := make(map[string]DorisColumnDescriptor, len(cols))
	for _, c := range cols {
		colByName[c.ColumnName] = c
	}
	placed := make(map[string]bool, len(keyCols))
	reordered := make([]DorisColumnDescriptor, 0, len(cols))
	for _, k := range keyCols {
		if c, ok := colByName[k]; ok && !placed[k] {
			placed[k] = true
			if forceKeyNonNull {
				c.Nullable = false
			}
			reordered = append(reordered, c)
		}
	}
	for _, c := range cols {
		if !placed[c.ColumnName] {
			reordered = append(reordered, c)
		}
	}
	return reordered
}

// isDorisReservedKeyword reports whether colName is a reserved SQL keyword in
// Apache Doris 4.x that must be backtick-quoted in DDL statements.
func isDorisReservedKeyword(colName string) bool {
	switch strings.ToLower(colName) {
	case "system", "key", "keys", "order", "range", "values", "index", "rank",
		"group", "table", "tables", "database", "databases", "schema", "schemas",
		"partition", "partitions", "buckets", "desc", "describe", "asc", "default",
		"primary", "unique", "duplicate", "aggregate", "check", "references",
		"foreign", "constraint", "select", "from", "where", "join", "inner",
		"left", "right", "full", "outer", "cross", "on", "using", "in", "is",
		"null", "not", "and", "or", "xor", "as", "by", "limit", "offset",
		"having", "union", "intersect", "except", "all", "distinct", "case",
		"when", "then", "else", "end", "exists", "between", "like", "ilike",
		"regexp", "rlike", "div", "mod", "interval", "function", "procedure",
		"trigger", "view", "grant", "revoke", "show", "explain", "use", "set",
		"kill", "load", "export", "backup", "restore", "alter", "create", "drop",
		"truncate", "rename", "replace", "insert", "update", "delete", "merge",
		"into", "over", "window", "rows", "preceding", "following", "current",
		"true", "false", "properties", "inverted", "ngram_bf":
		return true
	default:
		return false
	}
}

// formatDorisDDLColumnIdent formats a column identifier for Doris/Iceberg DDL,
// quoting it with backticks when it collides with a Doris reserved keyword.
func formatDorisDDLColumnIdent(colName string) string {
	if isDorisReservedKeyword(colName) {
		return "`" + colName + "`"
	}
	return colName
}

// formatDorisDDLColumnIdentList formats a comma-separated list of column identifiers
// for Doris/Iceberg DDL, backtick-quoting any reserved keywords.
func formatDorisDDLColumnIdentList(cols []string) string {
	formatted := make([]string, len(cols))
	for i, c := range cols {
		formatted[i] = formatDorisDDLColumnIdent(c)
	}
	return strings.Join(formatted, ", ")
}

// resolveDorisEmbeddingDimension returns the validated HNSW vector dimension for t.
// When t.EmbeddingDimension > 0, that explicit dimension is returned; when unset (0),
// only clinical_concept_search (guaranteed to use 768-element vectors) defaults to 768.
func resolveDorisEmbeddingDimension(t DorisTableDescriptor) (int32, bool) {
	if t.EmbeddingDimension > 0 {
		return t.EmbeddingDimension, true
	}
	if t.EmbeddingDimension == 0 && t.TableName == "clinical_concept_search" {
		return 768, true
	}
	return 0, false
}

// BuildDorisCreateTableSQL renders a deterministic Apache Doris 4.x
// CREATE TABLE IF NOT EXISTS statement for this table descriptor.
// Returns an empty string if the table is Iceberg-only or fails identifier validation.
func (t DorisTableDescriptor) BuildDorisCreateTableSQL(tenantSlug string, buckets int) string {
	if !validateDescriptorIdentifiers(t) {
		return ""
	}
	if t.SyncTier == "ICEBERG_ONLY" {
		return ""
	}

	// Determine UNIQUE KEY and DUPLICATE KEY columns (deduplicated in first-seen order).
	var uks []string
	if len(t.UniqueKeyColumns) > 0 {
		uks = dedupDorisColumnList(t.UniqueKeyColumns)
	} else {
		for _, c := range t.Columns {
			if c.IsUniqueKey {
				uks = append(uks, c.ColumnName)
			}
		}
		uks = dedupDorisColumnList(uks)
	}
	dupKeys := dedupDorisColumnList(t.DuplicateKeyColumns)
	ngramCols := dedupDorisColumnList(t.NgramBfColumns)
	bloomCols := dedupDorisColumnList(t.BloomFilterColumns)

	// Global terminology release-partitioned tables without Doris unique/duplicate keys
	// live exclusively in the Apache Iceberg catalog (<catalog>.terminology.*).
	if t.DatabaseScope == "GLOBAL_TERMINOLOGY" && t.SyncTier == "RELEASE_PARTITIONED" &&
		len(uks) == 0 && len(dupKeys) == 0 {
		return ""
	}

	orderedCols := reorderDorisDescriptorColumns(t.Columns, uks, dupKeys, true)
	if len(uks) == 0 && len(dupKeys) == 0 && len(orderedCols) > 0 {
		cp := make([]DorisColumnDescriptor, len(orderedCols))
		copy(cp, orderedCols)
		cp[0].Nullable = false
		orderedCols = cp
	}

	colTypes := make(map[string]string, len(orderedCols))
	for _, c := range orderedCols {
		colTypes[c.ColumnName] = strings.ToUpper(strings.TrimSpace(c.DorisType))
	}
	for _, uk := range uks {
		if !isDorisKeyEligibleType(colTypes[uk]) {
			return ""
		}
	}
	for _, dk := range dupKeys {
		if !isDorisKeyEligibleType(colTypes[dk]) {
			return ""
		}
	}
	if len(uks) == 0 && len(dupKeys) == 0 && len(orderedCols) > 0 {
		if !isDorisKeyEligibleType(orderedCols[0].DorisType) {
			return ""
		}
	}
	if t.DistributedByColumn != "" && !isDorisKeyEligibleType(colTypes[t.DistributedByColumn]) {
		return ""
	}

	slug, ok := sanitizeDorisTenantSlug(tenantSlug)
	if !ok {
		return ""
	}
	bucketStr := "${TENANT_BUCKETS}"
	if buckets > 0 {
		bucketStr = strconv.Itoa(buckets)
	}

	var fullTableName string
	switch t.DatabaseScope {
	case "GLOBAL_TERMINOLOGY":
		fullTableName = "internal.terminology." + t.TableName
		if buckets <= 0 {
			bucketStr = "1"
		}
	case "GLOBAL_TERMINOLOGY_RAG":
		fullTableName = "internal.terminology_rag." + t.TableName
		if buckets <= 0 {
			bucketStr = "8"
		}
	case "GLOBAL_TELEMETRY":
		fullTableName = "internal.telemetry." + t.TableName
		if buckets <= 0 {
			bucketStr = "8"
		}
	default:
		fullTableName = "internal.tenant_" + slug + "." + t.TableName
	}

	var bodyLines []string
	for _, c := range orderedCols {
		colIdent := formatDorisDDLColumnIdent(c.ColumnName)
		line := fmt.Sprintf("    %s %s", colIdent, c.DorisType)
		if !c.Nullable {
			line += " NOT NULL"
			upperType := strings.ToUpper(strings.TrimSpace(c.DorisType))
			if c.DefaultValue != "" {
				line += fmt.Sprintf(" DEFAULT %q", c.DefaultValue)
			} else if strings.HasPrefix(upperType, "ARRAY<") && c.ColumnName != "embedding" {
				line += ` DEFAULT "[]"`
			} else if c.ColumnName == "record_status" {
				line += ` DEFAULT "ACTIVE"`
			} else if c.IsSequenceCol {
				line += ` DEFAULT "0"`
			} else if upperType == "BOOLEAN" && !c.IsUniqueKey {
				line += ` DEFAULT "false"`
			} else if (upperType == "SMALLINT" || upperType == "INT") && !c.IsUniqueKey &&
				(strings.HasSuffix(c.ColumnName, "_count") || c.ColumnName == "display_order") {
				line += ` DEFAULT "0"`
			} else if upperType == "DOUBLE" && !c.IsUniqueKey && c.ColumnName == "additive_offset" {
				line += ` DEFAULT "0.0"`
			}
		} else if c.DefaultValue != "" {
			line += fmt.Sprintf(" DEFAULT %q", c.DefaultValue)
		}
		bodyLines = append(bodyLines, line)
	}

	// Collect inverted index columns in deterministic order without duplicates.
	seenInv := make(map[string]bool)
	var invCols []string
	for _, c := range orderedCols {
		if c.InvertedIndex && !seenInv[c.ColumnName] {
			seenInv[c.ColumnName] = true
			invCols = append(invCols, c.ColumnName)
		}
	}
	for _, colName := range t.InvertedIndexColumns {
		if colName != "" && !seenInv[colName] {
			seenInv[colName] = true
			invCols = append(invCols, colName)
		}
	}
	for _, idxCol := range invCols {
		if colTypes[idxCol] == "TEXT" {
			bodyLines = append(bodyLines, fmt.Sprintf(`    INDEX idx_%s_bm25 (%s) USING INVERTED PROPERTIES ("parser" = "english", "support_phrase" = "true", "lower_case" = "true")`, idxCol, formatDorisDDLColumnIdent(idxCol)))
		} else {
			bodyLines = append(bodyLines, fmt.Sprintf("    INDEX idx_%s_%s (%s) USING INVERTED", t.TableName, idxCol, formatDorisDDLColumnIdent(idxCol)))
		}
	}
	for _, ngCol := range ngramCols {
		bodyLines = append(bodyLines, fmt.Sprintf(`    INDEX idx_ngram_%s_%s (%s) USING NGRAM_BF PROPERTIES ("gram_size" = "3", "bf_size" = "1024")`, t.TableName, ngCol, formatDorisDDLColumnIdent(ngCol)))
	}
	if len(uks) == 0 {
		if embDim, ok := resolveDorisEmbeddingDimension(t); ok {
			for _, c := range orderedCols {
				if c.ColumnName == "embedding" && !c.Nullable && colTypes[c.ColumnName] == "ARRAY<FLOAT>" {
					bodyLines = append(bodyLines, fmt.Sprintf(`    INDEX idx_%s_hnsw (%s) USING ANN PROPERTIES ("index_type" = "hnsw", "metric_type" = "l2_distance", "dim" = "%d")`, c.ColumnName, formatDorisDDLColumnIdent(c.ColumnName), embDim))
				}
			}
		}
	}

	var b strings.Builder
	b.WriteString("CREATE TABLE IF NOT EXISTS ")
	b.WriteString(fullTableName)
	b.WriteString(" (\n")
	b.WriteString(strings.Join(bodyLines, ",\n"))
	b.WriteString("\n)\n")

	if len(uks) > 0 {
		b.WriteString("UNIQUE KEY(")
		b.WriteString(formatDorisDDLColumnIdentList(uks))
		b.WriteString(")\n")
		distCol := t.DistributedByColumn
		if distCol == "" {
			if len(uks) > 1 && uks[0] == "tenant_id" {
				distCol = uks[1]
			} else {
				distCol = uks[0]
			}
		}
		b.WriteString("DISTRIBUTED BY HASH(")
		b.WriteString(formatDorisDDLColumnIdent(distCol))
		b.WriteString(") BUCKETS ")
		b.WriteString(bucketStr)
		b.WriteString("\n")
	} else if len(dupKeys) > 0 {
		b.WriteString("DUPLICATE KEY(")
		b.WriteString(formatDorisDDLColumnIdentList(dupKeys))
		b.WriteString(")\n")
		distCol := t.DistributedByColumn
		if distCol == "" {
			distCol = dupKeys[0]
		}
		b.WriteString("DISTRIBUTED BY HASH(")
		b.WriteString(formatDorisDDLColumnIdent(distCol))
		b.WriteString(") BUCKETS ")
		b.WriteString(bucketStr)
		b.WriteString("\n")
	} else if len(orderedCols) > 0 {
		b.WriteString("DUPLICATE KEY(")
		b.WriteString(formatDorisDDLColumnIdent(orderedCols[0].ColumnName))
		b.WriteString(")\n")
		distCol := t.DistributedByColumn
		if distCol == "" {
			distCol = orderedCols[0].ColumnName
		}
		b.WriteString("DISTRIBUTED BY HASH(")
		b.WriteString(formatDorisDDLColumnIdent(distCol))
		b.WriteString(") BUCKETS ")
		b.WriteString(bucketStr)
		b.WriteString("\n")
	}

	var props []string
	if t.StorageVaultName != "" {
		props = append(props, fmt.Sprintf(`    "storage_vault_name" = %q`, t.StorageVaultName))
	}
	props = append(props, `    "compression" = "zstd"`)
	if len(uks) > 0 {
		props = append(props, `    "enable_unique_key_merge_on_write" = "true"`)
	}

	seqCol := t.SequenceCol
	if seqCol == "" {
		for _, c := range t.Columns {
			if c.IsSequenceCol {
				seqCol = c.ColumnName
				break
			}
		}
	}
	if seqCol != "" {
		props = append(props, fmt.Sprintf(`    "function_column.sequence_col" = %q`, seqCol))
	}
	if t.ColocateGroup != "" {
		props = append(props, fmt.Sprintf(`    "colocate_with" = %q`, t.ColocateGroup))
	}
	if t.FileCacheTTLSeconds > 0 {
		props = append(props, fmt.Sprintf(`    "file_cache_ttl_seconds" = "%d"`, t.FileCacheTTLSeconds))
	} else if t.DatabaseScope == "GLOBAL_TERMINOLOGY" {
		props = append(props, `    "file_cache_ttl_seconds" = "604800"`)
	}
	if t.DatabaseScope == "TENANT" || t.DatabaseScope == "" {
		props = append(props, `    "group_commit_interval_ms" = "200"`)
	}
	if len(bloomCols) > 0 {
		props = append(props, fmt.Sprintf(`    "bloom_filter_columns" = %q`, strings.Join(bloomCols, ", ")))
	}

	b.WriteString("PROPERTIES (\n")
	b.WriteString(strings.Join(props, ",\n"))
	b.WriteString("\n);")
	return b.String()
}

// BuildStarRocksCreateTableSQL renders a deterministic StarRocks 4.x
// CREATE TABLE IF NOT EXISTS statement for this table descriptor.
// Returns an empty string if the table is Iceberg-only or fails identifier validation.
func (t DorisTableDescriptor) BuildStarRocksCreateTableSQL(tenantSlug string, buckets int) string {
	if !validateDescriptorIdentifiers(t) {
		return ""
	}
	if t.SyncTier == "ICEBERG_ONLY" {
		return ""
	}

	var uks []string
	if len(t.UniqueKeyColumns) > 0 {
		uks = dedupDorisColumnList(t.UniqueKeyColumns)
	} else {
		for _, c := range t.Columns {
			if c.IsUniqueKey {
				uks = append(uks, c.ColumnName)
			}
		}
		uks = dedupDorisColumnList(uks)
	}
	dupKeys := dedupDorisColumnList(t.DuplicateKeyColumns)
	ngramCols := dedupDorisColumnList(t.NgramBfColumns)
	bloomCols := dedupDorisColumnList(t.BloomFilterColumns)

	if t.DatabaseScope == "GLOBAL_TERMINOLOGY" && t.SyncTier == "RELEASE_PARTITIONED" &&
		len(uks) == 0 && len(dupKeys) == 0 {
		return ""
	}

	orderedCols := reorderDorisDescriptorColumns(t.Columns, uks, dupKeys, true)
	if len(uks) == 0 && len(dupKeys) == 0 && len(orderedCols) > 0 {
		cp := make([]DorisColumnDescriptor, len(orderedCols))
		copy(cp, orderedCols)
		cp[0].Nullable = false
		orderedCols = cp
	}

	colTypes := make(map[string]string, len(orderedCols))
	for _, c := range orderedCols {
		colTypes[c.ColumnName] = dorisColumnStarRocksDDLType(c.DorisType, c.IcebergType)
	}
	for _, uk := range uks {
		if !isDorisKeyEligibleType(colTypes[uk]) {
			return ""
		}
	}
	for _, dk := range dupKeys {
		if !isDorisKeyEligibleType(colTypes[dk]) {
			return ""
		}
	}
	if len(uks) == 0 && len(dupKeys) == 0 && len(orderedCols) > 0 {
		if !isDorisKeyEligibleType(colTypes[orderedCols[0].ColumnName]) {
			return ""
		}
	}
	if t.DistributedByColumn != "" && !isDorisKeyEligibleType(colTypes[t.DistributedByColumn]) {
		return ""
	}
	for _, dc := range dedupDorisColumnList(t.DistributionColumns) {
		if !isDorisKeyEligibleType(colTypes[dc]) {
			return ""
		}
	}

	slug, ok := sanitizeDorisTenantSlug(tenantSlug)
	if !ok {
		return ""
	}
	bucketStr := "${TENANT_BUCKETS}"
	if buckets > 0 {
		bucketStr = strconv.Itoa(buckets)
	}

	var fullTableName string
	switch t.DatabaseScope {
	case "GLOBAL_TERMINOLOGY":
		fullTableName = "internal.terminology." + t.TableName
		if buckets <= 0 {
			bucketStr = "1"
		}
	case "GLOBAL_TERMINOLOGY_RAG":
		fullTableName = "internal.terminology_rag." + t.TableName
		if buckets <= 0 {
			bucketStr = "8"
		}
	case "GLOBAL_TELEMETRY":
		fullTableName = "internal.telemetry." + t.TableName
		if buckets <= 0 {
			bucketStr = "8"
		}
	default:
		fullTableName = "internal.tenant_" + slug + "." + t.TableName
	}

	var bodyLines []string
	for _, c := range orderedCols {
		colIdent := formatDorisDDLColumnIdent(c.ColumnName)
		srType := colTypes[c.ColumnName]
		line := fmt.Sprintf("    %s %s", colIdent, srType)
		if !c.Nullable {
			line += " NOT NULL"
			if c.DefaultValue != "" {
				line += fmt.Sprintf(" DEFAULT %q", c.DefaultValue)
			} else if strings.HasPrefix(srType, "ARRAY<") && c.ColumnName != "embedding" {
				line += ` DEFAULT "[]"`
			} else if c.ColumnName == "record_status" {
				line += ` DEFAULT "ACTIVE"`
			} else if c.IsSequenceCol {
				line += ` DEFAULT "0"`
			} else if srType == "BOOLEAN" && !c.IsUniqueKey {
				line += ` DEFAULT "false"`
			} else if (srType == "SMALLINT" || srType == "INT") && !c.IsUniqueKey &&
				(strings.HasSuffix(c.ColumnName, "_count") || c.ColumnName == "display_order") {
				line += ` DEFAULT "0"`
			} else if srType == "DOUBLE" && !c.IsUniqueKey && c.ColumnName == "additive_offset" {
				line += ` DEFAULT "0.0"`
			}
		} else if c.DefaultValue != "" {
			line += fmt.Sprintf(" DEFAULT %q", c.DefaultValue)
		}
		bodyLines = append(bodyLines, line)
	}

	seenInv := make(map[string]bool)
	var invCols []string
	for _, c := range orderedCols {
		if c.InvertedIndex && !seenInv[c.ColumnName] {
			seenInv[c.ColumnName] = true
			invCols = append(invCols, c.ColumnName)
		}
	}
	for _, colName := range t.InvertedIndexColumns {
		if colName != "" && !seenInv[colName] {
			seenInv[colName] = true
			invCols = append(invCols, colName)
		}
	}
	for _, idxCol := range invCols {
		if colTypes[idxCol] == "STRING" || colTypes[idxCol] == "TEXT" {
			bodyLines = append(bodyLines, fmt.Sprintf(`    INDEX idx_%s_bm25 (%s) USING GIN("parser" = "english")`, idxCol, formatDorisDDLColumnIdent(idxCol)))
		} else {
			bodyLines = append(bodyLines, fmt.Sprintf("    INDEX idx_%s_%s (%s) USING BITMAP", t.TableName, idxCol, formatDorisDDLColumnIdent(idxCol)))
		}
	}
	for _, ngCol := range ngramCols {
		bodyLines = append(bodyLines, fmt.Sprintf(`    INDEX idx_ngram_%s_%s (%s) USING NGRAMBF ("gram_num" = "3", "bloom_filter_fpp" = "0.05")`, t.TableName, ngCol, formatDorisDDLColumnIdent(ngCol)))
	}
	if len(uks) == 0 {
		if embDim, ok := resolveDorisEmbeddingDimension(t); ok {
			for _, c := range orderedCols {
				if c.ColumnName == "embedding" && !c.Nullable && colTypes[c.ColumnName] == "ARRAY<FLOAT>" {
					bodyLines = append(bodyLines, fmt.Sprintf(`    INDEX idx_%s_hnsw (%s) USING VECTOR ("index_type" = "hnsw", "metric_type" = "l2_distance", "dim" = "%d")`, c.ColumnName, formatDorisDDLColumnIdent(c.ColumnName), embDim))
				}
			}
		}
	}

	var b strings.Builder
	b.WriteString("CREATE TABLE IF NOT EXISTS ")
	b.WriteString(fullTableName)
	b.WriteString(" (\n")
	b.WriteString(strings.Join(bodyLines, ",\n"))
	b.WriteString("\n)\n")

	if len(uks) > 0 {
		b.WriteString("PRIMARY KEY(")
		b.WriteString(formatDorisDDLColumnIdentList(uks))
		b.WriteString(")\n")
		distCol := t.DistributedByColumn
		if distCol == "" && len(t.DistributionColumns) > 0 {
			distCol = t.DistributionColumns[0]
		}
		if distCol == "" {
			if len(uks) > 1 && uks[0] == "tenant_id" {
				distCol = uks[1]
			} else {
				distCol = uks[0]
			}
		}
		b.WriteString("DISTRIBUTED BY HASH(")
		b.WriteString(formatDorisDDLColumnIdent(distCol))
		b.WriteString(") BUCKETS ")
		b.WriteString(bucketStr)
		b.WriteString("\n")
	} else if len(dupKeys) > 0 {
		b.WriteString("DUPLICATE KEY(")
		b.WriteString(formatDorisDDLColumnIdentList(dupKeys))
		b.WriteString(")\n")
		distCol := t.DistributedByColumn
		if distCol == "" && len(t.DistributionColumns) > 0 {
			distCol = t.DistributionColumns[0]
		}
		if distCol == "" {
			distCol = dupKeys[0]
		}
		b.WriteString("DISTRIBUTED BY HASH(")
		b.WriteString(formatDorisDDLColumnIdent(distCol))
		b.WriteString(") BUCKETS ")
		b.WriteString(bucketStr)
		b.WriteString("\n")
	} else if len(orderedCols) > 0 {
		b.WriteString("DUPLICATE KEY(")
		b.WriteString(formatDorisDDLColumnIdent(orderedCols[0].ColumnName))
		b.WriteString(")\n")
		distCol := t.DistributedByColumn
		if distCol == "" && len(t.DistributionColumns) > 0 {
			distCol = t.DistributionColumns[0]
		}
		if distCol == "" {
			distCol = orderedCols[0].ColumnName
		}
		b.WriteString("DISTRIBUTED BY HASH(")
		b.WriteString(formatDorisDDLColumnIdent(distCol))
		b.WriteString(") BUCKETS ")
		b.WriteString(bucketStr)
		b.WriteString("\n")
	}

	var props []string
	if t.StorageVaultName != "" {
		props = append(props, fmt.Sprintf(`    "storage_volume" = %q`, t.StorageVaultName))
		props = append(props, `    "datacache.enable" = "true"`)
	}
	props = append(props, `    "compression" = "zstd"`)
	if len(uks) > 0 || t.EnableMoW {
		props = append(props, `    "enable_persistent_index" = "true"`)
	}

	seqCol := strings.TrimSpace(t.SequenceCol)
	if seqCol == "" {
		seqCol = strings.TrimSpace(t.SequenceColumn)
	}
	if seqCol == "" {
		for _, c := range t.Columns {
			if c.IsSequenceCol {
				seqCol = c.ColumnName
				break
			}
		}
	}
	if seqCol != "" {
		props = append(props, fmt.Sprintf(`    "function_column.sequence_col" = %q`, seqCol))
	}
	if t.ColocateGroup != "" {
		props = append(props, fmt.Sprintf(`    "colocate_with" = %q`, t.ColocateGroup))
	}
	if len(bloomCols) > 0 {
		props = append(props, fmt.Sprintf(`    "bloom_filter_columns" = %q`, strings.Join(bloomCols, ", ")))
	}

	b.WriteString("PROPERTIES (\n")
	b.WriteString(strings.Join(props, ",\n"))
	b.WriteString("\n);")
	return b.String()
}

// BuildIcebergCreateTableSQL renders a deterministic Apache Iceberg
// CREATE TABLE IF NOT EXISTS statement for this table descriptor.
// Returns an empty string if SyncTier == "INTERNAL_ONLY" or identifier validation fails.
func (t DorisTableDescriptor) BuildIcebergCreateTableSQL(tenantSlug string) string {
	if !validateDescriptorIdentifiers(t) {
		return ""
	}
	if t.SyncTier == "INTERNAL_ONLY" {
		return ""
	}
	slug, ok := sanitizeDorisTenantSlug(tenantSlug)
	if !ok {
		return ""
	}

	var uks []string
	if len(t.UniqueKeyColumns) > 0 {
		uks = dedupDorisColumnList(t.UniqueKeyColumns)
	} else {
		for _, c := range t.Columns {
			if c.IsUniqueKey {
				uks = append(uks, c.ColumnName)
			}
		}
		uks = dedupDorisColumnList(uks)
	}
	dupKeys := dedupDorisColumnList(t.DuplicateKeyColumns)
	orderedCols := reorderDorisDescriptorColumns(t.Columns, uks, dupKeys, t.SyncTier != "ICEBERG_ONLY")
	if len(uks) == 0 && len(dupKeys) == 0 && len(orderedCols) > 0 &&
		t.SyncTier != "ICEBERG_ONLY" &&
		(t.DatabaseScope != "GLOBAL_TERMINOLOGY" || t.SyncTier != "RELEASE_PARTITIONED") {
		cp := make([]DorisColumnDescriptor, len(orderedCols))
		copy(cp, orderedCols)
		cp[0].Nullable = false
		orderedCols = cp
	}
	bloomCols := dedupDorisColumnList(t.BloomFilterColumns)

	catalog := t.IcebergCatalog
	if catalog == "" {
		catalog = "iceberg"
	}
	if !isValidSQLIdentifier(catalog) {
		return ""
	}

	var fullTableName string
	switch t.DatabaseScope {
	case "GLOBAL_TERMINOLOGY":
		fullTableName = catalog + ".terminology." + t.TableName
	case "GLOBAL_TELEMETRY":
		fullTableName = catalog + ".telemetry." + t.TableName
	case "GLOBAL_TERMINOLOGY_RAG":
		fullTableName = catalog + ".terminology_rag." + t.TableName
	default:
		fullTableName = catalog + ".tenant_" + slug + "." + t.TableName
	}

	var bodyLines []string
	for _, c := range orderedCols {
		colIdent := formatDorisDDLColumnIdent(c.ColumnName)
		iceType := dorisColumnIcebergDDLType(c.DorisType, c.IcebergType)
		line := fmt.Sprintf("    %s %s", colIdent, iceType)
		if !c.Nullable {
			line += " NOT NULL"
		}
		bodyLines = append(bodyLines, line)
	}

	var partCols []string
	if len(t.PartitionByColumns) > 0 {
		partCols = dedupDorisColumnList(t.PartitionByColumns)
	} else {
		for _, c := range orderedCols {
			if c.IsPartitionKey {
				partCols = append(partCols, c.ColumnName)
			}
		}
		partCols = dedupDorisColumnList(partCols)
	}
	if len(partCols) == 0 && t.SyncTier == "MONTHLY_PARTITIONED" {
		for _, c := range orderedCols {
			if c.ColumnName == "event_month" {
				partCols = append(partCols, "event_month")
				break
			}
		}
	}
	if len(partCols) == 0 && t.SyncTier == "RELEASE_PARTITIONED" {
		for _, c := range orderedCols {
			if c.ColumnName == "release_id" {
				partCols = append(partCols, "release_id")
				break
			}
		}
	}

	var b strings.Builder
	b.WriteString("CREATE TABLE IF NOT EXISTS ")
	b.WriteString(fullTableName)
	b.WriteString(" (\n")
	b.WriteString(strings.Join(bodyLines, ",\n"))
	b.WriteString("\n)\n")

	if len(partCols) > 0 {
		b.WriteString("PARTITION BY LIST (")
		b.WriteString(formatDorisDDLColumnIdentList(partCols))
		b.WriteString(") ()\n")
	}

	var props []string
	props = append(props, `    "compression_codec" = "zstd"`)
	if len(bloomCols) > 0 {
		for _, bf := range bloomCols {
			props = append(props, fmt.Sprintf(`    "write.parquet.bloom-filter-enabled.column.%s" = "true"`, bf))
		}
	} else if (t.DatabaseScope == "TENANT" || t.DatabaseScope == "") && ((len(t.Columns) > 0 && t.Columns[0].ColumnName == "patient_id") || (len(orderedCols) > 0 && orderedCols[0].ColumnName == "patient_id")) {
		props = append(props, `    "write.parquet.bloom-filter-enabled.column.patient_id" = "true"`)
	}

	b.WriteString("PROPERTIES (\n")
	b.WriteString(strings.Join(props, ",\n"))
	b.WriteString("\n);")
	return b.String()
}

type ddlBlock struct {
	goName    string
	tableName string
	sql       string
}

// generateDorisDDLFiles emits <prefix>_doris.sql, <prefix>_starrocks.sql, and <prefix>_iceberg.sql
// for all Doris/StarRocks/Iceberg tables in the proto file.
func generateDorisDDLFiles(gen *protogen.Plugin, file *protogen.File, tables []*dorisTableIR) error {
	var dorisBlocks []ddlBlock
	var icebergBlocks []ddlBlock

	for _, tbl := range tables {
		if sql := tbl.Descriptor.BuildDorisCreateTableSQL("", 0); sql != "" {
			dorisBlocks = append(dorisBlocks, ddlBlock{
				goName:    tbl.GoName,
				tableName: tbl.Descriptor.TableName,
				sql:       sql,
			})
		}
		if sql := tbl.Descriptor.BuildIcebergCreateTableSQL(""); sql != "" {
			icebergBlocks = append(icebergBlocks, ddlBlock{
				goName:    tbl.GoName,
				tableName: tbl.Descriptor.TableName,
				sql:       sql,
			})
		}
	}

	if len(dorisBlocks) > 0 {
		dorisFilename, err := outputFilename(file.GeneratedFilenamePrefix, "_doris.sql")
		if err != nil {
			return err
		}
		dg := gen.NewGeneratedFile(dorisFilename, "")
		dg.P("-- Code generated by proto2type. DO NOT EDIT.")
		dg.P("-- source: ", file.Desc.Path())
		dg.P("-- backend: doris (ddl)")
		dg.P()

		for i, blk := range dorisBlocks {
			if i > 0 {
				dg.P()
			}
			dg.P("-- ", blk.goName, " (", blk.tableName, ")")
			dg.P(blk.sql)
		}
	}

	if err := generateStarRocksDDLFile(gen, file, tables); err != nil {
		return err
	}

	if len(icebergBlocks) > 0 {
		icebergFilename, err := outputFilename(file.GeneratedFilenamePrefix, "_iceberg.sql")
		if err != nil {
			return err
		}
		ig := gen.NewGeneratedFile(icebergFilename, "")
		ig.P("-- Code generated by proto2type. DO NOT EDIT.")
		ig.P("-- source: ", file.Desc.Path())
		ig.P("-- backend: iceberg (ddl)")
		ig.P()

		for i, blk := range icebergBlocks {
			if i > 0 {
				ig.P()
			}
			ig.P("-- ", blk.goName, " (", blk.tableName, ")")
			ig.P(blk.sql)
		}
	}

	return nil
}

// generateStarRocksDDLFile emits <prefix>_starrocks.sql for all StarRocks tables in the proto file.
func generateStarRocksDDLFile(gen *protogen.Plugin, file *protogen.File, tables []*dorisTableIR) error {
	var srBlocks []ddlBlock
	for _, tbl := range tables {
		if sql := tbl.Descriptor.BuildStarRocksCreateTableSQL("", 0); sql != "" {
			srBlocks = append(srBlocks, ddlBlock{
				goName:    tbl.GoName,
				tableName: tbl.Descriptor.TableName,
				sql:       sql,
			})
		}
	}
	if len(srBlocks) == 0 {
		return nil
	}
	srFilename, err := outputFilename(file.GeneratedFilenamePrefix, "_starrocks.sql")
	if err != nil {
		return err
	}
	sg := gen.NewGeneratedFile(srFilename, "")
	sg.P("-- Code generated by proto2type. DO NOT EDIT.")
	sg.P("-- source: ", file.Desc.Path())
	sg.P("-- backend: starrocks (ddl)")
	sg.P()

	for i, blk := range srBlocks {
		if i > 0 {
			sg.P()
		}
		sg.P("-- ", blk.goName, " (", blk.tableName, ")")
		sg.P(blk.sql)
	}
	return nil
}
