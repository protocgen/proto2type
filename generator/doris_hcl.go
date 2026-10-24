package generator

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

// dorisTypeToAtlasHCLType converts an Apache Doris SQL column type (e.g.
// "VARCHAR(64)", "DATETIMEV2(6)", "ARRAY<FLOAT>", "DECIMALV3(18, 4)") into its
// canonical lowercase Atlas HCL type expression.
func dorisTypeToAtlasHCLType(dorisType string) string {
	dt := strings.TrimSpace(dorisType)
	if dt == "" {
		return "varchar(256)"
	}
	lower := strings.ToLower(dt)
	lower = strings.ReplaceAll(lower, ", ", ",")
	lower = strings.ReplaceAll(lower, ",", ", ")
	return lower
}

// dorisColumnIcebergHCLType maps a column's Doris/Iceberg type metadata to the
// canonical Atlas HCL column type for an Apache Iceberg table, preserving the
// JSON/JSONB/VARIANT -> string type mapping.
func dorisColumnIcebergHCLType(dorisType, icebergType string) string {
	dt := strings.ToUpper(strings.TrimSpace(dorisType))
	switch dt {
	case "JSON", "JSONB", "VARIANT", "STRING", "TEXT":
		return "string"
	}
	ddlType := dorisColumnIcebergDDLType(dorisType, icebergType)
	lower := strings.ToLower(strings.TrimSpace(ddlType))
	lower = strings.ReplaceAll(lower, ", ", ",")
	lower = strings.ReplaceAll(lower, ",", ", ")
	return lower
}

// normalizeParsedDorisHCLType converts a parsed Atlas HCL column type expression
// back into canonical uppercase Apache Doris SQL type syntax.
func normalizeParsedDorisHCLType(rawType string) string {
	t := strings.TrimSpace(rawType)
	if len(t) >= 2 && t[0] == '"' && t[len(t)-1] == '"' {
		t = strings.TrimSpace(t[1 : len(t)-1])
	}
	upper := strings.ToUpper(t)
	upper = strings.ReplaceAll(upper, ", ", ",")
	upper = strings.ReplaceAll(upper, ",", ", ")
	return upper
}

// resolveDorisColumnDefaultValue determines the effective SQL/HCL default value
// for column c, matching BuildDorisCreateTableSQL default inference rules.
func resolveDorisColumnDefaultValue(c DorisColumnDescriptor) (string, bool) {
	if !c.Nullable {
		upperType := strings.ToUpper(strings.TrimSpace(c.DorisType))
		if c.DefaultValue != "" {
			return c.DefaultValue, true
		}
		if strings.HasPrefix(upperType, "ARRAY<") && c.ColumnName != "embedding" {
			return "[]", true
		}
		if c.ColumnName == "record_status" {
			return "ACTIVE", true
		}
		if c.IsSequenceCol {
			return "0", true
		}
		if upperType == "BOOLEAN" && !c.IsUniqueKey {
			return "false", true
		}
		if (upperType == "SMALLINT" || upperType == "INT") && !c.IsUniqueKey && (strings.HasSuffix(c.ColumnName, "_count") || c.ColumnName == "display_order") {
			return "0", true
		}
		if upperType == "DOUBLE" && !c.IsUniqueKey && c.ColumnName == "additive_offset" {
			return "0.0", true
		}
		return "", false
	}
	if c.DefaultValue != "" {
		return c.DefaultValue, true
	}
	return "", false
}

// formatAtlasHCLColRefList formats a list of column names as Atlas HCL column
// references (e.g. "[column.tenant_id, column.patient_id]").
func formatAtlasHCLColRefList(cols []string) string {
	refs := make([]string, len(cols))
	for i, c := range cols {
		refs[i] = "column." + c
	}
	return "[" + strings.Join(refs, ", ") + "]"
}

// formatAtlasHCLPropKey formats a property key for an Atlas HCL properties map,
// quoting keys that contain dots or hyphens.
func formatAtlasHCLPropKey(k string) string {
	if isValidSQLIdentifier(k) {
		return k
	}
	return fmt.Sprintf("%q", k)
}

