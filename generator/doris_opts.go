package generator

import (
	"fmt"
	"math"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	proto2typepb "github.com/protocgen/proto2type/proto/proto2type"
)

const (
	// proto2typeExtensionNumber is the permanent proto2type option extension (1307).
	proto2typeExtensionNumber protowire.Number = 1307
	// legacyColumnMetaExtensionNumber is the wire-compatible legacy column_meta extension on FieldOptions (50200).
	legacyColumnMetaExtensionNumber protowire.Number = 50200
	// legacyTableMetaExtensionNumber is the wire-compatible legacy table_meta extension on MessageOptions (50201).
	legacyTableMetaExtensionNumber protowire.Number = 50201
)

// IndexDefIR holds a relational index definition from TableOptions.indexes.
type IndexDefIR struct {
	Name    string
	Columns []string
	Unique  bool
}

// ForeignKeyIR holds a relational foreign key reference from ColumnOptions / FieldOptions.
type ForeignKeyIR struct {
	RefTable  string
	RefColumn string
	OnDelete  string
	OnUpdate  string
}

// DorisTableMetaIR holds Apache Doris 4.x, Apache Iceberg, and PostgreSQL table-level
// metadata extracted from either (proto2type.message).table (extension 1307)
// or legacy table_meta (extension 50201).
type DorisTableMetaIR struct {
	TableName            string
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
	StorageVaultName     string
	IcebergCatalog       string
	EmbeddingDimension   int32
	WriterService        string
	ReaderServices       []string
	PrimaryKeyFields     []string
	Indexes              []IndexDefIR
}

// isEmpty reports whether m is nil or has all zero-valued fields.
func (m *DorisTableMetaIR) isEmpty() bool {
	if m == nil {
		return true
	}
	return m.TableName == "" &&
		m.DatabaseScope == "" &&
		m.SyncTier == "" &&
		len(m.UniqueKeyColumns) == 0 &&
		len(m.PartitionByColumns) == 0 &&
		m.ColocateGroup == "" &&
		m.SequenceCol == "" &&
		len(m.InvertedIndexColumns) == 0 &&
		len(m.NgramBfColumns) == 0 &&
		len(m.BloomFilterColumns) == 0 &&
		m.DistributedByColumn == "" &&
		len(m.DuplicateKeyColumns) == 0 &&
		m.FileCacheTTLSeconds == 0 &&
		m.StorageVaultName == "" &&
		m.IcebergCatalog == "" &&
		m.EmbeddingDimension == 0 &&
		m.WriterService == "" &&
		len(m.ReaderServices) == 0 &&
		len(m.PrimaryKeyFields) == 0 &&
		len(m.Indexes) == 0
}

// hasDorisSpecificFields reports whether m has any Doris, StarRocks, or Iceberg
// table-level option set (excluding PostgreSQL-only and service-ownership-only fields).
func (m *DorisTableMetaIR) hasDorisSpecificFields() bool {
	if m == nil {
		return false
	}
	if m.DatabaseScope != "" ||
		m.SyncTier != "" ||
		len(m.UniqueKeyColumns) > 0 ||
		len(m.PartitionByColumns) > 0 ||
		m.ColocateGroup != "" ||
		m.SequenceCol != "" ||
		len(m.InvertedIndexColumns) > 0 ||
		len(m.NgramBfColumns) > 0 ||
		len(m.BloomFilterColumns) > 0 ||
		m.DistributedByColumn != "" ||
		len(m.DuplicateKeyColumns) > 0 ||
		m.FileCacheTTLSeconds != 0 ||
		m.StorageVaultName != "" ||
		m.IcebergCatalog != "" ||
		m.EmbeddingDimension != 0 {
		return true
	}
	return m.TableName != "" && len(m.PrimaryKeyFields) == 0 && len(m.Indexes) == 0
}

// DorisColumnMetaIR holds Apache Doris 4.x, Apache Iceberg, and PostgreSQL column-level
// metadata extracted from either (proto2type.field).column (extension 1307)
// or legacy column_meta (extension 50200).
type DorisColumnMetaIR struct {
	DorisType       string
	IcebergType     string
	InvertedIndex   bool
	IsUniqueKey     bool
	IsSequenceCol   bool
	InlineNested    bool
	IsPartitionKey  bool
	StripEnumPrefix bool
	ColumnName      string
	DefaultValue    string
	SQLType         string
	JSONB           bool
	Length          int32
	PrimaryKey      bool
	Index           bool
	Unique          bool
	Nullable        *bool
	ForeignKey      *ForeignKeyIR
}

// isEmpty reports whether m is nil or has no Doris/Iceberg column metadata set.
// PostgreSQL-only fields (SQLType, JSONB, Length, PrimaryKey, Index, Unique,
// Nullable, ForeignKey) are intentionally excluded so Postgres-annotated fields
// do not trigger Doris column filtering in messageHasAnnotatedDorisColumns.
func (m *DorisColumnMetaIR) isEmpty() bool {
	if m == nil {
		return true
	}
	return m.DorisType == "" &&
		m.IcebergType == "" &&
		!m.InvertedIndex &&
		!m.IsUniqueKey &&
		!m.IsSequenceCol &&
		!m.InlineNested &&
		!m.IsPartitionKey &&
		!m.StripEnumPrefix &&
		m.ColumnName == "" &&
		m.DefaultValue == ""
}

// isAllEmpty reports whether m is nil or has all zero-valued Doris, Iceberg,
// and PostgreSQL column metadata fields.
func (m *DorisColumnMetaIR) isAllEmpty() bool {
	if m == nil {
		return true
	}
	if !m.isEmpty() {
		return false
	}
	return m.SQLType == "" &&
		!m.JSONB &&
		m.Length == 0 &&
		!m.PrimaryKey &&
		!m.Index &&
		!m.Unique &&
		m.Nullable == nil &&
		m.ForeignKey == nil
}

