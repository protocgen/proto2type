package generator

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func escapeHCLInterpolation(s string) string {
	return strings.NewReplacer("${", "$${", "%{", "%%{").Replace(s)
}

// quoteHCLString quotes s as a double-quoted HCL string literal, escaping
// HCL interpolation/directive sequences (${ -> $${, %{ -> %%{).
func quoteHCLString(s string) string {
	return strconv.Quote(escapeHCLInterpolation(s))
}

func isValidPostgresIndexMethod(using string) bool {
	u := strings.ToUpper(strings.TrimSpace(using))
	if u == "" {
		return true
	}
	switch u {
	case "BTREE", "GIN", "GIST", "HASH", "BRIN", "SPGIST", "HNSW", "IVFFLAT":
		return true
	default:
		return false
	}
}

var builtinPostgresOpclasses = map[string]bool{
	"text_pattern_ops": true, "varchar_pattern_ops": true, "bpchar_pattern_ops": true, "name_pattern_ops": true,
	"gin_ops": true, "jsonb_ops": true, "jsonb_path_ops": true, "array_ops": true, "tsvector_ops": true,
	"gist_ops": true, "spgist_ops": true, "brin_ops": true, "btree_ops": true, "hash_ops": true,
	"bool_ops": true, "bytea_ops": true, "char_ops": true, "name_ops": true,
	"int2_ops": true, "int4_ops": true, "int8_ops": true, "float4_ops": true, "float8_ops": true, "numeric_ops": true,
	"text_ops": true, "varchar_ops": true, "bpchar_ops": true, "date_ops": true, "time_ops": true, "timetz_ops": true,
	"timestamp_ops": true, "timestamptz_ops": true, "interval_ops": true, "uuid_ops": true,
	"inet_ops": true, "cidr_ops": true, "macaddr_ops": true, "macaddr8_ops": true,
	"bit_ops": true, "varbit_ops": true, "oid_ops": true, "money_ops": true,
	"tid_ops": true, "xid_ops": true, "cid_ops": true, "pg_lsn_ops": true,
	"enum_ops": true, "record_ops": true, "range_ops": true, "multirange_ops": true,
	"box_ops": true, "poly_ops": true, "circle_ops": true, "point_ops": true,
	"quad_point_ops": true, "kd_point_ops": true,
	"int4_minmax_ops": true, "int8_minmax_ops": true, "date_minmax_ops": true,
	"timestamp_minmax_ops": true, "timestamptz_minmax_ops": true, "uuid_minmax_ops": true,
	"text_bloom_ops": true, "int4_bloom_ops": true, "int8_bloom_ops": true, "uuid_bloom_ops": true,
}

func formatPostgresAtlasHCLOpclass(op string) string {
	trimmed := strings.TrimSpace(op)
	if builtinPostgresOpclasses[strings.ToLower(trimmed)] {
		return trimmed
	}
	return quoteHCLString(trimmed)
}

// formatPostgresHCLDefaultValue formats the Atlas HCL default attribute value for a PostgreSQL column.
func formatPostgresHCLDefaultValue(c PostgresColumnDescriptor) string {
	if c.ServerDefault != "" {
		return fmt.Sprintf("sql(%s)", quoteHCLString(strings.ToLower(strings.TrimSpace(c.ServerDefault))))
	}
	if !c.HasDefaultValue && c.DefaultValue == "" {
		return ""
	}
	sqlType := strings.TrimSpace(c.SQLType)
	if sqlType == "" {
		sqlType = strings.TrimSpace(strings.ReplaceAll(c.HCLType, "double_precision", "double precision"))
	}
	upperType := strings.ToUpper(sqlType)
	if strings.HasSuffix(upperType, "[]") {
		rawVal := strings.TrimSpace(c.DefaultValue)
		if rawVal == "[]" {
			rawVal = "{}"
		}
		innerLiteral := "'" + strings.ReplaceAll(rawVal, "'", "''") + "'"
		castType := strings.ToLower(strings.TrimSpace(sqlType))
		return fmt.Sprintf("sql(%s)", quoteHCLString(innerLiteral+"::"+castType))
	}
	if c.DefaultValue != "" && isPostgresBooleanSQLType(sqlType) {
		return strings.ToLower(c.DefaultValue)
	}
	if c.DefaultValue != "" && isPostgresNumericSQLType(sqlType) {
		return c.DefaultValue
	}
	return quoteHCLString(c.DefaultValue)
}

func canonicalPostgresSQLCastType(s string) string {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return ""
	}
	suffix := ""
	for strings.HasSuffix(t, "[]") {
		suffix += "[]"
		t = strings.TrimSpace(strings.TrimSuffix(t, "[]"))
	}
	t = strings.Join(strings.Fields(t), " ")
	t = strings.ReplaceAll(t, ", ", ",")
	t = strings.ReplaceAll(t, ",", ", ")
	switch {
	case t == "int" || t == "int4":
		t = "integer"
	case t == "int2":
		t = "smallint"
	case t == "int8":
		t = "bigint"
	case t == "bool":
		t = "boolean"
	case t == "float4":
		t = "real"
	case t == "float8":
		t = "double precision"
	case t == "timestamp with time zone":
		t = "timestamptz"
	case t == "timestamp without time zone":
		t = "timestamp"
	case t == "time with time zone":
		t = "timetz"
	case t == "time without time zone":
		t = "time"
	case t == "character varying":
		t = "varchar"
	case strings.HasPrefix(t, "character varying("):
		t = "varchar(" + strings.TrimPrefix(t, "character varying(")
	case t == "decimal":
		t = "numeric"
	case strings.HasPrefix(t, "decimal("):
		t = "numeric(" + strings.TrimPrefix(t, "decimal(")
	}
	return t + suffix
}

// parsePostgresHCLSQLDefault parses the inner argument of an Atlas HCL default = sql("...")
// attribute, recognizing allowlisted SQL server functions (e.g. "now()") or typed literal
// casts (e.g. "'{}'::text[]", "'{}'::uuid[]") compatible with columnType.
func parsePostgresHCLSQLDefault(inner, columnType string) (defVal string, hasDefVal bool, srvDef string, ok bool) {
	trimmed := strings.TrimSpace(inner)
	upper := strings.ToUpper(trimmed)
	if isValidPostgresServerDefault(upper) {
		return "", false, upper, true
	}
	if !strings.HasPrefix(trimmed, "'") {
		return "", false, "", false
	}
	var literal strings.Builder
	endIdx := -1
	for i := 1; i < len(trimmed); i++ {
		if trimmed[i] == '\'' {
			if i+1 < len(trimmed) && trimmed[i+1] == '\'' {
				literal.WriteByte('\'')
				i++
				continue
			}
			endIdx = i
			break
		}
		literal.WriteByte(trimmed[i])
	}
	if endIdx < 0 {
		return "", false, "", false
	}
	rest := strings.TrimSpace(trimmed[endIdx+1:])
	if !strings.HasPrefix(rest, "::") {
		return "", false, "", false
	}
	castType := strings.TrimSpace(strings.TrimPrefix(rest, "::"))
	if castType == "" || (!isValidPostgresSQLTypeSyntax(castType) && !isValidPostgresTypeSyntax(castType)) {
		return "", false, "", false
	}
	if canonCol := canonicalPostgresSQLCastType(columnType); canonCol != "" {
		if canonicalPostgresSQLCastType(castType) != canonCol {
			return "", false, "", false
		}
	}
	unescaped := literal.String()
	if unescaped != "" && !isValidDorisDefaultValue(unescaped) {
		return "", false, "", false
	}
	if columnType != "" && validatePostgresColumnDefault(columnType, unescaped, true) != nil {
		return "", false, "", false
	}
	return unescaped, true, "", true
}

// emitPostgresAtlasIndexOnBlocks writes ordered Atlas HCL `on { ... }` blocks or `columns = [...]`
// for idx inside an `index "..." { ... }` block.
func emitPostgresAtlasIndexOnBlocks(b *strings.Builder, idx PostgresIndexDescriptor) {
	if len(idx.Parts) > 0 {
		for _, p := range idx.Parts {
			b.WriteString("    on {\n")
			if p.Desc {
				b.WriteString("      desc   = true\n")
			}
			if p.Expr != "" {
				fmt.Fprintf(b, "      expr = %s\n", quoteHCLString(strings.TrimSpace(p.Expr)))
			} else {
				fmt.Fprintf(b, "      column = column.%s\n", p.Column)
			}
			if p.Opclass != "" {
				fmt.Fprintf(b, "      ops    = %s\n", formatPostgresAtlasHCLOpclass(p.Opclass))
			}
			b.WriteString("    }\n")
		}
		return
	}
	if len(idx.DescColumns) > 0 || len(idx.Expressions) > 0 {
		for _, col := range idx.Columns {
			b.WriteString("    on {\n")
			if idx.IsDesc(col) {
				b.WriteString("      desc   = true\n")
			}
			fmt.Fprintf(b, "      column = column.%s\n", col)
			b.WriteString("    }\n")
		}
		for _, expr := range idx.Expressions {
			b.WriteString("    on {\n")
			fmt.Fprintf(b, "      expr = %s\n", quoteHCLString(strings.TrimSpace(expr)))
			b.WriteString("    }\n")
		}
		return
	}
	fmt.Fprintf(b, "    columns = %s\n", formatAtlasHCLColRefList(idx.Columns))
}

// formatPostgresIndexPartsSQL renders ordered index key items for CREATE INDEX statements.
func formatPostgresIndexPartsSQL(idx PostgresIndexDescriptor, quoteCol bool) []string {
	var parts []string
	if len(idx.Parts) > 0 {
		for _, p := range idx.Parts {
			var elem string
			if p.Column != "" {
				if quoteCol {
					elem = fmt.Sprintf("%q", p.Column)
				} else {
					elem = p.Column
				}
			} else {
				elem = formatPostgresIndexExprSQL(p.Expr)
			}
			if p.Opclass != "" {
				elem += " " + p.Opclass
			}
			if p.Desc {
				elem += " DESC"
			}
			parts = append(parts, elem)
		}
		return parts
	}
	for _, col := range idx.Columns {
		elem := col
		if quoteCol {
			elem = fmt.Sprintf("%q", col)
		}
		if idx.IsDesc(col) {
			elem += " DESC"
		}
		parts = append(parts, elem)
	}
	for _, expr := range idx.Expressions {
		parts = append(parts, formatPostgresIndexExprSQL(expr))
	}
	return parts
}

// formatPostgresHCLType normalizes a PostgreSQL type expression into a valid Atlas HCL type attribute.
// "double precision" maps to Atlas's native "double_precision" identifier, and array or multi-word
// SQL type expressions are wrapped in sql("...").
func formatPostgresHCLType(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "double precision" {
		return "double_precision"
	}
	if strings.HasSuffix(raw, "[]") || strings.Contains(raw, " ") || strings.HasPrefix(strings.ToLower(raw), "vector(") {
		return fmt.Sprintf("sql(%s)", quoteHCLString(raw))
	}
	return raw
}

// BuildPostgresAtlasHCL renders a deterministic Atlas HCL table block for t.
func (t PostgresTableSchemaDescriptor) BuildPostgresAtlasHCL() string {
	if !isValidSQLIdentifier(t.TableName) || len(t.TableName) > maxPostgresIdentifierBytes || len(t.Columns) == 0 {
		return ""
	}
	if t.WriterService != "" && !isValidServiceIdentifier(t.WriterService) {
		return ""
	}
	for _, rs := range t.ReaderServices {
		if rs != "*" && !isValidServiceIdentifier(rs) {
			return ""
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "table %q {\n", t.TableName)
	b.WriteString("  schema = schema.tenant\n")

	for _, c := range t.Columns {
		sqlType := strings.TrimSpace(c.SQLType)
		hclType := strings.TrimSpace(c.HCLType)
		if sqlType == "" && hclType != "" {
			sqlType = strings.ToUpper(strings.ReplaceAll(hclType, "double_precision", "double precision"))
		}
		if hclType == "" && sqlType != "" {
			hclType = strings.ToLower(sqlType)
		}
		hclTypeAsSQL := strings.ReplaceAll(hclType, "double_precision", "double precision")
		if !isValidSQLIdentifier(c.ColumnName) ||
			len(c.ColumnName) > maxPostgresIdentifierBytes ||
			!isValidPostgresSQLTypeSyntax(sqlType) ||
			!isValidPostgresSQLTypeSyntax(hclTypeAsSQL) ||
			!isValidPostgresServerDefault(c.ServerDefault) ||
			validatePostgresColumnDefault(sqlType, c.DefaultValue, c.HasDefaultValue) != nil {
			return ""
		}
		if c.Identity && (!isPostgresIdentityEligibleSQLType(sqlType) || c.Nullable || c.HasDefaultValue || c.DefaultValue != "" || c.ServerDefault != "") {
			return ""
		}
		normCol := c
		normCol.SQLType = sqlType
		normCol.HCLType = hclType
		fmt.Fprintf(&b, "  column %q {\n", normCol.ColumnName)
		fmt.Fprintf(&b, "    null = %t\n", normCol.Nullable)
		fmt.Fprintf(&b, "    type = %s\n", formatPostgresHCLType(normCol.HCLType))
		if normCol.Identity {
			b.WriteString("    identity {\n")
			b.WriteString("      generated = ALWAYS\n")
			b.WriteString("    }\n")
		} else if def := formatPostgresHCLDefaultValue(normCol); def != "" {
			fmt.Fprintf(&b, "    default = %s\n", def)
		}
		b.WriteString("  }\n")
	}

	if len(t.PrimaryKeys) > 0 {
		for _, pk := range t.PrimaryKeys {
			if !isValidSQLIdentifier(pk) || len(pk) > maxPostgresIdentifierBytes {
				return ""
			}
		}
		b.WriteString("  primary_key {\n")
		fmt.Fprintf(&b, "    columns = %s\n", formatAtlasHCLColRefList(t.PrimaryKeys))
		b.WriteString("  }\n")
	}

	for _, idx := range t.Indexes {
		if !isValidSQLIdentifier(idx.Name) || len(idx.Name) > maxPostgresIdentifierBytes || (len(idx.Parts) == 0 && len(idx.Columns) == 0 && len(idx.Expressions) == 0) {
			return ""
		}
		for _, col := range idx.Columns {
			if !isValidSQLIdentifier(col) || len(col) > maxPostgresIdentifierBytes {
				return ""
			}
		}
		for _, expr := range idx.Expressions {
			if !isValidPostgresCheckExpr(expr) {
				return ""
			}
		}
		for _, p := range idx.Parts {
			if (p.Column != "") == (p.Expr != "") {
				return ""
			}
			if p.Column != "" && (!isValidSQLIdentifier(p.Column) || len(p.Column) > maxPostgresIdentifierBytes) {
				return ""
			}
			if p.Expr != "" && !isValidPostgresCheckExpr(p.Expr) {
				return ""
			}
			if p.Opclass != "" && (!isValidSQLIdentifier(p.Opclass) || len(p.Opclass) > maxPostgresIdentifierBytes) {
				return ""
			}
		}
		if idx.Where != "" && !isValidPostgresCheckExpr(idx.Where) {
			return ""
		}
		fmt.Fprintf(&b, "  index %q {\n", idx.Name)
		if idx.Unique {
			b.WriteString("    unique  = true\n")
		}
		emitPostgresAtlasIndexOnBlocks(&b, idx)
		if idx.Where != "" {
			fmt.Fprintf(&b, "    where   = %s\n", quoteHCLString(idx.Where))
		}
		b.WriteString("  }\n")
	}

	for _, fk := range t.ForeignKeys {
		refCols := fk.EffectiveRefColumns()
		if !isValidSQLIdentifier(fk.Name) || len(fk.Name) > maxPostgresIdentifierBytes ||
			!isValidSQLIdentifier(fk.RefTable) || len(fk.RefTable) > maxPostgresIdentifierBytes ||
			len(refCols) == 0 || len(refCols) != len(fk.Columns) {
			return ""
		}
		for _, col := range fk.Columns {
			if !isValidSQLIdentifier(col) || len(col) > maxPostgresIdentifierBytes {
				return ""
			}
		}
		for _, rc := range refCols {
			if !isValidSQLIdentifier(rc) || len(rc) > maxPostgresIdentifierBytes {
				return ""
			}
		}
		delHCL, _, ok := isValidForeignKeyAction(fk.OnDelete)
		if !ok {
			return ""
		}
		updHCL, _, ok := isValidForeignKeyAction(fk.OnUpdate)
		if !ok {
			return ""
		}
		refColRefs := make([]string, 0, len(refCols))
		for _, rc := range refCols {
			refColRefs = append(refColRefs, fmt.Sprintf("table.%s.column.%s", fk.RefTable, rc))
		}
		fmt.Fprintf(&b, "  foreign_key %q {\n", fk.Name)
		fmt.Fprintf(&b, "    columns     = %s\n", formatAtlasHCLColRefList(fk.Columns))
		fmt.Fprintf(&b, "    ref_columns = [%s]\n", strings.Join(refColRefs, ", "))
		if delHCL != "" {
			fmt.Fprintf(&b, "    on_delete   = %s\n", delHCL)
		}
		if updHCL != "" {
			fmt.Fprintf(&b, "    on_update   = %s\n", updHCL)
		}
		b.WriteString("  }\n")
	}

	for _, chk := range t.Checks {
		if !isValidSQLIdentifier(chk.Name) || len(chk.Name) > maxPostgresIdentifierBytes || !isValidPostgresCheckExpr(chk.Expr) {
			return ""
		}
		fmt.Fprintf(&b, "  check %q {\n", chk.Name)
		fmt.Fprintf(&b, "    expr = %s\n", quoteHCLString(chk.Expr))
		b.WriteString("  }\n")
	}

	b.WriteString("}")
	return b.String()
}

// generatePostgresHCLFileWithOpts emits PostgreSQL Atlas HCL for file, honoring opts.OutputFile.
func generatePostgresHCLFileWithOpts(gen *protogen.Plugin, file *protogen.File, tables []*PostgresTableSchemaDescriptor, opts *Options) error {
	if len(tables) == 0 {
		return nil
	}
	outOverride := ""
	if opts != nil {
		outOverride = opts.OutputFile
	}
	filename, err := resolveOutputFilename(file.GeneratedFilenamePrefix, "_postgres.hcl", outOverride)
	if err != nil {
		return err
	}
	g := gen.NewGeneratedFile(filename, "")
	writePostgresHCLDocument(g, file.Desc.Path(), tables)
	return nil
}

func generateConsolidatedPostgresHCLFile(gen *protogen.Plugin, files []*protogen.File, tables []*PostgresTableSchemaDescriptor, outputFile string) error {
	if len(tables) == 0 {
		return nil
	}
	filename, err := resolveOutputFilename("", "_postgres.hcl", outputFile)
	if err != nil {
		return err
	}
	sources := make([]string, 0, len(files))
	for _, f := range files {
		sources = append(sources, f.Desc.Path())
	}
	g := gen.NewGeneratedFile(filename, "")
	writePostgresHCLDocument(g, strings.Join(sources, ", "), tables)
	return nil
}

func writePostgresHCLDocument(g *protogen.GeneratedFile, sourceDesc string, tables []*PostgresTableSchemaDescriptor) {
	g.P("# Code generated by proto2type. DO NOT EDIT.")
	g.P("# source: ", sourceDesc)
	g.P("# backend: postgres (hcl)")
	g.P()
	g.P("schema \"tenant\" {}")
	g.P()

	emitted := 0
	for _, tbl := range tables {
		hcl := tbl.BuildPostgresAtlasHCL()
		if hcl == "" {
			continue
		}
		if emitted > 0 {
			g.P()
		}
		emitted++
		g.P("# ", tbl.GoName, " (", tbl.TableName, ")")
		if tbl.WriterService != "" {
			g.P("# writer_service: ", tbl.WriterService)
		}
		if len(tbl.ReaderServices) > 0 {
			g.P("# reader_services: ", strings.Join(tbl.ReaderServices, ", "))
		}
		g.P(hcl)
	}
}

// ---------------------------------------------------------------------------
// Lakehouse-Compatible PostgreSQL Table Descriptor, HCL Parser, & Linter
// ---------------------------------------------------------------------------

// PostgresTableDescriptor describes a PostgreSQL table generated from a protobuf message,
// supporting both standard relational tables and lakehouse-annotated tables.
type PostgresTableDescriptor struct {
	TableName            string
	MessageName          string
	DatabaseScope        string
	WriterService        string
	ReaderServices       []string
	PrimaryKeyColumns    []string
	PartitionByColumns   []string
	InvertedIndexColumns []string
	NgramBfColumns       []string
	EmbeddingDimension   int32
	Indexes              []PostgresIndexDescriptor
	ForeignKeys          []PostgresForeignKeyDescriptor
	Columns              []PostgresColumnDescriptor
	Checks               []PostgresCheckDescriptor
	IsSingleton          bool
}

type postgresTableIR struct {
	Msg        *protogen.Message
	GoName     string
	Descriptor PostgresTableDescriptor
}

func effectiveColPostgresType(c PostgresColumnDescriptor) string {
	if s := strings.TrimSpace(c.PostgresType); s != "" {
		return s
	}
	return strings.TrimSpace(c.SQLType)
}

func messageHasLakehouseTableMeta(meta *DorisTableMetaIR) bool {
	if meta == nil {
		return false
	}
	return meta.DatabaseScope != "" ||
		meta.SyncTier != "" ||
		len(meta.PartitionByColumns) > 0 ||
		len(meta.NgramBfColumns) > 0 ||
		meta.EmbeddingDimension > 0
}

func resolvePostgresTableCollisionSchema(isLakehouse bool, meta *DorisTableMetaIR) string {
	if !isLakehouse || meta == nil {
		return "public"
	}
	schemaName, ok := resolvePostgresSchemaName(meta.DatabaseScope, "${TENANT_SLUG}")
	if !ok {
		return "public"
	}
	return schemaName
}

func postgresResolvedSchemaTableKey(schemaName, tableName string) string {
	return schemaName + "\x00" + tableName
}

func fileHasLakehousePostgresTables(file *protogen.File) bool {
	if file == nil {
		return false
	}
	for _, msg := range file.Messages {
		if isMessageSkipped(msg) {
			continue
		}
		if messageHasLakehouseTableMeta(extractDorisTableMeta(msg)) {
			return true
		}
	}
	return false
}

func isValidPostgresTypeSyntax(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return false
	}
	if isValidPostgresSQLTypeSyntax(s) {
		return true
	}
	if strings.HasSuffix(s, "[]") {
		base := strings.TrimSpace(strings.TrimSuffix(s, "[]"))
		if strings.HasSuffix(base, "[]") {
			return false
		}
		return isValidPostgresScalarTypeSyntax(base)
	}
	return isValidPostgresScalarTypeSyntax(s)
}