// BuildDorisAtlasHCL renders a deterministic Atlas HCL table block for this
// Apache Doris 4.x table descriptor.
//
// If tenantSlug is empty, "${TENANT_SLUG}" is interpolated for TENANT-scoped
// tables. If buckets <= 0, the descriptor's Buckets/AutoBuckets or scope-appropriate
// default ("auto" for TENANT, 1 for GLOBAL_TERMINOLOGY, 8 for RAG/TELEMETRY) is used.
// Returns an empty string if SyncTier == "ICEBERG_ONLY" or identifier validation fails.
func (t DorisTableDescriptor) BuildDorisAtlasHCL(tenantSlug string, buckets int) string {
	if !validateDescriptorIdentifiers(t) || t.SyncTier == "ICEBERG_ONLY" {
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

	if t.DatabaseScope == "GLOBAL_TERMINOLOGY" && t.SyncTier == "RELEASE_PARTITIONED" && len(uks) == 0 && len(dupKeys) == 0 {
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

	var distCols []string
	if len(t.DistributionColumns) > 0 {
		distCols = dedupDorisColumnList(t.DistributionColumns)
	} else if t.DistributedByColumn != "" {
		distCols = []string{t.DistributedByColumn}
	} else if len(uks) > 0 {
		if len(uks) > 1 && uks[0] == "tenant_id" {
			distCols = []string{uks[1]}
		} else {
			distCols = []string{uks[0]}
		}
	} else if len(dupKeys) > 0 {
		distCols = []string{dupKeys[0]}
	} else if len(orderedCols) > 0 {
		distCols = []string{orderedCols[0].ColumnName}
	}
	for _, dc := range distCols {
		if !isDorisKeyEligibleType(colTypes[dc]) {
			return ""
		}
	}

	slug, ok := sanitizeDorisTenantSlug(tenantSlug)
	if !ok {
		return ""
	}

	var schemaRef string
	switch t.DatabaseScope {
	case "GLOBAL_TERMINOLOGY":
		schemaRef = "schema.terminology"
	case "GLOBAL_TERMINOLOGY_RAG":
		schemaRef = "schema.terminology_rag"
	case "GLOBAL_TELEMETRY":
		schemaRef = "schema.telemetry"
	default:
		schemaRef = "schema.tenant_" + slug
	}

	var bucketsExpr string
	if buckets > 0 {
		bucketsExpr = strconv.Itoa(buckets)
	} else if t.Buckets > 0 {
		bucketsExpr = strconv.Itoa(t.Buckets)
	} else if t.AutoBuckets {
		bucketsExpr = `"auto"`
	} else {
		switch t.DatabaseScope {
		case "GLOBAL_TERMINOLOGY":
			bucketsExpr = "1"
		case "GLOBAL_TERMINOLOGY_RAG", "GLOBAL_TELEMETRY":
			bucketsExpr = "8"
		default:
			bucketsExpr = `"auto"`
		}
	}

	seqCol := strings.TrimSpace(t.SequenceCol)
	if seqCol == "" {
		seqCol = strings.TrimSpace(t.SequenceColumn)
	}
	if seqCol == "" {
		for _, c := range orderedCols {
			if c.IsSequenceCol {
				seqCol = c.ColumnName
				break
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "table %q {\n", t.TableName)
	fmt.Fprintf(&b, "  schema = %s\n", schemaRef)

	for _, c := range orderedCols {
		fmt.Fprintf(&b, "  column %q {\n", c.ColumnName)
		fmt.Fprintf(&b, "    type = %s\n", dorisTypeToAtlasHCLType(c.DorisType))
		fmt.Fprintf(&b, "    null = %t\n", c.Nullable)
		if defVal, hasDef := resolveDorisColumnDefaultValue(c); hasDef {
			fmt.Fprintf(&b, "    default = %q\n", defVal)
		}
		if c.Comment != "" {
			fmt.Fprintf(&b, "    comment = %q\n", c.Comment)
		}
		b.WriteString("  }\n")
	}

	if len(uks) > 0 {
		mow := true
		b.WriteString("  unique_key {\n")
		fmt.Fprintf(&b, "    columns = %s\n", formatAtlasHCLColRefList(uks))
		fmt.Fprintf(&b, "    merge_on_write = %t\n", mow)
		if seqCol != "" {
			fmt.Fprintf(&b, "    sequence_col = column.%s\n", seqCol)
		}
		b.WriteString("  }\n")
	} else {
		dkCols := dupKeys
		if len(dkCols) == 0 && len(orderedCols) > 0 {
			dkCols = []string{orderedCols[0].ColumnName}
		}
		if len(dkCols) > 0 {
			b.WriteString("  duplicate_key {\n")
			fmt.Fprintf(&b, "    columns = %s\n", formatAtlasHCLColRefList(dkCols))
			b.WriteString("  }\n")
		}
	}

	if len(distCols) > 0 {
		b.WriteString("  distributed_by_hash {\n")
		fmt.Fprintf(&b, "    columns = %s\n", formatAtlasHCLColRefList(distCols))
		fmt.Fprintf(&b, "    buckets = %s\n", bucketsExpr)
		b.WriteString("  }\n")
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
	if len(partCols) > 0 {
		partBlock := "partition_by_list"
		if strings.HasPrefix(colTypes[partCols[0]], "DATE") {
			partBlock = "partition_by_range"
		}
		fmt.Fprintf(&b, "  %s {\n", partBlock)
		fmt.Fprintf(&b, "    columns = %s\n", formatAtlasHCLColRefList(partCols))
		b.WriteString("  }\n")
	}

	emittedIdx := make(map[string]bool)
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
			idxName := fmt.Sprintf("idx_%s_bm25", idxCol)
			emittedIdx[idxName] = true
			fmt.Fprintf(&b, "  index %q {\n", idxName)
			fmt.Fprintf(&b, "    columns = [column.%s]\n", idxCol)
			b.WriteString("    using = \"INVERTED\"\n")
			b.WriteString("    properties = {\n")
			b.WriteString("      parser = \"english\"\n")
			b.WriteString("      support_phrase = \"true\"\n")
			b.WriteString("      lower_case = \"true\"\n")
			b.WriteString("    }\n")
			b.WriteString("  }\n")
		} else {
			idxName := fmt.Sprintf("idx_%s_%s", t.TableName, idxCol)
			emittedIdx[idxName] = true
			fmt.Fprintf(&b, "  index %q {\n", idxName)
			fmt.Fprintf(&b, "    columns = [column.%s]\n", idxCol)
			b.WriteString("    using = \"INVERTED\"\n")
			b.WriteString("  }\n")
		}
	}
	for _, ngCol := range ngramCols {
		idxName := fmt.Sprintf("idx_ngram_%s_%s", t.TableName, ngCol)
		emittedIdx[idxName] = true
		fmt.Fprintf(&b, "  index %q {\n", idxName)
		fmt.Fprintf(&b, "    columns = [column.%s]\n", ngCol)
		b.WriteString("    using = \"NGRAM_BF\"\n")
		b.WriteString("    properties = {\n")
		b.WriteString("      gram_size = \"3\"\n")
		b.WriteString("      bf_size = \"1024\"\n")
		b.WriteString("    }\n")
		b.WriteString("  }\n")
	}
	if len(uks) == 0 {
		if embDim, hasEmb := resolveDorisEmbeddingDimension(t); hasEmb {
			for _, c := range orderedCols {
				if c.ColumnName == "embedding" && !c.Nullable && colTypes[c.ColumnName] == "ARRAY<FLOAT>" {
					idxName := fmt.Sprintf("idx_%s_hnsw", c.ColumnName)
					emittedIdx[idxName] = true
					fmt.Fprintf(&b, "  index %q {\n", idxName)
					fmt.Fprintf(&b, "    columns = [column.%s]\n", c.ColumnName)
					b.WriteString("    using = \"ANN\"\n")
					b.WriteString("    properties = {\n")
					b.WriteString("      index_type = \"hnsw\"\n")
					b.WriteString("      metric_type = \"l2_distance\"\n")
					fmt.Fprintf(&b, "      dim = \"%d\"\n", embDim)
					b.WriteString("    }\n")
					b.WriteString("  }\n")
				}
			}
		}
	}
	for _, idx := range t.Indexes {
		if idx.Name == "" || emittedIdx[idx.Name] || len(idx.Columns) == 0 {
			continue
		}
		emittedIdx[idx.Name] = true
		using := idx.Using
		if using == "" {
			using = "INVERTED"
		}
		fmt.Fprintf(&b, "  index %q {\n", idx.Name)
		fmt.Fprintf(&b, "    columns = %s\n", formatAtlasHCLColRefList(idx.Columns))
		fmt.Fprintf(&b, "    using = %q\n", using)
		if len(idx.Properties) > 0 {
			keys := make([]string, 0, len(idx.Properties))
			for k := range idx.Properties {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteString("    properties = {\n")
			for _, k := range keys {
				fmt.Fprintf(&b, "      %s = %q\n", formatAtlasHCLPropKey(k), idx.Properties[k])
			}
			b.WriteString("    }\n")
		}
		b.WriteString("  }\n")
	}

	vault := t.StorageVaultName
	if vault == "" {
		vault = "azra_lakehouse_vault"
	}
	b.WriteString("  properties = {\n")
	fmt.Fprintf(&b, "    storage_vault_name = %q\n", vault)
	b.WriteString("    replication_num = \"1\"\n")
	b.WriteString("    compression = \"zstd\"\n")
	if len(uks) > 0 || t.EnableMoW {
		b.WriteString("    enable_unique_key_merge_on_write = \"true\"\n")
	}
	if seqCol != "" {
		fmt.Fprintf(&b, "    \"function_column.sequence_col\" = %q\n", seqCol)
	}
	if t.ColocateGroup != "" {
		fmt.Fprintf(&b, "    colocate_with = %q\n", t.ColocateGroup)
	}
	if t.FileCacheTTLSeconds > 0 {
		fmt.Fprintf(&b, "    file_cache_ttl_seconds = \"%d\"\n", t.FileCacheTTLSeconds)
	} else if t.DatabaseScope == "GLOBAL_TERMINOLOGY" {
		b.WriteString("    file_cache_ttl_seconds = \"604800\"\n")
	}
	if t.DatabaseScope == "TENANT" || t.DatabaseScope == "" {
		b.WriteString("    group_commit_interval_ms = \"200\"\n")
	}
	if len(bloomCols) > 0 {
		fmt.Fprintf(&b, "    bloom_filter_columns = %q\n", strings.Join(bloomCols, ", "))
	}
	b.WriteString("  }\n")
	b.WriteString("}")
	return b.String()
}

// BuildIcebergAtlasHCL renders a deterministic Atlas HCL table block for this
// Apache Iceberg table descriptor.
//
// Returns an empty string if SyncTier == "INTERNAL_ONLY" or identifier validation fails.
func (t DorisTableDescriptor) BuildIcebergAtlasHCL(tenantSlug string) string {
	if !validateDescriptorIdentifiers(t) || t.SyncTier == "INTERNAL_ONLY" {
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
	if len(uks) == 0 && len(dupKeys) == 0 && len(orderedCols) > 0 && t.SyncTier != "ICEBERG_ONLY" && (t.DatabaseScope != "GLOBAL_TERMINOLOGY" || t.SyncTier != "RELEASE_PARTITIONED") {
		cp := make([]DorisColumnDescriptor, len(orderedCols))
		copy(cp, orderedCols)
		cp[0].Nullable = false
		orderedCols = cp
	}
	bloomCols := dedupDorisColumnList(t.BloomFilterColumns)

	catalog := t.IcebergCatalog
	if catalog == "" {
		catalog = "azra_lake"
	}
	if !isValidSQLIdentifier(catalog) {
		return ""
	}

	var schemaRef string
	switch t.DatabaseScope {
	case "GLOBAL_TERMINOLOGY":
		schemaRef = "schema.terminology"
	case "GLOBAL_TELEMETRY":
		schemaRef = "schema.telemetry"
	case "GLOBAL_TERMINOLOGY_RAG":
		schemaRef = "schema.terminology_rag"
	default:
		schemaRef = "schema.tenant_" + slug
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
	partSpec := strings.TrimSpace(t.IcebergPartitionSpec)
	if partSpec == "" && len(partCols) > 0 {
		partSpec = strings.Join(partCols, ", ")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "table %q {\n", t.TableName)
	fmt.Fprintf(&b, "  catalog = %q\n", catalog)
	fmt.Fprintf(&b, "  schema = %s\n", schemaRef)

	for _, c := range orderedCols {
		fmt.Fprintf(&b, "  column %q {\n", c.ColumnName)
		fmt.Fprintf(&b, "    type = %s\n", dorisColumnIcebergHCLType(c.DorisType, c.IcebergType))
		fmt.Fprintf(&b, "    null = %t\n", c.Nullable)
		if c.Comment != "" {
			fmt.Fprintf(&b, "    comment = %q\n", c.Comment)
		}
		b.WriteString("  }\n")
	}

	if partSpec != "" {
		b.WriteString("  partition_by {\n")
		fmt.Fprintf(&b, "    spec = %q\n", partSpec)
		b.WriteString("  }\n")
	}

	b.WriteString("  properties = {\n")
	b.WriteString("    engine = \"iceberg\"\n")
	b.WriteString("    \"write.format.default\" = \"parquet\"\n")
	b.WriteString("    \"write.parquet.compression-codec\" = \"zstd\"\n")
	if len(bloomCols) > 0 {
		for _, bf := range bloomCols {
			fmt.Fprintf(&b, "    \"write.parquet.bloom-filter-enabled.column.%s\" = \"true\"\n", bf)
		}
	} else if (t.DatabaseScope == "TENANT" || t.DatabaseScope == "") && ((len(t.Columns) > 0 && t.Columns[0].ColumnName == "patient_id") || (len(orderedCols) > 0 && orderedCols[0].ColumnName == "patient_id")) {
		b.WriteString("    \"write.parquet.bloom-filter-enabled.column.patient_id\" = \"true\"\n")
	}
	b.WriteString("  }\n")
	b.WriteString("}")
	return b.String()
}

// stripAtlasHCLComments removes #, //, and /* ... */ comments outside of double-quoted strings.
func stripAtlasHCLComments(src string) (string, error) {
	var b strings.Builder
	b.Grow(len(src))
	inQuotes := false
	for i := 0; i < len(src); i++ {
		ch := src[i]
		if inQuotes {
			b.WriteByte(ch)
			if ch == '\\' && i+1 < len(src) {
				i++
				b.WriteByte(src[i])
				continue
			}
			if ch == '"' {
				inQuotes = false
			}
			continue
		}
		if ch == '"' {
			inQuotes = true
			b.WriteByte(ch)
			continue
		}
		if ch == '#' || (ch == '/' && i+1 < len(src) && src[i+1] == '/') {
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				b.WriteByte('\n')
			}
			continue
		}
		if ch == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			closed := false
			for i+1 < len(src) {
				if src[i] == '*' && src[i+1] == '/' {
					i++
					closed = true
					break
				}
				if src[i] == '\n' {
					b.WriteByte('\n')
				}
				i++
			}
			if !closed {
				return "", fmt.Errorf("unterminated block comment in Atlas HCL")
			}
			continue
		}
		b.WriteByte(ch)
	}
	if inQuotes {
		return "", fmt.Errorf("unterminated string literal in Atlas HCL")
	}
	return b.String(), nil
}

// findMatchingHCLBrace returns the index of the closing '}' matching the opening '{' at openIdx.
func findMatchingHCLBrace(s string, openIdx int) (int, error) {
	depth := 0
	inQuotes := false
	for i := openIdx; i < len(s); i++ {
		ch := s[i]
		if inQuotes {
			if ch == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if ch == '"' {
				inQuotes = false
			}
			continue
		}
		switch ch {
		case '"':
			inQuotes = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return -1, fmt.Errorf("unbalanced '{' in Atlas HCL")
}

// unquoteHCLString unquotes a double-quoted HCL string literal or trims whitespace.
func unquoteHCLString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if unq, err := strconv.Unquote(s); err == nil {
			return unq
		}
		return s[1 : len(s)-1]
	}
	return s
}

// parseHCLColumnRefList parses "[column.a, column.b]" or `["a", "b"]` into ["a", "b"].
func parseHCLColumnRefList(raw string) ([]string, error) {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("expected bracketed column list, got %q", raw)
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return nil, nil
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		item := unquoteHCLString(p)
		item = strings.TrimPrefix(item, "column.")
		item = strings.TrimSpace(item)
		if item == "" || !isValidSQLIdentifier(item) {
			return nil, fmt.Errorf("invalid column reference %q in %q", p, raw)
		}
		out = append(out, item)
	}
	return out, nil
}

// parseHCLKeyValueAssignments parses top-level key = value lines inside a flat HCL block body.
func parseHCLKeyValueAssignments(body string) (map[string]string, error) {
	res := make(map[string]string)
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		eqIdx := strings.IndexByte(line, '=')
		if eqIdx <= 0 {
			return nil, fmt.Errorf("expected key = value assignment, got %q", line)
		}
		k := unquoteHCLString(line[:eqIdx])
		v := strings.TrimSpace(line[eqIdx+1:])
		if k == "" || v == "" {
			return nil, fmt.Errorf("invalid assignment %q", line)
		}
		res[k] = v
	}
	return res, nil
}

// ParseDorisAtlasHCL parses one or more Atlas HCL `table "<name>"` blocks into
// []DorisTableDescriptor values ready for schema inspection and evolution linting
// via LintDorisEvolution / LintDorisSchemaEvolution.
func ParseDorisAtlasHCL(hclContent string) ([]DorisTableDescriptor, error) {
	cleaned, err := stripAtlasHCLComments(hclContent)
	if err != nil {
		return nil, err
	}

	var tables []DorisTableDescriptor
	pos := 0
	for pos < len(cleaned) {
		for pos < len(cleaned) && (cleaned[pos] == ' ' || cleaned[pos] == '\t' || cleaned[pos] == '\n' || cleaned[pos] == '\r') {
			pos++
		}
		if pos >= len(cleaned) {
			break
		}

		openIdx := strings.IndexByte(cleaned[pos:], '{')
		if openIdx < 0 {
			return nil, fmt.Errorf("unexpected trailing content in Atlas HCL: %q", strings.TrimSpace(cleaned[pos:]))
		}
		openAbs := pos + openIdx
		header := strings.TrimSpace(cleaned[pos:openAbs])
		closeAbs, err := findMatchingHCLBrace(cleaned, openAbs)
		if err != nil {
			return nil, err
		}
		body := cleaned[openAbs+1 : closeAbs]
		pos = closeAbs + 1

		// Ignore top-level `schema "<name>" { ... }` declarations if present.
		if strings.HasPrefix(header, "schema ") || header == "schema" {
			continue
		}
		if !strings.HasPrefix(header, "table ") {
			return nil, fmt.Errorf("unexpected top-level HCL block %q (expected table \"<name>\")", header)
		}
		tableName := unquoteHCLString(strings.TrimSpace(strings.TrimPrefix(header, "table")))
		if !isValidSQLIdentifier(tableName) {
			return nil, fmt.Errorf("invalid table name %q in Atlas HCL", tableName)
		}

		desc, err := parseDorisAtlasTableBlock(tableName, body)
		if err != nil {
			return nil, fmt.Errorf("table %q: %w", tableName, err)
		}
		tables = append(tables, desc)
	}

	if len(tables) == 0 {
		return nil, fmt.Errorf("no table blocks found in Atlas HCL")
	}
	return tables, nil
}

// parseDorisAtlasTableBlock parses the body of a single `table "<name>" { ... }` block.
func parseDorisAtlasTableBlock(tableName, body string) (DorisTableDescriptor, error) {
	desc := DorisTableDescriptor{
		TableName: tableName,
	}

	colOrder := make([]string, 0)
	colMap := make(map[string]DorisColumnDescriptor)

	i := 0
	for i < len(body) {
		for i < len(body) && (body[i] == ' ' || body[i] == '\t' || body[i] == '\n' || body[i] == '\r') {
			i++
		}
		if i >= len(body) {
			break
		}

		// Determine whether the next item is a single-line assignment (key = val)
		// or a block / map assignment containing '{'.
		lineEnd := strings.IndexByte(body[i:], '\n')
		var line string
		if lineEnd < 0 {
			line = body[i:]
		} else {
			line = body[i : i+lineEnd]
		}
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" {
			if lineEnd < 0 {
				break
			}
			i += lineEnd + 1
			continue
		}

		if !strings.Contains(trimmedLine, "{") {
			eqIdx := strings.IndexByte(trimmedLine, '=')
			if eqIdx <= 0 {
				return desc, fmt.Errorf("unexpected statement %q", trimmedLine)
			}
			k := unquoteHCLString(trimmedLine[:eqIdx])
			v := strings.TrimSpace(trimmedLine[eqIdx+1:])
			switch k {
			case "schema":
				rawSchema := unquoteHCLString(v)
				rawSchema = strings.TrimPrefix(rawSchema, "schema.")
				switch {
				case rawSchema == "terminology":
					desc.DatabaseScope = "GLOBAL_TERMINOLOGY"
				case rawSchema == "terminology_rag":
					desc.DatabaseScope = "GLOBAL_TERMINOLOGY_RAG"
				case rawSchema == "telemetry":
					desc.DatabaseScope = "GLOBAL_TELEMETRY"
				case strings.HasPrefix(rawSchema, "tenant_"):
					desc.DatabaseScope = "TENANT"
				default:
					desc.DatabaseScope = "TENANT"
				}
			case "catalog":
				desc.IcebergCatalog = unquoteHCLString(v)
			}
			if lineEnd < 0 {
				break
			}
			i += lineEnd + 1
			continue
		}

		openRel := strings.IndexByte(body[i:], '{')
		openAbs := i + openRel
		blockHeader := strings.TrimSpace(body[i:openAbs])
		closeAbs, err := findMatchingHCLBrace(body, openAbs)
		if err != nil {
			return desc, err
		}
		blockBody := body[openAbs+1 : closeAbs]
		i = closeAbs + 1

		if err := applyDorisAtlasSubBlock(&desc, blockHeader, blockBody, &colOrder, colMap); err != nil {
			return desc, err
		}
	}

	ukSet := make(map[string]bool, len(desc.UniqueKeyColumns))
	for _, uk := range desc.UniqueKeyColumns {
		ukSet[uk] = true
	}
	partSet := make(map[string]bool, len(desc.PartitionByColumns))
	for _, pk := range desc.PartitionByColumns {
		partSet[pk] = true
	}
	invSet := make(map[string]bool, len(desc.InvertedIndexColumns))
	for _, ic := range desc.InvertedIndexColumns {
		invSet[ic] = true
	}

	cols := make([]DorisColumnDescriptor, 0, len(colOrder))
	for _, name := range colOrder {
		c := colMap[name]
		if ukSet[name] {
			c.IsUniqueKey = true
		}
		if desc.SequenceCol != "" && name == desc.SequenceCol {
			c.IsSequenceCol = true
		}
		if partSet[name] {
			c.IsPartitionKey = true
		}
		if invSet[name] {
			c.InvertedIndex = true
		}
		cols = append(cols, c)
	}
	desc.Columns = cols

	if desc.SyncTier == "" {
		switch {
		case len(desc.PartitionByColumns) == 1 && desc.PartitionByColumns[0] == "event_month":
			desc.SyncTier = "MONTHLY_PARTITIONED"
		case len(desc.PartitionByColumns) == 1 && desc.PartitionByColumns[0] == "release_id":
			desc.SyncTier = "RELEASE_PARTITIONED"
		case desc.IcebergCatalog != "" && len(desc.UniqueKeyColumns) == 0 && len(desc.DuplicateKeyColumns) == 0:
			desc.SyncTier = "ICEBERG_ONLY"
		default:
			desc.SyncTier = "WHOLE_TABLE"
		}
	}

	return desc, nil
}

// applyDorisAtlasSubBlock parses a nested block inside `table "<name>" { ... }`
// and updates desc and the column map accordingly.
func applyDorisAtlasSubBlock(
	desc *DorisTableDescriptor,
	blockHeader string,
	blockBody string,
	colOrder *[]string,
	colMap map[string]DorisColumnDescriptor,
) error {
	header := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(blockHeader), "="))
	switch {
	case strings.HasPrefix(header, "column "):
		colName := unquoteHCLString(strings.TrimSpace(strings.TrimPrefix(header, "column")))
		if !isValidSQLIdentifier(colName) {
			return fmt.Errorf("invalid column name %q", colName)
		}
		if _, exists := colMap[colName]; exists {
			return fmt.Errorf("duplicate column %q", colName)
		}
		kv, err := parseHCLKeyValueAssignments(blockBody)
		if err != nil {
			return fmt.Errorf("column %q: %w", colName, err)
		}
		rawType, ok := kv["type"]
		if !ok || rawType == "" {
			return fmt.Errorf("column %q missing type attribute", colName)
		}
		dorisType := normalizeParsedDorisHCLType(rawType)
		nullable := false
		if nullStr, hasNull := kv["null"]; hasNull {
			parsedNull, bErr := strconv.ParseBool(unquoteHCLString(nullStr))
			if bErr != nil {
				return fmt.Errorf("column %q invalid null attribute %q", colName, nullStr)
			}
			nullable = parsedNull
		}
		defVal := ""
		if rawDef, hasDef := kv["default"]; hasDef {
			defVal = unquoteHCLString(rawDef)
		}
		comment := ""
		if rawComment, hasComment := kv["comment"]; hasComment {
			comment = unquoteHCLString(rawComment)
		}
		colDesc := DorisColumnDescriptor{
			ColumnName:     colName,
			ProtoFieldName: colName,
			DorisType:      dorisType,
			Nullable:       nullable,
			DefaultValue:   defVal,
			Comment:        comment,
		}
		colDesc.IcebergType = colDesc.CanonicalIcebergSpecType()
		*colOrder = append(*colOrder, colName)
		colMap[colName] = colDesc
		return nil

	case header == "unique_key":
		kv, err := parseHCLKeyValueAssignments(blockBody)
		if err != nil {
			return fmt.Errorf("unique_key: %w", err)
		}
		colsRaw, ok := kv["columns"]
		if !ok {
			return fmt.Errorf("unique_key missing columns attribute")
		}
		uks, err := parseHCLColumnRefList(colsRaw)
		if err != nil {
			return fmt.Errorf("unique_key: %w", err)
		}
		desc.UniqueKeyColumns = uks
		if mowStr, hasMoW := kv["merge_on_write"]; hasMoW {
			mow, bErr := strconv.ParseBool(unquoteHCLString(mowStr))
			if bErr != nil {
				return fmt.Errorf("unique_key invalid merge_on_write %q", mowStr)
			}
			desc.EnableMoW = mow
		}
		if seqRaw, hasSeq := kv["sequence_col"]; hasSeq {
			seqCol := strings.TrimPrefix(unquoteHCLString(seqRaw), "column.")
			if !isValidSQLIdentifier(seqCol) {
				return fmt.Errorf("unique_key invalid sequence_col %q", seqRaw)
			}
			desc.SequenceCol = seqCol
			desc.SequenceColumn = seqCol
		}
		return nil

	case header == "duplicate_key":
		kv, err := parseHCLKeyValueAssignments(blockBody)
		if err != nil {
			return fmt.Errorf("duplicate_key: %w", err)
		}
		colsRaw, ok := kv["columns"]
		if !ok {
			return fmt.Errorf("duplicate_key missing columns attribute")
		}
		dks, err := parseHCLColumnRefList(colsRaw)
		if err != nil {
			return fmt.Errorf("duplicate_key: %w", err)
		}
		desc.DuplicateKeyColumns = dks
		return nil

	case header == "distributed_by_hash":
		kv, err := parseHCLKeyValueAssignments(blockBody)
		if err != nil {
			return fmt.Errorf("distributed_by_hash: %w", err)
		}
		if colsRaw, ok := kv["columns"]; ok {
			dCols, cErr := parseHCLColumnRefList(colsRaw)
			if cErr != nil {
				return fmt.Errorf("distributed_by_hash: %w", cErr)
			}
			desc.DistributionColumns = dCols
			if len(dCols) > 0 {
				desc.DistributedByColumn = dCols[0]
			}
		}
		if bRaw, ok := kv["buckets"]; ok {
			bVal := unquoteHCLString(bRaw)
			if strings.EqualFold(bVal, "auto") {
				desc.AutoBuckets = true
				desc.Buckets = 0
			} else {
				n, nErr := strconv.Atoi(bVal)
				if nErr != nil || n <= 0 {
					return fmt.Errorf("distributed_by_hash invalid buckets %q", bRaw)
				}
				desc.Buckets = n
				desc.AutoBuckets = false
			}
		}
		return nil

	case header == "partition_by_range" || header == "partition_by_list":
		kv, err := parseHCLKeyValueAssignments(blockBody)
		if err != nil {
			return fmt.Errorf("%s: %w", header, err)
		}
		if colsRaw, ok := kv["columns"]; ok {
			pCols, cErr := parseHCLColumnRefList(colsRaw)
			if cErr != nil {
				return fmt.Errorf("%s: %w", header, cErr)
			}
			desc.PartitionByColumns = pCols
		}
		return nil

	case header == "partition_by":
		kv, err := parseHCLKeyValueAssignments(blockBody)
		if err != nil {
			return fmt.Errorf("partition_by: %w", err)
		}
		if specRaw, ok := kv["spec"]; ok {
			desc.IcebergPartitionSpec = unquoteHCLString(specRaw)
		}
		return nil

	case strings.HasPrefix(header, "index "):
		idxName := unquoteHCLString(strings.TrimSpace(strings.TrimPrefix(header, "index")))
		if !isValidSQLIdentifier(idxName) {
			return fmt.Errorf("invalid index name %q", idxName)
		}
		idxDesc, err := parseDorisAtlasIndexBlock(idxName, blockBody)
		if err != nil {
			return err
		}
		desc.Indexes = append(desc.Indexes, idxDesc)
		switch strings.ToUpper(idxDesc.Using) {
		case "INVERTED":
			desc.InvertedIndexColumns = dedupDorisColumnList(append(desc.InvertedIndexColumns, idxDesc.Columns...))
		case "NGRAM_BF":
			desc.NgramBfColumns = dedupDorisColumnList(append(desc.NgramBfColumns, idxDesc.Columns...))
		case "ANN":
			if dimStr, hasDim := idxDesc.Properties["dim"]; hasDim {
				dim64, dErr := strconv.ParseInt(dimStr, 10, 32)
				if dErr == nil && dim64 > 0 && dim64 <= math.MaxInt32 {
					desc.EmbeddingDimension = int32(dim64)
				}
			}
		}
		return nil

	case header == "properties":
		kv, err := parseHCLKeyValueAssignments(blockBody)
		if err != nil {
			return fmt.Errorf("properties: %w", err)
		}
		for k, rawV := range kv {
			v := unquoteHCLString(rawV)
			switch k {
			case "storage_vault_name":
				desc.StorageVaultName = v
			case "colocate_with":
				desc.ColocateGroup = v
			case "enable_unique_key_merge_on_write":
				if strings.EqualFold(v, "true") {
					desc.EnableMoW = true
				}
			case "function_column.sequence_col":
				if desc.SequenceCol == "" {
					desc.SequenceCol = v
					desc.SequenceColumn = v
				}
			case "file_cache_ttl_seconds":
				if ttl64, tErr := strconv.ParseInt(v, 10, 32); tErr == nil && ttl64 > 0 && ttl64 <= math.MaxInt32 {
					desc.FileCacheTTLSeconds = int32(ttl64)
				}
			case "bloom_filter_columns":
				var bfs []string
				for _, part := range strings.Split(v, ",") {
					if trimmed := strings.TrimSpace(part); trimmed != "" {
						bfs = append(bfs, trimmed)
					}
				}
				desc.BloomFilterColumns = dedupDorisColumnList(bfs)
			case "sync_tier":
				desc.SyncTier = normalizeSyncTier(v)
			}
		}
		return nil

	default:
		return fmt.Errorf("unsupported HCL block %q", blockHeader)
	}
}