// normalizeDatabaseScope normalizes raw scope strings or enum names to canonical
// short scope names ("TENANT", "GLOBAL_TERMINOLOGY", "GLOBAL_TERMINOLOGY_RAG", "GLOBAL_TELEMETRY").
func normalizeDatabaseScope(scope string) string {
	s := strings.TrimSpace(strings.ToUpper(scope))
	s = strings.TrimPrefix(s, "LAKEHOUSE_DATABASE_SCOPE_")
	if s == "UNSPECIFIED" {
		return ""
	}
	return s
}

// databaseScopeFromEnumNumber maps LakehouseDatabaseScope enum numbers.
func databaseScopeFromEnumNumber(n uint64) string {
	switch n {
	case 0:
		return ""
	case 1:
		return "TENANT"
	case 2:
		return "GLOBAL_TERMINOLOGY"
	case 3:
		return "GLOBAL_TERMINOLOGY_RAG"
	case 4:
		return "GLOBAL_TELEMETRY"
	default:
		return fmt.Sprintf("INVALID_DATABASE_SCOPE_%d", n)
	}
}

// normalizeSyncTier normalizes raw sync tier strings or enum names to canonical
// short tier names ("WHOLE_TABLE", "MONTHLY_PARTITIONED", "RELEASE_PARTITIONED", "INTERNAL_ONLY", "ICEBERG_ONLY").
func normalizeSyncTier(tier string) string {
	s := strings.TrimSpace(strings.ToUpper(tier))
	s = strings.TrimPrefix(s, "LAKEHOUSE_SYNC_TIER_")
	if s == "UNSPECIFIED" {
		return ""
	}
	return s
}

// syncTierFromEnumNumber maps LakehouseSyncTier enum numbers.
func syncTierFromEnumNumber(n uint64) string {
	switch n {
	case 0:
		return ""
	case 1:
		return "WHOLE_TABLE"
	case 2:
		return "MONTHLY_PARTITIONED"
	case 3:
		return "RELEASE_PARTITIONED"
	case 4:
		return "INTERNAL_ONLY"
	case 5:
		return "ICEBERG_ONLY"
	default:
		return fmt.Sprintf("INVALID_SYNC_TIER_%d", n)
	}
}

// isValidDatabaseScope reports whether scope is a recognized lakehouse database scope.
func isValidDatabaseScope(scope string) bool {
	switch scope {
	case "", "TENANT", "GLOBAL_TERMINOLOGY", "GLOBAL_TERMINOLOGY_RAG", "GLOBAL_TELEMETRY":
		return true
	default:
		return false
	}
}

// isValidSyncTier reports whether tier is a recognized lakehouse sync tier.
func isValidSyncTier(tier string) bool {
	switch tier {
	case "", "WHOLE_TABLE", "MONTHLY_PARTITIONED", "RELEASE_PARTITIONED", "INTERNAL_ONLY", "ICEBERG_ONLY":
		return true
	default:
		return false
	}
}

// isValidSQLIdentifier reports whether s matches ^[a-zA-Z_][a-zA-Z0-9_]*$.
func isValidSQLIdentifier(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
		isDigit := c >= '0' && c <= '9'
		if i == 0 && !isAlpha {
			return false
		}
		if !isAlpha && !isDigit {
			return false
		}
	}
	return true
}

// isValidSQLTypeSyntax reports whether s is a safe SQL column type expression
// matching ^[A-Za-z][A-Za-z0-9_<>, ()]*$ with balanced parentheses/angle brackets,
// numeric-only parenthesized type parameters, commas only inside (...) or <...>,
// spaces only immediately after commas inside (...) or <...> (preventing injected
// column constraints like "INT NOT NULL" or "ARRAY<INT NOT NULL>"), and no SQL comments.
func isValidSQLTypeSyntax(s string) bool {
	if len(s) == 0 {
		return false
	}
	firstAlpha := (s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z')
	if !firstAlpha {
		return false
	}
	parenDepth := 0
	angleDepth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '(':
			if parenDepth > 0 {
				return false
			}
			parenDepth++
		case ')':
			if i > 0 && (s[i-1] == '(' || s[i-1] == ',' || s[i-1] == ' ') {
				return false
			}
			parenDepth--
			if parenDepth < 0 {
				return false
			}
		case '<':
			if parenDepth > 0 {
				return false
			}
			angleDepth++
		case '>':
			if parenDepth > 0 || (i > 0 && (s[i-1] == '<' || s[i-1] == ',' || s[i-1] == ' ')) {
				return false
			}
			angleDepth--
			if angleDepth < 0 {
				return false
			}
		case ',':
			if (parenDepth == 0 && angleDepth == 0) || i == 0 || s[i-1] == '(' || s[i-1] == '<' || s[i-1] == ',' || s[i-1] == ' ' {
				return false
			}
		case ' ':
			if (parenDepth == 0 && angleDepth == 0) || i == 0 || s[i-1] != ',' {
				return false
			}
		default:
			if parenDepth > 0 {
				if c < '0' || c > '9' {
					return false
				}
			} else {
				ok := (c >= 'a' && c <= 'z') ||
					(c >= 'A' && c <= 'Z') ||
					(c >= '0' && c <= '9') ||
					c == '_'
				if !ok {
					return false
				}
			}
		}
	}
	return parenDepth == 0 && angleDepth == 0
}

// isValidDorisDefaultValue reports whether s is a safe SQL column default value literal
// containing only printable ASCII characters without quotes, backslashes, or semicolons.
func isValidDorisDefaultValue(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c > 0x7e {
			return false
		}
		switch c {
		case '"', '\'', '`', '\\', ';':
			return false
		}
	}
	return true
}

