package generator

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// PostgresColumnDescriptor describes a single physical column in a PostgreSQL table.
type PostgresColumnDescriptor struct {
	ColumnName     string
	ProtoFieldName string
	SQLType        string
	HCLType        string
	Nullable       bool
	DefaultValue   string
	ServerDefault  string
}

// PostgresIndexDescriptor describes a single or composite index on a PostgreSQL table.
type PostgresIndexDescriptor struct {
	Name    string
	Columns []string
	Unique  bool
}

// PostgresForeignKeyDescriptor describes a foreign key constraint on a PostgreSQL table.
type PostgresForeignKeyDescriptor struct {
	Name      string
	Columns   []string
	RefTable  string
	RefColumn string
	OnDelete  string
	OnUpdate  string
}

// PostgresTableSchemaDescriptor describes the complete relational schema of a PostgreSQL table
// for Atlas HCL and SQL DDL generation.
type PostgresTableSchemaDescriptor struct {
	GoName         string
	FullName       string
	TableName      string
	WriterService  string
	ReaderServices []string
	PrimaryKeys    []string
	Columns        []PostgresColumnDescriptor
	Indexes        []PostgresIndexDescriptor
	ForeignKeys    []PostgresForeignKeyDescriptor
}

// isDisallowedPostgresTypeKeyword reports whether word is a SQL constraint or DDL
// keyword that must never appear inside a column type expression.
func isDisallowedPostgresTypeKeyword(word string) bool {
	switch strings.ToUpper(word) {
	case "NOT", "NULL", "DEFAULT", "PRIMARY", "KEY", "REFERENCES", "CHECK",
		"UNIQUE", "CONSTRAINT", "FOREIGN", "DROP", "TABLE", "SELECT", "INSERT",
		"UPDATE", "DELETE", "ALTER", "CREATE", "GRANT", "REVOKE", "TRUNCATE",
		"UNION", "WHERE", "FROM":
		return true
	default:
		return false
	}
}

// isValidPostgresSQLTypeSyntax reports whether s is a safe PostgreSQL column type expression
// (e.g. "TEXT", "VARCHAR(64)", "NUMERIC(20,0)", "DOUBLE PRECISION", "TEXT[]", "UUID", "JSONB").
func isValidPostgresSQLTypeSyntax(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return false
	}
	for strings.HasSuffix(s, "[]") {
		s = strings.TrimSuffix(s, "[]")
	}
	if len(s) == 0 {
		return false
	}
	if isValidSQLTypeSyntax(s) {
		return !isDisallowedPostgresTypeKeyword(s)
	}
	// Allow multi-word standard Postgres types such as "DOUBLE PRECISION",
	// "TIMESTAMP WITH TIME ZONE", "CHARACTER VARYING(255)".
	words := strings.Fields(s)
	if len(words) == 0 {
		return false
	}
	for _, w := range words {
		if !isValidSQLTypeSyntax(w) || isDisallowedPostgresTypeKeyword(w) {
			return false
		}
	}
	// Reject consecutive spaces or control characters
	if strings.Join(words, " ") != s {
		return false
	}
	return true
}

// isValidForeignKeyAction reports whether action is a valid SQL referential action.
func isValidForeignKeyAction(action string) (string, string, bool) {
	norm := strings.ToUpper(strings.TrimSpace(action))
	norm = strings.ReplaceAll(norm, "_", " ")
	switch norm {
	case "":
		return "", "", true
	case "CASCADE":
		return "CASCADE", "CASCADE", true
	case "RESTRICT":
		return "RESTRICT", "RESTRICT", true
	case "SET NULL":
		return "SET_NULL", "SET NULL", true
	case "SET DEFAULT":
		return "SET_DEFAULT", "SET DEFAULT", true
	case "NO ACTION":
		return "NO_ACTION", "NO ACTION", true
	default:
		return "", "", false
	}
}

// mapScalarPostgresTypes returns (ddlType, hclType) for a protobuf scalar kind.
func mapScalarPostgresTypes(kind protoreflect.Kind, colMeta *DorisColumnMetaIR) (string, string) {
	if colMeta != nil {
		if colMeta.JSONB {
			return "JSONB", "jsonb"
		}
		if kind == protoreflect.StringKind && colMeta.Length > 0 {
			return fmt.Sprintf("VARCHAR(%d)", colMeta.Length), fmt.Sprintf("varchar(%d)", colMeta.Length)
		}
	}
	switch kind {
	case protoreflect.BoolKind:
		return "BOOLEAN", "boolean"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "INTEGER", "integer"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "BIGINT", "bigint"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "NUMERIC(20,0)", "numeric(20,0)"
	case protoreflect.FloatKind:
		return "REAL", "real"
	case protoreflect.DoubleKind:
		return "DOUBLE PRECISION", "double precision"
	case protoreflect.BytesKind:
		return "BYTEA", "bytea"
	case protoreflect.StringKind:
		return "TEXT", "text"
	default:
		return "TEXT", "text"
	}
}