func isValidPostgresScalarTypeSyntax(s string) bool {
	upper := strings.ToUpper(strings.TrimSpace(s))
	switch upper {
	case "DOUBLE PRECISION", "CHARACTER VARYING", "TIMESTAMP WITH TIME ZONE", "TIMESTAMP WITHOUT TIME ZONE":
		return true
	}
	return isValidPostgresSQLTypeSyntax(s)
}

func isValidPostgresDatabaseScope(scope string) bool {
	switch scope {
	case "", "UNSPECIFIED", "PUBLIC", "TENANT", "GLOBAL_TERMINOLOGY", "GLOBAL_TERMINOLOGY_RAG", "GLOBAL_TELEMETRY":
		return true
	default:
		return false
	}
}

func postgresSyntheticGinIndexName(tableName, colName string) string {
	return compactPostgresIdentifier("idx", tableName+"_"+colName)
}

func postgresSyntheticNgramIndexName(tableName, colName string) string {
	return compactPostgresIdentifier("idx_ngram", tableName+"_"+colName)
}

func postgresSyntheticHnswIndexName(colName string) string {
	return compactPostgresIdentifier("idx", colName+"_hnsw")
}

func validatePostgresDescriptor(t PostgresTableDescriptor) bool {
	if !isValidSQLIdentifier(t.TableName) || !isValidPostgresDatabaseScope(t.DatabaseScope) {
		return false
	}
	if t.EmbeddingDimension < 0 {
		return false
	}
	if t.IsSingleton && len(effectivePostgresPartitionCols(t)) > 0 {
		return false
	}
	colSet := make(map[string]bool, len(t.Columns))
	for _, c := range t.Columns {
		if !isValidSQLIdentifier(c.ColumnName) || colSet[c.ColumnName] {
			return false
		}
		colType := effectiveColPostgresType(c)
		if colType == "" || !isValidPostgresTypeSyntax(colType) || IsForbiddenPostgresColumnType(colType) {
			return false
		}
		if (c.HasDefaultValue || c.DefaultValue != "") && ((c.DefaultValue != "" && !isValidDorisDefaultValue(c.DefaultValue)) || validatePostgresColumnDefault(colType, c.DefaultValue, c.HasDefaultValue) != nil) {
			return false
		}
		if !isValidPostgresServerDefault(c.ServerDefault) {
			return false
		}
		if c.Identity && (!isPostgresIdentityEligibleSQLType(colType) || c.Nullable || c.HasDefaultValue || c.DefaultValue != "" || c.ServerDefault != "") {
			return false
		}
		colSet[c.ColumnName] = true
	}
	for _, pk := range t.PrimaryKeyColumns {
		if !isValidSQLIdentifier(pk) || !colSet[pk] {
			return false
		}
	}
	for _, part := range t.PartitionByColumns {
		if !isValidSQLIdentifier(part) || !colSet[part] {
			return false
		}
	}
	for _, inv := range t.InvertedIndexColumns {
		if !isValidSQLIdentifier(inv) || len(inv) > maxPostgresIdentifierBytes || !colSet[inv] {
			return false
		}
	}
	for _, ng := range t.NgramBfColumns {
		if !isValidSQLIdentifier(ng) || len(ng) > maxPostgresIdentifierBytes || !colSet[ng] {
			return false
		}
	}
	for _, idx := range t.Indexes {
		if idx.Name != "" && (!isValidSQLIdentifier(idx.Name) || len(idx.Name) > maxPostgresIdentifierBytes) {
			return false
		}
		if !isValidPostgresIndexMethod(idx.Using) {
			return false
		}
		isImplicitOrBTree := idx.Using == "" || strings.EqualFold(strings.TrimSpace(idx.Using), "btree")
		if idx.Ops != "" && (!isValidSQLIdentifier(idx.Ops) || len(idx.Ops) > maxPostgresIdentifierBytes) {
			return false
		}
		if isImplicitOrBTree && isPostgresBTreeIncompatibleOpclass(idx.Ops) {
			return false
		}
		for _, col := range idx.Columns {
			if !isValidSQLIdentifier(col) || len(col) > maxPostgresIdentifierBytes || !colSet[col] {
				return false
			}
		}
		for _, expr := range idx.Expressions {
			if !isValidPostgresCheckExpr(expr) {
				return false
			}
		}
		for _, p := range idx.Parts {
			if (p.Column != "") == (p.Expr != "") {
				return false
			}
			if p.Column != "" && (!isValidSQLIdentifier(p.Column) || len(p.Column) > maxPostgresIdentifierBytes || !colSet[p.Column]) {
				return false
			}
			if p.Expr != "" && !isValidPostgresCheckExpr(p.Expr) {
				return false
			}
			if p.Opclass != "" && (!isValidSQLIdentifier(p.Opclass) || len(p.Opclass) > maxPostgresIdentifierBytes) {
				return false
			}
			if isImplicitOrBTree && isPostgresBTreeIncompatibleOpclass(p.Opclass) {
				return false
			}
		}
		if idx.Where != "" && !isValidPostgresCheckExpr(idx.Where) {
			return false
		}
	}
	for _, fk := range t.ForeignKeys {
		if fk.Name != "" && (!isValidSQLIdentifier(fk.Name) || len(fk.Name) > maxPostgresIdentifierBytes) {
			return false
		}
		if fk.RefTable != "" && (!isValidSQLIdentifier(fk.RefTable) || len(fk.RefTable) > maxPostgresIdentifierBytes) {
			return false
		}
		for _, col := range fk.Columns {
			if !isValidSQLIdentifier(col) || len(col) > maxPostgresIdentifierBytes || !colSet[col] {
				return false
			}
		}
		refCols := fk.EffectiveRefColumns()
		for _, rc := range refCols {
			if !isValidSQLIdentifier(rc) || len(rc) > maxPostgresIdentifierBytes {
				return false
			}
		}
		if len(fk.Columns) > 0 && len(refCols) != len(fk.Columns) {
			return false
		}
		if _, _, ok := isValidForeignKeyAction(fk.OnDelete); !ok {
			return false
		}
		if _, _, ok := isValidForeignKeyAction(fk.OnUpdate); !ok {
			return false
		}
	}
	for _, chk := range t.Checks {
		if !isValidSQLIdentifier(chk.Name) || len(chk.Name) > maxPostgresIdentifierBytes || !isValidPostgresCheckExpr(chk.Expr) {
			return false
		}
	}
	return true
}

func dorisTypeToPostgresDDLType(dorisType string, isEmbedding bool, embeddingDim int32) (string, error) {
	dt := strings.ToUpper(strings.TrimSpace(dorisType))
	if dt == "" {
		return "", nil
	}
	if IsForbiddenPostgresColumnType(dt) {
		return "", fmt.Errorf("JSON/JSONB/VARIANT column types are forbidden in relational PostgreSQL schemas (got %q)", dorisType)
	}
	if strings.HasPrefix(dt, "ARRAY<") && strings.HasSuffix(dt, ">") {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(dt, "ARRAY<"), ">"))
		if isEmbedding && embeddingDim > 0 && (inner == "FLOAT" || inner == "REAL") {
			return fmt.Sprintf("VECTOR(%d)", embeddingDim), nil
		}
		elemType, err := dorisTypeToPostgresDDLType(inner, false, 0)
		if err != nil {
			return "", err
		}
		return elemType + "[]", nil
	}
	switch dt {
	case "STRING", "TEXT":
		return "TEXT", nil
	case "BOOLEAN", "BOOL":
		return "BOOLEAN", nil
	case "TINYINT", "SMALLINT":
		return "SMALLINT", nil
	case "INT", "INTEGER":
		return "INTEGER", nil
	case "BIGINT":
		return "BIGINT", nil
	case "LARGEINT":
		return "NUMERIC(38, 0)", nil
	case "FLOAT", "REAL":
		return "REAL", nil
	case "DOUBLE", "DOUBLE PRECISION":
		return "DOUBLE PRECISION", nil
	case "DATE", "DATEV2":
		return "DATE", nil
	case "DATETIME", "DATETIMEV2", "TIMESTAMPTZ":
		return "TIMESTAMPTZ", nil
	case "BYTEA", "BINARY", "VARBINARY":
		return "BYTEA", nil
	}
	if (strings.HasPrefix(dt, "DATETIME(") || strings.HasPrefix(dt, "DATETIMEV2(")) && strings.HasSuffix(dt, ")") {
		return "TIMESTAMPTZ", nil
	}
	if strings.HasPrefix(dt, "DECIMALV3(") && strings.HasSuffix(dt, ")") {
		return "NUMERIC(" + strings.TrimPrefix(dt, "DECIMALV3("), nil
	}
	if strings.HasPrefix(dt, "DECIMAL(") && strings.HasSuffix(dt, ")") {
		return "NUMERIC(" + strings.TrimPrefix(dt, "DECIMAL("), nil
	}
	if strings.HasPrefix(dt, "NUMERIC(") || strings.HasPrefix(dt, "VARCHAR(") || strings.HasPrefix(dt, "VECTOR(") {
		return dt, nil
	}
	if !isValidPostgresTypeSyntax(dt) {
		return "", fmt.Errorf("invalid PostgreSQL column type %q", dorisType)
	}
	return dt, nil
}

func defaultPostgresSQLType(field *protogen.Field, colMeta *DorisColumnMetaIR, tableMeta *DorisTableMetaIR, isPK bool, opts *Options) (string, error) {
	if colMeta != nil && colMeta.JSONB {
		return "", fmt.Errorf("field %s: JSON/JSONB/VARIANT column types are forbidden in relational PostgreSQL schemas", field.Desc.FullName())
	}
	if colMeta != nil && colMeta.SQLType != "" && colMeta.PostgresType != "" && !strings.EqualFold(strings.TrimSpace(colMeta.SQLType), strings.TrimSpace(colMeta.PostgresType)) {
		return "", fmt.Errorf("field %s: conflicting sql_type %q and postgres_type %q", field.Desc.FullName(), colMeta.SQLType, colMeta.PostgresType)
	}
	if colMeta != nil && (colMeta.PostgresType != "" || colMeta.SQLType != "") {
		raw := colMeta.PostgresType
		if raw == "" {
			raw = colMeta.SQLType
		}
		pt := strings.ToUpper(strings.TrimSpace(raw))
		if IsForbiddenPostgresColumnType(pt) {
			return "", fmt.Errorf("field %s: JSON/JSONB/VARIANT column types are forbidden in relational PostgreSQL schemas (got %q)", field.Desc.FullName(), raw)
		}
		if !isValidPostgresTypeSyntax(pt) {
			return "", fmt.Errorf("field %s: invalid postgres_type %q", field.Desc.FullName(), raw)
		}
		if field.Desc.IsList() && !strings.HasSuffix(pt, "[]") && !strings.HasPrefix(pt, "VECTOR(") {
			pt += "[]"
		}
		return pt, nil
	}
	isEmbedding := string(field.Desc.Name()) == "embedding" || (colMeta != nil && colMeta.ColumnName == "embedding")
	var embDim int32
	if tableMeta != nil {
		embDim = tableMeta.EmbeddingDimension
	}
	if colMeta != nil && colMeta.DorisType != "" {
		pt, err := dorisTypeToPostgresDDLType(colMeta.DorisType, isEmbedding, embDim)
		if err != nil {
			return "", fmt.Errorf("field %s: %w", field.Desc.FullName(), err)
		}
		return pt, nil
	}

	if field.Desc.IsList() {
		if isEmbedding && embDim > 0 && field.Desc.Kind() == protoreflect.FloatKind {
			return fmt.Sprintf("VECTOR(%d)", embDim), nil
		}
		elemType, err := defaultPostgresScalarSQLType(field, colMeta, false, opts)
		if err != nil {
			return "", err
		}
		return elemType + "[]", nil
	}

	return defaultPostgresScalarSQLType(field, colMeta, isPK, opts)
}

func defaultPostgresScalarSQLType(field *protogen.Field, colMeta *DorisColumnMetaIR, isPK bool, opts *Options) (string, error) {
	if field.Desc.Kind() == protoreflect.MessageKind && field.Desc.Message() != nil {
		fullName := string(field.Desc.Message().FullName())
		switch fullName {
		case "google.protobuf.Timestamp":
			return "TIMESTAMPTZ", nil
		case "google.type.Date":
			return "DATE", nil
		case "google.protobuf.Duration":
			return "BIGINT", nil
		case "google.protobuf.FieldMask":
			return "TEXT[]", nil
		case "google.protobuf.BoolValue":
			return "BOOLEAN", nil
		case "google.protobuf.Int32Value":
			return "INTEGER", nil
		case "google.protobuf.Int64Value", "google.protobuf.UInt32Value":
			return "BIGINT", nil
		case "google.protobuf.UInt64Value":
			return "NUMERIC(20, 0)", nil
		case "google.protobuf.FloatValue":
			return "REAL", nil
		case "google.protobuf.DoubleValue":
			return "DOUBLE PRECISION", nil
		case "google.protobuf.StringValue":
			return "VARCHAR(256)", nil
		case "google.protobuf.BytesValue":
			return "BYTEA", nil
		default:
			return "", fmt.Errorf("field %s: message type %s cannot be a relational PostgreSQL column without inline/inline_nested", field.Desc.FullName(), fullName)
		}
	}

	switch field.Desc.Kind() {
	case protoreflect.BoolKind:
		return "BOOLEAN", nil
	case protoreflect.EnumKind:
		if (colMeta != nil && colMeta.StripEnumPrefix) || isEnumAsString(field, opts) {
			return "VARCHAR(64)", nil
		}
		return "SMALLINT", nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "INTEGER", nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "BIGINT", nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "NUMERIC(20, 0)", nil
	case protoreflect.FloatKind:
		return "REAL", nil
	case protoreflect.DoubleKind:
		return "DOUBLE PRECISION", nil
	case protoreflect.BytesKind:
		return "BYTEA", nil
	case protoreflect.StringKind:
		if colMeta != nil && colMeta.Length > 0 {
			return fmt.Sprintf("VARCHAR(%d)", colMeta.Length), nil
		}
		if colMeta != nil && colMeta.InvertedIndex {
			return "TEXT", nil
		}
		if vc := extractValidateConstraints(field); vc != nil && vc.MaxLength != nil && *vc.MaxLength > 0 && *vc.MaxLength <= 10485760 {
			return fmt.Sprintf("VARCHAR(%d)", *vc.MaxLength), nil
		}
		if field.Desc.IsList() || isPK || (colMeta != nil && (colMeta.IsUniqueKey || colMeta.IsPartitionKey)) {
			return "VARCHAR(64)", nil
		}
		return "VARCHAR(256)", nil
	default:
		return "", fmt.Errorf("field %s: unsupported protobuf kind %v for PostgreSQL relational column", field.Desc.FullName(), field.Desc.Kind())
	}
}

func isPostgresSupportedWKT(field *protogen.Field) bool {
	if field == nil || field.Desc.Kind() != protoreflect.MessageKind || field.Desc.Message() == nil {
		return false
	}
	switch string(field.Desc.Message().FullName()) {
	case "google.protobuf.Timestamp",
		"google.type.Date",
		"google.protobuf.Duration",
		"google.protobuf.FieldMask",
		"google.protobuf.BoolValue",
		"google.protobuf.Int32Value",
		"google.protobuf.Int64Value",
		"google.protobuf.UInt32Value",
		"google.protobuf.UInt64Value",
		"google.protobuf.FloatValue",
		"google.protobuf.DoubleValue",
		"google.protobuf.StringValue",
		"google.protobuf.BytesValue":
		return true
	default:
		return false
	}
}

