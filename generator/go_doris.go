package generator

import (
	"fmt"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// dorisColumnIR holds the intermediate representation of a single flattened
// Doris/Iceberg column on a table message.
type dorisColumnIR struct {
	Field          *protogen.Field
	ParentChain    []*protogen.Field
	GoFieldName    string
	GoFieldType    string
	BaseScalarType string
	ColDesc        DorisColumnDescriptor
	IsTimestamp    bool
	IsDate         bool
	ArrayKind      string // "", "string", "float32", "float64", "int32", "int64", "bool"
	IsEnum         bool
	IsStringEnum   bool
	EnumPrefix     string
	IsPointer      bool
	IsUint64       bool
}

func (c *dorisColumnIR) isArray() bool {
	return c.ArrayKind != ""
}

// dorisTableIR holds the intermediate representation of a protobuf message
// annotated with table_meta / (proto2type.message).table.
type dorisTableIR struct {
	Msg        *protogen.Message
	GoName     string
	Meta       *DorisTableMetaIR
	Columns    []*dorisColumnIR
	Descriptor DorisTableDescriptor
}

// isWellKnownDate returns true if the field is google.type.Date.
func isWellKnownDate(field *protogen.Field) bool {
	if field == nil || field.Desc.Kind() != protoreflect.MessageKind || field.Desc.Message() == nil {
		return false
	}
	return string(field.Desc.Message().FullName()) == "google.type.Date"
}

// protoEnumPrefix computes the canonical UPPER_SNAKE_ prefix for a protobuf enum descriptor.
func protoEnumPrefix(ed protoreflect.EnumDescriptor) string {
	if ed == nil {
		return ""
	}
	if ed.Values().Len() > 0 {
		zeroName := string(ed.Values().Get(0).Name())
		if strings.HasSuffix(zeroName, "_UNSPECIFIED") {
			return strings.TrimSuffix(zeroName, "UNSPECIFIED")
		}
	}
	return strings.ToUpper(toSnakeCase(string(ed.Name()))) + "_"
}

// generateGoDoris generates Go Doris storage structs, SQL scanners, proto
// converters, and/or Doris & Iceberg SQL DDL files for a proto file.
func (gg *goGenerator) generateGoDoris(gen *protogen.Plugin, file *protogen.File, opts *Options) error {
	switch opts.DorisMode {
	case "", "go", "ddl", "both":
	default:
		return fmt.Errorf("unknown doris_mode: %q (must be '', 'go', 'ddl', or 'both')", opts.DorisMode)
	}

	tables, err := buildDorisTablesForFile(file, opts)
	if err != nil {
		return err
	}
	// Skip file generation cleanly if no messages in this .proto file define Lakehouse tables.
	if len(tables) == 0 {
		return nil
	}

	emitGo := opts.DorisMode == "" || opts.DorisMode == "go" || opts.DorisMode == "both"
	emitDDL := opts.DorisMode == "ddl" || opts.DorisMode == "both"

	if emitGo {
		if err := gg.generateGoDorisFile(gen, file, opts, tables); err != nil {
			return err
		}
	}
	if emitDDL {
		if err := generateDorisDDLFiles(gen, file, tables); err != nil {
			return err
		}
	}
	return nil
}

// buildDorisTablesForFile extracts all table-annotated messages from file in declaration order.
func buildDorisTablesForFile(file *protogen.File, opts *Options) ([]*dorisTableIR, error) {
	var tables []*dorisTableIR
	for _, msg := range file.Messages {
		if isMessageSkipped(msg) {
			continue
		}
		meta := extractDorisTableMeta(msg)
		if meta == nil {
			continue
		}
		if meta.TableName == "" {
			return nil, fmt.Errorf("message %s: table_meta has empty table_name", msg.Desc.FullName())
		}
		tbl, err := buildDorisTableIR(msg, meta, opts)
		if err != nil {
			return nil, err
		}
		tables = append(tables, tbl)
	}
	return tables, nil
}

func validateDorisTableMeta(msg *protogen.Message, meta *DorisTableMetaIR) error {
	fullName := msg.Desc.FullName()
	if !isValidSQLIdentifier(meta.TableName) {
		return fmt.Errorf("message %s: invalid SQL identifier for table_name %q", fullName, meta.TableName)
	}
	if !isValidDatabaseScope(meta.DatabaseScope) {
		return fmt.Errorf("message %s: unsupported database_scope %q", fullName, meta.DatabaseScope)
	}
	if !isValidSyncTier(meta.SyncTier) {
		return fmt.Errorf("message %s: unsupported sync_tier %q", fullName, meta.SyncTier)
	}
	if meta.ColocateGroup != "" && !isValidSQLIdentifier(meta.ColocateGroup) {
		return fmt.Errorf("message %s: invalid SQL identifier for colocate_group %q", fullName, meta.ColocateGroup)
	}
	if meta.SequenceCol != "" && !isValidSQLIdentifier(meta.SequenceCol) {
		return fmt.Errorf("message %s: invalid SQL identifier for sequence_col %q", fullName, meta.SequenceCol)
	}
	if meta.DistributedByColumn != "" && !isValidSQLIdentifier(meta.DistributedByColumn) {
		return fmt.Errorf("message %s: invalid SQL identifier for distributed_by_column %q", fullName, meta.DistributedByColumn)
	}
	checkList := func(label string, items []string) error {
		for _, item := range items {
			if !isValidSQLIdentifier(item) {
				return fmt.Errorf("message %s: invalid SQL identifier %q in %s", fullName, item, label)
			}
		}
		return nil
	}
	if err := checkList("unique_key_columns", meta.UniqueKeyColumns); err != nil {
		return err
	}
	if err := checkList("partition_by_columns", meta.PartitionByColumns); err != nil {
		return err
	}
	if err := checkList("inverted_index_columns", meta.InvertedIndexColumns); err != nil {
		return err
	}
	if err := checkList("ngram_bf_columns", meta.NgramBfColumns); err != nil {
		return err
	}
	if err := checkList("bloom_filter_columns", meta.BloomFilterColumns); err != nil {
		return err
	}
	if err := checkList("duplicate_key_columns", meta.DuplicateKeyColumns); err != nil {
		return err
	}
	return nil
}

// messageHasAnnotatedDorisColumns reports whether msg or any of its inline_nested
// children has at least one field with explicit Doris column_meta.
func messageHasAnnotatedDorisColumns(msg *protogen.Message, visited map[protoreflect.FullName]bool) bool {
	if msg == nil || visited[msg.Desc.FullName()] {
		return false
	}
	visited[msg.Desc.FullName()] = true
	for _, f := range msg.Fields {
		if isFieldSkipped(f) {
			continue
		}
		if cm := extractDorisColumnMeta(f); cm != nil {
			return true
		}
	}
	return false
}

// buildDorisTableIR constructs the flattened column list and DorisTableDescriptor for msg.
func buildDorisTableIR(msg *protogen.Message, meta *DorisTableMetaIR, opts *Options) (*dorisTableIR, error) {
	if err := validateDorisTableMeta(msg, meta); err != nil {
		return nil, err
	}

	ukSet := make(map[string]bool, len(meta.UniqueKeyColumns))
	for _, uk := range meta.UniqueKeyColumns {
		ukSet[uk] = true
	}
	partSet := make(map[string]bool, len(meta.PartitionByColumns))
	for _, pk := range meta.PartitionByColumns {
		partSet[pk] = true
	}

	hasAnnotatedCols := messageHasAnnotatedDorisColumns(msg, make(map[protoreflect.FullName]bool))

	// Collect root non-inline Go field names so inline_nested fields with the same
	// leaf name automatically prefix with their parent field name.
	rootGoNames := make(map[string]bool)
	for _, f := range msg.Fields {
		if isFieldSkipped(f) {
			continue
		}
		cm := extractDorisColumnMeta(f)
		if cm != nil && cm.InlineNested {
			continue
		}
		if hasAnnotatedCols && cm == nil {
			continue
		}
		rootGoNames[toPascalCase(string(f.Desc.Name()))] = true
	}

	usedGoNames := make(map[string]bool)
	usedColNames := make(map[string]bool)
	var cols []*dorisColumnIR

	var walkFields func(fields []*protogen.Field, chain []*protogen.Field, visited map[protoreflect.FullName]bool, parentHasAnnotated bool) error
	walkFields = func(fields []*protogen.Field, chain []*protogen.Field, visited map[protoreflect.FullName]bool, parentHasAnnotated bool) error {
		for _, field := range fields {
			if isFieldSkipped(field) {
				continue
			}
			colMeta := extractDorisColumnMeta(field)
			if colMeta != nil && colMeta.InlineNested {
				if field.Desc.Kind() != protoreflect.MessageKind || field.Desc.IsList() || field.Desc.IsMap() || field.Message == nil {
					return fmt.Errorf("field %s: inline_nested is only valid on singular message fields", field.Desc.FullName())
				}
				nestedFullName := field.Message.Desc.FullName()
				if visited[nestedFullName] {
					return fmt.Errorf("field %s: recursive inline_nested cycle detected on %s", field.Desc.FullName(), nestedFullName)
				}
				nextVisited := make(map[protoreflect.FullName]bool, len(visited)+1)
				for k, v := range visited {
					nextVisited[k] = v
				}
				nextVisited[nestedFullName] = true
				nextChain := append(append([]*protogen.Field(nil), chain...), field)
				nestedHasAnnotated := messageHasAnnotatedDorisColumns(field.Message, make(map[protoreflect.FullName]bool))
				if err := walkFields(field.Message.Fields, nextChain, nextVisited, nestedHasAnnotated); err != nil {
					return err
				}
				continue
			}

			// When a message uses explicit column_meta annotations, skip unannotated
			// read-time hydrated fields (such as repeated sub-messages or derived booleans).
			if parentHasAnnotated && colMeta == nil {
				continue
			}

			// Even on a table message without column_meta annotations, skip unannotated
			// map or non-WKT message fields. If explicitly annotated without inline_nested, return an error.
			if field.Desc.IsMap() || (field.Desc.Kind() == protoreflect.MessageKind && !isWellKnownTimestamp(field) && !isWellKnownDate(field)) {
				if colMeta != nil {
					return fmt.Errorf("field %s: message or map field cannot be a Doris column without inline_nested", field.Desc.FullName())
				}
				continue
			}

			colIR, err := buildDorisColumnIR(field, chain, colMeta, meta, ukSet, partSet, rootGoNames, usedGoNames, usedColNames, opts)
			if err != nil {
				return err
			}
			cols = append(cols, colIR)
		}
		return nil
	}

	rootVisited := map[protoreflect.FullName]bool{
		msg.Desc.FullName(): true,
	}
	if err := walkFields(msg.Fields, nil, rootVisited, hasAnnotatedCols); err != nil {
		return nil, err
	}

	colDescs := make([]DorisColumnDescriptor, len(cols))
	for i, c := range cols {
		colDescs[i] = c.ColDesc
	}

	uks := append([]string(nil), meta.UniqueKeyColumns...)
	if len(uks) == 0 {
		for _, c := range cols {
			if c.ColDesc.IsUniqueKey {
				uks = append(uks, c.ColDesc.ColumnName)
			}
		}
	}

	partCols := append([]string(nil), meta.PartitionByColumns...)
	if len(partCols) == 0 {
		for _, c := range cols {
			if c.ColDesc.IsPartitionKey {
				partCols = append(partCols, c.ColDesc.ColumnName)
			}
		}
	}

	seqCol := meta.SequenceCol
	if seqCol == "" {
		for _, c := range cols {
			if c.ColDesc.IsSequenceCol {
				seqCol = c.ColDesc.ColumnName
				break
			}
		}
	}

	seenInv := make(map[string]bool)
	var invCols []string
	for _, c := range cols {
		if c.ColDesc.InvertedIndex && !seenInv[c.ColDesc.ColumnName] {
			seenInv[c.ColDesc.ColumnName] = true
			invCols = append(invCols, c.ColDesc.ColumnName)
		}
	}
	for _, ic := range meta.InvertedIndexColumns {
		if ic != "" && !seenInv[ic] {
			seenInv[ic] = true
			invCols = append(invCols, ic)
		}
	}

	desc := DorisTableDescriptor{
		TableName:            meta.TableName,
		MessageName:          msg.GoIdent.GoName,
		DatabaseScope:        meta.DatabaseScope,
		SyncTier:             meta.SyncTier,
		UniqueKeyColumns:     uks,
		PartitionByColumns:   partCols,
		ColocateGroup:        meta.ColocateGroup,
		SequenceCol:          seqCol,
		InvertedIndexColumns: invCols,
		NgramBfColumns:       append([]string(nil), meta.NgramBfColumns...),
		BloomFilterColumns:   append([]string(nil), meta.BloomFilterColumns...),
		DistributedByColumn:  meta.DistributedByColumn,
		DuplicateKeyColumns:  append([]string(nil), meta.DuplicateKeyColumns...),
		FileCacheTTLSeconds:  meta.FileCacheTTLSeconds,
		Columns:              colDescs,
	}

	return &dorisTableIR{
		Msg:        msg,
		GoName:     msg.GoIdent.GoName,
		Meta:       meta,
		Columns:    cols,
		Descriptor: desc,
	}, nil
}

// buildDorisColumnIR constructs the dorisColumnIR for a single leaf proto field.
func buildDorisColumnIR(
	field *protogen.Field,
	chain []*protogen.Field,
	colMeta *DorisColumnMetaIR,
	tableMeta *DorisTableMetaIR,
	ukSet map[string]bool,
	partSet map[string]bool,
	rootGoNames map[string]bool,
	usedGoNames map[string]bool,
	usedColNames map[string]bool,
	opts *Options,
) (*dorisColumnIR, error) {
	if colMeta == nil {
		colMeta = &DorisColumnMetaIR{}
	}

	protoFieldName := string(field.Desc.Name())
	colName := protoFieldName
	if override := fieldNameOverride(field); override != "" {
		if errMsg := validateFieldNameOverride(override); errMsg != "" {
			return nil, fmt.Errorf("field %s: %s", field.Desc.FullName(), errMsg)
		}
		colName = override
	}
	if colMeta.ColumnName != "" {
		if errMsg := validateFieldNameOverride(colMeta.ColumnName); errMsg != "" {
			return nil, fmt.Errorf("field %s: %s", field.Desc.FullName(), errMsg)
		}
		colName = colMeta.ColumnName
	}
	if !isValidSQLIdentifier(colName) {
		return nil, fmt.Errorf("field %s: invalid SQL column name %q (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)", field.Desc.FullName(), colName)
	}
	if usedColNames[colName] {
		return nil, fmt.Errorf("field %s: duplicate SQL column name %q", field.Desc.FullName(), colName)
	}
	usedColNames[colName] = true

	if colMeta.DorisType != "" && !isValidSQLTypeSyntax(colMeta.DorisType) {
		return nil, fmt.Errorf("field %s: invalid doris_type %q", field.Desc.FullName(), colMeta.DorisType)
	}
	if colMeta.IcebergType != "" && !isValidSQLTypeSyntax(colMeta.IcebergType) {
		return nil, fmt.Errorf("field %s: invalid iceberg_type %q", field.Desc.FullName(), colMeta.IcebergType)
	}

	goFieldName := toPascalCase(protoFieldName)
	if len(chain) > 0 && (usedGoNames[goFieldName] || rootGoNames[goFieldName]) {
		for i := len(chain) - 1; i >= 0; i-- {
			parentPrefix := toPascalCase(string(chain[i].Desc.Name()))
			goFieldName = parentPrefix + goFieldName
			if !usedGoNames[goFieldName] && !rootGoNames[goFieldName] {
				break
			}
		}
	}
	if usedGoNames[goFieldName] {
		goFieldName = toPascalCase(colName)
	}
	usedGoNames[goFieldName] = true

	isUK := colMeta.IsUniqueKey || ukSet[colName]
	isPK := colMeta.IsPartitionKey || partSet[colName]
	isSeq := colMeta.IsSequenceCol || (tableMeta.SequenceCol != "" && tableMeta.SequenceCol == colName)

	isTS := isWellKnownTimestamp(field)
	isDt := isWellKnownDate(field)
	isList := field.Desc.IsList()
	isEnum := field.Desc.Kind() == protoreflect.EnumKind

	dorisType := colMeta.DorisType
	if dorisType == "" {
		dorisType = defaultDorisSQLType(field, colMeta, opts)
	}
	icebergType := colMeta.IcebergType
	if icebergType == "" {
		icebergType = dorisColumnIcebergDDLType(dorisType, "")
	}

	upperDoris := strings.ToUpper(dorisType)
	upperIceberg := strings.ToUpper(icebergType)
	stripPrefix := colMeta.StripEnumPrefix ||
		(isEnum && (strings.Contains(upperDoris, "VARCHAR") || strings.Contains(upperDoris, "STRING") || strings.Contains(upperIceberg, "STRING")))
	isStrEnum := isEnum && (stripPrefix || isEnumAsString(field, opts) || strings.Contains(upperDoris, "VARCHAR") || strings.Contains(upperDoris, "STRING"))

	nullable := isDorisColumnNullable(field, colName, isUK, isPK, isSeq)

	var (
		goFieldType    string
		baseScalarType string
		arrayKind      string
		isPtr          bool
		isUint64       bool
		enumPrefix     string
	)

	if isEnum {
		enumPrefix = protoEnumPrefix(field.Desc.Enum())
	}

	usePtrForScalar := field.Desc.HasOptionalKeyword()

	switch {
	case isList:
		switch field.Desc.Kind() {
		case protoreflect.StringKind, protoreflect.EnumKind:
			goFieldType = "DorisStringArray"
			arrayKind = "string"
		case protoreflect.FloatKind:
			goFieldType = "DorisFloat32Array"
			arrayKind = "float32"
		case protoreflect.DoubleKind:
			goFieldType = "DorisFloat64Array"
			arrayKind = "float64"
		case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
			goFieldType = "DorisInt32Array"
			arrayKind = "int32"
		case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
			protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
			goFieldType = "DorisInt64Array"
			arrayKind = "int64"
		case protoreflect.BoolKind:
			goFieldType = "DorisBoolArray"
			arrayKind = "bool"
		default:
			return nil, fmt.Errorf("field %s: unsupported repeated field kind %v for Doris backend", field.Desc.FullName(), field.Desc.Kind())
		}
	case isTS:
		if usePtrForScalar {
			goFieldType = "*time.Time"
			isPtr = true
		} else {
			goFieldType = "time.Time"
		}
	case isDt:
		goFieldType = "string"
		baseScalarType = "string"
	case isEnum:
		if isStrEnum {
			baseScalarType = "string"
			if usePtrForScalar {
				goFieldType = "*string"
				isPtr = true
			} else {
				goFieldType = "string"
			}
		} else {
			baseScalarType = "int32"
			if usePtrForScalar {
				goFieldType = "*int32"
				isPtr = true
			} else {
				goFieldType = "int32"
			}
		}
	default:
		switch field.Desc.Kind() {
		case protoreflect.BoolKind,
			protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
			protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
			protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
			protoreflect.Uint64Kind, protoreflect.Fixed64Kind,
			protoreflect.FloatKind, protoreflect.DoubleKind,
			protoreflect.StringKind, protoreflect.BytesKind:
		default:
			return nil, fmt.Errorf("field %s: unsupported field kind %v for Doris backend", field.Desc.FullName(), field.Desc.Kind())
		}
		if field.Desc.Kind() == protoreflect.Uint64Kind || field.Desc.Kind() == protoreflect.Fixed64Kind {
			isUint64 = true
		}
		baseScalarType = goType(field.Desc.Kind())
		if usePtrForScalar && field.Desc.Kind() != protoreflect.BytesKind {
			goFieldType = "*" + baseScalarType
			isPtr = true
		} else {
			goFieldType = baseScalarType
		}
	}

	colDesc := DorisColumnDescriptor{
		ColumnName:      colName,
		ProtoFieldName:  protoFieldName,
		DorisType:       dorisType,
		IcebergType:     icebergType,
		Nullable:        nullable,
		InvertedIndex:   colMeta.InvertedIndex,
		IsUniqueKey:     isUK,
		IsSequenceCol:   isSeq,
		IsPartitionKey:  isPK,
		StripEnumPrefix: stripPrefix,
		DefaultValue:    colMeta.DefaultValue,
	}

	return &dorisColumnIR{
		Field:          field,
		ParentChain:    append([]*protogen.Field(nil), chain...),
		GoFieldName:    goFieldName,
		GoFieldType:    goFieldType,
		BaseScalarType: baseScalarType,
		ColDesc:        colDesc,
		IsTimestamp:    isTS,
		IsDate:         isDt,
		ArrayKind:      arrayKind,
		IsEnum:         isEnum,
		IsStringEnum:   isStrEnum,
		EnumPrefix:     enumPrefix,
		IsPointer:      isPtr,
		IsUint64:       isUint64,
	}, nil
}

// isDorisColumnNullable determines whether a column allows SQL NULLs.
func isDorisColumnNullable(field *protogen.Field, colName string, isUK, isPK, isSeq bool) bool {
	if isUK || isPK || isSeq {
		return false
	}
	if field.Desc.IsList() {
		return false
	}
	if field.Desc.HasOptionalKeyword() {
		return true
	}
	if field.Desc.Kind() == protoreflect.MessageKind {
		// Well-known message types (google.type.Date, google.protobuf.Timestamp) are
		// nullable in Doris unless marked as required or audit timestamp columns.
		switch colName {
		case "updated_at", "created_at", "occurred_at", "ingested_at", "derived_at", "received_at",
			"sent_at", "applied_at", "evaluated_at", "summary_updated_at":
			return false
		}
		if vc := extractValidateConstraints(field); vc != nil && vc.Required {
			return false
		}
		return true
	}
	return false
}

// defaultDorisSQLType infers a default Apache Doris 4.x SQL column type when
// doris_type is not explicitly specified on column_meta.
func defaultDorisSQLType(field *protogen.Field, colMeta *DorisColumnMetaIR, opts *Options) string {
	if field.Desc.IsList() {
		switch field.Desc.Kind() {
		case protoreflect.FloatKind:
			return "ARRAY<FLOAT>"
		case protoreflect.DoubleKind:
			return "ARRAY<DOUBLE>"
		case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
			return "ARRAY<INT>"
		case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
			protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
			return "ARRAY<BIGINT>"
		case protoreflect.BoolKind:
			return "ARRAY<BOOLEAN>"
		default:
			return "ARRAY<VARCHAR(64)>"
		}
	}
	if isWellKnownTimestamp(field) {
		return "DATETIMEV2(6)"
	}
	if isWellKnownDate(field) {
		return "DATEV2"
	}
	switch field.Desc.Kind() {
	case protoreflect.BoolKind:
		return "BOOLEAN"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "INT"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return "BIGINT"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "LARGEINT"
	case protoreflect.FloatKind:
		return "FLOAT"
	case protoreflect.DoubleKind:
		return "DOUBLE"
	case protoreflect.EnumKind:
		if (colMeta != nil && colMeta.StripEnumPrefix) || isEnumAsString(field, opts) {
			return "VARCHAR(64)"
		}
		return "INT"
	default:
		return "VARCHAR(256)"
	}
}

// generateGoDorisFile emits <prefix>_doris.type.go for the given proto file and tables.
func (gg *goGenerator) generateGoDorisFile(
	gen *protogen.Plugin,
	file *protogen.File,
	opts *Options,
	tables []*dorisTableIR,
) error {
	filename, err := outputFilename(file.GeneratedFilenamePrefix, "_doris.type.go")
	if err != nil {
		return err
	}

	goImportPath := file.GoImportPath
	goPackageName := file.GoPackageName
	if opts.GoPackage != "" {
		importPath, pkgName := parseGoPackage(opts.GoPackage)
		goImportPath = protogen.GoImportPath(importPath)
		goPackageName = protogen.GoPackageName(pkgName)
		filename = adjustSubdirFilename(filename, string(file.GoImportPath), importPath)
	}

	g := gen.NewGeneratedFile(filename, goImportPath)

	// Header
	g.P("// Code generated by proto2type. DO NOT EDIT.")
	g.P("// source: ", file.Desc.Path())
	g.P("// backend: doris")
	g.P()
	g.P("package ", goPackageName)
	g.P()

	// Emit shared package-level Doris runtime types and helpers once per Go package.
	if !gg.emittedDorisTypes[goImportPath] {
		gg.emittedDorisTypes[goImportPath] = true
		emitDorisSharedRuntimeTypes(g)
	}

	// Emit google.type.Date helpers once per Go package if any table in this file uses google.type.Date.
	if dateIdent, ok := findDateGoIdent(tables); ok && !gg.emittedDorisDateHelpers[goImportPath] {
		gg.emittedDorisDateHelpers[goImportPath] = true
		emitDorisDateHelpers(g, dateIdent)
	}

	// Emit each table's descriptor function, struct, SQL methods, and proto converters.
	for _, tbl := range tables {
		emitDorisTableCode(g, tbl)
	}

	// Emit file-level table registry variable: var <FilePascalPrefix>DorisTables = []DorisTableDescriptor{ ... }
	baseName := strings.TrimSuffix(filepath.Base(file.Desc.Path()), ".proto")
	registryName := toPascalCase(baseName) + "DorisTables"
	g.P("// ", registryName, " registers all Apache Doris / Iceberg table descriptors defined in ", file.Desc.Path(), ".")
	g.P("var ", registryName, " = []DorisTableDescriptor{")
	for _, tbl := range tables {
		g.P("\t", tbl.GoName, "DorisTable(),")
	}
	g.P("}")
	g.P()

	return nil
}

// findDateGoIdent returns the protogen.GoIdent for google.type.Date if any column in tables uses it.
func findDateGoIdent(tables []*dorisTableIR) (protogen.GoIdent, bool) {
	for _, t := range tables {
		for _, c := range t.Columns {
			if c.IsDate && c.Field.Message != nil {
				return c.Field.Message.GoIdent, true
			}
		}
	}
	return protogen.GoIdent{}, false
}

// emitDorisSharedRuntimeTypes emits the package-level types and helper functions
// required by generated Doris structs. Guarded by gg.emittedDorisTypes[goImportPath].
func emitDorisSharedRuntimeTypes(g *protogen.GeneratedFile) {
	sqlNullString := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "database/sql", GoName: "NullString"})
	sqlNullTime := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "database/sql", GoName: "NullTime"})
	sqlNullInt64 := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "database/sql", GoName: "NullInt64"})
	sqlScanner := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "database/sql", GoName: "Scanner"})
	driverValue := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "database/sql/driver", GoName: "Value"})
	driverValuer := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "database/sql/driver", GoName: "Valuer"})
	driverDefaultConv := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "database/sql/driver", GoName: "DefaultParameterConverter"})
	jsonMarshal := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "encoding/json", GoName: "Marshal"})
	jsonUnmarshal := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "encoding/json", GoName: "Unmarshal"})
	fmtErrorf := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "fmt", GoName: "Errorf"})
	fmtSprintf := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "fmt", GoName: "Sprintf"})
	mathIsNaN := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "math", GoName: "IsNaN"})
	mathIsInf := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "math", GoName: "IsInf"})
	mathMaxInt64 := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "math", GoName: "MaxInt64"})
	strconvItoa := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strconv", GoName: "Itoa"})
	strconvFormatUint := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strconv", GoName: "FormatUint"})
	strconvParseUint := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strconv", GoName: "ParseUint"})
	stringsBuilder := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "Builder"})
	stringsHasPrefix := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "HasPrefix"})
	stringsHasSuffix := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "HasSuffix"})
	stringsJoin := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "Join"})
	stringsReplaceAll := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "ReplaceAll"})
	stringsToUpper := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "ToUpper"})
	stringsTrimPrefix := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "TrimPrefix"})
	stringsTrimSuffix := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "TrimSuffix"})
	stringsTrimSpace := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "TrimSpace"})
	timeTime := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "time", GoName: "Time"})
	timeDate := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "time", GoName: "Date"})
	timeMonth := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "time", GoName: "Month"})
	timeParse := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "time", GoName: "Parse"})
	timeParseInLocation := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "time", GoName: "ParseInLocation"})
	timeUTC := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "time", GoName: "UTC"})
	timeRFC3339 := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "time", GoName: "RFC3339"})
	timeRFC3339Nano := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "time", GoName: "RFC3339Nano"})

	g.P("// DorisScanner is implemented by *sql.Row and *sql.Rows.")
	g.P("type DorisScanner interface {")
	g.P("\tScan(dest ...any) error")
	g.P("}")
	g.P()

	// parseDorisSingleQuotedArray helper for Doris ['a', 'b'] wire format.
	g.P("func parseDorisSingleQuotedArray(s string) ([]string, error) {")
	g.P("\ts = ", stringsTrimSpace, "(s)")
	g.P("\tif len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"invalid Doris array literal: %q\", s)")
	g.P("\t}")
	g.P("\tinner := ", stringsTrimSpace, "(s[1 : len(s)-1])")
	g.P("\tif inner == \"\" {")
	g.P("\t\treturn []string{}, nil")
	g.P("\t}")
	g.P("\tvar out []string")
	g.P("\ti := 0")
	g.P("\tfor i < len(inner) {")
	g.P("\t\tfor i < len(inner) && (inner[i] == ' ' || inner[i] == '\\t' || inner[i] == '\\n' || inner[i] == '\\r') {")
	g.P("\t\t\ti++")
	g.P("\t\t}")
	g.P("\t\tif i >= len(inner) {")
	g.P("\t\t\tbreak")
	g.P("\t\t}")
	g.P("\t\tif inner[i] == '\\'' {")
	g.P("\t\t\ti++")
	g.P("\t\t\tvar elem ", stringsBuilder)
	g.P("\t\t\tclosed := false")
	g.P("\t\t\tfor i < len(inner) {")
	g.P("\t\t\t\tch := inner[i]")
	g.P("\t\t\t\tif ch == '\\\\' && i+1 < len(inner) {")
	g.P("\t\t\t\t\tnext := inner[i+1]")
	g.P("\t\t\t\t\tswitch next {")
	g.P("\t\t\t\t\tcase 'n':")
	g.P("\t\t\t\t\t\telem.WriteByte('\\n')")
	g.P("\t\t\t\t\tcase 'r':")
	g.P("\t\t\t\t\t\telem.WriteByte('\\r')")
	g.P("\t\t\t\t\tcase 't':")
	g.P("\t\t\t\t\t\telem.WriteByte('\\t')")
	g.P("\t\t\t\t\tdefault:")
	g.P("\t\t\t\t\t\telem.WriteByte(next)")
	g.P("\t\t\t\t\t}")
	g.P("\t\t\t\t\ti += 2")
	g.P("\t\t\t\t\tcontinue")
	g.P("\t\t\t\t}")
	g.P("\t\t\t\tif ch == '\\'' {")
	g.P("\t\t\t\t\tif i+1 < len(inner) && inner[i+1] == '\\'' {")
	g.P("\t\t\t\t\t\telem.WriteByte('\\'')")
	g.P("\t\t\t\t\t\ti += 2")
	g.P("\t\t\t\t\t\tcontinue")
	g.P("\t\t\t\t\t}")
	g.P("\t\t\t\t\ti++")
	g.P("\t\t\t\t\tclosed = true")
	g.P("\t\t\t\t\tbreak")
	g.P("\t\t\t\t}")
	g.P("\t\t\t\telem.WriteByte(ch)")
	g.P("\t\t\t\ti++")
	g.P("\t\t\t}")
	g.P("\t\t\tif !closed {")
	g.P("\t\t\t\treturn nil, ", fmtErrorf, "(\"unterminated single-quoted string in Doris array: %q\", s)")
	g.P("\t\t\t}")
	g.P("\t\t\tout = append(out, elem.String())")
	g.P("\t\t} else if i+4 <= len(inner) && (inner[i:i+4] == \"null\" || inner[i:i+4] == \"NULL\") {")
	g.P("\t\t\ti += 4")
	g.P("\t\t\tout = append(out, \"\")")
	g.P("\t\t} else {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"unexpected token in Doris array: %q\", s)")
	g.P("\t\t}")
	g.P("\t\tfor i < len(inner) && (inner[i] == ' ' || inner[i] == '\\t' || inner[i] == '\\n' || inner[i] == '\\r') {")
	g.P("\t\t\ti++")
	g.P("\t\t}")
	g.P("\t\tif i < len(inner) {")
	g.P("\t\t\tif inner[i] != ',' {")
	g.P("\t\t\t\treturn nil, ", fmtErrorf, "(\"expected comma in Doris array: %q\", s)")
	g.P("\t\t\t}")
	g.P("\t\t\ti++")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn out, nil")
	g.P("}")
	g.P()

	g.P("// DorisStringArray represents an Apache Doris ARRAY<VARCHAR(N)> column,")
	g.P("// serializing as a JSON string array for driver.Valuer and parsing JSON")
	g.P("// or single-quoted array literals in sql.Scanner.")
	g.P("type DorisStringArray []string")
	g.P()
	g.P("// Value implements driver.Valuer.")
	g.P("func (a DorisStringArray) Value() (", driverValue, ", error) {")
	g.P("\tif len(a) == 0 {")
	g.P("\t\treturn \"[]\", nil")
	g.P("\t}")
	g.P("\tb, err := ", jsonMarshal, "([]string(a))")
	g.P("\tif err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\treturn string(b), nil")
	g.P("}")
	g.P()
	g.P("// Scan implements sql.Scanner.")
	g.P("func (a *DorisStringArray) Scan(src any) error {")
	g.P("\tif src == nil {")
	g.P("\t\t*a = nil")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tvar s string")
	g.P("\tswitch v := src.(type) {")
	g.P("\tcase []byte:")
	g.P("\t\ts = string(v)")
	g.P("\tcase string:")
	g.P("\t\ts = v")
	g.P("\tcase DorisStringArray:")
	g.P("\t\tcp := make(DorisStringArray, len(v))")
	g.P("\t\tcopy(cp, v)")
	g.P("\t\t*a = cp")
	g.P("\t\treturn nil")
	g.P("\tcase []string:")
	g.P("\t\tcp := make(DorisStringArray, len(v))")
	g.P("\t\tcopy(cp, v)")
	g.P("\t\t*a = cp")
	g.P("\t\treturn nil")
	g.P("\tdefault:")
	g.P("\t\treturn ", fmtErrorf, "(\"DorisStringArray.Scan: unsupported type %T\", src)")
	g.P("\t}")
	g.P("\ts = ", stringsTrimSpace, "(s)")
	g.P("\tif s == \"\" || s == \"[]\" || s == \"null\" || s == \"NULL\" {")
	g.P("\t\t*a = DorisStringArray{}")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tvar out []string")
	g.P("\tif err := ", jsonUnmarshal, "([]byte(s), &out); err != nil {")
	g.P("\t\tparsed, err2 := parseDorisSingleQuotedArray(s)")
	g.P("\t\tif err2 != nil {")
	g.P("\t\t\treturn err")
	g.P("\t\t}")
	g.P("\t\tout = parsed")
	g.P("\t}")
	g.P("\t*a = DorisStringArray(out)")
	g.P("\treturn nil")
	g.P("}")
	g.P()

	g.P("// DorisFloat32Array represents an Apache Doris ARRAY<FLOAT> column,")
	g.P("// serializing as a JSON float array for driver.Valuer and parsing JSON")
	g.P("// array literals in sql.Scanner.")
	g.P("type DorisFloat32Array []float32")
	g.P()
	g.P("// Value implements driver.Valuer.")
	g.P("func (a DorisFloat32Array) Value() (", driverValue, ", error) {")
	g.P("\tif len(a) == 0 {")
	g.P("\t\treturn \"[]\", nil")
	g.P("\t}")
	g.P("\tfor i, v := range a {")
	g.P("\t\tif ", mathIsNaN, "(float64(v)) || ", mathIsInf, "(float64(v), 0) {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"DorisFloat32Array.Value: NaN or Inf at index %d is not supported\", i)")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tb, err := ", jsonMarshal, "([]float32(a))")
	g.P("\tif err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\treturn string(b), nil")
	g.P("}")
	g.P()
	g.P("// Scan implements sql.Scanner.")
	g.P("func (a *DorisFloat32Array) Scan(src any) error {")
	g.P("\tif src == nil {")
	g.P("\t\t*a = nil")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tvar s string")
	g.P("\tswitch v := src.(type) {")
	g.P("\tcase []byte:")
	g.P("\t\ts = string(v)")
	g.P("\tcase string:")
	g.P("\t\ts = v")
	g.P("\tcase DorisFloat32Array:")
	g.P("\t\tcp := make(DorisFloat32Array, len(v))")
	g.P("\t\tcopy(cp, v)")
	g.P("\t\t*a = cp")
	g.P("\t\treturn nil")
	g.P("\tcase []float32:")
	g.P("\t\tcp := make(DorisFloat32Array, len(v))")
	g.P("\t\tcopy(cp, v)")
	g.P("\t\t*a = cp")
	g.P("\t\treturn nil")
	g.P("\tdefault:")
	g.P("\t\treturn ", fmtErrorf, "(\"DorisFloat32Array.Scan: unsupported type %T\", src)")
	g.P("\t}")
	g.P("\ts = ", stringsTrimSpace, "(s)")
	g.P("\tif s == \"\" || s == \"[]\" || s == \"null\" || s == \"NULL\" {")
	g.P("\t\t*a = DorisFloat32Array{}")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tvar ptrs []*float32")
	g.P("\tif err := ", jsonUnmarshal, "([]byte(s), &ptrs); err != nil {")
	g.P("\t\treturn err")
	g.P("\t}")
	g.P("\tout := make(DorisFloat32Array, len(ptrs))")
	g.P("\tfor i, p := range ptrs {")
	g.P("\t\tif p == nil {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"DorisFloat32Array.Scan: null element at index %d is not supported\", i)")
	g.P("\t\t}")
	g.P("\t\tout[i] = *p")
	g.P("\t}")
	g.P("\t*a = out")
	g.P("\treturn nil")
	g.P("}")
	g.P()

	g.P("// DorisFloat64Array represents an Apache Doris ARRAY<DOUBLE> column.")
	g.P("type DorisFloat64Array []float64")
	g.P()
	g.P("// Value implements driver.Valuer.")
	g.P("func (a DorisFloat64Array) Value() (", driverValue, ", error) {")
	g.P("\tif len(a) == 0 {")
	g.P("\t\treturn \"[]\", nil")
	g.P("\t}")
	g.P("\tfor i, v := range a {")
	g.P("\t\tif ", mathIsNaN, "(v) || ", mathIsInf, "(v, 0) {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"DorisFloat64Array.Value: NaN or Inf at index %d is not supported\", i)")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tb, err := ", jsonMarshal, "([]float64(a))")
	g.P("\tif err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\treturn string(b), nil")
	g.P("}")
	g.P()
	g.P("// Scan implements sql.Scanner.")
	g.P("func (a *DorisFloat64Array) Scan(src any) error {")
	g.P("\tif src == nil {")
	g.P("\t\t*a = nil")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tvar s string")
	g.P("\tswitch v := src.(type) {")
	g.P("\tcase []byte:")
	g.P("\t\ts = string(v)")
	g.P("\tcase string:")
	g.P("\t\ts = v")
	g.P("\tcase DorisFloat64Array:")
	g.P("\t\tcp := make(DorisFloat64Array, len(v))")
	g.P("\t\tcopy(cp, v)")
	g.P("\t\t*a = cp")
	g.P("\t\treturn nil")
	g.P("\tcase []float64:")
	g.P("\t\tcp := make(DorisFloat64Array, len(v))")
	g.P("\t\tcopy(cp, v)")
	g.P("\t\t*a = cp")
	g.P("\t\treturn nil")
	g.P("\tdefault:")
	g.P("\t\treturn ", fmtErrorf, "(\"DorisFloat64Array.Scan: unsupported type %T\", src)")
	g.P("\t}")
	g.P("\ts = ", stringsTrimSpace, "(s)")
	g.P("\tif s == \"\" || s == \"[]\" || s == \"null\" || s == \"NULL\" {")
	g.P("\t\t*a = DorisFloat64Array{}")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tvar ptrs []*float64")
	g.P("\tif err := ", jsonUnmarshal, "([]byte(s), &ptrs); err != nil {")
	g.P("\t\treturn err")
	g.P("\t}")
	g.P("\tout := make(DorisFloat64Array, len(ptrs))")
	g.P("\tfor i, p := range ptrs {")
	g.P("\t\tif p == nil {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"DorisFloat64Array.Scan: null element at index %d is not supported\", i)")
	g.P("\t\t}")
	g.P("\t\tout[i] = *p")
	g.P("\t}")
	g.P("\t*a = out")
	g.P("\treturn nil")
	g.P("}")
	g.P()

	g.P("// DorisInt32Array represents an Apache Doris ARRAY<INT> column.")
	g.P("type DorisInt32Array []int32")
	g.P()
	g.P("// Value implements driver.Valuer.")
	g.P("func (a DorisInt32Array) Value() (", driverValue, ", error) {")
	g.P("\tif len(a) == 0 {")
	g.P("\t\treturn \"[]\", nil")
	g.P("\t}")
	g.P("\tb, err := ", jsonMarshal, "([]int32(a))")
	g.P("\tif err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\treturn string(b), nil")
	g.P("}")
	g.P()
	g.P("// Scan implements sql.Scanner.")
	g.P("func (a *DorisInt32Array) Scan(src any) error {")
	g.P("\tif src == nil {")
	g.P("\t\t*a = nil")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tvar s string")
	g.P("\tswitch v := src.(type) {")
	g.P("\tcase []byte:")
	g.P("\t\ts = string(v)")
	g.P("\tcase string:")
	g.P("\t\ts = v")
	g.P("\tcase DorisInt32Array:")
	g.P("\t\tcp := make(DorisInt32Array, len(v))")
	g.P("\t\tcopy(cp, v)")
	g.P("\t\t*a = cp")
	g.P("\t\treturn nil")
	g.P("\tcase []int32:")
	g.P("\t\tcp := make(DorisInt32Array, len(v))")
	g.P("\t\tcopy(cp, v)")
	g.P("\t\t*a = cp")
	g.P("\t\treturn nil")
	g.P("\tdefault:")
	g.P("\t\treturn ", fmtErrorf, "(\"DorisInt32Array.Scan: unsupported type %T\", src)")
	g.P("\t}")
	g.P("\ts = ", stringsTrimSpace, "(s)")
	g.P("\tif s == \"\" || s == \"[]\" || s == \"null\" || s == \"NULL\" {")
	g.P("\t\t*a = DorisInt32Array{}")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tvar ptrs []*int32")
	g.P("\tif err := ", jsonUnmarshal, "([]byte(s), &ptrs); err != nil {")
	g.P("\t\treturn err")
	g.P("\t}")
	g.P("\tout := make(DorisInt32Array, len(ptrs))")
	g.P("\tfor i, p := range ptrs {")
	g.P("\t\tif p == nil {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"DorisInt32Array.Scan: null element at index %d is not supported\", i)")
	g.P("\t\t}")
	g.P("\t\tout[i] = *p")
	g.P("\t}")
	g.P("\t*a = out")
	g.P("\treturn nil")
	g.P("}")
	g.P()

	g.P("// DorisInt64Array represents an Apache Doris ARRAY<BIGINT> column.")
	g.P("type DorisInt64Array []int64")
	g.P()
	g.P("// Value implements driver.Valuer.")
	g.P("func (a DorisInt64Array) Value() (", driverValue, ", error) {")
	g.P("\tif len(a) == 0 {")
	g.P("\t\treturn \"[]\", nil")
	g.P("\t}")
	g.P("\tb, err := ", jsonMarshal, "([]int64(a))")
	g.P("\tif err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\treturn string(b), nil")
	g.P("}")
	g.P()
	g.P("// Scan implements sql.Scanner.")
	g.P("func (a *DorisInt64Array) Scan(src any) error {")
	g.P("\tif src == nil {")
	g.P("\t\t*a = nil")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tvar s string")
	g.P("\tswitch v := src.(type) {")
	g.P("\tcase []byte:")
	g.P("\t\ts = string(v)")
	g.P("\tcase string:")
	g.P("\t\ts = v")
	g.P("\tcase DorisInt64Array:")
	g.P("\t\tcp := make(DorisInt64Array, len(v))")
	g.P("\t\tcopy(cp, v)")
	g.P("\t\t*a = cp")
	g.P("\t\treturn nil")
	g.P("\tcase []int64:")
	g.P("\t\tcp := make(DorisInt64Array, len(v))")
	g.P("\t\tcopy(cp, v)")
	g.P("\t\t*a = cp")
	g.P("\t\treturn nil")
	g.P("\tdefault:")
	g.P("\t\treturn ", fmtErrorf, "(\"DorisInt64Array.Scan: unsupported type %T\", src)")
	g.P("\t}")
	g.P("\ts = ", stringsTrimSpace, "(s)")
	g.P("\tif s == \"\" || s == \"[]\" || s == \"null\" || s == \"NULL\" {")
	g.P("\t\t*a = DorisInt64Array{}")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tvar ptrs []*int64")
	g.P("\tif err := ", jsonUnmarshal, "([]byte(s), &ptrs); err != nil {")
	g.P("\t\treturn err")
	g.P("\t}")
	g.P("\tout := make(DorisInt64Array, len(ptrs))")
	g.P("\tfor i, p := range ptrs {")
	g.P("\t\tif p == nil {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"DorisInt64Array.Scan: null element at index %d is not supported\", i)")
	g.P("\t\t}")
	g.P("\t\tout[i] = *p")
	g.P("\t}")
	g.P("\t*a = out")
	g.P("\treturn nil")
	g.P("}")
	g.P()

	g.P("// DorisBoolArray represents an Apache Doris ARRAY<BOOLEAN> column.")
	g.P("type DorisBoolArray []bool")
	g.P()
	g.P("// Value implements driver.Valuer.")
	g.P("func (a DorisBoolArray) Value() (", driverValue, ", error) {")
	g.P("\tif len(a) == 0 {")
	g.P("\t\treturn \"[]\", nil")
	g.P("\t}")
	g.P("\tb, err := ", jsonMarshal, "([]bool(a))")
	g.P("\tif err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\treturn string(b), nil")
	g.P("}")
	g.P()
	g.P("// Scan implements sql.Scanner.")
	g.P("func (a *DorisBoolArray) Scan(src any) error {")
	g.P("\tif src == nil {")
	g.P("\t\t*a = nil")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tvar s string")
	g.P("\tswitch v := src.(type) {")
	g.P("\tcase []byte:")
	g.P("\t\ts = string(v)")
	g.P("\tcase string:")
	g.P("\t\ts = v")
	g.P("\tcase DorisBoolArray:")
	g.P("\t\tcp := make(DorisBoolArray, len(v))")
	g.P("\t\tcopy(cp, v)")
	g.P("\t\t*a = cp")
	g.P("\t\treturn nil")
	g.P("\tcase []bool:")
	g.P("\t\tcp := make(DorisBoolArray, len(v))")
	g.P("\t\tcopy(cp, v)")
	g.P("\t\t*a = cp")
	g.P("\t\treturn nil")
	g.P("\tdefault:")
	g.P("\t\treturn ", fmtErrorf, "(\"DorisBoolArray.Scan: unsupported type %T\", src)")
	g.P("\t}")
	g.P("\ts = ", stringsTrimSpace, "(s)")
	g.P("\tif s == \"\" || s == \"[]\" || s == \"null\" || s == \"NULL\" {")
	g.P("\t\t*a = DorisBoolArray{}")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tvar ptrs []*bool")
	g.P("\tif err := ", jsonUnmarshal, "([]byte(s), &ptrs); err != nil {")
	g.P("\t\treturn err")
	g.P("\t}")
	g.P("\tout := make(DorisBoolArray, len(ptrs))")
	g.P("\tfor i, p := range ptrs {")
	g.P("\t\tif p == nil {")
	g.P("\t\t\treturn ", fmtErrorf, "(\"DorisBoolArray.Scan: null element at index %d is not supported\", i)")
	g.P("\t\t}")
	g.P("\t\tout[i] = *p")
	g.P("\t}")
	g.P("\t*a = out")
	g.P("\treturn nil")
	g.P("}")
	g.P()

	g.P("// DorisColumnDescriptor describes a single physical column in an Apache Doris")
	g.P("// and Apache Iceberg lakehouse table.")
	g.P("type DorisColumnDescriptor struct {")
	g.P("\tColumnName      string")
	g.P("\tProtoFieldName  string")
	g.P("\tDorisType       string")
	g.P("\tIcebergType     string")
	g.P("\tNullable        bool")
	g.P("\tInvertedIndex   bool")
	g.P("\tIsUniqueKey     bool")
	g.P("\tIsSequenceCol   bool")
	g.P("\tIsPartitionKey  bool")
	g.P("\tStripEnumPrefix bool")
	g.P("\tDefaultValue    string")
	g.P("}")
	g.P()

	g.P("// CanonicalIcebergSpecType returns the canonical Apache Iceberg specification")
	g.P("// type (e.g. \"STRING\", \"LONG\", \"INT\", \"TIMESTAMPTZ\", \"DATE\", \"LIST<STRING>\").")
	g.P("func (c DorisColumnDescriptor) CanonicalIcebergSpecType() string {")
	g.P("\tit := ", stringsTrimSpace, "(c.IcebergType)")
	g.P("\tif it != \"\" {")
	g.P("\t\treturn it")
	g.P("\t}")
	g.P("\tdt := ", stringsToUpper, "(", stringsTrimSpace, "(c.DorisType))")
	g.P("\tswitch dt {")
	g.P("\tcase \"DATEV2\", \"DATE\":")
	g.P("\t\treturn \"DATE\"")
	g.P("\tcase \"DATETIMEV2(6)\", \"DATETIMEV2(3)\", \"DATETIMEV2\", \"DATETIME\":")
	g.P("\t\treturn \"TIMESTAMPTZ\"")
	g.P("\tcase \"BIGINT\", \"LARGEINT\":")
	g.P("\t\treturn \"LONG\"")
	g.P("\tcase \"SMALLINT\", \"TINYINT\", \"INT\":")
	g.P("\t\treturn \"INT\"")
	g.P("\tcase \"BOOLEAN\":")
	g.P("\t\treturn \"BOOLEAN\"")
	g.P("\tcase \"FLOAT\":")
	g.P("\t\treturn \"FLOAT\"")
	g.P("\tcase \"DOUBLE\":")
	g.P("\t\treturn \"DOUBLE\"")
	g.P("\t}")
	g.P("\tif ", stringsHasPrefix, "(dt, \"VARCHAR\") || dt == \"TEXT\" || dt == \"STRING\" {")
	g.P("\t\treturn \"STRING\"")
	g.P("\t}")
	g.P("\tif ", stringsHasPrefix, "(dt, \"DECIMALV3(\") {")
	g.P("\t\treturn \"DECIMAL(\" + ", stringsTrimPrefix, "(dt, \"DECIMALV3(\")")
	g.P("\t}")
	g.P("\tif ", stringsHasPrefix, "(dt, \"ARRAY<\") && ", stringsHasSuffix, "(dt, \">\") {")
	g.P("\t\tinner := ", stringsTrimSuffix, "(", stringsTrimPrefix, "(dt, \"ARRAY<\"), \">\")")
	g.P("\t\tif ", stringsHasPrefix, "(inner, \"VARCHAR\") || inner == \"TEXT\" || inner == \"STRING\" {")
	g.P("\t\t\treturn \"LIST<STRING>\"")
	g.P("\t\t}")
	g.P("\t\tif inner == \"BIGINT\" || inner == \"LARGEINT\" {")
	g.P("\t\t\treturn \"LIST<LONG>\"")
	g.P("\t\t}")
	g.P("\t\treturn \"LIST<\" + inner + \">\"")
	g.P("\t}")
	g.P("\treturn dt")
	g.P("}")
	g.P()

	g.P("// DorisTableDescriptor describes an Apache Doris and Apache Iceberg table")
	g.P("// generated from a protobuf message annotated with table_meta.")
	g.P("type DorisTableDescriptor struct {")
	g.P("\tTableName            string")
	g.P("\tMessageName          string")
	g.P("\tDatabaseScope        string")
	g.P("\tSyncTier             string")
	g.P("\tUniqueKeyColumns     []string")
	g.P("\tPartitionByColumns   []string")
	g.P("\tColocateGroup        string")
	g.P("\tSequenceCol          string")
	g.P("\tInvertedIndexColumns []string")
	g.P("\tNgramBfColumns       []string")
	g.P("\tBloomFilterColumns   []string")
	g.P("\tDistributedByColumn  string")
	g.P("\tDuplicateKeyColumns  []string")
	g.P("\tFileCacheTTLSeconds  int32")
	g.P("\tColumns              []DorisColumnDescriptor")
	g.P("}")
	g.P()

	g.P("func isValidDorisSQLIdentifier(s string) bool {")
	g.P("\tif len(s) == 0 {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tfor i := 0; i < len(s); i++ {")
	g.P("\t\tc := s[i]")
	g.P("\t\tisAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'")
	g.P("\t\tisDigit := c >= '0' && c <= '9'")
	g.P("\t\tif i == 0 && !isAlpha {")
	g.P("\t\t\treturn false")
	g.P("\t\t}")
	g.P("\t\tif !isAlpha && !isDigit {")
	g.P("\t\t\treturn false")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn true")
	g.P("}")
	g.P()

	g.P("func isValidDorisSQLTypeSyntax(s string) bool {")
	g.P("\ts = ", stringsTrimSpace, "(s)")
	g.P("\tif len(s) == 0 {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tfor i := 0; i < len(s); i++ {")
	g.P("\t\tc := s[i]")
	g.P("\t\tok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||")
	g.P("\t\t\tc == '_' || c == '<' || c == '>' || c == ',' || c == ' ' || c == '(' || c == ')'")
	g.P("\t\tif !ok {")
	g.P("\t\t\treturn false")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn true")
	g.P("}")
	g.P()

	g.P("func sanitizeDorisTenantSlug(tenantSlug string) (string, bool) {")
	g.P("\tslug := ", stringsTrimSpace, "(tenantSlug)")
	g.P("\tif slug == \"\" || slug == \"${TENANT_SLUG}\" {")
	g.P("\t\treturn \"${TENANT_SLUG}\", true")
	g.P("\t}")
	g.P("\tslug = ", stringsReplaceAll, "(slug, \"-\", \"_\")")
	g.P("\tif !isValidDorisSQLIdentifier(slug) {")
	g.P("\t\treturn \"\", false")
	g.P("\t}")
	g.P("\treturn slug, true")
	g.P("}")
	g.P()

	g.P("func validateDorisDescriptor(t DorisTableDescriptor) bool {")
	g.P("\tif !isValidDorisSQLIdentifier(t.TableName) {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tif t.ColocateGroup != \"\" && !isValidDorisSQLIdentifier(t.ColocateGroup) {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tif t.SequenceCol != \"\" && !isValidDorisSQLIdentifier(t.SequenceCol) {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tif t.DistributedByColumn != \"\" && !isValidDorisSQLIdentifier(t.DistributedByColumn) {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tfor _, list := range [][]string{t.UniqueKeyColumns, t.PartitionByColumns, t.InvertedIndexColumns, t.NgramBfColumns, t.BloomFilterColumns, t.DuplicateKeyColumns} {")
	g.P("\t\tfor _, id := range list {")
	g.P("\t\t\tif id != \"\" && !isValidDorisSQLIdentifier(id) {")
	g.P("\t\t\t\treturn false")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tfor _, c := range t.Columns {")
	g.P("\t\tif !isValidDorisSQLIdentifier(c.ColumnName) {")
	g.P("\t\t\treturn false")
	g.P("\t\t}")
	g.P("\t\tif c.DorisType != \"\" && !isValidDorisSQLTypeSyntax(c.DorisType) {")
	g.P("\t\t\treturn false")
	g.P("\t\t}")
	g.P("\t\tif c.IcebergType != \"\" && !isValidDorisSQLTypeSyntax(c.IcebergType) {")
	g.P("\t\t\treturn false")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn true")
	g.P("}")
	g.P()

	g.P("func formatDorisDDLColumnIdent(colName string) string {")
	g.P("\tif colName == \"system\" {")
	g.P("\t\treturn \"`system`\"")
	g.P("\t}")
	g.P("\treturn colName")
	g.P("}")
	g.P()

	// Emit BuildDorisCreateTableSQL and BuildIcebergCreateTableSQL on DorisTableDescriptor.
	g.P("func dorisColumnIcebergDDLType(dorisType, icebergType string) string {")
	g.P("\tdt := ", stringsTrimSpace, "(dorisType)")
	g.P("\tswitch dt {")
	g.P("\tcase \"DATEV2\":")
	g.P("\t\treturn \"DATE\"")
	g.P("\tcase \"DATETIMEV2(6)\", \"DATETIMEV2(3)\", \"DATETIMEV2\":")
	g.P("\t\treturn \"DATETIME\"")
	g.P("\t}")
	g.P("\tif ", stringsHasPrefix, "(dt, \"DECIMALV3(\") {")
	g.P("\t\trest := ", stringsTrimPrefix, "(dt, \"DECIMALV3(\")")
	g.P("\t\trest = ", stringsReplaceAll, "(rest, \", \", \",\")")
	g.P("\t\trest = ", stringsReplaceAll, "(rest, \",\", \", \")")
	g.P("\t\treturn \"DECIMAL(\" + rest")
	g.P("\t}")
	g.P("\tif dt != \"\" {")
	g.P("\t\treturn dt")
	g.P("\t}")
	g.P("\tit := ", stringsTrimSpace, "(icebergType)")
	g.P("\tswitch ", stringsToUpper, "(it) {")
	g.P("\tcase \"STRING\":")
	g.P("\t\treturn \"VARCHAR(256)\"")
	g.P("\tcase \"LONG\":")
	g.P("\t\treturn \"BIGINT\"")
	g.P("\tcase \"TIMESTAMPTZ\", \"TIMESTAMP\":")
	g.P("\t\treturn \"DATETIME\"")
	g.P("\tcase \"LIST<STRING>\":")
	g.P("\t\treturn \"ARRAY<VARCHAR(64)>\"")
	g.P("\tcase \"LIST<FLOAT>\":")
	g.P("\t\treturn \"ARRAY<FLOAT>\"")
	g.P("\tcase \"LIST<DOUBLE>\":")
	g.P("\t\treturn \"ARRAY<DOUBLE>\"")
	g.P("\tcase \"LIST<INT>\":")
	g.P("\t\treturn \"ARRAY<INT>\"")
	g.P("\tcase \"LIST<LONG>\":")
	g.P("\t\treturn \"ARRAY<BIGINT>\"")
	g.P("\t}")
	g.P("\treturn it")
	g.P("}")
	g.P()

	g.P("// BuildDorisCreateTableSQL renders a deterministic Apache Doris 4.x")
	g.P("// CREATE TABLE IF NOT EXISTS statement for this table descriptor.")
	g.P("func (t DorisTableDescriptor) BuildDorisCreateTableSQL(tenantSlug string, buckets int) string {")
	g.P("\tif !validateDorisDescriptor(t) || t.SyncTier == \"ICEBERG_ONLY\" {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tvar uks []string")
	g.P("\tif len(t.UniqueKeyColumns) > 0 {")
	g.P("\t\tuks = append(uks, t.UniqueKeyColumns...)")
	g.P("\t} else {")
	g.P("\t\tfor _, c := range t.Columns {")
	g.P("\t\t\tif c.IsUniqueKey {")
	g.P("\t\t\t\tuks = append(uks, c.ColumnName)")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tif t.DatabaseScope == \"GLOBAL_TERMINOLOGY\" && t.SyncTier == \"RELEASE_PARTITIONED\" && len(uks) == 0 && len(t.DuplicateKeyColumns) == 0 {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tslug, ok := sanitizeDorisTenantSlug(tenantSlug)")
	g.P("\tif !ok {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tbucketStr := \"${TENANT_BUCKETS}\"")
	g.P("\tif buckets > 0 {")
	g.P("\t\tbucketStr = ", strconvItoa, "(buckets)")
	g.P("\t}")
	g.P("\tvar fullTableName string")
	g.P("\tswitch t.DatabaseScope {")
	g.P("\tcase \"GLOBAL_TERMINOLOGY\":")
	g.P("\t\tfullTableName = \"internal.terminology.\" + t.TableName")
	g.P("\t\tif buckets <= 0 {")
	g.P("\t\t\tbucketStr = \"1\"")
	g.P("\t\t}")
	g.P("\tcase \"GLOBAL_TERMINOLOGY_RAG\":")
	g.P("\t\tfullTableName = \"internal.terminology_rag.\" + t.TableName")
	g.P("\t\tif buckets <= 0 {")
	g.P("\t\t\tbucketStr = \"8\"")
	g.P("\t\t}")
	g.P("\tcase \"GLOBAL_TELEMETRY\":")
	g.P("\t\tfullTableName = \"internal.telemetry.\" + t.TableName")
	g.P("\t\tif buckets <= 0 {")
	g.P("\t\t\tbucketStr = \"8\"")
	g.P("\t\t}")
	g.P("\tdefault:")
	g.P("\t\tfullTableName = \"internal.tenant_\" + slug + \".\" + t.TableName")
	g.P("\t}")
	g.P("\tvar bodyLines []string")
	g.P("\tfor _, c := range t.Columns {")
	g.P("\t\tcolIdent := formatDorisDDLColumnIdent(c.ColumnName)")
	g.P("\t\tline := ", fmtSprintf, "(\"    %s %s\", colIdent, c.DorisType)")
	g.P("\t\tif !c.Nullable {")
	g.P("\t\t\tline += \" NOT NULL\"")
	g.P("\t\t\tupperType := ", stringsToUpper, "(", stringsTrimSpace, "(c.DorisType))")
	g.P("\t\t\tif c.DefaultValue != \"\" {")
	g.P("\t\t\t\tline += ", fmtSprintf, "(\" DEFAULT %q\", c.DefaultValue)")
	g.P("\t\t\t} else if ", stringsHasPrefix, "(upperType, \"ARRAY<\") && c.ColumnName != \"embedding\" {")
	g.P("\t\t\t\tline += ` DEFAULT \"[]\"`")
	g.P("\t\t\t} else if c.ColumnName == \"record_status\" {")
	g.P("\t\t\t\tline += ` DEFAULT \"ACTIVE\"`")
	g.P("\t\t\t} else if c.IsSequenceCol {")
	g.P("\t\t\t\tline += ` DEFAULT \"0\"`")
	g.P("\t\t\t} else if upperType == \"BOOLEAN\" && !c.IsUniqueKey {")
	g.P("\t\t\t\tline += ` DEFAULT \"false\"`")
	g.P("\t\t\t} else if (upperType == \"SMALLINT\" || upperType == \"INT\") && !c.IsUniqueKey && (", stringsHasSuffix, "(c.ColumnName, \"_count\") || c.ColumnName == \"display_order\") {")
	g.P("\t\t\t\tline += ` DEFAULT \"0\"`")
	g.P("\t\t\t} else if upperType == \"DOUBLE\" && !c.IsUniqueKey && c.ColumnName == \"additive_offset\" {")
	g.P("\t\t\t\tline += ` DEFAULT \"0.0\"`")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t\tbodyLines = append(bodyLines, line)")
	g.P("\t}")
	g.P("\tseenInv := make(map[string]bool)")
	g.P("\tvar invCols []string")
	g.P("\tfor _, c := range t.Columns {")
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
	g.P("\t\tbodyLines = append(bodyLines, ", fmtSprintf, "(\"    INDEX idx_%s_%s (%s) USING INVERTED\", t.TableName, idxCol, idxCol))")
	g.P("\t}")
	g.P("\tfor _, ngCol := range t.NgramBfColumns {")
	g.P("\t\tif ngCol == \"\" {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tbodyLines = append(bodyLines, ", fmtSprintf, "(`    INDEX idx_%s_%s_ngram (%s) USING NGRAM_BF PROPERTIES (\"gram_size\" = \"3\", \"bf_size\" = \"1024\")`, t.TableName, ngCol, ngCol))")
	g.P("\t}")
	g.P("\tvar b ", stringsBuilder)
	g.P("\tb.WriteString(\"CREATE TABLE IF NOT EXISTS \")")
	g.P("\tb.WriteString(fullTableName)")
	g.P("\tb.WriteString(\" (\\n\")")
	g.P("\tb.WriteString(", stringsJoin, "(bodyLines, \",\\n\"))")
	g.P("\tb.WriteString(\"\\n)\\n\")")
	g.P("\tif len(uks) > 0 {")
	g.P("\t\tb.WriteString(\"UNIQUE KEY(\")")
	g.P("\t\tb.WriteString(", stringsJoin, "(uks, \", \"))")
	g.P("\t\tb.WriteString(\")\\n\")")
	g.P("\t\tdistCol := t.DistributedByColumn")
	g.P("\t\tif distCol == \"\" {")
	g.P("\t\t\tif len(uks) > 1 && uks[0] == \"tenant_id\" {")
	g.P("\t\t\t\tdistCol = uks[1]")
	g.P("\t\t\t} else {")
	g.P("\t\t\t\tdistCol = uks[0]")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t\tb.WriteString(\"DISTRIBUTED BY HASH(\")")
	g.P("\t\tb.WriteString(distCol)")
	g.P("\t\tb.WriteString(\") BUCKETS \")")
	g.P("\t\tb.WriteString(bucketStr)")
	g.P("\t\tb.WriteString(\"\\n\")")
	g.P("\t} else if len(t.DuplicateKeyColumns) > 0 {")
	g.P("\t\tb.WriteString(\"DUPLICATE KEY(\")")
	g.P("\t\tb.WriteString(", stringsJoin, "(t.DuplicateKeyColumns, \", \"))")
	g.P("\t\tb.WriteString(\")\\n\")")
	g.P("\t\tdistCol := t.DistributedByColumn")
	g.P("\t\tif distCol == \"\" {")
	g.P("\t\t\tdistCol = t.DuplicateKeyColumns[0]")
	g.P("\t\t}")
	g.P("\t\tb.WriteString(\"DISTRIBUTED BY HASH(\")")
	g.P("\t\tb.WriteString(distCol)")
	g.P("\t\tb.WriteString(\") BUCKETS \")")
	g.P("\t\tb.WriteString(bucketStr)")
	g.P("\t\tb.WriteString(\"\\n\")")
	g.P("\t} else if len(t.Columns) > 0 {")
	g.P("\t\tb.WriteString(\"DUPLICATE KEY(\")")
	g.P("\t\tb.WriteString(t.Columns[0].ColumnName)")
	g.P("\t\tb.WriteString(\")\\n\")")
	g.P("\t\tdistCol := t.DistributedByColumn")
	g.P("\t\tif distCol == \"\" {")
	g.P("\t\t\tdistCol = t.Columns[0].ColumnName")
	g.P("\t\t}")
	g.P("\t\tb.WriteString(\"DISTRIBUTED BY HASH(\")")
	g.P("\t\tb.WriteString(distCol)")
	g.P("\t\tb.WriteString(\") BUCKETS \")")
	g.P("\t\tb.WriteString(bucketStr)")
	g.P("\t\tb.WriteString(\"\\n\")")
	g.P("\t}")
	g.P("\tvar props []string")
	g.P("\tprops = append(props, `    \"storage_vault_name\" = \"azra_lakehouse_vault\"`)")
	g.P("\tprops = append(props, `    \"compression\" = \"zstd\"`)")
	g.P("\tif len(uks) > 0 {")
	g.P("\t\tprops = append(props, `    \"enable_unique_key_merge_on_write\" = \"true\"`)")
	g.P("\t}")
	g.P("\tseqCol := t.SequenceCol")
	g.P("\tif seqCol == \"\" {")
	g.P("\t\tfor _, c := range t.Columns {")
	g.P("\t\t\tif c.IsSequenceCol {")
	g.P("\t\t\t\tseqCol = c.ColumnName")
	g.P("\t\t\t\tbreak")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tif seqCol != \"\" {")
	g.P("\t\tprops = append(props, ", fmtSprintf, "(`    \"function_column.sequence_col\" = %q`, seqCol))")
	g.P("\t}")
	g.P("\tif t.ColocateGroup != \"\" {")
	g.P("\t\tprops = append(props, ", fmtSprintf, "(`    \"colocate_with\" = %q`, t.ColocateGroup))")
	g.P("\t}")
	g.P("\tif t.FileCacheTTLSeconds > 0 {")
	g.P("\t\tprops = append(props, ", fmtSprintf, "(`    \"file_cache_ttl_seconds\" = \"%d\"`, t.FileCacheTTLSeconds))")
	g.P("\t} else if t.DatabaseScope == \"GLOBAL_TERMINOLOGY\" {")
	g.P("\t\tprops = append(props, `    \"file_cache_ttl_seconds\" = \"604800\"`)")
	g.P("\t}")
	g.P("\tif t.DatabaseScope == \"TENANT\" || t.DatabaseScope == \"\" {")
	g.P("\t\tprops = append(props, `    \"group_commit_interval_ms\" = \"200\"`)")
	g.P("\t}")
	g.P("\tif len(t.BloomFilterColumns) > 0 {")
	g.P("\t\tprops = append(props, ", fmtSprintf, "(`    \"bloom_filter_columns\" = %q`, ", stringsJoin, "(t.BloomFilterColumns, \", \")))")
	g.P("\t}")
	g.P("\tb.WriteString(\"PROPERTIES (\\n\")")
	g.P("\tb.WriteString(", stringsJoin, "(props, \",\\n\"))")
	g.P("\tb.WriteString(\"\\n);\")")
	g.P("\treturn b.String()")
	g.P("}")
	g.P()

	g.P("// BuildIcebergCreateTableSQL renders a deterministic Apache Iceberg")
	g.P("// CREATE TABLE IF NOT EXISTS statement for this table descriptor.")
	g.P("// Returns an empty string if SyncTier == \"INTERNAL_ONLY\" or identifier validation fails.")
	g.P("func (t DorisTableDescriptor) BuildIcebergCreateTableSQL(tenantSlug string) string {")
	g.P("\tif !validateDorisDescriptor(t) || t.SyncTier == \"INTERNAL_ONLY\" {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tslug, ok := sanitizeDorisTenantSlug(tenantSlug)")
	g.P("\tif !ok {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tvar fullTableName string")
	g.P("\tswitch t.DatabaseScope {")
	g.P("\tcase \"GLOBAL_TERMINOLOGY\":")
	g.P("\t\tfullTableName = \"azra_lake.terminology.\" + t.TableName")
	g.P("\tcase \"GLOBAL_TELEMETRY\":")
	g.P("\t\tfullTableName = \"azra_lake.telemetry.\" + t.TableName")
	g.P("\tcase \"GLOBAL_TERMINOLOGY_RAG\":")
	g.P("\t\tfullTableName = \"azra_lake.terminology_rag.\" + t.TableName")
	g.P("\tdefault:")
	g.P("\t\tfullTableName = \"azra_lake.tenant_\" + slug + \".\" + t.TableName")
	g.P("\t}")
	g.P("\tvar bodyLines []string")
	g.P("\tfor _, c := range t.Columns {")
	g.P("\t\tcolIdent := formatDorisDDLColumnIdent(c.ColumnName)")
	g.P("\t\ticeType := dorisColumnIcebergDDLType(c.DorisType, c.IcebergType)")
	g.P("\t\tline := ", fmtSprintf, "(\"    %s %s\", colIdent, iceType)")
	g.P("\t\tif !c.Nullable {")
	g.P("\t\t\tline += \" NOT NULL\"")
	g.P("\t\t}")
	g.P("\t\tbodyLines = append(bodyLines, line)")
	g.P("\t}")
	g.P("\tvar partCols []string")
	g.P("\tif len(t.PartitionByColumns) > 0 {")
	g.P("\t\tpartCols = append(partCols, t.PartitionByColumns...)")
	g.P("\t} else {")
	g.P("\t\tfor _, c := range t.Columns {")
	g.P("\t\t\tif c.IsPartitionKey {")
	g.P("\t\t\t\tpartCols = append(partCols, c.ColumnName)")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tif len(partCols) == 0 && t.SyncTier == \"MONTHLY_PARTITIONED\" {")
	g.P("\t\tfor _, c := range t.Columns {")
	g.P("\t\t\tif c.ColumnName == \"event_month\" {")
	g.P("\t\t\t\tpartCols = append(partCols, \"event_month\")")
	g.P("\t\t\t\tbreak")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tif len(partCols) == 0 && t.SyncTier == \"RELEASE_PARTITIONED\" {")
	g.P("\t\tfor _, c := range t.Columns {")
	g.P("\t\t\tif c.ColumnName == \"release_id\" {")
	g.P("\t\t\t\tpartCols = append(partCols, \"release_id\")")
	g.P("\t\t\t\tbreak")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tvar b ", stringsBuilder)
	g.P("\tb.WriteString(\"CREATE TABLE IF NOT EXISTS \")")
	g.P("\tb.WriteString(fullTableName)")
	g.P("\tb.WriteString(\" (\\n\")")
	g.P("\tb.WriteString(", stringsJoin, "(bodyLines, \",\\n\"))")
	g.P("\tb.WriteString(\"\\n)\\n\")")
	g.P("\tif len(partCols) > 0 {")
	g.P("\t\tb.WriteString(\"PARTITION BY LIST (\")")
	g.P("\t\tb.WriteString(", stringsJoin, "(partCols, \", \"))")
	g.P("\t\tb.WriteString(\") ()\\n\")")
	g.P("\t}")
	g.P("\tvar props []string")
	g.P("\tprops = append(props, `    \"compression_codec\" = \"zstd\"`)")
	g.P("\tif len(t.BloomFilterColumns) > 0 {")
	g.P("\t\tfor _, bf := range t.BloomFilterColumns {")
	g.P("\t\t\tif bf != \"\" {")
	g.P("\t\t\t\tprops = append(props, ", fmtSprintf, "(`    \"write.parquet.bloom-filter-enabled.column.%s\" = \"true\"`, bf))")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t} else if (t.DatabaseScope == \"TENANT\" || t.DatabaseScope == \"\") && len(t.Columns) > 0 && t.Columns[0].ColumnName == \"patient_id\" {")
	g.P("\t\tprops = append(props, `    \"write.parquet.bloom-filter-enabled.column.patient_id\" = \"true\"`)")
	g.P("\t}")
	g.P("\tb.WriteString(\"PROPERTIES (\\n\")")
	g.P("\tb.WriteString(", stringsJoin, "(props, \",\\n\"))")
	g.P("\tb.WriteString(\"\\n);\")")
	g.P("\treturn b.String()")
	g.P("}")
	g.P()

	// Emit value/scan/enum/date/time/uint64 helpers.
	g.P("type dorisValueScanner struct {")
	g.P("\tvalues []any")
	g.P("}")
	g.P()
	g.P("// NewDorisValueScanner wraps a slice of column values (such as r.Values())")
	g.P("// into a DorisScanner for in-memory row scanning and round-trip testing.")
	g.P("func NewDorisValueScanner(values []any) DorisScanner {")
	g.P("\treturn &dorisValueScanner{values: values}")
	g.P("}")
	g.P()
	g.P("func (s *dorisValueScanner) Scan(dest ...any) error {")
	g.P("\tif len(dest) != len(s.values) {")
	g.P("\t\treturn ", fmtErrorf, "(\"dorisValueScanner: expected %d destination arguments, got %d\", len(s.values), len(dest))")
	g.P("\t}")
	g.P("\tfor i, d := range dest {")
	g.P("\t\trawVal := s.values[i]")
	g.P("\t\tif valuer, ok := rawVal.(", driverValuer, "); ok && rawVal != nil {")
	g.P("\t\t\tvar err error")
	g.P("\t\t\trawVal, err = valuer.Value()")
	g.P("\t\t\tif err != nil {")
	g.P("\t\t\t\treturn err")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t\tdv, err := ", driverDefaultConv, ".ConvertValue(rawVal)")
	g.P("\t\tif err != nil {")
	g.P("\t\t\treturn err")
	g.P("\t\t}")
	g.P("\t\tif sc, ok := d.(", sqlScanner, "); ok {")
	g.P("\t\t\tif err := sc.Scan(dv); err != nil {")
	g.P("\t\t\t\treturn err")
	g.P("\t\t\t}")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tswitch ptr := d.(type) {")
	g.P("\t\tcase *any:")
	g.P("\t\t\t*ptr = dv")
	g.P("\t\tcase *[]byte:")
	g.P("\t\t\tif dv == nil {")
	g.P("\t\t\t\t*ptr = nil")
	g.P("\t\t\t} else if b, ok := dv.([]byte); ok {")
	g.P("\t\t\t\tcp := make([]byte, len(b))")
	g.P("\t\t\t\tcopy(cp, b)")
	g.P("\t\t\t\t*ptr = cp")
	g.P("\t\t\t} else if str, ok := dv.(string); ok {")
	g.P("\t\t\t\t*ptr = []byte(str)")
	g.P("\t\t\t} else {")
	g.P("\t\t\t\treturn ", fmtErrorf, "(\"dorisValueScanner: cannot scan %T into *[]byte\", dv)")
	g.P("\t\t\t}")
	g.P("\t\tdefault:")
	g.P("\t\t\treturn ", fmtErrorf, "(\"dorisValueScanner: unsupported scan target %T at column %d\", d, i)")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn nil")
	g.P("}")
	g.P()

	g.P("func dorisUint64Value(v uint64) any {")
	g.P("\tif v > uint64(", mathMaxInt64, ") {")
	g.P("\t\treturn ", strconvFormatUint, "(v, 10)")
	g.P("\t}")
	g.P("\treturn int64(v)")
	g.P("}")
	g.P()

	g.P("func dorisPtrUint64Value(v *uint64) any {")
	g.P("\tif v == nil {")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\treturn dorisUint64Value(*v)")
	g.P("}")
	g.P()

	g.P("func scanDorisUint64(raw any) (uint64, bool, error) {")
	g.P("\tif raw == nil {")
	g.P("\t\treturn 0, false, nil")
	g.P("\t}")
	g.P("\tswitch v := raw.(type) {")
	g.P("\tcase uint64:")
	g.P("\t\treturn v, true, nil")
	g.P("\tcase int64:")
	g.P("\t\treturn uint64(v), true, nil")
	g.P("\tcase ", sqlNullInt64, ":")
	g.P("\t\tif !v.Valid {")
	g.P("\t\t\treturn 0, false, nil")
	g.P("\t\t}")
	g.P("\t\treturn uint64(v.Int64), true, nil")
	g.P("\tcase []byte:")
	g.P("\t\ts := ", stringsTrimSpace, "(string(v))")
	g.P("\t\tif s == \"\" {")
	g.P("\t\t\treturn 0, false, nil")
	g.P("\t\t}")
	g.P("\t\tu, err := ", strconvParseUint, "(s, 10, 64)")
	g.P("\t\treturn u, err == nil, err")
	g.P("\tcase string:")
	g.P("\t\ts := ", stringsTrimSpace, "(v)")
	g.P("\t\tif s == \"\" {")
	g.P("\t\t\treturn 0, false, nil")
	g.P("\t\t}")
	g.P("\t\tu, err := ", strconvParseUint, "(s, 10, 64)")
	g.P("\t\treturn u, err == nil, err")
	g.P("\tdefault:")
	g.P("\t\treturn 0, false, ", fmtErrorf, "(\"scanDorisUint64: unsupported type %T\", raw)")
	g.P("\t}")
	g.P("}")
	g.P()

	g.P("func dorisNullableDateValue(s string) any {")
	g.P("\tif s == \"\" {")
	g.P("\t\treturn ", sqlNullString, "{}")
	g.P("\t}")
	g.P("\treturn s")
	g.P("}")
	g.P()

	g.P("func dorisNullableTimeValue(t ", timeTime, ") any {")
	g.P("\tif t.IsZero() {")
	g.P("\t\treturn ", sqlNullTime, "{}")
	g.P("\t}")
	g.P("\treturn t.UTC()")
	g.P("}")
	g.P()

	g.P("func dorisPtrTimeValue(t *", timeTime, ") any {")
	g.P("\tif t == nil || t.IsZero() {")
	g.P("\t\treturn ", sqlNullTime, "{}")
	g.P("\t}")
	g.P("\treturn t.UTC()")
	g.P("}")
	g.P()

	g.P("func scanDorisTime(raw any) (", timeTime, ", bool) {")
	g.P("\tif raw == nil {")
	g.P("\t\treturn ", timeTime, "{}, false")
	g.P("\t}")
	g.P("\tswitch v := raw.(type) {")
	g.P("\tcase ", timeTime, ":")
	g.P("\t\tif v.IsZero() {")
	g.P("\t\t\treturn ", timeTime, "{}, false")
	g.P("\t\t}")
	g.P("\t\treturn v.UTC(), true")
	g.P("\tcase *", timeTime, ":")
	g.P("\t\tif v == nil || v.IsZero() {")
	g.P("\t\t\treturn ", timeTime, "{}, false")
	g.P("\t\t}")
	g.P("\t\treturn v.UTC(), true")
	g.P("\tcase ", sqlNullTime, ":")
	g.P("\t\tif !v.Valid || v.Time.IsZero() {")
	g.P("\t\t\treturn ", timeTime, "{}, false")
	g.P("\t\t}")
	g.P("\t\treturn v.Time.UTC(), true")
	g.P("\tcase ", sqlNullString, ":")
	g.P("\t\tif !v.Valid {")
	g.P("\t\t\treturn ", timeTime, "{}, false")
	g.P("\t\t}")
	g.P("\t\treturn parseDorisTimeString(v.String)")
	g.P("\tcase []byte:")
	g.P("\t\treturn parseDorisTimeString(string(v))")
	g.P("\tcase string:")
	g.P("\t\treturn parseDorisTimeString(v)")
	g.P("\tdefault:")
	g.P("\t\treturn ", timeTime, "{}, false")
	g.P("\t}")
	g.P("}")
	g.P()

	g.P("func parseDorisTimeString(s string) (", timeTime, ", bool) {")
	g.P("\ts = ", stringsTrimSpace, "(s)")
	g.P("\tif s == \"\" || ", stringsHasPrefix, "(s, \"0000-00-00\") {")
	g.P("\t\treturn ", timeTime, "{}, false")
	g.P("\t}")
	g.P("\tlayouts := []string{")
	g.P("\t\t\"2006-01-02 15:04:05.999999\",")
	g.P("\t\t\"2006-01-02 15:04:05\",")
	g.P("\t\t", timeRFC3339Nano, ",")
	g.P("\t\t", timeRFC3339, ",")
	g.P("\t\t\"2006-01-02\",")
	g.P("\t}")
	g.P("\tfor _, layout := range layouts {")
	g.P("\t\tif t, err := ", timeParseInLocation, "(layout, s, ", timeUTC, "); err == nil {")
	g.P("\t\t\tif t.IsZero() {")
	g.P("\t\t\t\treturn ", timeTime, "{}, false")
	g.P("\t\t\t}")
	g.P("\t\t\treturn t.UTC(), true")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn ", timeTime, "{}, false")
	g.P("}")
	g.P()

	g.P("func scanDorisDateString(raw any) string {")
	g.P("\tif raw == nil {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tswitch v := raw.(type) {")
	g.P("\tcase ", timeTime, ":")
	g.P("\t\tif v.IsZero() {")
	g.P("\t\t\treturn \"\"")
	g.P("\t\t}")
	g.P("\t\treturn v.UTC().Format(\"2006-01-02\")")
	g.P("\tcase *", timeTime, ":")
	g.P("\t\tif v == nil || v.IsZero() {")
	g.P("\t\t\treturn \"\"")
	g.P("\t\t}")
	g.P("\t\treturn v.UTC().Format(\"2006-01-02\")")
	g.P("\tcase ", sqlNullTime, ":")
	g.P("\t\tif !v.Valid || v.Time.IsZero() {")
	g.P("\t\t\treturn \"\"")
	g.P("\t\t}")
	g.P("\t\treturn v.Time.UTC().Format(\"2006-01-02\")")
	g.P("\tcase ", sqlNullString, ":")
	g.P("\t\tif !v.Valid {")
	g.P("\t\t\treturn \"\"")
	g.P("\t\t}")
	g.P("\t\treturn normalizeDorisDateStr(v.String)")
	g.P("\tcase []byte:")
	g.P("\t\treturn normalizeDorisDateStr(string(v))")
	g.P("\tcase string:")
	g.P("\t\treturn normalizeDorisDateStr(v)")
	g.P("\tdefault:")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("}")
	g.P()

	g.P("func normalizeDorisDateStr(s string) string {")
	g.P("\ts = ", stringsTrimSpace, "(s)")
	g.P("\tif s == \"\" || ", stringsHasPrefix, "(s, \"0000-00-00\") {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tif len(s) >= 10 && s[4] == '-' && s[7] == '-' {")
	g.P("\t\treturn s[:10]")
	g.P("\t}")
	g.P("\treturn s")
	g.P("}")
	g.P()

	g.P("func parseDorisDateParts(s string) (int32, int32, int32, bool) {")
	g.P("\ts = normalizeDorisDateStr(s)")
	g.P("\tif len(s) != 10 {")
	g.P("\t\treturn 0, 0, 0, false")
	g.P("\t}")
	g.P("\tt, err := ", timeParse, "(\"2006-01-02\", s)")
	g.P("\tif err != nil || t.Year() < 1 || t.Year() > 9999 {")
	g.P("\t\treturn 0, 0, 0, false")
	g.P("\t}")
	g.P("\treturn int32(t.Year()), int32(t.Month()), int32(t.Day()), true")
	g.P("}")
	g.P()

	g.P("func formatDorisDateParts(year, month, day int32) string {")
	g.P("\tif year < 1 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31 {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tt := ", timeDate, "(int(year), ", timeMonth, "(month), int(day), 0, 0, 0, 0, ", timeUTC, ")")
	g.P("\tif t.Year() != int(year) || int32(t.Month()) != month || int32(t.Day()) != day {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\treturn ", fmtSprintf, "(\"%04d-%02d-%02d\", year, month, day)")
	g.P("}")
	g.P()

	g.P("func stripProtoEnumPrefix(val, prefix string) string {")
	g.P("\tif val == \"\" || val == prefix+\"UNSPECIFIED\" || val == \"UNSPECIFIED\" {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\treturn ", stringsTrimPrefix, "(val, prefix)")
	g.P("}")
	g.P()

	g.P("func lookupProtoEnumValue(valMap map[string]int32, raw, prefix string) int32 {")
	g.P("\tif raw == \"\" {")
	g.P("\t\treturn 0")
	g.P("\t}")
	g.P("\tif v, ok := valMap[raw]; ok {")
	g.P("\t\treturn v")
	g.P("\t}")
	g.P("\tif prefix != \"\" {")
	g.P("\t\tif v, ok := valMap[prefix+raw]; ok {")
	g.P("\t\t\treturn v")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn 0")
	g.P("}")
	g.P()
}