// resolvePostgresFieldTypes maps a DomainField and optional ColumnConfig to (ddlType, hclType, error).
func resolvePostgresFieldTypes(f *DomainField, colMeta *DorisColumnMetaIR, opts *Options) (string, string, error) {
	if colMeta != nil && colMeta.SQLType != "" {
		if !isValidPostgresSQLTypeSyntax(colMeta.SQLType) {
			return "", "", fmt.Errorf("field %s: invalid PostgreSQL sql_type %q", f.Name, colMeta.SQLType)
		}
		ddl := strings.ToUpper(strings.TrimSpace(colMeta.SQLType))
		hcl := strings.ToLower(strings.TrimSpace(colMeta.SQLType))
		return ddl, hcl, nil
	}
	if colMeta != nil && colMeta.JSONB {
		return "JSONB", "jsonb", nil
	}

	if f.IsMap {
		return "JSONB", "jsonb", nil
	}

	if f.Repeated {
		switch f.Kind {
		case FieldKindMessage, FieldKindStruct, FieldKindValue, FieldKindListValue, FieldKindAny:
			return "JSONB", "jsonb", nil
		case FieldKindTimestamp:
			return "TIMESTAMPTZ[]", "timestamptz[]", nil
		case FieldKindDuration:
			return "BIGINT[]", "bigint[]", nil
		case FieldKindEnum:
			if f.EnumAsString || (colMeta != nil && colMeta.StripEnumPrefix) || (opts != nil && opts.EnumAsString) {
				return "TEXT[]", "text[]", nil
			}
			return "INTEGER[]", "integer[]", nil
		case FieldKindWrapperBool:
			return "BOOLEAN[]", "boolean[]", nil
		case FieldKindWrapperInt32:
			return "INTEGER[]", "integer[]", nil
		case FieldKindWrapperInt64, FieldKindWrapperUInt32:
			return "BIGINT[]", "bigint[]", nil
		case FieldKindWrapperUInt64:
			return "NUMERIC(20,0)[]", "numeric(20,0)[]", nil
		case FieldKindWrapperFloat:
			return "REAL[]", "real[]", nil
		case FieldKindWrapperDouble:
			return "DOUBLE PRECISION[]", "double precision[]", nil
		case FieldKindWrapperString:
			if colMeta != nil && colMeta.Length > 0 {
				return fmt.Sprintf("VARCHAR(%d)[]", colMeta.Length), fmt.Sprintf("varchar(%d)[]", colMeta.Length), nil
			}
			return "TEXT[]", "text[]", nil
		case FieldKindWrapperBytes:
			return "BYTEA[]", "bytea[]", nil
		default:
			if f.ProtoField != nil && isWellKnownDate(f.ProtoField) {
				return "DATE[]", "date[]", nil
			}
			ddl, hcl := mapScalarPostgresTypes(f.ScalarKind, colMeta)
			return ddl + "[]", hcl + "[]", nil
		}
	}

	if f.ProtoField != nil && isWellKnownDate(f.ProtoField) {
		return "DATE", "date", nil
	}

	switch f.Kind {
	case FieldKindTimestamp:
		return "TIMESTAMPTZ", "timestamptz", nil
	case FieldKindDuration:
		return "BIGINT", "bigint", nil
	case FieldKindStruct, FieldKindValue, FieldKindListValue, FieldKindAny, FieldKindMessage:
		return "JSONB", "jsonb", nil
	case FieldKindFieldMask:
		return "TEXT[]", "text[]", nil
	case FieldKindEmpty:
		return "JSONB", "jsonb", nil
	case FieldKindEnum:
		if f.EnumAsString || (colMeta != nil && colMeta.StripEnumPrefix) || (opts != nil && opts.EnumAsString) {
			if colMeta != nil && colMeta.Length > 0 {
				return fmt.Sprintf("VARCHAR(%d)", colMeta.Length), fmt.Sprintf("varchar(%d)", colMeta.Length), nil
			}
			return "TEXT", "text", nil
		}
		return "INTEGER", "integer", nil
	case FieldKindWrapperString:
		if colMeta != nil && colMeta.Length > 0 {
			return fmt.Sprintf("VARCHAR(%d)", colMeta.Length), fmt.Sprintf("varchar(%d)", colMeta.Length), nil
		}
		return "TEXT", "text", nil
	case FieldKindWrapperInt32:
		return "INTEGER", "integer", nil
	case FieldKindWrapperInt64, FieldKindWrapperUInt32:
		return "BIGINT", "bigint", nil
	case FieldKindWrapperUInt64:
		return "NUMERIC(20,0)", "numeric(20,0)", nil
	case FieldKindWrapperBool:
		return "BOOLEAN", "boolean", nil
	case FieldKindWrapperFloat:
		return "REAL", "real", nil
	case FieldKindWrapperDouble:
		return "DOUBLE PRECISION", "double precision", nil
	case FieldKindWrapperBytes:
		return "BYTEA", "bytea", nil
	default:
		ddl, hcl := mapScalarPostgresTypes(f.ScalarKind, colMeta)
		return ddl, hcl, nil
	}
}