func isPostgresWrapperWKT(field *protogen.Field) bool {
	if field == nil || field.Desc.Kind() != protoreflect.MessageKind || field.Desc.Message() == nil {
		return false
	}
	switch string(field.Desc.Message().FullName()) {
	case "google.protobuf.BoolValue",
		"google.protobuf.Int32Value",
		"google.protobuf.Int64Value",
		"google.protobuf.UInt32Value",
		"google.protobuf.UInt64Value",
		"google.protobuf.FloatValue",
		"google.protobuf.DoubleValue",
		"google.protobuf.StringValue",
		"google.protobuf.BytesValue":
		return true
	default:
		return false
	}
}

func messageHasAnnotatedPostgresColumns(msg *protogen.Message, visited map[protoreflect.FullName]bool) bool {
	if msg == nil || visited[msg.Desc.FullName()] {
		return false
	}
	visited[msg.Desc.FullName()] = true
	for _, f := range msg.Fields {
		if isFieldSkipped(f) {
			continue
		}
		cm := extractColumnMeta(f)
		if (cm != nil && cm.InlineNested) || isInline(f) {
			if f.Message != nil && messageHasAnnotatedPostgresColumns(f.Message, visited) {
				return true
			}
			continue
		}
		if cm != nil && (cm.PostgresType != "" || cm.SQLType != "" || cm.DorisType != "" || cm.IcebergType != "") {
			return true
		}
	}
	return false
}

func collectLakehouseInlineTargetMsgs(files []*protogen.File) map[protoreflect.FullName]bool {
	inlineTargetMsgs := make(map[protoreflect.FullName]bool)
	var collectInlineTargets func(msg *protogen.Message, visited map[protoreflect.FullName]bool)
	collectInlineTargets = func(msg *protogen.Message, visited map[protoreflect.FullName]bool) {
		if msg == nil || visited[msg.Desc.FullName()] {
			return
		}
		visited[msg.Desc.FullName()] = true
		for _, f := range msg.Fields {
			if isFieldSkipped(f) || f.Message == nil {
				continue
			}
			cm := extractColumnMeta(f)
			if (cm != nil && cm.InlineNested) || isInline(f) {
				inlineTargetMsgs[f.Message.Desc.FullName()] = true
				collectInlineTargets(f.Message, visited)
			}
		}
	}
	var walkFileMsgs func(msgs []*protogen.Message)
	walkFileMsgs = func(msgs []*protogen.Message) {
		for _, msg := range msgs {
			if isMessageSkipped(msg) {
				continue
			}
			collectInlineTargets(msg, make(map[protoreflect.FullName]bool))
			walkFileMsgs(msg.Messages)
		}
	}
	for _, file := range files {
		if file == nil {
			continue
		}
		walkFileMsgs(file.Messages)
	}
	return inlineTargetMsgs
}

func buildPostgresTablesForFile(file *protogen.File, opts *Options) ([]*postgresTableIR, error) {
	inlineTargetMsgs := collectLakehouseInlineTargetMsgs([]*protogen.File{file})
	return buildPostgresTablesForFileWithInlineTargets(file, inlineTargetMsgs, opts)
}

func buildPostgresTablesForFileWithInlineTargets(file *protogen.File, inlineTargetMsgs map[protoreflect.FullName]bool, opts *Options) ([]*postgresTableIR, error) {
	var tables []*postgresTableIR
	seenTables := make(map[string]protoreflect.FullName)
	for _, msg := range file.Messages {
		if isMessageSkipped(msg) {
			continue
		}
		origMeta := extractDorisTableMeta(msg)
		if origMeta == nil && inlineTargetMsgs[msg.Desc.FullName()] {
			continue
		}
		meta := origMeta
		if meta == nil {
			meta = &DorisTableMetaIR{
				TableName:     toSnakeCase(string(msg.Desc.Name())),
				DatabaseScope: "PUBLIC",
			}
		} else if meta.TableName == "" {
			metaCopy := *meta
			metaCopy.TableName = resolvePostgresTableName(msg, meta)
			meta = &metaCopy
		}
		tbl, err := buildPostgresTableIR(msg, meta, origMeta != nil, opts)
		if err != nil {
			return nil, err
		}
		if len(tbl.Descriptor.Columns) == 0 {
			continue
		}
		schemaName := resolvePostgresTableCollisionSchema(true, meta)
		id := postgresResolvedSchemaTableKey(schemaName, tbl.Descriptor.TableName)
		if prev, exists := seenTables[id]; exists {
			return nil, fmt.Errorf("message %s: duplicate PostgreSQL table identity %q (already declared by %s)", msg.Desc.FullName(), meta.TableName, prev)
		}
		seenTables[id] = msg.Desc.FullName()
		tables = append(tables, tbl)
	}
	return tables, nil
}

func buildPostgresTableIR(msg *protogen.Message, meta *DorisTableMetaIR, strictTableMeta bool, opts *Options) (*postgresTableIR, error) {
	if !isValidSQLIdentifier(meta.TableName) {
		return nil, fmt.Errorf("message %s: invalid SQL identifier for table_name %q", msg.Desc.FullName(), meta.TableName)
	}
	if !isValidPostgresDatabaseScope(meta.DatabaseScope) {
		return nil, fmt.Errorf("message %s: unsupported database_scope %q", msg.Desc.FullName(), meta.DatabaseScope)
	}
	if meta.EmbeddingDimension < 0 {
		return nil, fmt.Errorf("message %s: embedding_dimension cannot be negative (%d)", msg.Desc.FullName(), meta.EmbeddingDimension)
	}

	pkSet := make(map[string]bool)
	for _, uk := range meta.UniqueKeyColumns {
		if !isValidSQLIdentifier(uk) {
			return nil, fmt.Errorf("message %s: invalid SQL identifier %q in unique_key_columns", msg.Desc.FullName(), uk)
		}
		pkSet[uk] = true
	}
	for _, pk := range meta.PrimaryKeyFields {
		if !isValidSQLIdentifier(pk) {
			return nil, fmt.Errorf("message %s: invalid SQL identifier %q in primary_key_fields", msg.Desc.FullName(), pk)
		}
		pkSet[pk] = true
	}
	for _, dk := range meta.DuplicateKeyColumns {
		if !isValidSQLIdentifier(dk) {
			return nil, fmt.Errorf("message %s: invalid SQL identifier %q in duplicate_key_columns", msg.Desc.FullName(), dk)
		}
	}
	partSet := make(map[string]bool)
	for _, pk := range meta.PartitionByColumns {
		if !isValidSQLIdentifier(pk) {
			return nil, fmt.Errorf("message %s: invalid SQL identifier %q in partition_by_columns", msg.Desc.FullName(), pk)
		}
		partSet[pk] = true
	}

	hasAnnotatedCols := messageHasAnnotatedPostgresColumns(msg, make(map[protoreflect.FullName]bool))
	usedColNames := make(map[string]bool)
	var cols []PostgresColumnDescriptor
	var docIDCols []string
	var indexes []PostgresIndexDescriptor
	var fks []PostgresForeignKeyDescriptor

	var walkFields func(fields []*protogen.Field, chain []*protogen.Field, visited map[protoreflect.FullName]bool, parentHasAnnotated bool) error
	walkFields = func(fields []*protogen.Field, chain []*protogen.Field, visited map[protoreflect.FullName]bool, parentHasAnnotated bool) error {
		for _, field := range fields {
			if isFieldSkipped(field) {
				continue
			}
			colMeta := extractColumnMeta(field)
			inlineNested := (colMeta != nil && colMeta.InlineNested) || isInline(field)
			autoFlattenSingularMsg := !strictTableMeta && !parentHasAnnotated &&
				field.Desc.Kind() == protoreflect.MessageKind && !field.Desc.IsList() && !field.Desc.IsMap() &&
				field.Message != nil && !isPostgresSupportedWKT(field) &&
				string(field.Message.Desc.FullName()) != "google.protobuf.Struct" &&
				string(field.Message.Desc.FullName()) != "google.protobuf.Value" &&
				string(field.Message.Desc.FullName()) != "google.protobuf.ListValue" &&
				string(field.Message.Desc.FullName()) != "google.protobuf.Any" &&
				string(field.Message.Desc.FullName()) != "google.protobuf.Empty"

			if inlineNested || autoFlattenSingularMsg {
				if field.Desc.Kind() != protoreflect.MessageKind || field.Desc.IsList() || field.Desc.IsMap() || field.Message == nil {
					return fmt.Errorf("field %s: inline/inline_nested is only valid on singular message fields", field.Desc.FullName())
				}
				nestedFullName := field.Message.Desc.FullName()
				if visited[nestedFullName] {
					if inlineNested {
						return fmt.Errorf("field %s: recursive inline_nested cycle detected on %s", field.Desc.FullName(), nestedFullName)
					}
					continue
				}
				nextVisited := make(map[protoreflect.FullName]bool, len(visited)+1)
				for k, v := range visited {
					nextVisited[k] = v
				}
				nextVisited[nestedFullName] = true
				nextChain := append(append([]*protogen.Field(nil), chain...), field)
				nestedHasAnnotated := messageHasAnnotatedPostgresColumns(field.Message, make(map[protoreflect.FullName]bool))
				if err := walkFields(field.Message.Fields, nextChain, nextVisited, nestedHasAnnotated); err != nil {
					return err
				}
				continue
			}

			if parentHasAnnotated && colMeta == nil {
				continue
			}

			if field.Desc.IsMap() || (field.Desc.Kind() == protoreflect.MessageKind && (!isPostgresSupportedWKT(field) || field.Desc.IsList())) {
				if colMeta != nil {
					return fmt.Errorf("field %s: message or map field cannot be a relational PostgreSQL column without inline/inline_nested", field.Desc.FullName())
				}
				continue
			}

			protoFieldName := string(field.Desc.Name())
			colName := protoFieldName
			hasExplicitName := false
			if isDocumentID(field) && len(chain) == 0 {
				colName = "id"
				hasExplicitName = true
			}
			if override := fieldNameOverride(field); override != "" {
				if errMsg := validateFieldNameOverride(override); errMsg != "" {
					return fmt.Errorf("field %s: %s", field.Desc.FullName(), errMsg)
				}
				colName = override
				hasExplicitName = true
			}
			if colMeta != nil && colMeta.ColumnName != "" {
				if errMsg := validateFieldNameOverride(colMeta.ColumnName); errMsg != "" {
					return fmt.Errorf("field %s: %s", field.Desc.FullName(), errMsg)
				}
				colName = colMeta.ColumnName
				hasExplicitName = true
			}
			if len(chain) > 0 && !hasExplicitName {
				var prefixParts []string
				for _, p := range chain {
					pName := string(p.Desc.Name())
					if ov := fieldNameOverride(p); ov != "" {
						pName = ov
					}
					prefixParts = append(prefixParts, pName)
				}
				colName = strings.Join(prefixParts, "_") + "_" + colName
			}
			if !isValidSQLIdentifier(colName) {
				return fmt.Errorf("field %s: invalid SQL column name %q", field.Desc.FullName(), colName)
			}
			if usedColNames[colName] {
				return fmt.Errorf("field %s: duplicate SQL column name %q", field.Desc.FullName(), colName)
			}
			usedColNames[colName] = true

			if colMeta != nil && colMeta.DefaultValue != "" && !isValidDorisDefaultValue(colMeta.DefaultValue) {
				return fmt.Errorf("field %s: invalid default_value %q", field.Desc.FullName(), colMeta.DefaultValue)
			}

			isPK := pkSet[colName] || (colMeta != nil && (colMeta.IsUniqueKey || colMeta.PrimaryKey)) || (isDocumentID(field) && len(chain) == 0)
			if isDocumentID(field) && len(chain) == 0 {
				docIDCols = append(docIDCols, colName)
			}
			isPart := partSet[colName] || (colMeta != nil && colMeta.IsPartitionKey)
			isSeq := (colMeta != nil && colMeta.IsSequenceCol) || (meta.SequenceCol != "" && meta.SequenceCol == colName)

			pgType, err := defaultPostgresSQLType(field, colMeta, meta, isPK, opts)
			if err != nil {
				return err
			}
			if colMeta != nil && (colMeta.HasDefaultValue || colMeta.DefaultValue != "") {
				if err := validatePostgresColumnDefault(pgType, colMeta.DefaultValue, colMeta.HasDefaultValue); err != nil {
					return fmt.Errorf("field %s: %w", field.Desc.FullName(), err)
				}
			}
			isIdentity := colMeta != nil && colMeta.Identity
			if isIdentity {
				if !isPostgresIdentityEligibleSQLType(pgType) {
					return fmt.Errorf("field %s: identity column requires SMALLINT, INTEGER, or BIGINT type (got %q)", field.Desc.FullName(), pgType)
				}
				if colMeta.Nullable != nil && *colMeta.Nullable {
					return fmt.Errorf("field %s: identity column cannot be nullable", field.Desc.FullName())
				}
				if colMeta.HasDefaultValue || colMeta.DefaultValue != "" || isServerTimestamp(field) {
					return fmt.Errorf("field %s: identity column cannot specify default_value or server_timestamp", field.Desc.FullName())
				}
			}

			isRealOneof := field.Oneof != nil && !field.Oneof.Desc.IsSynthetic()
			nullable := field.Desc.HasOptionalKeyword() || isRealOneof || isPostgresWrapperWKT(field) ||
				(isWellKnownTimestamp(field) && !isPK && !isPart && !isSeq && !isServerTimestamp(field) && colName != "created_at" && colName != "updated_at")
			if colMeta != nil && colMeta.Nullable != nil {
				nullable = *colMeta.Nullable
			}
			if isPK || isPart || isSeq || isIdentity {
				nullable = false
			}

			defVal := ""
			hasDefVal := false
			if colMeta != nil && (colMeta.HasDefaultValue || colMeta.DefaultValue != "") {
				defVal = colMeta.DefaultValue
				hasDefVal = true
			} else if !nullable && !isIdentity {
				upperType := strings.ToUpper(strings.TrimSpace(pgType))
				if strings.HasSuffix(upperType, "[]") {
					defVal = "{}"
				} else if isSeq {
					defVal = "0"
				} else if colName == "record_status" {
					defVal = "ACTIVE"
				}
			}

			var srvDef string
			if isServerTimestamp(field) {
				srvDef = "NOW()"
			}

			if colMeta != nil && colMeta.Unique {
				indexes = append(indexes, PostgresIndexDescriptor{
					Name:    compactPostgresIdentifier("idx", fmt.Sprintf("%s_%s_key", meta.TableName, colName)),
					Columns: []string{colName},
					Unique:  true,
					Using:   "BTREE",
				})
			} else if colMeta != nil && colMeta.Index && !isPK {
				indexes = append(indexes, PostgresIndexDescriptor{
					Name:    compactPostgresIdentifier("idx", fmt.Sprintf("%s_%s", meta.TableName, colName)),
					Columns: []string{colName},
					Using:   "BTREE",
				})
			}

			if colMeta != nil && colMeta.ForeignKey != nil && strings.TrimSpace(colMeta.ForeignKey.RefTable) != "" {
				refTable := strings.TrimSpace(colMeta.ForeignKey.RefTable)
				refCol := strings.TrimSpace(colMeta.ForeignKey.RefColumn)
				if refCol == "" {
					refCol = "id"
				}
				if !isValidSQLIdentifier(refTable) || len(refTable) > maxPostgresIdentifierBytes ||
					!isValidSQLIdentifier(refCol) || len(refCol) > maxPostgresIdentifierBytes {
					return fmt.Errorf("field %s: invalid foreign_key reference %q.%q", field.Desc.FullName(), refTable, refCol)
				}
				onDel := strings.TrimSpace(colMeta.ForeignKey.OnDelete)
				onUpd := strings.TrimSpace(colMeta.ForeignKey.OnUpdate)
				if _, _, ok := isValidForeignKeyAction(onDel); !ok {
					return fmt.Errorf("field %s: invalid foreign_key on_delete %q", field.Desc.FullName(), colMeta.ForeignKey.OnDelete)
				}
				if _, _, ok := isValidForeignKeyAction(onUpd); !ok {
					return fmt.Errorf("field %s: invalid foreign_key on_update %q", field.Desc.FullName(), colMeta.ForeignKey.OnUpdate)
				}
				fks = append(fks, PostgresForeignKeyDescriptor{
					Name:      compactPostgresIdentifier("fk", fmt.Sprintf("%s_%s", meta.TableName, colName)),
					Columns:   []string{colName},
					RefTable:  refTable,
					RefColumn: refCol,
					OnDelete:  onDel,
					OnUpdate:  onUpd,
				})
			}

			cols = append(cols, PostgresColumnDescriptor{
				ColumnName:      colName,
				ProtoFieldName:  protoFieldName,
				SQLType:         pgType,
				PostgresType:    pgType,
				HCLType:         strings.ToLower(pgType),
				Nullable:        nullable,
				IsPrimaryKey:    isPK,
				IsPartitionKey:  isPart,
				InvertedIndex:   colMeta != nil && colMeta.InvertedIndex,
				Identity:        isIdentity,
				DefaultValue:    defVal,
				HasDefaultValue: hasDefVal,
				ServerDefault:   srvDef,
				Comment:         cleanComment(string(field.Comments.Leading)),
			})
		}
		return nil
	}

	rootVisited := map[protoreflect.FullName]bool{
		msg.Desc.FullName(): true,
	}
	if err := walkFields(msg.Fields, nil, rootVisited, hasAnnotatedCols); err != nil {
		return nil, err
	}

	var pks []string
	if len(meta.UniqueKeyColumns) > 0 {
		pks = dedupDorisColumnList(meta.UniqueKeyColumns)
	} else if len(meta.PrimaryKeyFields) > 0 {
		pks = dedupDorisColumnList(meta.PrimaryKeyFields)
	} else if len(docIDCols) > 0 {
		pks = dedupDorisColumnList(docIDCols)
	} else {
		for _, c := range cols {
			if c.IsPrimaryKey {
				pks = append(pks, c.ColumnName)
			}
		}
		pks = dedupDorisColumnList(pks)
	}
	if len(pks) == 0 && len(meta.DuplicateKeyColumns) == 0 {
		for i := range cols {
			if (cols[i].ColumnName == "id" || cols[i].ColumnName == meta.TableName+"_id") && !cols[i].Nullable && !strings.HasSuffix(effectiveColPostgresType(cols[i]), "[]") {
				cols[i].IsPrimaryKey = true
				pks = []string{cols[i].ColumnName}
				break
			}
		}
	}
	if len(pks) == 0 && len(meta.DuplicateKeyColumns) > 0 {
		indexes = append(indexes, PostgresIndexDescriptor{
			Name:    compactPostgresIdentifier("idx", fmt.Sprintf("%s_dup_key", meta.TableName)),
			Columns: dedupDorisColumnList(meta.DuplicateKeyColumns),
			Using:   "BTREE",
		})
	}
	for _, idxIR := range meta.Indexes {
		idxCols := dedupDorisColumnList(idxIR.Columns)
		idxName := strings.TrimSpace(idxIR.Name)
		if idxName != "" && (!isValidSQLIdentifier(idxName) || len(idxName) > maxPostgresIdentifierBytes) {
			return nil, fmt.Errorf("message %s: invalid index name %q", msg.Desc.FullName(), idxIR.Name)
		}
		var cleanExprs []string
		for _, rawExpr := range idxIR.Expressions {
			expr := strings.TrimSpace(rawExpr)
			if !isValidPostgresCheckExpr(expr) {
				return nil, fmt.Errorf("message %s: invalid index expression %q", msg.Desc.FullName(), rawExpr)
			}
			cleanExprs = append(cleanExprs, expr)
		}
		whereExpr := strings.TrimSpace(idxIR.Where)
		if whereExpr != "" && !isValidPostgresCheckExpr(whereExpr) {
			return nil, fmt.Errorf("message %s: invalid index where expression %q", msg.Desc.FullName(), idxIR.Where)
		}
		var parts []PostgresIndexPartDescriptor
		if len(idxIR.Parts) > 0 {
			if len(idxCols) > 0 || len(cleanExprs) > 0 || len(idxIR.DescColumns) > 0 {
				return nil, fmt.Errorf("message %s: index %q cannot combine parts with columns, desc_columns, or expressions", msg.Desc.FullName(), idxIR.Name)
			}
			for _, p := range idxIR.Parts {
				col := strings.TrimSpace(p.Column)
				expr := strings.TrimSpace(p.Expr)
				opclass := strings.TrimSpace(p.Opclass)
				if (col != "") == (expr != "") {
					return nil, fmt.Errorf("message %s: index %q part must specify exactly one of column or expr", msg.Desc.FullName(), idxIR.Name)
				}
				if col != "" && (!isValidSQLIdentifier(col) || len(col) > maxPostgresIdentifierBytes) {
					return nil, fmt.Errorf("message %s: invalid index column %q", msg.Desc.FullName(), col)
				}
				if expr != "" && !isValidPostgresCheckExpr(expr) {
					return nil, fmt.Errorf("message %s: invalid index expression %q", msg.Desc.FullName(), expr)
				}
				if opclass != "" && (!isValidSQLIdentifier(opclass) || len(opclass) > maxPostgresIdentifierBytes) {
					return nil, fmt.Errorf("message %s: invalid index opclass %q", msg.Desc.FullName(), opclass)
				}
				if isPostgresBTreeIncompatibleOpclass(opclass) {
					return nil, fmt.Errorf("message %s: index %q operator class %q is incompatible with BTREE (specify a compatible USING method)", msg.Desc.FullName(), idxIR.Name, opclass)
				}
				parts = append(parts, PostgresIndexPartDescriptor{
					Column:  col,
					Expr:    expr,
					Opclass: opclass,
					Desc:    p.Desc,
				})
			}
		}
		if len(parts) == 0 && len(idxCols) == 0 && len(cleanExprs) == 0 {
			continue
		}
		idxDesc := PostgresIndexDescriptor{
			Name:        idxName,
			Columns:     idxCols,
			Unique:      idxIR.Unique,
			Using:       "BTREE",
			DescColumns: idxIR.DescColumns,
			Where:       whereExpr,
			Expressions: cleanExprs,
			Parts:       parts,
		}
		if idxDesc.Name == "" {
			suffix := postgresIndexSuffix(idxDesc)
			if idxIR.Unique {
				idxDesc.Name = compactPostgresIdentifier("idx", fmt.Sprintf("%s_%s_key", meta.TableName, suffix))
			} else {
				idxDesc.Name = compactPostgresIdentifier("idx", fmt.Sprintf("%s_%s", meta.TableName, suffix))
			}
		}
		indexes = append(indexes, idxDesc)
	}
	for _, fkIR := range meta.ForeignKeys {
		cols := dedupDorisColumnList(fkIR.Columns)
		if len(cols) == 0 || fkIR.RefTable == "" {
			continue
		}
		refTable := strings.TrimSpace(fkIR.RefTable)
		if !isValidSQLIdentifier(refTable) || len(refTable) > maxPostgresIdentifierBytes {
			return nil, fmt.Errorf("message %s: invalid foreign_key ref_table %q", msg.Desc.FullName(), fkIR.RefTable)
		}
		refCols := dedupDorisColumnList(fkIR.RefColumns)
		if len(refCols) == 0 {
			refCols = []string{"id"}
		}
		for _, rc := range refCols {
			if !isValidSQLIdentifier(rc) || len(rc) > maxPostgresIdentifierBytes {
				return nil, fmt.Errorf("message %s: invalid foreign_key ref_column %q", msg.Desc.FullName(), rc)
			}
		}
		if len(cols) != len(refCols) {
			return nil, fmt.Errorf("message %s: foreign_key columns count (%d) does not match ref_columns count (%d)",
				msg.Desc.FullName(), len(cols), len(refCols))
		}
		fkName := strings.TrimSpace(fkIR.Name)
		if fkName == "" {
			fkName = compactPostgresIdentifier("fk", fmt.Sprintf("%s_%s", meta.TableName, strings.Join(cols, "_")))
		}
		if !isValidSQLIdentifier(fkName) || len(fkName) > maxPostgresIdentifierBytes {
			return nil, fmt.Errorf("message %s: invalid foreign_key name %q", msg.Desc.FullName(), fkName)
		}
		onDel := strings.TrimSpace(fkIR.OnDelete)
		onUpd := strings.TrimSpace(fkIR.OnUpdate)
		if _, _, ok := isValidForeignKeyAction(onDel); !ok {
			return nil, fmt.Errorf("message %s: invalid foreign_key on_delete %q", msg.Desc.FullName(), fkIR.OnDelete)
		}
		if _, _, ok := isValidForeignKeyAction(onUpd); !ok {
			return nil, fmt.Errorf("message %s: invalid foreign_key on_update %q", msg.Desc.FullName(), fkIR.OnUpdate)
		}
		fks = append(fks, PostgresForeignKeyDescriptor{
			Name:       fkName,
			Columns:    cols,
			RefTable:   refTable,
			RefColumn:  refCols[0],
			RefColumns: refCols,
			OnDelete:   onDel,
			OnUpdate:   onUpd,
		})
	}

	if !meta.IsSingleton && (meta.SingletonCheckName != "" || meta.SingletonCheckExpr != "") {
		return nil, fmt.Errorf("message %s: singleton_check_name/singleton_check_expr requires is_singleton=true", msg.Desc.FullName())
	}

	var checks []PostgresCheckDescriptor
	if meta.IsSingleton {
		hasSingletonCol := false
		for i := range cols {
			if cols[i].ColumnName == "singleton" {
				if cols[i].DefaultValue != "" && (strings.EqualFold(cols[i].DefaultValue, "false") || cols[i].DefaultValue == "0") {
					return nil, fmt.Errorf("message %s: singleton table cannot have singleton column with default false", msg.Desc.FullName())
				}
				cols[i].Nullable = false
				cols[i].IsPrimaryKey = true
				hasSingletonCol = true
				break
			}
		}
		if !hasSingletonCol {
			cols = append([]PostgresColumnDescriptor{{
				ColumnName:   "singleton",
				SQLType:      "BOOLEAN",
				PostgresType: "BOOLEAN",
				HCLType:      "boolean",
				Nullable:     false,
				DefaultValue: "true",
				IsPrimaryKey: true,
			}}, cols...)
			usedColNames["singleton"] = true
		}
		pks = []string{"singleton"}
		chkName := meta.SingletonCheckName
		if chkName == "" {
			chkName = compactPostgresIdentifier("chk", meta.TableName+"_singleton")
		}
		chkExpr := meta.SingletonCheckExpr
		if chkExpr == "" {
			chkExpr = "(singleton = true)"
		}
		if !isValidSQLIdentifier(chkName) || len(chkName) > maxPostgresIdentifierBytes {
			return nil, fmt.Errorf("message %s: invalid singleton check constraint name %q", msg.Desc.FullName(), chkName)
		}
		if !isValidPostgresCheckExpr(chkExpr) {
			return nil, fmt.Errorf("message %s: invalid singleton check constraint expression %q", msg.Desc.FullName(), chkExpr)
		}
		checks = append(checks, PostgresCheckDescriptor{
			Name: chkName,
			Expr: chkExpr,
		})
	}
	seenChk := make(map[string]bool)
	for _, c := range checks {
		seenChk[c.Name] = true
	}
	for _, chkIR := range meta.Checks {
		chkName := chkIR.Name
		if chkName == "" {
			chkName = compactPostgresIdentifier("chk", meta.TableName+"_"+fmt.Sprintf("%d", len(checks)+1))
		}
		if !isValidSQLIdentifier(chkName) || len(chkName) > maxPostgresIdentifierBytes {
			return nil, fmt.Errorf("message %s: invalid check constraint name %q", msg.Desc.FullName(), chkName)
		}
		if seenChk[chkName] {
			return nil, fmt.Errorf("message %s: duplicate check constraint name %q", msg.Desc.FullName(), chkName)
		}
		if !isValidPostgresCheckExpr(chkIR.Expr) {
			return nil, fmt.Errorf("message %s: invalid check constraint expression %q", msg.Desc.FullName(), chkIR.Expr)
		}
		seenChk[chkName] = true
		checks = append(checks, PostgresCheckDescriptor{
			Name: chkName,
			Expr: chkIR.Expr,
		})
	}

	var partCols []string
	if len(meta.PartitionByColumns) > 0 {
		partCols = dedupDorisColumnList(meta.PartitionByColumns)
	} else {
		for _, c := range cols {
			if c.IsPartitionKey {
				partCols = append(partCols, c.ColumnName)
			}
		}
		partCols = dedupDorisColumnList(partCols)
	}
	if meta.IsSingleton && len(partCols) > 0 {
		return nil, fmt.Errorf("message %s: is_singleton=true cannot be combined with partition_by_columns", msg.Desc.FullName())
	}
	if len(pks) > 0 && len(partCols) > 0 {
		pkMap := make(map[string]bool, len(pks))
		for _, k := range pks {
			pkMap[k] = true
		}
		for _, pc := range partCols {
			if !pkMap[pc] {
				pks = append(pks, pc)
				pkMap[pc] = true
			}
		}
	}

	seenInv := make(map[string]bool)
	var invCols []string
	for _, c := range cols {
		if c.InvertedIndex && !seenInv[c.ColumnName] {
			seenInv[c.ColumnName] = true
			invCols = append(invCols, c.ColumnName)
		}
	}
	for _, ic := range meta.InvertedIndexColumns {
		if ic != "" && !seenInv[ic] {
			seenInv[ic] = true
			invCols = append(invCols, ic)
		}
	}
	ngramCols := dedupDorisColumnList(meta.NgramBfColumns)

	checkColExists := func(label string, names []string) error {
		for _, name := range names {
			if name != "" && !usedColNames[name] {
				return fmt.Errorf("message %s: %s references unknown column %q", msg.Desc.FullName(), label, name)
			}
		}
		return nil
	}
	if err := checkColExists("unique_key_columns", pks); err != nil {
		return nil, err
	}
	if err := checkColExists("duplicate_key_columns", meta.DuplicateKeyColumns); err != nil {
		return nil, err
	}
	if err := checkColExists("partition_by_columns", partCols); err != nil {
		return nil, err
	}
	if err := checkColExists("inverted_index_columns", invCols); err != nil {
		return nil, err
	}
	if err := checkColExists("ngram_bf_columns", ngramCols); err != nil {
		return nil, err
	}
	for _, idx := range indexes {
		if err := checkColExists("indexes", idx.Columns); err != nil {
			return nil, err
		}
		for _, p := range idx.Parts {
			if p.Column != "" && !usedColNames[p.Column] {
				return nil, fmt.Errorf("message %s: indexes part references unknown column %q", msg.Desc.FullName(), p.Column)
			}
		}
	}
	for _, fk := range fks {
		if err := checkColExists("foreign_keys", fk.Columns); err != nil {
			return nil, err
		}
	}

	desc := PostgresTableDescriptor{
		TableName:            meta.TableName,
		MessageName:          msg.GoIdent.GoName,
		DatabaseScope:        meta.DatabaseScope,
		WriterService:        meta.WriterService,
		ReaderServices:       append([]string(nil), meta.ReaderServices...),
		PrimaryKeyColumns:    pks,
		PartitionByColumns:   partCols,
		InvertedIndexColumns: invCols,
		NgramBfColumns:       ngramCols,
		EmbeddingDimension:   meta.EmbeddingDimension,
		Indexes:              indexes,
		ForeignKeys:          fks,
		Columns:              cols,
		Checks:               checks,
		IsSingleton:          meta.IsSingleton,
	}

	return &postgresTableIR{
		Msg:        msg,
		GoName:     msg.GoIdent.GoName,
		Descriptor: desc,
	}, nil
}