// emitDorisDateHelpers emits parseDorisDateToProto and formatProtoDateToDoris
// once per Go package when google.type.Date is used.
func emitDorisDateHelpers(g *protogen.GeneratedFile, dateGoIdent protogen.GoIdent) {
	dateType := g.QualifiedGoIdent(dateGoIdent)
	g.P("func parseDorisDateToProto(s string) *", dateType, " {")
	g.P("\tyear, month, day, ok := parseDorisDateParts(s)")
	g.P("\tif !ok {")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\treturn &", dateType, "{")
	g.P("\t\tYear:  year,")
	g.P("\t\tMonth: month,")
	g.P("\t\tDay:   day,")
	g.P("\t}")
	g.P("}")
	g.P()
	g.P("func formatProtoDateToDoris(d *", dateType, ") string {")
	g.P("\tif d == nil {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\treturn formatDorisDateParts(d.GetYear(), d.GetMonth(), d.GetDay())")
	g.P("}")
	g.P()
}

// emitDorisTableCode emits all generated code for a single Doris table message.
func emitDorisTableCode(g *protogen.GeneratedFile, tbl *dorisTableIR) {
	structName := tbl.GoName + "Doris"
	protoType := g.QualifiedGoIdent(tbl.Msg.GoIdent)
	d := tbl.Descriptor

	// 1. <Name>DorisTable() DorisTableDescriptor
	g.P("// ", tbl.GoName, "DorisTable returns the Apache Doris and Apache Iceberg table descriptor for ", tbl.GoName, ".")
	g.P("func ", tbl.GoName, "DorisTable() DorisTableDescriptor {")
	g.P("\treturn DorisTableDescriptor{")
	g.P("\t\tTableName:            ", fmt.Sprintf("%q", d.TableName), ",")
	g.P("\t\tMessageName:          ", fmt.Sprintf("%q", d.MessageName), ",")
	g.P("\t\tDatabaseScope:        ", fmt.Sprintf("%q", d.DatabaseScope), ",")
	g.P("\t\tSyncTier:             ", fmt.Sprintf("%q", d.SyncTier), ",")
	g.P("\t\tUniqueKeyColumns:     ", formatGoStringSliceLiteral(d.UniqueKeyColumns), ",")
	g.P("\t\tPartitionByColumns:   ", formatGoStringSliceLiteral(d.PartitionByColumns), ",")
	g.P("\t\tColocateGroup:        ", fmt.Sprintf("%q", d.ColocateGroup), ",")
	g.P("\t\tSequenceCol:          ", fmt.Sprintf("%q", d.SequenceCol), ",")
	g.P("\t\tInvertedIndexColumns: ", formatGoStringSliceLiteral(d.InvertedIndexColumns), ",")
	g.P("\t\tNgramBfColumns:       ", formatGoStringSliceLiteral(d.NgramBfColumns), ",")
	g.P("\t\tBloomFilterColumns:   ", formatGoStringSliceLiteral(d.BloomFilterColumns), ",")
	g.P("\t\tDistributedByColumn:  ", fmt.Sprintf("%q", d.DistributedByColumn), ",")
	g.P("\t\tDuplicateKeyColumns:  ", formatGoStringSliceLiteral(d.DuplicateKeyColumns), ",")
	g.P("\t\tFileCacheTTLSeconds:  ", d.FileCacheTTLSeconds, ",")
	g.P("\t\tColumns: []DorisColumnDescriptor{")
	for _, c := range d.Columns {
		g.P("\t\t\t{")
		g.P("\t\t\t\tColumnName:      ", fmt.Sprintf("%q", c.ColumnName), ",")
		g.P("\t\t\t\tProtoFieldName:  ", fmt.Sprintf("%q", c.ProtoFieldName), ",")
		g.P("\t\t\t\tDorisType:       ", fmt.Sprintf("%q", c.DorisType), ",")
		g.P("\t\t\t\tIcebergType:     ", fmt.Sprintf("%q", c.IcebergType), ",")
		g.P("\t\t\t\tNullable:        ", c.Nullable, ",")
		g.P("\t\t\t\tInvertedIndex:   ", c.InvertedIndex, ",")
		g.P("\t\t\t\tIsUniqueKey:     ", c.IsUniqueKey, ",")
		g.P("\t\t\t\tIsSequenceCol:   ", c.IsSequenceCol, ",")
		g.P("\t\t\t\tIsPartitionKey:  ", c.IsPartitionKey, ",")
		g.P("\t\t\t\tStripEnumPrefix: ", c.StripEnumPrefix, ",")
		g.P("\t\t\t\tDefaultValue:    ", fmt.Sprintf("%q", c.DefaultValue), ",")
		g.P("\t\t\t},")
	}
	g.P("\t\t},")
	g.P("\t}")
	g.P("}")
	g.P()

	// 2. type <Name>Doris struct
	g.P("// ", structName, " is the Apache Doris and Apache Iceberg storage representation of ", tbl.Msg.Desc.FullName(), ".")
	g.P("type ", structName, " struct {")
	for _, c := range tbl.Columns {
		fieldType := c.GoFieldType
		if strings.HasSuffix(fieldType, "time.Time") {
			timeIdent := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "time", GoName: "Time"})
			if c.IsPointer {
				fieldType = "*" + timeIdent
			} else {
				fieldType = timeIdent
			}
		}
		g.P("\t", c.GoFieldName, " ", fieldType, " `db:", fmt.Sprintf("%q", c.ColDesc.ColumnName), " doris:", fmt.Sprintf("%q", c.ColDesc.DorisType), "`")
	}
	g.P("}")
	g.P()

	// 3. TableName() and Columns()
	g.P("// TableName returns the physical Doris/Iceberg table name.")
	g.P("func (", structName, ") TableName() string {")
	g.P("\treturn ", fmt.Sprintf("%q", d.TableName))
	g.P("}")
	g.P()

	colNames := make([]string, len(tbl.Columns))
	for i, c := range tbl.Columns {
		colNames[i] = c.ColDesc.ColumnName
	}
	g.P("// Columns returns the ordered SQL column names for ", structName, ".")
	g.P("func (", structName, ") Columns() []string {")
	g.P("\treturn ", formatGoStringSliceLiteral(colNames))
	g.P("}")
	g.P()

	// 4. Values() []any
	g.P("// Values returns a slice of column values in exact Columns() order ready for SQL execution.")
	g.P("func (r *", structName, ") Values() []any {")
	g.P("\tif r == nil {")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\treturn []any{")
	for _, c := range tbl.Columns {
		switch {
		case c.isArray():
			g.P("\t\tr.", c.GoFieldName, ",")
		case c.IsDate:
			if c.ColDesc.Nullable {
				g.P("\t\tdorisNullableDateValue(r.", c.GoFieldName, "),")
			} else {
				g.P("\t\tr.", c.GoFieldName, ",")
			}
		case c.IsTimestamp:
			if c.IsPointer {
				g.P("\t\tdorisPtrTimeValue(r.", c.GoFieldName, "),")
			} else if c.ColDesc.Nullable {
				g.P("\t\tdorisNullableTimeValue(r.", c.GoFieldName, "),")
			} else {
				g.P("\t\tr.", c.GoFieldName, ".UTC(),")
			}
		case c.IsUint64:
			if c.IsPointer {
				g.P("\t\tdorisPtrUint64Value(r.", c.GoFieldName, "),")
			} else {
				g.P("\t\tdorisUint64Value(r.", c.GoFieldName, "),")
			}
		default:
			g.P("\t\tr.", c.GoFieldName, ",")
		}
	}
	g.P("\t}")
	g.P("}")
	g.P()

	// 5. ScanRow(scanner DorisScanner) error
	emitDorisScanRow(g, tbl, structName)

	// 6. ToProto() and FromProto()
	emitDorisToProto(g, tbl, structName, protoType)
	emitDorisFromProto(g, tbl, structName, protoType)

	// 7. Convenience wrappers: Scan<Name>Proto and <Name>ProtoValues
	g.P("// Scan", tbl.GoName, "Proto scans a row from scanner and converts it to *", protoType, ".")
	g.P("func Scan", tbl.GoName, "Proto(scanner DorisScanner) (*", protoType, ", error) {")
	g.P("\tvar row ", structName)
	g.P("\tif err := row.ScanRow(scanner); err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\treturn row.ToProto(), nil")
	g.P("}")
	g.P()

	g.P("// ", tbl.GoName, "ProtoValues converts a *", protoType, " into SQL column values for ", d.TableName, ".")
	g.P("func ", tbl.GoName, "ProtoValues(m *", protoType, ") []any {")
	g.P("\tvar row ", structName)
	g.P("\trow.FromProto(m)")
	g.P("\treturn row.Values()")
	g.P("}")
	g.P()
}