// resolveOneofVariantPostgresTypes maps a OneofVariant and optional ColumnConfig to (ddlType, hclType, error).
func resolveOneofVariantPostgresTypes(v *OneofVariant, colMeta *DorisColumnMetaIR, opts *Options) (string, string, error) {
	if colMeta != nil && colMeta.SQLType != "" {
		if !isValidPostgresSQLTypeSyntax(colMeta.SQLType) {
			return "", "", fmt.Errorf("oneof variant %s: invalid PostgreSQL sql_type %q", v.ProtoName, colMeta.SQLType)
		}
		ddl := strings.ToUpper(strings.TrimSpace(colMeta.SQLType))
		hcl := strings.ToLower(strings.TrimSpace(colMeta.SQLType))
		return ddl, hcl, nil
	}
	if colMeta != nil && colMeta.JSONB {
		return "JSONB", "jsonb", nil
	}
	if v.ProtoField != nil && isWellKnownDate(v.ProtoField) {
		return "DATE", "date", nil
	}
	switch v.Kind {
	case FieldKindTimestamp:
		return "TIMESTAMPTZ", "timestamptz", nil
	case FieldKindDuration:
		return "BIGINT", "bigint", nil
	case FieldKindMessage, FieldKindStruct, FieldKindValue, FieldKindListValue, FieldKindAny, FieldKindEmpty:
		return "JSONB", "jsonb", nil
	case FieldKindFieldMask:
		return "TEXT[]", "text[]", nil
	case FieldKindEnum:
		if v.EnumAsString || (colMeta != nil && colMeta.StripEnumPrefix) || (opts != nil && opts.EnumAsString) {
			if colMeta != nil && colMeta.Length > 0 {
				return fmt.Sprintf("VARCHAR(%d)", colMeta.Length), fmt.Sprintf("varchar(%d)", colMeta.Length), nil
			}
			return "TEXT", "text", nil
		}
		return "INTEGER", "integer", nil
	case FieldKindWrapperString:
		if colMeta != nil && colMeta.Length > 0 {
			return fmt.Sprintf("VARCHAR(%d)", colMeta.Length), fmt.Sprintf("varchar(%d)", colMeta.Length), nil
		}
		return "TEXT", "text", nil
	case FieldKindWrapperInt32:
		return "INTEGER", "integer", nil
	case FieldKindWrapperInt64, FieldKindWrapperUInt32:
		return "BIGINT", "bigint", nil
	case FieldKindWrapperUInt64:
		return "NUMERIC(20,0)", "numeric(20,0)", nil
	case FieldKindWrapperBool:
		return "BOOLEAN", "boolean", nil
	case FieldKindWrapperFloat:
		return "REAL", "real", nil
	case FieldKindWrapperDouble:
		return "DOUBLE PRECISION", "double precision", nil
	case FieldKindWrapperBytes:
		return "BYTEA", "bytea", nil
	default:
		ddl, hcl := mapScalarPostgresTypes(v.ScalarKind, colMeta)
		return ddl, hcl, nil
	}
}