func resolvePostgresSchemaName(scope, tenantSlug string) (string, bool) {
	switch scope {
	case "PUBLIC":
		return "public", true
	case "GLOBAL_TERMINOLOGY":
		return "terminology", true
	case "GLOBAL_TERMINOLOGY_RAG":
		return "terminology_rag", true
	case "GLOBAL_TELEMETRY":
		return "telemetry", true
	default:
		slug, ok := sanitizeDorisTenantSlug(tenantSlug)
		if !ok {
			return "", false
		}
		return "tenant_" + slug, true
	}
}

func normPostgresIndexUsing(using string) string {
	u := strings.ToUpper(strings.TrimSpace(using))
	if u == "" {
		return "BTREE"
	}
	return u
}

func postgresIndexesEffectivelyEqual(a, b PostgresIndexDescriptor) bool {
	if a.Unique != b.Unique {
		return false
	}
	usingA := normPostgresIndexUsing(a.Using)
	usingB := normPostgresIndexUsing(b.Using)
	if usingA != usingB {
		return false
	}
	if strings.TrimSpace(a.Where) != strings.TrimSpace(b.Where) {
		return false
	}
	partsA := canonicalPostgresIndexParts(a)
	partsB := canonicalPostgresIndexParts(b)
	opsA := strings.TrimSpace(a.Ops)
	opsB := strings.TrimSpace(b.Ops)
	if usingA != "BTREE" && opsA != "" && len(partsA) == 1 && partsA[0].Opclass == "" {
		partsA[0].Opclass = opsA
		opsA = ""
	} else if len(partsA) == 1 && partsA[0].Opclass != "" && opsA == partsA[0].Opclass {
		opsA = ""
	}
	if usingB != "BTREE" && opsB != "" && len(partsB) == 1 && partsB[0].Opclass == "" {
		partsB[0].Opclass = opsB
		opsB = ""
	} else if len(partsB) == 1 && partsB[0].Opclass != "" && opsB == partsB[0].Opclass {
		opsB = ""
	}
	if !postgresIndexPartsEqual(partsA, partsB) {
		return false
	}
	return opsA == opsB
}

func effectivePostgresColumnDefaultClause(pgType string, c PostgresColumnDescriptor) (string, bool) {
	if c.Identity {
		return "", false
	}
	if strings.TrimSpace(c.ServerDefault) != "" {
		return strings.ToUpper(strings.TrimSpace(c.ServerDefault)), true
	}
	if c.HasDefaultValue || c.DefaultValue != "" {
		return formatPostgresDefaultLiteral(pgType, c.DefaultValue), true
	}
	return "", false
}

func formatPostgresDefaultLiteral(pgType, defVal string) string {
	upper := strings.ToUpper(strings.TrimSpace(pgType))
	trimmed := strings.TrimSpace(defVal)
	if trimmed == "[]" && strings.HasSuffix(upper, "[]") {
		return "'{}'"
	}
	if strings.HasSuffix(upper, "[]") {
		return fmt.Sprintf("'%s'", strings.ReplaceAll(trimmed, "'", "''"))
	}
	if isPostgresBooleanSQLType(upper) && validatePostgresDefaultValue(upper, trimmed) == nil {
		return strings.ToLower(trimmed)
	}
	if isPostgresNumericSQLType(upper) && validatePostgresDefaultValue(upper, trimmed) == nil {
		return trimmed
	}
	return fmt.Sprintf("'%s'", strings.ReplaceAll(defVal, "'", "''"))
}

