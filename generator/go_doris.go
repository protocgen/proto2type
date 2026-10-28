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
	ArrayKind      string // "", "string", "float32", "float64", "int32", "int64", "uint64", "bool"
	IsEnum         bool
	IsStringEnum   bool
	EnumPrefix     string
	IsPointer      bool
	IsUint64       bool
	IsOneofVariant bool
}

// isArray reports whether the column represents a Doris ARRAY<T> column.
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
// converters, Doris & Iceberg SQL DDL files, and/or Atlas HCL files for a proto file.
func (gg *goGenerator) generateGoDoris(gen *protogen.Plugin, file *protogen.File, opts *Options) error {
	switch opts.DorisMode {
	case "", "go", "ddl", "hcl", "both", "all", "starrocks", "starrocks_both", "starrocks_ddl", "starrocks_hcl":
	default:
		return fmt.Errorf("unknown doris_mode: %q (must be '', 'go', 'ddl', 'hcl', 'both', 'all', 'starrocks', 'starrocks_both', 'starrocks_ddl', or 'starrocks_hcl')", opts.DorisMode)
	}
	if opts.DorisStorageVault != "" && !isValidSQLIdentifier(opts.DorisStorageVault) {
		return fmt.Errorf("invalid doris_storage_vault %q (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)", opts.DorisStorageVault)
	}
	if opts.IcebergCatalog != "" && !isValidSQLIdentifier(opts.IcebergCatalog) {
		return fmt.Errorf("invalid iceberg_catalog %q (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)", opts.IcebergCatalog)
	}

	tables, err := buildDorisTablesForFile(file, opts)
	if err != nil {
		return err
	}
	// Skip file generation cleanly if no messages in this .proto file define Lakehouse tables.
	if len(tables) == 0 {
		return nil
	}

	emitGo := opts.DorisMode == "" || opts.DorisMode == "go" || opts.DorisMode == "both" || opts.DorisMode == "all" || opts.DorisMode == "starrocks_both"
	emitDDL := opts.DorisMode == "ddl" || opts.DorisMode == "both" || opts.DorisMode == "all"
	emitHCL := opts.DorisMode == "hcl" || opts.DorisMode == "all"
	emitStarRocksDDL := opts.DorisMode == "all" || opts.DorisMode == "starrocks" || opts.DorisMode == "starrocks_both" || opts.DorisMode == "starrocks_ddl" || (opts.Backend == "starrocks" && opts.DorisMode == "")
	emitStarRocksHCL := opts.DorisMode == "all" || opts.DorisMode == "starrocks" || opts.DorisMode == "starrocks_both" || opts.DorisMode == "starrocks_hcl" || (opts.Backend == "starrocks" && opts.DorisMode == "")

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
	if emitStarRocksDDL {
		if err := generateStarRocksDDLFile(gen, file, tables); err != nil {
			return err
		}
	}
	if emitHCL {
		if err := generateDorisHCLFiles(gen, file, tables); err != nil {
			return err
		}
	}
	if emitStarRocksHCL {
		if err := generateStarRocksHCLFile(gen, file, tables); err != nil {
			return err
		}
	}
	return nil
}

// buildDorisTablesForFile extracts all table-annotated messages from file in declaration order.
func buildDorisTablesForFile(file *protogen.File, opts *Options) ([]*dorisTableIR, error) {
	var tables []*dorisTableIR
	seenTables := make(map[string]protoreflect.FullName)
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
		id := dorisTableEvolutionIdentity(tbl.Descriptor)
		if prev, exists := seenTables[id]; exists {
			return nil, fmt.Errorf("message %s: duplicate table identity %q (already declared by %s)", msg.Desc.FullName(), meta.TableName, prev)
		}
		seenTables[id] = msg.Desc.FullName()
		tables = append(tables, tbl)
	}
	return tables, nil
}

// validateDorisTableMeta validates all SQL identifiers, database scopes, and sync tiers
// on a message's DorisTableMetaIR before code generation.
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
	if meta.StorageVaultName != "" && !isValidSQLIdentifier(meta.StorageVaultName) {
		return fmt.Errorf("message %s: invalid SQL identifier for storage_vault_name %q", fullName, meta.StorageVaultName)
	}
	if meta.IcebergCatalog != "" && !isValidSQLIdentifier(meta.IcebergCatalog) {
		return fmt.Errorf("message %s: invalid SQL identifier for iceberg_catalog %q", fullName, meta.IcebergCatalog)
	}
	if meta.EmbeddingDimension < 0 {
		return fmt.Errorf("message %s: embedding_dimension cannot be negative (%d)", fullName, meta.EmbeddingDimension)
	}
	if meta.EmbeddingDimension > 0 && len(meta.UniqueKeyColumns) > 0 {
		return fmt.Errorf("message %s: embedding_dimension (%d) requires a DUPLICATE KEY table and cannot be combined with unique_key_columns", fullName, meta.EmbeddingDimension)
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

// messageHasAnnotatedDorisColumns reports whether msg has at least one non-inline
// leaf field with an explicit Doris or Iceberg column type annotation.
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
			if cm.InlineNested {
				if f.Message != nil && messageHasAnnotatedDorisColumns(f.Message, visited) {
					return true
				}
				continue
			}
			if cm.DorisType != "" || cm.IcebergType != "" {
				return true
			}
		}
	}
	return false
}

// buildDorisTableIR constructs the flattened column list and DorisTableDescriptor for msg.
func buildDorisTableIR(msg *protogen.Message, meta *DorisTableMetaIR, opts *Options) (*dorisTableIR, error) {
	if meta.StorageVaultName == "" && opts != nil && opts.DorisStorageVault != "" {
		meta.StorageVaultName = opts.DorisStorageVault
	}
	if meta.IcebergCatalog == "" && opts != nil && opts.IcebergCatalog != "" {
		meta.IcebergCatalog = opts.IcebergCatalog
	}
	if err := validateDorisTableMeta(msg, meta); err != nil {
		return nil, err
	}

	ukSet := make(map[string]bool, len(meta.UniqueKeyColumns))
	for _, uk := range meta.UniqueKeyColumns {
		ukSet[uk] = true
	}
	dupSet := make(map[string]bool, len(meta.DuplicateKeyColumns))
	for _, dk := range meta.DuplicateKeyColumns {
		dupSet[dk] = true
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
		if f.Desc.IsMap() || (f.Desc.Kind() == protoreflect.MessageKind && !isWellKnownTimestamp(f) && !isWellKnownDate(f)) {
			continue
		}
		rootGoNames[toPascalCase(string(f.Desc.Name()))] = true
		if ov := fieldNameOverride(f); ov != "" {
			rootGoNames[toPascalCase(ov)] = true
		}
		if cm != nil && cm.ColumnName != "" {
			rootGoNames[toPascalCase(cm.ColumnName)] = true
		}
	}

	// Pre-seed usedGoNames with the reserved method names on <Msg>Doris so fields
	// like table_name, columns, values, or descriptor never collide with methods.
	usedGoNames := map[string]bool{
		"TableName":  true,
		"Columns":    true,
		"Values":     true,
		"ScanRow":    true,
		"ToProto":    true,
		"FromProto":  true,
		"Descriptor": true,
	}
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

			colIR, err := buildDorisColumnIR(field, chain, colMeta, meta, ukSet, dupSet, partSet, rootGoNames, usedGoNames, usedColNames, opts)
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

	uks := dedupDorisColumnList(meta.UniqueKeyColumns)
	if len(uks) == 0 {
		for _, c := range cols {
			if c.ColDesc.IsUniqueKey {
				uks = append(uks, c.ColDesc.ColumnName)
			}
		}
		uks = dedupDorisColumnList(uks)
	}
	dupKeys := dedupDorisColumnList(meta.DuplicateKeyColumns)

	partCols := dedupDorisColumnList(meta.PartitionByColumns)
	if len(partCols) == 0 {
		for _, c := range cols {
			if c.ColDesc.IsPartitionKey {
				partCols = append(partCols, c.ColDesc.ColumnName)
			}
		}
		partCols = dedupDorisColumnList(partCols)
	}
	ngramCols := dedupDorisColumnList(meta.NgramBfColumns)
	bloomCols := dedupDorisColumnList(meta.BloomFilterColumns)

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

	// Verify that every column referenced in table metadata actually exists in the flattened schema.
	checkColExists := func(label, colName string) error {
		if colName != "" && !usedColNames[colName] {
			return fmt.Errorf("message %s: %s references unknown column %q", msg.Desc.FullName(), label, colName)
		}
		return nil
	}
	checkColsExist := func(label string, colNames []string) error {
		for _, name := range colNames {
			if err := checkColExists(label, name); err != nil {
				return err
			}
		}
		return nil
	}
	if err := checkColsExist("unique_key_columns", uks); err != nil {
		return nil, err
	}
	if err := checkColsExist("duplicate_key_columns", dupKeys); err != nil {
		return nil, err
	}
	if err := checkColsExist("partition_by_columns", partCols); err != nil {
		return nil, err
	}
	if err := checkColsExist("inverted_index_columns", invCols); err != nil {
		return nil, err
	}
	if err := checkColsExist("ngram_bf_columns", ngramCols); err != nil {
		return nil, err
	}
	if err := checkColsExist("bloom_filter_columns", bloomCols); err != nil {
		return nil, err
	}
	if err := checkColExists("distributed_by_column", meta.DistributedByColumn); err != nil {
		return nil, err
	}
	if err := checkColExists("sequence_col", seqCol); err != nil {
		return nil, err
	}

	isIcebergOnly := meta.SyncTier == "ICEBERG_ONLY" ||
		(meta.DatabaseScope == "GLOBAL_TERMINOLOGY" && meta.SyncTier == "RELEASE_PARTITIONED" && len(uks) == 0 && len(dupKeys) == 0)

	// Apache Doris requires UNIQUE KEY and DUPLICATE KEY columns to precede value columns
	// in configured key order. Move key columns to the schema prefix while preserving
	// the relative order of all non-key columns.
	colByName := make(map[string]*dorisColumnIR, len(cols))
	for _, c := range cols {
		colByName[c.ColDesc.ColumnName] = c
	}
	keyCols := uks
	if len(keyCols) == 0 && len(dupKeys) > 0 {
		keyCols = dupKeys
	}
	if len(keyCols) > 0 {
		placed := make(map[string]bool, len(keyCols))
		reordered := make([]*dorisColumnIR, 0, len(cols))
		for _, k := range keyCols {
			if c, ok := colByName[k]; ok && !placed[k] {
				placed[k] = true
				if !isIcebergOnly {
					c.ColDesc.Nullable = false
				}
				reordered = append(reordered, c)
			}
		}
		for _, c := range cols {
			if !placed[c.ColDesc.ColumnName] {
				reordered = append(reordered, c)
			}
		}
		cols = reordered
	}

	if !isIcebergOnly {
		if len(uks) == 0 && len(dupKeys) == 0 && len(cols) > 0 {
			fallbackIdx := -1
			for i, c := range cols {
				if !c.IsOneofVariant {
					fallbackIdx = i
					break
				}
			}
			if fallbackIdx < 0 {
				return nil, fmt.Errorf("message %s: fallback DUPLICATE KEY column %q cannot be a oneof variant", msg.Desc.FullName(), cols[0].ColDesc.ColumnName)
			}
			if fallbackIdx > 0 {
				reordered := make([]*dorisColumnIR, 0, len(cols))
				reordered = append(reordered, cols[fallbackIdx])
				reordered = append(reordered, cols[:fallbackIdx]...)
				reordered = append(reordered, cols[fallbackIdx+1:]...)
				cols = reordered
			}
			cols[0].ColDesc.Nullable = false
			if !isDorisKeyEligibleType(cols[0].ColDesc.DorisType) {
				return nil, fmt.Errorf("message %s: fallback DUPLICATE KEY column %q has disallowed Doris key type %q", msg.Desc.FullName(), cols[0].ColDesc.ColumnName, cols[0].ColDesc.DorisType)
			}
		}
		for _, uk := range uks {
			if c := colByName[uk]; c != nil && !isDorisKeyEligibleType(c.ColDesc.DorisType) {
				return nil, fmt.Errorf("message %s: UNIQUE KEY column %q has disallowed Doris key type %q", msg.Desc.FullName(), uk, c.ColDesc.DorisType)
			}
		}
		for _, dk := range dupKeys {
			if c := colByName[dk]; c != nil && !isDorisKeyEligibleType(c.ColDesc.DorisType) {
				return nil, fmt.Errorf("message %s: DUPLICATE KEY column %q has disallowed Doris key type %q", msg.Desc.FullName(), dk, c.ColDesc.DorisType)
			}
		}
		if meta.DistributedByColumn != "" {
			if c := colByName[meta.DistributedByColumn]; c != nil && !isDorisKeyEligibleType(c.ColDesc.DorisType) {
				return nil, fmt.Errorf("message %s: distributed_by_column %q has disallowed Doris hash distribution type %q", msg.Desc.FullName(), meta.DistributedByColumn, c.ColDesc.DorisType)
			}
		}
	}
	if meta.EmbeddingDimension > 0 {
		if len(uks) > 0 {
			return nil, fmt.Errorf("message %s: embedding_dimension (%d) requires a DUPLICATE KEY table and cannot be combined with UNIQUE KEY columns", msg.Desc.FullName(), meta.EmbeddingDimension)
		}
		embCol := colByName["embedding"]
		if embCol == nil || embCol.ColDesc.Nullable || strings.ToUpper(strings.TrimSpace(embCol.ColDesc.DorisType)) != "ARRAY<FLOAT>" {
			return nil, fmt.Errorf("message %s: embedding_dimension (%d) requires a non-nullable ARRAY<FLOAT> column named \"embedding\"", msg.Desc.FullName(), meta.EmbeddingDimension)
		}
	}

	colDescs := make([]DorisColumnDescriptor, len(cols))
	for i, c := range cols {
		colDescs[i] = c.ColDesc
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
		NgramBfColumns:       ngramCols,
		BloomFilterColumns:   bloomCols,
		DistributedByColumn:  meta.DistributedByColumn,
		DuplicateKeyColumns:  dupKeys,
		FileCacheTTLSeconds:  meta.FileCacheTTLSeconds,
		EmbeddingDimension:   meta.EmbeddingDimension,
		StorageVaultName:     meta.StorageVaultName,
		IcebergCatalog:       meta.IcebergCatalog,
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
	dupSet map[string]bool,
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
	if colMeta.DefaultValue != "" && !isValidDorisDefaultValue(colMeta.DefaultValue) {
		return nil, fmt.Errorf("field %s: invalid default_value %q", field.Desc.FullName(), colMeta.DefaultValue)
	}

	isConflict := func(name string) bool {
		return usedGoNames[name] || (len(chain) > 0 && rootGoNames[name])
	}
	goFieldName := toPascalCase(protoFieldName)
	if len(chain) > 0 && isConflict(goFieldName) {
		for i := len(chain) - 1; i >= 0; i-- {
			parentPrefix := toPascalCase(string(chain[i].Desc.Name()))
			goFieldName = parentPrefix + goFieldName
			if !isConflict(goFieldName) {
				break
			}
		}
	}
	if isConflict(goFieldName) && !isConflict(toPascalCase(colName)) {
		goFieldName = toPascalCase(colName)
	}
	for isConflict(goFieldName) {
		goFieldName += "Column"
	}
	usedGoNames[goFieldName] = true

	isUK := colMeta.IsUniqueKey || ukSet[colName]
	isDupKey := dupSet[colName]
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

	nullable := isDorisColumnNullable(field, colName, isUK && tableMeta.SyncTier != "ICEBERG_ONLY", isDupKey && tableMeta.SyncTier != "ICEBERG_ONLY", isPK, isSeq)

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

	isRealOneof := field.Oneof != nil && !field.Oneof.Desc.IsSynthetic()
	usePtrForScalar := field.Desc.HasOptionalKeyword() || isRealOneof

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
			protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
			goFieldType = "DorisInt64Array"
			arrayKind = "int64"
		case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
			goFieldType = "DorisUint64Array"
			arrayKind = "uint64"
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
		IsOneofVariant: isRealOneof,
	}, nil
}