// isPostgresFieldNullable determines whether a DomainField is nullable in PostgreSQL.
func isPostgresFieldNullable(f *DomainField, colMeta *DorisColumnMetaIR, isPK bool) bool {
	if isPK {
		return false
	}
	if colMeta != nil && colMeta.Nullable != nil {
		return *colMeta.Nullable
	}
	if f.ServerTimestamp {
		return false
	}
	if f.ValidateConstraints != nil && f.ValidateConstraints.Required {
		return false
	}
	if f.IsRequired() {
		return false
	}
	if f.Repeated || f.IsMap {
		return false
	}
	if f.Optional || f.Kind.IsWrapper() {
		return true
	}
	if f.ProtoField != nil && isWellKnownDate(f.ProtoField) {
		return true
	}
	switch f.Kind {
	case FieldKindTimestamp, FieldKindMessage, FieldKindStruct, FieldKindValue, FieldKindListValue, FieldKindAny, FieldKindEmpty:
		return true
	default:
		return false
	}
}

// buildPostgresTableSchema builds a PostgresTableSchemaDescriptor for a DomainMessage.
func buildPostgresTableSchema(dm *DomainMessage, opts *Options) (*PostgresTableSchemaDescriptor, error) {
	meta := extractTableMeta(dm.ProtoMsg)
	if err := validateTableServiceMeta(dm.FullName, meta); err != nil {
		return nil, err
	}

	tableName := resolvePostgresTableName(dm.ProtoMsg, meta)
	if !isValidSQLIdentifier(tableName) {
		return nil, fmt.Errorf("message %s: invalid PostgreSQL table name %q (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)", dm.FullName, tableName)
	}
	if len(tableName) > maxPostgresIdentifierBytes {
		return nil, fmt.Errorf("message %s: PostgreSQL table name %q exceeds %d-byte identifier limit (%d bytes)",
			dm.FullName, tableName, maxPostgresIdentifierBytes, len(tableName))
	}

	explicitPKSet := make(map[string]bool)
	var explicitPKOrder []string
	if meta != nil {
		pkList := meta.PrimaryKeyFields
		if len(pkList) == 0 {
			pkList = meta.UniqueKeyColumns
		}
		for _, pk := range pkList {
			if pk != "" && !explicitPKSet[pk] {
				explicitPKSet[pk] = true
				explicitPKOrder = append(explicitPKOrder, pk)
			}
		}
	}

	type rawColInfo struct {
		colDesc PostgresColumnDescriptor
		colMeta *DorisColumnMetaIR
		fieldPK bool
	}

	var rawCols []rawColInfo
	colSet := make(map[string]bool)

	for _, f := range dm.Fields {
		if f.IsOneof {
			oneof := findOneof(dm, f.OneofTypeName)
			for _, v := range oneof.Variants {
				colMeta := v.ColumnMeta
				if colMeta == nil && v.ProtoField != nil {
					colMeta = extractColumnMeta(v.ProtoField)
				}
				colName := v.ProtoName
				if v.NameOverride != "" {
					if errMsg := validateFieldNameOverride(v.NameOverride); errMsg != "" {
						return nil, fmt.Errorf("oneof variant %s in %s: %s", v.Name, dm.FullName, errMsg)
					}
					colName = v.NameOverride
				}
				if colMeta != nil && colMeta.ColumnName != "" {
					if errMsg := validateFieldNameOverride(colMeta.ColumnName); errMsg != "" {
						return nil, fmt.Errorf("oneof variant %s in %s: %s", v.Name, dm.FullName, errMsg)
					}
					colName = colMeta.ColumnName
				}
				if !isValidSQLIdentifier(colName) {
					return nil, fmt.Errorf("oneof variant %s in %s: invalid SQL column name %q", v.Name, dm.FullName, colName)
				}
				if colSet[colName] {
					return nil, fmt.Errorf("oneof variant %s in %s: duplicate SQL column name %q", v.Name, dm.FullName, colName)
				}
				colSet[colName] = true
				if colMeta != nil && colMeta.DefaultValue != "" && !isValidDorisDefaultValue(colMeta.DefaultValue) {
					return nil, fmt.Errorf("oneof variant %s in %s: invalid default_value %q", v.Name, dm.FullName, colMeta.DefaultValue)
				}
				ddlType, hclType, err := resolveOneofVariantPostgresTypes(v, colMeta, opts)
				if err != nil {
					return nil, err
				}
				isVariantPK := colMeta != nil && (colMeta.PrimaryKey || colMeta.IsUniqueKey)
				nullable := true
				if isVariantPK || explicitPKSet[colName] {
					nullable = false
				} else if colMeta != nil && colMeta.Nullable != nil {
					nullable = *colMeta.Nullable
				}
				var defVal string
				if colMeta != nil && colMeta.DefaultValue != "" {
					defVal = colMeta.DefaultValue
				}
				rawCols = append(rawCols, rawColInfo{
					colDesc: PostgresColumnDescriptor{
						ColumnName:     colName,
						ProtoFieldName: v.ProtoName,
						SQLType:        ddlType,
						HCLType:        hclType,
						Nullable:       nullable,
						DefaultValue:   defVal,
					},
					colMeta: colMeta,
					fieldPK: isVariantPK,
				})
			}
			continue
		}

		var colMeta *DorisColumnMetaIR
		if f.ProtoField != nil {
			colMeta = extractColumnMeta(f.ProtoField)
		}

		colName := storageFieldName(f.Name)
		if f.DocID {
			colName = "id"
		}
		if f.NameOverride != "" {
			if errMsg := validateFieldNameOverride(f.NameOverride); errMsg != "" {
				return nil, fmt.Errorf("field %s in %s: %s", f.Name, dm.FullName, errMsg)
			}
			colName = f.NameOverride
		}
		if colMeta != nil && colMeta.ColumnName != "" {
			if errMsg := validateFieldNameOverride(colMeta.ColumnName); errMsg != "" {
				return nil, fmt.Errorf("field %s in %s: %s", f.Name, dm.FullName, errMsg)
			}
			colName = colMeta.ColumnName
		}

		if !isValidSQLIdentifier(colName) {
			return nil, fmt.Errorf("field %s in %s: invalid SQL column name %q (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)", f.Name, dm.FullName, colName)
		}
		if colSet[colName] {
			return nil, fmt.Errorf("field %s in %s: duplicate SQL column name %q", f.Name, dm.FullName, colName)
		}
		colSet[colName] = true

		if colMeta != nil && colMeta.DefaultValue != "" && !isValidDorisDefaultValue(colMeta.DefaultValue) {
			return nil, fmt.Errorf("field %s in %s: invalid default_value %q", f.Name, dm.FullName, colMeta.DefaultValue)
		}

		ddlType, hclType, err := resolvePostgresFieldTypes(f, colMeta, opts)
		if err != nil {
			return nil, err
		}

		isFieldPK := f.DocID || (colMeta != nil && (colMeta.PrimaryKey || colMeta.IsUniqueKey))
		var defVal, srvDef string
		if colMeta != nil && colMeta.DefaultValue != "" {
			defVal = colMeta.DefaultValue
		} else if f.ServerTimestamp && (ddlType == "TIMESTAMPTZ" || ddlType == "TIMESTAMP WITH TIME ZONE") {
			srvDef = "NOW()"
		}

		rawCols = append(rawCols, rawColInfo{
			colDesc: PostgresColumnDescriptor{
				ColumnName:     colName,
				ProtoFieldName: f.Name,
				SQLType:        ddlType,
				HCLType:        hclType,
				Nullable:       isPostgresFieldNullable(f, colMeta, isFieldPK || explicitPKSet[colName]),
				DefaultValue:   defVal,
				ServerDefault:  srvDef,
			},
			colMeta: colMeta,
			fieldPK: isFieldPK,
		})
	}

	// Determine final primary key list.
	var pkCols []string
	if len(explicitPKOrder) > 0 {
		for _, pk := range explicitPKOrder {
			if !colSet[pk] {
				return nil, fmt.Errorf("message %s: primary key references unknown column %q", dm.FullName, pk)
			}
			pkCols = append(pkCols, pk)
		}
	} else {
		for _, rc := range rawCols {
			if rc.fieldPK && !explicitPKSet[rc.colDesc.ColumnName] {
				explicitPKSet[rc.colDesc.ColumnName] = true
				pkCols = append(pkCols, rc.colDesc.ColumnName)
			}
		}
		if len(pkCols) == 0 && colSet["id"] {
			explicitPKSet["id"] = true
			pkCols = []string{"id"}
		}
	}

	// Finalize columns, ensuring all primary key columns are non-nullable.
	cols := make([]PostgresColumnDescriptor, len(rawCols))
	for i, rc := range rawCols {
		c := rc.colDesc
		if explicitPKSet[c.ColumnName] {
			c.Nullable = false
		}
		cols[i] = c
	}

	// Collect indexes and foreign keys in deterministic order.
	var indexes []PostgresIndexDescriptor
	seenIdx := make(map[string]PostgresIndexDescriptor)
	addIndex := func(idx PostgresIndexDescriptor) error {
		if idx.Name == "" {
			prefix := "idx_"
			if idx.Unique {
				prefix = "uq_"
			}
			idx.Name = prefix + tableName + "_" + strings.Join(idx.Columns, "_")
		}
		if !isValidSQLIdentifier(idx.Name) {
			return fmt.Errorf("message %s: invalid index name %q", dm.FullName, idx.Name)
		}
		if len(idx.Columns) == 0 {
			return fmt.Errorf("message %s: index %q has no columns", dm.FullName, idx.Name)
		}
		for _, col := range idx.Columns {
			if !colSet[col] {
				return fmt.Errorf("message %s: index %q references unknown column %q", dm.FullName, idx.Name, col)
			}
		}
		if prev, exists := seenIdx[idx.Name]; exists {
			if prev.Unique != idx.Unique || strings.Join(prev.Columns, ",") != strings.Join(idx.Columns, ",") {
				return fmt.Errorf("message %s: conflicting definitions for index %q", dm.FullName, idx.Name)
			}
			return nil
		}
		seenIdx[idx.Name] = idx
		indexes = append(indexes, idx)
		return nil
	}

	var fks []PostgresForeignKeyDescriptor
	for _, rc := range rawCols {
		cm := rc.colMeta
		if cm == nil {
			continue
		}
		colName := rc.colDesc.ColumnName
		if cm.Unique {
			if err := addIndex(PostgresIndexDescriptor{
				Name:    "uq_" + tableName + "_" + colName,
				Columns: []string{colName},
				Unique:  true,
			}); err != nil {
				return nil, err
			}
		}
		if cm.Index || cm.InvertedIndex {
			if err := addIndex(PostgresIndexDescriptor{
				Name:    "idx_" + tableName + "_" + colName,
				Columns: []string{colName},
				Unique:  false,
			}); err != nil {
				return nil, err
			}
		}
		if cm.ForeignKey != nil && cm.ForeignKey.RefTable != "" {
			refTable := cm.ForeignKey.RefTable
			if !isValidSQLIdentifier(refTable) {
				return nil, fmt.Errorf("field %s in %s: invalid foreign key ref_table %q", colName, dm.FullName, refTable)
			}
			refCol := cm.ForeignKey.RefColumn
			if refCol == "" {
				refCol = "id"
			}
			if !isValidSQLIdentifier(refCol) {
				return nil, fmt.Errorf("field %s in %s: invalid foreign key ref_column %q", colName, dm.FullName, refCol)
			}
			if _, _, ok := isValidForeignKeyAction(cm.ForeignKey.OnDelete); !ok {
				return nil, fmt.Errorf("field %s in %s: invalid foreign key on_delete %q", colName, dm.FullName, cm.ForeignKey.OnDelete)
			}
			if _, _, ok := isValidForeignKeyAction(cm.ForeignKey.OnUpdate); !ok {
				return nil, fmt.Errorf("field %s in %s: invalid foreign key on_update %q", colName, dm.FullName, cm.ForeignKey.OnUpdate)
			}
			fks = append(fks, PostgresForeignKeyDescriptor{
				Name:      "fk_" + tableName + "_" + colName,
				Columns:   []string{colName},
				RefTable:  refTable,
				RefColumn: refCol,
				OnDelete:  cm.ForeignKey.OnDelete,
				OnUpdate:  cm.ForeignKey.OnUpdate,
			})
		}
	}

	if meta != nil {
		for _, invCol := range meta.InvertedIndexColumns {
			if invCol == "" {
				continue
			}
			if err := addIndex(PostgresIndexDescriptor{
				Name:    "idx_" + tableName + "_" + invCol,
				Columns: []string{invCol},
				Unique:  false,
			}); err != nil {
				return nil, err
			}
		}
		for _, idxDef := range meta.Indexes {
			if err := addIndex(PostgresIndexDescriptor(idxDef)); err != nil {
				return nil, err
			}
		}
	}

	var writerService string
	var readerServices []string
	if meta != nil {
		writerService = meta.WriterService
		readerServices = append([]string(nil), meta.ReaderServices...)
	}

	return &PostgresTableSchemaDescriptor{
		GoName:         dm.Name,
		FullName:       dm.FullName,
		TableName:      tableName,
		WriterService:  writerService,
		ReaderServices: readerServices,
		PrimaryKeys:    pkCols,
		Columns:        cols,
		Indexes:        indexes,
		ForeignKeys:    fks,
	}, nil
}