// BuildPostgresCreateTableWithoutForeignKeysSQL renders a deterministic PostgreSQL CREATE TABLE
// statement (plus any secondary CREATE INDEX statements, without foreign key constraints)
// for this descriptor.
func (t PostgresTableDescriptor) BuildPostgresCreateTableWithoutForeignKeysSQL(tenantSlug string) string {
	if !validatePostgresDescriptor(t) || len(t.Columns) == 0 {
		return ""
	}
	schemaName, ok := resolvePostgresSchemaName(t.DatabaseScope, tenantSlug)
	if !ok {
		return ""
	}
	pks := dedupDorisColumnList(t.PrimaryKeyColumns)
	if len(pks) == 0 {
		for _, c := range t.Columns {
			if c.IsPrimaryKey {
				pks = append(pks, c.ColumnName)
			}
		}
		pks = dedupDorisColumnList(pks)
	}
	partCols := effectivePostgresPartitionCols(t)
	if len(pks) > 0 && len(partCols) > 0 {
		pkMap := make(map[string]bool, len(pks))
		for _, k := range pks {
			pkMap[k] = true
		}
		for _, pc := range partCols {
			if !pkMap[pc] {
				pks = append(pks, pc)
				pkMap[pc] = true
			}
		}
	}

	colTypes := make(map[string]string, len(t.Columns))
	var colLines []string
	for _, c := range t.Columns {
		upperType := strings.ToUpper(effectiveColPostgresType(c))
		colTypes[c.ColumnName] = upperType
		line := fmt.Sprintf("    %s %s", c.ColumnName, upperType)
		if !c.Nullable {
			line += " NOT NULL"
		}
		if c.Identity {
			line += " GENERATED ALWAYS AS IDENTITY"
		} else if defClause, hasDef := effectivePostgresColumnDefaultClause(upperType, c); hasDef {
			line += " DEFAULT " + defClause
		}
		colLines = append(colLines, line)
	}
	if len(pks) > 0 {
		colLines = append(colLines, fmt.Sprintf("    PRIMARY KEY (%s)", strings.Join(pks, ", ")))
	}
	for _, chk := range t.Checks {
		if isValidPostgresCheckExpr(chk.Expr) {
			chkName := chk.Name
			if chkName == "" {
				chkName = compactPostgresIdentifier("chk", t.TableName)
			}
			colLines = append(colLines, fmt.Sprintf("    CONSTRAINT %q CHECK (%s)", chkName, chk.Expr))
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE IF NOT EXISTS %s.%s (\n", schemaName, t.TableName)
	b.WriteString(strings.Join(colLines, ",\n"))
	b.WriteString("\n)")
	if len(partCols) > 0 {
		partMethod := "LIST"
		if colTypes[partCols[0]] == "DATE" || colTypes[partCols[0]] == "TIMESTAMPTZ" {
			partMethod = "RANGE"
		}
		fmt.Fprintf(&b, "\nPARTITION BY %s (%s)", partMethod, strings.Join(partCols, ", "))
	}
	b.WriteString(";")

	if t.IsSingleton {
		var seedCols []string
		var seedVals []string
		for _, c := range t.Columns {
			seedCols = append(seedCols, c.ColumnName)
			if c.ColumnName == "singleton" {
				seedVals = append(seedVals, "true")
			} else if c.ServerDefault == "" && (c.HasDefaultValue || c.DefaultValue != "") {
				upperType := strings.ToUpper(effectiveColPostgresType(c))
				seedVals = append(seedVals, formatPostgresDefaultLiteral(upperType, c.DefaultValue))
			} else {
				seedVals = append(seedVals, "DEFAULT")
			}
		}
		fmt.Fprintf(&b, "\n-- SEED_SINGLETON: INSERT INTO %s.%s (%s) VALUES (%s) ON CONFLICT (singleton) DO NOTHING;",
			schemaName, t.TableName, strings.Join(seedCols, ", "), strings.Join(seedVals, ", "))
	}

	emittedIdx := make(map[string]bool)
	seenInv := make(map[string]bool)
	var invCols []string
	for _, c := range t.Columns {
		if c.InvertedIndex && !seenInv[c.ColumnName] {
			seenInv[c.ColumnName] = true
			invCols = append(invCols, c.ColumnName)
		}
	}
	for _, ic := range t.InvertedIndexColumns {
		if ic != "" && !seenInv[ic] {
			seenInv[ic] = true
			invCols = append(invCols, ic)
		}
	}
	for _, idxCol := range invCols {
		idxName := postgresSyntheticGinIndexName(t.TableName, idxCol)
		emittedIdx[idxName] = true
		fmt.Fprintf(&b, "\nCREATE INDEX IF NOT EXISTS %s ON %s.%s USING gin (%s);", idxName, schemaName, t.TableName, idxCol)
	}
	for _, ngCol := range dedupDorisColumnList(t.NgramBfColumns) {
		idxName := postgresSyntheticNgramIndexName(t.TableName, ngCol)
		emittedIdx[idxName] = true
		fmt.Fprintf(&b, "\nCREATE INDEX IF NOT EXISTS %s ON %s.%s USING gin (%s);", idxName, schemaName, t.TableName, ngCol)
	}
	for _, c := range t.Columns {
		if strings.HasPrefix(colTypes[c.ColumnName], "VECTOR(") {
			idxName := postgresSyntheticHnswIndexName(c.ColumnName)
			emittedIdx[idxName] = true
			fmt.Fprintf(&b, "\nCREATE INDEX IF NOT EXISTS %s ON %s.%s USING hnsw (%s vector_l2_ops);", idxName, schemaName, t.TableName, c.ColumnName)
		}
	}
	for _, idx := range t.Indexes {
		if idx.Name == "" || emittedIdx[idx.Name] || (len(idx.Parts) == 0 && len(idx.Columns) == 0 && len(idx.Expressions) == 0) {
			continue
		}
		emittedIdx[idx.Name] = true
		uniqueKw := ""
		if idx.Unique {
			uniqueKw = "UNIQUE "
		}
		colParts := formatPostgresIndexPartsSQL(idx, false)
		using := strings.ToLower(strings.TrimSpace(idx.Using))
		whereClause := ""
		if idx.Where != "" {
			whereClause = " WHERE " + idx.Where
		}
		if using == "" || using == "btree" {
			fmt.Fprintf(&b, "\nCREATE %sINDEX IF NOT EXISTS %s ON %s.%s (%s)%s;", uniqueKw, idx.Name, schemaName, t.TableName, strings.Join(colParts, ", "), whereClause)
		} else if idx.Ops != "" && len(colParts) == 1 && (len(idx.Parts) != 1 || idx.Parts[0].Opclass == "") {
			fmt.Fprintf(&b, "\nCREATE %sINDEX IF NOT EXISTS %s ON %s.%s USING %s (%s %s)%s;", uniqueKw, idx.Name, schemaName, t.TableName, using, colParts[0], idx.Ops, whereClause)
		} else {
			fmt.Fprintf(&b, "\nCREATE %sINDEX IF NOT EXISTS %s ON %s.%s USING %s (%s)%s;", uniqueKw, idx.Name, schemaName, t.TableName, using, strings.Join(colParts, ", "), whereClause)
		}
	}
	return b.String()
}

// BuildPostgresForeignKeysSQL renders ALTER TABLE ... ADD CONSTRAINT ... FOREIGN KEY
// statements for this descriptor so foreign keys can be emitted after all tables exist.
func (t PostgresTableDescriptor) BuildPostgresForeignKeysSQL(tenantSlug string) string {
	if !validatePostgresDescriptor(t) || len(t.Columns) == 0 || len(t.ForeignKeys) == 0 {
		return ""
	}
	schemaName, ok := resolvePostgresSchemaName(t.DatabaseScope, tenantSlug)
	if !ok {
		return ""
	}
	var lines []string
	for _, fk := range t.ForeignKeys {
		if fk.Name == "" || fk.RefTable == "" || len(fk.Columns) == 0 {
			continue
		}
		refCols := fk.EffectiveRefColumns()
		clause := fmt.Sprintf("ALTER TABLE %s.%s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s.%s (%s)",
			schemaName, t.TableName, fk.Name, strings.Join(fk.Columns, ", "), schemaName, fk.RefTable, strings.Join(refCols, ", "))
		if _, delSQL, ok := isValidForeignKeyAction(fk.OnDelete); ok && delSQL != "" {
			clause += " ON DELETE " + delSQL
		}
		if _, updSQL, ok := isValidForeignKeyAction(fk.OnUpdate); ok && updSQL != "" {
			clause += " ON UPDATE " + updSQL
		}
		lines = append(lines, clause+";")
	}
	return strings.Join(lines, "\n")
}

// BuildPostgresCreateTableSQL renders a deterministic PostgreSQL CREATE TABLE statement
// (plus any secondary GIN or pgvector HNSW CREATE INDEX and FOREIGN KEY statements) for this descriptor.
func (t PostgresTableDescriptor) BuildPostgresCreateTableSQL(tenantSlug string) string {
	base := t.BuildPostgresCreateTableWithoutForeignKeysSQL(tenantSlug)
	if base == "" {
		return ""
	}
	if fks := t.BuildPostgresForeignKeysSQL(tenantSlug); fks != "" {
		return base + "\n" + fks
	}
	return base
}

func postgresTypeToAtlasHCLType(pgType string) string {
	pt := strings.TrimSpace(pgType)
	if pt == "" {
		return "varchar(256)"
	}
	lower := strings.ToLower(pt)
	lower = strings.ReplaceAll(lower, ", ", ",")
	lower = strings.ReplaceAll(lower, ",", ", ")
	if lower == "double precision" {
		return "double_precision"
	}
	if strings.HasPrefix(lower, "vector(") || strings.HasSuffix(lower, "[]") || strings.Contains(lower, " ") {
		return fmt.Sprintf("sql(%s)", quoteHCLString(lower))
	}
	return lower
}

func hclAttrTypeToPostgresType(rawType string) string {
	t := strings.TrimSpace(rawType)
	if strings.HasPrefix(t, "sql(") && strings.HasSuffix(t, ")") {
		t = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, "sql("), ")"))
	}
	t = unquoteHCLString(t)
	upper := strings.ToUpper(strings.TrimSpace(t))
	upper = strings.ReplaceAll(upper, "DOUBLE_PRECISION", "DOUBLE PRECISION")
	upper = strings.ReplaceAll(upper, ", ", ",")
	upper = strings.ReplaceAll(upper, ",", ", ")
	return upper
}

// BuildPostgresAtlasHCL renders a deterministic Atlas HCL table block for this
// PostgreSQL table descriptor.
func (t PostgresTableDescriptor) BuildPostgresAtlasHCL(tenantSlug string) string {
	if !validatePostgresDescriptor(t) || len(t.Columns) == 0 {
		return ""
	}
	schemaName, ok := resolvePostgresSchemaName(t.DatabaseScope, tenantSlug)
	if !ok {
		return ""
	}
	var schemaRef string
	if strings.Contains(schemaName, "${") {
		schemaRef = fmt.Sprintf("%q", "schema."+schemaName)
	} else {
		schemaRef = "schema." + schemaName
	}

	pks := dedupDorisColumnList(t.PrimaryKeyColumns)
	if len(pks) == 0 {
		for _, c := range t.Columns {
			if c.IsPrimaryKey {
				pks = append(pks, c.ColumnName)
			}
		}
		pks = dedupDorisColumnList(pks)
	}
	partCols := effectivePostgresPartitionCols(t)
	if len(pks) > 0 && len(partCols) > 0 {
		pkMap := make(map[string]bool, len(pks))
		for _, k := range pks {
			pkMap[k] = true
		}
		for _, pc := range partCols {
			if !pkMap[pc] {
				pks = append(pks, pc)
				pkMap[pc] = true
			}
		}
	}

	colTypes := make(map[string]string, len(t.Columns))
	var b strings.Builder
	fmt.Fprintf(&b, "table %q {\n", t.TableName)
	fmt.Fprintf(&b, "  schema = %s\n", schemaRef)
	for _, c := range t.Columns {
		colType := effectiveColPostgresType(c)
		upperType := strings.ToUpper(colType)
		colTypes[c.ColumnName] = upperType
		fmt.Fprintf(&b, "  column %q {\n", c.ColumnName)
		fmt.Fprintf(&b, "    type = %s\n", postgresTypeToAtlasHCLType(colType))
		fmt.Fprintf(&b, "    null = %t\n", c.Nullable)
		if c.Identity {
			b.WriteString("    identity {\n")
			b.WriteString("      generated = ALWAYS\n")
			b.WriteString("    }\n")
		} else {
			normCol := c
			if normCol.SQLType == "" {
				normCol.SQLType = colType
			}
			if normCol.HCLType == "" {
				normCol.HCLType = strings.ToLower(colType)
			}
			if def := formatPostgresHCLDefaultValue(normCol); def != "" {
				fmt.Fprintf(&b, "    default = %s\n", def)
			}
		}
		if c.Comment != "" {
			fmt.Fprintf(&b, "    comment = %s\n", quoteHCLString(c.Comment))
		}
		b.WriteString("  }\n")
	}
	if len(pks) > 0 {
		b.WriteString("  primary_key {\n")
		fmt.Fprintf(&b, "    columns = %s\n", formatAtlasHCLColRefList(pks))
		b.WriteString("  }\n")
	}
	if len(partCols) > 0 {
		partType := "LIST"
		if colTypes[partCols[0]] == "DATE" || colTypes[partCols[0]] == "TIMESTAMPTZ" {
			partType = "RANGE"
		}
		b.WriteString("  partition {\n")
		fmt.Fprintf(&b, "    type = %s\n", partType)
		fmt.Fprintf(&b, "    columns = %s\n", formatAtlasHCLColRefList(partCols))
		b.WriteString("  }\n")
	}

	emittedIdx := make(map[string]bool)
	seenInv := make(map[string]bool)
	var invCols []string
	for _, c := range t.Columns {
		if c.InvertedIndex && !seenInv[c.ColumnName] {
			seenInv[c.ColumnName] = true
			invCols = append(invCols, c.ColumnName)
		}
	}
	for _, ic := range t.InvertedIndexColumns {
		if ic != "" && !seenInv[ic] {
			seenInv[ic] = true
			invCols = append(invCols, ic)
		}
	}
	for _, idxCol := range invCols {
		idxName := postgresSyntheticGinIndexName(t.TableName, idxCol)
		emittedIdx[idxName] = true
		fmt.Fprintf(&b, "  index %q {\n", idxName)
		fmt.Fprintf(&b, "    columns = [column.%s]\n", idxCol)
		b.WriteString("    type = \"GIN\"\n")
		b.WriteString("  }\n")
	}
	for _, ngCol := range dedupDorisColumnList(t.NgramBfColumns) {
		idxName := postgresSyntheticNgramIndexName(t.TableName, ngCol)
		emittedIdx[idxName] = true
		fmt.Fprintf(&b, "  index %q {\n", idxName)
		fmt.Fprintf(&b, "    columns = [column.%s]\n", ngCol)
		b.WriteString("    type = \"GIN\"\n")
		b.WriteString("  }\n")
	}
	for _, c := range t.Columns {
		if strings.HasPrefix(colTypes[c.ColumnName], "VECTOR(") {
			idxName := postgresSyntheticHnswIndexName(c.ColumnName)
			emittedIdx[idxName] = true
			fmt.Fprintf(&b, "  index %q {\n", idxName)
			fmt.Fprintf(&b, "    columns = [column.%s]\n", c.ColumnName)
			b.WriteString("    type = \"HNSW\"\n")
			b.WriteString("    ops = \"vector_l2_ops\"\n")
			b.WriteString("  }\n")
		}
	}
	for _, idx := range t.Indexes {
		if idx.Name == "" || emittedIdx[idx.Name] || (len(idx.Parts) == 0 && len(idx.Columns) == 0 && len(idx.Expressions) == 0) {
			continue
		}
		emittedIdx[idx.Name] = true
		using := strings.ToUpper(strings.TrimSpace(idx.Using))
		if using == "" {
			using = "BTREE"
		}
		fmt.Fprintf(&b, "  index %q {\n", idx.Name)
		if idx.Unique {
			b.WriteString("    unique = true\n")
		}
		emitPostgresAtlasIndexOnBlocks(&b, idx)
		fmt.Fprintf(&b, "    type = %q\n", using)
		if idx.Ops != "" {
			fmt.Fprintf(&b, "    ops = %s\n", formatPostgresAtlasHCLOpclass(idx.Ops))
		}
		if idx.Where != "" {
			fmt.Fprintf(&b, "    where = %s\n", quoteHCLString(idx.Where))
		}
		b.WriteString("  }\n")
	}
	for _, fk := range t.ForeignKeys {
		if fk.Name == "" || fk.RefTable == "" || len(fk.Columns) == 0 {
			continue
		}
		refCols := fk.EffectiveRefColumns()
		refColRefs := make([]string, 0, len(refCols))
		for _, rc := range refCols {
			refColRefs = append(refColRefs, fmt.Sprintf("table.%s.column.%s", fk.RefTable, rc))
		}
		fmt.Fprintf(&b, "  foreign_key %q {\n", fk.Name)
		fmt.Fprintf(&b, "    columns     = %s\n", formatAtlasHCLColRefList(fk.Columns))
		fmt.Fprintf(&b, "    ref_columns = [%s]\n", strings.Join(refColRefs, ", "))
		if delHCL, _, ok := isValidForeignKeyAction(fk.OnDelete); ok && delHCL != "" {
			fmt.Fprintf(&b, "    on_delete   = %s\n", delHCL)
		}
		if updHCL, _, ok := isValidForeignKeyAction(fk.OnUpdate); ok && updHCL != "" {
			fmt.Fprintf(&b, "    on_update   = %s\n", updHCL)
		}
		b.WriteString("  }\n")
	}
	for _, chk := range t.Checks {
		fmt.Fprintf(&b, "  check %q {\n", chk.Name)
		fmt.Fprintf(&b, "    expr = %s\n", quoteHCLString(chk.Expr))
		b.WriteString("  }\n")
	}
	b.WriteString("}")
	return b.String()
}

// ParsePostgresAtlasHCL parses one or more Atlas HCL table blocks into
// []PostgresTableDescriptor values.
func ParsePostgresAtlasHCL(hclContent string) ([]PostgresTableDescriptor, error) {
	cleaned, err := stripAtlasHCLComments(hclContent)
	if err != nil {
		return nil, err
	}
	var tables []PostgresTableDescriptor
	pos := 0
	for pos < len(cleaned) {
		for pos < len(cleaned) && (cleaned[pos] == ' ' || cleaned[pos] == '\t' || cleaned[pos] == '\n' || cleaned[pos] == '\r') {
			pos++
		}
		if pos >= len(cleaned) {
			break
		}
		openIdx := indexUnquotedHCLBrace(cleaned[pos:])
		if openIdx < 0 {
			return nil, fmt.Errorf("unexpected trailing content in PostgreSQL Atlas HCL: %q", strings.TrimSpace(cleaned[pos:]))
		}
		openAbs := pos + openIdx
		header := strings.TrimSpace(cleaned[pos:openAbs])
		closeAbs, err := findMatchingHCLBrace(cleaned, openAbs)
		if err != nil {
			return nil, err
		}
		body := cleaned[openAbs+1 : closeAbs]
		pos = closeAbs + 1
		if strings.HasPrefix(header, "schema ") || header == "schema" {
			continue
		}
		if !strings.HasPrefix(header, "table ") {
			return nil, fmt.Errorf("unexpected top-level HCL block %q (expected table \"<name>\")", header)
		}
		tableName := unquoteHCLString(strings.TrimSpace(strings.TrimPrefix(header, "table")))
		if !isValidSQLIdentifier(tableName) {
			return nil, fmt.Errorf("invalid table name %q in PostgreSQL Atlas HCL", tableName)
		}
		desc, err := parsePostgresAtlasTableBlock(tableName, body)
		if err != nil {
			return nil, fmt.Errorf("table %q: %w", tableName, err)
		}
		tables = append(tables, desc)
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("no table blocks found in PostgreSQL Atlas HCL")
	}
	return tables, nil
}

func parsePostgresAtlasTableBlock(tableName, body string) (PostgresTableDescriptor, error) {
	desc := PostgresTableDescriptor{TableName: tableName, DatabaseScope: "PUBLIC"}
	colOrder := make([]string, 0)
	colMap := make(map[string]PostgresColumnDescriptor)
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
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == "" {
			if lineEnd < 0 {
				break
			}
			i += lineEnd + 1
			continue
		}
		if indexUnquotedHCLBrace(trimmedLine) < 0 {
			eqIdx := strings.IndexByte(trimmedLine, '=')
			if eqIdx <= 0 {
				return desc, fmt.Errorf("unexpected statement %q", trimmedLine)
			}
			k := unquoteHCLString(trimmedLine[:eqIdx])
			v := strings.TrimSpace(trimmedLine[eqIdx+1:])
			if k == "schema" {
				rawSchema := unquoteHCLString(v)
				rawSchema = strings.TrimPrefix(rawSchema, "schema.")
				switch rawSchema {
				case "public":
					desc.DatabaseScope = "PUBLIC"
				case "terminology":
					desc.DatabaseScope = "GLOBAL_TERMINOLOGY"
				case "terminology_rag":
					desc.DatabaseScope = "GLOBAL_TERMINOLOGY_RAG"
				case "telemetry":
					desc.DatabaseScope = "GLOBAL_TELEMETRY"
				default:
					desc.DatabaseScope = "TENANT"
				}
			}
			if lineEnd < 0 {
				break
			}
			i += lineEnd + 1
			continue
		}
		openRel := indexUnquotedHCLBrace(body[i:])
		openAbs := i + openRel
		blockHeader := strings.TrimSpace(body[i:openAbs])
		closeAbs, err := findMatchingHCLBrace(body, openAbs)
		if err != nil {
			return desc, err
		}
		blockBody := body[openAbs+1 : closeAbs]
		i = closeAbs + 1
		if err := applyPostgresAtlasSubBlock(&desc, blockHeader, blockBody, &colOrder, colMap); err != nil {
			return desc, err
		}
	}

	pkSet := make(map[string]bool, len(desc.PrimaryKeyColumns))
	for _, pk := range desc.PrimaryKeyColumns {
		if _, exists := colMap[pk]; !exists {
			return desc, fmt.Errorf("primary_key references unknown column %q", pk)
		}
		pkSet[pk] = true
	}
	partSet := make(map[string]bool, len(desc.PartitionByColumns))
	for _, pk := range desc.PartitionByColumns {
		if _, exists := colMap[pk]; !exists {
			return desc, fmt.Errorf("partition references unknown column %q", pk)
		}
		partSet[pk] = true
	}
	for _, idx := range desc.Indexes {
		for _, c := range idx.Columns {
			if _, exists := colMap[c]; !exists {
				return desc, fmt.Errorf("index %q references unknown column %q", idx.Name, c)
			}
		}
		for _, p := range idx.Parts {
			if p.Column != "" {
				if _, exists := colMap[p.Column]; !exists {
					return desc, fmt.Errorf("index %q on block references unknown column %q", idx.Name, p.Column)
				}
			}
		}
	}
	for _, fk := range desc.ForeignKeys {
		for _, c := range fk.Columns {
			if _, exists := colMap[c]; !exists {
				return desc, fmt.Errorf("foreign_key %q references unknown column %q", fk.Name, c)
			}
		}
	}
	invSet := make(map[string]bool, len(desc.InvertedIndexColumns))
	for _, ic := range desc.InvertedIndexColumns {
		invSet[ic] = true
	}
	cols := make([]PostgresColumnDescriptor, 0, len(colOrder))
	for _, name := range colOrder {
		c := colMap[name]
		if pkSet[name] {
			c.IsPrimaryKey = true
		}
		if partSet[name] {
			c.IsPartitionKey = true
		}
		if invSet[name] {
			c.InvertedIndex = true
		}
		if strings.HasPrefix(c.PostgresType, "VECTOR(") && strings.HasSuffix(c.PostgresType, ")") {
			dimStr := strings.TrimSuffix(strings.TrimPrefix(c.PostgresType, "VECTOR("), ")")
			if dim64, err := strconv.ParseInt(strings.TrimSpace(dimStr), 10, 32); err == nil && dim64 > 0 && dim64 <= math.MaxInt32 {
				desc.EmbeddingDimension = int32(dim64)
			}
		}
		cols = append(cols, c)
	}
	desc.Columns = cols
	return desc, nil
}