// isDorisKeyEligibleType reports whether dorisType is permitted by Apache Doris
// in a UNIQUE KEY, DUPLICATE KEY, or DISTRIBUTED BY HASH clause.
// Apache Doris forbids FLOAT, DOUBLE, STRING, TEXT, JSON, JSONB, VARIANT, HLL,
// BITMAP, QUANTILE_STATE, ARRAY<...>, MAP<...>, and STRUCT<...> as key or hash distribution columns.
func isDorisKeyEligibleType(dorisType string) bool {
	dt := strings.ToUpper(strings.TrimSpace(dorisType))
	if dt == "" {
		return false
	}
	switch dt {
	case "FLOAT", "DOUBLE", "STRING", "TEXT", "JSON", "JSONB", "VARIANT", "HLL", "BITMAP", "QUANTILE_STATE":
		return false
	}
	return !strings.HasPrefix(dt, "ARRAY<") &&
		!strings.HasPrefix(dt, "MAP<") &&
		!strings.HasPrefix(dt, "STRUCT<")
}

// isValidServiceIdentifier reports whether s matches ^[a-z][a-z0-9_-]*$.
func isValidServiceIdentifier(s string) bool {
	if len(s) == 0 {
		return false
	}
	if s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

// validateTableServiceMeta validates WriterService and ReaderServices on table metadata.
func validateTableServiceMeta(msgName string, meta *DorisTableMetaIR) error {
	if meta == nil {
		return nil
	}
	if meta.WriterService != "" && !isValidServiceIdentifier(meta.WriterService) {
		return fmt.Errorf("message %s: invalid writer_service %q (must match ^[a-z][a-z0-9_-]*$)", msgName, meta.WriterService)
	}
	for _, rs := range meta.ReaderServices {
		if rs != "*" && !isValidServiceIdentifier(rs) {
			return fmt.Errorf("message %s: invalid reader_services entry %q (must be \"*\" or match ^[a-z][a-z0-9_-]*$)", msgName, rs)
		}
	}
	return nil
}

// PostgresPrefixedTableName resolves a PostgreSQL physical table name from table metadata
// and a default snake_case message name:
//   - If meta.TableName != "" -> meta.TableName (explicit override wins)
//   - If meta.TableName == "" && meta.WriterService != "" -> "<norm_writer>__<defaultName>"
//   - Otherwise -> defaultName
func PostgresPrefixedTableName(meta *DorisTableMetaIR, defaultName string) string {
	if meta != nil {
		if name := strings.TrimSpace(meta.TableName); name != "" {
			return name
		}
		if ws := strings.TrimSpace(meta.WriterService); ws != "" {
			normWriter := strings.ReplaceAll(ws, "-", "_")
			prefix := normWriter + "__"
			if strings.HasPrefix(defaultName, prefix) {
				return defaultName
			}
			return prefix + defaultName
		}
	}
	return defaultName
}

// resolvePostgresTableName resolves the PostgreSQL physical table name for a proto message.
func resolvePostgresTableName(msg *protogen.Message, meta *DorisTableMetaIR) string {
	defaultName := ""
	if msg != nil {
		defaultName = toSnakeCase(msg.GoIdent.GoName)
	}
	return PostgresPrefixedTableName(meta, defaultName)
}

// trimNonEmptyStrings trims whitespace from each element of in and omits empty strings.
func trimNonEmptyStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	var out []string
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// mergeUniqueStrings appends elements from src into dst, preserving order and deduplicating.
func mergeUniqueStrings(dst, src []string) []string {
	if len(src) == 0 {
		return dst
	}
	seen := make(map[string]bool, len(dst)+len(src))
	var out []string
	for _, s := range dst {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range src {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// mergeTableMeta merges non-empty fields from src into dst without duplicating slices.
func mergeTableMeta(dst, src *DorisTableMetaIR) {
	if dst == nil || src == nil {
		return
	}
	if dst.TableName == "" && src.TableName != "" {
		dst.TableName = src.TableName
	}
	if dst.DatabaseScope == "" && src.DatabaseScope != "" {
		dst.DatabaseScope = src.DatabaseScope
	}
	if dst.SyncTier == "" && src.SyncTier != "" {
		dst.SyncTier = src.SyncTier
	}
	dst.UniqueKeyColumns = mergeUniqueStrings(dst.UniqueKeyColumns, src.UniqueKeyColumns)
	dst.PartitionByColumns = mergeUniqueStrings(dst.PartitionByColumns, src.PartitionByColumns)
	if dst.ColocateGroup == "" && src.ColocateGroup != "" {
		dst.ColocateGroup = src.ColocateGroup
	}
	if dst.SequenceCol == "" && src.SequenceCol != "" {
		dst.SequenceCol = src.SequenceCol
	}
	dst.InvertedIndexColumns = mergeUniqueStrings(dst.InvertedIndexColumns, src.InvertedIndexColumns)
	dst.NgramBfColumns = mergeUniqueStrings(dst.NgramBfColumns, src.NgramBfColumns)
	dst.BloomFilterColumns = mergeUniqueStrings(dst.BloomFilterColumns, src.BloomFilterColumns)
	if dst.DistributedByColumn == "" && src.DistributedByColumn != "" {
		dst.DistributedByColumn = src.DistributedByColumn
	}
	dst.DuplicateKeyColumns = mergeUniqueStrings(dst.DuplicateKeyColumns, src.DuplicateKeyColumns)
	if dst.FileCacheTTLSeconds == 0 && src.FileCacheTTLSeconds != 0 {
		dst.FileCacheTTLSeconds = src.FileCacheTTLSeconds
	}
	if dst.StorageVaultName == "" && src.StorageVaultName != "" {
		dst.StorageVaultName = src.StorageVaultName
	}
	if dst.IcebergCatalog == "" && src.IcebergCatalog != "" {
		dst.IcebergCatalog = src.IcebergCatalog
	}
	if dst.EmbeddingDimension == 0 && src.EmbeddingDimension != 0 {
		dst.EmbeddingDimension = src.EmbeddingDimension
	}
	if dst.WriterService == "" && src.WriterService != "" {
		dst.WriterService = src.WriterService
	}
	dst.ReaderServices = mergeUniqueStrings(dst.ReaderServices, src.ReaderServices)
	dst.PrimaryKeyFields = mergeUniqueStrings(dst.PrimaryKeyFields, src.PrimaryKeyFields)
	for _, idx := range src.Indexes {
		exists := false
		for _, existing := range dst.Indexes {
			if existing.Name == idx.Name && strings.Join(existing.Columns, ",") == strings.Join(idx.Columns, ",") && existing.Unique == idx.Unique {
				exists = true
				break
			}
		}
		if !exists {
			dst.Indexes = append(dst.Indexes, idx)
		}
	}
}

// extractTableMeta is an alias for extractDorisTableMeta, returning table-level
// metadata for any relational or lakehouse backend (Postgres, Doris, StarRocks, Iceberg).
func extractTableMeta(msg *protogen.Message) *TableMetaConfig {
	return extractDorisTableMeta(msg)
}

// extractColumnMeta extracts column-level metadata (including PostgreSQL, Doris, StarRocks,
// and Iceberg options) from a field's options.
func extractColumnMeta(field *protogen.Field) *ColumnConfig {
	return extractColumnMetaInternal(field, true)
}

// extractDorisTableMeta extracts Doris/Iceberg/Postgres table metadata from a message's options.
// It inspects both native (proto2type.message).table (extension 1307) and
// wire-level legacy extension 50201 (table_meta). Returns nil if neither is set.
func extractDorisTableMeta(msg *protogen.Message) *DorisTableMetaIR {
	if msg == nil || msg.Desc == nil {
		return nil
	}

	var typedOut *DorisTableMetaIR
	// 1. Check typed (proto2type.message).table extension first.
	if mo := getMessageOptions(msg); mo.GetTable() != nil {
		t := mo.GetTable()
		var indexes []IndexDefIR
		for _, idx := range t.GetIndexes() {
			if idx == nil {
				continue
			}
			cols := trimNonEmptyStrings(idx.GetColumns())
			name := strings.TrimSpace(idx.GetName())
			if len(cols) > 0 || name != "" {
				indexes = append(indexes, IndexDefIR{
					Name:    name,
					Columns: cols,
					Unique:  idx.GetUnique(),
				})
			}
		}
		out := &DorisTableMetaIR{
			TableName:            t.GetTableName(),
			DatabaseScope:        normalizeDatabaseScope(t.GetDatabaseScope()),
			SyncTier:             normalizeSyncTier(t.GetSyncTier()),
			UniqueKeyColumns:     mergeUniqueStrings(nil, t.GetUniqueKeyColumns()),
			PartitionByColumns:   mergeUniqueStrings(nil, t.GetPartitionByColumns()),
			ColocateGroup:        t.GetColocateGroup(),
			SequenceCol:          t.GetSequenceCol(),
			InvertedIndexColumns: mergeUniqueStrings(nil, t.GetInvertedIndexColumns()),
			NgramBfColumns:       mergeUniqueStrings(nil, t.GetNgramBfColumns()),
			BloomFilterColumns:   mergeUniqueStrings(nil, t.GetBloomFilterColumns()),
			DistributedByColumn:  t.GetDistributedByColumn(),
			DuplicateKeyColumns:  mergeUniqueStrings(nil, t.GetDuplicateKeyColumns()),
			FileCacheTTLSeconds:  t.GetFileCacheTtlSeconds(),
			StorageVaultName:     t.GetStorageVaultName(),
			IcebergCatalog:       t.GetIcebergCatalog(),
			EmbeddingDimension:   t.GetEmbeddingDimension(),
			WriterService:        strings.TrimSpace(t.GetWriterService()),
			ReaderServices:       mergeUniqueStrings(nil, trimNonEmptyStrings(t.GetReaderServices())),
			PrimaryKeyFields:     mergeUniqueStrings(nil, trimNonEmptyStrings(t.GetPrimaryKeyFields())),
			Indexes:              indexes,
		}
		if !out.isEmpty() {
			typedOut = out
		}
	}

	// 2. Inspect wire bytes on MessageOptions for extension 50201 (or fallback 1307.2).
	opts := msg.Desc.Options()
	if opts == nil {
		return typedOut
	}
	raw, err := proto.Marshal(opts)
	if err != nil || len(raw) == 0 {
		return typedOut
	}
	wireOut := parseDorisTableMetaFromOptionsWire(raw)
	if typedOut != nil {
		if wireOut != nil {
			mergeTableMeta(typedOut, wireOut)
		}
		return typedOut
	}
	if wireOut != nil {
		wireOut.UniqueKeyColumns = mergeUniqueStrings(nil, wireOut.UniqueKeyColumns)
		wireOut.PartitionByColumns = mergeUniqueStrings(nil, wireOut.PartitionByColumns)
		wireOut.InvertedIndexColumns = mergeUniqueStrings(nil, wireOut.InvertedIndexColumns)
		wireOut.NgramBfColumns = mergeUniqueStrings(nil, wireOut.NgramBfColumns)
		wireOut.BloomFilterColumns = mergeUniqueStrings(nil, wireOut.BloomFilterColumns)
		wireOut.DuplicateKeyColumns = mergeUniqueStrings(nil, wireOut.DuplicateKeyColumns)
		wireOut.ReaderServices = mergeUniqueStrings(nil, wireOut.ReaderServices)
		wireOut.PrimaryKeyFields = mergeUniqueStrings(nil, wireOut.PrimaryKeyFields)
	}
	return wireOut
}

// parseDorisTableMetaFromOptionsWire scans marshaled MessageOptions wire bytes
// for tag 50201 (legacy table_meta) or tag 1307 field 2 ((proto2type.message).table).
func parseDorisTableMetaFromOptionsWire(raw []byte) *DorisTableMetaIR {
	var legacyOut *DorisTableMetaIR
	var ext1307Out *DorisTableMetaIR
	b := raw
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			break
		}
		b = b[n:]

		if typ == protowire.BytesType && num == legacyTableMetaExtensionNumber {
			val, m := protowire.ConsumeBytes(b)
			if m < 0 {
				break
			}
			b = b[m:]
			if legacyOut == nil {
				legacyOut = &DorisTableMetaIR{}
			}
			parseTableMetaBytesWithVault(val, legacyOut, false)
			continue
		}

		if typ == protowire.BytesType && num == proto2typeExtensionNumber {
			val, m := protowire.ConsumeBytes(b)
			if m < 0 {
				break
			}
			b = b[m:]
			// Check field 2 (TableOptions table = 2) inside MessageOptions
			inner := val
			for len(inner) > 0 {
				inum, ityp, in := protowire.ConsumeTag(inner)
				if in < 0 {
					break
				}
				inner = inner[in:]
				if ityp == protowire.BytesType && inum == 2 {
					tval, tm := protowire.ConsumeBytes(inner)
					if tm < 0 {
						break
					}
					inner = inner[tm:]
					if ext1307Out == nil {
						ext1307Out = &DorisTableMetaIR{}
					}
					parseTableMetaBytesWithVault(tval, ext1307Out, true)
				} else {
					im := protowire.ConsumeFieldValue(inum, ityp, inner)
					if im < 0 {
						break
					}
					inner = inner[im:]
				}
			}
			continue
		}

		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			break
		}
		b = b[m:]
	}

	var out *DorisTableMetaIR
	switch {
	case ext1307Out != nil && legacyOut != nil:
		mergeTableMeta(ext1307Out, legacyOut)
		out = ext1307Out
	case ext1307Out != nil:
		out = ext1307Out
	default:
		out = legacyOut
	}
	if out.isEmpty() {
		return nil
	}
	return out
}

// parseTableMetaBytes decodes a serialized TableMeta / TableOptions message into dst.
func parseTableMetaBytes(b []byte, dst *DorisTableMetaIR) {
	parseTableMetaBytesWithVault(b, dst, true)
}

// parseIndexDefBytes decodes a serialized IndexDef message into IndexDefIR.
func parseIndexDefBytes(b []byte) IndexDefIR {
	var idx IndexDefIR
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			break
		}
		b = b[n:]
		switch num {
		case 1: // string name = 1
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					idx.Name = strings.TrimSpace(string(v))
					b = b[m:]
					continue
				}
			}
		case 2: // repeated string columns = 2
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					if col := strings.TrimSpace(string(v)); col != "" {
						idx.Columns = append(idx.Columns, col)
					}
					b = b[m:]
					continue
				}
			}
		case 3: // bool unique = 3
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					idx.Unique = v != 0
					b = b[m:]
					continue
				}
			}
		}
		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			break
		}
		b = b[m:]
	}
	return idx
}