// collectPostgresTableSchemas extracts all top-level and nested table messages from df in declaration order.
func collectPostgresTableSchemas(df *DomainFile, opts *Options) ([]*PostgresTableSchemaDescriptor, error) {
	embeddedMsgs := make(map[string]bool)
	var markEmbedded func(msgs []*DomainMessage)
	markEmbedded = func(msgs []*DomainMessage) {
		for _, m := range msgs {
			for _, f := range m.Fields {
				if f.Kind == FieldKindMessage && f.MessageTypeName != "" {
					embeddedMsgs[f.MessageTypeName] = true
				}
				if f.IsMap && f.MapValue != nil && f.MapValue.Kind == FieldKindMessage && f.MapValue.MessageTypeName != "" {
					embeddedMsgs[f.MapValue.MessageTypeName] = true
				}
				if f.IsOneof {
					oneof := findOneof(m, f.OneofTypeName)
					for _, v := range oneof.Variants {
						if v.Kind == FieldKindMessage && v.TypeName != "" {
							embeddedMsgs[v.TypeName] = true
						}
					}
				}
			}
			markEmbedded(m.NestedMessages)
		}
	}
	markEmbedded(df.Messages)

	var tables []*PostgresTableSchemaDescriptor
	seenTables := make(map[string]string)
	var collectMsgs func(msgs []*DomainMessage) error
	collectMsgs = func(msgs []*DomainMessage) error {
		for _, dm := range msgs {
			if dm.Skip {
				continue
			}
			meta := extractTableMeta(dm.ProtoMsg)
			if (meta != nil || !embeddedMsgs[dm.Name]) && len(dm.Fields) > 0 {
				tbl, err := buildPostgresTableSchema(dm, opts)
				if err != nil {
					return err
				}
				owner := dm.FullName
				if owner == "" {
					owner = dm.Name
				}
				if prev, exists := seenTables[tbl.TableName]; exists && prev != owner {
					return fmt.Errorf("message %s: duplicate PostgreSQL table name %q (already resolved by %s)", owner, tbl.TableName, prev)
				}
				seenTables[tbl.TableName] = owner
				tables = append(tables, tbl)
			}
			if err := collectMsgs(dm.NestedMessages); err != nil {
				return err
			}
		}
		return nil
	}
	if err := collectMsgs(df.Messages); err != nil {
		return nil, err
	}
	return tables, nil
}

