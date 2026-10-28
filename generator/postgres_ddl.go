package generator

import (
	"crypto/sha256"
	"fmt"
	"regexp"
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

var (
	// validPostgresSQLTypeRe matches safe, standard PostgreSQL column type expressions
	// (including optional type parameters and trailing array dimensions), rejecting
	// injected DDL constraints such as "VARCHAR(10) REFERENCES other(id)".
	validPostgresSQLTypeRe = regexp.MustCompile(
		`^(?i)(?:` +
			`SMALLINT|INT2|INTEGER|INT|INT4|BIGINT|INT8|` +
			`SMALLSERIAL|SERIAL2|SERIAL|SERIAL4|BIGSERIAL|SERIAL8|` +
			`REAL|FLOAT4|DOUBLE PRECISION|FLOAT8|` +
			`BOOLEAN|BOOL|TEXT|CITEXT|BYTEA|DATE|UUID|JSON|JSONB|` +
			`INET|CIDR|MACADDR|MACADDR8|POINT|LINE|LSEG|BOX|PATH|POLYGON|CIRCLE|` +
			`XML|MONEY|TSVECTOR|TSQUERY|` +
			`INT4RANGE|INT8RANGE|NUMRANGE|TSRANGE|TSTZRANGE|DATERANGE|` +
			`VECTOR\([1-9][0-9]*\)|` +
			`(?:VARCHAR|CHARACTER VARYING|CHAR|CHARACTER|BPCHAR|BIT|BIT VARYING|VARBIT)(?:\([1-9][0-9]*\))?|` +
			`(?:NUMERIC|DECIMAL)(?:\([1-9][0-9]*(?:,\s*[0-9]+)?\))?|` +
			`(?:TIME|TIMESTAMP)(?:\([0-9]+\))?(?: WITH(?:OUT)? TIME ZONE)?|` +
			`(?:TIMETZ|TIMESTAMPTZ|INTERVAL)(?:\([0-9]+\))?` +
			`)(?:\[\])*$`,
	)

	// strictNumericLiteralRe matches a signed or unsigned integer or decimal literal without spaces.
	strictNumericLiteralRe = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)
)

// isValidPostgresSQLTypeSyntax reports whether s is a safe PostgreSQL column type expression
// matching the strict PostgreSQL type allowlist (e.g. "TEXT", "VARCHAR(64)", "NUMERIC(20,0)",
// "DOUBLE PRECISION", "TIMESTAMP WITH TIME ZONE", "TEXT[]", "UUID", "JSONB").
func isValidPostgresSQLTypeSyntax(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return false
	}
	return validPostgresSQLTypeRe.MatchString(s)
}

// isPostgresBooleanSQLType reports whether sqlType is a scalar PostgreSQL boolean type (not an array).
func isPostgresBooleanSQLType(sqlType string) bool {
	u := strings.ToUpper(strings.TrimSpace(sqlType))
	return u == "BOOLEAN" || u == "BOOL"
}

// isPostgresNumericSQLType reports whether sqlType is a scalar PostgreSQL numeric type (not an array).
func isPostgresNumericSQLType(sqlType string) bool {
	u := strings.ToUpper(strings.TrimSpace(sqlType))
	if strings.HasSuffix(u, "[]") {
		return false
	}
	switch u {
	case "SMALLINT", "INT2", "INTEGER", "INT", "INT4", "BIGINT", "INT8",
		"SMALLSERIAL", "SERIAL2", "SERIAL", "SERIAL4", "BIGSERIAL", "SERIAL8",
		"REAL", "FLOAT4", "DOUBLE PRECISION", "FLOAT8", "NUMERIC", "DECIMAL":
		return true
	}
	return strings.HasPrefix(u, "NUMERIC(") || strings.HasPrefix(u, "DECIMAL(")
}

// isPostgresTimestampSQLType reports whether sqlType is a PostgreSQL timestamp type
// eligible for a server_timestamp DEFAULT NOW() expression.
func isPostgresTimestampSQLType(sqlType string) bool {
	u := strings.ToUpper(strings.TrimSpace(sqlType))
	if strings.HasSuffix(u, "[]") {
		return false
	}
	return u == "TIMESTAMPTZ" ||
		u == "TIMESTAMP" ||
		u == "TIMESTAMP WITH TIME ZONE" ||
		u == "TIMESTAMP WITHOUT TIME ZONE" ||
		strings.HasPrefix(u, "TIMESTAMPTZ(") ||
		strings.HasPrefix(u, "TIMESTAMP(")
}