// emitDorisScanRow generates ScanRow(scanner DorisScanner) error for a Doris table struct.
func emitDorisScanRow(g *protogen.GeneratedFile, tbl *dorisTableIR, structName string) {
	sqlNullString := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "database/sql", GoName: "NullString"})
	sqlNullInt64 := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "database/sql", GoName: "NullInt64"})
	sqlNullFloat64 := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "database/sql", GoName: "NullFloat64"})
	sqlNullBool := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "database/sql", GoName: "NullBool"})

	g.P("// ScanRow scans a single row from *sql.Row or *sql.Rows into ", structName, ".")
	g.P("func (r *", structName, ") ScanRow(scanner DorisScanner) error {")
	g.P("\t*r = ", structName, "{}")

	var scanArgs []string
	for i, c := range tbl.Columns {
		varName := fmt.Sprintf("col%d", i)
		scanArgs = append(scanArgs, "&"+varName)
		switch {
		case c.isArray():
			g.P("\tvar ", varName, " ", c.GoFieldType)
		case c.IsTimestamp || c.IsDate || c.IsUint64:
			g.P("\tvar ", varName, " any")
		case c.IsStringEnum || c.Field.Desc.Kind() == protoreflect.StringKind:
			g.P("\tvar ", varName, " ", sqlNullString)
		case c.Field.Desc.Kind() == protoreflect.BoolKind:
			g.P("\tvar ", varName, " ", sqlNullBool)
		case c.Field.Desc.Kind() == protoreflect.FloatKind || c.Field.Desc.Kind() == protoreflect.DoubleKind:
			g.P("\tvar ", varName, " ", sqlNullFloat64)
		case c.Field.Desc.Kind() == protoreflect.BytesKind:
			g.P("\tvar ", varName, " []byte")
		default:
			g.P("\tvar ", varName, " ", sqlNullInt64)
		}
	}

	g.P("\tif err := scanner.Scan(", strings.Join(scanArgs, ", "), "); err != nil {")
	g.P("\t\treturn err")
	g.P("\t}")

	for i, c := range tbl.Columns {
		varName := fmt.Sprintf("col%d", i)
		switch {
		case c.isArray():
			g.P("\tr.", c.GoFieldName, " = ", varName)
		case c.IsTimestamp:
			g.P("\tif t, ok := scanDorisTime(", varName, "); ok {")
			if c.IsPointer {
				g.P("\t\tr.", c.GoFieldName, " = &t")
			} else {
				g.P("\t\tr.", c.GoFieldName, " = t")
			}
			g.P("\t}")
		case c.IsDate:
			g.P("\tr.", c.GoFieldName, " = scanDorisDateString(", varName, ")")
		case c.IsUint64:
			g.P("\t{")
			g.P("\t\tu, ok, err := scanDorisUint64(", varName, ")")
			g.P("\t\tif err != nil {")
			g.P("\t\t\treturn err")
			g.P("\t\t}")
			g.P("\t\tif ok {")
			if c.IsPointer {
				g.P("\t\t\tr.", c.GoFieldName, " = &u")
			} else {
				g.P("\t\t\tr.", c.GoFieldName, " = u")
			}
			g.P("\t\t}")
			g.P("\t}")
		case c.IsStringEnum || c.Field.Desc.Kind() == protoreflect.StringKind:
			g.P("\tif ", varName, ".Valid {")
			if c.IsPointer {
				g.P("\t\tv := ", varName, ".String")
				g.P("\t\tr.", c.GoFieldName, " = &v")
			} else {
				g.P("\t\tr.", c.GoFieldName, " = ", varName, ".String")
			}
			g.P("\t}")
		case c.Field.Desc.Kind() == protoreflect.BoolKind:
			g.P("\tif ", varName, ".Valid {")
			if c.IsPointer {
				g.P("\t\tv := ", varName, ".Bool")
				g.P("\t\tr.", c.GoFieldName, " = &v")
			} else {
				g.P("\t\tr.", c.GoFieldName, " = ", varName, ".Bool")
			}
			g.P("\t}")
		case c.Field.Desc.Kind() == protoreflect.FloatKind:
			g.P("\tif ", varName, ".Valid {")
			if c.IsPointer {
				g.P("\t\tv := float32(", varName, ".Float64)")
				g.P("\t\tr.", c.GoFieldName, " = &v")
			} else {
				g.P("\t\tr.", c.GoFieldName, " = float32(", varName, ".Float64)")
			}
			g.P("\t}")
		case c.Field.Desc.Kind() == protoreflect.DoubleKind:
			g.P("\tif ", varName, ".Valid {")
			if c.IsPointer {
				g.P("\t\tv := ", varName, ".Float64")
				g.P("\t\tr.", c.GoFieldName, " = &v")
			} else {
				g.P("\t\tr.", c.GoFieldName, " = ", varName, ".Float64")
			}
			g.P("\t}")
		case c.Field.Desc.Kind() == protoreflect.BytesKind:
			g.P("\tif len(", varName, ") > 0 {")
			g.P("\t\tb := make([]byte, len(", varName, "))")
			g.P("\t\tcopy(b, ", varName, ")")
			g.P("\t\tr.", c.GoFieldName, " = b")
			g.P("\t}")
		default:
			g.P("\tif ", varName, ".Valid {")
			if c.IsPointer {
				g.P("\t\tv := ", c.BaseScalarType, "(", varName, ".Int64)")
				g.P("\t\tr.", c.GoFieldName, " = &v")
			} else {
				g.P("\t\tr.", c.GoFieldName, " = ", c.BaseScalarType, "(", varName, ".Int64)")
			}
			g.P("\t}")
		}
	}

	g.P("\treturn nil")
	g.P("}")
	g.P()
}