// parseForeignKeyBytes decodes a serialized ForeignKey message into *ForeignKeyIR.
func parseForeignKeyBytes(b []byte) *ForeignKeyIR {
	fk := &ForeignKeyIR{}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			break
		}
		b = b[n:]
		switch num {
		case 1: // string ref_table = 1
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					fk.RefTable = strings.TrimSpace(string(v))
					b = b[m:]
					continue
				}
			}
		case 2: // string ref_column = 2
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					fk.RefColumn = strings.TrimSpace(string(v))
					b = b[m:]
					continue
				}
			}
		case 3: // string on_delete = 3
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					fk.OnDelete = strings.TrimSpace(string(v))
					b = b[m:]
					continue
				}
			}
		case 4: // string on_update = 4
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					fk.OnUpdate = strings.TrimSpace(string(v))
					b = b[m:]
					continue
				}
			}
		}
		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			break
		}
		b = b[m:]
	}
	if fk.RefTable == "" && fk.RefColumn == "" && fk.OnDelete == "" && fk.OnUpdate == "" {
		return nil
	}
	return fk
}

// parseTableMetaBytesWithVault decodes a serialized TableMeta / TableOptions message into dst.
// When includeVaultAndCatalog is false (legacy extension 50201), fields 14 and 15
// decode writer_service and reader_services (from candela.types.TableMeta), whereas
// when includeVaultAndCatalog is true (extension 1307 proto2type.TableOptions),
// fields 14 and 15 decode storage_vault_name and iceberg_catalog and fields 17 and 18
// decode writer_service and reader_services.
func parseTableMetaBytesWithVault(b []byte, dst *DorisTableMetaIR, includeVaultAndCatalog bool) {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			break
		}
		b = b[n:]

		switch num {
		case 1: // string table_name = 1
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.TableName = string(v)
					b = b[m:]
					continue
				}
			}
		case 2: // database_scope = 2 (enum varint in legacy TableMeta, or string in proto2type.TableOptions)
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					dst.DatabaseScope = databaseScopeFromEnumNumber(v)
					b = b[m:]
					continue
				}
			} else if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.DatabaseScope = normalizeDatabaseScope(string(v))
					b = b[m:]
					continue
				}
			}
		case 3: // sync_tier = 3 (enum varint in legacy TableMeta, or string in proto2type.TableOptions)
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					dst.SyncTier = syncTierFromEnumNumber(v)
					b = b[m:]
					continue
				}
			} else if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.SyncTier = normalizeSyncTier(string(v))
					b = b[m:]
					continue
				}
			}
		case 4: // repeated string unique_key_columns = 4
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.UniqueKeyColumns = append(dst.UniqueKeyColumns, string(v))
					b = b[m:]
					continue
				}
			}
		case 5: // repeated string partition_by_columns = 5
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.PartitionByColumns = append(dst.PartitionByColumns, string(v))
					b = b[m:]
					continue
				}
			}
		case 6: // string colocate_group = 6
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.ColocateGroup = string(v)
					b = b[m:]
					continue
				}
			}
		case 7: // string sequence_col = 7
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.SequenceCol = string(v)
					b = b[m:]
					continue
				}
			}
		case 8: // repeated string inverted_index_columns = 8
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.InvertedIndexColumns = append(dst.InvertedIndexColumns, string(v))
					b = b[m:]
					continue
				}
			}
		case 9: // repeated string ngram_bf_columns = 9
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.NgramBfColumns = append(dst.NgramBfColumns, string(v))
					b = b[m:]
					continue
				}
			}
		case 10: // repeated string bloom_filter_columns = 10
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.BloomFilterColumns = append(dst.BloomFilterColumns, string(v))
					b = b[m:]
					continue
				}
			}
		case 11: // string distributed_by_column = 11
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.DistributedByColumn = string(v)
					b = b[m:]
					continue
				}
			}
		case 12: // repeated string duplicate_key_columns = 12
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.DuplicateKeyColumns = append(dst.DuplicateKeyColumns, string(v))
					b = b[m:]
					continue
				}
			}
		case 13: // int32 file_cache_ttl_seconds = 13
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					if v <= math.MaxInt32 {
						dst.FileCacheTTLSeconds = int32(v)
					} else if negMag := ^v + 1; negMag <= math.MaxInt32 {
						dst.FileCacheTTLSeconds = -int32(negMag) // #nosec G115 -- bounded by negMag <= math.MaxInt32
					} else if ^v+1 == 1<<31 {
						dst.FileCacheTTLSeconds = math.MinInt32
					}
					b = b[m:]
					continue
				}
			}
		case 14: // string storage_vault_name = 14 (1307) OR string writer_service = 14 (50201)
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					if includeVaultAndCatalog {
						dst.StorageVaultName = string(v)
					} else {
						dst.WriterService = strings.TrimSpace(string(v))
					}
					b = b[m:]
					continue
				}
			}
		case 15: // string iceberg_catalog = 15 (1307) OR repeated string reader_services = 15 (50201)
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					if includeVaultAndCatalog {
						dst.IcebergCatalog = string(v)
					} else if s := strings.TrimSpace(string(v)); s != "" {
						dst.ReaderServices = mergeUniqueStrings(dst.ReaderServices, []string{s})
					}
					b = b[m:]
					continue
				}
			}
		case 17: // string writer_service = 17 (TableOptions 1307)
			if includeVaultAndCatalog && typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.WriterService = strings.TrimSpace(string(v))
					b = b[m:]
					continue
				}
			}
		case 18: // repeated string reader_services = 18 (TableOptions 1307)
			if includeVaultAndCatalog && typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					if s := strings.TrimSpace(string(v)); s != "" {
						dst.ReaderServices = mergeUniqueStrings(dst.ReaderServices, []string{s})
					}
					b = b[m:]
					continue
				}
			}
		case 19: // repeated string primary_key_fields = 19 (TableOptions 1307)
			if includeVaultAndCatalog && typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					if s := strings.TrimSpace(string(v)); s != "" {
						dst.PrimaryKeyFields = mergeUniqueStrings(dst.PrimaryKeyFields, []string{s})
					}
					b = b[m:]
					continue
				}
			}
		case 20: // repeated IndexDef indexes = 20 (TableOptions 1307)
			if includeVaultAndCatalog && typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					if idx := parseIndexDefBytes(v); len(idx.Columns) > 0 || idx.Name != "" {
						dst.Indexes = append(dst.Indexes, idx)
					}
					b = b[m:]
					continue
				}
			}
		case 16: // int32 embedding_dimension = 16
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					if v <= math.MaxInt32 {
						dst.EmbeddingDimension = int32(v)
					} else if negMag := ^v + 1; negMag <= math.MaxInt32 {
						dst.EmbeddingDimension = -int32(negMag) // #nosec G115 -- bounded by negMag <= math.MaxInt32
					} else if ^v+1 == 1<<31 {
						dst.EmbeddingDimension = math.MinInt32
					}
					b = b[m:]
					continue
				}
			}
		}

		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			break
		}
		b = b[m:]
	}
}