// isValidPostgresServerDefault reports whether s is an empty or allowlisted SQL server default expression.
func isValidPostgresServerDefault(s string) bool {
	if s == "" {
		return true
	}
	if strings.TrimSpace(s) != s {
		return false
	}
	switch strings.ToUpper(s) {
	case "NOW()", "CURRENT_TIMESTAMP", "GEN_RANDOM_UUID()":
		return true
	default:
		return false
	}
}

// validatePostgresDefaultValue validates that defaultValue is a safe literal for sqlType.
// Scalar boolean columns require "true" or "false" (case-insensitive); scalar numeric columns
// require a strict numeric literal (^-?[0-9]+(\.[0-9]+)?$); all other types require a safe
// printable ASCII literal without quotes, backslashes, or semicolons.
func validatePostgresDefaultValue(sqlType, defaultValue string) error {
	if defaultValue == "" {
		return nil
	}
	if !isValidDorisDefaultValue(defaultValue) {
		return fmt.Errorf("invalid default_value %q", defaultValue)
	}
	if isPostgresBooleanSQLType(sqlType) {
		lower := strings.ToLower(defaultValue)
		if lower != "true" && lower != "false" {
			return fmt.Errorf("invalid boolean default_value %q for %s (must be true or false)", defaultValue, sqlType)
		}
		return nil
	}
	if isPostgresNumericSQLType(sqlType) {
		if !strictNumericLiteralRe.MatchString(defaultValue) {
			return fmt.Errorf("invalid numeric default_value %q for %s", defaultValue, sqlType)
		}
		return nil
	}
	return nil
}