// dorisInlineNode represents a node in the hierarchy of inline_nested message fields.
type dorisInlineNode struct {
	field     *protogen.Field
	pathKey   string
	varSuffix string
	cols      []*dorisColumnIR
	children  []*dorisInlineNode
	byName    map[string]*dorisInlineNode
}

func buildDorisInlineTree(cols []*dorisColumnIR) []*dorisInlineNode {
	var roots []*dorisInlineNode
	rootByName := make(map[string]*dorisInlineNode)

	for _, c := range cols {
		if len(c.ParentChain) == 0 {
			continue
		}
		var curr *dorisInlineNode
		for depth, f := range c.ParentChain {
			name := string(f.Desc.Name())
			if depth == 0 {
				node, ok := rootByName[name]
				if !ok {
					node = &dorisInlineNode{
						field:     f,
						pathKey:   name,
						varSuffix: f.GoName,
						byName:    make(map[string]*dorisInlineNode),
					}
					rootByName[name] = node
					roots = append(roots, node)
				}
				curr = node
			} else {
				node, ok := curr.byName[name]
				if !ok {
					node = &dorisInlineNode{
						field:     f,
						pathKey:   curr.pathKey + "." + name,
						varSuffix: curr.varSuffix + f.GoName,
						byName:    make(map[string]*dorisInlineNode),
					}
					curr.byName[name] = node
					curr.children = append(curr.children, node)
				}
				curr = node
			}
		}
		curr.cols = append(curr.cols, c)
	}
	return roots
}