// isDorisColumnNullable determines whether a column allows SQL NULLs.
func isDorisColumnNullable(field *protogen.Field, colName string, isUK, isDupKey, isPK, isSeq bool) bool {
	if isUK || isDupKey || isPK || isSeq {
		return false
	}
	if field.Desc.IsList() {
		return false
	}
	if field.Desc.HasOptionalKeyword() || (field.Oneof != nil && !field.Oneof.Desc.IsSynthetic()) {
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
			protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
			return "ARRAY<BIGINT>"
		case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
			return "ARRAY<LARGEINT>"
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
	jsonRawMessage := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "encoding/json", GoName: "RawMessage"})
	jsonUnmarshal := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "encoding/json", GoName: "Unmarshal"})
	errorsJoin := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "errors", GoName: "Join"})
	fmtErrorf := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "fmt", GoName: "Errorf"})
	fmtSprintf := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "fmt", GoName: "Sprintf"})
	mathIsNaN := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "math", GoName: "IsNaN"})
	mathIsInf := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "math", GoName: "IsInf"})
	mathMinInt32 := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "math", GoName: "MinInt32"})
	mathMaxInt32 := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "math", GoName: "MaxInt32"})
	mathMaxUint32 := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "math", GoName: "MaxUint32"})
	mathMaxInt64 := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "math", GoName: "MaxInt64"})
	strconvAtoi := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strconv", GoName: "Atoi"})
	strconvItoa := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strconv", GoName: "Itoa"})
	strconvFormatUint := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strconv", GoName: "FormatUint"})
	strconvParseUint := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strconv", GoName: "ParseUint"})
	stringsBuilder := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "Builder"})
	stringsHasPrefix := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "HasPrefix"})
	stringsHasSuffix := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "HasSuffix"})
	stringsJoin := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "Join"})
	stringsReplaceAll := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "ReplaceAll"})
	stringsSplit := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "Split"})
	stringsToLower := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "ToLower"})
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
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"null element in Doris array is not supported: %q\", s)")
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
	g.P("\tvar ptrs []*string")
	g.P("\tif err := ", jsonUnmarshal, "([]byte(s), &ptrs); err == nil {")
	g.P("\t\tout := make(DorisStringArray, len(ptrs))")
	g.P("\t\tfor i, p := range ptrs {")
	g.P("\t\t\tif p == nil {")
	g.P("\t\t\t\treturn ", fmtErrorf, "(\"DorisStringArray.Scan: null element at index %d is not supported\", i)")
	g.P("\t\t\t}")
	g.P("\t\t\tout[i] = *p")
	g.P("\t\t}")
	g.P("\t\t*a = out")
	g.P("\t\treturn nil")
	g.P("\t} else {")
	g.P("\t\tparsed, err2 := parseDorisSingleQuotedArray(s)")
	g.P("\t\tif err2 != nil {")
	g.P("\t\t\treturn err")
	g.P("\t\t}")
	g.P("\t\t*a = DorisStringArray(parsed)")
	g.P("\t\treturn nil")
	g.P("\t}")
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

	g.P("// DorisUint64Array represents an Apache Doris ARRAY<LARGEINT> column.")
	g.P("type DorisUint64Array []uint64")
	g.P()
	g.P("// Value implements driver.Valuer.")
	g.P("func (a DorisUint64Array) Value() (", driverValue, ", error) {")
	g.P("\tif len(a) == 0 {")
	g.P("\t\treturn \"[]\", nil")
	g.P("\t}")
	g.P("\tb, err := ", jsonMarshal, "([]uint64(a))")
	g.P("\tif err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\treturn string(b), nil")
	g.P("}")
	g.P()
	g.P("// Scan implements sql.Scanner.")
	g.P("func (a *DorisUint64Array) Scan(src any) error {")
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
	g.P("\tcase DorisUint64Array:")
	g.P("\t\tcp := make(DorisUint64Array, len(v))")
	g.P("\t\tcopy(cp, v)")
	g.P("\t\t*a = cp")
	g.P("\t\treturn nil")
	g.P("\tcase []uint64:")
	g.P("\t\tcp := make(DorisUint64Array, len(v))")
	g.P("\t\tcopy(cp, v)")
	g.P("\t\t*a = cp")
	g.P("\t\treturn nil")
	g.P("\tdefault:")
	g.P("\t\treturn ", fmtErrorf, "(\"DorisUint64Array.Scan: unsupported type %T\", src)")
	g.P("\t}")
	g.P("\ts = ", stringsTrimSpace, "(s)")
	g.P("\tif s == \"\" || s == \"[]\" || s == \"null\" || s == \"NULL\" {")
	g.P("\t\t*a = DorisUint64Array{}")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tvar rawElems []", jsonRawMessage)
	g.P("\tif err := ", jsonUnmarshal, "([]byte(s), &rawElems); err == nil {")
	g.P("\t\tout := make(DorisUint64Array, len(rawElems))")
	g.P("\t\tfor i, elem := range rawElems {")
	g.P("\t\t\tes := ", stringsTrimSpace, "(string(elem))")
	g.P("\t\t\tif es == \"\" || es == \"null\" || es == \"NULL\" {")
	g.P("\t\t\t\treturn ", fmtErrorf, "(\"DorisUint64Array.Scan: null element at index %d is not supported\", i)")
	g.P("\t\t\t}")
	g.P("\t\t\tif len(es) >= 2 && es[0] == '\"' && es[len(es)-1] == '\"' {")
	g.P("\t\t\t\tvar unquoted string")
	g.P("\t\t\t\tif uErr := ", jsonUnmarshal, "(elem, &unquoted); uErr != nil {")
	g.P("\t\t\t\t\treturn uErr")
	g.P("\t\t\t\t}")
	g.P("\t\t\t\tes = ", stringsTrimSpace, "(unquoted)")
	g.P("\t\t\t}")
	g.P("\t\t\tu, parseErr := ", strconvParseUint, "(es, 10, 64)")
	g.P("\t\t\tif parseErr != nil {")
	g.P("\t\t\t\treturn parseErr")
	g.P("\t\t\t}")
	g.P("\t\t\tout[i] = u")
	g.P("\t\t}")
	g.P("\t\t*a = out")
	g.P("\t\treturn nil")
	g.P("\t} else {")
	g.P("\t\tparsed, err2 := parseDorisSingleQuotedArray(s)")
	g.P("\t\tif err2 != nil {")
	g.P("\t\t\treturn err")
	g.P("\t\t}")
	g.P("\t\tout := make(DorisUint64Array, len(parsed))")
	g.P("\t\tfor i, se := range parsed {")
	g.P("\t\t\tu, parseErr := ", strconvParseUint, "(", stringsTrimSpace, "(se), 10, 64)")
	g.P("\t\t\tif parseErr != nil {")
	g.P("\t\t\t\treturn parseErr")
	g.P("\t\t\t}")
	g.P("\t\t\tout[i] = u")
	g.P("\t\t}")
	g.P("\t\t*a = out")
	g.P("\t\treturn nil")
	g.P("\t}")
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
	g.P("\tif err := ", jsonUnmarshal, "([]byte(s), &ptrs); err == nil {")
	g.P("\t\tout := make(DorisBoolArray, len(ptrs))")
	g.P("\t\tfor i, p := range ptrs {")
	g.P("\t\t\tif p == nil {")
	g.P("\t\t\t\treturn ", fmtErrorf, "(\"DorisBoolArray.Scan: null element at index %d is not supported\", i)")
	g.P("\t\t\t}")
	g.P("\t\t\tout[i] = *p")
	g.P("\t\t}")
	g.P("\t\t*a = out")
	g.P("\t\treturn nil")
	g.P("\t} else {")
	g.P("\t\tvar intPtrs []*int")
	g.P("\t\tif err2 := ", jsonUnmarshal, "([]byte(s), &intPtrs); err2 != nil {")
	g.P("\t\t\treturn err")
	g.P("\t\t}")
	g.P("\t\tout := make(DorisBoolArray, len(intPtrs))")
	g.P("\t\tfor i, p := range intPtrs {")
	g.P("\t\t\tif p == nil {")
	g.P("\t\t\t\treturn ", fmtErrorf, "(\"DorisBoolArray.Scan: null element at index %d is not supported\", i)")
	g.P("\t\t\t}")
	g.P("\t\t\tif *p != 0 && *p != 1 {")
	g.P("\t\t\t\treturn err")
	g.P("\t\t\t}")
	g.P("\t\t\tout[i] = *p == 1")
	g.P("\t\t}")
	g.P("\t\t*a = out")
	g.P("\t\treturn nil")
	g.P("\t}")
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
	g.P("\tComment         string")
	g.P("}")
	g.P()

	g.P("// DorisIndexDescriptor describes a physical index on an Apache Doris table")
	g.P("// (INVERTED, NGRAM_BF, or ANN).")
	g.P("type DorisIndexDescriptor struct {")
	g.P("\tName       string")
	g.P("\tColumns    []string")
	g.P("\tUsing      string")
	g.P("\tProperties map[string]string")
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
	g.P("\tcase \"DATETIMEV2\", \"DATETIME\":")
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
	g.P("\tif (", stringsHasPrefix, "(dt, \"DATETIMEV2(\") || ", stringsHasPrefix, "(dt, \"DATETIME(\")) && ", stringsHasSuffix, "(dt, \")\") {")
	g.P("\t\treturn \"TIMESTAMPTZ\"")
	g.P("\t}")
	g.P("\tif ", stringsHasPrefix, "(dt, \"VARCHAR\") || dt == \"TEXT\" || dt == \"STRING\" || dt == \"JSON\" || dt == \"JSONB\" || dt == \"VARIANT\" {")
	g.P("\t\treturn \"STRING\"")
	g.P("\t}")
	g.P("\tif ", stringsHasPrefix, "(dt, \"DECIMALV3(\") {")
	g.P("\t\treturn \"DECIMAL(\" + ", stringsTrimPrefix, "(dt, \"DECIMALV3(\")")
	g.P("\t}")
	g.P("\tif ", stringsHasPrefix, "(dt, \"ARRAY<\") && ", stringsHasSuffix, "(dt, \">\") {")
	g.P("\t\tinner := ", stringsTrimSuffix, "(", stringsTrimPrefix, "(dt, \"ARRAY<\"), \">\")")
	g.P("\t\treturn \"LIST<\" + (DorisColumnDescriptor{DorisType: inner}).CanonicalIcebergSpecType() + \">\"")
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
	g.P("\tSequenceColumn       string")
	g.P("\tInvertedIndexColumns []string")
	g.P("\tNgramBfColumns       []string")
	g.P("\tBloomFilterColumns   []string")
	g.P("\tDistributedByColumn  string")
	g.P("\tDistributionColumns  []string")
	g.P("\tDuplicateKeyColumns  []string")
	g.P("\tFileCacheTTLSeconds  int32")
	g.P("\tEmbeddingDimension   int32")
	g.P("\tStorageVaultName     string")
	g.P("\tIcebergCatalog       string")
	g.P("\tIcebergPartitionSpec string")
	g.P("\tBuckets              int")
	g.P("\tAutoBuckets          bool")
	g.P("\tEnableMoW            bool")
	g.P("\tIndexes              []DorisIndexDescriptor")
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
	g.P("\tfirstAlpha := (s[0] >= 'a' && s[0] <= 'z') || (s[0] >= 'A' && s[0] <= 'Z')")
	g.P("\tif !firstAlpha {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tparenDepth := 0")
	g.P("\tangleDepth := 0")
	g.P("\tfor i := 0; i < len(s); i++ {")
	g.P("\t\tc := s[i]")
	g.P("\t\tswitch c {")
	g.P("\t\tcase '(':")
	g.P("\t\t\tif parenDepth > 0 {")
	g.P("\t\t\t\treturn false")
	g.P("\t\t\t}")
	g.P("\t\t\tparenDepth++")
	g.P("\t\tcase ')':")
	g.P("\t\t\tif i > 0 && (s[i-1] == '(' || s[i-1] == ',' || s[i-1] == ' ') {")
	g.P("\t\t\t\treturn false")
	g.P("\t\t\t}")
	g.P("\t\t\tparenDepth--")
	g.P("\t\t\tif parenDepth < 0 {")
	g.P("\t\t\t\treturn false")
	g.P("\t\t\t}")
	g.P("\t\tcase '<':")
	g.P("\t\t\tif parenDepth > 0 {")
	g.P("\t\t\t\treturn false")
	g.P("\t\t\t}")
	g.P("\t\t\tangleDepth++")
	g.P("\t\tcase '>':")
	g.P("\t\t\tif parenDepth > 0 || (i > 0 && (s[i-1] == '<' || s[i-1] == ',' || s[i-1] == ' ')) {")
	g.P("\t\t\t\treturn false")
	g.P("\t\t\t}")
	g.P("\t\t\tangleDepth--")
	g.P("\t\t\tif angleDepth < 0 {")
	g.P("\t\t\t\treturn false")
	g.P("\t\t\t}")
	g.P("\t\tcase ',':")
	g.P("\t\t\tif (parenDepth == 0 && angleDepth == 0) || i == 0 || s[i-1] == '(' || s[i-1] == '<' || s[i-1] == ',' || s[i-1] == ' ' {")
	g.P("\t\t\t\treturn false")
	g.P("\t\t\t}")
	g.P("\t\tcase ' ':")
	g.P("\t\t\tif (parenDepth == 0 && angleDepth == 0) || i == 0 || s[i-1] != ',' {")
	g.P("\t\t\t\treturn false")
	g.P("\t\t\t}")
	g.P("\t\tdefault:")
	g.P("\t\t\tif parenDepth > 0 {")
	g.P("\t\t\t\tif c < '0' || c > '9' {")
	g.P("\t\t\t\t\treturn false")
	g.P("\t\t\t\t}")
	g.P("\t\t\t} else {")
	g.P("\t\t\t\tok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'")
	g.P("\t\t\t\tif !ok {")
	g.P("\t\t\t\t\treturn false")
	g.P("\t\t\t\t}")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn parenDepth == 0 && angleDepth == 0")
	g.P("}")
	g.P()

	g.P("func isValidDorisDatabaseScope(scope string) bool {")
	g.P("\tswitch scope {")
	g.P("\tcase \"\", \"UNSPECIFIED\", \"TENANT\", \"GLOBAL_TERMINOLOGY\", \"GLOBAL_TERMINOLOGY_RAG\", \"GLOBAL_TELEMETRY\":")
	g.P("\t\treturn true")
	g.P("\tdefault:")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("}")
	g.P()

	g.P("func isValidDorisSyncTier(tier string) bool {")
	g.P("\tswitch tier {")
	g.P("\tcase \"\", \"UNSPECIFIED\", \"WHOLE_TABLE\", \"MONTHLY_PARTITIONED\", \"RELEASE_PARTITIONED\", \"INTERNAL_ONLY\", \"ICEBERG_ONLY\":")
	g.P("\t\treturn true")
	g.P("\tdefault:")
	g.P("\t\treturn false")
	g.P("\t}")
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

	g.P("func isValidDorisDefaultValue(s string) bool {")
	g.P("\tfor i := 0; i < len(s); i++ {")
	g.P("\t\tc := s[i]")
	g.P("\t\tif c < 0x20 || c > 0x7e {")
	g.P("\t\t\treturn false")
	g.P("\t\t}")
	g.P("\t\tswitch c {")
	g.P("\t\tcase '\"', '\\'', '`', '\\\\', ';':")
	g.P("\t\t\treturn false")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn true")
	g.P("}")
	g.P()

	g.P("func isDorisKeyEligibleType(dorisType string) bool {")
	g.P("\tdt := ", stringsToUpper, "(", stringsTrimSpace, "(dorisType))")
	g.P("\tif dt == \"\" {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tswitch dt {")
	g.P("\tcase \"FLOAT\", \"DOUBLE\", \"STRING\", \"TEXT\", \"JSON\", \"JSONB\", \"VARIANT\", \"HLL\", \"BITMAP\", \"QUANTILE_STATE\":")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\treturn !", stringsHasPrefix, "(dt, \"ARRAY<\") && !", stringsHasPrefix, "(dt, \"MAP<\") && !", stringsHasPrefix, "(dt, \"STRUCT<\")")
	g.P("}")
	g.P()

	g.P("func dedupDorisColumnList(cols []string) []string {")
	g.P("\tif len(cols) == 0 {")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\tseen := make(map[string]bool, len(cols))")
	g.P("\tout := make([]string, 0, len(cols))")
	g.P("\tfor _, c := range cols {")
	g.P("\t\tif c != \"\" && !seen[c] {")
	g.P("\t\t\tseen[c] = true")
	g.P("\t\t\tout = append(out, c)")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn out")
	g.P("}")
	g.P()

	g.P("func reorderDorisDescriptorColumns(cols []DorisColumnDescriptor, uks, dupKeys []string, forceKeyNonNull bool) []DorisColumnDescriptor {")
	g.P("\tkeyCols := uks")
	g.P("\tif len(keyCols) == 0 && len(dupKeys) > 0 {")
	g.P("\t\tkeyCols = dupKeys")
	g.P("\t}")
	g.P("\tif len(keyCols) == 0 {")
	g.P("\t\treturn cols")
	g.P("\t}")
	g.P("\tcolByName := make(map[string]DorisColumnDescriptor, len(cols))")
	g.P("\tfor _, c := range cols {")
	g.P("\t\tcolByName[c.ColumnName] = c")
	g.P("\t}")
	g.P("\tplaced := make(map[string]bool, len(keyCols))")
	g.P("\treordered := make([]DorisColumnDescriptor, 0, len(cols))")
	g.P("\tfor _, k := range keyCols {")
	g.P("\t\tif c, ok := colByName[k]; ok && !placed[k] {")
	g.P("\t\t\tplaced[k] = true")
	g.P("\t\t\tif forceKeyNonNull {")
	g.P("\t\t\t\tc.Nullable = false")
	g.P("\t\t\t}")
	g.P("\t\t\treordered = append(reordered, c)")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tfor _, c := range cols {")
	g.P("\t\tif !placed[c.ColumnName] {")
	g.P("\t\t\treordered = append(reordered, c)")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn reordered")
	g.P("}")
	g.P()

	g.P("func validateDorisDescriptor(t DorisTableDescriptor) bool {")
	g.P("\tif !isValidDorisSQLIdentifier(t.TableName) || !isValidDorisDatabaseScope(t.DatabaseScope) || !isValidDorisSyncTier(t.SyncTier) {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tif t.ColocateGroup != \"\" && !isValidDorisSQLIdentifier(t.ColocateGroup) {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tif t.StorageVaultName != \"\" && !isValidDorisSQLIdentifier(t.StorageVaultName) {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tif t.IcebergCatalog != \"\" && !isValidDorisSQLIdentifier(t.IcebergCatalog) {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tif t.EmbeddingDimension < 0 {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tcolSet := make(map[string]bool, len(t.Columns))")
	g.P("\tfor _, c := range t.Columns {")
	g.P("\t\tif !isValidDorisSQLIdentifier(c.ColumnName) || colSet[c.ColumnName] {")
	g.P("\t\t\treturn false")
	g.P("\t\t}")
	g.P("\t\tif c.DorisType != \"\" && !isValidDorisSQLTypeSyntax(c.DorisType) {")
	g.P("\t\t\treturn false")
	g.P("\t\t}")
	g.P("\t\tif c.IcebergType != \"\" && !isValidDorisSQLTypeSyntax(c.IcebergType) {")
	g.P("\t\t\treturn false")
	g.P("\t\t}")
	g.P("\t\tif c.DefaultValue != \"\" && !isValidDorisDefaultValue(c.DefaultValue) {")
	g.P("\t\t\treturn false")
	g.P("\t\t}")
	g.P("\t\tcolSet[c.ColumnName] = true")
	g.P("\t}")
	g.P("\tif t.SequenceCol != \"\" && (!isValidDorisSQLIdentifier(t.SequenceCol) || !colSet[t.SequenceCol]) {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tif t.SequenceColumn != \"\" && (!isValidDorisSQLIdentifier(t.SequenceColumn) || !colSet[t.SequenceColumn]) {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tif t.DistributedByColumn != \"\" && (!isValidDorisSQLIdentifier(t.DistributedByColumn) || !colSet[t.DistributedByColumn]) {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tfor _, list := range [][]string{t.UniqueKeyColumns, t.PartitionByColumns, t.InvertedIndexColumns, t.NgramBfColumns, t.BloomFilterColumns, t.DuplicateKeyColumns, t.DistributionColumns} {")
	g.P("\t\tfor _, id := range list {")
	g.P("\t\t\tif !isValidDorisSQLIdentifier(id) || !colSet[id] {")
	g.P("\t\t\t\treturn false")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tfor _, idx := range t.Indexes {")
	g.P("\t\tif idx.Name != \"\" && !isValidDorisSQLIdentifier(idx.Name) {")
	g.P("\t\t\treturn false")
	g.P("\t\t}")
	g.P("\t\tfor _, id := range idx.Columns {")
	g.P("\t\t\tif !isValidDorisSQLIdentifier(id) || !colSet[id] {")
	g.P("\t\t\t\treturn false")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn true")
	g.P("}")
	g.P()

	g.P("func isDorisReservedKeyword(colName string) bool {")
	g.P("\tswitch ", stringsToLower, "(colName) {")
	g.P("\tcase \"system\", \"key\", \"keys\", \"order\", \"range\", \"values\", \"index\", \"rank\",")
	g.P("\t\t\"group\", \"table\", \"tables\", \"database\", \"databases\", \"schema\", \"schemas\",")
	g.P("\t\t\"partition\", \"partitions\", \"buckets\", \"desc\", \"describe\", \"asc\", \"default\",")
	g.P("\t\t\"primary\", \"unique\", \"duplicate\", \"aggregate\", \"check\", \"references\",")
	g.P("\t\t\"foreign\", \"constraint\", \"select\", \"from\", \"where\", \"join\", \"inner\",")
	g.P("\t\t\"left\", \"right\", \"full\", \"outer\", \"cross\", \"on\", \"using\", \"in\", \"is\",")
	g.P("\t\t\"null\", \"not\", \"and\", \"or\", \"xor\", \"as\", \"by\", \"limit\", \"offset\",")
	g.P("\t\t\"having\", \"union\", \"intersect\", \"except\", \"all\", \"distinct\", \"case\",")
	g.P("\t\t\"when\", \"then\", \"else\", \"end\", \"exists\", \"between\", \"like\", \"ilike\",")
	g.P("\t\t\"regexp\", \"rlike\", \"div\", \"mod\", \"interval\", \"function\", \"procedure\",")
	g.P("\t\t\"trigger\", \"view\", \"grant\", \"revoke\", \"show\", \"explain\", \"use\", \"set\",")
	g.P("\t\t\"kill\", \"load\", \"export\", \"backup\", \"restore\", \"alter\", \"create\", \"drop\",")
	g.P("\t\t\"truncate\", \"rename\", \"replace\", \"insert\", \"update\", \"delete\", \"merge\",")
	g.P("\t\t\"into\", \"over\", \"window\", \"rows\", \"preceding\", \"following\", \"current\",")
	g.P("\t\t\"true\", \"false\", \"properties\", \"inverted\", \"ngram_bf\":")
	g.P("\t\treturn true")
	g.P("\tdefault:")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("}")
	g.P()

	g.P("func formatDorisDDLColumnIdent(colName string) string {")
	g.P("\tif isDorisReservedKeyword(colName) {")
	g.P("\t\treturn \"`\" + colName + \"`\"")
	g.P("\t}")
	g.P("\treturn colName")
	g.P("}")
	g.P()

	g.P("func formatDorisDDLColumnIdentList(cols []string) string {")
	g.P("\tformatted := make([]string, len(cols))")
	g.P("\tfor i, c := range cols {")
	g.P("\t\tformatted[i] = formatDorisDDLColumnIdent(c)")
	g.P("\t}")
	g.P("\treturn ", stringsJoin, "(formatted, \", \")")
	g.P("}")
	g.P()

	g.P("func resolveDorisEmbeddingDimension(t DorisTableDescriptor) (int32, bool) {")
	g.P("\tif t.EmbeddingDimension > 0 {")
	g.P("\t\treturn t.EmbeddingDimension, true")
	g.P("\t}")
	g.P("\tif t.EmbeddingDimension == 0 && t.TableName == \"clinical_concept_search\" {")
	g.P("\t\treturn 768, true")
	g.P("\t}")
	g.P("\treturn 0, false")
	g.P("}")
	g.P()

	// Emit BuildDorisCreateTableSQL and BuildIcebergCreateTableSQL on DorisTableDescriptor.
	g.P("func dorisColumnIcebergDDLType(dorisType, icebergType string) string {")
	g.P("\tdt := ", stringsToUpper, "(", stringsTrimSpace, "(dorisType))")
	g.P("\tswitch dt {")
	g.P("\tcase \"DATEV2\":")
	g.P("\t\treturn \"DATE\"")
	g.P("\tcase \"DATETIMEV2\":")
	g.P("\t\treturn \"DATETIME\"")
	g.P("\tcase \"LARGEINT\":")
	g.P("\t\treturn \"DECIMAL(38, 0)\"")
	g.P("\tcase \"ARRAY<LARGEINT>\":")
	g.P("\t\treturn \"ARRAY<DECIMAL(38, 0)>\"")
	g.P("\tcase \"JSON\", \"JSONB\", \"VARIANT\":")
	g.P("\t\treturn \"STRING\"")
	g.P("\t}")
	g.P("\tif ", stringsHasPrefix, "(dt, \"DATETIMEV2(\") && ", stringsHasSuffix, "(dt, \")\") {")
	g.P("\t\treturn \"DATETIME\"")
	g.P("\t}")
	g.P("\tif ", stringsHasPrefix, "(dt, \"DECIMALV3(\") {")
	g.P("\t\trest := ", stringsTrimPrefix, "(dt, \"DECIMALV3(\")")
	g.P("\t\trest = ", stringsReplaceAll, "(rest, \", \", \",\")")
	g.P("\t\trest = ", stringsReplaceAll, "(rest, \",\", \", \")")
	g.P("\t\treturn \"DECIMAL(\" + rest")
	g.P("\t}")
	g.P("\tif ", stringsHasPrefix, "(dt, \"ARRAY<\") && ", stringsHasSuffix, "(dt, \">\") {")
	g.P("\t\tinner := ", stringsTrimSuffix, "(", stringsTrimPrefix, "(dt, \"ARRAY<\"), \">\")")
	g.P("\t\treturn \"ARRAY<\" + dorisColumnIcebergDDLType(inner, \"\") + \">\"")
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
	g.P("\treturn ", stringsToUpper, "(it)")
	g.P("}")
	g.P()

	g.P("func dorisColumnStarRocksDDLType(dorisType, icebergType string) string {")
	g.P("\tdt := ", stringsToUpper, "(", stringsTrimSpace, "(dorisType))")
	g.P("\tswitch dt {")
	g.P("\tcase \"DATEV2\", \"DATE\":")
	g.P("\t\treturn \"DATE\"")
	g.P("\tcase \"DATETIMEV2\", \"DATETIME\":")
	g.P("\t\treturn \"DATETIME\"")
	g.P("\tcase \"TEXT\", \"STRING\":")
	g.P("\t\treturn \"STRING\"")
	g.P("\t}")
	g.P("\tif (", stringsHasPrefix, "(dt, \"DATETIMEV2(\") || ", stringsHasPrefix, "(dt, \"DATETIME(\")) && ", stringsHasSuffix, "(dt, \")\") {")
	g.P("\t\treturn \"DATETIME\"")
	g.P("\t}")
	g.P("\tif ", stringsHasPrefix, "(dt, \"DECIMALV3(\") {")
	g.P("\t\trest := ", stringsTrimPrefix, "(dt, \"DECIMALV3(\")")
	g.P("\t\trest = ", stringsReplaceAll, "(rest, \", \", \",\")")
	g.P("\t\trest = ", stringsReplaceAll, "(rest, \",\", \", \")")
	g.P("\t\treturn \"DECIMAL(\" + rest")
	g.P("\t}")
	g.P("\tif ", stringsHasPrefix, "(dt, \"DECIMAL(\") {")
	g.P("\t\trest := ", stringsTrimPrefix, "(dt, \"DECIMAL(\")")
	g.P("\t\trest = ", stringsReplaceAll, "(rest, \", \", \",\")")
	g.P("\t\trest = ", stringsReplaceAll, "(rest, \",\", \", \")")
	g.P("\t\treturn \"DECIMAL(\" + rest")
	g.P("\t}")
	g.P("\tif ", stringsHasPrefix, "(dt, \"ARRAY<\") && ", stringsHasSuffix, "(dt, \">\") {")
	g.P("\t\tinner := ", stringsTrimSuffix, "(", stringsTrimPrefix, "(dt, \"ARRAY<\"), \">\")")
	g.P("\t\treturn \"ARRAY<\" + dorisColumnStarRocksDDLType(inner, \"\") + \">\"")
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
	g.P("\treturn ", stringsToUpper, "(it)")
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
	g.P("\tif t.DistributedByColumn != \"\" && !isDorisKeyEligibleType(colTypes[t.DistributedByColumn]) {")
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
	g.P("\tfor _, c := range orderedCols {")
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
	g.P("\t\t} else if c.DefaultValue != \"\" {")
	g.P("\t\t\tline += ", fmtSprintf, "(\" DEFAULT %q\", c.DefaultValue)")
	g.P("\t\t}")
	g.P("\t\tbodyLines = append(bodyLines, line)")
	g.P("\t}")
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
	g.P("\t\t\tbodyLines = append(bodyLines, ", fmtSprintf, "(`    INDEX idx_%s_bm25 (%s) USING INVERTED PROPERTIES (\"parser\" = \"english\", \"support_phrase\" = \"true\", \"lower_case\" = \"true\")`, idxCol, formatDorisDDLColumnIdent(idxCol)))")
	g.P("\t\t} else {")
	g.P("\t\t\tbodyLines = append(bodyLines, ", fmtSprintf, "(\"    INDEX idx_%s_%s (%s) USING INVERTED\", t.TableName, idxCol, formatDorisDDLColumnIdent(idxCol)))")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tfor _, ngCol := range ngramCols {")
	g.P("\t\tbodyLines = append(bodyLines, ", fmtSprintf, "(`    INDEX idx_ngram_%s_%s (%s) USING NGRAM_BF PROPERTIES (\"gram_size\" = \"3\", \"bf_size\" = \"1024\")`, t.TableName, ngCol, formatDorisDDLColumnIdent(ngCol)))")
	g.P("\t}")
	g.P("\tif len(uks) == 0 {")
	g.P("\t\tif embDim, ok := resolveDorisEmbeddingDimension(t); ok {")
	g.P("\t\t\tfor _, c := range orderedCols {")
	g.P("\t\t\t\tif c.ColumnName == \"embedding\" && !c.Nullable && colTypes[c.ColumnName] == \"ARRAY<FLOAT>\" {")
	g.P("\t\t\t\t\tbodyLines = append(bodyLines, ", fmtSprintf, "(`    INDEX idx_%s_hnsw (%s) USING ANN PROPERTIES (\"index_type\" = \"hnsw\", \"metric_type\" = \"l2_distance\", \"dim\" = \"%d\")`, c.ColumnName, formatDorisDDLColumnIdent(c.ColumnName), embDim))")
	g.P("\t\t\t\t}")
	g.P("\t\t\t}")
	g.P("\t\t}")
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
	g.P("\tvar b ", stringsBuilder)
	g.P("\tb.WriteString(\"CREATE TABLE IF NOT EXISTS \")")
	g.P("\tb.WriteString(fullTableName)")
	g.P("\tb.WriteString(\" (\\n\")")
	g.P("\tb.WriteString(", stringsJoin, "(bodyLines, \",\\n\"))")
	g.P("\tb.WriteString(\"\\n)\\n\")")
	g.P("\tvar distCol string")
	g.P("\tif len(uks) > 0 {")
	g.P("\t\tb.WriteString(\"UNIQUE KEY(\")")
	g.P("\t\tb.WriteString(formatDorisDDLColumnIdentList(uks))")
	g.P("\t\tb.WriteString(\")\\n\")")
	g.P("\t\tdistCol = t.DistributedByColumn")
	g.P("\t\tif distCol == \"\" {")
	g.P("\t\t\tif len(uks) > 1 && uks[0] == \"tenant_id\" {")
	g.P("\t\t\t\tdistCol = uks[1]")
	g.P("\t\t\t} else {")
	g.P("\t\t\t\tdistCol = uks[0]")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t} else if len(dupKeys) > 0 {")
	g.P("\t\tb.WriteString(\"DUPLICATE KEY(\")")
	g.P("\t\tb.WriteString(formatDorisDDLColumnIdentList(dupKeys))")
	g.P("\t\tb.WriteString(\")\\n\")")
	g.P("\t\tdistCol = t.DistributedByColumn")
	g.P("\t\tif distCol == \"\" {")
	g.P("\t\t\tdistCol = dupKeys[0]")
	g.P("\t\t}")
	g.P("\t} else if len(orderedCols) > 0 {")
	g.P("\t\tb.WriteString(\"DUPLICATE KEY(\")")
	g.P("\t\tb.WriteString(formatDorisDDLColumnIdent(orderedCols[0].ColumnName))")
	g.P("\t\tb.WriteString(\")\\n\")")
	g.P("\t\tdistCol = t.DistributedByColumn")
	g.P("\t\tif distCol == \"\" {")
	g.P("\t\t\tdistCol = orderedCols[0].ColumnName")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tif len(partCols) > 0 {")
	g.P("\t\tpartKind := \"LIST\"")
	g.P("\t\tif ", stringsHasPrefix, "(colTypes[partCols[0]], \"DATE\") {")
	g.P("\t\t\tpartKind = \"RANGE\"")
	g.P("\t\t}")
	g.P("\t\tb.WriteString(\"PARTITION BY \")")
	g.P("\t\tb.WriteString(partKind)")
	g.P("\t\tb.WriteString(\"(\")")
	g.P("\t\tb.WriteString(formatDorisDDLColumnIdentList(partCols))")
	g.P("\t\tb.WriteString(\") ()\\n\")")
	g.P("\t}")
	g.P("\tif distCol != \"\" {")
	g.P("\t\tb.WriteString(\"DISTRIBUTED BY HASH(\")")
	g.P("\t\tb.WriteString(formatDorisDDLColumnIdent(distCol))")
	g.P("\t\tb.WriteString(\") BUCKETS \")")
	g.P("\t\tb.WriteString(bucketStr)")
	g.P("\t\tb.WriteString(\"\\n\")")
	g.P("\t}")
	g.P("\tvar props []string")
	g.P("\tif t.StorageVaultName != \"\" {")
	g.P("\t\tprops = append(props, ", fmtSprintf, "(`    \"storage_vault_name\" = %q`, t.StorageVaultName))")
	g.P("\t}")
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
	g.P("\tif len(bloomCols) > 0 {")
	g.P("\t\tprops = append(props, ", fmtSprintf, "(`    \"bloom_filter_columns\" = %q`, ", stringsJoin, "(bloomCols, \", \")))")
	g.P("\t}")
	g.P("\tb.WriteString(\"PROPERTIES (\\n\")")
	g.P("\tb.WriteString(", stringsJoin, "(props, \",\\n\"))")
	g.P("\tb.WriteString(\"\\n);\")")
	g.P("\treturn b.String()")
	g.P("}")
	g.P()

	g.P("// BuildStarRocksCreateTableSQL renders a deterministic StarRocks 4.x")
	g.P("// CREATE TABLE IF NOT EXISTS statement for this table descriptor.")
	g.P("func (t DorisTableDescriptor) BuildStarRocksCreateTableSQL(tenantSlug string, buckets int) string {")
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
	g.P("\t\tcolTypes[c.ColumnName] = dorisColumnStarRocksDDLType(c.DorisType, c.IcebergType)")
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
	g.P("\t\tif !isDorisKeyEligibleType(colTypes[orderedCols[0].ColumnName]) {")
	g.P("\t\t\treturn \"\"")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tif t.DistributedByColumn != \"\" && !isDorisKeyEligibleType(colTypes[t.DistributedByColumn]) {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tfor _, dc := range dedupDorisColumnList(t.DistributionColumns) {")
	g.P("\t\tif !isDorisKeyEligibleType(colTypes[dc]) {")
	g.P("\t\t\treturn \"\"")
	g.P("\t\t}")
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
	g.P("\tfor _, c := range orderedCols {")
	g.P("\t\tcolIdent := formatDorisDDLColumnIdent(c.ColumnName)")
	g.P("\t\tsrType := colTypes[c.ColumnName]")
	g.P("\t\tline := ", fmtSprintf, "(\"    %s %s\", colIdent, srType)")
	g.P("\t\tif !c.Nullable {")
	g.P("\t\t\tline += \" NOT NULL\"")
	g.P("\t\t\tif c.DefaultValue != \"\" {")
	g.P("\t\t\t\tline += ", fmtSprintf, "(\" DEFAULT %q\", c.DefaultValue)")
	g.P("\t\t\t} else if ", stringsHasPrefix, "(srType, \"ARRAY<\") && c.ColumnName != \"embedding\" {")
	g.P("\t\t\t\tline += ` DEFAULT \"[]\"`")
	g.P("\t\t\t} else if c.ColumnName == \"record_status\" {")
	g.P("\t\t\t\tline += ` DEFAULT \"ACTIVE\"`")
	g.P("\t\t\t} else if c.IsSequenceCol {")
	g.P("\t\t\t\tline += ` DEFAULT \"0\"`")
	g.P("\t\t\t} else if srType == \"BOOLEAN\" && !c.IsUniqueKey {")
	g.P("\t\t\t\tline += ` DEFAULT \"false\"`")
	g.P("\t\t\t} else if (srType == \"SMALLINT\" || srType == \"INT\") && !c.IsUniqueKey && (", stringsHasSuffix, "(c.ColumnName, \"_count\") || c.ColumnName == \"display_order\") {")
	g.P("\t\t\t\tline += ` DEFAULT \"0\"`")
	g.P("\t\t\t} else if srType == \"DOUBLE\" && !c.IsUniqueKey && c.ColumnName == \"additive_offset\" {")
	g.P("\t\t\t\tline += ` DEFAULT \"0.0\"`")
	g.P("\t\t\t}")
	g.P("\t\t} else if c.DefaultValue != \"\" {")
	g.P("\t\t\tline += ", fmtSprintf, "(\" DEFAULT %q\", c.DefaultValue)")
	g.P("\t\t}")
	g.P("\t\tbodyLines = append(bodyLines, line)")
	g.P("\t}")
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
	g.P("\t\tif colTypes[idxCol] == \"STRING\" || colTypes[idxCol] == \"TEXT\" {")
	g.P("\t\t\tbodyLines = append(bodyLines, ", fmtSprintf, "(`    INDEX idx_%s_bm25 (%s) USING GIN(\"parser\" = \"english\")`, idxCol, formatDorisDDLColumnIdent(idxCol)))")
	g.P("\t\t} else if !", stringsHasPrefix, "(colTypes[idxCol], \"ARRAY<\") {")
	g.P("\t\t\tbodyLines = append(bodyLines, ", fmtSprintf, "(\"    INDEX idx_%s_%s (%s) USING BITMAP\", t.TableName, idxCol, formatDorisDDLColumnIdent(idxCol)))")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tfor _, ngCol := range ngramCols {")
	g.P("\t\tbodyLines = append(bodyLines, ", fmtSprintf, "(`    INDEX idx_ngram_%s_%s (%s) USING NGRAMBF (\"gram_num\" = \"3\", \"bloom_filter_fpp\" = \"0.05\")`, t.TableName, ngCol, formatDorisDDLColumnIdent(ngCol)))")
	g.P("\t}")
	g.P("\tif len(uks) == 0 {")
	g.P("\t\tif embDim, ok := resolveDorisEmbeddingDimension(t); ok {")
	g.P("\t\t\tfor _, c := range orderedCols {")
	g.P("\t\t\t\tif c.ColumnName == \"embedding\" && !c.Nullable && colTypes[c.ColumnName] == \"ARRAY<FLOAT>\" {")
	g.P("\t\t\t\t\tbodyLines = append(bodyLines, ", fmtSprintf, "(`    INDEX idx_%s_hnsw (%s) USING VECTOR (\"index_type\" = \"hnsw\", \"metric_type\" = \"l2_distance\", \"dim\" = \"%d\")`, c.ColumnName, formatDorisDDLColumnIdent(c.ColumnName), embDim))")
	g.P("\t\t\t\t}")
	g.P("\t\t\t}")
	g.P("\t\t}")
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
	g.P("\tvar b ", stringsBuilder)
	g.P("\tb.WriteString(\"CREATE TABLE IF NOT EXISTS \")")
	g.P("\tb.WriteString(fullTableName)")
	g.P("\tb.WriteString(\" (\\n\")")
	g.P("\tb.WriteString(", stringsJoin, "(bodyLines, \",\\n\"))")
	g.P("\tb.WriteString(\"\\n)\\n\")")
	g.P("\tvar distCol string")
	g.P("\tif len(uks) > 0 {")
	g.P("\t\tb.WriteString(\"PRIMARY KEY(\")")
	g.P("\t\tb.WriteString(formatDorisDDLColumnIdentList(uks))")
	g.P("\t\tb.WriteString(\")\\n\")")
	g.P("\t\tdistCol = t.DistributedByColumn")
	g.P("\t\tif distCol == \"\" && len(t.DistributionColumns) > 0 {")
	g.P("\t\t\tdistCol = t.DistributionColumns[0]")
	g.P("\t\t}")
	g.P("\t\tif distCol == \"\" {")
	g.P("\t\t\tif len(uks) > 1 && uks[0] == \"tenant_id\" {")
	g.P("\t\t\t\tdistCol = uks[1]")
	g.P("\t\t\t} else {")
	g.P("\t\t\t\tdistCol = uks[0]")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t} else if len(dupKeys) > 0 {")
	g.P("\t\tb.WriteString(\"DUPLICATE KEY(\")")
	g.P("\t\tb.WriteString(formatDorisDDLColumnIdentList(dupKeys))")
	g.P("\t\tb.WriteString(\")\\n\")")
	g.P("\t\tdistCol = t.DistributedByColumn")
	g.P("\t\tif distCol == \"\" && len(t.DistributionColumns) > 0 {")
	g.P("\t\t\tdistCol = t.DistributionColumns[0]")
	g.P("\t\t}")
	g.P("\t\tif distCol == \"\" {")
	g.P("\t\t\tdistCol = dupKeys[0]")
	g.P("\t\t}")
	g.P("\t} else if len(orderedCols) > 0 {")
	g.P("\t\tb.WriteString(\"DUPLICATE KEY(\")")
	g.P("\t\tb.WriteString(formatDorisDDLColumnIdent(orderedCols[0].ColumnName))")
	g.P("\t\tb.WriteString(\")\\n\")")
	g.P("\t\tdistCol = t.DistributedByColumn")
	g.P("\t\tif distCol == \"\" && len(t.DistributionColumns) > 0 {")
	g.P("\t\t\tdistCol = t.DistributionColumns[0]")
	g.P("\t\t}")
	g.P("\t\tif distCol == \"\" {")
	g.P("\t\t\tdistCol = orderedCols[0].ColumnName")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tif len(partCols) > 0 {")
	g.P("\t\tpartKind := \"LIST\"")
	g.P("\t\tif ", stringsHasPrefix, "(colTypes[partCols[0]], \"DATE\") {")
	g.P("\t\t\tpartKind = \"RANGE\"")
	g.P("\t\t}")
	g.P("\t\tb.WriteString(\"PARTITION BY \")")
	g.P("\t\tb.WriteString(partKind)")
	g.P("\t\tb.WriteString(\"(\")")
	g.P("\t\tb.WriteString(formatDorisDDLColumnIdentList(partCols))")
	g.P("\t\tb.WriteString(\") ()\\n\")")
	g.P("\t}")
	g.P("\tif distCol != \"\" {")
	g.P("\t\tb.WriteString(\"DISTRIBUTED BY HASH(\")")
	g.P("\t\tb.WriteString(formatDorisDDLColumnIdent(distCol))")
	g.P("\t\tb.WriteString(\") BUCKETS \")")
	g.P("\t\tb.WriteString(bucketStr)")
	g.P("\t\tb.WriteString(\"\\n\")")
	g.P("\t}")
	g.P("\tvar props []string")
	g.P("\tif t.StorageVaultName != \"\" {")
	g.P("\t\tprops = append(props, ", fmtSprintf, "(`    \"storage_volume\" = %q`, t.StorageVaultName))")
	g.P("\t\tprops = append(props, `    \"datacache.enable\" = \"true\"`)")
	g.P("\t}")
	g.P("\tprops = append(props, `    \"compression\" = \"zstd\"`)")
	g.P("\tif len(uks) > 0 || t.EnableMoW {")
	g.P("\t\tprops = append(props, `    \"enable_persistent_index\" = \"true\"`)")
	g.P("\t}")
	g.P("\tseqCol := ", stringsTrimSpace, "(t.SequenceCol)")
	g.P("\tif seqCol == \"\" {")
	g.P("\t\tseqCol = ", stringsTrimSpace, "(t.SequenceColumn)")
	g.P("\t}")
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
	g.P("\tif len(bloomCols) > 0 {")
	g.P("\t\tprops = append(props, ", fmtSprintf, "(`    \"bloom_filter_columns\" = %q`, ", stringsJoin, "(bloomCols, \", \")))")
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
	g.P("\t\tcatalog = \"iceberg\"")
	g.P("\t}")
	g.P("\tif !isValidDorisSQLIdentifier(catalog) {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tvar fullTableName string")
	g.P("\tswitch t.DatabaseScope {")
	g.P("\tcase \"GLOBAL_TERMINOLOGY\":")
	g.P("\t\tfullTableName = catalog + \".terminology.\" + t.TableName")
	g.P("\tcase \"GLOBAL_TELEMETRY\":")
	g.P("\t\tfullTableName = catalog + \".telemetry.\" + t.TableName")
	g.P("\tcase \"GLOBAL_TERMINOLOGY_RAG\":")
	g.P("\t\tfullTableName = catalog + \".terminology_rag.\" + t.TableName")
	g.P("\tdefault:")
	g.P("\t\tfullTableName = catalog + \".tenant_\" + slug + \".\" + t.TableName")
	g.P("\t}")
	g.P("\tvar bodyLines []string")
	g.P("\tfor _, c := range orderedCols {")
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
	g.P("\tvar b ", stringsBuilder)
	g.P("\tb.WriteString(\"CREATE TABLE IF NOT EXISTS \")")
	g.P("\tb.WriteString(fullTableName)")
	g.P("\tb.WriteString(\" (\\n\")")
	g.P("\tb.WriteString(", stringsJoin, "(bodyLines, \",\\n\"))")
	g.P("\tb.WriteString(\"\\n)\\n\")")
	g.P("\tif len(partCols) > 0 {")
	g.P("\t\tb.WriteString(\"PARTITION BY LIST (\")")
	g.P("\t\tb.WriteString(formatDorisDDLColumnIdentList(partCols))")
	g.P("\t\tb.WriteString(\") ()\\n\")")
	g.P("\t}")
	g.P("\tvar props []string")
	g.P("\tprops = append(props, `    \"compression_codec\" = \"zstd\"`)")
	g.P("\tif len(bloomCols) > 0 {")
	g.P("\t\tfor _, bf := range bloomCols {")
	g.P("\t\t\tprops = append(props, ", fmtSprintf, "(`    \"write.parquet.bloom-filter-enabled.column.%s\" = \"true\"`, bf))")
	g.P("\t\t}")
	g.P("\t} else if (t.DatabaseScope == \"TENANT\" || t.DatabaseScope == \"\") && ((len(t.Columns) > 0 && t.Columns[0].ColumnName == \"patient_id\") || (len(orderedCols) > 0 && orderedCols[0].ColumnName == \"patient_id\")) {")
	g.P("\t\tprops = append(props, `    \"write.parquet.bloom-filter-enabled.column.patient_id\" = \"true\"`)")
	g.P("\t}")
	g.P("\tb.WriteString(\"PROPERTIES (\\n\")")
	g.P("\tb.WriteString(", stringsJoin, "(props, \",\\n\"))")
	g.P("\tb.WriteString(\"\\n);\")")
	g.P("\treturn b.String()")
	g.P("}")
	g.P()

	emitDorisAtlasHCLRuntimeHelpers(g)

	// Emit Apache Doris / StarRocks / Apache Iceberg schema evolution linter (DORIS001 through DORIS006).
	g.P("// DorisLintRule identifies a compile-time Apache Doris / Apache Iceberg")
	g.P("// schema evolution safety rule.")
	g.P("type DorisLintRule string")
	g.P()
	g.P("const (")
	g.P("\t// DORIS001_IMMUTABLE_KEY blocks mutations to UniqueKeyColumns or DuplicateKeyColumns on an existing table.")
	g.P("\tDORIS001_IMMUTABLE_KEY DorisLintRule = \"DORIS001_IMMUTABLE_KEY\"")
	g.P("\t// DORIS002_IMMUTABLE_SEQUENCE blocks mutations to SequenceCol on an existing table.")
	g.P("\tDORIS002_IMMUTABLE_SEQUENCE DorisLintRule = \"DORIS002_IMMUTABLE_SEQUENCE\"")
	g.P("\t// DORIS003_IMMUTABLE_DISTRIBUTION blocks mutations to DistributedByColumn or ColocateGroup on an existing table.")
	g.P("\tDORIS003_IMMUTABLE_DISTRIBUTION DorisLintRule = \"DORIS003_IMMUTABLE_DISTRIBUTION\"")
	g.P("\t// DORIS004_NO_COLUMN_DROP blocks dropping existing columns from an existing table.")
	g.P("\tDORIS004_NO_COLUMN_DROP DorisLintRule = \"DORIS004_NO_COLUMN_DROP\"")
	g.P("\t// DORIS005_NON_NULL_SCALAR_DEFAULT requires an explicit DefaultValue on newly added NOT NULL scalar columns.")
	g.P("\tDORIS005_NON_NULL_SCALAR_DEFAULT DorisLintRule = \"DORIS005_NON_NULL_SCALAR_DEFAULT\"")
	g.P("\t// DORIS006_SAFE_TYPE_EVOLUTION allows only safe widening type evolutions supported by both Doris 4.x LSC and Iceberg v2.")
	g.P("\tDORIS006_SAFE_TYPE_EVOLUTION DorisLintRule = \"DORIS006_SAFE_TYPE_EVOLUTION\"")
	g.P()
	g.P("\tDorisRuleImmutableKey          = DORIS001_IMMUTABLE_KEY")
	g.P("\tDorisRuleImmutableSequence     = DORIS002_IMMUTABLE_SEQUENCE")
	g.P("\tDorisRuleImmutableDistribution = DORIS003_IMMUTABLE_DISTRIBUTION")
	g.P("\tDorisRuleNoColumnDrop          = DORIS004_NO_COLUMN_DROP")
	g.P("\tDorisRuleNonNullScalarDefault  = DORIS005_NON_NULL_SCALAR_DEFAULT")
	g.P("\tDorisRuleSafeTypeEvolution     = DORIS006_SAFE_TYPE_EVOLUTION")
	g.P(")")
	g.P()
	g.P("// DorisEvolutionViolation describes a single schema evolution rule violation")
	g.P("// detected between a before and after DorisTableDescriptor.")
	g.P("type DorisEvolutionViolation struct {")
	g.P("\tRule       DorisLintRule")
	g.P("\tTableName  string")
	g.P("\tColumnName string")
	g.P("\tMessage    string")
	g.P("}")
	g.P()
	g.P("// String formats the violation as a deterministic human-readable diagnostic.")
	g.P("func (v DorisEvolutionViolation) String() string {")
	g.P("\tif v.ColumnName != \"\" {")
	g.P("\t\treturn ", fmtSprintf, "(\"[%s] table %s column %s: %s\", v.Rule, v.TableName, v.ColumnName, v.Message)")
	g.P("\t}")
	g.P("\treturn ", fmtSprintf, "(\"[%s] table %s: %s\", v.Rule, v.TableName, v.Message)")
	g.P("}")
	g.P()
	g.P("// Error implements the error interface.")
	g.P("func (v DorisEvolutionViolation) Error() string {")
	g.P("\treturn v.String()")
	g.P("}")
	g.P()
	g.P("// LintEvolution validates evolving this table descriptor (before) into after.")
	g.P("func (t DorisTableDescriptor) LintEvolution(after DorisTableDescriptor) []DorisEvolutionViolation {")
	g.P("\treturn LintDorisTableEvolution(t, after)")
	g.P("}")
	g.P()
	g.P("// LintDorisTableEvolution validates evolving an existing table descriptor from")
	g.P("// before to after and returns all detected violations in deterministic rule order")
	g.P("// (DORIS001 through DORIS006).")
	g.P("func LintDorisTableEvolution(before, after DorisTableDescriptor) []DorisEvolutionViolation {")
	g.P("\ttableName := ", stringsTrimSpace, "(after.TableName)")
	g.P("\tif tableName == \"\" {")
	g.P("\t\ttableName = ", stringsTrimSpace, "(before.TableName)")
	g.P("\t}")
	g.P("\tvar violations []DorisEvolutionViolation")
	g.P("\tbeforeUKs := effectiveDorisUniqueKeys(before)")
	g.P("\tafterUKs := effectiveDorisUniqueKeys(after)")
	g.P("\tif !dorisStringSlicesEqual(beforeUKs, afterUKs) {")
	g.P("\t\tviolations = append(violations, DorisEvolutionViolation{")
	g.P("\t\t\tRule:      DORIS001_IMMUTABLE_KEY,")
	g.P("\t\t\tTableName: tableName,")
	g.P("\t\t\tMessage:   ", fmtSprintf, "(\"UNIQUE KEY columns cannot be mutated on existing table (before=%v, after=%v)\", beforeUKs, afterUKs),")
	g.P("\t\t})")
	g.P("\t}")
	g.P("\tbeforeDKs := effectiveDorisDuplicateKeys(before)")
	g.P("\tafterDKs := effectiveDorisDuplicateKeys(after)")
	g.P("\tif !dorisStringSlicesEqual(beforeDKs, afterDKs) {")
	g.P("\t\tviolations = append(violations, DorisEvolutionViolation{")
	g.P("\t\t\tRule:      DORIS001_IMMUTABLE_KEY,")
	g.P("\t\t\tTableName: tableName,")
	g.P("\t\t\tMessage:   ", fmtSprintf, "(\"DUPLICATE KEY columns cannot be mutated on existing table (before=%v, after=%v)\", beforeDKs, afterDKs),")
	g.P("\t\t})")
	g.P("\t}")
	g.P("\tbeforeSeq := effectiveDorisSequenceCol(before)")
	g.P("\tafterSeq := effectiveDorisSequenceCol(after)")
	g.P("\tif beforeSeq != afterSeq {")
	g.P("\t\tseqCol := afterSeq")
	g.P("\t\tif seqCol == \"\" {")
	g.P("\t\t\tseqCol = beforeSeq")
	g.P("\t\t}")
	g.P("\t\tviolations = append(violations, DorisEvolutionViolation{")
	g.P("\t\t\tRule:       DORIS002_IMMUTABLE_SEQUENCE,")
	g.P("\t\t\tTableName:  tableName,")
	g.P("\t\t\tColumnName: seqCol,")
	g.P("\t\t\tMessage:    ", fmtSprintf, "(\"sequence_col cannot be mutated on existing table (before=%q, after=%q)\", beforeSeq, afterSeq),")
	g.P("\t\t})")
	g.P("\t}")
	g.P("\tbeforeDist := effectiveDorisDistributionCols(before)")
	g.P("\tafterDist := effectiveDorisDistributionCols(after)")
	g.P("\tif !dorisStringSlicesEqual(beforeDist, afterDist) {")
	g.P("\t\tbeforeDistStr := ", stringsJoin, "(beforeDist, \", \")")
	g.P("\t\tafterDistStr := ", stringsJoin, "(afterDist, \", \")")
	g.P("\t\tdistCol := afterDistStr")
	g.P("\t\tif distCol == \"\" {")
	g.P("\t\t\tdistCol = beforeDistStr")
	g.P("\t\t}")
	g.P("\t\tviolations = append(violations, DorisEvolutionViolation{")
	g.P("\t\t\tRule:       DORIS003_IMMUTABLE_DISTRIBUTION,")
	g.P("\t\t\tTableName:  tableName,")
	g.P("\t\t\tColumnName: distCol,")
	g.P("\t\t\tMessage:    ", fmtSprintf, "(\"distributed_by_column cannot be mutated on existing table (before=%q, after=%q)\", beforeDistStr, afterDistStr),")
	g.P("\t\t})")
	g.P("\t}")
	g.P("\tbeforeColocate := ", stringsTrimSpace, "(before.ColocateGroup)")
	g.P("\tafterColocate := ", stringsTrimSpace, "(after.ColocateGroup)")
	g.P("\tif beforeColocate != afterColocate {")
	g.P("\t\tviolations = append(violations, DorisEvolutionViolation{")
	g.P("\t\t\tRule:      DORIS003_IMMUTABLE_DISTRIBUTION,")
	g.P("\t\t\tTableName: tableName,")
	g.P("\t\t\tMessage:   ", fmtSprintf, "(\"colocate_with group cannot be mutated on existing table (before=%q, after=%q)\", beforeColocate, afterColocate),")
	g.P("\t\t})")
	g.P("\t}")
	g.P("\tbeforeColByName := make(map[string]DorisColumnDescriptor, len(before.Columns))")
	g.P("\tfor _, c := range before.Columns {")
	g.P("\t\tbeforeColByName[c.ColumnName] = c")
	g.P("\t}")
	g.P("\tafterColByName := make(map[string]DorisColumnDescriptor, len(after.Columns))")
	g.P("\tfor _, c := range after.Columns {")
	g.P("\t\tafterColByName[c.ColumnName] = c")
	g.P("\t}")
	g.P("\tfor _, bc := range before.Columns {")
	g.P("\t\tif _, ok := afterColByName[bc.ColumnName]; !ok {")
	g.P("\t\t\tviolations = append(violations, DorisEvolutionViolation{")
	g.P("\t\t\t\tRule:       DORIS004_NO_COLUMN_DROP,")
	g.P("\t\t\t\tTableName:  tableName,")
	g.P("\t\t\t\tColumnName: bc.ColumnName,")
	g.P("\t\t\t\tMessage:    ", fmtSprintf, "(\"dropping existing column %q is forbidden\", bc.ColumnName),")
	g.P("\t\t\t})")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tfor _, ac := range after.Columns {")
	g.P("\t\tif _, existed := beforeColByName[ac.ColumnName]; existed {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tif !ac.Nullable && !isDorisNonScalarColumnType(ac.DorisType) && ", stringsTrimSpace, "(ac.DefaultValue) == \"\" {")
	g.P("\t\t\tviolations = append(violations, DorisEvolutionViolation{")
	g.P("\t\t\t\tRule:       DORIS005_NON_NULL_SCALAR_DEFAULT,")
	g.P("\t\t\t\tTableName:  tableName,")
	g.P("\t\t\t\tColumnName: ac.ColumnName,")
	g.P("\t\t\t\tMessage:    ", fmtSprintf, "(\"newly added NOT NULL scalar column %q (%s) requires an explicit DefaultValue\", ac.ColumnName, ac.DorisType),")
	g.P("\t\t\t})")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tfor _, ac := range after.Columns {")
	g.P("\t\tbc, existed := beforeColByName[ac.ColumnName]")
	g.P("\t\tif !existed {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tbeforeType := ", stringsTrimSpace, "(bc.DorisType)")
	g.P("\t\tafterType := ", stringsTrimSpace, "(ac.DorisType)")
	g.P("\t\tif beforeType == \"\" && afterType == \"\" {")
	g.P("\t\t\tbeforeType = ", stringsTrimSpace, "(bc.IcebergType)")
	g.P("\t\t\tafterType = ", stringsTrimSpace, "(ac.IcebergType)")
	g.P("\t\t}")
	g.P("\t\tif !IsSafeDorisTypeEvolution(beforeType, afterType) {")
	g.P("\t\t\tviolations = append(violations, DorisEvolutionViolation{")
	g.P("\t\t\t\tRule:       DORIS006_SAFE_TYPE_EVOLUTION,")
	g.P("\t\t\t\tTableName:  tableName,")
	g.P("\t\t\t\tColumnName: ac.ColumnName,")
	g.P("\t\t\t\tMessage:    ", fmtSprintf, "(\"unsafe type evolution on column %q from %q to %q\", ac.ColumnName, beforeType, afterType),")
	g.P("\t\t\t})")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn violations")
	g.P("}")
	g.P()
	g.P("// ValidateDorisTableEvolution returns an error joining all violations if evolving")
	g.P("// before into after violates any Doris schema evolution rule, or nil if safe.")
	g.P("func ValidateDorisTableEvolution(before, after DorisTableDescriptor) error {")
	g.P("\tviolations := LintDorisTableEvolution(before, after)")
	g.P("\tif len(violations) == 0 {")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\terrs := make([]error, len(violations))")
	g.P("\tfor i, v := range violations {")
	g.P("\t\terrs[i] = v")
	g.P("\t}")
	g.P("\treturn ", errorsJoin, "(errs...)")
	g.P("}")
	g.P()
	g.P("func dorisScopeTableIdentity(t DorisTableDescriptor) string {")
	g.P("\ttableName := ", stringsTrimSpace, "(t.TableName)")
	g.P("\tif tableName == \"\" {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tscope := ", stringsTrimSpace, "(t.DatabaseScope)")
	g.P("\tif scope == \"\" || scope == \"UNSPECIFIED\" {")
	g.P("\t\tscope = \"TENANT\"")
	g.P("\t}")
	g.P("\treturn scope + \"\\x00\" + tableName")
	g.P("}")
	g.P()
	g.P("func dorisSameDorisPhysicalTable(before, after DorisTableDescriptor) bool {")
	g.P("\tsBefore := dorisScopeTableIdentity(before)")
	g.P("\tsAfter := dorisScopeTableIdentity(after)")
	g.P("\tif sBefore == \"\" || sBefore != sAfter {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tcBefore := ", stringsTrimSpace, "(before.IcebergCatalog)")
	g.P("\tcAfter := ", stringsTrimSpace, "(after.IcebergCatalog)")
	g.P("\tif cBefore != \"\" && cAfter != \"\" && cBefore != cAfter {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\treturn true")
	g.P("}")
	g.P()
	g.P("func dorisTableEvolutionIdentity(t DorisTableDescriptor) string {")
	g.P("\ttableName := ", stringsTrimSpace, "(t.TableName)")
	g.P("\tif tableName == \"\" {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tscope := ", stringsTrimSpace, "(t.DatabaseScope)")
	g.P("\tif scope == \"\" || scope == \"UNSPECIFIED\" {")
	g.P("\t\tscope = \"TENANT\"")
	g.P("\t}")
	g.P("\tcatalog := ", stringsTrimSpace, "(t.IcebergCatalog)")
	g.P("\tif catalog == \"\" {")
	g.P("\t\tcatalog = \"iceberg\"")
	g.P("\t}")
	g.P("\treturn scope + \"\\x00\" + catalog + \"\\x00\" + tableName")
	g.P("}")
	g.P()
	g.P("func matchBeforeDorisTable(")
	g.P("\tafterTbl DorisTableDescriptor,")
	g.P("\tbeforeByKey map[string]DorisTableDescriptor,")
	g.P("\tbeforeByScope map[string]DorisTableDescriptor,")
	g.P("\tbeforeScopeCount map[string]int,")
	g.P("\tafterScopeCount map[string]int,")
	g.P(") (DorisTableDescriptor, string, bool) {")
	g.P("\tkey := dorisTableEvolutionIdentity(afterTbl)")
	g.P("\tif beforeTbl, ok := beforeByKey[key]; ok {")
	g.P("\t\treturn beforeTbl, key, true")
	g.P("\t}")
	g.P("\tscopeKey := dorisScopeTableIdentity(afterTbl)")
	g.P("\tif scopeKey != \"\" && beforeScopeCount[scopeKey] == 1 && afterScopeCount[scopeKey] == 1 {")
	g.P("\t\tif beforeTbl, ok := beforeByScope[scopeKey]; ok && dorisSameDorisPhysicalTable(beforeTbl, afterTbl) {")
	g.P("\t\t\treturn beforeTbl, dorisTableEvolutionIdentity(beforeTbl), true")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn DorisTableDescriptor{}, \"\", false")
	g.P("}")
	g.P()
	g.P("// LintDorisSchemaEvolution validates evolving a slice of before table descriptors")
	g.P("// into after table descriptors. Tables present in both slices (matched by physical")
	g.P("// table identity: DatabaseScope, IcebergCatalog, and TableName) are validated in")
	g.P("// deterministic order.")
	g.P("func LintDorisSchemaEvolution(before, after []DorisTableDescriptor) []DorisEvolutionViolation {")
	g.P("\tbeforeByKey := make(map[string]DorisTableDescriptor, len(before))")
	g.P("\tbeforeByScope := make(map[string]DorisTableDescriptor, len(before))")
	g.P("\tbeforeScopeCount := make(map[string]int, len(before))")
	g.P("\tfor _, t := range before {")
	g.P("\t\tif key := dorisTableEvolutionIdentity(t); key != \"\" {")
	g.P("\t\t\tbeforeByKey[key] = t")
	g.P("\t\t}")
	g.P("\t\tif scopeKey := dorisScopeTableIdentity(t); scopeKey != \"\" {")
	g.P("\t\t\tbeforeByScope[scopeKey] = t")
	g.P("\t\t\tbeforeScopeCount[scopeKey]++")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tafterScopeCount := make(map[string]int, len(after))")
	g.P("\tfor _, t := range after {")
	g.P("\t\tif scopeKey := dorisScopeTableIdentity(t); scopeKey != \"\" {")
	g.P("\t\t\tafterScopeCount[scopeKey]++")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tvar violations []DorisEvolutionViolation")
	g.P("\tseen := make(map[string]bool, len(after))")
	g.P("\tfor _, afterTbl := range after {")
	g.P("\t\tkey := dorisTableEvolutionIdentity(afterTbl)")
	g.P("\t\tif key == \"\" || seen[key] {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tseen[key] = true")
	g.P("\t\tif beforeTbl, _, ok := matchBeforeDorisTable(afterTbl, beforeByKey, beforeByScope, beforeScopeCount, afterScopeCount); ok {")
	g.P("\t\t\tviolations = append(violations, LintDorisTableEvolution(beforeTbl, afterTbl)...)")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn violations")
	g.P("}")
	g.P()
	g.P("// ValidateDorisSchemaEvolution returns an error joining all violations across")
	g.P("// the schema evolution from before to after, or nil if safe.")
	g.P("func ValidateDorisSchemaEvolution(before, after []DorisTableDescriptor) error {")
	g.P("\tviolations := LintDorisSchemaEvolution(before, after)")
	g.P("\tif len(violations) == 0 {")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\terrs := make([]error, len(violations))")
	g.P("\tfor i, v := range violations {")
	g.P("\t\terrs[i] = v")
	g.P("\t}")
	g.P("\treturn ", errorsJoin, "(errs...)")
	g.P("}")
	g.P()
	g.P("// LintDorisEvolution is an alias for LintDorisSchemaEvolution that validates")
	g.P("// evolving a slice of before table descriptors into after table descriptors.")
	g.P("func LintDorisEvolution(before, after []DorisTableDescriptor) []DorisEvolutionViolation {")
	g.P("\treturn LintDorisSchemaEvolution(before, after)")
	g.P("}")
	g.P()
	g.P("// ValidateDorisEvolution is an alias for ValidateDorisSchemaEvolution that returns")
	g.P("// an error joining all schema evolution violations, or nil if safe.")
	g.P("func ValidateDorisEvolution(before, after []DorisTableDescriptor) error {")
	g.P("\treturn ValidateDorisSchemaEvolution(before, after)")
	g.P("}")
	g.P()
	g.P("// BuildDorisAlterTableSQL validates evolving this table descriptor (before) into after")
	g.P("// and renders deterministic Apache Doris 4.x ALTER TABLE statements.")
	g.P("func (t DorisTableDescriptor) BuildDorisAlterTableSQL(after DorisTableDescriptor, tenantSlug string) ([]string, error) {")
	g.P("\treturn BuildDorisAlterTableSQL(t, after, tenantSlug)")
	g.P("}")
	g.P()
	g.P("// BuildStarRocksAlterTableSQL validates evolving this table descriptor (before) into after")
	g.P("// and renders deterministic StarRocks 4.x ALTER TABLE statements.")
	g.P("func (t DorisTableDescriptor) BuildStarRocksAlterTableSQL(after DorisTableDescriptor, tenantSlug string) ([]string, error) {")
	g.P("\treturn BuildStarRocksAlterTableSQL(t, after, tenantSlug)")
	g.P("}")
	g.P()
	g.P("// BuildIcebergAlterTableSQL validates evolving this table descriptor (before) into after")
	g.P("// and renders deterministic Apache Iceberg ALTER TABLE statements.")
	g.P("func (t DorisTableDescriptor) BuildIcebergAlterTableSQL(after DorisTableDescriptor, tenantSlug string) ([]string, error) {")
	g.P("\treturn BuildIcebergAlterTableSQL(t, after, tenantSlug)")
	g.P("}")
	g.P()
	g.P("// BuildDorisAlterTableSQL validates evolving before into after and returns the ordered")
	g.P("// Apache Doris 4.x ALTER TABLE statements (MODIFY COLUMN for widened columns, then")
	g.P("// ADD COLUMN for new columns). Returns (nil, nil) when no changes are required or when")
	g.P("// the table is Iceberg-only in both before and after.")
	g.P("func BuildDorisAlterTableSQL(before, after DorisTableDescriptor, tenantSlug string) ([]string, error) {")
	g.P("\tif !validateDorisDescriptor(before) || !validateDorisDescriptor(after) {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"invalid DorisTableDescriptor identifiers in BuildDorisAlterTableSQL\")")
	g.P("\t}")
	g.P("\tif !dorisSameDorisPhysicalTable(before, after) {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"cannot generate ALTER TABLE across different physical tables (%q vs %q)\", before.TableName, after.TableName)")
	g.P("\t}")
	g.P("\tslug, ok := sanitizeDorisTenantSlug(tenantSlug)")
	g.P("\tif !ok {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"invalid tenantSlug %q in BuildDorisAlterTableSQL\", tenantSlug)")
	g.P("\t}")
	g.P("\tif err := ValidateDorisTableEvolution(before, after); err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\tbeforeHasDoris := hasDorisTargetTable(before)")
	g.P("\tafterHasDoris := hasDorisTargetTable(after)")
	g.P("\tif !beforeHasDoris && !afterHasDoris {")
	g.P("\t\treturn nil, nil")
	g.P("\t}")
	g.P("\tif beforeHasDoris != afterHasDoris {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"cannot generate Doris ALTER TABLE across sync_tier target transition (%q -> %q) for table %q\", before.SyncTier, after.SyncTier, after.TableName)")
	g.P("\t}")
	g.P("\tbeforeParts := effectiveDorisPartitionCols(before)")
	g.P("\tafterParts := effectiveDorisPartitionCols(after)")
	g.P("\tif !dorisStringSlicesEqual(beforeParts, afterParts) {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"cannot mutate partition_by_columns on existing table %q (before=%v, after=%v)\", after.TableName, beforeParts, afterParts)")
	g.P("\t}")
	g.P("\tfullTableName := dorisQualifiedTableName(after, slug)")
	g.P("\tbeforeColByName := make(map[string]DorisColumnDescriptor, len(before.Columns))")
	g.P("\tfor _, bc := range before.Columns {")
	g.P("\t\tbeforeColByName[bc.ColumnName] = bc")
	g.P("\t}")
	g.P("\tvar stmts []string")
	g.P("\tfor _, ac := range after.Columns {")
	g.P("\t\tbc, existed := beforeColByName[ac.ColumnName]")
	g.P("\t\tif !existed {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tbeforeType := resolveDorisAlterColumnType(bc)")
	g.P("\t\tafterType := resolveDorisAlterColumnType(ac)")
	g.P("\t\tif beforeType == \"\" || afterType == \"\" {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"column %q in table %q has empty DorisType and IcebergType\", ac.ColumnName, after.TableName)")
	g.P("\t\t}")
	g.P("\t\tif beforeType != afterType || bc.Nullable != ac.Nullable {")
	g.P("\t\t\tstmts = append(stmts, ", fmtSprintf, "(\"ALTER TABLE %s MODIFY COLUMN %s;\", fullTableName, formatDorisAlterColumnClause(ac, afterType, true)))")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tfor _, ac := range after.Columns {")
	g.P("\t\tif _, existed := beforeColByName[ac.ColumnName]; existed {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tafterType := resolveDorisAlterColumnType(ac)")
	g.P("\t\tif afterType == \"\" {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"column %q in table %q has empty DorisType and IcebergType\", ac.ColumnName, after.TableName)")
	g.P("\t\t}")
	g.P("\t\tstmts = append(stmts, ", fmtSprintf, "(\"ALTER TABLE %s ADD COLUMN %s;\", fullTableName, formatDorisAlterColumnClause(ac, afterType, true)))")
	g.P("\t}")
	g.P("\treturn stmts, nil")
	g.P("}")
	g.P()
	g.P("// BuildStarRocksAlterTableSQL validates evolving before into after and returns the ordered")
	g.P("// StarRocks 4.x ALTER TABLE statements (MODIFY COLUMN for widened columns, then")
	g.P("// ADD COLUMN for new columns). Returns (nil, nil) when no changes are required or when")
	g.P("// the table is Iceberg-only in both before and after.")
	g.P("func BuildStarRocksAlterTableSQL(before, after DorisTableDescriptor, tenantSlug string) ([]string, error) {")
	g.P("\tif !validateDorisDescriptor(before) || !validateDorisDescriptor(after) {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"invalid DorisTableDescriptor identifiers in BuildStarRocksAlterTableSQL\")")
	g.P("\t}")
	g.P("\tif !dorisSameDorisPhysicalTable(before, after) {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"cannot generate ALTER TABLE across different physical tables (%q vs %q)\", before.TableName, after.TableName)")
	g.P("\t}")
	g.P("\tslug, ok := sanitizeDorisTenantSlug(tenantSlug)")
	g.P("\tif !ok {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"invalid tenantSlug %q in BuildStarRocksAlterTableSQL\", tenantSlug)")
	g.P("\t}")
	g.P("\tif err := ValidateDorisTableEvolution(before, after); err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\tbeforeHasSR := hasDorisTargetTable(before)")
	g.P("\tafterHasSR := hasDorisTargetTable(after)")
	g.P("\tif !beforeHasSR && !afterHasSR {")
	g.P("\t\treturn nil, nil")
	g.P("\t}")
	g.P("\tif beforeHasSR != afterHasSR {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"cannot generate StarRocks ALTER TABLE across sync_tier target transition (%q -> %q) for table %q\", before.SyncTier, after.SyncTier, after.TableName)")
	g.P("\t}")
	g.P("\tbeforeParts := effectiveDorisPartitionCols(before)")
	g.P("\tafterParts := effectiveDorisPartitionCols(after)")
	g.P("\tif !dorisStringSlicesEqual(beforeParts, afterParts) {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"cannot mutate partition_by_columns on existing table %q (before=%v, after=%v)\", after.TableName, beforeParts, afterParts)")
	g.P("\t}")
	g.P("\tfullTableName := dorisQualifiedTableName(after, slug)")
	g.P("\tbeforeColByName := make(map[string]DorisColumnDescriptor, len(before.Columns))")
	g.P("\tfor _, bc := range before.Columns {")
	g.P("\t\tbeforeColByName[bc.ColumnName] = bc")
	g.P("\t}")
	g.P("\tvar stmts []string")
	g.P("\tfor _, ac := range after.Columns {")
	g.P("\t\tbc, existed := beforeColByName[ac.ColumnName]")
	g.P("\t\tif !existed {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tbeforeSR := dorisColumnStarRocksDDLType(bc.DorisType, bc.IcebergType)")
	g.P("\t\tafterSR := dorisColumnStarRocksDDLType(ac.DorisType, ac.IcebergType)")
	g.P("\t\tif beforeSR == \"\" || afterSR == \"\" {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"column %q in table %q has empty DorisType and IcebergType\", ac.ColumnName, after.TableName)")
	g.P("\t\t}")
	g.P("\t\tif beforeSR != afterSR || bc.Nullable != ac.Nullable {")
	g.P("\t\t\tstmts = append(stmts, ", fmtSprintf, "(\"ALTER TABLE %s MODIFY COLUMN %s;\", fullTableName, formatDorisAlterColumnClause(ac, afterSR, true)))")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tfor _, ac := range after.Columns {")
	g.P("\t\tif _, existed := beforeColByName[ac.ColumnName]; existed {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tafterSR := dorisColumnStarRocksDDLType(ac.DorisType, ac.IcebergType)")
	g.P("\t\tif afterSR == \"\" {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"column %q in table %q has empty DorisType and IcebergType\", ac.ColumnName, after.TableName)")
	g.P("\t\t}")
	g.P("\t\tstmts = append(stmts, ", fmtSprintf, "(\"ALTER TABLE %s ADD COLUMN %s;\", fullTableName, formatDorisAlterColumnClause(ac, afterSR, true)))")
	g.P("\t}")
	g.P("\treturn stmts, nil")
	g.P("}")
	g.P()
	g.P("// BuildIcebergAlterTableSQL validates evolving before into after and returns the ordered")
	g.P("// Apache Iceberg ALTER TABLE statements (MODIFY COLUMN for widened columns, then")
	g.P("// ADD COLUMN for new columns). Returns (nil, nil) when no changes are required or when")
	g.P("// SyncTier == \"INTERNAL_ONLY\" in both before and after.")
	g.P("func BuildIcebergAlterTableSQL(before, after DorisTableDescriptor, tenantSlug string) ([]string, error) {")
	g.P("\tif !validateDorisDescriptor(before) || !validateDorisDescriptor(after) {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"invalid DorisTableDescriptor identifiers in BuildIcebergAlterTableSQL\")")
	g.P("\t}")
	g.P("\tif dorisTableEvolutionIdentity(before) != dorisTableEvolutionIdentity(after) {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"cannot generate ALTER TABLE across different physical tables (%q vs %q)\", before.TableName, after.TableName)")
	g.P("\t}")
	g.P("\tslug, ok := sanitizeDorisTenantSlug(tenantSlug)")
	g.P("\tif !ok {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"invalid tenantSlug %q in BuildIcebergAlterTableSQL\", tenantSlug)")
	g.P("\t}")
	g.P("\tif err := ValidateDorisTableEvolution(before, after); err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\tbeforeHasIce := hasIcebergTargetTable(before)")
	g.P("\tafterHasIce := hasIcebergTargetTable(after)")
	g.P("\tif !beforeHasIce && !afterHasIce {")
	g.P("\t\treturn nil, nil")
	g.P("\t}")
	g.P("\tif beforeHasIce != afterHasIce {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"cannot generate Iceberg ALTER TABLE across sync_tier target transition (%q -> %q) for table %q\", before.SyncTier, after.SyncTier, after.TableName)")
	g.P("\t}")
	g.P("\tbeforeParts := effectiveDorisPartitionCols(before)")
	g.P("\tafterParts := effectiveDorisPartitionCols(after)")
	g.P("\tif !dorisStringSlicesEqual(beforeParts, afterParts) || ", stringsTrimSpace, "(before.IcebergPartitionSpec) != ", stringsTrimSpace, "(after.IcebergPartitionSpec) {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"cannot mutate partition_by_columns on existing Iceberg table %q (before=%v, after=%v)\", after.TableName, beforeParts, afterParts)")
	g.P("\t}")
	g.P("\tfullTableName := icebergQualifiedTableName(after, slug)")
	g.P("\tbeforeColByName := make(map[string]DorisColumnDescriptor, len(before.Columns))")
	g.P("\tfor _, bc := range before.Columns {")
	g.P("\t\tbeforeColByName[bc.ColumnName] = bc")
	g.P("\t}")
	g.P("\tvar stmts []string")
	g.P("\tfor _, ac := range after.Columns {")
	g.P("\t\tbc, existed := beforeColByName[ac.ColumnName]")
	g.P("\t\tif !existed {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tbeforeIce := dorisColumnIcebergDDLType(bc.DorisType, bc.IcebergType)")
	g.P("\t\tafterIce := dorisColumnIcebergDDLType(ac.DorisType, ac.IcebergType)")
	g.P("\t\tif beforeIce == \"\" || afterIce == \"\" {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"column %q in table %q has empty DorisType and IcebergType\", ac.ColumnName, after.TableName)")
	g.P("\t\t}")
	g.P("\t\tif beforeIce != afterIce || bc.Nullable != ac.Nullable {")
	g.P("\t\t\tstmts = append(stmts, ", fmtSprintf, "(\"ALTER TABLE %s MODIFY COLUMN %s;\", fullTableName, formatDorisAlterColumnClause(ac, afterIce, false)))")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tfor _, ac := range after.Columns {")
	g.P("\t\tif _, existed := beforeColByName[ac.ColumnName]; existed {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tafterIce := dorisColumnIcebergDDLType(ac.DorisType, ac.IcebergType)")
	g.P("\t\tif afterIce == \"\" {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"column %q in table %q has empty DorisType and IcebergType\", ac.ColumnName, after.TableName)")
	g.P("\t\t}")
	g.P("\t\tif !ac.Nullable && !isDorisNonScalarColumnType(afterIce) {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"cannot add NOT NULL scalar column %q to existing Iceberg table %q: Iceberg ALTER TABLE ADD COLUMN does not support initial DEFAULT values\", ac.ColumnName, after.TableName)")
	g.P("\t\t}")
	g.P("\t\tstmts = append(stmts, ", fmtSprintf, "(\"ALTER TABLE %s ADD COLUMN %s;\", fullTableName, formatDorisAlterColumnClause(ac, afterIce, false)))")
	g.P("\t}")
	g.P("\treturn stmts, nil")
	g.P("}")
	g.P()
	g.P("func indexDorisSchemaEvolutionTables(")
	g.P("\tbefore, after []DorisTableDescriptor,")
	g.P("\tcaller string,")
	g.P(") (map[string]DorisTableDescriptor, map[string]DorisTableDescriptor, map[string]int, map[string]int, error) {")
	g.P("\tbeforeByKey := make(map[string]DorisTableDescriptor, len(before))")
	g.P("\tbeforeByScope := make(map[string]DorisTableDescriptor, len(before))")
	g.P("\tbeforeScopeCount := make(map[string]int, len(before))")
	g.P("\tfor _, t := range before {")
	g.P("\t\tif !validateDorisDescriptor(t) {")
	g.P("\t\t\treturn nil, nil, nil, nil, ", fmtErrorf, "(\"invalid DorisTableDescriptor identifiers for table %q in %s\", t.TableName, caller)")
	g.P("\t\t}")
	g.P("\t\tkey := dorisTableEvolutionIdentity(t)")
	g.P("\t\tif _, dup := beforeByKey[key]; dup {")
	g.P("\t\t\treturn nil, nil, nil, nil, ", fmtErrorf, "(\"duplicate table identity for table %q in %s\", t.TableName, caller)")
	g.P("\t\t}")
	g.P("\t\tbeforeByKey[key] = t")
	g.P("\t\tscopeKey := dorisScopeTableIdentity(t)")
	g.P("\t\tbeforeByScope[scopeKey] = t")
	g.P("\t\tbeforeScopeCount[scopeKey]++")
	g.P("\t}")
	g.P("\tafterSeenKey := make(map[string]bool, len(after))")
	g.P("\tafterScopeCount := make(map[string]int, len(after))")
	g.P("\tfor _, t := range after {")
	g.P("\t\tif !validateDorisDescriptor(t) {")
	g.P("\t\t\treturn nil, nil, nil, nil, ", fmtErrorf, "(\"invalid DorisTableDescriptor identifiers for table %q in %s\", t.TableName, caller)")
	g.P("\t\t}")
	g.P("\t\tkey := dorisTableEvolutionIdentity(t)")
	g.P("\t\tif afterSeenKey[key] {")
	g.P("\t\t\treturn nil, nil, nil, nil, ", fmtErrorf, "(\"duplicate table identity for table %q in %s\", t.TableName, caller)")
	g.P("\t\t}")
	g.P("\t\tafterSeenKey[key] = true")
	g.P("\t\tscopeKey := dorisScopeTableIdentity(t)")
	g.P("\t\tafterScopeCount[scopeKey]++")
	g.P("\t}")
	g.P("\treturn beforeByKey, beforeByScope, beforeScopeCount, afterScopeCount, nil")
	g.P("}")
	g.P()
	g.P("// BuildDorisSchemaMigrationSQL validates evolving a slice of before table descriptors")
	g.P("// into after table descriptors and returns the ordered Apache Doris 4.x DDL statements:")
	g.P("// ALTER TABLE statements for evolved existing tables and CREATE TABLE IF NOT EXISTS")
	g.P("// statements for newly added tables. Rejects dropping existing Doris tables.")
	g.P("func BuildDorisSchemaMigrationSQL(before, after []DorisTableDescriptor, tenantSlug string, buckets int) ([]string, error) {")
	g.P("\tif _, ok := sanitizeDorisTenantSlug(tenantSlug); !ok {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"invalid tenantSlug %q in BuildDorisSchemaMigrationSQL\", tenantSlug)")
	g.P("\t}")
	g.P("\tbeforeByKey, beforeByScope, beforeScopeCount, afterScopeCount, err := indexDorisSchemaEvolutionTables(before, after, \"BuildDorisSchemaMigrationSQL\")")
	g.P("\tif err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\tif err := ValidateDorisSchemaEvolution(before, after); err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\tmatchedBeforeKeys := make(map[string]bool, len(before))")
	g.P("\tvar stmts []string")
	g.P("\tfor _, afterTbl := range after {")
	g.P("\t\tif beforeTbl, matchedKey, ok := matchBeforeDorisTable(afterTbl, beforeByKey, beforeByScope, beforeScopeCount, afterScopeCount); ok {")
	g.P("\t\t\tmatchedBeforeKeys[matchedKey] = true")
	g.P("\t\t\talterStmts, err := BuildDorisAlterTableSQL(beforeTbl, afterTbl, tenantSlug)")
	g.P("\t\t\tif err != nil {")
	g.P("\t\t\t\treturn nil, err")
	g.P("\t\t\t}")
	g.P("\t\t\tstmts = append(stmts, alterStmts...)")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tif !hasDorisTargetTable(afterTbl) {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tcreateSQL := afterTbl.BuildDorisCreateTableSQL(tenantSlug, buckets)")
	g.P("\t\tif createSQL == \"\" {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"failed to build Doris CREATE TABLE SQL for new table %q\", afterTbl.TableName)")
	g.P("\t\t}")
	g.P("\t\tstmts = append(stmts, createSQL)")
	g.P("\t}")
	g.P("\tfor _, beforeTbl := range before {")
	g.P("\t\tkey := dorisTableEvolutionIdentity(beforeTbl)")
	g.P("\t\tif !matchedBeforeKeys[key] && hasDorisTargetTable(beforeTbl) {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"dropping existing Doris table %q is forbidden in BuildDorisSchemaMigrationSQL\", beforeTbl.TableName)")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn stmts, nil")
	g.P("}")
	g.P()
	g.P("// BuildStarRocksSchemaMigrationSQL validates evolving a slice of before table descriptors")
	g.P("// into after table descriptors and returns the ordered StarRocks 4.x DDL statements:")
	g.P("// ALTER TABLE statements for evolved existing tables and CREATE TABLE IF NOT EXISTS")
	g.P("// statements for newly added tables. Rejects dropping existing StarRocks tables.")
	g.P("func BuildStarRocksSchemaMigrationSQL(before, after []DorisTableDescriptor, tenantSlug string, buckets int) ([]string, error) {")
	g.P("\tif _, ok := sanitizeDorisTenantSlug(tenantSlug); !ok {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"invalid tenantSlug %q in BuildStarRocksSchemaMigrationSQL\", tenantSlug)")
	g.P("\t}")
	g.P("\tbeforeByKey, beforeByScope, beforeScopeCount, afterScopeCount, err := indexDorisSchemaEvolutionTables(before, after, \"BuildStarRocksSchemaMigrationSQL\")")
	g.P("\tif err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\tif err := ValidateDorisSchemaEvolution(before, after); err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\tmatchedBeforeKeys := make(map[string]bool, len(before))")
	g.P("\tvar stmts []string")
	g.P("\tfor _, afterTbl := range after {")
	g.P("\t\tif beforeTbl, matchedKey, ok := matchBeforeDorisTable(afterTbl, beforeByKey, beforeByScope, beforeScopeCount, afterScopeCount); ok {")
	g.P("\t\t\tmatchedBeforeKeys[matchedKey] = true")
	g.P("\t\t\talterStmts, err := BuildStarRocksAlterTableSQL(beforeTbl, afterTbl, tenantSlug)")
	g.P("\t\t\tif err != nil {")
	g.P("\t\t\t\treturn nil, err")
	g.P("\t\t\t}")
	g.P("\t\t\tstmts = append(stmts, alterStmts...)")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tif !hasDorisTargetTable(afterTbl) {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tcreateSQL := afterTbl.BuildStarRocksCreateTableSQL(tenantSlug, buckets)")
	g.P("\t\tif createSQL == \"\" {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"failed to build StarRocks CREATE TABLE SQL for new table %q\", afterTbl.TableName)")
	g.P("\t\t}")
	g.P("\t\tstmts = append(stmts, createSQL)")
	g.P("\t}")
	g.P("\tfor _, beforeTbl := range before {")
	g.P("\t\tkey := dorisTableEvolutionIdentity(beforeTbl)")
	g.P("\t\tif !matchedBeforeKeys[key] && hasDorisTargetTable(beforeTbl) {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"dropping existing StarRocks table %q is forbidden in BuildStarRocksSchemaMigrationSQL\", beforeTbl.TableName)")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn stmts, nil")
	g.P("}")
	g.P()
	g.P("// BuildIcebergSchemaMigrationSQL validates evolving a slice of before table descriptors")
	g.P("// into after table descriptors and returns the ordered Apache Iceberg DDL statements:")
	g.P("// ALTER TABLE statements for evolved existing tables and CREATE TABLE IF NOT EXISTS")
	g.P("// statements for newly added tables. Rejects dropping existing Iceberg tables.")
	g.P("func BuildIcebergSchemaMigrationSQL(before, after []DorisTableDescriptor, tenantSlug string) ([]string, error) {")
	g.P("\tif _, ok := sanitizeDorisTenantSlug(tenantSlug); !ok {")
	g.P("\t\treturn nil, ", fmtErrorf, "(\"invalid tenantSlug %q in BuildIcebergSchemaMigrationSQL\", tenantSlug)")
	g.P("\t}")
	g.P("\tbeforeByKey, _, _, _, err := indexDorisSchemaEvolutionTables(before, after, \"BuildIcebergSchemaMigrationSQL\")")
	g.P("\tif err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\tif err := ValidateDorisSchemaEvolution(before, after); err != nil {")
	g.P("\t\treturn nil, err")
	g.P("\t}")
	g.P("\tmatchedBeforeKeys := make(map[string]bool, len(before))")
	g.P("\tvar stmts []string")
	g.P("\tfor _, afterTbl := range after {")
	g.P("\t\tkey := dorisTableEvolutionIdentity(afterTbl)")
	g.P("\t\tif beforeTbl, ok := beforeByKey[key]; ok {")
	g.P("\t\t\tmatchedBeforeKeys[key] = true")
	g.P("\t\t\talterStmts, err := BuildIcebergAlterTableSQL(beforeTbl, afterTbl, tenantSlug)")
	g.P("\t\t\tif err != nil {")
	g.P("\t\t\t\treturn nil, err")
	g.P("\t\t\t}")
	g.P("\t\t\tstmts = append(stmts, alterStmts...)")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tif !hasIcebergTargetTable(afterTbl) {")
	g.P("\t\t\tcontinue")
	g.P("\t\t}")
	g.P("\t\tcreateSQL := afterTbl.BuildIcebergCreateTableSQL(tenantSlug)")
	g.P("\t\tif createSQL == \"\" {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"failed to build Iceberg CREATE TABLE SQL for new table %q\", afterTbl.TableName)")
	g.P("\t\t}")
	g.P("\t\tstmts = append(stmts, createSQL)")
	g.P("\t}")
	g.P("\tfor _, beforeTbl := range before {")
	g.P("\t\tkey := dorisTableEvolutionIdentity(beforeTbl)")
	g.P("\t\tif !matchedBeforeKeys[key] && hasIcebergTargetTable(beforeTbl) {")
	g.P("\t\t\treturn nil, ", fmtErrorf, "(\"dropping existing Iceberg table %q is forbidden in BuildIcebergSchemaMigrationSQL\", beforeTbl.TableName)")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn stmts, nil")
	g.P("}")
	g.P()
	g.P("func hasDorisTargetTable(t DorisTableDescriptor) bool {")
	g.P("\tif t.SyncTier == \"ICEBERG_ONLY\" {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tuks := effectiveDorisUniqueKeys(t)")
	g.P("\tdks := dedupDorisColumnList(t.DuplicateKeyColumns)")
	g.P("\tif t.DatabaseScope == \"GLOBAL_TERMINOLOGY\" && t.SyncTier == \"RELEASE_PARTITIONED\" && len(uks) == 0 && len(dks) == 0 {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\treturn true")
	g.P("}")
	g.P()
	g.P("func hasIcebergTargetTable(t DorisTableDescriptor) bool {")
	g.P("\treturn t.SyncTier != \"INTERNAL_ONLY\"")
	g.P("}")
	g.P()
	g.P("func effectiveDorisPartitionCols(t DorisTableDescriptor) []string {")
	g.P("\tif len(t.PartitionByColumns) > 0 {")
	g.P("\t\treturn dedupDorisColumnList(t.PartitionByColumns)")
	g.P("\t}")
	g.P("\tvar cols []string")
	g.P("\tfor _, c := range t.Columns {")
	g.P("\t\tif c.IsPartitionKey {")
	g.P("\t\t\tcols = append(cols, c.ColumnName)")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tcols = dedupDorisColumnList(cols)")
	g.P("\tif len(cols) == 0 && t.SyncTier == \"MONTHLY_PARTITIONED\" {")
	g.P("\t\tfor _, c := range t.Columns {")
	g.P("\t\t\tif c.ColumnName == \"event_month\" {")
	g.P("\t\t\t\treturn []string{\"event_month\"}")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\tif len(cols) == 0 && t.SyncTier == \"RELEASE_PARTITIONED\" {")
	g.P("\t\tfor _, c := range t.Columns {")
	g.P("\t\t\tif c.ColumnName == \"release_id\" {")
	g.P("\t\t\t\treturn []string{\"release_id\"}")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn cols")
	g.P("}")
	g.P()
	g.P("func resolveDorisAlterColumnType(c DorisColumnDescriptor) string {")
	g.P("\tdt := ", stringsToUpper, "(", stringsTrimSpace, "(c.DorisType))")
	g.P("\tif dt != \"\" {")
	g.P("\t\treturn dt")
	g.P("\t}")
	g.P("\treturn dorisColumnIcebergDDLType(c.DorisType, c.IcebergType)")
	g.P("}")
	g.P()
	g.P("func dorisQualifiedTableName(t DorisTableDescriptor, slug string) string {")
	g.P("\tswitch t.DatabaseScope {")
	g.P("\tcase \"GLOBAL_TERMINOLOGY\":")
	g.P("\t\treturn \"internal.terminology.\" + t.TableName")
	g.P("\tcase \"GLOBAL_TERMINOLOGY_RAG\":")
	g.P("\t\treturn \"internal.terminology_rag.\" + t.TableName")
	g.P("\tcase \"GLOBAL_TELEMETRY\":")
	g.P("\t\treturn \"internal.telemetry.\" + t.TableName")
	g.P("\tdefault:")
	g.P("\t\treturn \"internal.tenant_\" + slug + \".\" + t.TableName")
	g.P("\t}")
	g.P("}")
	g.P()
	g.P("func icebergQualifiedTableName(t DorisTableDescriptor, slug string) string {")
	g.P("\tcatalog := ", stringsTrimSpace, "(t.IcebergCatalog)")
	g.P("\tif catalog == \"\" {")
	g.P("\t\tcatalog = \"iceberg\"")
	g.P("\t}")
	g.P("\tswitch t.DatabaseScope {")
	g.P("\tcase \"GLOBAL_TERMINOLOGY\":")
	g.P("\t\treturn catalog + \".terminology.\" + t.TableName")
	g.P("\tcase \"GLOBAL_TELEMETRY\":")
	g.P("\t\treturn catalog + \".telemetry.\" + t.TableName")
	g.P("\tcase \"GLOBAL_TERMINOLOGY_RAG\":")
	g.P("\t\treturn catalog + \".terminology_rag.\" + t.TableName")
	g.P("\tdefault:")
	g.P("\t\treturn catalog + \".tenant_\" + slug + \".\" + t.TableName")
	g.P("\t}")
	g.P("}")
	g.P()
	g.P("func formatDorisAlterColumnClause(c DorisColumnDescriptor, colType string, includeDefaults bool) string {")
	g.P("\tcolIdent := formatDorisDDLColumnIdent(c.ColumnName)")
	g.P("\tclause := ", fmtSprintf, "(\"%s %s\", colIdent, colType)")
	g.P("\tif !c.Nullable {")
	g.P("\t\tclause += \" NOT NULL\"")
	g.P("\t\tif includeDefaults {")
	g.P("\t\t\tcWithType := c")
	g.P("\t\t\tcWithType.DorisType = colType")
	g.P("\t\t\tif defVal, hasDef := resolveDorisColumnDefaultValue(cWithType); hasDef {")
	g.P("\t\t\t\tclause += ", fmtSprintf, "(\" DEFAULT %q\", defVal)")
	g.P("\t\t\t}")
	g.P("\t\t}")
	g.P("\t} else if includeDefaults && c.DefaultValue != \"\" {")
	g.P("\t\tclause += ", fmtSprintf, "(\" DEFAULT %q\", c.DefaultValue)")
	g.P("\t}")
	g.P("\treturn clause")
	g.P("}")
	g.P()
	g.P("func isDorisNonScalarColumnType(dorisType string) bool {")
	g.P("\tdt := ", stringsToUpper, "(", stringsTrimSpace, "(dorisType))")
	g.P("\tswitch dt {")
	g.P("\tcase \"JSON\", \"JSONB\", \"VARIANT\":")
	g.P("\t\treturn true")
	g.P("\t}")
	g.P("\treturn ", stringsHasPrefix, "(dt, \"ARRAY<\") || ", stringsHasPrefix, "(dt, \"MAP<\") || ", stringsHasPrefix, "(dt, \"STRUCT<\")")
	g.P("}")
	g.P()
	g.P("func effectiveDorisUniqueKeys(t DorisTableDescriptor) []string {")
	g.P("\tif len(t.UniqueKeyColumns) > 0 {")
	g.P("\t\treturn dedupDorisColumnList(t.UniqueKeyColumns)")
	g.P("\t}")
	g.P("\tvar uks []string")
	g.P("\tfor _, c := range t.Columns {")
	g.P("\t\tif c.IsUniqueKey {")
	g.P("\t\t\tuks = append(uks, c.ColumnName)")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn dedupDorisColumnList(uks)")
	g.P("}")
	g.P()
	g.P("func effectiveDorisDuplicateKeys(t DorisTableDescriptor) []string {")
	g.P("\tif len(t.DuplicateKeyColumns) > 0 {")
	g.P("\t\treturn dedupDorisColumnList(t.DuplicateKeyColumns)")
	g.P("\t}")
	g.P("\tif len(effectiveDorisUniqueKeys(t)) == 0 && len(t.Columns) > 0 && t.Columns[0].ColumnName != \"\" {")
	g.P("\t\treturn []string{t.Columns[0].ColumnName}")
	g.P("\t}")
	g.P("\treturn nil")
	g.P("}")
	g.P()
	g.P("func effectiveDorisDistributionCols(t DorisTableDescriptor) []string {")
	g.P("\tif len(t.DistributionColumns) > 0 {")
	g.P("\t\treturn dedupDorisColumnList(t.DistributionColumns)")
	g.P("\t}")
	g.P("\tif s := ", stringsTrimSpace, "(t.DistributedByColumn); s != \"\" {")
	g.P("\t\treturn []string{s}")
	g.P("\t}")
	g.P("\tif uks := effectiveDorisUniqueKeys(t); len(uks) > 0 {")
	g.P("\t\tif len(uks) > 1 && uks[0] == \"tenant_id\" {")
	g.P("\t\t\treturn []string{uks[1]}")
	g.P("\t\t}")
	g.P("\t\treturn []string{uks[0]}")
	g.P("\t}")
	g.P("\tif dks := effectiveDorisDuplicateKeys(t); len(dks) > 0 {")
	g.P("\t\treturn []string{dks[0]}")
	g.P("\t}")
	g.P("\treturn nil")
	g.P("}")
	g.P()
	g.P("func effectiveDorisSequenceCol(t DorisTableDescriptor) string {")
	g.P("\tif s := ", stringsTrimSpace, "(t.SequenceCol); s != \"\" {")
	g.P("\t\treturn s")
	g.P("\t}")
	g.P("\tif s := ", stringsTrimSpace, "(t.SequenceColumn); s != \"\" {")
	g.P("\t\treturn s")
	g.P("\t}")
	g.P("\tfor _, c := range t.Columns {")
	g.P("\t\tif c.IsSequenceCol {")
	g.P("\t\t\treturn c.ColumnName")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn \"\"")
	g.P("}")
	g.P()
	g.P("func dorisStringSlicesEqual(a, b []string) bool {")
	g.P("\tif len(a) != len(b) {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tfor i := range a {")
	g.P("\t\tif a[i] != b[i] {")
	g.P("\t\t\treturn false")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn true")
	g.P("}")
	g.P()

	g.P("// IsSafeDorisTypeEvolution reports whether evolving a column from beforeType to")
	g.P("// afterType is either a no-op or a safe widening type change supported by")
	g.P("// Apache Doris 4.x Light Schema Change (LSC), StarRocks 4.x, and Apache Iceberg v2.")
	g.P("func IsSafeDorisTypeEvolution(beforeType, afterType string) bool {")
	g.P("\tbt := ", stringsToUpper, "(", stringsTrimSpace, "(beforeType))")
	g.P("\tat := ", stringsToUpper, "(", stringsTrimSpace, "(afterType))")
	g.P("\tif bt == at {")
	g.P("\t\treturn true")
	g.P("\t}")
	g.P("\tif bt == \"\" || at == \"\" {")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tif ", stringsHasPrefix, "(bt, \"ARRAY<\") && ", stringsHasSuffix, "(bt, \">\") {")
	g.P("\t\tif ", stringsHasPrefix, "(at, \"ARRAY<\") && ", stringsHasSuffix, "(at, \">\") {")
	g.P("\t\t\tinnerB := ", stringsTrimSuffix, "(", stringsTrimPrefix, "(bt, \"ARRAY<\"), \">\")")
	g.P("\t\t\tinnerA := ", stringsTrimSuffix, "(", stringsTrimPrefix, "(at, \"ARRAY<\"), \">\")")
	g.P("\t\t\treturn IsSafeDorisTypeEvolution(innerB, innerA)")
	g.P("\t\t}")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tif p1, okB := parseDorisDatetimePrecision(bt); okB {")
	g.P("\t\tif p2, okA := parseDorisDatetimePrecision(at); okA {")
	g.P("\t\t\treturn p2 >= p1")
	g.P("\t\t}")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tcanonBefore := dorisColumnStarRocksDDLType(bt, \"\")")
	g.P("\tcanonAfter := dorisColumnStarRocksDDLType(at, \"\")")
	g.P("\tif canonBefore == canonAfter {")
	g.P("\t\treturn true")
	g.P("\t}")
	g.P("\tif m, okB := parseDorisVarcharLength(canonBefore); okB {")
	g.P("\t\tif n, okA := parseDorisVarcharLength(canonAfter); okA {")
	g.P("\t\t\treturn n >= m")
	g.P("\t\t}")
	g.P("\t\treturn canonAfter == \"STRING\"")
	g.P("\t}")
	g.P("\tif p1, s1, okB := parseDorisDecimalParams(canonBefore); okB {")
	g.P("\t\tif p2, s2, okA := parseDorisDecimalParams(canonAfter); okA {")
	g.P("\t\t\treturn p2 >= p1 && s1 == s2")
	g.P("\t\t}")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("\tswitch canonBefore {")
	g.P("\tcase \"TINYINT\":")
	g.P("\t\treturn canonAfter == \"SMALLINT\" || canonAfter == \"INT\" || canonAfter == \"BIGINT\" || canonAfter == \"LARGEINT\"")
	g.P("\tcase \"SMALLINT\":")
	g.P("\t\treturn canonAfter == \"INT\" || canonAfter == \"BIGINT\" || canonAfter == \"LARGEINT\"")
	g.P("\tcase \"INT\":")
	g.P("\t\treturn canonAfter == \"BIGINT\" || canonAfter == \"LARGEINT\"")
	g.P("\tcase \"BIGINT\":")
	g.P("\t\treturn canonAfter == \"LARGEINT\"")
	g.P("\tcase \"FLOAT\":")
	g.P("\t\treturn canonAfter == \"DOUBLE\"")
	g.P("\tcase \"DATE\":")
	g.P("\t\t_, okA := parseDorisDatetimePrecision(at)")
	g.P("\t\treturn okA")
	g.P("\tdefault:")
	g.P("\t\treturn false")
	g.P("\t}")
	g.P("}")
	g.P()
	g.P("func parseDorisDatetimePrecision(upperType string) (int, bool) {")
	g.P("\tswitch upperType {")
	g.P("\tcase \"DATETIME\", \"DATETIMEV2\":")
	g.P("\t\treturn 0, true")
	g.P("\t}")
	g.P("\tvar inner string")
	g.P("\tswitch {")
	g.P("\tcase ", stringsHasPrefix, "(upperType, \"DATETIME(\") && ", stringsHasSuffix, "(upperType, \")\"):")
	g.P("\t\tinner = ", stringsTrimSuffix, "(", stringsTrimPrefix, "(upperType, \"DATETIME(\"), \")\")")
	g.P("\tcase ", stringsHasPrefix, "(upperType, \"DATETIMEV2(\") && ", stringsHasSuffix, "(upperType, \")\"):")
	g.P("\t\tinner = ", stringsTrimSuffix, "(", stringsTrimPrefix, "(upperType, \"DATETIMEV2(\"), \")\")")
	g.P("\tdefault:")
	g.P("\t\treturn 0, false")
	g.P("\t}")
	g.P("\tp, err := ", strconvAtoi, "(", stringsTrimSpace, "(inner))")
	g.P("\tif err != nil || p < 0 || p > 6 {")
	g.P("\t\treturn 0, false")
	g.P("\t}")
	g.P("\treturn p, true")
	g.P("}")
	g.P()
	g.P("func parseDorisDecimalParams(upperType string) (int, int, bool) {")
	g.P("\tvar inner string")
	g.P("\tswitch {")
	g.P("\tcase ", stringsHasPrefix, "(upperType, \"DECIMAL(\") && ", stringsHasSuffix, "(upperType, \")\"):")
	g.P("\t\tinner = ", stringsTrimSuffix, "(", stringsTrimPrefix, "(upperType, \"DECIMAL(\"), \")\")")
	g.P("\tcase ", stringsHasPrefix, "(upperType, \"DECIMALV3(\") && ", stringsHasSuffix, "(upperType, \")\"):")
	g.P("\t\tinner = ", stringsTrimSuffix, "(", stringsTrimPrefix, "(upperType, \"DECIMALV3(\"), \")\")")
	g.P("\tdefault:")
	g.P("\t\treturn 0, 0, false")
	g.P("\t}")
	g.P("\tparts := ", stringsSplit, "(inner, \",\")")
	g.P("\tif len(parts) != 2 {")
	g.P("\t\treturn 0, 0, false")
	g.P("\t}")
	g.P("\tp, errP := ", strconvAtoi, "(", stringsTrimSpace, "(parts[0]))")
	g.P("\ts, errS := ", strconvAtoi, "(", stringsTrimSpace, "(parts[1]))")
	g.P("\tif errP != nil || errS != nil || p <= 0 || s < 0 || s > p {")
	g.P("\t\treturn 0, 0, false")
	g.P("\t}")
	g.P("\treturn p, s, true")
	g.P("}")
	g.P()
	g.P("func parseDorisVarcharLength(upperType string) (int, bool) {")
	g.P("\tif !", stringsHasPrefix, "(upperType, \"VARCHAR(\") || !", stringsHasSuffix, "(upperType, \")\") {")
	g.P("\t\treturn 0, false")
	g.P("\t}")
	g.P("\tinner := ", stringsTrimSpace, "(", stringsTrimSuffix, "(", stringsTrimPrefix, "(upperType, \"VARCHAR(\"), \")\"))")
	g.P("\tif inner == \"\" {")
	g.P("\t\treturn 0, false")
	g.P("\t}")
	g.P("\tn, err := ", strconvAtoi, "(inner)")
	g.P("\tif err != nil || n <= 0 {")
	g.P("\t\treturn 0, false")
	g.P("\t}")
	g.P("\treturn n, true")
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
	g.P("\t\t\t\tif b == nil {")
	g.P("\t\t\t\t\t*ptr = nil")
	g.P("\t\t\t\t} else {")
	g.P("\t\t\t\t\tcp := make([]byte, len(b))")
	g.P("\t\t\t\t\tcopy(cp, b)")
	g.P("\t\t\t\t\t*ptr = cp")
	g.P("\t\t\t\t}")
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

	g.P("func dorisRequiredPtrValue[T any](v *T) T {")
	g.P("\tif v == nil {")
	g.P("\t\tvar zero T")
	g.P("\t\treturn zero")
	g.P("\t}")
	g.P("\treturn *v")
	g.P("}")
	g.P()

	g.P("func dorisRequiredBytesValue(b []byte) any {")
	g.P("\tif b == nil {")
	g.P("\t\treturn []byte{}")
	g.P("\t}")
	g.P("\treturn b")
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

	g.P("func dorisRequiredPtrUint64Value(v *uint64) any {")
	g.P("\tif v == nil {")
	g.P("\t\treturn int64(0)")
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
	g.P("\t\tif v < 0 {")
	g.P("\t\t\treturn 0, false, ", fmtErrorf, "(\"scanDorisUint64: negative int64 %d cannot be converted to uint64\", v)")
	g.P("\t\t}")
	g.P("\t\treturn uint64(v), true, nil")
	g.P("\tcase ", sqlNullInt64, ":")
	g.P("\t\tif !v.Valid {")
	g.P("\t\t\treturn 0, false, nil")
	g.P("\t\t}")
	g.P("\t\tif v.Int64 < 0 {")
	g.P("\t\t\treturn 0, false, ", fmtErrorf, "(\"scanDorisUint64: negative int64 %d cannot be converted to uint64\", v.Int64)")
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

	g.P("func scanDorisInt32(v ", sqlNullInt64, ") (int32, bool, error) {")
	g.P("\tif !v.Valid {")
	g.P("\t\treturn 0, false, nil")
	g.P("\t}")
	g.P("\tif v.Int64 < ", mathMinInt32, " || v.Int64 > ", mathMaxInt32, " {")
	g.P("\t\treturn 0, false, ", fmtErrorf, "(\"scanDorisInt32: value %d overflows int32\", v.Int64)")
	g.P("\t}")
	g.P("\treturn int32(v.Int64), true, nil")
	g.P("}")
	g.P()

	g.P("func scanDorisUint32(v ", sqlNullInt64, ") (uint32, bool, error) {")
	g.P("\tif !v.Valid {")
	g.P("\t\treturn 0, false, nil")
	g.P("\t}")
	g.P("\tif v.Int64 < 0 || v.Int64 > ", mathMaxUint32, " {")
	g.P("\t\treturn 0, false, ", fmtErrorf, "(\"scanDorisUint32: value %d overflows uint32\", v.Int64)")
	g.P("\t}")
	g.P("\treturn uint32(v.Int64), true, nil")
	g.P("}")
	g.P()

	g.P("func dorisNullableDateValue(s string) any {")
	g.P("\tif s == \"\" {")
	g.P("\t\treturn ", sqlNullString, "{}")
	g.P("\t}")
	g.P("\treturn s")
	g.P("}")
	g.P()

	g.P("func dorisRequiredDateValue(s string) any {")
	g.P("\tif s == \"\" {")
	g.P("\t\treturn \"0001-01-01\"")
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

	g.P("func dorisRequiredPtrTimeValue(t *", timeTime, ") any {")
	g.P("\tif t == nil || t.IsZero() {")
	g.P("\t\treturn ", timeTime, "{}.UTC()")
	g.P("\t}")
	g.P("\treturn t.UTC()")
	g.P("}")
	g.P()

	g.P("func scanDorisTime(raw any) (", timeTime, ", bool, error) {")
	g.P("\tif raw == nil {")
	g.P("\t\treturn ", timeTime, "{}, false, nil")
	g.P("\t}")
	g.P("\tswitch v := raw.(type) {")
	g.P("\tcase ", timeTime, ":")
	g.P("\t\tif v.IsZero() {")
	g.P("\t\t\treturn ", timeTime, "{}, false, nil")
	g.P("\t\t}")
	g.P("\t\treturn v.UTC(), true, nil")
	g.P("\tcase *", timeTime, ":")
	g.P("\t\tif v == nil || v.IsZero() {")
	g.P("\t\t\treturn ", timeTime, "{}, false, nil")
	g.P("\t\t}")
	g.P("\t\treturn v.UTC(), true, nil")
	g.P("\tcase ", sqlNullTime, ":")
	g.P("\t\tif !v.Valid || v.Time.IsZero() {")
	g.P("\t\t\treturn ", timeTime, "{}, false, nil")
	g.P("\t\t}")
	g.P("\t\treturn v.Time.UTC(), true, nil")
	g.P("\tcase ", sqlNullString, ":")
	g.P("\t\tif !v.Valid {")
	g.P("\t\t\treturn ", timeTime, "{}, false, nil")
	g.P("\t\t}")
	g.P("\t\treturn scanDorisTimeStrChecked(v.String)")
	g.P("\tcase []byte:")
	g.P("\t\treturn scanDorisTimeStrChecked(string(v))")
	g.P("\tcase string:")
	g.P("\t\treturn scanDorisTimeStrChecked(v)")
	g.P("\tdefault:")
	g.P("\t\treturn ", timeTime, "{}, false, ", fmtErrorf, "(\"scanDorisTime: unsupported type %T\", raw)")
	g.P("\t}")
	g.P("}")
	g.P()

	g.P("func scanDorisTimeStrChecked(s string) (", timeTime, ", bool, error) {")
	g.P("\ts = ", stringsTrimSpace, "(s)")
	g.P("\tif s == \"\" || ", stringsHasPrefix, "(s, \"0000-00-00\") || ", stringsHasPrefix, "(s, \"0001-01-01\") {")
	g.P("\t\treturn ", timeTime, "{}, false, nil")
	g.P("\t}")
	g.P("\tt, ok := parseDorisTimeString(s)")
	g.P("\tif !ok {")
	g.P("\t\treturn ", timeTime, "{}, false, ", fmtErrorf, "(\"scanDorisTime: invalid datetime %q\", s)")
	g.P("\t}")
	g.P("\treturn t, true, nil")
	g.P("}")
	g.P()

	g.P("func parseDorisTimeString(s string) (", timeTime, ", bool) {")
	g.P("\ts = ", stringsTrimSpace, "(s)")
	g.P("\tif s == \"\" || ", stringsHasPrefix, "(s, \"0000-00-00\") {")
	g.P("\t\treturn ", timeTime, "{}, false")
	g.P("\t}")
	g.P("\tlayouts := []string{")
	g.P("\t\t\"2006-01-02 15:04:05.999999999\",")
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

	g.P("func scanDorisDateString(raw any) (string, error) {")
	g.P("\tif raw == nil {")
	g.P("\t\treturn \"\", nil")
	g.P("\t}")
	g.P("\tswitch v := raw.(type) {")
	g.P("\tcase ", timeTime, ":")
	g.P("\t\tif v.IsZero() {")
	g.P("\t\t\treturn \"\", nil")
	g.P("\t\t}")
	g.P("\t\treturn v.UTC().Format(\"2006-01-02\"), nil")
	g.P("\tcase *", timeTime, ":")
	g.P("\t\tif v == nil || v.IsZero() {")
	g.P("\t\t\treturn \"\", nil")
	g.P("\t\t}")
	g.P("\t\treturn v.UTC().Format(\"2006-01-02\"), nil")
	g.P("\tcase ", sqlNullTime, ":")
	g.P("\t\tif !v.Valid || v.Time.IsZero() {")
	g.P("\t\t\treturn \"\", nil")
	g.P("\t\t}")
	g.P("\t\treturn v.Time.UTC().Format(\"2006-01-02\"), nil")
	g.P("\tcase ", sqlNullString, ":")
	g.P("\t\tif !v.Valid {")
	g.P("\t\t\treturn \"\", nil")
	g.P("\t\t}")
	g.P("\t\treturn scanDorisDateStrChecked(v.String)")
	g.P("\tcase []byte:")
	g.P("\t\treturn scanDorisDateStrChecked(string(v))")
	g.P("\tcase string:")
	g.P("\t\treturn scanDorisDateStrChecked(v)")
	g.P("\tdefault:")
	g.P("\t\treturn \"\", ", fmtErrorf, "(\"scanDorisDateString: unsupported type %T\", raw)")
	g.P("\t}")
	g.P("}")
	g.P()

	g.P("func scanDorisDateStrChecked(s string) (string, error) {")
	g.P("\tnorm := normalizeDorisDateStr(s)")
	g.P("\tif norm == \"\" {")
	g.P("\t\treturn \"\", nil")
	g.P("\t}")
	g.P("\tif _, _, _, ok := parseDorisDateParts(norm); !ok {")
	g.P("\t\treturn \"\", ", fmtErrorf, "(\"scanDorisDateString: invalid date %q\", s)")
	g.P("\t}")
	g.P("\treturn norm, nil")
	g.P("}")
	g.P()

	g.P("func normalizeDorisDateStr(s string) string {")
	g.P("\ts = ", stringsTrimSpace, "(s)")
	g.P("\tif s == \"\" || ", stringsHasPrefix, "(s, \"0000-00-00\") || ", stringsHasPrefix, "(s, \"0001-01-01\") {")
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
	g.P("\t// #nosec G115 -- year [1,9999], month [1,12], day [1,31]")
	g.P("\treturn int32(t.Year()), int32(t.Month()), int32(t.Day()), true")
	g.P("}")
	g.P()

	g.P("func formatDorisDateParts(year, month, day int32) string {")
	g.P("\tif year < 1 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31 {")
	g.P("\t\treturn \"\"")
	g.P("\t}")
	g.P("\tt := ", timeDate, "(int(year), ", timeMonth, "(month), int(day), 0, 0, 0, 0, ", timeUTC, ")")
	g.P("\tif t.Year() != int(year) || t.Month() != ", timeMonth, "(month) || t.Day() != int(day) {")
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

	g.P("func lookupProtoEnumValueOk(valMap map[string]int32, raw, prefix string) (int32, bool) {")
	g.P("\tif raw == \"\" {")
	g.P("\t\treturn 0, false")
	g.P("\t}")
	g.P("\tif v, ok := valMap[raw]; ok {")
	g.P("\t\treturn v, true")
	g.P("\t}")
	g.P("\tif prefix != \"\" {")
	g.P("\t\tif v, ok := valMap[prefix+raw]; ok {")
	g.P("\t\t\treturn v, true")
	g.P("\t\t}")
	g.P("\t}")
	g.P("\treturn 0, false")
	g.P("}")
	g.P()

	g.P("func lookupProtoEnumValue(valMap map[string]int32, raw, prefix string) int32 {")
	g.P("\tv, _ := lookupProtoEnumValueOk(valMap, raw, prefix)")
	g.P("\treturn v")
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
	g.P("\t\tEmbeddingDimension:   ", d.EmbeddingDimension, ",")
	g.P("\t\tStorageVaultName:     ", fmt.Sprintf("%q", d.StorageVaultName), ",")
	g.P("\t\tIcebergCatalog:       ", fmt.Sprintf("%q", d.IcebergCatalog), ",")
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
				g.P("\t\tdorisRequiredDateValue(r.", c.GoFieldName, "),")
			}
		case c.IsTimestamp:
			if c.IsPointer {
				if c.ColDesc.Nullable {
					g.P("\t\tdorisPtrTimeValue(r.", c.GoFieldName, "),")
				} else {
					g.P("\t\tdorisRequiredPtrTimeValue(r.", c.GoFieldName, "),")
				}
			} else if c.ColDesc.Nullable {
				g.P("\t\tdorisNullableTimeValue(r.", c.GoFieldName, "),")
			} else {
				g.P("\t\tr.", c.GoFieldName, ".UTC(),")
			}
		case c.IsUint64:
			if c.IsPointer {
				if c.ColDesc.Nullable {
					g.P("\t\tdorisPtrUint64Value(r.", c.GoFieldName, "),")
				} else {
					g.P("\t\tdorisRequiredPtrUint64Value(r.", c.GoFieldName, "),")
				}
			} else {
				g.P("\t\tdorisUint64Value(r.", c.GoFieldName, "),")
			}
		default:
			if c.Field.Desc.Kind() == protoreflect.BytesKind && !c.ColDesc.Nullable {
				g.P("\t\tdorisRequiredBytesValue(r.", c.GoFieldName, "),")
			} else if c.IsPointer && !c.ColDesc.Nullable {
				g.P("\t\tdorisRequiredPtrValue(r.", c.GoFieldName, "),")
			} else {
				g.P("\t\tr.", c.GoFieldName, ",")
			}
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
			sqlNullString := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "database/sql", GoName: "NullString"})
			g.P("\tvar ", varName, " ", sqlNullString)
		case c.Field.Desc.Kind() == protoreflect.BoolKind:
			sqlNullBool := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "database/sql", GoName: "NullBool"})
			g.P("\tvar ", varName, " ", sqlNullBool)
		case c.Field.Desc.Kind() == protoreflect.FloatKind || c.Field.Desc.Kind() == protoreflect.DoubleKind:
			sqlNullFloat64 := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "database/sql", GoName: "NullFloat64"})
			g.P("\tvar ", varName, " ", sqlNullFloat64)
		case c.Field.Desc.Kind() == protoreflect.BytesKind:
			g.P("\tvar ", varName, " []byte")
		default:
			sqlNullInt64 := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "database/sql", GoName: "NullInt64"})
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
			if c.ArrayKind == "int64" && goType(c.Field.Desc.Kind()) == "uint32" {
				fmtErrorf := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "fmt", GoName: "Errorf"})
				mathMaxUint32 := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "math", GoName: "MaxUint32"})
				g.P("\tfor idx, elem := range ", varName, " {")
				g.P("\t\tif elem < 0 || elem > ", mathMaxUint32, " {")
				g.P("\t\t\treturn ", fmtErrorf, "(\"ScanRow: column %q element %d value %d overflows uint32\", ", fmt.Sprintf("%q", c.ColDesc.ColumnName), ", idx, elem)")
				g.P("\t\t}")
				g.P("\t}")
			}
			g.P("\tr.", c.GoFieldName, " = ", varName)
		case c.IsTimestamp:
			g.P("\t{")
			g.P("\t\tt, ok, err := scanDorisTime(", varName, ")")
			g.P("\t\tif err != nil {")
			g.P("\t\t\treturn err")
			g.P("\t\t}")
			g.P("\t\tif ok {")
			if c.IsPointer {
				g.P("\t\t\tr.", c.GoFieldName, " = &t")
			} else {
				g.P("\t\t\tr.", c.GoFieldName, " = t")
			}
			g.P("\t\t}")
			g.P("\t}")
		case c.IsDate:
			g.P("\t{")
			g.P("\t\tds, err := scanDorisDateString(", varName, ")")
			g.P("\t\tif err != nil {")
			g.P("\t\t\treturn err")
			g.P("\t\t}")
			g.P("\t\tr.", c.GoFieldName, " = ds")
			g.P("\t}")
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
			if c.IsOneofVariant || (c.Field.Desc.HasOptionalKeyword() && c.ColDesc.Nullable) {
				g.P("\tif ", varName, " != nil {")
			} else {
				g.P("\tif len(", varName, ") > 0 {")
			}
			g.P("\t\tb := make([]byte, len(", varName, "))")
			g.P("\t\tcopy(b, ", varName, ")")
			g.P("\t\tr.", c.GoFieldName, " = b")
			g.P("\t}")
		case c.BaseScalarType == "int32":
			g.P("\t{")
			g.P("\t\tv, ok, err := scanDorisInt32(", varName, ")")
			g.P("\t\tif err != nil {")
			g.P("\t\t\treturn err")
			g.P("\t\t}")
			g.P("\t\tif ok {")
			if c.IsPointer {
				g.P("\t\t\tr.", c.GoFieldName, " = &v")
			} else {
				g.P("\t\t\tr.", c.GoFieldName, " = v")
			}
			g.P("\t\t}")
			g.P("\t}")
		case c.BaseScalarType == "uint32":
			g.P("\t{")
			g.P("\t\tv, ok, err := scanDorisUint32(", varName, ")")
			g.P("\t\tif err != nil {")
			g.P("\t\t\treturn err")
			g.P("\t\t}")
			g.P("\t\tif ok {")
			if c.IsPointer {
				g.P("\t\t\tr.", c.GoFieldName, " = &v")
			} else {
				g.P("\t\t\tr.", c.GoFieldName, " = v")
			}
			g.P("\t\t}")
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

