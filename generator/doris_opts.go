package generator

import (
	"fmt"
	"math"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

const (
	// proto2typeExtensionNumber is the permanent proto2type option extension (1307).
	proto2typeExtensionNumber protowire.Number = 1307
	// azraColumnMetaExtensionNumber is azra.common.v1.column_meta on FieldOptions (50200).
	azraColumnMetaExtensionNumber protowire.Number = 50200
	// azraTableMetaExtensionNumber is azra.common.v1.table_meta on MessageOptions (50201).
	azraTableMetaExtensionNumber protowire.Number = 50201
)

// DorisTableMetaIR holds Apache Doris 4.x and Apache Iceberg table-level
// metadata extracted from either (proto2type.message).table (extension 1307)
// or (azra.common.v1.table_meta) (extension 50201).
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
		m.FileCacheTTLSeconds == 0
}

// DorisColumnMetaIR holds Apache Doris 4.x and Apache Iceberg column-level
// metadata extracted from either (proto2type.field).column (extension 1307)
// or (azra.common.v1.column_meta) (extension 50200).
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

// databaseScopeFromEnumNumber maps azra.common.v1.LakehouseDatabaseScope enum numbers.
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

// syncTierFromEnumNumber maps azra.common.v1.LakehouseSyncTier enum numbers.
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
	s = strings.TrimSpace(s)
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

// extractDorisTableMeta extracts Doris/Iceberg table metadata from a message's options.
// It inspects both native (proto2type.message).table (extension 1307) and
// wire-level extension 50201 (azra.common.v1.table_meta). Returns nil if neither is set.
func extractDorisTableMeta(msg *protogen.Message) *DorisTableMetaIR {
	if msg == nil || msg.Desc == nil {
		return nil
	}

	// 1. Check typed (proto2type.message).table extension first.
	if mo := getMessageOptions(msg); mo.GetTable() != nil {
		t := mo.GetTable()
		out := &DorisTableMetaIR{
			TableName:            t.GetTableName(),
			DatabaseScope:        normalizeDatabaseScope(t.GetDatabaseScope()),
			SyncTier:             normalizeSyncTier(t.GetSyncTier()),
			UniqueKeyColumns:     append([]string(nil), t.GetUniqueKeyColumns()...),
			PartitionByColumns:   append([]string(nil), t.GetPartitionByColumns()...),
			ColocateGroup:        t.GetColocateGroup(),
			SequenceCol:          t.GetSequenceCol(),
			InvertedIndexColumns: append([]string(nil), t.GetInvertedIndexColumns()...),
			NgramBfColumns:       append([]string(nil), t.GetNgramBfColumns()...),
			BloomFilterColumns:   append([]string(nil), t.GetBloomFilterColumns()...),
			DistributedByColumn:  t.GetDistributedByColumn(),
			DuplicateKeyColumns:  append([]string(nil), t.GetDuplicateKeyColumns()...),
			FileCacheTTLSeconds:  t.GetFileCacheTtlSeconds(),
		}
		if out.isEmpty() {
			return nil
		}
		return out
	}

	// 2. Inspect wire bytes on MessageOptions for extension 50201 (or fallback 1307.2).
	opts := msg.Desc.Options()
	if opts == nil {
		return nil
	}
	raw, err := proto.Marshal(opts)
	if err != nil || len(raw) == 0 {
		return nil
	}
	return parseDorisTableMetaFromOptionsWire(raw)
}

// parseDorisTableMetaFromOptionsWire scans marshaled MessageOptions wire bytes
// for tag 50201 (azra.common.v1.table_meta) or tag 1307 field 2 ((proto2type.message).table).
func parseDorisTableMetaFromOptionsWire(raw []byte) *DorisTableMetaIR {
	var out *DorisTableMetaIR
	b := raw
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			break
		}
		b = b[n:]

		if typ == protowire.BytesType && num == azraTableMetaExtensionNumber {
			val, m := protowire.ConsumeBytes(b)
			if m < 0 {
				break
			}
			b = b[m:]
			if out == nil {
				out = &DorisTableMetaIR{}
			}
			parseTableMetaBytes(val, out)
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
					if out == nil {
						out = &DorisTableMetaIR{}
					}
					parseTableMetaBytes(tval, out)
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
	if out.isEmpty() {
		return nil
	}
	return out
}

// parseTableMetaBytes decodes a serialized TableMeta / TableOptions message into dst.
func parseTableMetaBytes(b []byte, dst *DorisTableMetaIR) {
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
		case 2: // database_scope = 2 (enum varint in azra.common.v1.TableMeta, or string in proto2type.TableOptions)
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
		case 3: // sync_tier = 3 (enum varint in azra.common.v1.TableMeta, or string in proto2type.TableOptions)
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

// extractDorisColumnMeta extracts Doris/Iceberg column metadata from a field's options.
// It inspects both native (proto2type.field).column (extension 1307) and
// wire-level extension 50200 (azra.common.v1.column_meta). Returns nil if neither is set.
func extractDorisColumnMeta(field *protogen.Field) *DorisColumnMetaIR {
	if field == nil || field.Desc == nil {
		return nil
	}

	// 1. Check typed (proto2type.field).column extension first.
	if fo := getFieldOptions(field); fo.GetColumn() != nil {
		c := fo.GetColumn()
		return &DorisColumnMetaIR{
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
		}
	}

	// 2. Inspect wire bytes on FieldOptions for extension 50200 (or fallback 1307.10).
	opts := field.Desc.Options()
	if opts == nil {
		return nil
	}
	raw, err := proto.Marshal(opts)
	if err != nil || len(raw) == 0 {
		return nil
	}
	return parseDorisColumnMetaFromOptionsWire(raw)
}

// parseDorisColumnMetaFromOptionsWire scans marshaled FieldOptions wire bytes
// for tag 50200 (azra.common.v1.column_meta) or tag 1307 field 10 ((proto2type.field).column).
func parseDorisColumnMetaFromOptionsWire(raw []byte) *DorisColumnMetaIR {
	var out *DorisColumnMetaIR
	b := raw
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			break
		}
		b = b[n:]

		if typ == protowire.BytesType && num == azraColumnMetaExtensionNumber {
			val, m := protowire.ConsumeBytes(b)
			if m < 0 {
				break
			}
			b = b[m:]
			if out == nil {
				out = &DorisColumnMetaIR{}
			}
			parseColumnMetaBytes(val, out)
			continue
		}

		if typ == protowire.BytesType && num == proto2typeExtensionNumber {
			val, m := protowire.ConsumeBytes(b)
			if m < 0 {
				break
			}
			b = b[m:]
			// Check field 10 (ColumnOptions column = 10) inside FieldOptions
			inner := val
			for len(inner) > 0 {
				inum, ityp, in := protowire.ConsumeTag(inner)
				if in < 0 {
					break
				}
				inner = inner[in:]
				if ityp == protowire.BytesType && inum == 10 {
					cval, cm := protowire.ConsumeBytes(inner)
					if cm < 0 {
						break
					}
					inner = inner[cm:]
					if out == nil {
						out = &DorisColumnMetaIR{}
					}
					parseColumnMetaBytes(cval, out)
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
		}

		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			break
		}
		b = b[m:]
	}
}
