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

// formatPostgresHCLDefaultValue formats the Atlas HCL default attribute value for a PostgreSQL column.
func formatPostgresHCLDefaultValue(c PostgresColumnDescriptor) string {
	if c.ServerDefault != "" {
		return fmt.Sprintf("sql(%q)", strings.ToLower(strings.TrimSpace(c.ServerDefault)))
	}
	if c.DefaultValue == "" {
		return ""
	}
	sqlType := c.SQLType
	if sqlType == "" {
		sqlType = strings.ReplaceAll(c.HCLType, "double_precision", "double precision")
	}
	if isPostgresBooleanSQLType(sqlType) {
		return strings.ToLower(c.DefaultValue)
	}
	if isPostgresNumericSQLType(sqlType) {
		return c.DefaultValue
	}
	return strconv.Quote(strings.NewReplacer("${", "$${", "%{", "%%{").Replace(c.DefaultValue))
}

// formatPostgresHCLType normalizes a PostgreSQL type expression into a valid Atlas HCL type attribute.
// "double precision" maps to Atlas's native "double_precision" identifier, and array or multi-word
// SQL type expressions are wrapped in sql("...").
func formatPostgresHCLType(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "double precision" {
		return "double_precision"
	}
	if strings.HasSuffix(raw, "[]") || strings.Contains(raw, " ") {
		return fmt.Sprintf("sql(%q)", raw)
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
			validatePostgresDefaultValue(sqlType, c.DefaultValue) != nil {
			return ""
		}
		normCol := c
		normCol.SQLType = sqlType
		normCol.HCLType = hclType
		fmt.Fprintf(&b, "  column %q {\n", normCol.ColumnName)
		fmt.Fprintf(&b, "    null = %t\n", normCol.Nullable)
		fmt.Fprintf(&b, "    type = %s\n", formatPostgresHCLType(normCol.HCLType))
		if def := formatPostgresHCLDefaultValue(normCol); def != "" {
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
		if !isValidSQLIdentifier(idx.Name) || len(idx.Name) > maxPostgresIdentifierBytes || len(idx.Columns) == 0 {
			return ""
		}
		for _, col := range idx.Columns {
			if !isValidSQLIdentifier(col) || len(col) > maxPostgresIdentifierBytes {
				return ""
			}
		}
		fmt.Fprintf(&b, "  index %q {\n", idx.Name)
		if idx.Unique {
			b.WriteString("    unique  = true\n")
		}
		fmt.Fprintf(&b, "    columns = %s\n", formatAtlasHCLColRefList(idx.Columns))
		b.WriteString("  }\n")
	}

	for _, fk := range t.ForeignKeys {
		if !isValidSQLIdentifier(fk.Name) || len(fk.Name) > maxPostgresIdentifierBytes ||
			!isValidSQLIdentifier(fk.RefTable) || len(fk.RefTable) > maxPostgresIdentifierBytes ||
			!isValidSQLIdentifier(fk.RefColumn) || len(fk.RefColumn) > maxPostgresIdentifierBytes ||
			len(fk.Columns) == 0 {
			return ""
		}
		for _, col := range fk.Columns {
			if !isValidSQLIdentifier(col) || len(col) > maxPostgresIdentifierBytes {
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
		fmt.Fprintf(&b, "  foreign_key %q {\n", fk.Name)
		fmt.Fprintf(&b, "    columns     = %s\n", formatAtlasHCLColRefList(fk.Columns))
		fmt.Fprintf(&b, "    ref_columns = [table.%s.column.%s]\n", fk.RefTable, fk.RefColumn)
		if delHCL != "" {
			fmt.Fprintf(&b, "    on_delete   = %s\n", delHCL)
		}
		if updHCL != "" {
			fmt.Fprintf(&b, "    on_update   = %s\n", updHCL)
		}
		b.WriteString("  }\n")
	}

	b.WriteString("}")
	return b.String()
}

// generatePostgresHCLFile emits <prefix>_postgres.hcl for all PostgreSQL tables in file.
func generatePostgresHCLFile(gen *protogen.Plugin, file *protogen.File, tables []*PostgresTableSchemaDescriptor) error {
	if len(tables) == 0 {
		return nil
	}
	filename, err := outputFilename(file.GeneratedFilenamePrefix, "_postgres.hcl")
	if err != nil {
		return err
	}
	g := gen.NewGeneratedFile(filename, "")
	g.P("# Code generated by proto2type. DO NOT EDIT.")
	g.P("# source: ", file.Desc.Path())
	g.P("# backend: postgres (hcl)")
	g.P()
	g.P("schema \"tenant\" {}")
	g.P()

	for i, tbl := range tables {
		hcl := tbl.BuildPostgresAtlasHCL()
		if hcl == "" {
			continue
		}
		if i > 0 {
			g.P()
		}
		g.P("# ", tbl.GoName, " (", tbl.TableName, ")")
		if tbl.WriterService != "" {
			g.P("# writer_service: ", tbl.WriterService)
		}
		if len(tbl.ReaderServices) > 0 {
			g.P("# reader_services: ", strings.Join(tbl.ReaderServices, ", "))
		}
		g.P(hcl)
	}
	return nil
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

func fileHasLakehousePostgresTables(file *protogen.File) bool {
	if file == nil {
		return false
	}
	for _, msg := range file.Messages {
		if isMessageSkipped(msg) {
			continue
		}
		meta := extractDorisTableMeta(msg)
		if meta == nil {
			continue
		}
		if meta.DatabaseScope != "" ||
			meta.SyncTier != "" ||
			len(meta.PartitionByColumns) > 0 ||
			len(meta.NgramBfColumns) > 0 ||
			meta.EmbeddingDimension > 0 {
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

func validatePostgresDescriptor(t PostgresTableDescriptor) bool {
	if !isValidSQLIdentifier(t.TableName) || !isValidPostgresDatabaseScope(t.DatabaseScope) {
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
		colType := effectiveColPostgresType(c)
		if colType == "" || !isValidPostgresTypeSyntax(colType) || IsForbiddenPostgresColumnType(colType) {
			return false
		}
		if c.DefaultValue != "" && (!isValidDorisDefaultValue(c.DefaultValue) || validatePostgresDefaultValue(colType, c.DefaultValue) != nil) {
			return false
		}
		if !isValidPostgresServerDefault(c.ServerDefault) {
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

func buildPostgresTablesForFile(file *protogen.File, opts *Options) ([]*postgresTableIR, error) {
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
	for _, msg := range file.Messages {
		if !isMessageSkipped(msg) {
			collectInlineTargets(msg, make(map[protoreflect.FullName]bool))
		}
	}

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
		id := postgresScopeTableIdentity(tbl.Descriptor)
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
			if colMeta != nil && colMeta.DefaultValue != "" {
				if err := validatePostgresDefaultValue(pgType, colMeta.DefaultValue); err != nil {
					return fmt.Errorf("field %s: %w", field.Desc.FullName(), err)
				}
			}

			isRealOneof := field.Oneof != nil && !field.Oneof.Desc.IsSynthetic()
			nullable := field.Desc.HasOptionalKeyword() || isRealOneof || isPostgresWrapperWKT(field) ||
				(isWellKnownTimestamp(field) && !isPK && !isPart && !isSeq && !isServerTimestamp(field) && colName != "created_at" && colName != "updated_at")
			if colMeta != nil && colMeta.Nullable != nil {
				nullable = *colMeta.Nullable
			}
			if isPK || isPart || isSeq {
				nullable = false
			}

			defVal := ""
			if colMeta != nil && colMeta.DefaultValue != "" {
				defVal = colMeta.DefaultValue
			} else if !nullable {
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
					Name:    fmt.Sprintf("idx_%s_%s_key", meta.TableName, colName),
					Columns: []string{colName},
					Unique:  true,
					Using:   "BTREE",
				})
			} else if colMeta != nil && colMeta.Index && !isPK {
				indexes = append(indexes, PostgresIndexDescriptor{
					Name:    fmt.Sprintf("idx_%s_%s", meta.TableName, colName),
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
				if !isValidSQLIdentifier(refTable) || !isValidSQLIdentifier(refCol) {
					return fmt.Errorf("field %s: invalid foreign_key reference %q.%q", field.Desc.FullName(), refTable, refCol)
				}
				fks = append(fks, PostgresForeignKeyDescriptor{
					Name:      fmt.Sprintf("fk_%s_%s", meta.TableName, colName),
					Columns:   []string{colName},
					RefTable:  refTable,
					RefColumn: refCol,
					OnDelete:  strings.TrimSpace(colMeta.ForeignKey.OnDelete),
					OnUpdate:  strings.TrimSpace(colMeta.ForeignKey.OnUpdate),
				})
			}

			cols = append(cols, PostgresColumnDescriptor{
				ColumnName:     colName,
				ProtoFieldName: protoFieldName,
				SQLType:        pgType,
				PostgresType:   pgType,
				HCLType:        strings.ToLower(pgType),
				Nullable:       nullable,
				IsPrimaryKey:   isPK,
				IsPartitionKey: isPart,
				InvertedIndex:  colMeta != nil && colMeta.InvertedIndex,
				DefaultValue:   defVal,
				ServerDefault:  srvDef,
				Comment:        cleanComment(string(field.Comments.Leading)),
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
			Name:    fmt.Sprintf("idx_%s_dup_key", meta.TableName),
			Columns: dedupDorisColumnList(meta.DuplicateKeyColumns),
			Using:   "BTREE",
		})
	}
	for _, idxIR := range meta.Indexes {
		idxCols := dedupDorisColumnList(idxIR.Columns)
		if len(idxCols) == 0 {
			continue
		}
		idxName := strings.TrimSpace(idxIR.Name)
		if idxName == "" {
			suffix := strings.Join(idxCols, "_")
			if idxIR.Unique {
				idxName = fmt.Sprintf("idx_%s_%s_key", meta.TableName, suffix)
			} else {
				idxName = fmt.Sprintf("idx_%s_%s", meta.TableName, suffix)
			}
		}
		indexes = append(indexes, PostgresIndexDescriptor{
			Name:    idxName,
			Columns: idxCols,
			Unique:  idxIR.Unique,
			Using:   "BTREE",
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

func formatPostgresDefaultLiteral(pgType, defVal string) string {
	upper := strings.ToUpper(strings.TrimSpace(pgType))
	trimmed := strings.TrimSpace(defVal)
	if trimmed == "[]" && strings.HasSuffix(upper, "[]") {
		return "'{}'"
	}
	if strings.HasSuffix(upper, "[]") {
		if strings.HasPrefix(trimmed, "'") && strings.HasSuffix(trimmed, "'") {
			return trimmed
		}
		return fmt.Sprintf("'%s'", strings.ReplaceAll(trimmed, "'", "''"))
	}
	if isPostgresBooleanSQLType(upper) && validatePostgresDefaultValue(upper, trimmed) == nil {
		return strings.ToLower(trimmed)
	}
	if isPostgresNumericSQLType(upper) && validatePostgresDefaultValue(upper, trimmed) == nil {
		return trimmed
	}
	return fmt.Sprintf("'%s'", strings.ReplaceAll(trimmed, "'", "''"))
}

// BuildPostgresCreateTableSQL renders a deterministic PostgreSQL CREATE TABLE statement
// (plus any secondary GIN or pgvector HNSW CREATE INDEX statements) for this descriptor.
func (t PostgresTableDescriptor) BuildPostgresCreateTableSQL(tenantSlug string) string {
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
	partCols := dedupDorisColumnList(t.PartitionByColumns)
	if len(partCols) == 0 {
		for _, c := range t.Columns {
			if c.IsPartitionKey {
				partCols = append(partCols, c.ColumnName)
			}
		}
		partCols = dedupDorisColumnList(partCols)
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

	colTypes := make(map[string]string, len(t.Columns))
	var colLines []string
	for _, c := range t.Columns {
		upperType := strings.ToUpper(effectiveColPostgresType(c))
		colTypes[c.ColumnName] = upperType
		line := fmt.Sprintf("    %s %s", c.ColumnName, upperType)
		if !c.Nullable {
			line += " NOT NULL"
		}
		if c.DefaultValue != "" {
			line += " DEFAULT " + formatPostgresDefaultLiteral(upperType, c.DefaultValue)
		}
		colLines = append(colLines, line)
	}
	if len(pks) > 0 {
		colLines = append(colLines, fmt.Sprintf("    PRIMARY KEY (%s)", strings.Join(pks, ", ")))
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
		idxName := fmt.Sprintf("idx_%s_%s", t.TableName, idxCol)
		emittedIdx[idxName] = true
		fmt.Fprintf(&b, "\nCREATE INDEX IF NOT EXISTS %s ON %s.%s USING gin (%s);", idxName, schemaName, t.TableName, idxCol)
	}
	for _, ngCol := range dedupDorisColumnList(t.NgramBfColumns) {
		idxName := fmt.Sprintf("idx_ngram_%s_%s", t.TableName, ngCol)
		emittedIdx[idxName] = true
		fmt.Fprintf(&b, "\nCREATE INDEX IF NOT EXISTS %s ON %s.%s USING gin (%s);", idxName, schemaName, t.TableName, ngCol)
	}
	for _, c := range t.Columns {
		if strings.HasPrefix(colTypes[c.ColumnName], "VECTOR(") {
			idxName := fmt.Sprintf("idx_%s_hnsw", c.ColumnName)
			emittedIdx[idxName] = true
			fmt.Fprintf(&b, "\nCREATE INDEX IF NOT EXISTS %s ON %s.%s USING hnsw (%s vector_l2_ops);", idxName, schemaName, t.TableName, c.ColumnName)
		}
	}
	for _, idx := range t.Indexes {
		if idx.Name == "" || emittedIdx[idx.Name] || len(idx.Columns) == 0 {
			continue
		}
		emittedIdx[idx.Name] = true
		uniqueKw := ""
		if idx.Unique {
			uniqueKw = "UNIQUE "
		}
		using := strings.ToLower(strings.TrimSpace(idx.Using))
		if using == "" || using == "btree" {
			fmt.Fprintf(&b, "\nCREATE %sINDEX IF NOT EXISTS %s ON %s.%s (%s);", uniqueKw, idx.Name, schemaName, t.TableName, strings.Join(idx.Columns, ", "))
		} else if idx.Ops != "" && len(idx.Columns) == 1 {
			fmt.Fprintf(&b, "\nCREATE %sINDEX IF NOT EXISTS %s ON %s.%s USING %s (%s %s);", uniqueKw, idx.Name, schemaName, t.TableName, using, idx.Columns[0], idx.Ops)
		} else {
			fmt.Fprintf(&b, "\nCREATE %sINDEX IF NOT EXISTS %s ON %s.%s USING %s (%s);", uniqueKw, idx.Name, schemaName, t.TableName, using, strings.Join(idx.Columns, ", "))
		}
	}
	for _, fk := range t.ForeignKeys {
		if fk.Name == "" || fk.RefTable == "" || len(fk.Columns) == 0 {
			continue
		}
		refCol := fk.RefColumn
		if refCol == "" {
			refCol = "id"
		}
		clause := fmt.Sprintf("\nALTER TABLE %s.%s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s.%s (%s)",
			schemaName, t.TableName, fk.Name, strings.Join(fk.Columns, ", "), schemaName, fk.RefTable, refCol)
		if _, delSQL, ok := isValidForeignKeyAction(fk.OnDelete); ok && delSQL != "" {
			clause += " ON DELETE " + delSQL
		}
		if _, updSQL, ok := isValidForeignKeyAction(fk.OnUpdate); ok && updSQL != "" {
			clause += " ON UPDATE " + updSQL
		}
		b.WriteString(clause + ";")
	}
	return b.String()
}

func postgresTypeToAtlasHCLType(pgType string) string {
	pt := strings.TrimSpace(pgType)
	if pt == "" {
		return "varchar(256)"
	}
	lower := strings.ToLower(pt)
	lower = strings.ReplaceAll(lower, ", ", ",")
	lower = strings.ReplaceAll(lower, ",", ", ")
	if strings.HasPrefix(lower, "vector(") {
		return fmt.Sprintf("sql(%q)", lower)
	}
	if lower == "double precision" {
		return "double_precision"
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
	partCols := dedupDorisColumnList(t.PartitionByColumns)
	if len(partCols) == 0 {
		for _, c := range t.Columns {
			if c.IsPartitionKey {
				partCols = append(partCols, c.ColumnName)
			}
		}
		partCols = dedupDorisColumnList(partCols)
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
		if c.DefaultValue != "" {
			fmt.Fprintf(&b, "    default = %q\n", c.DefaultValue)
		}
		if c.Comment != "" {
			fmt.Fprintf(&b, "    comment = %q\n", c.Comment)
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
		idxName := fmt.Sprintf("idx_%s_%s", t.TableName, idxCol)
		emittedIdx[idxName] = true
		fmt.Fprintf(&b, "  index %q {\n", idxName)
		fmt.Fprintf(&b, "    columns = [column.%s]\n", idxCol)
		b.WriteString("    type = \"GIN\"\n")
		b.WriteString("  }\n")
	}
	for _, ngCol := range dedupDorisColumnList(t.NgramBfColumns) {
		idxName := fmt.Sprintf("idx_ngram_%s_%s", t.TableName, ngCol)
		emittedIdx[idxName] = true
		fmt.Fprintf(&b, "  index %q {\n", idxName)
		fmt.Fprintf(&b, "    columns = [column.%s]\n", ngCol)
		b.WriteString("    type = \"GIN\"\n")
		b.WriteString("  }\n")
	}
	for _, c := range t.Columns {
		if strings.HasPrefix(colTypes[c.ColumnName], "VECTOR(") {
			idxName := fmt.Sprintf("idx_%s_hnsw", c.ColumnName)
			emittedIdx[idxName] = true
			fmt.Fprintf(&b, "  index %q {\n", idxName)
			fmt.Fprintf(&b, "    columns = [column.%s]\n", c.ColumnName)
			b.WriteString("    type = \"HNSW\"\n")
			b.WriteString("    ops = \"vector_l2_ops\"\n")
			b.WriteString("  }\n")
		}
	}
	for _, idx := range t.Indexes {
		if idx.Name == "" || emittedIdx[idx.Name] || len(idx.Columns) == 0 {
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
		fmt.Fprintf(&b, "    columns = %s\n", formatAtlasHCLColRefList(idx.Columns))
		fmt.Fprintf(&b, "    type = %q\n", using)
		if idx.Ops != "" {
			fmt.Fprintf(&b, "    ops = %q\n", idx.Ops)
		}
		b.WriteString("  }\n")
	}
	for _, fk := range t.ForeignKeys {
		if fk.Name == "" || fk.RefTable == "" || len(fk.Columns) == 0 {
			continue
		}
		refCol := fk.RefColumn
		if refCol == "" {
			refCol = "id"
		}
		fmt.Fprintf(&b, "  foreign_key %q {\n", fk.Name)
		fmt.Fprintf(&b, "    columns     = %s\n", formatAtlasHCLColRefList(fk.Columns))
		fmt.Fprintf(&b, "    ref_columns = [table.%s.column.%s]\n", fk.RefTable, refCol)
		if delHCL, _, ok := isValidForeignKeyAction(fk.OnDelete); ok && delHCL != "" {
			fmt.Fprintf(&b, "    on_delete   = %s\n", delHCL)
		}
		if updHCL, _, ok := isValidForeignKeyAction(fk.OnUpdate); ok && updHCL != "" {
			fmt.Fprintf(&b, "    on_update   = %s\n", updHCL)
		}
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
		pkSet[pk] = true
	}
	partSet := make(map[string]bool, len(desc.PartitionByColumns))
	for _, pk := range desc.PartitionByColumns {
		partSet[pk] = true
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
		kv, err := parseHCLKeyValueAssignments(blockBody)
		if err != nil {
			return fmt.Errorf("column %q: %w", colName, err)
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
		srvDef := ""
		if rawDef, hasDef := kv["default"]; hasDef {
			trimmedDef := strings.TrimSpace(rawDef)
			if strings.HasPrefix(trimmedDef, "sql(") && strings.HasSuffix(trimmedDef, ")") {
				inner := unquoteHCLString(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmedDef, "sql("), ")")))
				srvDef = strings.ToUpper(inner)
			} else {
				defVal = unquoteHCLString(rawDef)
			}
		}
		comment := ""
		if rawComment, hasComment := kv["comment"]; hasComment {
			comment = unquoteHCLString(rawComment)
		}
		*colOrder = append(*colOrder, colName)
		colMap[colName] = PostgresColumnDescriptor{
			ColumnName:     colName,
			ProtoFieldName: colName,
			SQLType:        pgType,
			PostgresType:   pgType,
			HCLType:        strings.ToLower(pgType),
			Nullable:       nullable,
			DefaultValue:   defVal,
			ServerDefault:  srvDef,
			Comment:        comment,
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
		if !isValidSQLIdentifier(idxName) {
			return fmt.Errorf("invalid index name %q", idxName)
		}
		kv, err := parseHCLKeyValueAssignments(blockBody)
		if err != nil {
			return fmt.Errorf("index %q: %w", idxName, err)
		}
		var cols []string
		if colsRaw, ok := kv["columns"]; ok {
			cols, err = parseHCLColumnRefList(colsRaw)
			if err != nil {
				return fmt.Errorf("index %q: %w", idxName, err)
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
		ops := unquoteHCLString(kv["ops"])
		desc.Indexes = append(desc.Indexes, PostgresIndexDescriptor{
			Name:    idxName,
			Columns: cols,
			Unique:  unique,
			Using:   using,
			Ops:     ops,
		})
		if using == "GIN" {
			if strings.HasPrefix(idxName, "idx_ngram_") {
				desc.NgramBfColumns = dedupDorisColumnList(append(desc.NgramBfColumns, cols...))
			} else {
				desc.InvertedIndexColumns = dedupDorisColumnList(append(desc.InvertedIndexColumns, cols...))
			}
		}
		return nil

	case strings.HasPrefix(header, "foreign_key "):
		fkName := unquoteHCLString(strings.TrimSpace(strings.TrimPrefix(header, "foreign_key")))
		if !isValidSQLIdentifier(fkName) {
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
		if refRaw, ok := kv["ref_columns"]; ok {
			trimmedRef := strings.TrimSpace(refRaw)
			if strings.HasPrefix(trimmedRef, "[") && strings.HasSuffix(trimmedRef, "]") {
				innerRef := strings.TrimSpace(trimmedRef[1 : len(trimmedRef)-1])
				parts := strings.Split(innerRef, ".")
				if len(parts) == 4 && parts[0] == "table" && parts[2] == "column" {
					refTable = unquoteHCLString(parts[1])
					refCol = unquoteHCLString(parts[3])
				}
			}
		}
		desc.ForeignKeys = append(desc.ForeignKeys, PostgresForeignKeyDescriptor{
			Name:      fkName,
			Columns:   cols,
			RefTable:  refTable,
			RefColumn: refCol,
			OnDelete:  unquoteHCLString(kv["on_delete"]),
			OnUpdate:  unquoteHCLString(kv["on_update"]),
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
		if !ac.Nullable && !strings.HasSuffix(strings.ToUpper(acType), "[]") &&
			!IsForbiddenPostgresColumnType(acType) && strings.TrimSpace(ac.DefaultValue) == "" && strings.TrimSpace(ac.ServerDefault) == "" {
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
		if bc.DefaultValue != ac.DefaultValue || bc.ServerDefault != ac.ServerDefault {
			if ac.DefaultValue != "" {
				stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s;", fullTableName, ac.ColumnName, formatPostgresDefaultLiteral(afterType, ac.DefaultValue)))
			} else if ac.ServerDefault != "" {
				stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s;", fullTableName, ac.ColumnName, strings.ToUpper(strings.TrimSpace(ac.ServerDefault))))
			} else {
				stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT;", fullTableName, ac.ColumnName))
			}
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
		if ac.DefaultValue != "" {
			clause += " DEFAULT " + formatPostgresDefaultLiteral(afterType, ac.DefaultValue)
		} else if ac.ServerDefault != "" {
			clause += " DEFAULT " + strings.ToUpper(strings.TrimSpace(ac.ServerDefault))
		}
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;", fullTableName, clause))
	}
	beforeIdx := make(map[string]bool)
	for _, c := range before.Columns {
		if c.InvertedIndex {
			beforeIdx[fmt.Sprintf("idx_%s_%s", before.TableName, c.ColumnName)] = true
		}
	}
	for _, ic := range before.InvertedIndexColumns {
		if ic != "" {
			beforeIdx[fmt.Sprintf("idx_%s_%s", before.TableName, ic)] = true
		}
	}
	for _, ngCol := range dedupDorisColumnList(before.NgramBfColumns) {
		beforeIdx[fmt.Sprintf("idx_ngram_%s_%s", before.TableName, ngCol)] = true
	}
	for _, idx := range before.Indexes {
		if idx.Name != "" {
			beforeIdx[idx.Name] = true
		}
	}
	for _, idx := range after.Indexes {
		if idx.Name == "" || beforeIdx[idx.Name] || len(idx.Columns) == 0 {
			continue
		}
		beforeIdx[idx.Name] = true
		uniqueKw := ""
		if idx.Unique {
			uniqueKw = "UNIQUE "
		}
		using := strings.ToLower(strings.TrimSpace(idx.Using))
		if using == "" || using == "btree" {
			stmts = append(stmts, fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s (%s);", uniqueKw, idx.Name, fullTableName, strings.Join(idx.Columns, ", ")))
		} else if idx.Ops != "" && len(idx.Columns) == 1 {
			stmts = append(stmts, fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s USING %s (%s %s);", uniqueKw, idx.Name, fullTableName, using, idx.Columns[0], idx.Ops))
		} else {
			stmts = append(stmts, fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s USING %s (%s);", uniqueKw, idx.Name, fullTableName, using, strings.Join(idx.Columns, ", ")))
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
		refCol := fk.RefColumn
		if refCol == "" {
			refCol = "id"
		}
		clause := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s.%s (%s)",
			fullTableName, fk.Name, strings.Join(fk.Columns, ", "), schemaName, fk.RefTable, refCol)
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
	filename, err := outputFilename(file.GeneratedFilenamePrefix, "_postgres.sql")
	if err != nil {
		return err
	}
	g := gen.NewGeneratedFile(filename, "")
	g.P("-- Code generated by proto2type. DO NOT EDIT.")
	g.P("-- source: ", file.Desc.Path())
	g.P("-- backend: postgres")
	g.P()
	for i, tbl := range tables {
		if i > 0 {
			g.P()
		}
		g.P("-- ", tbl.GoName, " (", tbl.Descriptor.TableName, ")")
		if tbl.Descriptor.WriterService != "" {
			g.P("-- writer_service: ", tbl.Descriptor.WriterService)
		}
		if len(tbl.Descriptor.ReaderServices) > 0 {
			g.P("-- reader_services: ", strings.Join(tbl.Descriptor.ReaderServices, ", "))
		}
		g.P(tbl.Descriptor.BuildPostgresCreateTableSQL("${TENANT_SLUG}"))
	}
	return nil
}

func generateLakehousePostgresHCLFile(gen *protogen.Plugin, file *protogen.File, opts *Options) error {
	tables, err := buildPostgresTablesForFile(file, opts)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return nil
	}
	filename, err := outputFilename(file.GeneratedFilenamePrefix, "_postgres.hcl")
	if err != nil {
		return err
	}
	g := gen.NewGeneratedFile(filename, "")
	g.P("# Code generated by proto2type. DO NOT EDIT.")
	g.P("# source: ", file.Desc.Path())
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
	return nil
}

func generatePostgresSearchIndexDDLFile(gen *protogen.Plugin, file *protogen.File) error {
	filename, err := outputFilename(file.GeneratedFilenamePrefix, "_search_index.sql")
	if err != nil {
		return err
	}
	g := gen.NewGeneratedFile(filename, "")
	g.P("-- Code generated by proto2type. DO NOT EDIT.")
	g.P("-- source: ", file.Desc.Path())
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

func generatePostgresSearchIndexHCLFile(gen *protogen.Plugin, file *protogen.File) error {
	filename, err := outputFilename(file.GeneratedFilenamePrefix, "_search_index.hcl")
	if err != nil {
		return err
	}
	g := gen.NewGeneratedFile(filename, "")
	g.P("# Code generated by proto2type. DO NOT EDIT.")
	g.P("# source: ", file.Desc.Path())
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