// emitDorisToProto generates ToProto() *pb.<Name> for a Doris table struct.
func emitDorisToProto(g *protogen.GeneratedFile, tbl *dorisTableIR, structName, protoType string) {
	g.P("// ToProto converts ", structName, " to the protobuf message *", protoType, ".")
	g.P("func (r *", structName, ") ToProto() *", protoType, " {")
	g.P("\tif r == nil {")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tout := &", protoType, "{}")

	for _, c := range tbl.Columns {
		if len(c.ParentChain) == 0 {
			emitDorisColumnToProto(g, c, "out", "")
		}
	}

	roots := buildDorisInlineTree(tbl.Columns)
	var emitNode func(node *dorisInlineNode, parentTargetVar, parentHasVar string)
	emitNode = func(node *dorisInlineNode, parentTargetVar, parentHasVar string) {
		nestedType := g.QualifiedGoIdent(node.field.Message.GoIdent)
		nestedVar := "nested" + node.varSuffix
		hasVar := "has" + node.varSuffix
		g.P("\t", nestedVar, " := &", nestedType, "{}")
		g.P("\t", hasVar, " := false")
		for _, c := range node.cols {
			emitDorisColumnToProto(g, c, nestedVar, hasVar)
		}
		for _, child := range node.children {
			emitNode(child, nestedVar, hasVar)
		}
		g.P("\tif ", hasVar, " {")
		if parentHasVar != "" {
			g.P("\t\t", parentHasVar, " = true")
		}
		g.P("\t\t", parentTargetVar, ".", node.field.GoName, " = ", nestedVar)
		g.P("\t}")
	}

	for _, root := range roots {
		emitNode(root, "out", "")
	}

	g.P("\treturn out")
	g.P("}")
	g.P()
}