// formatPostgresQuotedIdentList formats a slice of SQL column identifiers as a comma-separated
// double-quoted list (e.g. `"tenant_id", "id"`).
func formatPostgresQuotedIdentList(cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = fmt.Sprintf("%q", c)
	}
	return strings.Join(quoted, ", ")
}

// formatPostgresSQLDefaultValue formats the SQL DEFAULT expression for a PostgreSQL column.
func formatPostgresSQLDefaultValue(c PostgresColumnDescriptor) string {
	if c.ServerDefault != "" {
		return c.ServerDefault
	}
	if c.DefaultValue == "" {
		return ""
	}
	upperType := strings.ToUpper(c.SQLType)
	if upperType == "TEXT" ||
		strings.HasPrefix(upperType, "VARCHAR") ||
		strings.HasPrefix(upperType, "CHARACTER VARYING") ||
		upperType == "DATE" ||
		upperType == "TIMESTAMPTZ" ||
		upperType == "TIMESTAMP WITH TIME ZONE" ||
		upperType == "UUID" ||
		upperType == "JSONB" {
		return "'" + c.DefaultValue + "'"
	}
	return c.DefaultValue
}

// BuildPostgresCreateTableWithoutForeignKeysSQL renders a deterministic PostgreSQL
// CREATE TABLE IF NOT EXISTS statement (followed by any CREATE [UNIQUE] INDEX IF NOT EXISTS
// statements, without foreign key constraints) for t.
func (t PostgresTableSchemaDescriptor) BuildPostgresCreateTableWithoutForeignKeysSQL() string {
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

	var bodyLines []string
	for _, c := range t.Columns {
		if !isValidSQLIdentifier(c.ColumnName) || !isValidPostgresSQLTypeSyntax(c.SQLType) {
			return ""
		}
		line := fmt.Sprintf("    %q %s", c.ColumnName, c.SQLType)
		if !c.Nullable {
			line += " NOT NULL"
		}
		if def := formatPostgresSQLDefaultValue(c); def != "" {
			line += " DEFAULT " + def
		}
		bodyLines = append(bodyLines, line)
	}

	if len(t.PrimaryKeys) > 0 {
		bodyLines = append(bodyLines, fmt.Sprintf("    PRIMARY KEY (%s)", formatPostgresQuotedIdentList(t.PrimaryKeys)))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE IF NOT EXISTS %q (\n", t.TableName)
	b.WriteString(strings.Join(bodyLines, ",\n"))
	b.WriteString("\n);")

	for _, idx := range t.Indexes {
		b.WriteString("\n")
		if idx.Unique {
			fmt.Fprintf(&b, "CREATE UNIQUE INDEX IF NOT EXISTS %q ON %q (%s);",
				idx.Name, t.TableName, formatPostgresQuotedIdentList(idx.Columns))
		} else {
			fmt.Fprintf(&b, "CREATE INDEX IF NOT EXISTS %q ON %q (%s);",
				idx.Name, t.TableName, formatPostgresQuotedIdentList(idx.Columns))
		}
	}

	return b.String()
}

// BuildPostgresForeignKeysSQL renders ALTER TABLE ... ADD CONSTRAINT ... FOREIGN KEY
// statements for t so foreign keys can be applied after all tables are created.
func (t PostgresTableSchemaDescriptor) BuildPostgresForeignKeysSQL() string {
	if !isValidSQLIdentifier(t.TableName) || len(t.TableName) > maxPostgresIdentifierBytes || len(t.ForeignKeys) == 0 {
		return ""
	}
	var lines []string
	for _, fk := range t.ForeignKeys {
		if !isValidSQLIdentifier(fk.Name) || !isValidSQLIdentifier(fk.RefTable) || !isValidSQLIdentifier(fk.RefColumn) || len(fk.Columns) == 0 {
			return ""
		}
		fkLine := fmt.Sprintf("ALTER TABLE %q ADD CONSTRAINT %q FOREIGN KEY (%s) REFERENCES %q (%q)",
			t.TableName, fk.Name, formatPostgresQuotedIdentList(fk.Columns), fk.RefTable, fk.RefColumn)
		if _, sqlAct, ok := isValidForeignKeyAction(fk.OnDelete); ok && sqlAct != "" {
			fkLine += " ON DELETE " + sqlAct
		}
		if _, sqlAct, ok := isValidForeignKeyAction(fk.OnUpdate); ok && sqlAct != "" {
			fkLine += " ON UPDATE " + sqlAct
		}
		fkLine += ";"
		lines = append(lines, fkLine)
	}
	return strings.Join(lines, "\n")
}

// BuildPostgresCreateTableSQL renders a deterministic PostgreSQL CREATE TABLE IF NOT EXISTS
// statement (followed by any CREATE [UNIQUE] INDEX IF NOT EXISTS and ALTER TABLE ... FOREIGN KEY
// statements) for t.
func (t PostgresTableSchemaDescriptor) BuildPostgresCreateTableSQL() string {
	base := t.BuildPostgresCreateTableWithoutForeignKeysSQL()
	if base == "" {
		return ""
	}
	if fkSQL := t.BuildPostgresForeignKeysSQL(); fkSQL != "" {
		return base + "\n" + fkSQL
	}
	return base
}

// generatePostgresDDLFile emits <prefix>_postgres.sql for all PostgreSQL tables in file,
// emitting all CREATE TABLE and CREATE INDEX statements first followed by ALTER TABLE
// FOREIGN KEY constraints so forward table references succeed regardless of declaration order.
func generatePostgresDDLFile(gen *protogen.Plugin, file *protogen.File, tables []*PostgresTableSchemaDescriptor) error {
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
	g.P("-- backend: postgres (ddl)")
	g.P()

	emitted := 0
	for _, tbl := range tables {
		sql := tbl.BuildPostgresCreateTableWithoutForeignKeysSQL()
		if sql == "" {
			continue
		}
		if emitted > 0 {
			g.P()
		}
		emitted++
		g.P("-- ", tbl.GoName, " (", tbl.TableName, ")")
		if tbl.WriterService != "" {
			g.P("-- writer_service: ", tbl.WriterService)
		}
		if len(tbl.ReaderServices) > 0 {
			g.P("-- reader_services: ", strings.Join(tbl.ReaderServices, ", "))
		}
		g.P(sql)
	}

	var fkBlocks []string
	for _, tbl := range tables {
		if fkSQL := tbl.BuildPostgresForeignKeysSQL(); fkSQL != "" {
			fkBlocks = append(fkBlocks, fkSQL)
		}
	}
	if len(fkBlocks) > 0 {
		g.P()
		g.P("-- Foreign key constraints")
		g.P(strings.Join(fkBlocks, "\n"))
	}
	return nil
}