// parseDorisAtlasIndexBlock parses an `index "<name>" { ... }` block body.
func parseDorisAtlasIndexBlock(idxName, body string) (DorisIndexDescriptor, error) {
	idx := DorisIndexDescriptor{
		Name:       idxName,
		Properties: make(map[string]string),
	}
	var outerLines []string
	i := 0
	for i < len(body) {
		for i < len(body) && (body[i] == ' ' || body[i] == '\t' || body[i] == '\n' || body[i] == '\r') {
			i++
		}
		if i >= len(body) {
			break
		}
		lineEnd := strings.IndexByte(body[i:], '\n')
		var line string
		if lineEnd < 0 {
			line = body[i:]
		} else {
			line = body[i : i+lineEnd]
		}
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "{") {
			openRel := strings.IndexByte(body[i:], '{')
			openAbs := i + openRel
			subHeader := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body[i:openAbs]), "="))
			closeAbs, err := findMatchingHCLBrace(body, openAbs)
			if err != nil {
				return idx, fmt.Errorf("index %q: %w", idxName, err)
			}
			subBody := body[openAbs+1 : closeAbs]
			i = closeAbs + 1
			if subHeader != "properties" {
				return idx, fmt.Errorf("index %q: unsupported sub-block %q", idxName, subHeader)
			}
			propsKV, err := parseHCLKeyValueAssignments(subBody)
			if err != nil {
				return idx, fmt.Errorf("index %q properties: %w", idxName, err)
			}
			for k, v := range propsKV {
				idx.Properties[k] = unquoteHCLString(v)
			}
			continue
		}
		outerLines = append(outerLines, trimmed)
		if lineEnd < 0 {
			break
		}
		i += lineEnd + 1
	}

	kv, err := parseHCLKeyValueAssignments(strings.Join(outerLines, "\n"))
	if err != nil {
		return idx, fmt.Errorf("index %q: %w", idxName, err)
	}
	if colsRaw, ok := kv["columns"]; ok {
		cols, cErr := parseHCLColumnRefList(colsRaw)
		if cErr != nil {
			return idx, fmt.Errorf("index %q: %w", idxName, cErr)
		}
		idx.Columns = cols
	}
	if usingRaw, ok := kv["using"]; ok {
		idx.Using = strings.ToUpper(unquoteHCLString(usingRaw))
	}
	return idx, nil
}

// generateDorisHCLFiles emits <prefix>_doris.hcl and <prefix>_iceberg.hcl
// for all Doris/Iceberg tables in the proto file when doris_mode is "hcl" or "all".
func generateDorisHCLFiles(gen *protogen.Plugin, file *protogen.File, tables []*dorisTableIR) error {
	var dorisBlocks []ddlBlock
	var icebergBlocks []ddlBlock

	for _, tbl := range tables {
		if hcl := tbl.Descriptor.BuildDorisAtlasHCL("", 0); hcl != "" {
			dorisBlocks = append(dorisBlocks, ddlBlock{
				goName:    tbl.GoName,
				tableName: tbl.Descriptor.TableName,
				sql:       hcl,
			})
		}
		if hcl := tbl.Descriptor.BuildIcebergAtlasHCL(""); hcl != "" {
			icebergBlocks = append(icebergBlocks, ddlBlock{
				goName:    tbl.GoName,
				tableName: tbl.Descriptor.TableName,
				sql:       hcl,
			})
		}
	}

	if len(dorisBlocks) > 0 {
		dorisFilename, err := outputFilename(file.GeneratedFilenamePrefix, "_doris.hcl")
		if err != nil {
			return err
		}
		dg := gen.NewGeneratedFile(dorisFilename, "")
		dg.P("# Code generated by proto2type. DO NOT EDIT.")
		dg.P("# source: ", file.Desc.Path())
		dg.P("# backend: doris (hcl)")
		dg.P()

		for i, blk := range dorisBlocks {
			if i > 0 {
				dg.P()
			}
			dg.P("# ", blk.goName, " (", blk.tableName, ")")
			dg.P(blk.sql)
		}
	}

	if len(icebergBlocks) > 0 {
		icebergFilename, err := outputFilename(file.GeneratedFilenamePrefix, "_iceberg.hcl")
		if err != nil {
			return err
		}
		ig := gen.NewGeneratedFile(icebergFilename, "")
		ig.P("# Code generated by proto2type. DO NOT EDIT.")
		ig.P("# source: ", file.Desc.Path())
		ig.P("# backend: iceberg (hcl)")
		ig.P()

		for i, blk := range icebergBlocks {
			if i > 0 {
				ig.P()
			}
			ig.P("# ", blk.goName, " (", blk.tableName, ")")
			ig.P(blk.sql)
		}
	}

	return nil
}