// parseHCLForeignKeyRefColumns parses a bracketed Atlas HCL ref_columns expression like
// "[table.parent.column.id]" or "[table.parent.column.c1, table.parent.column.c2]" into
// the referenced table name and list of referenced columns.
func parseHCLForeignKeyRefColumns(raw string) (string, []string, error) {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return "", nil, fmt.Errorf("expected bracketed ref_columns list, got %q", raw)
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return "", nil, fmt.Errorf("empty ref_columns list in %q", raw)
	}
	parts := strings.Split(inner, ",")
	var refTable string
	var refCols []string
	for _, p := range parts {
		item := strings.TrimSpace(p)
		item = unquoteHCLString(item)
		subParts := strings.Split(item, ".")
		if len(subParts) != 4 || subParts[0] != "table" || subParts[2] != "column" {
			return "", nil, fmt.Errorf("invalid foreign key ref_column format %q (expected table.<ref_table>.column.<ref_col>)", p)
		}
		tbl := unquoteHCLString(subParts[1])
		col := unquoteHCLString(subParts[3])
		if !isValidSQLIdentifier(tbl) || !isValidSQLIdentifier(col) {
			return "", nil, fmt.Errorf("invalid identifier in ref_column %q", p)
		}
		if refTable == "" {
			refTable = tbl
		} else if refTable != tbl {
			return "", nil, fmt.Errorf("foreign key references multiple tables: %q and %q", refTable, tbl)
		}
		refCols = append(refCols, col)
	}
	return refTable, refCols, nil
}

func applyPostgresAtlasSubBlock(
	desc *PostgresTableDescriptor,
	blockHeader, blockBody string,
	colOrder *[]string,
	colMap map[string]PostgresColumnDescriptor,
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
		kv := make(map[string]string)
		hasIdentity := false
		pos := 0
		for pos < len(blockBody) {
			openRel := indexUnquotedHCLBrace(blockBody[pos:])
			if openRel < 0 {
				restKV, err := parseHCLKeyValueAssignments(blockBody[pos:])
				if err != nil {
					return fmt.Errorf("column %q: %w", colName, err)
				}
				for k, v := range restKV {
					kv[k] = v
				}
				break
			}
			openAbs := pos + openRel
			headerStart := strings.LastIndexByte(blockBody[pos:openAbs], '\n')
			var subHdr string
			if headerStart >= 0 {
				preKV, err := parseHCLKeyValueAssignments(blockBody[pos : pos+headerStart])
				if err != nil {
					return fmt.Errorf("column %q: %w", colName, err)
				}
				for k, v := range preKV {
					kv[k] = v
				}
				subHdr = strings.TrimSpace(blockBody[pos+headerStart+1 : openAbs])
			} else {
				subHdr = strings.TrimSpace(blockBody[pos:openAbs])
			}
			closeAbs, err := findMatchingHCLBrace(blockBody, openAbs)
			if err != nil {
				return fmt.Errorf("column %q: %w", colName, err)
			}
			subBody := blockBody[openAbs+1 : closeAbs]
			pos = closeAbs + 1

			if subHdr == "identity" {
				subKV, err := parseHCLKeyValueAssignments(subBody)
				if err != nil {
					return fmt.Errorf("column %q identity block: %w", colName, err)
				}
				if genRaw, hasGen := subKV["generated"]; hasGen {
					genMode := strings.ToUpper(strings.TrimSpace(unquoteHCLString(genRaw)))
					if genMode != "ALWAYS" {
						return fmt.Errorf("column %q identity block unsupported generated mode %q (only ALWAYS is supported)", colName, genRaw)
					}
				}
				hasIdentity = true
			} else {
				return fmt.Errorf("column %q unsupported sub-block %q", colName, subHdr)
			}
		}
		rawType, ok := kv["type"]
		if !ok || rawType == "" {
			return fmt.Errorf("column %q missing type attribute", colName)
		}
		pgType := hclAttrTypeToPostgresType(rawType)
		if IsForbiddenPostgresColumnType(pgType) {
			return fmt.Errorf("column %q uses forbidden non-relational type %q", colName, pgType)
		}
		nullable := false
		if nullStr, hasNull := kv["null"]; hasNull {
			parsedNull, bErr := strconv.ParseBool(unquoteHCLString(nullStr))
			if bErr != nil {
				return fmt.Errorf("column %q invalid null attribute %q", colName, nullStr)
			}
			nullable = parsedNull
		}
		defVal := ""
		hasDefVal := false
		srvDef := ""
		if rawDef, hasDef := kv["default"]; hasDef {
			trimmedDef := strings.TrimSpace(rawDef)
			if strings.HasPrefix(trimmedDef, "sql(") && strings.HasSuffix(trimmedDef, ")") {
				inner := unquoteHCLString(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmedDef, "sql("), ")")))
				parsedDefVal, parsedHasDefVal, parsedSrvDef, ok := parsePostgresHCLSQLDefault(inner, pgType)
				if !ok {
					return fmt.Errorf("column %q invalid sql() default %q", colName, rawDef)
				}
				defVal = parsedDefVal
				hasDefVal = parsedHasDefVal
				srvDef = parsedSrvDef
			} else {
				defVal = unquoteHCLString(rawDef)
				hasDefVal = true
			}
		}
		if hasIdentity {
			if !isPostgresIdentityEligibleSQLType(pgType) {
				return fmt.Errorf("column %q identity requires SMALLINT, INTEGER, or BIGINT type (got %q)", colName, pgType)
			}
			if nullable {
				return fmt.Errorf("column %q identity column cannot be nullable", colName)
			}
			if hasDefVal || defVal != "" || srvDef != "" {
				return fmt.Errorf("column %q identity column cannot specify default", colName)
			}
		}
		comment := ""
		if rawComment, hasComment := kv["comment"]; hasComment {
			comment = unquoteHCLString(rawComment)
		}
		*colOrder = append(*colOrder, colName)
		colMap[colName] = PostgresColumnDescriptor{
			ColumnName:      colName,
			ProtoFieldName:  colName,
			SQLType:         pgType,
			PostgresType:    pgType,
			HCLType:         strings.ToLower(pgType),
			Nullable:        nullable,
			Identity:        hasIdentity,
			DefaultValue:    defVal,
			HasDefaultValue: hasDefVal,
			ServerDefault:   srvDef,
			Comment:         comment,
		}
		return nil

	case header == "primary_key":
		kv, err := parseHCLKeyValueAssignments(blockBody)
		if err != nil {
			return fmt.Errorf("primary_key: %w", err)
		}
		colsRaw, ok := kv["columns"]
		if !ok {
			return fmt.Errorf("primary_key missing columns attribute")
		}
		pks, err := parseHCLColumnRefList(colsRaw)
		if err != nil {
			return fmt.Errorf("primary_key: %w", err)
		}
		desc.PrimaryKeyColumns = pks
		return nil

	case header == "partition" || header == "partition_by_range" || header == "partition_by_list":
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

	case strings.HasPrefix(header, "index "):
		idxName := unquoteHCLString(strings.TrimSpace(strings.TrimPrefix(header, "index")))
		if !isValidSQLIdentifier(idxName) || len(idxName) > maxPostgresIdentifierBytes {
			return fmt.Errorf("invalid index name %q", idxName)
		}
		var cols []string
		var descCols []string
		var exprs []string
		var parts []PostgresIndexPartDescriptor
		kv := make(map[string]string)

		pos := 0
		for pos < len(blockBody) {
			openRel := indexUnquotedHCLBrace(blockBody[pos:])
			if openRel < 0 {
				restKV, err := parseHCLKeyValueAssignments(blockBody[pos:])
				if err != nil {
					return fmt.Errorf("index %q: %w", idxName, err)
				}
				for k, v := range restKV {
					kv[k] = v
				}
				break
			}
			openAbs := pos + openRel
			headerStart := strings.LastIndexByte(blockBody[pos:openAbs], '\n')
			var subHdr string
			if headerStart >= 0 {
				preKV, err := parseHCLKeyValueAssignments(blockBody[pos : pos+headerStart])
				if err != nil {
					return fmt.Errorf("index %q: %w", idxName, err)
				}
				for k, v := range preKV {
					kv[k] = v
				}
				subHdr = strings.TrimSpace(blockBody[pos+headerStart+1 : openAbs])
			} else {
				subHdr = strings.TrimSpace(blockBody[pos:openAbs])
			}
			closeAbs, err := findMatchingHCLBrace(blockBody, openAbs)
			if err != nil {
				return fmt.Errorf("index %q: %w", idxName, err)
			}
			subBody := blockBody[openAbs+1 : closeAbs]
			pos = closeAbs + 1

			if subHdr == "on" {
				subKV, err := parseHCLKeyValueAssignments(subBody)
				if err != nil {
					return fmt.Errorf("index %q on block: %w", idxName, err)
				}
				rawExpr, hasExpr := subKV["expr"]
				rawCol, hasCol := subKV["column"]
				if hasExpr && hasCol {
					return fmt.Errorf("index %q on block cannot specify both column and expr", idxName)
				}
				if !hasExpr && !hasCol {
					return fmt.Errorf("index %q on block missing column or expr attribute", idxName)
				}
				var part PostgresIndexPartDescriptor
				if hasExpr {
					exprVal := strings.TrimSpace(unquoteHCLString(rawExpr))
					if !isValidPostgresCheckExpr(exprVal) {
						return fmt.Errorf("index %q on block invalid expr %q", idxName, exprVal)
					}
					part.Expr = exprVal
					exprs = append(exprs, exprVal)
				} else {
					rawCol = unquoteHCLString(rawCol)
					colRef := strings.TrimPrefix(rawCol, "column.")
					if colRef == "" || !isValidSQLIdentifier(colRef) || len(colRef) > maxPostgresIdentifierBytes {
						return fmt.Errorf("index %q on block references invalid column %q", idxName, colRef)
					}
					part.Column = colRef
					cols = append(cols, colRef)
				}
				if dRaw, ok := subKV["desc"]; ok {
					isDesc, err := strconv.ParseBool(unquoteHCLString(dRaw))
					if err != nil {
						return fmt.Errorf("index %q on block invalid desc value %q: %w", idxName, dRaw, err)
					}
					part.Desc = isDesc
					if isDesc && part.Column != "" {
						descCols = append(descCols, part.Column)
					}
				}
				rawOps, hasOps := subKV["ops"]
				if !hasOps {
					rawOps, hasOps = subKV["opclass"]
				}
				if hasOps {
					opVal := strings.TrimSpace(unquoteHCLString(rawOps))
					if opVal == "" || !isValidSQLIdentifier(opVal) || len(opVal) > maxPostgresIdentifierBytes {
						return fmt.Errorf("index %q on block invalid ops value %q", idxName, rawOps)
					}
					part.Opclass = opVal
				}
				parts = append(parts, part)
			} else {
				return fmt.Errorf("index %q unsupported sub-block %q", idxName, subHdr)
			}
		}

		if colsRaw, ok := kv["columns"]; ok && len(cols) == 0 && len(parts) == 0 {
			var err error
			cols, err = parseHCLColumnRefList(colsRaw)
			if err != nil {
				return fmt.Errorf("index %q: %w", idxName, err)
			}
		}
		if len(parts) == 0 && len(cols) == 0 && len(exprs) == 0 {
			return fmt.Errorf("index %q must specify columns, expressions, or on blocks", idxName)
		}
		whereExpr := ""
		if wRaw, hasWhere := kv["where"]; hasWhere {
			whereExpr = unquoteHCLString(wRaw)
			if !isValidPostgresCheckExpr(whereExpr) {
				return fmt.Errorf("index %q: invalid where predicate %q", idxName, whereExpr)
			}
		}
		unique := false
		if uRaw, hasUnique := kv["unique"]; hasUnique {
			unique, _ = strconv.ParseBool(unquoteHCLString(uRaw))
		}
		using := strings.ToUpper(unquoteHCLString(kv["type"]))
		if using == "" {
			using = strings.ToUpper(unquoteHCLString(kv["using"]))
		}
		if using == "" {
			using = "BTREE"
		}
		if !isValidPostgresIndexMethod(using) {
			return fmt.Errorf("index %q: invalid index method %q", idxName, using)
		}
		ops := strings.TrimSpace(unquoteHCLString(kv["ops"]))
		if ops != "" && (!isValidSQLIdentifier(ops) || len(ops) > maxPostgresIdentifierBytes) {
			return fmt.Errorf("index %q: invalid ops %q", idxName, ops)
		}
		if using == "BTREE" {
			if isPostgresBTreeIncompatibleOpclass(ops) {
				return fmt.Errorf("index %q: operator class %q is incompatible with BTREE (specify a compatible type/using method)", idxName, ops)
			}
			for _, p := range parts {
				if isPostgresBTreeIncompatibleOpclass(p.Opclass) {
					return fmt.Errorf("index %q: operator class %q is incompatible with BTREE (specify a compatible type/using method)", idxName, p.Opclass)
				}
			}
		}
		desc.Indexes = append(desc.Indexes, PostgresIndexDescriptor{
			Name:        idxName,
			Columns:     cols,
			Unique:      unique,
			Using:       using,
			Ops:         ops,
			DescColumns: descCols,
			Where:       whereExpr,
			Expressions: exprs,
			Parts:       parts,
		})
		if using == "GIN" && !unique && len(parts) == 0 && len(exprs) == 0 && len(cols) == 1 && whereExpr == "" && ops == "" {
			if idxName == postgresSyntheticNgramIndexName(desc.TableName, cols[0]) {
				desc.NgramBfColumns = dedupDorisColumnList(append(desc.NgramBfColumns, cols...))
			} else if idxName == postgresSyntheticGinIndexName(desc.TableName, cols[0]) {
				desc.InvertedIndexColumns = dedupDorisColumnList(append(desc.InvertedIndexColumns, cols...))
			}
		}
		return nil

	case strings.HasPrefix(header, "foreign_key "):
		fkName := unquoteHCLString(strings.TrimSpace(strings.TrimPrefix(header, "foreign_key")))
		if !isValidSQLIdentifier(fkName) || len(fkName) > maxPostgresIdentifierBytes {
			return fmt.Errorf("invalid foreign_key name %q", fkName)
		}
		kv, err := parseHCLKeyValueAssignments(blockBody)
		if err != nil {
			return fmt.Errorf("foreign_key %q: %w", fkName, err)
		}
		var cols []string
		if colsRaw, ok := kv["columns"]; ok {
			cols, err = parseHCLColumnRefList(colsRaw)
			if err != nil {
				return fmt.Errorf("foreign_key %q: %w", fkName, err)
			}
		}
		var refTable, refCol string
		var refCols []string
		if refRaw, ok := kv["ref_columns"]; ok {
			var err error
			refTable, refCols, err = parseHCLForeignKeyRefColumns(refRaw)
			if err != nil {
				return fmt.Errorf("foreign_key %q: %w", fkName, err)
			}
			if len(refTable) > maxPostgresIdentifierBytes {
				return fmt.Errorf("foreign_key %q ref_table %q exceeds identifier limit", fkName, refTable)
			}
			for _, rc := range refCols {
				if len(rc) > maxPostgresIdentifierBytes {
					return fmt.Errorf("foreign_key %q ref_column %q exceeds identifier limit", fkName, rc)
				}
			}
			if len(refCols) > 0 {
				refCol = refCols[0]
			}
		}
		if len(cols) != len(refCols) {
			return fmt.Errorf("foreign_key %q: columns count (%d) does not match ref_columns count (%d)",
				fkName, len(cols), len(refCols))
		}
		onDelete := strings.TrimSpace(unquoteHCLString(kv["on_delete"]))
		if _, _, ok := isValidForeignKeyAction(onDelete); !ok {
			return fmt.Errorf("foreign_key %q invalid on_delete %q", fkName, onDelete)
		}
		onUpdate := strings.TrimSpace(unquoteHCLString(kv["on_update"]))
		if _, _, ok := isValidForeignKeyAction(onUpdate); !ok {
			return fmt.Errorf("foreign_key %q invalid on_update %q", fkName, onUpdate)
		}
		desc.ForeignKeys = append(desc.ForeignKeys, PostgresForeignKeyDescriptor{
			Name:       fkName,
			Columns:    cols,
			RefTable:   refTable,
			RefColumn:  refCol,
			RefColumns: refCols,
			OnDelete:   onDelete,
			OnUpdate:   onUpdate,
		})
		return nil

	case strings.HasPrefix(header, "check "):
		chkName := unquoteHCLString(strings.TrimSpace(strings.TrimPrefix(header, "check")))
		if !isValidSQLIdentifier(chkName) || len(chkName) > maxPostgresIdentifierBytes {
			return fmt.Errorf("invalid check constraint name %q", chkName)
		}
		kv, err := parseHCLKeyValueAssignments(blockBody)
		if err != nil {
			return fmt.Errorf("check %q: %w", chkName, err)
		}
		rawExpr, ok := kv["expr"]
		if !ok || rawExpr == "" {
			return fmt.Errorf("check %q missing expr attribute", chkName)
		}
		expr := unquoteHCLString(rawExpr)
		if !isValidPostgresCheckExpr(expr) {
			return fmt.Errorf("check %q invalid expr %q", chkName, expr)
		}
		desc.Checks = append(desc.Checks, PostgresCheckDescriptor{
			Name: chkName,
			Expr: expr,
		})
		return nil

	default:
		return fmt.Errorf("unsupported PostgreSQL HCL block %q", blockHeader)
	}
}

// PostgresLintRule identifies a compile-time PostgreSQL schema evolution safety rule.
type PostgresLintRule string