// emitDorisColumnToProto emits the conversion from r.<GoFieldName> to targetVar.<ProtoGoName>.
// If hasFlagVar is non-empty, sets hasFlagVar = true whenever r.<GoFieldName> is non-zero.
func emitDorisColumnToProto(g *protogen.GeneratedFile, c *dorisColumnIR, targetVar, hasFlagVar string) {
	protoField := c.Field.GoName
	markSet := func(indent string) {
		if hasFlagVar != "" {
			g.P(indent, hasFlagVar, " = true")
		}
	}

	switch {
	case c.ArrayKind == "string":
		g.P("\tif len(r.", c.GoFieldName, ") > 0 {")
		markSet("\t\t")
		if c.IsEnum {
			enumIdent := g.QualifiedGoIdent(c.Field.Enum.GoIdent)
			g.P("\t\t", targetVar, ".", protoField, " = make([]", enumIdent, ", len(r.", c.GoFieldName, "))")
			g.P("\t\tfor i, s := range r.", c.GoFieldName, " {")
			g.P("\t\t\t", targetVar, ".", protoField, "[i] = ", enumIdent, "(lookupProtoEnumValue(", enumIdent, "_value, s, ", fmt.Sprintf("%q", c.EnumPrefix), "))")
			g.P("\t\t}")
		} else {
			g.P("\t\t", targetVar, ".", protoField, " = make([]string, len(r.", c.GoFieldName, "))")
			g.P("\t\tcopy(", targetVar, ".", protoField, ", r.", c.GoFieldName, ")")
		}
		g.P("\t}")
	case c.ArrayKind == "float32":
		g.P("\tif len(r.", c.GoFieldName, ") > 0 {")
		markSet("\t\t")
		g.P("\t\t", targetVar, ".", protoField, " = make([]float32, len(r.", c.GoFieldName, "))")
		g.P("\t\tcopy(", targetVar, ".", protoField, ", r.", c.GoFieldName, ")")
		g.P("\t}")
	case c.ArrayKind == "float64":
		g.P("\tif len(r.", c.GoFieldName, ") > 0 {")
		markSet("\t\t")
		g.P("\t\t", targetVar, ".", protoField, " = make([]float64, len(r.", c.GoFieldName, "))")
		g.P("\t\tcopy(", targetVar, ".", protoField, ", r.", c.GoFieldName, ")")
		g.P("\t}")
	case c.ArrayKind == "int32":
		g.P("\tif len(r.", c.GoFieldName, ") > 0 {")
		markSet("\t\t")
		g.P("\t\t", targetVar, ".", protoField, " = make([]int32, len(r.", c.GoFieldName, "))")
		g.P("\t\tcopy(", targetVar, ".", protoField, ", r.", c.GoFieldName, ")")
		g.P("\t}")
	case c.ArrayKind == "int64":
		g.P("\tif len(r.", c.GoFieldName, ") > 0 {")
		markSet("\t\t")
		elemGoType := goType(c.Field.Desc.Kind())
		if elemGoType == "int64" {
			g.P("\t\t", targetVar, ".", protoField, " = make([]int64, len(r.", c.GoFieldName, "))")
			g.P("\t\tcopy(", targetVar, ".", protoField, ", r.", c.GoFieldName, ")")
		} else {
			g.P("\t\t", targetVar, ".", protoField, " = make([]", elemGoType, ", len(r.", c.GoFieldName, "))")
			g.P("\t\tfor i, v := range r.", c.GoFieldName, " {")
			g.P("\t\t\t", targetVar, ".", protoField, "[i] = ", elemGoType, "(v)")
			g.P("\t\t}")
		}
		g.P("\t}")
	case c.ArrayKind == "bool":
		g.P("\tif len(r.", c.GoFieldName, ") > 0 {")
		markSet("\t\t")
		g.P("\t\t", targetVar, ".", protoField, " = make([]bool, len(r.", c.GoFieldName, "))")
		g.P("\t\tcopy(", targetVar, ".", protoField, ", r.", c.GoFieldName, ")")
		g.P("\t}")
	case c.IsTimestamp:
		tsNew := g.QualifiedGoIdent(protogen.GoIdent{
			GoImportPath: "google.golang.org/protobuf/types/known/timestamppb",
			GoName:       "New",
		})
		if c.IsPointer {
			g.P("\tif r.", c.GoFieldName, " != nil && !r.", c.GoFieldName, ".IsZero() {")
			markSet("\t\t")
			g.P("\t\t", targetVar, ".", protoField, " = ", tsNew, "(r.", c.GoFieldName, ".UTC())")
			g.P("\t}")
		} else {
			g.P("\tif !r.", c.GoFieldName, ".IsZero() {")
			markSet("\t\t")
			g.P("\t\t", targetVar, ".", protoField, " = ", tsNew, "(r.", c.GoFieldName, ".UTC())")
			g.P("\t}")
		}
	case c.IsDate:
		g.P("\tif r.", c.GoFieldName, " != \"\" {")
		g.P("\t\tif dt := parseDorisDateToProto(r.", c.GoFieldName, "); dt != nil {")
		markSet("\t\t\t")
		g.P("\t\t\t", targetVar, ".", protoField, " = dt")
		g.P("\t\t}")
		g.P("\t}")
	case c.IsEnum:
		enumIdent := g.QualifiedGoIdent(c.Field.Enum.GoIdent)
		if c.IsStringEnum {
			if c.IsPointer {
				g.P("\tif r.", c.GoFieldName, " != nil && *r.", c.GoFieldName, " != \"\" {")
				g.P("\t\tif rawVal := lookupProtoEnumValue(", enumIdent, "_value, *r.", c.GoFieldName, ", ", fmt.Sprintf("%q", c.EnumPrefix), "); rawVal != 0 {")
				markSet("\t\t\t")
				if c.Field.Desc.HasOptionalKeyword() {
					g.P("\t\t\tv := ", enumIdent, "(rawVal)")
					g.P("\t\t\t", targetVar, ".", protoField, " = &v")
				} else {
					g.P("\t\t\t", targetVar, ".", protoField, " = ", enumIdent, "(rawVal)")
				}
				g.P("\t\t}")
				g.P("\t}")
			} else {
				g.P("\tif r.", c.GoFieldName, " != \"\" {")
				g.P("\t\tif rawVal := lookupProtoEnumValue(", enumIdent, "_value, r.", c.GoFieldName, ", ", fmt.Sprintf("%q", c.EnumPrefix), "); rawVal != 0 {")
				markSet("\t\t\t")
				g.P("\t\t\t", targetVar, ".", protoField, " = ", enumIdent, "(rawVal)")
				g.P("\t\t}")
				g.P("\t}")
			}
		} else {
			if c.IsPointer {
				g.P("\tif r.", c.GoFieldName, " != nil {")
				markSet("\t\t")
				if c.Field.Desc.HasOptionalKeyword() {
					g.P("\t\tv := ", enumIdent, "(*r.", c.GoFieldName, ")")
					g.P("\t\t", targetVar, ".", protoField, " = &v")
				} else {
					g.P("\t\t", targetVar, ".", protoField, " = ", enumIdent, "(*r.", c.GoFieldName, ")")
				}
				g.P("\t}")
			} else {
				if hasFlagVar != "" {
					g.P("\tif r.", c.GoFieldName, " != 0 {")
					markSet("\t\t")
					g.P("\t\t", targetVar, ".", protoField, " = ", enumIdent, "(r.", c.GoFieldName, ")")
					g.P("\t}")
				} else {
					g.P("\t", targetVar, ".", protoField, " = ", enumIdent, "(r.", c.GoFieldName, ")")
				}
			}
		}
	case c.IsPointer:
		g.P("\tif r.", c.GoFieldName, " != nil {")
		markSet("\t\t")
		if c.Field.Desc.HasOptionalKeyword() {
			g.P("\t\tv := *r.", c.GoFieldName)
			g.P("\t\t", targetVar, ".", protoField, " = &v")
		} else {
			g.P("\t\t", targetVar, ".", protoField, " = *r.", c.GoFieldName)
		}
		g.P("\t}")
	case c.Field.Desc.Kind() == protoreflect.BytesKind:
		g.P("\tif len(r.", c.GoFieldName, ") > 0 {")
		markSet("\t\t")
		g.P("\t\tb := make([]byte, len(r.", c.GoFieldName, "))")
		g.P("\t\tcopy(b, r.", c.GoFieldName, ")")
		g.P("\t\t", targetVar, ".", protoField, " = b")
		g.P("\t}")
	default:
		if hasFlagVar != "" {
			zeroVal := goZeroValue(c.Field.Desc.Kind())
			if c.Field.Desc.Kind() == protoreflect.BoolKind {
				g.P("\tif r.", c.GoFieldName, " {")
			} else {
				g.P("\tif r.", c.GoFieldName, " != ", zeroVal, " {")
			}
			markSet("\t\t")
			g.P("\t\t", targetVar, ".", protoField, " = r.", c.GoFieldName)
			g.P("\t}")
		} else {
			g.P("\t", targetVar, ".", protoField, " = r.", c.GoFieldName)
		}
	}
}