// compactPostgresIdentifier constructs "<prefix>_<body>" and, if it exceeds
// maxPostgresIdentifierBytes (63), deterministically compacts it to <= 63 bytes
// using a short 8-character SHA-256 hex suffix ("<prefix>_<truncated>_<hash8>").
func compactPostgresIdentifier(prefix, body string) string {
	full := prefix + "_" + body
	if len(full) <= maxPostgresIdentifierBytes {
		return full
	}
	sum := sha256.Sum256([]byte(full))
	hashSuffix := fmt.Sprintf("%x", sum[:4])
	maxBodyLen := maxPostgresIdentifierBytes - len(prefix) - 1 - 1 - len(hashSuffix)
	if maxBodyLen < 1 {
		return full[:maxPostgresIdentifierBytes]
	}
	truncated := strings.TrimRight(body[:maxBodyLen], "_")
	if truncated == "" {
		truncated = body[:maxBodyLen]
	}
	return prefix + "_" + truncated + "_" + hashSuffix
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
				if len(colName) > maxPostgresIdentifierBytes {
					return nil, fmt.Errorf("oneof variant %s in %s: SQL column name %q exceeds %d-byte identifier limit (%d bytes)",
						v.Name, dm.FullName, colName, maxPostgresIdentifierBytes, len(colName))
				}
				if colSet[colName] {
					return nil, fmt.Errorf("oneof variant %s in %s: duplicate SQL column name %q", v.Name, dm.FullName, colName)
				}
				colSet[colName] = true
				if colMeta != nil && colMeta.Length < 0 {
					return nil, fmt.Errorf("oneof variant %s in %s: column length cannot be negative (%d)", v.Name, dm.FullName, colMeta.Length)
				}
				ddlType, hclType, err := resolveOneofVariantPostgresTypes(v, colMeta, opts)
				if err != nil {
					return nil, err
				}
				if colMeta != nil && colMeta.DefaultValue != "" {
					if err := validatePostgresDefaultValue(ddlType, colMeta.DefaultValue); err != nil {
						return nil, fmt.Errorf("oneof variant %s in %s: %w", v.Name, dm.FullName, err)
					}
				}
				isVariantPK := colMeta != nil && (colMeta.PrimaryKey || colMeta.IsUniqueKey)
				isVariantServerTS := v.ProtoField != nil && isServerTimestamp(v.ProtoField)
				nullable := true
				if isVariantPK || explicitPKSet[colName] || (isVariantServerTS && isPostgresTimestampSQLType(ddlType)) {
					nullable = false
				} else if colMeta != nil && colMeta.Nullable != nil {
					nullable = *colMeta.Nullable
				}
				var defVal, srvDef string
				if colMeta != nil && colMeta.DefaultValue != "" {
					defVal = colMeta.DefaultValue
				} else if isVariantServerTS && isPostgresTimestampSQLType(ddlType) {
					srvDef = "NOW()"
				}
				rawCols = append(rawCols, rawColInfo{
					colDesc: PostgresColumnDescriptor{
						ColumnName:     colName,
						ProtoFieldName: v.ProtoName,
						SQLType:        ddlType,
						HCLType:        hclType,
						Nullable:       nullable,
						DefaultValue:   defVal,
						ServerDefault:  srvDef,
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
		if len(colName) > maxPostgresIdentifierBytes {
			return nil, fmt.Errorf("field %s in %s: SQL column name %q exceeds %d-byte identifier limit (%d bytes)",
				f.Name, dm.FullName, colName, maxPostgresIdentifierBytes, len(colName))
		}
		if colSet[colName] {
			return nil, fmt.Errorf("field %s in %s: duplicate SQL column name %q", f.Name, dm.FullName, colName)
		}
		colSet[colName] = true

		if colMeta != nil && colMeta.Length < 0 {
			return nil, fmt.Errorf("field %s in %s: column length cannot be negative (%d)", f.Name, dm.FullName, colMeta.Length)
		}

		ddlType, hclType, err := resolvePostgresFieldTypes(f, colMeta, opts)
		if err != nil {
			return nil, err
		}

		if colMeta != nil && colMeta.DefaultValue != "" {
			if err := validatePostgresDefaultValue(ddlType, colMeta.DefaultValue); err != nil {
				return nil, fmt.Errorf("field %s in %s: %w", f.Name, dm.FullName, err)
			}
		}

		isFieldPK := f.DocID || (colMeta != nil && (colMeta.PrimaryKey || colMeta.IsUniqueKey))
		var defVal, srvDef string
		if colMeta != nil && colMeta.DefaultValue != "" {
			defVal = colMeta.DefaultValue
		} else if f.ServerTimestamp && isPostgresTimestampSQLType(ddlType) {
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
	seenIdxCols := make(map[string]bool)
	seenAutoIdxPos := make(map[string]int)
	addIndex := func(idx PostgresIndexDescriptor, autoNamed bool) error {
		if idx.Name == "" {
			autoNamed = true
			prefix := "idx"
			if idx.Unique {
				prefix = "uq"
			}
			idx.Name = compactPostgresIdentifier(prefix, tableName+"_"+strings.Join(idx.Columns, "_"))
		} else if autoNamed {
			prefix := "idx"
			if idx.Unique {
				prefix = "uq"
			}
			idx.Name = compactPostgresIdentifier(prefix, tableName+"_"+strings.Join(idx.Columns, "_"))
		}
		if !isValidSQLIdentifier(idx.Name) {
			return fmt.Errorf("message %s: invalid index name %q", dm.FullName, idx.Name)
		}
		if len(idx.Name) > maxPostgresIdentifierBytes {
			return fmt.Errorf("message %s: index name %q exceeds %d-byte identifier limit (%d bytes)",
				dm.FullName, idx.Name, maxPostgresIdentifierBytes, len(idx.Name))
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
		// Skip auto-generated unique index if it duplicates a single-column PRIMARY KEY.
		if autoNamed && idx.Unique && len(idx.Columns) == 1 && len(pkCols) == 1 && idx.Columns[0] == pkCols[0] {
			return nil
		}
		colSig := fmt.Sprintf("%t:%s", idx.Unique, strings.Join(idx.Columns, ","))
		if autoNamed {
			if seenIdxCols[colSig] {
				return nil
			}
			seenAutoIdxPos[colSig] = len(indexes)
		} else if pos, hasAuto := seenAutoIdxPos[colSig]; hasAuto {
			delete(seenIdx, indexes[pos].Name)
			delete(seenAutoIdxPos, colSig)
			indexes[pos] = idx
			seenIdx[idx.Name] = idx
			return nil
		}
		seenIdx[idx.Name] = idx
		seenIdxCols[colSig] = true
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
				Columns: []string{colName},
				Unique:  true,
			}, true); err != nil {
				return nil, err
			}
		}
		if cm.Index || cm.InvertedIndex {
			if err := addIndex(PostgresIndexDescriptor{
				Columns: []string{colName},
				Unique:  false,
			}, true); err != nil {
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
			if len(refCol) > maxPostgresIdentifierBytes {
				return nil, fmt.Errorf("field %s in %s: foreign key ref_column %q exceeds %d-byte identifier limit (%d bytes)",
					colName, dm.FullName, refCol, maxPostgresIdentifierBytes, len(refCol))
			}
			if _, _, ok := isValidForeignKeyAction(cm.ForeignKey.OnDelete); !ok {
				return nil, fmt.Errorf("field %s in %s: invalid foreign key on_delete %q", colName, dm.FullName, cm.ForeignKey.OnDelete)
			}
			if _, _, ok := isValidForeignKeyAction(cm.ForeignKey.OnUpdate); !ok {
				return nil, fmt.Errorf("field %s in %s: invalid foreign key on_update %q", colName, dm.FullName, cm.ForeignKey.OnUpdate)
			}
			fks = append(fks, PostgresForeignKeyDescriptor{
				Name:      compactPostgresIdentifier("fk", tableName+"_"+colName),
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
				Columns: []string{invCol},
				Unique:  false,
			}, true); err != nil {
				return nil, err
			}
		}
		for _, idxDef := range meta.Indexes {
			if err := addIndex(PostgresIndexDescriptor(idxDef), idxDef.Name == ""); err != nil {
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

// collectPostgresTableSchemas extracts all top-level and explicitly table-annotated nested
// messages from df in declaration order, resolving intra-schema foreign key RefTable prefixes.
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
	byResolvedName := make(map[string]string)
	byUnprefixedSnake := make(map[string][]string)
	byGoName := make(map[string][]string)

	addAlias := func(m map[string][]string, key, tableName string) {
		if key == "" {
			return
		}
		for _, existing := range m[key] {
			if existing == tableName {
				return
			}
		}
		m[key] = append(m[key], tableName)
	}

	var collectMsgs func(msgs []*DomainMessage, isNested bool) error
	collectMsgs = func(msgs []*DomainMessage, isNested bool) error {
		for _, dm := range msgs {
			if dm.Skip {
				continue
			}
			meta := extractTableMeta(dm.ProtoMsg)
			isEligible := (!isNested && (meta != nil || !embeddedMsgs[dm.Name])) || (isNested && meta != nil)
			if isEligible && len(dm.Fields) > 0 {
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
				byResolvedName[tbl.TableName] = tbl.TableName
				addAlias(byUnprefixedSnake, toSnakeCase(dm.Name), tbl.TableName)
				if dm.ProtoMsg != nil {
					addAlias(byUnprefixedSnake, toSnakeCase(dm.ProtoMsg.GoIdent.GoName), tbl.TableName)
					leafName := string(dm.ProtoMsg.Desc.Name())
					addAlias(byUnprefixedSnake, toSnakeCase(leafName), tbl.TableName)
					addAlias(byGoName, leafName, tbl.TableName)
				}
				if tbl.WriterService != "" {
					normWriter := strings.ReplaceAll(strings.TrimSpace(tbl.WriterService), "-", "_")
					if rest, ok := strings.CutPrefix(tbl.TableName, normWriter+"__"); ok && rest != "" {
						addAlias(byUnprefixedSnake, rest, tbl.TableName)
					}
				}
				addAlias(byGoName, dm.Name, tbl.TableName)
				tables = append(tables, tbl)
			}
			if err := collectMsgs(dm.NestedMessages, true); err != nil {
				return err
			}
		}
		return nil
	}
	if err := collectMsgs(df.Messages, false); err != nil {
		return nil, err
	}

	// Post-pass: resolve foreign key RefTable references to physical prefixed table names
	// when referencing another table in the schema, and validate the 63-byte limit.
	for _, tbl := range tables {
		for i := range tbl.ForeignKeys {
			fk := &tbl.ForeignKeys[i]
			if byResolvedName[fk.RefTable] == "" {
				snakeRef := toSnakeCase(fk.RefTable)
				if tbl.WriterService != "" {
					normWriter := strings.ReplaceAll(strings.TrimSpace(tbl.WriterService), "-", "_")
					candidate := normWriter + "__" + snakeRef
					if byResolvedName[candidate] != "" {
						fk.RefTable = candidate
					}
				}
				if byResolvedName[fk.RefTable] == "" {
					matches := byUnprefixedSnake[snakeRef]
					if len(matches) == 0 {
						matches = byGoName[fk.RefTable]
					}
					if len(matches) > 1 {
						return nil, fmt.Errorf("table %s: foreign key %q has ambiguous unprefixed ref_table %q (matches physical tables: %s)",
							tbl.TableName, fk.Name, fk.RefTable, strings.Join(matches, ", "))
					}
					if len(matches) == 1 {
						fk.RefTable = matches[0]
					}
				}
			}
			if len(fk.RefTable) > maxPostgresIdentifierBytes {
				return nil, fmt.Errorf("table %s: foreign key %q resolved ref_table %q exceeds %d-byte identifier limit (%d bytes)",
					tbl.TableName, fk.Name, fk.RefTable, maxPostgresIdentifierBytes, len(fk.RefTable))
			}
		}
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
// Boolean defaults are normalized to TRUE/FALSE, numeric defaults are emitted as validated
// numeric literals, and all other defaults (including strings, dates, timestamps, intervals,
// network addresses, UUIDs, JSONB, and arrays) are single-quoted SQL literals.
func formatPostgresSQLDefaultValue(c PostgresColumnDescriptor) string {
	if c.ServerDefault != "" {
		return strings.ToUpper(strings.TrimSpace(c.ServerDefault))
	}
	if c.DefaultValue == "" {
		return ""
	}
	if isPostgresBooleanSQLType(c.SQLType) {
		return strings.ToUpper(c.DefaultValue)
	}
	if isPostgresNumericSQLType(c.SQLType) {
		return c.DefaultValue
	}
	return "'" + strings.ReplaceAll(c.DefaultValue, "'", "''") + "'"
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
		if !isValidSQLIdentifier(c.ColumnName) || len(c.ColumnName) > maxPostgresIdentifierBytes || !isValidPostgresSQLTypeSyntax(c.SQLType) {
			return ""
		}
		if !isValidPostgresServerDefault(c.ServerDefault) || validatePostgresDefaultValue(c.SQLType, c.DefaultValue) != nil {
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
		for _, pk := range t.PrimaryKeys {
			if !isValidSQLIdentifier(pk) || len(pk) > maxPostgresIdentifierBytes {
				return ""
			}
		}
		bodyLines = append(bodyLines, fmt.Sprintf("    PRIMARY KEY (%s)", formatPostgresQuotedIdentList(t.PrimaryKeys)))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE IF NOT EXISTS %q (\n", t.TableName)
	b.WriteString(strings.Join(bodyLines, ",\n"))
	b.WriteString("\n);")

	for _, idx := range t.Indexes {
		if !isValidSQLIdentifier(idx.Name) || len(idx.Name) > maxPostgresIdentifierBytes || len(idx.Columns) == 0 {
			return ""
		}
		for _, col := range idx.Columns {
			if !isValidSQLIdentifier(col) || len(col) > maxPostgresIdentifierBytes {
				return ""
			}
		}
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
		delAct, delSQL, ok := isValidForeignKeyAction(fk.OnDelete)
		if !ok {
			return ""
		}
		_ = delAct
		updAct, updSQL, ok := isValidForeignKeyAction(fk.OnUpdate)
		if !ok {
			return ""
		}
		_ = updAct
		fkLine := fmt.Sprintf("ALTER TABLE %q ADD CONSTRAINT %q FOREIGN KEY (%s) REFERENCES %q (%q)",
			t.TableName, fk.Name, formatPostgresQuotedIdentList(fk.Columns), fk.RefTable, fk.RefColumn)
		if delSQL != "" {
			fkLine += " ON DELETE " + delSQL
		}
		if updSQL != "" {
			fkLine += " ON UPDATE " + updSQL
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