// foreignKeyIRFromProto converts a protobuf ForeignKey option message into a ForeignKeyIR,
// returning nil if fk is nil or all of its fields are empty.
func foreignKeyIRFromProto(fk *proto2typepb.ForeignKey) *ForeignKeyIR {
	if fk == nil {
		return nil
	}
	out := &ForeignKeyIR{
		RefTable:  strings.TrimSpace(fk.GetRefTable()),
		RefColumn: strings.TrimSpace(fk.GetRefColumn()),
		OnDelete:  strings.TrimSpace(fk.GetOnDelete()),
		OnUpdate:  strings.TrimSpace(fk.GetOnUpdate()),
	}
	if out.RefTable == "" && out.RefColumn == "" && out.OnDelete == "" && out.OnUpdate == "" {
		return nil
	}
	return out
}

// mergeColumnMeta merges non-empty fields from src into dst without overwriting
// fields already set on dst.
func mergeColumnMeta(dst, src *DorisColumnMetaIR) {
	if dst == nil || src == nil {
		return
	}
	if dst.DorisType == "" && src.DorisType != "" {
		dst.DorisType = src.DorisType
	}
	if dst.IcebergType == "" && src.IcebergType != "" {
		dst.IcebergType = src.IcebergType
	}
	if !dst.InvertedIndex && src.InvertedIndex {
		dst.InvertedIndex = true
	}
	if !dst.IsUniqueKey && src.IsUniqueKey {
		dst.IsUniqueKey = true
	}
	if !dst.IsSequenceCol && src.IsSequenceCol {
		dst.IsSequenceCol = true
	}
	if !dst.InlineNested && src.InlineNested {
		dst.InlineNested = true
	}
	if !dst.IsPartitionKey && src.IsPartitionKey {
		dst.IsPartitionKey = true
	}
	if !dst.StripEnumPrefix && src.StripEnumPrefix {
		dst.StripEnumPrefix = true
	}
	if dst.ColumnName == "" && src.ColumnName != "" {
		dst.ColumnName = src.ColumnName
	}
	if dst.DefaultValue == "" && src.DefaultValue != "" {
		dst.DefaultValue = src.DefaultValue
	}
	if dst.SQLType == "" && src.SQLType != "" {
		dst.SQLType = src.SQLType
	}
	if !dst.JSONB && src.JSONB {
		dst.JSONB = true
	}
	if dst.Length == 0 && src.Length != 0 {
		dst.Length = src.Length
	}
	if !dst.PrimaryKey && src.PrimaryKey {
		dst.PrimaryKey = true
	}
	if !dst.Index && src.Index {
		dst.Index = true
	}
	if !dst.Unique && src.Unique {
		dst.Unique = true
	}
	if dst.Nullable == nil && src.Nullable != nil {
		b := *src.Nullable
		dst.Nullable = &b
	}
	if dst.ForeignKey == nil && src.ForeignKey != nil {
		fkCopy := *src.ForeignKey
		dst.ForeignKey = &fkCopy
	}
}