// emitDorisAtlasHCLRuntimeHelpers emits BuildDorisAtlasHCL, BuildIcebergAtlasHCL,
// and ParseDorisAtlasHCL into a generated *_doris.type.go file.
func emitDorisAtlasHCLRuntimeHelpers(g *protogen.GeneratedFile) {
	fmtErrorf := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "fmt", GoName: "Errorf"})
	fmtSprintf := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "fmt", GoName: "Sprintf"})
	mathMaxInt32 := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "math", GoName: "MaxInt32"})
	sortStrings := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "sort", GoName: "Strings"})
	strconvAtoi := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strconv", GoName: "Atoi"})
	strconvItoa := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strconv", GoName: "Itoa"})
	strconvParseBool := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strconv", GoName: "ParseBool"})
	strconvParseInt := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strconv", GoName: "ParseInt"})
	strconvUnquote := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strconv", GoName: "Unquote"})
	stringsBuilder := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "Builder"})
	stringsContains := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "Contains"})
	stringsEqualFold := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "EqualFold"})
	stringsHasPrefix := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "HasPrefix"})
	stringsHasSuffix := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "HasSuffix"})
	stringsIndexByte := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "IndexByte"})
	stringsJoin := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "Join"})
	stringsReplaceAll := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "ReplaceAll"})
	stringsSplit := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "Split"})
	stringsToLower := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "ToLower"})
	stringsToUpper := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "ToUpper"})
	stringsTrimPrefix := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "TrimPrefix"})
	stringsTrimSpace := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "TrimSpace"})
	stringsTrimSuffix := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "TrimSuffix"})

	g.P("func dorisTypeToAtlasHCLType(dorisType string) string {")
	g.P("\tdt := ", stringsTrimSpace, "(dorisType)")
	g.P("\tif dt == \"\" {")
	g.P("\t\treturn \"varchar(256)\"")
	g.P("\t}")
	g.P("\tlower := ", stringsToLower, "(dt)")
	g.P("\tlower = ", stringsReplaceAll, "(lower, \", \", \",\")")
	g.P("\tlower = ", stringsReplaceAll, "(lower, \",\", \", \")")
	g.P("\treturn lower")
	g.P("}")
	g.P()

	g.P("func dorisColumnIcebergHCLType(dorisType, icebergType string) string {")
	g.P("\tdt := ", stringsToUpper, "(", stringsTrimSpace, "(dorisType))")
	g.P("\tswitch dt {")
	g.P("\tcase \"JSON\", \"JSONB\", \"VARIANT\", \"STRING\", \"TEXT\":")
	g.P("\t\treturn \"string\"")
	g.P("\t}")
	g.P("\tddlType := dorisColumnIcebergDDLType(dorisType, icebergType)")
	g.P("\tlower := ", stringsToLower, "(", stringsTrimSpace, "(ddlType))")
	g.P("\tlower = ", stringsReplaceAll, "(lower, \", \", \",\")")
	g.P("\tlower = ", stringsReplaceAll, "(lower, \",\", \", \")")
	g.P("\treturn lower")
	g.P("}")
	g.P()

	g.P("func resolveDorisColumnDefaultValue(c DorisColumnDescriptor) (string, bool) {")
	g.P("\tif !c.Nullable {")
	g.P("\t\tupperType := ", stringsToUpper, "(", stringsTrimSpace, "(c.DorisType))")
	g.P("\t\tif c.DefaultValue != \"\" {")
	g.P("\t\t\treturn c.DefaultValue, true")
	g.P("\t\t}")
	g.P("\t\tif ", stringsHasPrefix, "(upperType, \"ARRAY<\") && c.ColumnName != \"embedding\" {")
	g.P("\t\t\treturn \"[]\", true")
	g.P("\t\t}")
	g.P("\t\tif c.ColumnName == \"record_status\" {")
	g.P("\t\t\treturn \"ACTIVE\", true")
	g.P("\t\t}")
	g.P("\t\tif c.IsSequenceCol {")
	g.P("\t\t\treturn \"0\", true")
	g.P("\t\t}")
	g.P("\t\tif upperType == \"BOOLEAN\" && !c.IsUniqueKey {")
	g.P("\t\t\treturn \"false\", true")
	g.P("\t\t}")
	g.P("\t\tif (upperType == \"SMALLINT\" || upperType == \"INT\") && !c.IsUniqueKey && (", stringsHasSuffix, "(c.ColumnName, \"_count\") || c.ColumnName == \"display_order\") {")
	g.P("\t\t\treturn \"0\", true")
	g.P("\t\t}")
	g.P("\t\tif upperType == \"DOUBLE\" && !c.IsUniqueKey && c.ColumnName == \"additive_offset\" {")
	g.P("\t\t\treturn \"0.0\", true")
	g.P("\t\t}")
	g.P("\t\treturn \"\", false")
	g.P("\t}")
	g.P("\tif c.DefaultValue != \"\" {")
	g.P("\t\treturn c.DefaultValue, true")
	g.P("\t}")
	g.P("\treturn \"\", false")
	g.P("}")
	g.P()

	g.P("func formatAtlasHCLColRefList(cols []string) string {")
	g.P("\trefs := make([]string, len(cols))")
	g.P("\tfor i, c := range cols {")
	g.P("\t\trefs[i] = \"column.\" + c")
	g.P("\t}")
	g.P("\treturn \"[\" + ", stringsJoin, "(refs, \", \") + \"]\"")
	g.P("}")
	g.P()

	g.P("func formatAtlasHCLPropKey(k string) string {")
	g.P("\tif isValidDorisSQLIdentifier(k) {")
	g.P("\t\treturn k")
	g.P("\t}")
	g.P("\treturn ", fmtSprintf, "(\"%q\", k)")
	g.P("}")
	g.P()

	g.P("// BuildDorisAtlasHCL renders a deterministic Atlas HCL table block for this")
	g.P("// Apache Doris 4.x table descriptor.")
	g.P("func (t DorisTableDescriptor) BuildDorisAtlasHCL(tenantSlug string, buckets int) string {")
	g.P("\tif !validateDorisDescriptor(t) || t.SyncTier == \"ICEBERG_ONLY\" {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tvar uks []string")
	g.P("\tif len(t.UniqueKeyColumns) > 0 {")
	g.P("\t\tuks = dedupDorisColumnList(t.UniqueKeyColumns)")
	g.P("\t} else {")
	g.P("\t\tfor _, c := range t.Columns {")
	g.P("\t\t\tif c.IsUniqueKey {")
	g.P("\t\t\t\tuks = append(uks, c.ColumnName)")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t\tuks = dedupDorisColumnList(uks)")
	g.P("\t}")
	g.P("\tdupKeys := dedupDorisColumnList(t.DuplicateKeyColumns)")
	g.P("\tngramCols := dedupDorisColumnList(t.NgramBfColumns)")
	g.P("\tbloomCols := dedupDorisColumnList(t.BloomFilterColumns)")
	g.P("\tif t.DatabaseScope == \"GLOBAL_TERMINOLOGY\" && t.SyncTier == \"RELEASE_PARTITIONED\" && len(uks) == 0 && len(dupKeys) == 0 {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\torderedCols := reorderDorisDescriptorColumns(t.Columns, uks, dupKeys, true)")
	g.P("\tif len(uks) == 0 && len(dupKeys) == 0 && len(orderedCols) > 0 {")
	g.P("\t\tcp := make([]DorisColumnDescriptor, len(orderedCols))")
	g.P("\t\tcopy(cp, orderedCols)")
	g.P("\t\tcp[0].Nullable = false")
	g.P("\t\torderedCols = cp")
	g.P("\t}")
	g.P("\tcolTypes := make(map[string]string, len(orderedCols))")
	g.P("\tfor _, c := range orderedCols {")
	g.P("\t\tcolTypes[c.ColumnName] = ", stringsToUpper, "(", stringsTrimSpace, "(c.DorisType))")
	g.P("\t}")
	g.P("\tfor _, uk := range uks {")
	g.P("\t\tif !isDorisKeyEligibleType(colTypes[uk]) {")
	g.P("\t\t\treturn \"\"")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tfor _, dk := range dupKeys {")
	g.P("\t\tif !isDorisKeyEligibleType(colTypes[dk]) {")
	g.P("\t\t\treturn \"\"")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tif len(uks) == 0 && len(dupKeys) == 0 && len(orderedCols) > 0 {")
	g.P("\t\tif !isDorisKeyEligibleType(orderedCols[0].DorisType) {")
	g.P("\t\t\treturn \"\"")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tvar distCols []string")
	g.P("\tif len(t.DistributionColumns) > 0 {")
	g.P("\t\tdistCols = dedupDorisColumnList(t.DistributionColumns)")
	g.P("\t} else if t.DistributedByColumn != \"\" {")
	g.P("\t\tdistCols = []string{t.DistributedByColumn}")
	g.P("\t} else if len(uks) > 0 {")
	g.P("\t\tif len(uks) > 1 && uks[0] == \"tenant_id\" {")
	g.P("\t\t\tdistCols = []string{uks[1]}")
	g.P("\t\t} else {")
	g.P("\t\t\tdistCols = []string{uks[0]}")
	g.P("\t\t}")
	g.P("\t} else if len(dupKeys) > 0 {")
	g.P("\t\tdistCols = []string{dupKeys[0]}")
	g.P("\t} else if len(orderedCols) > 0 {")
	g.P("\t\tdistCols = []string{orderedCols[0].ColumnName}")
	g.P("\t}")
	g.P("\tfor _, dc := range distCols {")
	g.P("\t\tif !isDorisKeyEligibleType(colTypes[dc]) {")
	g.P("\t\t\treturn \"\"")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tslug, ok := sanitizeDorisTenantSlug(tenantSlug)")
	g.P("\tif !ok {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tvar schemaRef string")
	g.P("\tswitch t.DatabaseScope {")
	g.P("\tcase \"GLOBAL_TERMINOLOGY\":")
	g.P("\t\tschemaRef = \"schema.terminology\"")
	g.P("\tcase \"GLOBAL_TERMINOLOGY_RAG\":")
	g.P("\t\tschemaRef = \"schema.terminology_rag\"")
	g.P("\tcase \"GLOBAL_TELEMETRY\":")
	g.P("\t\tschemaRef = \"schema.telemetry\"")
	g.P("\tdefault:")
	g.P("\t\tschemaRef = \"schema.tenant_\" + slug")
	g.P("\t}")
	g.P("\tvar bucketsExpr string")
	g.P("\tif buckets > 0 {")
	g.P("\t\tbucketsExpr = ", strconvItoa, "(buckets)")
	g.P("\t} else if t.Buckets > 0 {")
	g.P("\t\tbucketsExpr = ", strconvItoa, "(t.Buckets)")
	g.P("\t} else if t.AutoBuckets {")
	g.P("\t\tbucketsExpr = `\"auto\"`")
	g.P("\t} else {")
	g.P("\t\tswitch t.DatabaseScope {")
	g.P("\t\tcase \"GLOBAL_TERMINOLOGY\":")
	g.P("\t\t\tbucketsExpr = \"1\"")
	g.P("\t\tcase \"GLOBAL_TERMINOLOGY_RAG\", \"GLOBAL_TELEMETRY\":")
	g.P("\t\t\tbucketsExpr = \"8\"")
	g.P("\t\tdefault:")
	g.P("\t\t\tbucketsExpr = `\"auto\"`")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tseqCol := ", stringsTrimSpace, "(t.SequenceCol)")
	g.P("\tif seqCol == \"\" {")
	g.P("\t\tseqCol = ", stringsTrimSpace, "(t.SequenceColumn)")
	g.P("\t}")
	g.P("\tif seqCol == \"\" {")
	g.P("\t\tfor _, c := range orderedCols {")
	g.P("\t\t\tif c.IsSequenceCol {")
	g.P("\t\t\t\tseqCol = c.ColumnName")
	g.P("\t\t\t\tbreak")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tvar b ", stringsBuilder)
	g.P("\tb.WriteString(", fmtSprintf, "(\"table %q {\\n\", t.TableName))")
	g.P("\tb.WriteString(", fmtSprintf, "(\"  schema = %s\\n\", schemaRef))")
	g.P("\tfor _, c := range orderedCols {")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"  column %q {\\n\", c.ColumnName))")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    type = %s\\n\", dorisTypeToAtlasHCLType(c.DorisType)))")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    null = %t\\n\", c.Nullable))")
	g.P("\t\tif defVal, hasDef := resolveDorisColumnDefaultValue(c); hasDef {")
	g.P("\t\t\tb.WriteString(", fmtSprintf, "(\"    default = %q\\n\", defVal))")
	g.P("\t\t}")
	g.P("\t\tif c.Comment != \"\" {")
	g.P("\t\t\tb.WriteString(", fmtSprintf, "(\"    comment = %q\\n\", c.Comment))")
	g.P("\t\t}")
	g.P("\t\tb.WriteString(\"  }\\n\")")
	g.P("\t}")
	g.P("\tif len(uks) > 0 {")
	g.P("\t\tb.WriteString(\"  unique_key {\\n\")")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    columns = %s\\n\", formatAtlasHCLColRefList(uks)))")
	g.P("\t\tb.WriteString(\"    merge_on_write = true\\n\")")
	g.P("\t\tif seqCol != \"\" {")
	g.P("\t\t\tb.WriteString(", fmtSprintf, "(\"    sequence_col = column.%s\\n\", seqCol))")
	g.P("\t\t}")
	g.P("\t\tb.WriteString(\"  }\\n\")")
	g.P("\t} else {")
	g.P("\t\tdkCols := dupKeys")
	g.P("\t\tif len(dkCols) == 0 && len(orderedCols) > 0 {")
	g.P("\t\t\tdkCols = []string{orderedCols[0].ColumnName}")
	g.P("\t\t}")
	g.P("\t\tif len(dkCols) > 0 {")
	g.P("\t\t\tb.WriteString(\"  duplicate_key {\\n\")")
	g.P("\t\t\tb.WriteString(", fmtSprintf, "(\"    columns = %s\\n\", formatAtlasHCLColRefList(dkCols)))")
	g.P("\t\t\tb.WriteString(\"  }\\n\")")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tif len(distCols) > 0 {")
	g.P("\t\tb.WriteString(\"  distributed_by_hash {\\n\")")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    columns = %s\\n\", formatAtlasHCLColRefList(distCols)))")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    buckets = %s\\n\", bucketsExpr))")
	g.P("\t\tb.WriteString(\"  }\\n\")")
	g.P("\t}")
	g.P("\tvar partCols []string")
	g.P("\tif len(t.PartitionByColumns) > 0 {")
	g.P("\t\tpartCols = dedupDorisColumnList(t.PartitionByColumns)")
	g.P("\t} else {")
	g.P("\t\tfor _, c := range orderedCols {")
	g.P("\t\t\tif c.IsPartitionKey {")
	g.P("\t\t\t\tpartCols = append(partCols, c.ColumnName)")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t\tpartCols = dedupDorisColumnList(partCols)")
	g.P("\t}")
	g.P("\tif len(partCols) > 0 {")
	g.P("\t\tpartBlock := \"partition_by_list\"")
	g.P("\t\tif ", stringsHasPrefix, "(colTypes[partCols[0]], \"DATE\") {")
	g.P("\t\t\tpartBlock = \"partition_by_range\"")
	g.P("\t\t}")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"  %s {\\n\", partBlock))")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    columns = %s\\n\", formatAtlasHCLColRefList(partCols)))")
	g.P("\t\tb.WriteString(\"  }\\n\")")
	g.P("\t}")
	g.P("\temittedIdx := make(map[string]bool)")
	g.P("\tseenInv := make(map[string]bool)")
	g.P("\tvar invCols []string")
	g.P("\tfor _, c := range orderedCols {")
	g.P("\t\tif c.InvertedIndex && !seenInv[c.ColumnName] {")
	g.P("\t\t\tseenInv[c.ColumnName] = true")
	g.P("\t\t\tinvCols = append(invCols, c.ColumnName)")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tfor _, colName := range t.InvertedIndexColumns {")
	g.P("\t\tif colName != \"\" && !seenInv[colName] {")
	g.P("\t\t\tseenInv[colName] = true")
	g.P("\t\t\tinvCols = append(invCols, colName)")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tfor _, idxCol := range invCols {")
	g.P("\t\tif colTypes[idxCol] == \"TEXT\" {")
	g.P("\t\t\tidxName := ", fmtSprintf, "(\"idx_%s_bm25\", idxCol)")
	g.P("\t\t\temittedIdx[idxName] = true")
	g.P("\t\t\tb.WriteString(", fmtSprintf, "(\"  index %q {\\n\", idxName))")
	g.P("\t\t\tb.WriteString(", fmtSprintf, "(\"    columns = [column.%s]\\n\", idxCol))")
	g.P("\t\t\tb.WriteString(\"    using = \\\"INVERTED\\\"\\n\")")
	g.P("\t\t\tb.WriteString(\"    properties = {\\n\")")
	g.P("\t\t\tb.WriteString(\"      parser = \\\"english\\\"\\n\")")
	g.P("\t\t\tb.WriteString(\"      support_phrase = \\\"true\\\"\\n\")")
	g.P("\t\t\tb.WriteString(\"      lower_case = \\\"true\\\"\\n\")")
	g.P("\t\t\tb.WriteString(\"    }\\n\")")
	g.P("\t\t\tb.WriteString(\"  }\\n\")")
	g.P("\t\t} else {")
	g.P("\t\t\tidxName := ", fmtSprintf, "(\"idx_%s_%s\", t.TableName, idxCol)")
	g.P("\t\t\temittedIdx[idxName] = true")
	g.P("\t\t\tb.WriteString(", fmtSprintf, "(\"  index %q {\\n\", idxName))")
	g.P("\t\t\tb.WriteString(", fmtSprintf, "(\"    columns = [column.%s]\\n\", idxCol))")
	g.P("\t\t\tb.WriteString(\"    using = \\\"INVERTED\\\"\\n\")")
	g.P("\t\t\tb.WriteString(\"  }\\n\")")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tfor _, ngCol := range ngramCols {")
	g.P("\t\tidxName := ", fmtSprintf, "(\"idx_ngram_%s_%s\", t.TableName, ngCol)")
	g.P("\t\temittedIdx[idxName] = true")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"  index %q {\\n\", idxName))")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    columns = [column.%s]\\n\", ngCol))")
	g.P("\t\tb.WriteString(\"    using = \\\"NGRAM_BF\\\"\\n\")")
	g.P("\t\tb.WriteString(\"    properties = {\\n\")")
	g.P("\t\tb.WriteString(\"      gram_size = \\\"3\\\"\\n\")")
	g.P("\t\tb.WriteString(\"      bf_size = \\\"1024\\\"\\n\")")
	g.P("\t\tb.WriteString(\"    }\\n\")")
	g.P("\t\tb.WriteString(\"  }\\n\")")
	g.P("\t}")
	g.P("\tif len(uks) == 0 {")
	g.P("\t\tif embDim, hasEmb := resolveDorisEmbeddingDimension(t); hasEmb {")
	g.P("\t\t\tfor _, c := range orderedCols {")
	g.P("\t\t\t\tif c.ColumnName == \"embedding\" && !c.Nullable && colTypes[c.ColumnName] == \"ARRAY<FLOAT>\" {")
	g.P("\t\t\t\t\tidxName := ", fmtSprintf, "(\"idx_%s_hnsw\", c.ColumnName)")
	g.P("\t\t\t\t\temittedIdx[idxName] = true")
	g.P("\t\t\t\t\tb.WriteString(", fmtSprintf, "(\"  index %q {\\n\", idxName))")
	g.P("\t\t\t\t\tb.WriteString(", fmtSprintf, "(\"    columns = [column.%s]\\n\", c.ColumnName))")
	g.P("\t\t\t\t\tb.WriteString(\"    using = \\\"ANN\\\"\\n\")")
	g.P("\t\t\t\t\tb.WriteString(\"    properties = {\\n\")")
	g.P("\t\t\t\t\tb.WriteString(\"      index_type = \\\"hnsw\\\"\\n\")")
	g.P("\t\t\t\t\tb.WriteString(\"      metric_type = \\\"l2_distance\\\"\\n\")")
	g.P("\t\t\t\t\tb.WriteString(", fmtSprintf, "(\"      dim = \\\"%d\\\"\\n\", embDim))")
	g.P("\t\t\t\t\tb.WriteString(\"    }\\n\")")
	g.P("\t\t\t\t\tb.WriteString(\"  }\\n\")")
	g.P("\t\t\t\t}")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tfor _, idx := range t.Indexes {")
	g.P("\t\tif idx.Name == \"\" || emittedIdx[idx.Name] || len(idx.Columns) == 0 {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\temittedIdx[idx.Name] = true")
	g.P("\t\tusing := idx.Using")
	g.P("\t\tif using == \"\" {")
	g.P("\t\t\tusing = \"INVERTED\"")
	g.P("\t\t}")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"  index %q {\\n\", idx.Name))")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    columns = %s\\n\", formatAtlasHCLColRefList(idx.Columns)))")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    using = %q\\n\", using))")
	g.P("\t\tif len(idx.Properties) > 0 {")
	g.P("\t\t\tkeys := make([]string, 0, len(idx.Properties))")
	g.P("\t\t\tfor k := range idx.Properties {")
	g.P("\t\t\t\tkeys = append(keys, k)")
	g.P("\t\t\t}")
	g.P("\t\t\t", sortStrings, "(keys)")
	g.P("\t\t\tb.WriteString(\"    properties = {\\n\")")
	g.P("\t\t\tfor _, k := range keys {")
	g.P("\t\t\t\tb.WriteString(", fmtSprintf, "(\"      %s = %q\\n\", formatAtlasHCLPropKey(k), idx.Properties[k]))")
	g.P("\t\t\t}")
	g.P("\t\t\tb.WriteString(\"    }\\n\")")
	g.P("\t\t}")
	g.P("\t\tb.WriteString(\"  }\\n\")")
	g.P("\t}")
	g.P("\tvault := t.StorageVaultName")
	g.P("\tif vault == \"\" {")
	g.P("\t\tvault = \"azra_lakehouse_vault\"")
	g.P("\t}")
	g.P("\tb.WriteString(\"  properties = {\\n\")")
	g.P("\tb.WriteString(", fmtSprintf, "(\"    storage_vault_name = %q\\n\", vault))")
	g.P("\tb.WriteString(\"    replication_num = \\\"1\\\"\\n\")")
	g.P("\tb.WriteString(\"    compression = \\\"zstd\\\"\\n\")")
	g.P("\tif len(uks) > 0 || t.EnableMoW {")
	g.P("\t\tb.WriteString(\"    enable_unique_key_merge_on_write = \\\"true\\\"\\n\")")
	g.P("\t}")
	g.P("\tif seqCol != \"\" {")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    \\\"function_column.sequence_col\\\" = %q\\n\", seqCol))")
	g.P("\t}")
	g.P("\tif t.ColocateGroup != \"\" {")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    colocate_with = %q\\n\", t.ColocateGroup))")
	g.P("\t}")
	g.P("\tif t.FileCacheTTLSeconds > 0 {")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    file_cache_ttl_seconds = \\\"%d\\\"\\n\", t.FileCacheTTLSeconds))")
	g.P("\t} else if t.DatabaseScope == \"GLOBAL_TERMINOLOGY\" {")
	g.P("\t\tb.WriteString(\"    file_cache_ttl_seconds = \\\"604800\\\"\\n\")")
	g.P("\t}")
	g.P("\tif t.DatabaseScope == \"TENANT\" || t.DatabaseScope == \"\" {")
	g.P("\t\tb.WriteString(\"    group_commit_interval_ms = \\\"200\\\"\\n\")")
	g.P("\t}")
	g.P("\tif len(bloomCols) > 0 {")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    bloom_filter_columns = %q\\n\", ", stringsJoin, "(bloomCols, \", \")))")
	g.P("\t}")
	g.P("\tb.WriteString(\"  }\\n\")")
	g.P("\tb.WriteString(\"}\")")
	g.P("\treturn b.String()")
	g.P("}")
	g.P()

	g.P("// BuildIcebergAtlasHCL renders a deterministic Atlas HCL table block for this")
	g.P("// Apache Iceberg table descriptor.")
	g.P("func (t DorisTableDescriptor) BuildIcebergAtlasHCL(tenantSlug string) string {")
	g.P("\tif !validateDorisDescriptor(t) || t.SyncTier == \"INTERNAL_ONLY\" {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tslug, ok := sanitizeDorisTenantSlug(tenantSlug)")
	g.P("\tif !ok {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tvar uks []string")
	g.P("\tif len(t.UniqueKeyColumns) > 0 {")
	g.P("\t\tuks = dedupDorisColumnList(t.UniqueKeyColumns)")
	g.P("\t} else {")
	g.P("\t\tfor _, c := range t.Columns {")
	g.P("\t\t\tif c.IsUniqueKey {")
	g.P("\t\t\t\tuks = append(uks, c.ColumnName)")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t\tuks = dedupDorisColumnList(uks)")
	g.P("\t}")
	g.P("\tdupKeys := dedupDorisColumnList(t.DuplicateKeyColumns)")
	g.P("\torderedCols := reorderDorisDescriptorColumns(t.Columns, uks, dupKeys, t.SyncTier != \"ICEBERG_ONLY\")")
	g.P("\tif len(uks) == 0 && len(dupKeys) == 0 && len(orderedCols) > 0 && t.SyncTier != \"ICEBERG_ONLY\" && (t.DatabaseScope != \"GLOBAL_TERMINOLOGY\" || t.SyncTier != \"RELEASE_PARTITIONED\") {")
	g.P("\t\tcp := make([]DorisColumnDescriptor, len(orderedCols))")
	g.P("\t\tcopy(cp, orderedCols)")
	g.P("\t\tcp[0].Nullable = false")
	g.P("\t\torderedCols = cp")
	g.P("\t}")
	g.P("\tbloomCols := dedupDorisColumnList(t.BloomFilterColumns)")
	g.P("\tcatalog := t.IcebergCatalog")
	g.P("\tif catalog == \"\" {")
	g.P("\t\tcatalog = \"azra_lake\"")
	g.P("\t}")
	g.P("\tif !isValidDorisSQLIdentifier(catalog) {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tvar schemaRef string")
	g.P("\tswitch t.DatabaseScope {")
	g.P("\tcase \"GLOBAL_TERMINOLOGY\":")
	g.P("\t\tschemaRef = \"schema.terminology\"")
	g.P("\tcase \"GLOBAL_TELEMETRY\":")
	g.P("\t\tschemaRef = \"schema.telemetry\"")
	g.P("\tcase \"GLOBAL_TERMINOLOGY_RAG\":")
	g.P("\t\tschemaRef = \"schema.terminology_rag\"")
	g.P("\tdefault:")
	g.P("\t\tschemaRef = \"schema.tenant_\" + slug")
	g.P("\t}")
	g.P("\tvar partCols []string")
	g.P("\tif len(t.PartitionByColumns) > 0 {")
	g.P("\t\tpartCols = dedupDorisColumnList(t.PartitionByColumns)")
	g.P("\t} else {")
	g.P("\t\tfor _, c := range orderedCols {")
	g.P("\t\t\tif c.IsPartitionKey {")
	g.P("\t\t\t\tpartCols = append(partCols, c.ColumnName)")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t\tpartCols = dedupDorisColumnList(partCols)")
	g.P("\t}")
	g.P("\tif len(partCols) == 0 && t.SyncTier == \"MONTHLY_PARTITIONED\" {")
	g.P("\t\tfor _, c := range orderedCols {")
	g.P("\t\t\tif c.ColumnName == \"event_month\" {")
	g.P("\t\t\t\tpartCols = append(partCols, \"event_month\")")
	g.P("\t\t\t\tbreak")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tif len(partCols) == 0 && t.SyncTier == \"RELEASE_PARTITIONED\" {")
	g.P("\t\tfor _, c := range orderedCols {")
	g.P("\t\t\tif c.ColumnName == \"release_id\" {")
	g.P("\t\t\t\tpartCols = append(partCols, \"release_id\")")
	g.P("\t\t\t\tbreak")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tpartSpec := ", stringsTrimSpace, "(t.IcebergPartitionSpec)")
	g.P("\tif partSpec == \"\" && len(partCols) > 0 {")
	g.P("\t\tpartSpec = ", stringsJoin, "(partCols, \", \")")
	g.P("\t}")
	g.P("\tvar b ", stringsBuilder)
	g.P("\tb.WriteString(", fmtSprintf, "(\"table %q {\\n\", t.TableName))")
	g.P("\tb.WriteString(", fmtSprintf, "(\"  catalog = %q\\n\", catalog))")
	g.P("\tb.WriteString(", fmtSprintf, "(\"  schema = %s\\n\", schemaRef))")
	g.P("\tfor _, c := range orderedCols {")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"  column %q {\\n\", c.ColumnName))")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    type = %s\\n\", dorisColumnIcebergHCLType(c.DorisType, c.IcebergType)))")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    null = %t\\n\", c.Nullable))")
	g.P("\t\tif c.Comment != \"\" {")
	g.P("\t\t\tb.WriteString(", fmtSprintf, "(\"    comment = %q\\n\", c.Comment))")
	g.P("\t\t}")
	g.P("\t\tb.WriteString(\"  }\\n\")")
	g.P("\t}")
	g.P("\tif partSpec != \"\" {")
	g.P("\t\tb.WriteString(\"  partition_by {\\n\")")
	g.P("\t\tb.WriteString(", fmtSprintf, "(\"    spec = %q\\n\", partSpec))")
	g.P("\t\tb.WriteString(\"  }\\n\")")
	g.P("\t}")
	g.P("\tb.WriteString(\"  properties = {\\n\")")
	g.P("\tb.WriteString(\"    engine = \\\"iceberg\\\"\\n\")")
	g.P("\tb.WriteString(\"    \\\"write.format.default\\\" = \\\"parquet\\\"\\n\")")
	g.P("\tb.WriteString(\"    \\\"write.parquet.compression-codec\\\" = \\\"zstd\\\"\\n\")")
	g.P("\tif len(bloomCols) > 0 {")
	g.P("\t\tfor _, bf := range bloomCols {")
	g.P("\t\t\tb.WriteString(", fmtSprintf, "(\"    \\\"write.parquet.bloom-filter-enabled.column.%s\\\" = \\\"true\\\"\\n\", bf))")
	g.P("\t\t}")
	g.P("\t} else if (t.DatabaseScope == \"TENANT\" || t.DatabaseScope == \"\") && ((len(t.Columns) > 0 && t.Columns[0].ColumnName == \"patient_id\") || (len(orderedCols) > 0 && orderedCols[0].ColumnName == \"patient_id\")) {")
	g.P("\t\tb.WriteString(\"    \\\"write.parquet.bloom-filter-enabled.column.patient_id\\\" = \\\"true\\\"\\n\")")
	g.P("\t}")
	g.P("\tb.WriteString(\"  }\\n\")")
	g.P("\tb.WriteString(\"}\")")
	g.P("\treturn b.String()")
	g.P("}")
	g.P()

	g.P("func normalizeParsedDorisHCLType(rawType string) string {")
	g.P("\tt := ", stringsTrimSpace, "(rawType)")
	g.P("\tif len(t) >= 2 && t[0] == '\"' && t[len(t)-1] == '\"' {")
	g.P("\t\tt = ", stringsTrimSpace, "(t[1 : len(t)-1])")
	g.P("\t}")
	g.P("\tupper := ", stringsToUpper, "(t)")
	g.P("\tupper = ", stringsReplaceAll, "(upper, \", \", \",\")")
	g.P("\tupper = ", stringsReplaceAll, "(upper, \",\", \", \")")
	g.P("\treturn upper")
	g.P("}")
	g.P()

	g.P("func stripAtlasHCLComments(src string) (string, error) {")
	g.P("\tvar b ", stringsBuilder)
	g.P("\tb.Grow(len(src))")
	g.P("\tinQuotes := false")
	g.P("\tfor i := 0; i < len(src); i++ {")
	g.P("\t\tch := src[i]")
	g.P("\t\tif inQuotes {")
	g.P("\t\t\tb.WriteByte(ch)")
	g.P("\t\t\tif ch == '\\\\' && i+1 < len(src) {")
	g.P("\t\t\t\ti++")
	g.P("\t\t\t\tb.WriteByte(src[i])")
	g.P("\t\t\t\tcontinue")
	g.P("\t\t\t}")
	g.P("\t\t\tif ch == '\"' {")
	g.P("\t\t\t\tinQuotes = false")
	g.P("\t\t\t}")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tif ch == '\"' {")
	g.P("\t\t\tinQuotes = true")
	g.P("\t\t\tb.WriteByte(ch)")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tif ch == '#' || (ch == '/' && i+1 < len(src) && src[i+1] == '/') {")
	g.P("\t\t\tfor i < len(src) && src[i] != '\\n' {")
	g.P("\t\t\t\ti++")
	g.P("\t\t\t}")
	g.P("\t\t\tif i < len(src) {")
	g.P("\t\t\t\tb.WriteByte('\\n')")
	g.P("\t\t\t}")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tif ch == '/' && i+1 < len(src) && src[i+1] == '*' {")
	g.P("\t\t\ti += 2")
	g.P("\t\t\tclosed := false")
	g.P("\t\t\tfor i+1 < len(src) {")
	g.P("\t\t\t\tif src[i] == '*' && src[i+1] == '/' {")
	g.P("\t\t\t\t\ti++")
	g.P("\t\t\t\t\tclosed = true")
	g.P("\t\t\t\t\tbreak")
	g.P("\t\t\t\t}")
	g.P("\t\t\t\tif src[i] == '\\n' {")
	g.P("\t\t\t\t\tb.WriteByte('\\n')")
	g.P("\t\t\t\t}")
	g.P("\t\t\t\ti++")
	g.P("\t\t\t}")
	g.P("\t\t\tif !closed {")
	g.P("\t\t\t\treturn \"\", ", fmtErrorf, "(\"unterminated block comment in Atlas HCL\")")
	g.P("\t\t\t}")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tb.WriteByte(ch)")
	g.P("\t}")
	g.P("\tif inQuotes {")
	g.P("\t\treturn \"\", ", fmtErrorf, "(\"unterminated string literal in Atlas HCL\")")
	g.P("\t}")
	g.P("\treturn b.String(), nil")
	g.P("}")
	g.P()

	g.P("func findMatchingHCLBrace(s string, openIdx int) (int, error) {")
	g.P("\tdepth := 0")
	g.P("\tinQuotes := false")
	g.P("\tfor i := openIdx; i < len(s); i++ {")
	g.P("\t\tch := s[i]")
	g.P("\t\tif inQuotes {")
	g.P("\t\t\tif ch == '\\\\' && i+1 < len(s) {")
	g.P("\t\t\t\ti++")
	g.P("\t\t\t\tcontinue")
	g.P("\t\t\t}")
	g.P("\t\t\tif ch == '\"' {")
	g.P("\t\t\t\tinQuotes = false")
	g.P("\t\t\t}")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tswitch ch {")
	g.P("\t\tcase '\"':")
	g.P("\t\t\tinQuotes = true")
	g.P("\t\tcase '{':")
	g.P("\t\t\tdepth++")
	g.P("\t\tcase '}':")
	g.P("\t\t\tdepth--")
	g.P("\t\t\tif depth == 0 {")
	g.P("\t\t\t\treturn i, nil")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn -1, ", fmtErrorf, "(\"unbalanced '{' in Atlas HCL\")")
	g.P("}")
	g.P()

	g.P("func unquoteHCLString(s string) string {")
	g.P("\ts = ", stringsTrimSpace, "(s)")
	g.P("\tif len(s) >= 2 && s[0] == '\"' && s[len(s)-1] == '\"' {")
	g.P("\t\tif unq, err := ", strconvUnquote, "(s); err == nil {")
	g.P("\t\t\treturn unq")
	g.P("\t\t}")
	g.P("\t\treturn s[1 : len(s)-1]")
	g.P("\t}")
	g.P("\treturn s")
	g.P("}")
	g.P()

	g.P("func parseHCLColumnRefList(raw string) ([]string, error) {")
	g.P("\ts := ", stringsTrimSpace, "(raw)")
	g.P("\tif !", stringsHasPrefix, "(s, \"[\") || !", stringsHasSuffix, "(s, \"]\") {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"expected bracketed column list, got %q\", raw)")
	g.P("\t}")
	g.P("\tinner := ", stringsTrimSpace, "(s[1 : len(s)-1])")
	g.P("\tif inner == \"\" {")
	g.P("\t\treturn nil, nil")
	g.P("\t}")
	g.P("\tparts := ", stringsSplit, "(inner, \",\")")
	g.P("\tout := make([]string, 0, len(parts))")
	g.P("\tfor _, p := range parts {")
	g.P("\t\titem := unquoteHCLString(p)")
	g.P("\t\titem = ", stringsTrimPrefix, "(item, \"column.\")")
	g.P("\t\titem = ", stringsTrimSpace, "(item)")
	g.P("\t\tif item == \"\" || !isValidDorisSQLIdentifier(item) {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"invalid column reference %q in %q\", p, raw)")
	g.P("\t\t}")
	g.P("\t\tout = append(out, item)")
	g.P("\t}")
	g.P("\treturn out, nil")
	g.P("}")
	g.P()

	g.P("func parseHCLKeyValueAssignments(body string) (map[string]string, error) {")
	g.P("\tres := make(map[string]string)")
	g.P("\tfor _, rawLine := range ", stringsSplit, "(body, \"\\n\") {")
	g.P("\t\tline := ", stringsTrimSpace, "(rawLine)")
	g.P("\t\tif line == \"\" {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\teqIdx := ", stringsIndexByte, "(line, '=')")
	g.P("\t\tif eqIdx <= 0 {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"expected key = value assignment, got %q\", line)")
	g.P("\t\t}")
	g.P("\t\tk := unquoteHCLString(line[:eqIdx])")
	g.P("\t\tv := ", stringsTrimSpace, "(line[eqIdx+1:])")
	g.P("\t\tif k == \"\" || v == \"\" {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"invalid assignment %q\", line)")
	g.P("\t\t}")
	g.P("\t\tres[k] = v")
	g.P("\t}")
	g.P("\treturn res, nil")
	g.P("}")
	g.P()

	g.P("// ParseDorisAtlasHCL parses one or more Atlas HCL table blocks into")
	g.P("// []DorisTableDescriptor values.")
	g.P("func ParseDorisAtlasHCL(hclContent string) ([]DorisTableDescriptor, error) {")
	g.P("\tcleaned, err := stripAtlasHCLComments(hclContent)")
	g.P("\tif err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\tvar tables []DorisTableDescriptor")
	g.P("\tpos := 0")
	g.P("\tfor pos < len(cleaned) {")
	g.P("\t\tfor pos < len(cleaned) && (cleaned[pos] == ' ' || cleaned[pos] == '\\t' || cleaned[pos] == '\\n' || cleaned[pos] == '\\r') {")
	g.P("\t\t\tpos++")
	g.P("\t\t}")
	g.P("\t\tif pos >= len(cleaned) {")
	g.P("\t\t\tbreak")
	g.P("\t\t}")
	g.P("\t\topenIdx := ", stringsIndexByte, "(cleaned[pos:], '{')")
	g.P("\t\tif openIdx < 0 {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"unexpected trailing content in Atlas HCL: %q\", ", stringsTrimSpace, "(cleaned[pos:]))")
	g.P("\t\t}")
	g.P("\t\topenAbs := pos + openIdx")
	g.P("\t\theader := ", stringsTrimSpace, "(cleaned[pos:openAbs])")
	g.P("\t\tcloseAbs, err := findMatchingHCLBrace(cleaned, openAbs)")
	g.P("\t\tif err != nil {")
	g.P("\t\t\treturn nil, err")
	g.P("\t\t}")
	g.P("\t\tbody := cleaned[openAbs+1 : closeAbs]")
	g.P("\t\tpos = closeAbs + 1")
	g.P("\t\tif ", stringsHasPrefix, "(header, \"schema \") || header == \"schema\" {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tif !", stringsHasPrefix, "(header, \"table \") {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"unexpected top-level HCL block %q (expected table \\\"<name>\\\")\", header)")
	g.P("\t\t}")
	g.P("\t\ttableName := unquoteHCLString(", stringsTrimSpace, "(", stringsTrimPrefix, "(header, \"table\")))")
	g.P("\t\tif !isValidDorisSQLIdentifier(tableName) {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"invalid table name %q in Atlas HCL\", tableName)")
	g.P("\t\t}")
	g.P("\t\tdesc, err := parseDorisAtlasTableBlock(tableName, body)")
	g.P("\t\tif err != nil {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"table %q: %w\", tableName, err)")
	g.P("\t\t}")
	g.P("\t\ttables = append(tables, desc)")
	g.P("\t}")
	g.P("\tif len(tables) == 0 {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"no table blocks found in Atlas HCL\")")
	g.P("\t}")
	g.P("\treturn tables, nil")
	g.P("}")
	g.P()

	g.P("func parseDorisAtlasTableBlock(tableName, body string) (DorisTableDescriptor, error) {")
	g.P("\tdesc := DorisTableDescriptor{TableName: tableName}")
	g.P("\tcolOrder := make([]string, 0)")
	g.P("\tcolMap := make(map[string]DorisColumnDescriptor)")
	g.P("\ti := 0")
	g.P("\tfor i < len(body) {")
	g.P("\t\tfor i < len(body) && (body[i] == ' ' || body[i] == '\\t' || body[i] == '\\n' || body[i] == '\\r') {")
	g.P("\t\t\ti++")
	g.P("\t\t}")
	g.P("\t\tif i >= len(body) {")
	g.P("\t\t\tbreak")
	g.P("\t\t}")
	g.P("\t\tlineEnd := ", stringsIndexByte, "(body[i:], '\\n')")
	g.P("\t\tvar line string")
	g.P("\t\tif lineEnd < 0 {")
	g.P("\t\t\tline = body[i:]")
	g.P("\t\t} else {")
	g.P("\t\t\tline = body[i : i+lineEnd]")
	g.P("\t\t}")
	g.P("\t\ttrimmedLine := ", stringsTrimSpace, "(line)")
	g.P("\t\tif trimmedLine == \"\" {")
	g.P("\t\t\tif lineEnd < 0 {")
	g.P("\t\t\t\tbreak")
	g.P("\t\t\t}")
	g.P("\t\t\ti += lineEnd + 1")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tif !", stringsContains, "(trimmedLine, \"{\") {")
	g.P("\t\t\teqIdx := ", stringsIndexByte, "(trimmedLine, '=')")
	g.P("\t\t\tif eqIdx <= 0 {")
	g.P("\t\t\t\treturn desc, ", fmtErrorf, "(\"unexpected statement %q\", trimmedLine)")
	g.P("\t\t\t}")
	g.P("\t\t\tk := unquoteHCLString(trimmedLine[:eqIdx])")
	g.P("\t\t\tv := ", stringsTrimSpace, "(trimmedLine[eqIdx+1:])")
	g.P("\t\t\tswitch k {")
	g.P("\t\t\tcase \"schema\":")
	g.P("\t\t\t\trawSchema := unquoteHCLString(v)")
	g.P("\t\t\t\trawSchema = ", stringsTrimPrefix, "(rawSchema, \"schema.\")")
	g.P("\t\t\t\tswitch {")
	g.P("\t\t\t\tcase rawSchema == \"terminology\":")
	g.P("\t\t\t\t\tdesc.DatabaseScope = \"GLOBAL_TERMINOLOGY\"")
	g.P("\t\t\t\tcase rawSchema == \"terminology_rag\":")
	g.P("\t\t\t\t\tdesc.DatabaseScope = \"GLOBAL_TERMINOLOGY_RAG\"")
	g.P("\t\t\t\tcase rawSchema == \"telemetry\":")
	g.P("\t\t\t\t\tdesc.DatabaseScope = \"GLOBAL_TELEMETRY\"")
	g.P("\t\t\t\tdefault:")
	g.P("\t\t\t\t\tdesc.DatabaseScope = \"TENANT\"")
	g.P("\t\t\t\t}")
	g.P("\t\t\tcase \"catalog\":")
	g.P("\t\t\t\tdesc.IcebergCatalog = unquoteHCLString(v)")
	g.P("\t\t\t}")
	g.P("\t\t\tif lineEnd < 0 {")
	g.P("\t\t\t\tbreak")
	g.P("\t\t\t}")
	g.P("\t\t\ti += lineEnd + 1")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\topenRel := ", stringsIndexByte, "(body[i:], '{')")
	g.P("\t\topenAbs := i + openRel")
	g.P("\t\tblockHeader := ", stringsTrimSpace, "(body[i:openAbs])")
	g.P("\t\tcloseAbs, err := findMatchingHCLBrace(body, openAbs)")
	g.P("\t\tif err != nil {")
	g.P("\t\t\treturn desc, err")
	g.P("\t\t}")
	g.P("\t\tblockBody := body[openAbs+1 : closeAbs]")
	g.P("\t\ti = closeAbs + 1")
	g.P("\t\tif err := applyDorisAtlasSubBlock(&desc, blockHeader, blockBody, &colOrder, colMap); err != nil {")
	g.P("\t\t\treturn desc, err")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tukSet := make(map[string]bool, len(desc.UniqueKeyColumns))")
	g.P("\tfor _, uk := range desc.UniqueKeyColumns {")
	g.P("\t\tukSet[uk] = true")
	g.P("\t}")
	g.P("\tpartSet := make(map[string]bool, len(desc.PartitionByColumns))")
	g.P("\tfor _, pk := range desc.PartitionByColumns {")
	g.P("\t\tpartSet[pk] = true")
	g.P("\t}")
	g.P("\tinvSet := make(map[string]bool, len(desc.InvertedIndexColumns))")
	g.P("\tfor _, ic := range desc.InvertedIndexColumns {")
	g.P("\t\tinvSet[ic] = true")
	g.P("\t}")
	g.P("\tcols := make([]DorisColumnDescriptor, 0, len(colOrder))")
	g.P("\tfor _, name := range colOrder {")
	g.P("\t\tc := colMap[name]")
	g.P("\t\tif ukSet[name] {")
	g.P("\t\t\tc.IsUniqueKey = true")
	g.P("\t\t}")
	g.P("\t\tif desc.SequenceCol != \"\" && name == desc.SequenceCol {")
	g.P("\t\t\tc.IsSequenceCol = true")
	g.P("\t\t}")
	g.P("\t\tif partSet[name] {")
	g.P("\t\t\tc.IsPartitionKey = true")
	g.P("\t\t}")
	g.P("\t\tif invSet[name] {")
	g.P("\t\t\tc.InvertedIndex = true")
	g.P("\t\t}")
	g.P("\t\tcols = append(cols, c)")
	g.P("\t}")
	g.P("\tdesc.Columns = cols")
	g.P("\tif desc.SyncTier == \"\" {")
	g.P("\t\tswitch {")
	g.P("\t\tcase len(desc.PartitionByColumns) == 1 && desc.PartitionByColumns[0] == \"event_month\":")
	g.P("\t\t\tdesc.SyncTier = \"MONTHLY_PARTITIONED\"")
	g.P("\t\tcase len(desc.PartitionByColumns) == 1 && desc.PartitionByColumns[0] == \"release_id\":")
	g.P("\t\t\tdesc.SyncTier = \"RELEASE_PARTITIONED\"")
	g.P("\t\tcase desc.IcebergCatalog != \"\" && len(desc.UniqueKeyColumns) == 0 && len(desc.DuplicateKeyColumns) == 0:")
	g.P("\t\t\tdesc.SyncTier = \"ICEBERG_ONLY\"")
	g.P("\t\tdefault:")
	g.P("\t\t\tdesc.SyncTier = \"WHOLE_TABLE\"")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn desc, nil")
	g.P("}")
	g.P()

	g.P("func applyDorisAtlasSubBlock(desc *DorisTableDescriptor, blockHeader, blockBody string, colOrder *[]string, colMap map[string]DorisColumnDescriptor) error {")
	g.P("\theader := ", stringsTrimSpace, "(", stringsTrimSuffix, "(", stringsTrimSpace, "(blockHeader), \"=\"))")
	g.P("\tswitch {")
	g.P("\tcase ", stringsHasPrefix, "(header, \"column \"):")
	g.P("\t\tcolName := unquoteHCLString(", stringsTrimSpace, "(", stringsTrimPrefix, "(header, \"column\")))")
	g.P("\t\tif !isValidDorisSQLIdentifier(colName) {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"invalid column name %q\", colName)")
	g.P("\t\t}")
	g.P("\t\tif _, exists := colMap[colName]; exists {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"duplicate column %q\", colName)")
	g.P("\t\t}")
	g.P("\t\tkv, err := parseHCLKeyValueAssignments(blockBody)")
	g.P("\t\tif err != nil {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"column %q: %w\", colName, err)")
	g.P("\t\t}")
	g.P("\t\trawType, ok := kv[\"type\"]")
	g.P("\t\tif !ok || rawType == \"\" {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"column %q missing type attribute\", colName)")
	g.P("\t\t}")
	g.P("\t\tdorisType := normalizeParsedDorisHCLType(rawType)")
	g.P("\t\tnullable := false")
	g.P("\t\tif nullStr, hasNull := kv[\"null\"]; hasNull {")
	g.P("\t\t\tparsedNull, bErr := ", strconvParseBool, "(unquoteHCLString(nullStr))")
	g.P("\t\t\tif bErr != nil {")
	g.P("\t\t\t\treturn ", fmtErrorf, "(\"column %q invalid null attribute %q\", colName, nullStr)")
	g.P("\t\t\t}")
	g.P("\t\t\tnullable = parsedNull")
	g.P("\t\t}")
	g.P("\t\tdefVal := \"\"")
	g.P("\t\tif rawDef, hasDef := kv[\"default\"]; hasDef {")
	g.P("\t\t\tdefVal = unquoteHCLString(rawDef)")
	g.P("\t\t}")
	g.P("\t\tcomment := \"\"")
	g.P("\t\tif rawComment, hasComment := kv[\"comment\"]; hasComment {")
	g.P("\t\t\tcomment = unquoteHCLString(rawComment)")
	g.P("\t\t}")
	g.P("\t\tcolDesc := DorisColumnDescriptor{ColumnName: colName, ProtoFieldName: colName, DorisType: dorisType, Nullable: nullable, DefaultValue: defVal, Comment: comment}")
	g.P("\t\tcolDesc.IcebergType = colDesc.CanonicalIcebergSpecType()")
	g.P("\t\t*colOrder = append(*colOrder, colName)")
	g.P("\t\tcolMap[colName] = colDesc")
	g.P("\t\treturn nil")
	g.P("\tcase header == \"unique_key\":")
	g.P("\t\tkv, err := parseHCLKeyValueAssignments(blockBody)")
	g.P("\t\tif err != nil {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"unique_key: %w\", err)")
	g.P("\t\t}")
	g.P("\t\tcolsRaw, ok := kv[\"columns\"]")
	g.P("\t\tif !ok {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"unique_key missing columns attribute\")")
	g.P("\t\t}")
	g.P("\t\tuks, err := parseHCLColumnRefList(colsRaw)")
	g.P("\t\tif err != nil {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"unique_key: %w\", err)")
	g.P("\t\t}")
	g.P("\t\tdesc.UniqueKeyColumns = uks")
	g.P("\t\tif mowStr, hasMoW := kv[\"merge_on_write\"]; hasMoW {")
	g.P("\t\t\tmow, bErr := ", strconvParseBool, "(unquoteHCLString(mowStr))")
	g.P("\t\t\tif bErr != nil {")
	g.P("\t\t\t\treturn ", fmtErrorf, "(\"unique_key invalid merge_on_write %q\", mowStr)")
	g.P("\t\t\t}")
	g.P("\t\t\tdesc.EnableMoW = mow")
	g.P("\t\t}")
	g.P("\t\tif seqRaw, hasSeq := kv[\"sequence_col\"]; hasSeq {")
	g.P("\t\t\tseqCol := ", stringsTrimPrefix, "(unquoteHCLString(seqRaw), \"column.\")")
	g.P("\t\t\tif !isValidDorisSQLIdentifier(seqCol) {")
	g.P("\t\t\t\treturn ", fmtErrorf, "(\"unique_key invalid sequence_col %q\", seqRaw)")
	g.P("\t\t\t}")
	g.P("\t\t\tdesc.SequenceCol = seqCol")
	g.P("\t\t\tdesc.SequenceColumn = seqCol")
	g.P("\t\t}")
	g.P("\t\treturn nil")
	g.P("\tcase header == \"duplicate_key\":")
	g.P("\t\tkv, err := parseHCLKeyValueAssignments(blockBody)")
	g.P("\t\tif err != nil {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"duplicate_key: %w\", err)")
	g.P("\t\t}")
	g.P("\t\tcolsRaw, ok := kv[\"columns\"]")
	g.P("\t\tif !ok {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"duplicate_key missing columns attribute\")")
	g.P("\t\t}")
	g.P("\t\tdks, err := parseHCLColumnRefList(colsRaw)")
	g.P("\t\tif err != nil {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"duplicate_key: %w\", err)")
	g.P("\t\t}")
	g.P("\t\tdesc.DuplicateKeyColumns = dks")
	g.P("\t\treturn nil")
	g.P("\tcase header == \"distributed_by_hash\":")
	g.P("\t\tkv, err := parseHCLKeyValueAssignments(blockBody)")
	g.P("\t\tif err != nil {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"distributed_by_hash: %w\", err)")
	g.P("\t\t}")
	g.P("\t\tif colsRaw, ok := kv[\"columns\"]; ok {")
	g.P("\t\t\tdCols, cErr := parseHCLColumnRefList(colsRaw)")
	g.P("\t\t\tif cErr != nil {")
	g.P("\t\t\t\treturn ", fmtErrorf, "(\"distributed_by_hash: %w\", cErr)")
	g.P("\t\t\t}")
	g.P("\t\t\tdesc.DistributionColumns = dCols")
	g.P("\t\t\tif len(dCols) > 0 {")
	g.P("\t\t\t\tdesc.DistributedByColumn = dCols[0]")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t\tif bRaw, ok := kv[\"buckets\"]; ok {")
	g.P("\t\t\tbVal := unquoteHCLString(bRaw)")
	g.P("\t\t\tif ", stringsEqualFold, "(bVal, \"auto\") {")
	g.P("\t\t\t\tdesc.AutoBuckets = true")
	g.P("\t\t\t\tdesc.Buckets = 0")
	g.P("\t\t\t} else {")
	g.P("\t\t\t\tn, nErr := ", strconvAtoi, "(bVal)")
	g.P("\t\t\t\tif nErr != nil || n <= 0 {")
	g.P("\t\t\t\t\treturn ", fmtErrorf, "(\"distributed_by_hash invalid buckets %q\", bRaw)")
	g.P("\t\t\t\t}")
	g.P("\t\t\t\tdesc.Buckets = n")
	g.P("\t\t\t\tdesc.AutoBuckets = false")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t\treturn nil")
	g.P("\tcase header == \"partition_by_range\" || header == \"partition_by_list\":")
	g.P("\t\tkv, err := parseHCLKeyValueAssignments(blockBody)")
	g.P("\t\tif err != nil {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"%s: %w\", header, err)")
	g.P("\t\t}")
	g.P("\t\tif colsRaw, ok := kv[\"columns\"]; ok {")
	g.P("\t\t\tpCols, cErr := parseHCLColumnRefList(colsRaw)")
	g.P("\t\t\tif cErr != nil {")
	g.P("\t\t\t\treturn ", fmtErrorf, "(\"%s: %w\", header, cErr)")
	g.P("\t\t\t}")
	g.P("\t\t\tdesc.PartitionByColumns = pCols")
	g.P("\t\t}")
	g.P("\t\treturn nil")
	g.P("\tcase header == \"partition_by\":")
	g.P("\t\tkv, err := parseHCLKeyValueAssignments(blockBody)")
	g.P("\t\tif err != nil {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"partition_by: %w\", err)")
	g.P("\t\t}")
	g.P("\t\tif specRaw, ok := kv[\"spec\"]; ok {")
	g.P("\t\t\tdesc.IcebergPartitionSpec = unquoteHCLString(specRaw)")
	g.P("\t\t}")
	g.P("\t\treturn nil")
	g.P("\tcase ", stringsHasPrefix, "(header, \"index \"):")
	g.P("\t\tidxName := unquoteHCLString(", stringsTrimSpace, "(", stringsTrimPrefix, "(header, \"index\")))")
	g.P("\t\tif !isValidDorisSQLIdentifier(idxName) {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"invalid index name %q\", idxName)")
	g.P("\t\t}")
	g.P("\t\tidxDesc, err := parseDorisAtlasIndexBlock(idxName, blockBody)")
	g.P("\t\tif err != nil {")
	g.P("\t\t\treturn err")
	g.P("\t\t}")
	g.P("\t\tdesc.Indexes = append(desc.Indexes, idxDesc)")
	g.P("\t\tswitch ", stringsToUpper, "(idxDesc.Using) {")
	g.P("\t\tcase \"INVERTED\":")
	g.P("\t\t\tdesc.InvertedIndexColumns = dedupDorisColumnList(append(desc.InvertedIndexColumns, idxDesc.Columns...))")
	g.P("\t\tcase \"NGRAM_BF\":")
	g.P("\t\t\tdesc.NgramBfColumns = dedupDorisColumnList(append(desc.NgramBfColumns, idxDesc.Columns...))")
	g.P("\t\tcase \"ANN\":")
	g.P("\t\t\tif dimStr, hasDim := idxDesc.Properties[\"dim\"]; hasDim {")
	g.P("\t\t\t\tdim64, dErr := ", strconvParseInt, "(dimStr, 10, 32)")
	g.P("\t\t\t\tif dErr == nil && dim64 > 0 && dim64 <= ", mathMaxInt32, " {")
	g.P("\t\t\t\t\tdesc.EmbeddingDimension = int32(dim64)")
	g.P("\t\t\t\t}")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t\treturn nil")
	g.P("\tcase header == \"properties\":")
	g.P("\t\tkv, err := parseHCLKeyValueAssignments(blockBody)")
	g.P("\t\tif err != nil {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"properties: %w\", err)")
	g.P("\t\t}")
	g.P("\t\tfor k, rawV := range kv {")
	g.P("\t\t\tv := unquoteHCLString(rawV)")
	g.P("\t\t\tswitch k {")
	g.P("\t\t\tcase \"storage_vault_name\":")
	g.P("\t\t\t\tdesc.StorageVaultName = v")
	g.P("\t\t\tcase \"colocate_with\":")
	g.P("\t\t\t\tdesc.ColocateGroup = v")
	g.P("\t\t\tcase \"enable_unique_key_merge_on_write\":")
	g.P("\t\t\t\tif ", stringsEqualFold, "(v, \"true\") {")
	g.P("\t\t\t\t\tdesc.EnableMoW = true")
	g.P("\t\t\t\t}")
	g.P("\t\t\tcase \"function_column.sequence_col\":")
	g.P("\t\t\t\tif desc.SequenceCol == \"\" {")
	g.P("\t\t\t\t\tdesc.SequenceCol = v")
	g.P("\t\t\t\t\tdesc.SequenceColumn = v")
	g.P("\t\t\t\t}")
	g.P("\t\t\tcase \"file_cache_ttl_seconds\":")
	g.P("\t\t\t\tif ttl64, tErr := ", strconvParseInt, "(v, 10, 32); tErr == nil && ttl64 > 0 && ttl64 <= ", mathMaxInt32, " {")
	g.P("\t\t\t\t\tdesc.FileCacheTTLSeconds = int32(ttl64)")
	g.P("\t\t\t\t}")
	g.P("\t\t\tcase \"bloom_filter_columns\":")
	g.P("\t\t\t\tvar bfs []string")
	g.P("\t\t\t\tfor _, part := range ", stringsSplit, "(v, \",\") {")
	g.P("\t\t\t\t\tif trimmed := ", stringsTrimSpace, "(part); trimmed != \"\" {")
	g.P("\t\t\t\t\t\tbfs = append(bfs, trimmed)")
	g.P("\t\t\t\t\t}")
	g.P("\t\t\t\t}")
	g.P("\t\t\t\tdesc.BloomFilterColumns = dedupDorisColumnList(bfs)")
	g.P("\t\t\tcase \"sync_tier\":")
	g.P("\t\t\t\ts := ", stringsTrimSpace, "(", stringsToUpper, "(v))")
	g.P("\t\t\t\ts = ", stringsTrimPrefix, "(s, \"LAKEHOUSE_SYNC_TIER_\")")
	g.P("\t\t\t\tif s == \"UNSPECIFIED\" {")
	g.P("\t\t\t\t\ts = \"\"")
	g.P("\t\t\t\t}")
	g.P("\t\t\t\tdesc.SyncTier = s")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t\treturn nil")
	g.P("\tdefault:")
	g.P("\t\treturn ", fmtErrorf, "(\"unsupported HCL block %q\", blockHeader)")
	g.P("\t}")
	g.P("}")
	g.P()

	g.P("func parseDorisAtlasIndexBlock(idxName, body string) (DorisIndexDescriptor, error) {")
	g.P("\tidx := DorisIndexDescriptor{Name: idxName, Properties: make(map[string]string)}")
	g.P("\tvar outerLines []string")
	g.P("\ti := 0")
	g.P("\tfor i < len(body) {")
	g.P("\t\tfor i < len(body) && (body[i] == ' ' || body[i] == '\\t' || body[i] == '\\n' || body[i] == '\\r') {")
	g.P("\t\t\ti++")
	g.P("\t\t}")
	g.P("\t\tif i >= len(body) {")
	g.P("\t\t\tbreak")
	g.P("\t\t}")
	g.P("\t\tlineEnd := ", stringsIndexByte, "(body[i:], '\\n')")
	g.P("\t\tvar line string")
	g.P("\t\tif lineEnd < 0 {")
	g.P("\t\t\tline = body[i:]")
	g.P("\t\t} else {")
	g.P("\t\t\tline = body[i : i+lineEnd]")
	g.P("\t\t}")
	g.P("\t\ttrimmed := ", stringsTrimSpace, "(line)")
	g.P("\t\tif ", stringsContains, "(trimmed, \"{\") {")
	g.P("\t\t\topenRel := ", stringsIndexByte, "(body[i:], '{')")
	g.P("\t\t\topenAbs := i + openRel")
	g.P("\t\t\tsubHeader := ", stringsTrimSpace, "(", stringsTrimSuffix, "(", stringsTrimSpace, "(body[i:openAbs]), \"=\"))")
	g.P("\t\t\tcloseAbs, err := findMatchingHCLBrace(body, openAbs)")
	g.P("\t\t\tif err != nil {")
	g.P("\t\t\t\treturn idx, ", fmtErrorf, "(\"index %q: %w\", idxName, err)")
	g.P("\t\t\t}")
	g.P("\t\t\tsubBody := body[openAbs+1 : closeAbs]")
	g.P("\t\t\ti = closeAbs + 1")
	g.P("\t\t\tif subHeader != \"properties\" {")
	g.P("\t\t\t\treturn idx, ", fmtErrorf, "(\"index %q: unsupported sub-block %q\", idxName, subHeader)")
	g.P("\t\t\t}")
	g.P("\t\t\tpropsKV, err := parseHCLKeyValueAssignments(subBody)")
	g.P("\t\t\tif err != nil {")
	g.P("\t\t\t\treturn idx, ", fmtErrorf, "(\"index %q properties: %w\", idxName, err)")
	g.P("\t\t\t}")
	g.P("\t\t\tfor k, v := range propsKV {")
	g.P("\t\t\t\tidx.Properties[k] = unquoteHCLString(v)")
	g.P("\t\t\t}")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\touterLines = append(outerLines, trimmed)")
	g.P("\t\tif lineEnd < 0 {")
	g.P("\t\t\tbreak")
	g.P("\t\t}")
	g.P("\t\ti += lineEnd + 1")
	g.P("\t}")
	g.P("\tkv, err := parseHCLKeyValueAssignments(", stringsJoin, "(outerLines, \"\\n\"))")
	g.P("\tif err != nil {")
	g.P("\t\treturn idx, ", fmtErrorf, "(\"index %q: %w\", idxName, err)")
	g.P("\t}")
	g.P("\tif colsRaw, ok := kv[\"columns\"]; ok {")
	g.P("\t\tcols, cErr := parseHCLColumnRefList(colsRaw)")
	g.P("\t\tif cErr != nil {")
	g.P("\t\t\treturn idx, ", fmtErrorf, "(\"index %q: %w\", idxName, cErr)")
	g.P("\t\t}")
	g.P("\t\tidx.Columns = cols")
	g.P("\t}")
	g.P("\tif usingRaw, ok := kv[\"using\"]; ok {")
	g.P("\t\tidx.Using = ", stringsToUpper, "(unquoteHCLString(usingRaw))")
	g.P("\t}")
	g.P("\treturn idx, nil")
	g.P("}")
	g.P()
}