// emitDorisFromProto generates FromProto(m *pb.<Name>) for a Doris table struct.
func emitDorisFromProto(g *protogen.GeneratedFile, tbl *dorisTableIR, structName, protoType string) {
	g.P("// FromProto populates ", structName, " from the protobuf message *", protoType, ".")
	g.P("func (r *", structName, ") FromProto(m *", protoType, ") {")
	g.P("\tif m == nil {")
	g.P("\t\treturn")
	g.P("\t}")
	g.P("\t*r = ", structName, "{}")

	for _, c := range tbl.Columns {
		if len(c.ParentChain) == 0 {
			emitDorisColumnFromProto(g, c, "m", "\t")
		}
	}

	roots := buildDorisInlineTree(tbl.Columns)
	var emitNode func(node *dorisInlineNode, parentSrcVar, indent string)
	emitNode = func(node *dorisInlineNode, parentSrcVar, indent string) {
		srcVar := "src" + node.varSuffix
		g.P(indent, "if ", srcVar, " := ", parentSrcVar, ".Get", node.field.GoName, "(); ", srcVar, " != nil {")
		for _, c := range node.cols {
			emitDorisColumnFromProto(g, c, srcVar, indent+"\t")
		}
		for _, child := range node.children {
			emitNode(child, srcVar, indent+"\t")
		}
		g.P(indent, "}")
	}

	for _, root := range roots {
		emitNode(root, "m", "\t")
	}

	g.P("}")
	g.P()
}