const (
	// PG001_IMMUTABLE_PK blocks mutations to PrimaryKeyColumns on an existing table.
	PG001_IMMUTABLE_PK PostgresLintRule = "PG001_IMMUTABLE_PK"
	// PG002_IMMUTABLE_PARTITION blocks mutations to PartitionByColumns on an existing table.
	PG002_IMMUTABLE_PARTITION PostgresLintRule = "PG002_IMMUTABLE_PARTITION"
	// PG004_NO_COLUMN_DROP blocks dropping existing columns from an existing table.
	PG004_NO_COLUMN_DROP PostgresLintRule = "PG004_NO_COLUMN_DROP"
	// PG005_NON_NULL_SCALAR_DEFAULT requires an explicit DefaultValue on newly added NOT NULL scalar columns.
	PG005_NON_NULL_SCALAR_DEFAULT PostgresLintRule = "PG005_NON_NULL_SCALAR_DEFAULT"
	// PG006_SAFE_TYPE_EVOLUTION allows only safe widening type evolutions.
	PG006_SAFE_TYPE_EVOLUTION PostgresLintRule = "PG006_SAFE_TYPE_EVOLUTION"
	// PG007_FORBIDDEN_TYPE forbids JSON/JSONB/VARIANT column types in relational PostgreSQL tables.
	PG007_FORBIDDEN_TYPE PostgresLintRule = "PG007_FORBIDDEN_TYPE"
)

// PostgresEvolutionViolation describes a single PostgreSQL schema evolution rule violation.
type PostgresEvolutionViolation struct {
	Rule       PostgresLintRule
	TableName  string
	ColumnName string
	Message    string
}

// String formats the violation as a deterministic human-readable diagnostic.
func (v PostgresEvolutionViolation) String() string {
	if v.ColumnName != "" {
		return fmt.Sprintf("[%s] table %s column %s: %s", v.Rule, v.TableName, v.ColumnName, v.Message)
	}
	return fmt.Sprintf("[%s] table %s: %s", v.Rule, v.TableName, v.Message)
}

// Error implements the error interface.
func (v PostgresEvolutionViolation) Error() string {
	return v.String()
}

func effectivePostgresPrimaryKeys(t PostgresTableDescriptor) []string {
	var pks []string
	if len(t.PrimaryKeyColumns) > 0 {
		pks = dedupDorisColumnList(t.PrimaryKeyColumns)
	} else {
		for _, c := range t.Columns {
			if c.IsPrimaryKey {
				pks = append(pks, c.ColumnName)
			}
		}
		pks = dedupDorisColumnList(pks)
	}
	partCols := effectivePostgresPartitionCols(t)
	if len(pks) > 0 && len(partCols) > 0 {
		pkMap := make(map[string]bool, len(pks))
		for _, k := range pks {
			pkMap[k] = true
		}
		for _, pc := range partCols {
			if !pkMap[pc] {
				pks = append(pks, pc)
				pkMap[pc] = true
			}
		}
	}
	return pks
}

func effectivePostgresPartitionCols(t PostgresTableDescriptor) []string {
	if len(t.PartitionByColumns) > 0 {
		return dedupDorisColumnList(t.PartitionByColumns)
	}
	var cols []string
	for _, c := range t.Columns {
		if c.IsPartitionKey {
			cols = append(cols, c.ColumnName)
		}
	}
	return dedupDorisColumnList(cols)
}

// IsSafePostgresTypeEvolution reports whether evolving a PostgreSQL column from
// beforeType to afterType is a no-op or a safe widening type evolution.
func IsSafePostgresTypeEvolution(beforeType, afterType string) bool {
	bt := strings.ToUpper(strings.TrimSpace(beforeType))
	at := strings.ToUpper(strings.TrimSpace(afterType))
	if bt == at {
		return true
	}
	if bt == "" || at == "" {
		return false
	}
	if strings.HasSuffix(bt, "[]") || strings.HasSuffix(at, "[]") {
		if strings.HasSuffix(bt, "[]") && strings.HasSuffix(at, "[]") {
			return IsSafePostgresTypeEvolution(strings.TrimSuffix(bt, "[]"), strings.TrimSuffix(at, "[]"))
		}
		return false
	}
	if m, okB := parseDorisVarcharLength(bt); okB {
		if n, okA := parseDorisVarcharLength(at); okA {
			return n >= m
		}
		return at == "TEXT"
	}
	if p1, s1, okB := parsePostgresNumericParams(bt); okB {
		if p2, s2, okA := parsePostgresNumericParams(at); okA {
			return p2 >= p1 && s1 == s2
		}
		return false
	}
	switch bt {
	case "SMALLINT":
		if p, s, ok := parsePostgresNumericParams(at); ok {
			return p >= 5 && s == 0
		}
		return at == "INTEGER" || at == "INT" || at == "BIGINT"
	case "INTEGER", "INT":
		if p, s, ok := parsePostgresNumericParams(at); ok {
			return p >= 10 && s == 0
		}
		return at == "BIGINT"
	case "BIGINT":
		if p, s, ok := parsePostgresNumericParams(at); ok {
			return p >= 20 && s == 0
		}
		return false
	case "REAL", "FLOAT":
		return at == "DOUBLE PRECISION" || at == "DOUBLE"
	case "DATE":
		return at == "TIMESTAMPTZ"
	default:
		return false
	}
}