// extractDorisColumnMeta extracts Doris/Iceberg column metadata from a field's options.
// It inspects both native (proto2type.field).column (extension 1307) and
// wire-level legacy extension 50200 (column_meta), returning nil if no Doris/Iceberg
// column options are set.
func extractDorisColumnMeta(field *protogen.Field) *DorisColumnMetaIR {
	return extractColumnMetaInternal(field, false)
}

// extractColumnMetaInternal extracts column metadata from both typed (proto2type.field)
// and wire-level legacy extension 50200, merging non-empty fields so top-level FieldOptions
// shorthand does not shadow legacy 50200 metadata. When includePostgres is false,
// returns nil unless at least one Doris/Iceberg column field is non-empty.
func extractColumnMetaInternal(field *protogen.Field, includePostgres bool) *DorisColumnMetaIR {
	if field == nil || field.Desc == nil {
		return nil
	}

	var typedOut *DorisColumnMetaIR
	// 1. Check typed (proto2type.field) extension first.
	if fo := getFieldOptions(field); fo != nil {
		c := fo.GetColumn()
		var nullable *bool
		if c != nil {
			switch c.GetNullable() {
			case proto2typepb.OptionalBool_OPTIONAL_BOOL_TRUE:
				b := true
				nullable = &b
			case proto2typepb.OptionalBool_OPTIONAL_BOOL_FALSE:
				b := false
				nullable = &b
			}
		}
		fk := foreignKeyIRFromProto(c.GetForeignKey())
		if fk == nil {
			fk = foreignKeyIRFromProto(fo.GetForeignKey())
		}
		out := &DorisColumnMetaIR{
			DorisType:       c.GetDorisType(),
			IcebergType:     c.GetIcebergType(),
			InvertedIndex:   c.GetInvertedIndex(),
			IsUniqueKey:     c.GetIsUniqueKey(),
			IsSequenceCol:   c.GetIsSequenceCol(),
			InlineNested:    c.GetInlineNested(),
			IsPartitionKey:  c.GetIsPartitionKey(),
			StripEnumPrefix: c.GetStripEnumPrefix(),
			ColumnName:      c.GetColumnName(),
			DefaultValue:    c.GetDefaultValue(),
			SQLType:         strings.TrimSpace(c.GetSqlType()),
			JSONB:           c.GetJsonb(),
			Length:          c.GetLength(),
			PrimaryKey:      c.GetPrimaryKey() || fo.GetPrimaryKey(),
			Index:           c.GetIndex() || fo.GetIndex(),
			Unique:          c.GetUnique() || fo.GetUnique(),
			Nullable:        nullable,
			ForeignKey:      fk,
		}
		if !out.isAllEmpty() {
			typedOut = out
		}
	}

	// 2. Inspect wire bytes on FieldOptions for extension 50200 (or fallback 1307.10).
	var wireOut *DorisColumnMetaIR
	if opts := field.Desc.Options(); opts != nil {
		if raw, err := proto.Marshal(opts); err == nil && len(raw) > 0 {
			wireOut = parseDorisColumnMetaFromOptionsWire(raw)
		}
	}

	var merged *DorisColumnMetaIR
	switch {
	case typedOut != nil && wireOut != nil:
		mergeColumnMeta(typedOut, wireOut)
		merged = typedOut
	case typedOut != nil:
		merged = typedOut
	default:
		merged = wireOut
	}

	if includePostgres {
		if merged.isAllEmpty() {
			return nil
		}
		return merged
	}
	if merged.isEmpty() {
		return nil
	}
	return merged
}