// emitDorisColumnFromProto emits the population of r.<GoFieldName> from srcVar.<ProtoGoName>.
func emitDorisColumnFromProto(g *protogen.GeneratedFile, c *dorisColumnIR, srcVar, indent string) {
	protoField := c.Field.GoName

	switch {
	case c.ArrayKind == "string":
		g.P(indent, "if src := ", srcVar, ".Get", protoField, "(); len(src) > 0 {")
		g.P(indent, "\tr.", c.GoFieldName, " = make(DorisStringArray, len(src))")
		if c.IsEnum {
			g.P(indent, "\tfor i, v := range src {")
			if c.ColDesc.StripEnumPrefix {
				g.P(indent, "\t\tr.", c.GoFieldName, "[i] = stripProtoEnumPrefix(v.String(), ", fmt.Sprintf("%q", c.EnumPrefix), ")")
			} else {
				g.P(indent, "\t\tr.", c.GoFieldName, "[i] = v.String()")
			}
			g.P(indent, "\t}")
		} else {
			g.P(indent, "\tcopy(r.", c.GoFieldName, ", src)")
		}
		g.P(indent, "}")
	case c.ArrayKind == "float32":
		g.P(indent, "if src := ", srcVar, ".Get", protoField, "(); len(src) > 0 {")
		g.P(indent, "\tr.", c.GoFieldName, " = make(DorisFloat32Array, len(src))")
		g.P(indent, "\tcopy(r.", c.GoFieldName, ", src)")
		g.P(indent, "}")
	case c.ArrayKind == "float64":
		g.P(indent, "if src := ", srcVar, ".Get", protoField, "(); len(src) > 0 {")
		g.P(indent, "\tr.", c.GoFieldName, " = make(DorisFloat64Array, len(src))")
		g.P(indent, "\tcopy(r.", c.GoFieldName, ", src)")
		g.P(indent, "}")
	case c.ArrayKind == "int32":
		g.P(indent, "if src := ", srcVar, ".Get", protoField, "(); len(src) > 0 {")
		g.P(indent, "\tr.", c.GoFieldName, " = make(DorisInt32Array, len(src))")
		g.P(indent, "\tcopy(r.", c.GoFieldName, ", src)")
		g.P(indent, "}")
	case c.ArrayKind == "int64":
		g.P(indent, "if src := ", srcVar, ".Get", protoField, "(); len(src) > 0 {")
		g.P(indent, "\tr.", c.GoFieldName, " = make(DorisInt64Array, len(src))")
		if goType(c.Field.Desc.Kind()) == "int64" {
			g.P(indent, "\tcopy(r.", c.GoFieldName, ", src)")
		} else {
			g.P(indent, "\tfor i, v := range src {")
			g.P(indent, "\t\tr.", c.GoFieldName, "[i] = int64(v)")
			g.P(indent, "\t}")
		}
		g.P(indent, "}")
	case c.ArrayKind == "bool":
		g.P(indent, "if src := ", srcVar, ".Get", protoField, "(); len(src) > 0 {")
		g.P(indent, "\tr.", c.GoFieldName, " = make(DorisBoolArray, len(src))")
		g.P(indent, "\tcopy(r.", c.GoFieldName, ", src)")
		g.P(indent, "}")
	case c.IsTimestamp:
		g.P(indent, "if ts := ", srcVar, ".Get", protoField, "(); ts != nil {")
		if c.IsPointer {
			g.P(indent, "\tv := ts.AsTime().UTC()")
			g.P(indent, "\tr.", c.GoFieldName, " = &v")
		} else {
			g.P(indent, "\tr.", c.GoFieldName, " = ts.AsTime().UTC()")
		}
		g.P(indent, "}")
	case c.IsDate:
		g.P(indent, "if dt := ", srcVar, ".Get", protoField, "(); dt != nil {")
		g.P(indent, "\tr.", c.GoFieldName, " = formatProtoDateToDoris(dt)")
		g.P(indent, "}")
	case c.IsEnum:
		if c.IsStringEnum {
			if c.IsPointer {
				if c.Field.Desc.HasOptionalKeyword() {
					g.P(indent, "if ", srcVar, ".", protoField, " != nil && ", srcVar, ".Get", protoField, "() != 0 {")
				} else {
					g.P(indent, "if ", srcVar, ".Get", protoField, "() != 0 {")
				}
				if c.ColDesc.StripEnumPrefix {
					g.P(indent, "\tv := stripProtoEnumPrefix(", srcVar, ".Get", protoField, "().String(), ", fmt.Sprintf("%q", c.EnumPrefix), ")")
				} else {
					g.P(indent, "\tv := ", srcVar, ".Get", protoField, "().String()")
				}
				g.P(indent, "\tr.", c.GoFieldName, " = &v")
				g.P(indent, "}")
			} else if c.ColDesc.ColumnName == "record_status" {
				if c.ColDesc.StripEnumPrefix {
					g.P(indent, "if v := stripProtoEnumPrefix(", srcVar, ".Get", protoField, "().String(), ", fmt.Sprintf("%q", c.EnumPrefix), "); v != \"\" {")
					g.P(indent, "\tr.", c.GoFieldName, " = v")
					g.P(indent, "} else {")
					g.P(indent, "\tr.", c.GoFieldName, " = \"ACTIVE\"")
					g.P(indent, "}")
				} else {
					g.P(indent, "r.", c.GoFieldName, " = ", srcVar, ".Get", protoField, "().String()")
				}
			} else {
				if c.ColDesc.StripEnumPrefix {
					g.P(indent, "r.", c.GoFieldName, " = stripProtoEnumPrefix(", srcVar, ".Get", protoField, "().String(), ", fmt.Sprintf("%q", c.EnumPrefix), ")")
				} else {
					g.P(indent, "r.", c.GoFieldName, " = ", srcVar, ".Get", protoField, "().String()")
				}
			}
		} else {
			if c.IsPointer {
				if c.Field.Desc.HasOptionalKeyword() {
					g.P(indent, "if ", srcVar, ".", protoField, " != nil {")
					g.P(indent, "\tv := int32(", srcVar, ".Get", protoField, "())")
					g.P(indent, "\tr.", c.GoFieldName, " = &v")
					g.P(indent, "}")
				} else {
					g.P(indent, "if ", srcVar, ".Get", protoField, "() != 0 {")
					g.P(indent, "\tv := int32(", srcVar, ".Get", protoField, "())")
					g.P(indent, "\tr.", c.GoFieldName, " = &v")
					g.P(indent, "}")
				}
			} else {
				g.P(indent, "r.", c.GoFieldName, " = int32(", srcVar, ".Get", protoField, "())")
			}
		}
	case c.IsPointer:
		if c.Field.Desc.HasOptionalKeyword() {
			g.P(indent, "if ", srcVar, ".", protoField, " != nil {")
			g.P(indent, "\tv := *", srcVar, ".", protoField)
			g.P(indent, "\tr.", c.GoFieldName, " = &v")
			g.P(indent, "}")
		} else {
			zeroVal := goZeroValue(c.Field.Desc.Kind())
			if c.Field.Desc.Kind() == protoreflect.BoolKind {
				g.P(indent, "if v := ", srcVar, ".Get", protoField, "(); v {")
			} else {
				g.P(indent, "if v := ", srcVar, ".Get", protoField, "(); v != ", zeroVal, " {")
			}
			g.P(indent, "\tr.", c.GoFieldName, " = &v")
			g.P(indent, "}")
		}
	case c.Field.Desc.Kind() == protoreflect.BytesKind:
		g.P(indent, "if src := ", srcVar, ".Get", protoField, "(); len(src) > 0 {")
		g.P(indent, "\tb := make([]byte, len(src))")
		g.P(indent, "\tcopy(b, src)")
		g.P(indent, "\tr.", c.GoFieldName, " = b")
		g.P(indent, "}")
	default:
		g.P(indent, "r.", c.GoFieldName, " = ", srcVar, ".Get", protoField, "()")
	}
}

func formatGoStringSliceLiteral(items []string) string {
	if len(items) == 0 {
		return "nil"
	}
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return "[]string{" + strings.Join(quoted, ", ") + "}"
}