func parsePostgresNumericParams(upperType string) (int, int, bool) {
	if p, s, ok := parseDorisDecimalParams(upperType); ok {
		return p, s, true
	}
	if !strings.HasPrefix(upperType, "NUMERIC(") || !strings.HasSuffix(upperType, ")") {
		return 0, 0, false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(upperType, "NUMERIC("), ")")
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

// LintPostgresTableEvolution validates evolving an existing PostgreSQL table descriptor
// from before to after and returns all detected violations in deterministic rule order.
func LintPostgresTableEvolution(before, after PostgresTableDescriptor) []PostgresEvolutionViolation {
	tableName := strings.TrimSpace(after.TableName)
	if tableName == "" {
		tableName = strings.TrimSpace(before.TableName)
	}
	var violations []PostgresEvolutionViolation

	beforePKs := effectivePostgresPrimaryKeys(before)
	afterPKs := effectivePostgresPrimaryKeys(after)
	if !dorisStringSlicesEqual(beforePKs, afterPKs) {
		violations = append(violations, PostgresEvolutionViolation{
			Rule:      PG001_IMMUTABLE_PK,
			TableName: tableName,
			Message:   fmt.Sprintf("PRIMARY KEY columns cannot be mutated on existing table (before=%v, after=%v)", beforePKs, afterPKs),
		})
	}

	beforeParts := effectivePostgresPartitionCols(before)
	afterParts := effectivePostgresPartitionCols(after)
	if !dorisStringSlicesEqual(beforeParts, afterParts) {
		violations = append(violations, PostgresEvolutionViolation{
			Rule:      PG002_IMMUTABLE_PARTITION,
			TableName: tableName,
			Message:   fmt.Sprintf("PARTITION BY columns cannot be mutated on existing table (before=%v, after=%v)", beforeParts, afterParts),
		})
	}

	beforeColByName := make(map[string]PostgresColumnDescriptor, len(before.Columns))
	for _, c := range before.Columns {
		beforeColByName[c.ColumnName] = c
	}
	afterColByName := make(map[string]PostgresColumnDescriptor, len(after.Columns))
	for _, c := range after.Columns {
		afterColByName[c.ColumnName] = c
	}

	for _, bc := range before.Columns {
		if _, ok := afterColByName[bc.ColumnName]; !ok {
			violations = append(violations, PostgresEvolutionViolation{
				Rule:       PG004_NO_COLUMN_DROP,
				TableName:  tableName,
				ColumnName: bc.ColumnName,
				Message:    fmt.Sprintf("dropping existing column %q is forbidden", bc.ColumnName),
			})
		}
	}

	for _, ac := range after.Columns {
		if _, existed := beforeColByName[ac.ColumnName]; existed {
			continue
		}
		acType := effectiveColPostgresType(ac)
		if !ac.Nullable && !ac.Identity && !strings.HasSuffix(strings.ToUpper(acType), "[]") &&
			!IsForbiddenPostgresColumnType(acType) && !ac.HasDefaultValue && strings.TrimSpace(ac.DefaultValue) == "" && strings.TrimSpace(ac.ServerDefault) == "" {
			violations = append(violations, PostgresEvolutionViolation{
				Rule:       PG005_NON_NULL_SCALAR_DEFAULT,
				TableName:  tableName,
				ColumnName: ac.ColumnName,
				Message:    fmt.Sprintf("newly added NOT NULL scalar column %q (%s) requires an explicit DefaultValue", ac.ColumnName, acType),
			})
		}
	}

	for _, ac := range after.Columns {
		bc, existed := beforeColByName[ac.ColumnName]
		if !existed {
			continue
		}
		bcType := effectiveColPostgresType(bc)
		acType := effectiveColPostgresType(ac)
		if !IsSafePostgresTypeEvolution(bcType, acType) {
			violations = append(violations, PostgresEvolutionViolation{
				Rule:       PG006_SAFE_TYPE_EVOLUTION,
				TableName:  tableName,
				ColumnName: ac.ColumnName,
				Message:    fmt.Sprintf("unsafe type evolution on column %q from %q to %q", ac.ColumnName, bcType, acType),
			})
		}
	}

	seenForbidden := make(map[string]bool)
	for _, bc := range before.Columns {
		bcType := effectiveColPostgresType(bc)
		if IsForbiddenPostgresColumnType(bcType) {
			seenForbidden[bc.ColumnName] = true
			violations = append(violations, PostgresEvolutionViolation{
				Rule:       PG007_FORBIDDEN_TYPE,
				TableName:  tableName,
				ColumnName: bc.ColumnName,
				Message:    fmt.Sprintf("column %q uses forbidden non-relational type %q", bc.ColumnName, bcType),
			})
		}
	}
	for _, ac := range after.Columns {
		acType := effectiveColPostgresType(ac)
		if !seenForbidden[ac.ColumnName] && IsForbiddenPostgresColumnType(acType) {
			violations = append(violations, PostgresEvolutionViolation{
				Rule:       PG007_FORBIDDEN_TYPE,
				TableName:  tableName,
				ColumnName: ac.ColumnName,
				Message:    fmt.Sprintf("column %q uses forbidden non-relational type %q", ac.ColumnName, acType),
			})
		}
	}

	return violations
}

// ValidatePostgresTableEvolution returns an error joining all violations if evolving
// before into after violates any PostgreSQL schema evolution rule, or nil if safe.
func ValidatePostgresTableEvolution(before, after PostgresTableDescriptor) error {
	violations := LintPostgresTableEvolution(before, after)
	if len(violations) == 0 {
		return nil
	}
	errs := make([]error, len(violations))
	for i, v := range violations {
		errs[i] = v
	}
	return errors.Join(errs...)
}

func postgresScopeTableIdentity(t PostgresTableDescriptor) string {
	tableName := strings.TrimSpace(t.TableName)
	if tableName == "" {
		return ""
	}
	scope := strings.TrimSpace(t.DatabaseScope)
	if scope == "" || scope == "UNSPECIFIED" {
		scope = "PUBLIC"
	}
	return scope + "\x00" + tableName
}

// LintPostgresSchemaEvolution validates evolving a slice of before table descriptors
// into after table descriptors.
func LintPostgresSchemaEvolution(before, after []PostgresTableDescriptor) []PostgresEvolutionViolation {
	beforeByKey := make(map[string]PostgresTableDescriptor, len(before))
	for _, t := range before {
		if key := postgresScopeTableIdentity(t); key != "" {
			beforeByKey[key] = t
		}
	}
	var violations []PostgresEvolutionViolation
	seen := make(map[string]bool, len(after))
	for _, afterTbl := range after {
		key := postgresScopeTableIdentity(afterTbl)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if beforeTbl, ok := beforeByKey[key]; ok {
			violations = append(violations, LintPostgresTableEvolution(beforeTbl, afterTbl)...)
		} else {
			for _, ac := range afterTbl.Columns {
				acType := effectiveColPostgresType(ac)
				if IsForbiddenPostgresColumnType(acType) {
					violations = append(violations, PostgresEvolutionViolation{
						Rule:       PG007_FORBIDDEN_TYPE,
						TableName:  afterTbl.TableName,
						ColumnName: ac.ColumnName,
						Message:    fmt.Sprintf("column %q uses forbidden non-relational type %q", ac.ColumnName, acType),
					})
				}
			}
		}
	}
	return violations
}

// ValidatePostgresSchemaEvolution returns an error joining all violations across
// the schema evolution from before to after, or nil if safe.
func ValidatePostgresSchemaEvolution(before, after []PostgresTableDescriptor) error {
	violations := LintPostgresSchemaEvolution(before, after)
	if len(violations) == 0 {
		return nil
	}
	errs := make([]error, len(violations))
	for i, v := range violations {
		errs[i] = v
	}
	return errors.Join(errs...)
}

// BuildPostgresAlterTableSQL validates evolving this table descriptor (before) into after
// and renders deterministic PostgreSQL ALTER TABLE statements.
func (t PostgresTableDescriptor) BuildPostgresAlterTableSQL(after PostgresTableDescriptor, tenantSlug string) ([]string, error) {
	return BuildPostgresAlterTableSQL(t, after, tenantSlug)
}

// BuildPostgresAlterTableSQL validates evolving before into after and returns the ordered
// PostgreSQL ALTER TABLE statements.
func BuildPostgresAlterTableSQL(before, after PostgresTableDescriptor, tenantSlug string) ([]string, error) {
	if err := ValidatePostgresTableEvolution(before, after); err != nil {
		return nil, err
	}
	if !validatePostgresDescriptor(before) || !validatePostgresDescriptor(after) {
		return nil, fmt.Errorf("invalid PostgresTableDescriptor identifiers in BuildPostgresAlterTableSQL")
	}
	if postgresScopeTableIdentity(before) != postgresScopeTableIdentity(after) {
		return nil, fmt.Errorf("cannot generate PostgreSQL ALTER TABLE across different tables (%q vs %q)", before.TableName, after.TableName)
	}
	schemaName, ok := resolvePostgresSchemaName(after.DatabaseScope, tenantSlug)
	if !ok {
		return nil, fmt.Errorf("invalid tenantSlug %q in BuildPostgresAlterTableSQL", tenantSlug)
	}
	fullTableName := schemaName + "." + after.TableName
	beforeColByName := make(map[string]PostgresColumnDescriptor, len(before.Columns))
	for _, bc := range before.Columns {
		beforeColByName[bc.ColumnName] = bc
	}
	var stmts []string
	for _, ac := range after.Columns {
		bc, existed := beforeColByName[ac.ColumnName]
		if !existed {
			continue
		}
		beforeType := strings.ToUpper(effectiveColPostgresType(bc))
		afterType := strings.ToUpper(effectiveColPostgresType(ac))
		if beforeType != afterType {
			stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s;", fullTableName, ac.ColumnName, afterType))
		}
		if bc.Nullable != ac.Nullable {
			if ac.Nullable {
				stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL;", fullTableName, ac.ColumnName))
			} else {
				stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL;", fullTableName, ac.ColumnName))
			}
		}
		if bc.Identity && !ac.Identity {
			stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP IDENTITY IF EXISTS;", fullTableName, ac.ColumnName))
		}
		beforeDef, beforeHasDef := effectivePostgresColumnDefaultClause(beforeType, bc)
		afterDef, afterHasDef := effectivePostgresColumnDefaultClause(afterType, ac)
		if beforeHasDef != afterHasDef || beforeDef != afterDef {
			if afterHasDef && !ac.Identity {
				stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s;", fullTableName, ac.ColumnName, afterDef))
			} else if beforeHasDef {
				stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT;", fullTableName, ac.ColumnName))
			}
		}
		if !bc.Identity && ac.Identity {
			stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s ADD GENERATED ALWAYS AS IDENTITY;", fullTableName, ac.ColumnName))
			stmts = append(stmts, fmt.Sprintf(
				"SELECT setval(pg_get_serial_sequence('%s', '%s'), GREATEST(COALESCE((SELECT MAX(%s) FROM %s), 0), 0) + 1, false);",
				fullTableName, ac.ColumnName, ac.ColumnName, fullTableName))
		}
	}
	for _, ac := range after.Columns {
		if _, existed := beforeColByName[ac.ColumnName]; existed {
			continue
		}
		afterType := strings.ToUpper(effectiveColPostgresType(ac))
		clause := fmt.Sprintf("%s %s", ac.ColumnName, afterType)
		if !ac.Nullable {
			clause += " NOT NULL"
		}
		if ac.Identity {
			clause += " GENERATED ALWAYS AS IDENTITY"
		} else if defClause, hasDef := effectivePostgresColumnDefaultClause(afterType, ac); hasDef {
			clause += " DEFAULT " + defClause
		}
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;", fullTableName, clause))
	}
	beforeIdxByName := make(map[string]PostgresIndexDescriptor)
	for _, c := range before.Columns {
		if c.InvertedIndex {
			idxName := postgresSyntheticGinIndexName(before.TableName, c.ColumnName)
			beforeIdxByName[idxName] = PostgresIndexDescriptor{
				Name:    idxName,
				Columns: []string{c.ColumnName},
				Using:   "GIN",
			}
		}
	}
	for _, ic := range before.InvertedIndexColumns {
		if ic != "" {
			idxName := postgresSyntheticGinIndexName(before.TableName, ic)
			beforeIdxByName[idxName] = PostgresIndexDescriptor{
				Name:    idxName,
				Columns: []string{ic},
				Using:   "GIN",
			}
		}
	}
	for _, ngCol := range dedupDorisColumnList(before.NgramBfColumns) {
		idxName := postgresSyntheticNgramIndexName(before.TableName, ngCol)
		beforeIdxByName[idxName] = PostgresIndexDescriptor{
			Name:    idxName,
			Columns: []string{ngCol},
			Using:   "GIN",
		}
	}
	for _, idx := range before.Indexes {
		if idx.Name != "" {
			beforeIdxByName[idx.Name] = idx
		}
	}
	for _, idx := range after.Indexes {
		if idx.Name == "" || (len(idx.Parts) == 0 && len(idx.Columns) == 0 && len(idx.Expressions) == 0) {
			continue
		}
		if prev, exists := beforeIdxByName[idx.Name]; exists {
			if postgresIndexesEffectivelyEqual(prev, idx) {
				continue
			}
			stmts = append(stmts, fmt.Sprintf("DROP INDEX IF EXISTS %s.%s;", schemaName, idx.Name))
		}
		beforeIdxByName[idx.Name] = idx
		uniqueKw := ""
		if idx.Unique {
			uniqueKw = "UNIQUE "
		}
		idxParts := formatPostgresIndexPartsSQL(idx, false)
		using := strings.ToLower(strings.TrimSpace(idx.Using))
		whereClause := ""
		if idx.Where != "" {
			whereClause = " WHERE " + idx.Where
		}
		if using == "" || using == "btree" {
			if isPostgresBTreeIncompatibleOpclass(idx.Ops) {
				return nil, fmt.Errorf("index %q operator class %q is incompatible with BTREE (specify a compatible USING method)", idx.Name, idx.Ops)
			}
			for _, p := range idx.Parts {
				if isPostgresBTreeIncompatibleOpclass(p.Opclass) {
					return nil, fmt.Errorf("index %q operator class %q is incompatible with BTREE (specify a compatible USING method)", idx.Name, p.Opclass)
				}
			}
			stmts = append(stmts, fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)%s;", uniqueKw, idx.Name, fullTableName, strings.Join(idxParts, ", "), whereClause))
		} else if idx.Ops != "" && len(idxParts) == 1 && (len(idx.Parts) != 1 || idx.Parts[0].Opclass == "") {
			stmts = append(stmts, fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s USING %s (%s %s)%s;", uniqueKw, idx.Name, fullTableName, using, idxParts[0], idx.Ops, whereClause))
		} else {
			stmts = append(stmts, fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s USING %s (%s)%s;", uniqueKw, idx.Name, fullTableName, using, strings.Join(idxParts, ", "), whereClause))
		}
	}
	beforeFK := make(map[string]bool, len(before.ForeignKeys))
	for _, fk := range before.ForeignKeys {
		if fk.Name != "" {
			beforeFK[fk.Name] = true
		}
	}
	for _, fk := range after.ForeignKeys {
		if fk.Name == "" || beforeFK[fk.Name] || fk.RefTable == "" || len(fk.Columns) == 0 {
			continue
		}
		beforeFK[fk.Name] = true
		refCols := fk.EffectiveRefColumns()
		clause := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s.%s (%s)",
			fullTableName, fk.Name, strings.Join(fk.Columns, ", "), schemaName, fk.RefTable, strings.Join(refCols, ", "))
		if _, delSQL, ok := isValidForeignKeyAction(fk.OnDelete); ok && delSQL != "" {
			clause += " ON DELETE " + delSQL
		}
		if _, updSQL, ok := isValidForeignKeyAction(fk.OnUpdate); ok && updSQL != "" {
			clause += " ON UPDATE " + updSQL
		}
		stmts = append(stmts, clause+";")
	}
	return stmts, nil
}

// BuildPostgresSchemaMigrationSQL validates evolving a slice of before table descriptors
// into after table descriptors and returns the ordered PostgreSQL DDL statements
// (ALTER TABLE for evolved existing tables and CREATE TABLE IF NOT EXISTS for new tables).
func BuildPostgresSchemaMigrationSQL(before, after []PostgresTableDescriptor, tenantSlug string) ([]string, error) {
	if err := ValidatePostgresSchemaEvolution(before, after); err != nil {
		return nil, err
	}
	beforeByKey := make(map[string]PostgresTableDescriptor, len(before))
	for _, t := range before {
		if !validatePostgresDescriptor(t) {
			return nil, fmt.Errorf("invalid PostgresTableDescriptor for table %q in BuildPostgresSchemaMigrationSQL", t.TableName)
		}
		key := postgresScopeTableIdentity(t)
		if _, dup := beforeByKey[key]; dup {
			return nil, fmt.Errorf("duplicate table %q in BuildPostgresSchemaMigrationSQL", t.TableName)
		}
		beforeByKey[key] = t
	}
	afterSeen := make(map[string]bool, len(after))
	matchedBefore := make(map[string]bool, len(before))
	var stmts []string
	for _, afterTbl := range after {
		if !validatePostgresDescriptor(afterTbl) {
			return nil, fmt.Errorf("invalid PostgresTableDescriptor for table %q in BuildPostgresSchemaMigrationSQL", afterTbl.TableName)
		}
		key := postgresScopeTableIdentity(afterTbl)
		if afterSeen[key] {
			return nil, fmt.Errorf("duplicate table %q in BuildPostgresSchemaMigrationSQL", afterTbl.TableName)
		}
		afterSeen[key] = true
		if beforeTbl, ok := beforeByKey[key]; ok {
			matchedBefore[key] = true
			alterStmts, err := BuildPostgresAlterTableSQL(beforeTbl, afterTbl, tenantSlug)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, alterStmts...)
			continue
		}
		createSQL := afterTbl.BuildPostgresCreateTableSQL(tenantSlug)
		if createSQL == "" {
			return nil, fmt.Errorf("failed to build PostgreSQL CREATE TABLE SQL for new table %q", afterTbl.TableName)
		}
		stmts = append(stmts, createSQL)
	}
	for _, beforeTbl := range before {
		key := postgresScopeTableIdentity(beforeTbl)
		if !matchedBefore[key] {
			return nil, fmt.Errorf("dropping existing PostgreSQL table %q is forbidden in BuildPostgresSchemaMigrationSQL", beforeTbl.TableName)
		}
	}
	return stmts, nil
}

// PostgresSearchIndexTables returns the 6 relational HAPI-style FHIR search index
// table descriptors (spidx_token, spidx_string, spidx_date, spidx_quantity,
// spidx_reference, spidx_uri) matching the Spidx* structs generated by search_index mode.
func PostgresSearchIndexTables() []PostgresTableDescriptor {
	return []PostgresTableDescriptor{
		{
			TableName:         "spidx_token",
			MessageName:       "SpidxToken",
			DatabaseScope:     "PUBLIC",
			PrimaryKeyColumns: []string{"tenant_id", "res_type", "res_id", "sp_name", "sp_system", "sp_value"},
			Indexes: []PostgresIndexDescriptor{
				{Name: "idx_spidx_token_lookup", Columns: []string{"tenant_id", "res_type", "sp_name", "sp_system", "sp_value"}, Using: "BTREE"},
			},
			Columns: []PostgresColumnDescriptor{
				{ColumnName: "tenant_id", ProtoFieldName: "tenant_id", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "res_type", ProtoFieldName: "res_type", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "res_id", ProtoFieldName: "res_id", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "sp_name", ProtoFieldName: "sp_name", SQLType: "VARCHAR(128)", PostgresType: "VARCHAR(128)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "sp_system", ProtoFieldName: "sp_system", SQLType: "VARCHAR(256)", PostgresType: "VARCHAR(256)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "sp_value", ProtoFieldName: "sp_value", SQLType: "VARCHAR(256)", PostgresType: "VARCHAR(256)", Nullable: false, IsPrimaryKey: true},
			},
		},
		{
			TableName:         "spidx_string",
			MessageName:       "SpidxString",
			DatabaseScope:     "PUBLIC",
			PrimaryKeyColumns: []string{"tenant_id", "res_type", "res_id", "sp_name", "sp_value"},
			Indexes: []PostgresIndexDescriptor{
				{Name: "idx_spidx_string_lookup", Columns: []string{"tenant_id", "res_type", "sp_name", "sp_value"}, Using: "BTREE"},
			},
			Columns: []PostgresColumnDescriptor{
				{ColumnName: "tenant_id", ProtoFieldName: "tenant_id", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "res_type", ProtoFieldName: "res_type", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "res_id", ProtoFieldName: "res_id", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "sp_name", ProtoFieldName: "sp_name", SQLType: "VARCHAR(128)", PostgresType: "VARCHAR(128)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "sp_value", ProtoFieldName: "sp_value", SQLType: "VARCHAR(256)", PostgresType: "VARCHAR(256)", Nullable: false, IsPrimaryKey: true},
			},
		},
		{
			TableName:         "spidx_date",
			MessageName:       "SpidxDate",
			DatabaseScope:     "PUBLIC",
			PrimaryKeyColumns: []string{"tenant_id", "res_type", "res_id", "sp_name", "sp_low", "sp_high"},
			Indexes: []PostgresIndexDescriptor{
				{Name: "idx_spidx_date_lookup", Columns: []string{"tenant_id", "res_type", "sp_name", "sp_low", "sp_high"}, Using: "BTREE"},
			},
			Columns: []PostgresColumnDescriptor{
				{ColumnName: "tenant_id", ProtoFieldName: "tenant_id", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "res_type", ProtoFieldName: "res_type", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "res_id", ProtoFieldName: "res_id", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "sp_name", ProtoFieldName: "sp_name", SQLType: "VARCHAR(128)", PostgresType: "VARCHAR(128)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "sp_low", ProtoFieldName: "sp_low", SQLType: "TIMESTAMPTZ", PostgresType: "TIMESTAMPTZ", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "sp_high", ProtoFieldName: "sp_high", SQLType: "TIMESTAMPTZ", PostgresType: "TIMESTAMPTZ", Nullable: false, IsPrimaryKey: true},
			},
		},
		{
			TableName:         "spidx_quantity",
			MessageName:       "SpidxQuantity",
			DatabaseScope:     "PUBLIC",
			PrimaryKeyColumns: []string{"tenant_id", "res_type", "res_id", "sp_name", "sp_system", "sp_code", "sp_value"},
			Indexes: []PostgresIndexDescriptor{
				{Name: "idx_spidx_quantity_lookup", Columns: []string{"tenant_id", "res_type", "sp_name", "sp_system", "sp_code", "sp_value"}, Using: "BTREE"},
			},
			Columns: []PostgresColumnDescriptor{
				{ColumnName: "tenant_id", ProtoFieldName: "tenant_id", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "res_type", ProtoFieldName: "res_type", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "res_id", ProtoFieldName: "res_id", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "sp_name", ProtoFieldName: "sp_name", SQLType: "VARCHAR(128)", PostgresType: "VARCHAR(128)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "sp_system", ProtoFieldName: "sp_system", SQLType: "VARCHAR(256)", PostgresType: "VARCHAR(256)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "sp_code", ProtoFieldName: "sp_code", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "sp_value", ProtoFieldName: "sp_value", SQLType: "DOUBLE PRECISION", PostgresType: "DOUBLE PRECISION", Nullable: false, IsPrimaryKey: true},
			},
		},
		{
			TableName:         "spidx_reference",
			MessageName:       "SpidxReference",
			DatabaseScope:     "PUBLIC",
			PrimaryKeyColumns: []string{"tenant_id", "res_type", "res_id", "sp_name", "target_type", "target_id"},
			Indexes: []PostgresIndexDescriptor{
				{Name: "idx_spidx_reference_lookup", Columns: []string{"tenant_id", "res_type", "sp_name", "target_type", "target_id"}, Using: "BTREE"},
			},
			Columns: []PostgresColumnDescriptor{
				{ColumnName: "tenant_id", ProtoFieldName: "tenant_id", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "res_type", ProtoFieldName: "res_type", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "res_id", ProtoFieldName: "res_id", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "sp_name", ProtoFieldName: "sp_name", SQLType: "VARCHAR(128)", PostgresType: "VARCHAR(128)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "target_type", ProtoFieldName: "target_type", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "target_id", ProtoFieldName: "target_id", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
			},
		},
		{
			TableName:         "spidx_uri",
			MessageName:       "SpidxURI",
			DatabaseScope:     "PUBLIC",
			PrimaryKeyColumns: []string{"tenant_id", "res_type", "res_id", "sp_name", "sp_value"},
			Indexes: []PostgresIndexDescriptor{
				{Name: "idx_spidx_uri_lookup", Columns: []string{"tenant_id", "res_type", "sp_name", "sp_value"}, Using: "BTREE"},
			},
			Columns: []PostgresColumnDescriptor{
				{ColumnName: "tenant_id", ProtoFieldName: "tenant_id", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "res_type", ProtoFieldName: "res_type", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "res_id", ProtoFieldName: "res_id", SQLType: "VARCHAR(64)", PostgresType: "VARCHAR(64)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "sp_name", ProtoFieldName: "sp_name", SQLType: "VARCHAR(128)", PostgresType: "VARCHAR(128)", Nullable: false, IsPrimaryKey: true},
				{ColumnName: "sp_value", ProtoFieldName: "sp_value", SQLType: "VARCHAR(512)", PostgresType: "VARCHAR(512)", Nullable: false, IsPrimaryKey: true},
			},
		},
	}
}

func generateLakehousePostgresDDLFile(gen *protogen.Plugin, file *protogen.File, opts *Options) error {
	tables, err := buildPostgresTablesForFile(file, opts)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return nil
	}
	outOverride := ""
	if opts != nil {
		outOverride = opts.OutputFile
	}
	filename, err := resolveOutputFilename(file.GeneratedFilenamePrefix, "_postgres.sql", outOverride)
	if err != nil {
		return err
	}
	g := gen.NewGeneratedFile(filename, "")
	writeLakehousePostgresDDLDocument(g, file.Desc.Path(), tables)
	return nil
}

func generateConsolidatedLakehousePostgresDDLFile(gen *protogen.Plugin, files []*protogen.File, tables []*postgresTableIR, outputFile string) error {
	if len(tables) == 0 {
		return nil
	}
	filename, err := resolveOutputFilename("", "_postgres.sql", outputFile)
	if err != nil {
		return err
	}
	sources := make([]string, 0, len(files))
	for _, f := range files {
		sources = append(sources, f.Desc.Path())
	}
	g := gen.NewGeneratedFile(filename, "")
	writeLakehousePostgresDDLDocument(g, strings.Join(sources, ", "), tables)
	return nil
}

func writeLakehousePostgresDDLDocument(g *protogen.GeneratedFile, sourceDesc string, tables []*postgresTableIR) {
	g.P("-- Code generated by proto2type. DO NOT EDIT.")
	g.P("-- source: ", sourceDesc)
	g.P("-- backend: postgres")
	g.P()
	emitted := 0
	for _, tbl := range tables {
		sql := tbl.Descriptor.BuildPostgresCreateTableWithoutForeignKeysSQL("${TENANT_SLUG}")
		if sql == "" {
			continue
		}
		if emitted > 0 {
			g.P()
		}
		emitted++
		g.P("-- ", tbl.GoName, " (", tbl.Descriptor.TableName, ")")
		if tbl.Descriptor.WriterService != "" {
			g.P("-- writer_service: ", tbl.Descriptor.WriterService)
		}
		if len(tbl.Descriptor.ReaderServices) > 0 {
			g.P("-- reader_services: ", strings.Join(tbl.Descriptor.ReaderServices, ", "))
		}
		g.P(sql)
	}

	var fkBlocks []string
	for _, tbl := range tables {
		if fkSQL := tbl.Descriptor.BuildPostgresForeignKeysSQL("${TENANT_SLUG}"); fkSQL != "" {
			fkBlocks = append(fkBlocks, fkSQL)
		}
	}
	if len(fkBlocks) > 0 {
		g.P()
		g.P("-- Foreign key constraints")
		g.P(strings.Join(fkBlocks, "\n"))
	}
}

func generateLakehousePostgresHCLFile(gen *protogen.Plugin, file *protogen.File, opts *Options) error {
	tables, err := buildPostgresTablesForFile(file, opts)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return nil
	}
	outOverride := ""
	if opts != nil {
		outOverride = opts.OutputFile
	}
	filename, err := resolveOutputFilename(file.GeneratedFilenamePrefix, "_postgres.hcl", outOverride)
	if err != nil {
		return err
	}
	g := gen.NewGeneratedFile(filename, "")
	writeLakehousePostgresHCLDocument(g, file.Desc.Path(), tables)
	return nil
}

func generateConsolidatedLakehousePostgresHCLFile(gen *protogen.Plugin, files []*protogen.File, tables []*postgresTableIR, outputFile string) error {
	if len(tables) == 0 {
		return nil
	}
	filename, err := resolveOutputFilename("", "_postgres.hcl", outputFile)
	if err != nil {
		return err
	}
	sources := make([]string, 0, len(files))
	for _, f := range files {
		sources = append(sources, f.Desc.Path())
	}
	g := gen.NewGeneratedFile(filename, "")
	writeLakehousePostgresHCLDocument(g, strings.Join(sources, ", "), tables)
	return nil
}

func writeLakehousePostgresHCLDocument(g *protogen.GeneratedFile, sourceDesc string, tables []*postgresTableIR) {
	g.P("# Code generated by proto2type. DO NOT EDIT.")
	g.P("# source: ", sourceDesc)
	g.P("# backend: postgres (hcl)")

	seenSchemas := make(map[string]bool)
	var schemaNames []string
	for _, tbl := range tables {
		if sName, ok := resolvePostgresSchemaName(tbl.Descriptor.DatabaseScope, "${TENANT_SLUG}"); ok && !seenSchemas[sName] {
			seenSchemas[sName] = true
			schemaNames = append(schemaNames, sName)
		}
	}
	sort.Strings(schemaNames)
	if len(schemaNames) > 0 {
		g.P()
		for _, sName := range schemaNames {
			g.P(fmt.Sprintf("schema %q {}", sName))
		}
	}
	for _, tbl := range tables {
		g.P()
		g.P("# ", tbl.GoName, " (", tbl.Descriptor.TableName, ")")
		if tbl.Descriptor.WriterService != "" {
			g.P("# writer_service: ", tbl.Descriptor.WriterService)
		}
		if len(tbl.Descriptor.ReaderServices) > 0 {
			g.P("# reader_services: ", strings.Join(tbl.Descriptor.ReaderServices, ", "))
		}
		g.P(tbl.Descriptor.BuildPostgresAtlasHCL("${TENANT_SLUG}"))
	}
}

func generatePostgresSearchIndexDDLFile(gen *protogen.Plugin, file *protogen.File, opts *Options) error {
	outOverride := ""
	if opts != nil {
		outOverride = opts.OutputFile
	}
	return generateConsolidatedPostgresSearchIndexDDLFile(gen, []*protogen.File{file}, outOverride)
}

func generateConsolidatedPostgresSearchIndexDDLFile(gen *protogen.Plugin, files []*protogen.File, outputFile string) error {
	if len(files) == 0 {
		return nil
	}
	filename, err := resolveOutputFilename(files[0].GeneratedFilenamePrefix, "_search_index.sql", outputFile)
	if err != nil {
		return err
	}
	sources := make([]string, 0, len(files))
	for _, f := range files {
		sources = append(sources, f.Desc.Path())
	}
	g := gen.NewGeneratedFile(filename, "")
	g.P("-- Code generated by proto2type. DO NOT EDIT.")
	g.P("-- source: ", strings.Join(sources, ", "))
	g.P("-- backend: postgres")
	g.P("-- mode: search_index")
	g.P()
	for i, tbl := range PostgresSearchIndexTables() {
		if i > 0 {
			g.P()
		}
		g.P("-- ", tbl.MessageName, " (", tbl.TableName, ")")
		g.P(tbl.BuildPostgresCreateTableSQL(""))
	}
	return nil
}

func generatePostgresSearchIndexHCLFile(gen *protogen.Plugin, file *protogen.File, opts *Options) error {
	outOverride := ""
	if opts != nil {
		outOverride = opts.OutputFile
	}
	return generateConsolidatedPostgresSearchIndexHCLFile(gen, []*protogen.File{file}, outOverride)
}

func generateConsolidatedPostgresSearchIndexHCLFile(gen *protogen.Plugin, files []*protogen.File, outputFile string) error {
	if len(files) == 0 {
		return nil
	}
	filename, err := resolveOutputFilename(files[0].GeneratedFilenamePrefix, "_search_index.hcl", outputFile)
	if err != nil {
		return err
	}
	sources := make([]string, 0, len(files))
	for _, f := range files {
		sources = append(sources, f.Desc.Path())
	}
	g := gen.NewGeneratedFile(filename, "")
	g.P("# Code generated by proto2type. DO NOT EDIT.")
	g.P("# source: ", strings.Join(sources, ", "))
	g.P("# backend: postgres (hcl)")
	g.P("# mode: search_index")
	g.P()
	g.P(`schema "public" {}`)
	for _, tbl := range PostgresSearchIndexTables() {
		g.P()
		g.P("# ", tbl.MessageName, " (", tbl.TableName, ")")
		g.P(tbl.BuildPostgresAtlasHCL(""))
	}
	return nil
}