// parseDorisColumnMetaFromOptionsWire scans marshaled FieldOptions wire bytes
// for tag 50200 (legacy column_meta) or tag 1307 field 10 ((proto2type.field).column).
func parseDorisColumnMetaFromOptionsWire(raw []byte) *DorisColumnMetaIR {
	var legacyOut *DorisColumnMetaIR
	var ext1307Out *DorisColumnMetaIR
	b := raw
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			break
		}
		b = b[n:]

		if typ == protowire.BytesType && num == legacyColumnMetaExtensionNumber {
			val, m := protowire.ConsumeBytes(b)
			if m < 0 {
				break
			}
			b = b[m:]
			if legacyOut == nil {
				legacyOut = &DorisColumnMetaIR{}
			}
			parseColumnMetaBytes(val, legacyOut)
			continue
		}

		if typ == protowire.BytesType && num == proto2typeExtensionNumber {
			val, m := protowire.ConsumeBytes(b)
			if m < 0 {
				break
			}
			b = b[m:]
			// Check field 10 (ColumnOptions column = 10) and top-level relational fields 11..14 inside FieldOptions
			inner := val
			var topLevelFK *ForeignKeyIR
			for len(inner) > 0 {
				inum, ityp, in := protowire.ConsumeTag(inner)
				if in < 0 {
					break
				}
				inner = inner[in:]
				switch {
				case ityp == protowire.BytesType && inum == 10:
					cval, cm := protowire.ConsumeBytes(inner)
					if cm < 0 {
						inner = nil
						break
					}
					inner = inner[cm:]
					if ext1307Out == nil {
						ext1307Out = &DorisColumnMetaIR{}
					}
					parseColumnMetaBytes(cval, ext1307Out)
				case ityp == protowire.VarintType && inum == 11: // bool primary_key = 11
					v, cm := protowire.ConsumeVarint(inner)
					if cm < 0 {
						inner = nil
						break
					}
					inner = inner[cm:]
					if ext1307Out == nil {
						ext1307Out = &DorisColumnMetaIR{}
					}
					if v != 0 {
						ext1307Out.PrimaryKey = true
					}
				case ityp == protowire.VarintType && inum == 12: // bool index = 12
					v, cm := protowire.ConsumeVarint(inner)
					if cm < 0 {
						inner = nil
						break
					}
					inner = inner[cm:]
					if ext1307Out == nil {
						ext1307Out = &DorisColumnMetaIR{}
					}
					if v != 0 {
						ext1307Out.Index = true
					}
				case ityp == protowire.VarintType && inum == 13: // bool unique = 13
					v, cm := protowire.ConsumeVarint(inner)
					if cm < 0 {
						inner = nil
						break
					}
					inner = inner[cm:]
					if ext1307Out == nil {
						ext1307Out = &DorisColumnMetaIR{}
					}
					if v != 0 {
						ext1307Out.Unique = true
					}
				case ityp == protowire.BytesType && inum == 14: // ForeignKey foreign_key = 14
					fval, cm := protowire.ConsumeBytes(inner)
					if cm < 0 {
						inner = nil
						break
					}
					inner = inner[cm:]
					if fk := parseForeignKeyBytes(fval); fk != nil {
						if ext1307Out == nil {
							ext1307Out = &DorisColumnMetaIR{}
						}
						topLevelFK = fk
					}
				default:
					im := protowire.ConsumeFieldValue(inum, ityp, inner)
					if im < 0 {
						inner = nil
						break
					}
					inner = inner[im:]
				}
			}
			if ext1307Out != nil && ext1307Out.ForeignKey == nil && topLevelFK != nil {
				ext1307Out.ForeignKey = topLevelFK
			}
			continue
		}

		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			break
		}
		b = b[m:]
	}
	var out *DorisColumnMetaIR
	switch {
	case ext1307Out != nil && legacyOut != nil:
		mergeColumnMeta(ext1307Out, legacyOut)
		out = ext1307Out
	case ext1307Out != nil:
		out = ext1307Out
	default:
		out = legacyOut
	}
	if out.isAllEmpty() {
		return nil
	}
	return out
}

