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
	case "DATETIMEV2(6)", "DATETIMEV2(3)", "DATETIMEV2", "DATETIME":
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
	if strings.HasPrefix(dt, "VARCHAR") || dt == "TEXT" || dt == "STRING" {
		return "STRING"
	}
	if strings.HasPrefix(dt, "DECIMALV3(") {
		return "DECIMAL(" + strings.TrimPrefix(dt, "DECIMALV3(")
	}
	if strings.HasPrefix(dt, "ARRAY<") && strings.HasSuffix(dt, ">") {
		inner := strings.TrimSuffix(strings.TrimPrefix(dt, "ARRAY<"), ">")
		if strings.HasPrefix(inner, "VARCHAR") || inner == "TEXT" || inner == "STRING" {
			return "LIST<STRING>"
		}
		if inner == "BIGINT" || inner == "LARGEINT" {
			return "LIST<LONG>"
		}
		return "LIST<" + inner + ">"
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
	InvertedIndexColumns []string
	NgramBfColumns       []string
	BloomFilterColumns   []string
	DistributedByColumn  string
	DuplicateKeyColumns  []string
	FileCacheTTLSeconds  int32
	Columns              []DorisColumnDescriptor
}

// dorisColumnIcebergDDLType maps a column's Doris/Iceberg type metadata to the
// SQL column type used in Doris's Iceberg catalog CREATE TABLE DDL.
func dorisColumnIcebergDDLType(dorisType, icebergType string) string {
	dt := strings.TrimSpace(dorisType)
	switch dt {
	case "DATEV2":
		return "DATE"
	case "DATETIMEV2(6)", "DATETIMEV2(3)", "DATETIMEV2":
		return "DATETIME"
	}
	if strings.HasPrefix(dt, "DECIMALV3(") {
		rest := strings.TrimPrefix(dt, "DECIMALV3(")
		rest = strings.ReplaceAll(rest, ", ", ",")
		rest = strings.ReplaceAll(rest, ",", ", ")
		return "DECIMAL(" + rest
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
	return it
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

// validateDescriptorIdentifiers verifies that all table, column, index, and type
// strings in t are safe for SQL DDL emission.
func validateDescriptorIdentifiers(t DorisTableDescriptor) bool {
	if !isValidSQLIdentifier(t.TableName) || !isValidDatabaseScope(t.DatabaseScope) || !isValidSyncTier(t.SyncTier) {
		return false
	}
	if t.ColocateGroup != "" && !isValidSQLIdentifier(t.ColocateGroup) {
		return false
	}
	if t.SequenceCol != "" && !isValidSQLIdentifier(t.SequenceCol) {
		return false
	}
	if t.DistributedByColumn != "" && !isValidSQLIdentifier(t.DistributedByColumn) {
		return false
	}
	for _, list := range [][]string{
		t.UniqueKeyColumns,
		t.PartitionByColumns,
		t.InvertedIndexColumns,
		t.NgramBfColumns,
		t.BloomFilterColumns,
		t.DuplicateKeyColumns,
	} {
		for _, id := range list {
			if id != "" && !isValidSQLIdentifier(id) {
				return false
			}
		}
	}
	for _, c := range t.Columns {
		if !isValidSQLIdentifier(c.ColumnName) {
			return false
		}
		if c.DorisType != "" && !isValidSQLTypeSyntax(c.DorisType) {
			return false
		}
		if c.IcebergType != "" && !isValidSQLTypeSyntax(c.IcebergType) {
			return false
		}
	}
	return true
}

func formatDorisDDLColumnIdent(colName string) string {
	if colName == "system" {
		return "`system`"
	}
	return colName
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

	// Determine UNIQUE KEY columns.
	var uks []string
	if len(t.UniqueKeyColumns) > 0 {
		uks = append(uks, t.UniqueKeyColumns...)
	} else {
		for _, c := range t.Columns {
			if c.IsUniqueKey {
				uks = append(uks, c.ColumnName)
			}
		}
	}

	// Global terminology release-partitioned tables without Doris unique/duplicate keys
	// live exclusively in the Apache Iceberg catalog (azra_lake.terminology.*).
	if t.DatabaseScope == "GLOBAL_TERMINOLOGY" && t.SyncTier == "RELEASE_PARTITIONED" &&
		len(uks) == 0 && len(t.DuplicateKeyColumns) == 0 {
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
	for _, c := range t.Columns {
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
		}
		bodyLines = append(bodyLines, line)
	}

	// Collect inverted index columns in deterministic order without duplicates.
	seenInv := make(map[string]bool)
	var invCols []string
	for _, c := range t.Columns {
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
		bodyLines = append(bodyLines, fmt.Sprintf("    INDEX idx_%s_%s (%s) USING INVERTED", t.TableName, idxCol, idxCol))
	}
	for _, ngCol := range t.NgramBfColumns {
		if ngCol == "" {
			continue
		}
		bodyLines = append(bodyLines, fmt.Sprintf(`    INDEX idx_%s_%s_ngram (%s) USING NGRAM_BF PROPERTIES ("gram_size" = "3", "bf_size" = "1024")`, t.TableName, ngCol, ngCol))
	}

	var b strings.Builder
	b.WriteString("CREATE TABLE IF NOT EXISTS ")
	b.WriteString(fullTableName)
	b.WriteString(" (\n")
	b.WriteString(strings.Join(bodyLines, ",\n"))
	b.WriteString("\n)\n")

	if len(uks) > 0 {
		b.WriteString("UNIQUE KEY(")
		b.WriteString(strings.Join(uks, ", "))
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
		b.WriteString(distCol)
		b.WriteString(") BUCKETS ")
		b.WriteString(bucketStr)
		b.WriteString("\n")
	} else if len(t.DuplicateKeyColumns) > 0 {
		b.WriteString("DUPLICATE KEY(")
		b.WriteString(strings.Join(t.DuplicateKeyColumns, ", "))
		b.WriteString(")\n")
		distCol := t.DistributedByColumn
		if distCol == "" {
			distCol = t.DuplicateKeyColumns[0]
		}
		b.WriteString("DISTRIBUTED BY HASH(")
		b.WriteString(distCol)
		b.WriteString(") BUCKETS ")
		b.WriteString(bucketStr)
		b.WriteString("\n")
	} else if len(t.Columns) > 0 {
		b.WriteString("DUPLICATE KEY(")
		b.WriteString(t.Columns[0].ColumnName)
		b.WriteString(")\n")
		distCol := t.DistributedByColumn
		if distCol == "" {
			distCol = t.Columns[0].ColumnName
		}
		b.WriteString("DISTRIBUTED BY HASH(")
		b.WriteString(distCol)
		b.WriteString(") BUCKETS ")
		b.WriteString(bucketStr)
		b.WriteString("\n")
	}

	var props []string
	props = append(props, `    "storage_vault_name" = "azra_lakehouse_vault"`)
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
	if len(t.BloomFilterColumns) > 0 {
		props = append(props, fmt.Sprintf(`    "bloom_filter_columns" = %q`, strings.Join(t.BloomFilterColumns, ", ")))
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

	var fullTableName string
	switch t.DatabaseScope {
	case "GLOBAL_TERMINOLOGY":
		fullTableName = "azra_lake.terminology." + t.TableName
	case "GLOBAL_TELEMETRY":
		fullTableName = "azra_lake.telemetry." + t.TableName
	case "GLOBAL_TERMINOLOGY_RAG":
		fullTableName = "azra_lake.terminology_rag." + t.TableName
	default:
		fullTableName = "azra_lake.tenant_" + slug + "." + t.TableName
	}

	var bodyLines []string
	for _, c := range t.Columns {
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
		partCols = append(partCols, t.PartitionByColumns...)
	} else {
		for _, c := range t.Columns {
			if c.IsPartitionKey {
				partCols = append(partCols, c.ColumnName)
			}
		}
	}
	if len(partCols) == 0 && t.SyncTier == "MONTHLY_PARTITIONED" {
		for _, c := range t.Columns {
			if c.ColumnName == "event_month" {
				partCols = append(partCols, "event_month")
				break
			}
		}
	}
	if len(partCols) == 0 && t.SyncTier == "RELEASE_PARTITIONED" {
		for _, c := range t.Columns {
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
		b.WriteString(strings.Join(partCols, ", "))
		b.WriteString(") ()\n")
	}

	var props []string
	props = append(props, `    "compression_codec" = "zstd"`)
	if len(t.BloomFilterColumns) > 0 {
		for _, bf := range t.BloomFilterColumns {
			if bf != "" {
				props = append(props, fmt.Sprintf(`    "write.parquet.bloom-filter-enabled.column.%s" = "true"`, bf))
			}
		}
	} else if (t.DatabaseScope == "TENANT" || t.DatabaseScope == "") && len(t.Columns) > 0 && t.Columns[0].ColumnName == "patient_id" {
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

// generateDorisDDLFiles emits <prefix>_doris.sql and <prefix>_iceberg.sql
// for all Doris/Iceberg tables in the proto file.
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