// buildDorisInlineTree groups flattened inline_nested columns into a tree of
// nested message nodes so ToProto and FromProto can reconstruct/read nested messages.
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
	case c.ArrayKind == "uint64":
		g.P("\tif len(r.", c.GoFieldName, ") > 0 {")
		markSet("\t\t")
		g.P("\t\t", targetVar, ".", protoField, " = make([]uint64, len(r.", c.GoFieldName, "))")
		g.P("\t\tcopy(", targetVar, ".", protoField, ", r.", c.GoFieldName, ")")
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
			if c.IsOneofVariant {
				oneofWrapper := g.QualifiedGoIdent(c.Field.GoIdent)
				g.P("\t\t", targetVar, ".", c.Field.Oneof.GoName, " = &", oneofWrapper, "{", protoField, ": ", tsNew, "(r.", c.GoFieldName, ".UTC())}")
			} else {
				g.P("\t\t", targetVar, ".", protoField, " = ", tsNew, "(r.", c.GoFieldName, ".UTC())")
			}
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
		if c.IsOneofVariant {
			oneofWrapper := g.QualifiedGoIdent(c.Field.GoIdent)
			g.P("\t\t\t", targetVar, ".", c.Field.Oneof.GoName, " = &", oneofWrapper, "{", protoField, ": dt}")
		} else {
			g.P("\t\t\t", targetVar, ".", protoField, " = dt")
		}
		g.P("\t\t}")
		g.P("\t}")
	case c.IsEnum:
		enumIdent := g.QualifiedGoIdent(c.Field.Enum.GoIdent)
		if c.IsStringEnum {
			if c.IsPointer {
				g.P("\tif r.", c.GoFieldName, " != nil && *r.", c.GoFieldName, " != \"\" {")
				if c.IsOneofVariant {
					oneofWrapper := g.QualifiedGoIdent(c.Field.GoIdent)
					g.P("\t\tif rawVal, ok := lookupProtoEnumValueOk(", enumIdent, "_value, *r.", c.GoFieldName, ", ", fmt.Sprintf("%q", c.EnumPrefix), "); ok {")
					markSet("\t\t\t")
					g.P("\t\t\t", targetVar, ".", c.Field.Oneof.GoName, " = &", oneofWrapper, "{", protoField, ": ", enumIdent, "(rawVal)}")
					g.P("\t\t}")
				} else if c.Field.Desc.HasOptionalKeyword() {
					g.P("\t\tif rawVal, ok := lookupProtoEnumValueOk(", enumIdent, "_value, *r.", c.GoFieldName, ", ", fmt.Sprintf("%q", c.EnumPrefix), "); ok {")
					markSet("\t\t\t")
					g.P("\t\t\tv := ", enumIdent, "(rawVal)")
					g.P("\t\t\t", targetVar, ".", protoField, " = &v")
					g.P("\t\t}")
				} else {
					g.P("\t\tif rawVal := lookupProtoEnumValue(", enumIdent, "_value, *r.", c.GoFieldName, ", ", fmt.Sprintf("%q", c.EnumPrefix), "); rawVal != 0 {")
					markSet("\t\t\t")
					g.P("\t\t\t", targetVar, ".", protoField, " = ", enumIdent, "(rawVal)")
					g.P("\t\t}")
				}
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
				if c.IsOneofVariant {
					oneofWrapper := g.QualifiedGoIdent(c.Field.GoIdent)
					g.P("\t\t", targetVar, ".", c.Field.Oneof.GoName, " = &", oneofWrapper, "{", protoField, ": ", enumIdent, "(*r.", c.GoFieldName, ")}")
				} else if c.Field.Desc.HasOptionalKeyword() {
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
		if c.IsOneofVariant {
			oneofWrapper := g.QualifiedGoIdent(c.Field.GoIdent)
			g.P("\t\t", targetVar, ".", c.Field.Oneof.GoName, " = &", oneofWrapper, "{", protoField, ": *r.", c.GoFieldName, "}")
		} else if c.Field.Desc.HasOptionalKeyword() {
			g.P("\t\tv := *r.", c.GoFieldName)
			g.P("\t\t", targetVar, ".", protoField, " = &v")
		} else {
			g.P("\t\t", targetVar, ".", protoField, " = *r.", c.GoFieldName)
		}
		g.P("\t}")
	case c.Field.Desc.Kind() == protoreflect.BytesKind:
		if c.IsOneofVariant {
			g.P("\tif r.", c.GoFieldName, " != nil {")
			markSet("\t\t")
			g.P("\t\tb := make([]byte, len(r.", c.GoFieldName, "))")
			g.P("\t\tcopy(b, r.", c.GoFieldName, ")")
			oneofWrapper := g.QualifiedGoIdent(c.Field.GoIdent)
			g.P("\t\t", targetVar, ".", c.Field.Oneof.GoName, " = &", oneofWrapper, "{", protoField, ": b}")
			g.P("\t}")
		} else if c.Field.Desc.HasOptionalKeyword() {
			g.P("\tif r.", c.GoFieldName, " != nil {")
			markSet("\t\t")
			g.P("\t\tb := make([]byte, len(r.", c.GoFieldName, "))")
			g.P("\t\tcopy(b, r.", c.GoFieldName, ")")
			g.P("\t\t", targetVar, ".", protoField, " = b")
			g.P("\t}")
		} else {
			g.P("\tif len(r.", c.GoFieldName, ") > 0 {")
			markSet("\t\t")
			g.P("\t\tb := make([]byte, len(r.", c.GoFieldName, "))")
			g.P("\t\tcopy(b, r.", c.GoFieldName, ")")
			g.P("\t\t", targetVar, ".", protoField, " = b")
			g.P("\t}")
		}
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
	case c.ArrayKind == "uint64":
		g.P(indent, "if src := ", srcVar, ".Get", protoField, "(); len(src) > 0 {")
		g.P(indent, "\tr.", c.GoFieldName, " = make(DorisUint64Array, len(src))")
		g.P(indent, "\tcopy(r.", c.GoFieldName, ", src)")
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
				if c.IsOneofVariant {
					oneofWrapper := g.QualifiedGoIdent(c.Field.GoIdent)
					g.P(indent, "if ov, ok := ", srcVar, ".Get", c.Field.Oneof.GoName, "().(*", oneofWrapper, "); ok && ov != nil {")
					if c.ColDesc.StripEnumPrefix {
						stringsTrimPrefix := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "TrimPrefix"})
						g.P(indent, "\tv := ", stringsTrimPrefix, "(ov.", protoField, ".String(), ", fmt.Sprintf("%q", c.EnumPrefix), ")")
					} else {
						g.P(indent, "\tv := ov.", protoField, ".String()")
					}
					g.P(indent, "\tr.", c.GoFieldName, " = &v")
					g.P(indent, "}")
				} else if c.Field.Desc.HasOptionalKeyword() {
					g.P(indent, "if ", srcVar, ".", protoField, " != nil {")
					if c.ColDesc.StripEnumPrefix {
						stringsTrimPrefix := g.QualifiedGoIdent(protogen.GoIdent{GoImportPath: "strings", GoName: "TrimPrefix"})
						g.P(indent, "\tv := ", stringsTrimPrefix, "(", srcVar, ".Get", protoField, "().String(), ", fmt.Sprintf("%q", c.EnumPrefix), ")")
					} else {
						g.P(indent, "\tv := ", srcVar, ".Get", protoField, "().String()")
					}
					g.P(indent, "\tr.", c.GoFieldName, " = &v")
					g.P(indent, "}")
				} else {
					g.P(indent, "if ", srcVar, ".Get", protoField, "() != 0 {")
					if c.ColDesc.StripEnumPrefix {
						g.P(indent, "\tv := stripProtoEnumPrefix(", srcVar, ".Get", protoField, "().String(), ", fmt.Sprintf("%q", c.EnumPrefix), ")")
					} else {
						g.P(indent, "\tv := ", srcVar, ".Get", protoField, "().String()")
					}
					g.P(indent, "\tr.", c.GoFieldName, " = &v")
					g.P(indent, "}")
				}
			} else if c.ColDesc.ColumnName == "record_status" {
				if c.ColDesc.StripEnumPrefix {
					g.P(indent, "if ", srcVar, ".Get", protoField, "() != 0 {")
					g.P(indent, "\tif v := stripProtoEnumPrefix(", srcVar, ".Get", protoField, "().String(), ", fmt.Sprintf("%q", c.EnumPrefix), "); v != \"\" {")
					g.P(indent, "\t\tr.", c.GoFieldName, " = v")
					g.P(indent, "\t} else {")
					g.P(indent, "\t\tr.", c.GoFieldName, " = \"ACTIVE\"")
					g.P(indent, "\t}")
					g.P(indent, "} else {")
					g.P(indent, "\tr.", c.GoFieldName, " = \"ACTIVE\"")
					g.P(indent, "}")
				} else {
					g.P(indent, "r.", c.GoFieldName, " = ", srcVar, ".Get", protoField, "().String()")
				}
			} else {
				if c.ColDesc.StripEnumPrefix {
					g.P(indent, "if ", srcVar, ".Get", protoField, "() != 0 {")
					g.P(indent, "\tr.", c.GoFieldName, " = stripProtoEnumPrefix(", srcVar, ".Get", protoField, "().String(), ", fmt.Sprintf("%q", c.EnumPrefix), ")")
					g.P(indent, "}")
				} else {
					g.P(indent, "r.", c.GoFieldName, " = ", srcVar, ".Get", protoField, "().String()")
				}
			}
		} else {
			if c.IsPointer {
				if c.IsOneofVariant {
					oneofWrapper := g.QualifiedGoIdent(c.Field.GoIdent)
					g.P(indent, "if ov, ok := ", srcVar, ".Get", c.Field.Oneof.GoName, "().(*", oneofWrapper, "); ok && ov != nil {")
					g.P(indent, "\tv := int32(ov.", protoField, ")")
					g.P(indent, "\tr.", c.GoFieldName, " = &v")
					g.P(indent, "}")
				} else if c.Field.Desc.HasOptionalKeyword() {
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
		if c.IsOneofVariant {
			oneofWrapper := g.QualifiedGoIdent(c.Field.GoIdent)
			g.P(indent, "if ov, ok := ", srcVar, ".Get", c.Field.Oneof.GoName, "().(*", oneofWrapper, "); ok && ov != nil {")
			g.P(indent, "\tv := ov.", protoField)
			g.P(indent, "\tr.", c.GoFieldName, " = &v")
			g.P(indent, "}")
		} else if c.Field.Desc.HasOptionalKeyword() {
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
		if c.IsOneofVariant {
			oneofWrapper := g.QualifiedGoIdent(c.Field.GoIdent)
			g.P(indent, "if ov, ok := ", srcVar, ".Get", c.Field.Oneof.GoName, "().(*", oneofWrapper, "); ok && ov != nil {")
			g.P(indent, "\tb := make([]byte, len(ov.", protoField, "))")
			g.P(indent, "\tcopy(b, ov.", protoField, ")")
			g.P(indent, "\tr.", c.GoFieldName, " = b")
			g.P(indent, "}")
		} else if c.Field.Desc.HasOptionalKeyword() {
			g.P(indent, "if ", srcVar, ".", protoField, " != nil {")
			g.P(indent, "\tb := make([]byte, len(", srcVar, ".", protoField, "))")
			g.P(indent, "\tcopy(b, ", srcVar, ".", protoField, ")")
			g.P(indent, "\tr.", c.GoFieldName, " = b")
			g.P(indent, "}")
		} else {
			g.P(indent, "if src := ", srcVar, ".Get", protoField, "(); len(src) > 0 {")
			g.P(indent, "\tb := make([]byte, len(src))")
			g.P(indent, "\tcopy(b, src)")
			g.P(indent, "\tr.", c.GoFieldName, " = b")
			g.P(indent, "}")
		}
	default:
		g.P(indent, "r.", c.GoFieldName, " = ", srcVar, ".Get", protoField, "()")
	}
}

// formatGoStringSliceLiteral formats a string slice as a Go []string{...} literal or "nil" if empty.
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