// parseColumnMetaBytes decodes a serialized ColumnMeta / ColumnOptions message into dst.
func parseColumnMetaBytes(b []byte, dst *DorisColumnMetaIR) {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			break
		}
		b = b[n:]

		switch num {
		case 1: // string doris_type = 1
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.DorisType = string(v)
					b = b[m:]
					continue
				}
			}
		case 2: // string iceberg_type = 2
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.IcebergType = string(v)
					b = b[m:]
					continue
				}
			}
		case 3: // bool inverted_index = 3
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					dst.InvertedIndex = v != 0
					b = b[m:]
					continue
				}
			}
		case 4: // bool is_unique_key = 4
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					dst.IsUniqueKey = v != 0
					b = b[m:]
					continue
				}
			}
		case 5: // bool is_sequence_col = 5
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					dst.IsSequenceCol = v != 0
					b = b[m:]
					continue
				}
			}
		case 6: // bool inline_nested = 6
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					dst.InlineNested = v != 0
					b = b[m:]
					continue
				}
			}
		case 7: // bool is_partition_key = 7
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					dst.IsPartitionKey = v != 0
					b = b[m:]
					continue
				}
			}
		case 8: // bool strip_enum_prefix = 8
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					dst.StripEnumPrefix = v != 0
					b = b[m:]
					continue
				}
			}
		case 9: // string column_name = 9
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.ColumnName = string(v)
					b = b[m:]
					continue
				}
			}
		case 10: // string default_value = 10
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.DefaultValue = string(v)
					b = b[m:]
					continue
				}
			}
		case 11: // string sql_type = 11
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					dst.SQLType = strings.TrimSpace(string(v))
					b = b[m:]
					continue
				}
			}
		case 12: // bool jsonb = 12
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					dst.JSONB = v != 0
					b = b[m:]
					continue
				}
			}
		case 13: // int32 length = 13
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					if v <= math.MaxInt32 {
						dst.Length = int32(v)
					} else if negMag := ^v + 1; negMag <= math.MaxInt32 {
						dst.Length = -int32(negMag) // #nosec G115 -- bounded by negMag <= math.MaxInt32
					} else if ^v+1 == 1<<31 {
						dst.Length = math.MinInt32
					}
					b = b[m:]
					continue
				}
			}
		case 14: // bool primary_key = 14
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					dst.PrimaryKey = v != 0
					b = b[m:]
					continue
				}
			}
		case 15: // bool index = 15
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					dst.Index = v != 0
					b = b[m:]
					continue
				}
			}
		case 16: // bool unique = 16
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					dst.Unique = v != 0
					b = b[m:]
					continue
				}
			}
		case 17: // OptionalBool nullable = 17
			if typ == protowire.VarintType {
				if v, m := protowire.ConsumeVarint(b); m >= 0 {
					switch v {
					case 1:
						bv := true
						dst.Nullable = &bv
					case 2:
						bv := false
						dst.Nullable = &bv
					}
					b = b[m:]
					continue
				}
			}
		case 18: // ForeignKey foreign_key = 18
			if typ == protowire.BytesType {
				if v, m := protowire.ConsumeBytes(b); m >= 0 {
					if fk := parseForeignKeyBytes(v); fk != nil {
						dst.ForeignKey = fk
					}
					b = b[m:]
					continue
				}
			}
		}

		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			break
		}
		b = b[m:]
	}
}
