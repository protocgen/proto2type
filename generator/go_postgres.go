package generator

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// maxPostgresIdentifierBytes is the PostgreSQL NAMEDATALEN-1 identifier byte limit (63 bytes).
const maxPostgresIdentifierBytes = 63

// domainMessagesHaveTableMeta reports whether any non-skipped message in msgs (or its
// nested messages) carries table-level metadata.
func domainMessagesHaveTableMeta(msgs []*DomainMessage) bool {
	for _, m := range msgs {
		if m.Skip {
			continue
		}
		if extractTableMeta(m.ProtoMsg) != nil {
			return true
		}
		if domainMessagesHaveTableMeta(m.NestedMessages) {
			return true
		}
	}
	return false
}

// validateDomainMessagesTableMeta validates table metadata, table name syntax, 63-byte
// identifier length, and table name uniqueness across messages.
func validateDomainMessagesTableMeta(msgs []*DomainMessage, seenTables map[string]string) error {
	for _, m := range msgs {
		if m.Skip {
			continue
		}
		meta := extractTableMeta(m.ProtoMsg)
		tableName := resolvePostgresTableName(m.ProtoMsg, meta)
		if meta != nil {
			if err := validateTableServiceMeta(m.FullName, meta); err != nil {
				return err
			}
			if !isValidSQLIdentifier(tableName) {
				return fmt.Errorf("message %s: invalid PostgreSQL table name %q (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)", m.FullName, tableName)
			}
			if len(tableName) > maxPostgresIdentifierBytes {
				return fmt.Errorf("message %s: PostgreSQL table name %q exceeds %d-byte identifier limit (%d bytes)",
					m.FullName, tableName, maxPostgresIdentifierBytes, len(tableName))
			}
			if seenTables != nil {
				if prev, exists := seenTables[tableName]; exists && prev != m.FullName {
					return fmt.Errorf("message %s: duplicate PostgreSQL table name %q (already resolved by %s)", m.FullName, tableName, prev)
				}
				seenTables[tableName] = m.FullName
			}
		} else if len(tableName) > maxPostgresIdentifierBytes {
			return fmt.Errorf("message %s: PostgreSQL table name %q exceeds %d-byte identifier limit (%d bytes)",
				m.FullName, tableName, maxPostgresIdentifierBytes, len(tableName))
		}
		if err := validateDomainMessagesTableMeta(m.NestedMessages, seenTables); err != nil {
			return err
		}
	}
	return nil
}

// validatePostgresTableNamesAcrossPlugin scans all generated files in gen to detect
// cross-file PostgreSQL table name collisions before emitting ORM, HCL, or DDL files.
func (gg *goGenerator) validatePostgresTableNamesAcrossPlugin(gen *protogen.Plugin) error {
	if gen == nil {
		return nil
	}
	var walkMsgs func(msgs []*protogen.Message) error
	walkMsgs = func(msgs []*protogen.Message) error {
		for _, msg := range msgs {
			if msg.Desc.IsMapEntry() || isMessageSkipped(msg) {
				continue
			}
			meta := extractTableMeta(msg)
			fullName := string(msg.Desc.FullName())
			if meta != nil {
				tableName := resolvePostgresTableName(msg, meta)
				if prev, exists := gg.seenPostgresTables[tableName]; exists && prev != fullName {
					return fmt.Errorf("message %s: duplicate PostgreSQL table name %q (already resolved by %s)", fullName, tableName, prev)
				}
				gg.seenPostgresTables[tableName] = fullName
			}
			if err := walkMsgs(msg.Messages); err != nil {
				return err
			}
		}
		return nil
	}
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		if err := walkMsgs(f.Messages); err != nil {
			return err
		}
	}
	return nil
}

// generateGoPostgresDispatch dispatches PostgreSQL generation across ORM structs,
// Atlas HCL, SQL DDL, and FHIR search_index modes.
func (gg *goGenerator) generateGoPostgresDispatch(gen *protogen.Plugin, file *protogen.File, opts *Options) error {
	effectiveOpts := opts
	if effectiveOpts == nil {
		effectiveOpts = &Options{}
	}
	pgTargets, err := effectiveOpts.ParsePostgresMode()
	if err != nil {
		return fmt.Errorf("proto2type: %w", err)
	}
	if !pgTargets.EmitTypes && !pgTargets.EmitDDL && !pgTargets.EmitHCL &&
		!pgTargets.EmitSearchIndexGo && !pgTargets.EmitSearchIndexDDL && !pgTargets.EmitSearchIndexHCL {
		return nil
	}

	if pgTargets.EmitSearchIndexGo {
		if err := gg.generateGoPostgresSearchIndex(gen, file, effectiveOpts); err != nil {
			return err
		}
	}
	if pgTargets.EmitSearchIndexDDL {
		if err := generatePostgresSearchIndexDDLFile(gen, file); err != nil {
			return err
		}
	}
	if pgTargets.EmitSearchIndexHCL {
		if err := generatePostgresSearchIndexHCLFile(gen, file); err != nil {
			return err
		}
	}

	if !pgTargets.EmitTypes && !pgTargets.EmitDDL && !pgTargets.EmitHCL {
		return nil
	}

	if err := gg.validatePostgresTableNamesAcrossPlugin(gen); err != nil {
		return err
	}

	df, err := BuildDomainFile(file, effectiveOpts)
	if err != nil {
		return err
	}
	if err := validateDomainMessagesTableMeta(df.Messages, gg.seenPostgresTables); err != nil {
		return err
	}

	if pgTargets.EmitTypes {
		if err := gg.generateGoPostgresORMFile(gen, file, df, effectiveOpts); err != nil {
			return err
		}
	}
	if pgTargets.EmitHCL || pgTargets.EmitDDL {
		if fileHasLakehousePostgresTables(file) {
			if pgTargets.EmitHCL {
				if err := generateLakehousePostgresHCLFile(gen, file, effectiveOpts); err != nil {
					return err
				}
			}
			if pgTargets.EmitDDL {
				if err := generateLakehousePostgresDDLFile(gen, file, effectiveOpts); err != nil {
					return err
				}
			}
			return nil
		}
		tables, err := collectPostgresTableSchemas(df, effectiveOpts)
		if err != nil {
			return err
		}
		for _, tbl := range tables {
			owner := tbl.GoName
			if tbl.FullName != "" {
				owner = tbl.FullName
			}
			if prev, exists := gg.seenPostgresTables[tbl.TableName]; exists && prev != owner {
				return fmt.Errorf("message %s: duplicate PostgreSQL table name %q (already resolved by %s)", owner, tbl.TableName, prev)
			}
			gg.seenPostgresTables[tbl.TableName] = owner
		}
		if pgTargets.EmitHCL {
			if err := generatePostgresHCLFile(gen, file, tables); err != nil {
				return err
			}
		}
		if pgTargets.EmitDDL {
			if err := generatePostgresDDLFile(gen, file, tables); err != nil {
				return err
			}
		}
	}
	return nil
}

// generateGoPostgresORMFile generates a Go PostgreSQL storage type file (<prefix>_postgres.type.go).
// Each message gets a `<Name>Postgres` struct with `db:"..."` tags
// suitable for use with sqlx, pgx/v5, or similar Go database libraries.
func (gg *goGenerator) generateGoPostgresORMFile(gen *protogen.Plugin, file *protogen.File, df *DomainFile, opts *Options) error {
	filename, err := outputFilename(file.GeneratedFilenamePrefix, "_postgres.type.go")
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

	msgByName := indexDomainMessages(df.Messages)

	// Header
	g.P("// Code generated by proto2type. DO NOT EDIT.")
	g.P("// source: ", file.Desc.Path())
	g.P("// backend: postgres")
	g.P()
	g.P("package ", goPackageName)
	g.P()

	if irNeedsTime(df.Messages) {
		g.P(`import "time"`)
		g.P()
	}

	if domainMessagesHaveTableMeta(df.Messages) && !gg.emittedPostgresTypes[goImportPath] {
		gg.emittedPostgresTypes[goImportPath] = true
		g.P("// PostgresTableDescriptor describes a PostgreSQL table and its owning writer/reader services.")
		g.P("type PostgresTableDescriptor struct {")
		g.P("\tTableName      string")
		g.P("\tWriterService  string")
		g.P("\tReaderServices []string")
		g.P("}")
		g.P()
	}

	for _, dm := range df.Messages {
		generateGoPostgresMessage(gen, g, dm, msgByName, opts)
	}

	return nil
}

func indexDomainMessages(msgs []*DomainMessage) map[string]*DomainMessage {
	out := make(map[string]*DomainMessage)
	var walk func(list []*DomainMessage)
	walk = func(list []*DomainMessage) {
		for _, m := range list {
			out[m.Name] = m
			walk(m.NestedMessages)
		}
	}
	walk(msgs)
	return out
}

func isDomainFieldInlineNested(f *DomainField) bool {
	if f.Inline {
		return true
	}
	if f.ProtoField != nil {
		if cm := extractColumnMeta(f.ProtoField); cm != nil && cm.InlineNested {
			return true
		}
	}
	return false
}

func cloneDomainMessageWithFlattenedInline(dm *DomainMessage, msgByName map[string]*DomainMessage) (*DomainMessage, bool) {
	visited := map[string]bool{dm.Name: true}
	flatFields, flattened := flattenInlineDomainFields(dm.Fields, "", "", msgByName, visited)
	if !flattened {
		return dm, false
	}
	cp := *dm
	cp.Fields = flatFields
	return &cp, true
}

func flattenInlineDomainFields(
	fields []*DomainField,
	pascalPrefix string,
	snakePrefix string,
	msgByName map[string]*DomainMessage,
	visited map[string]bool,
) ([]*DomainField, bool) {
	var flatFields []*DomainField
	flattened := false
	for _, f := range fields {
		if pascalPrefix != "" && f.IsOneof {
			continue
		}
		if isDomainFieldInlineNested(f) && f.Kind == FieldKindMessage && !f.Repeated && !f.IsMap {
			if nested := msgByName[f.MessageTypeName]; nested != nil && !visited[nested.Name] {
				flattened = true
				fieldSnake := storageFieldName(f.Name)
				if f.NameOverride != "" {
					fieldSnake = f.NameOverride
				}
				nextSnake := fieldSnake
				if snakePrefix != "" && f.NameOverride == "" {
					nextSnake = snakePrefix + "_" + fieldSnake
				}
				nextVisited := make(map[string]bool, len(visited)+1)
				for k, v := range visited {
					nextVisited[k] = v
				}
				nextVisited[nested.Name] = true
				childFields, _ := flattenInlineDomainFields(nested.Fields, pascalPrefix+f.PascalName, nextSnake, msgByName, nextVisited)
				flatFields = append(flatFields, childFields...)
				continue
			}
		}
		if pascalPrefix == "" {
			flatFields = append(flatFields, f)
			continue
		}
		cfCopy := *f
		cfCopy.PascalName = pascalPrefix + f.PascalName
		if f.NameOverride != "" {
			cfCopy.NameOverride = f.NameOverride
		} else {
			cfCopy.NameOverride = snakePrefix + "_" + storageFieldName(f.Name)
		}
		flatFields = append(flatFields, &cfCopy)
	}
	return flatFields, flattened
}

// generateGoPostgresMessage generates a PostgreSQL storage struct for a message.
func generateGoPostgresMessage(gen *protogen.Plugin, g *protogen.GeneratedFile, dm *DomainMessage, msgByName map[string]*DomainMessage, opts *Options) {
	if dm.Skip {
		return
	}

	effectiveDM, flattenedInline := cloneDomainMessageWithFlattenedInline(dm, msgByName)
	name := dm.Name + "Postgres"

	g.P("// ", name, " is the PostgreSQL storage representation of ", dm.FullName, ".")
	g.P("type ", name, " struct {")

	for _, f := range effectiveDM.Fields {
		if f.IsOneof {
			// Emit flattened variant fields with db tags
			oneof := findOneof(dm, f.OneofTypeName)
			g.P("\t// oneof: ", oneof.FieldName)
			for _, v := range oneof.Variants {
				vType := goOneofVariantStorageType(v, "Postgres")
				dbTag := v.ProtoName
				if v.NameOverride != "" {
					if errMsg := validateFieldNameOverride(v.NameOverride); errMsg != "" {
						gen.Error(fmt.Errorf("oneof variant %s in %s: %s", v.Name, dm.FullName, errMsg))
						continue
					}
					dbTag = v.NameOverride
				}
				colMeta := v.ColumnMeta
				if colMeta == nil && v.ProtoField != nil {
					colMeta = extractColumnMeta(v.ProtoField)
				}
				if colMeta != nil && colMeta.ColumnName != "" {
					if errMsg := validateFieldNameOverride(colMeta.ColumnName); errMsg != "" {
						gen.Error(fmt.Errorf("oneof variant %s in %s: %s", v.Name, dm.FullName, errMsg))
						continue
					}
					dbTag = colMeta.ColumnName
				}
				g.P("\t", v.Name, " ", vType, " `db:\"", dbTag, "\"`")
			}
			continue
		}

		fieldName := f.PascalName
		fieldType := goStorageFieldTypeFromIR(f, "Postgres")

		// Build db tag
		var dbTag string
		if f.DocID {
			dbTag = "id"
		} else {
			dbName := storageFieldName(f.Name)
			if f.NameOverride != "" {
				if errMsg := validateFieldNameOverride(f.NameOverride); errMsg != "" {
					gen.Error(fmt.Errorf("field %s in %s: %s", f.Name, dm.FullName, errMsg))
					continue
				}
				dbName = f.NameOverride
			}
			dbTag = dbName
		}
		if f.ProtoField != nil {
			if colMeta := extractColumnMeta(f.ProtoField); colMeta != nil && colMeta.ColumnName != "" {
				if errMsg := validateFieldNameOverride(colMeta.ColumnName); errMsg != "" {
					gen.Error(fmt.Errorf("field %s in %s: %s", f.Name, dm.FullName, errMsg))
					continue
				}
				dbTag = colMeta.ColumnName
			}
		}

		g.P("\t", fieldName, " ", fieldType, " `db:\"", dbTag, "\"`")
	}

	g.P("}")
	g.P()

	if meta := extractTableMeta(dm.ProtoMsg); meta != nil {
		tableName := resolvePostgresTableName(dm.ProtoMsg, meta)
		g.P("// TableName returns the physical PostgreSQL table name for ", name, ".")
		g.P("func (x *", name, ") TableName() string {")
		g.P("\treturn ", fmt.Sprintf("%q", tableName))
		g.P("}")
		g.P()

		g.P("// PostgresTableDescriptor returns the PostgreSQL table descriptor for ", name, ".")
		g.P("func (x *", name, ") PostgresTableDescriptor() PostgresTableDescriptor {")
		g.P("\treturn PostgresTableDescriptor{")
		g.P("\t\tTableName:      ", fmt.Sprintf("%q", tableName), ",")
		g.P("\t\tWriterService:  ", fmt.Sprintf("%q", meta.WriterService), ",")
		g.P("\t\tReaderServices: ", formatGoStringSliceLiteral(meta.ReaderServices), ",")
		g.P("\t}")
		g.P("}")
		g.P()
	}

	if flattenedInline {
		generateFlattenedInlinePostgresConverters(g, dm, msgByName)
	} else {
		// Generate ToProto and FromProto converters
		generateConverters(g, dm, "Postgres", opts)

		// Generate ToDomain and FromDomain converters
		generateDomainConverters(g, dm, "Postgres")
	}

	// Nested messages
	for _, nested := range dm.NestedMessages {
		generateGoPostgresMessage(gen, g, nested, msgByName, opts)
	}
}

func generateFlattenedInlinePostgresConverters(g *protogen.GeneratedFile, dm *DomainMessage, msgByName map[string]*DomainMessage) {
	structName := dm.Name + "Postgres"
	protoType := g.QualifiedGoIdent(dm.ProtoGoIdent)
	recv := receiverName(structName)
	if recv == "d" {
		recv = "s"
	}

	// ToProto
	g.P("// ToProto converts to the protobuf message.")
	g.P("func (", recv, " *", structName, ") ToProto() *", protoType, " {")
	g.P("\tif ", recv, " == nil {")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\td := ", recv, ".ToDomain()")
	g.P("\tif d == nil {")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\treturn d.ToProto()")
	g.P("}")
	g.P()

	// FromProto
	g.P("// FromProto populates from a protobuf message.")
	g.P("func (", recv, " *", structName, ") FromProto(msg *", protoType, ") {")
	g.P("\tif msg == nil {")
	g.P("\t\treturn")
	g.P("\t}")
	g.P("\tvar d ", dm.Name)
	g.P("\td.FromProto(msg)")
	g.P("\t", recv, ".FromDomain(&d)")
	g.P("}")
	g.P()

	var emitToDomainFields func(curDM *DomainMessage, domainPath string, pascalPrefix string, visited map[string]bool)
	emitToDomainFields = func(curDM *DomainMessage, domainPath string, pascalPrefix string, visited map[string]bool) {
		for _, f := range curDM.Fields {
			if f.Computed != nil {
				continue
			}
			if f.IsOneof {
				if pascalPrefix != "" {
					continue
				}
				oneof := findOneof(curDM, f.OneofTypeName)
				for _, v := range oneof.Variants {
					if v.Kind == FieldKindMessage {
						g.P("\tif ", recv, ".", v.Name, " != nil {")
						g.P("\t\t", domainPath, ".", v.Name, " = ", recv, ".", v.Name, ".ToDomain()")
						g.P("\t}")
					} else {
						g.P("\t", domainPath, ".", v.Name, " = ", recv, ".", v.Name)
					}
				}
				continue
			}
			if isDomainFieldInlineNested(f) && f.Kind == FieldKindMessage && !f.Repeated && !f.IsMap {
				if nested := msgByName[f.MessageTypeName]; nested != nil && !visited[nested.Name] {
					g.P("\t", domainPath, ".", f.PascalName, " = &", nested.Name, "{}")
					nextVisited := make(map[string]bool, len(visited)+1)
					for k, v := range visited {
						nextVisited[k] = v
					}
					nextVisited[nested.Name] = true
					emitToDomainFields(nested, domainPath+"."+f.PascalName, pascalPrefix+f.PascalName, nextVisited)
					continue
				}
			}
			storageField := recv + "." + pascalPrefix + f.PascalName
			targetField := domainPath + "." + f.PascalName
			switch {
			case f.Kind == FieldKindTimestamp && f.Optional && !f.Repeated:
				vVar := "v" + pascalPrefix + f.PascalName
				g.P("\t", vVar, " := ", storageField)
				g.P("\tif !", vVar, ".IsZero() {")
				g.P("\t\t", targetField, " = &", vVar)
				g.P("\t}")
			case f.Kind == FieldKindDuration && f.Optional && !f.Repeated:
				vVar := "v" + pascalPrefix + f.PascalName
				g.P("\t", vVar, " := ", storageField)
				g.P("\tif ", vVar, " != 0 {")
				g.P("\t\t", targetField, " = &", vVar)
				g.P("\t}")
			case f.Kind == FieldKindEnum && f.Optional && !f.Repeated:
				vVar := "v" + pascalPrefix + f.PascalName
				g.P("\t", vVar, " := ", storageField)
				if f.EnumAsString {
					g.P("\tif ", vVar, " != \"\" {")
				} else {
					g.P("\tif ", vVar, " != 0 {")
				}
				g.P("\t\t", targetField, " = &", vVar)
				g.P("\t}")
			case f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind && !f.Repeated:
				if f.Optional {
					g.P("\tif ", storageField, " != nil {")
					g.P("\t\tb := make([]byte, len(*", storageField, "))")
					g.P("\t\tcopy(b, *", storageField, ")")
					g.P("\t\t", targetField, " = &b")
					g.P("\t}")
				} else {
					g.P("\tif ", storageField, " != nil {")
					g.P("\t\t", targetField, " = make([]byte, len(", storageField, "))")
					g.P("\t\tcopy(", targetField, ", ", storageField, ")")
					g.P("\t}")
				}
			case f.Kind == FieldKindMessage && !f.IsMap:
				if f.Repeated {
					g.P("\tif len(", storageField, ") > 0 {")
					g.P("\t\t", targetField, " = make([]*", f.MessageTypeName, ", len(", storageField, "))")
					g.P("\t\tfor i, v := range ", storageField, " {")
					g.P("\t\t\tif v != nil {")
					g.P("\t\t\t\t", targetField, "[i] = v.ToDomain()")
					g.P("\t\t\t}")
					g.P("\t\t}")
					g.P("\t}")
				} else {
					g.P("\tif ", storageField, " != nil {")
					g.P("\t\t", targetField, " = ", storageField, ".ToDomain()")
					g.P("\t}")
				}
			default:
				g.P("\t", targetField, " = ", storageField)
			}
		}
	}

	var emitZeroFlattenedFields func(curDM *DomainMessage, pascalPrefix string, visited map[string]bool, indent string)
	emitZeroFlattenedFields = func(curDM *DomainMessage, pascalPrefix string, visited map[string]bool, indent string) {
		for _, cf := range curDM.Fields {
			if cf.Computed != nil || cf.IsOneof {
				continue
			}
			if isDomainFieldInlineNested(cf) && cf.Kind == FieldKindMessage && !cf.Repeated && !cf.IsMap {
				if nested := msgByName[cf.MessageTypeName]; nested != nil && !visited[nested.Name] {
					nextVisited := make(map[string]bool, len(visited)+1)
					for k, v := range visited {
						nextVisited[k] = v
					}
					nextVisited[nested.Name] = true
					emitZeroFlattenedFields(nested, pascalPrefix+cf.PascalName, nextVisited, indent)
					continue
				}
			}
			dstField := recv + "." + pascalPrefix + cf.PascalName
			zeroVar := "zero" + pascalPrefix + cf.PascalName
			g.P(indent, "var ", zeroVar, " ", goStorageFieldTypeFromIR(cf, "Postgres"))
			g.P(indent, dstField, " = ", zeroVar)
		}
	}

	var emitFromDomainFields func(curDM *DomainMessage, domainPath string, pascalPrefix string, visited map[string]bool, indent string)
	emitFromDomainFields = func(curDM *DomainMessage, domainPath string, pascalPrefix string, visited map[string]bool, indent string) {
		for _, f := range curDM.Fields {
			if f.Computed != nil {
				continue
			}
			if f.IsOneof {
				if pascalPrefix != "" {
					continue
				}
				oneof := findOneof(curDM, f.OneofTypeName)
				for _, v := range oneof.Variants {
					if v.Kind == FieldKindMessage {
						g.P(indent, "if ", domainPath, ".", v.Name, " != nil {")
						g.P(indent, "\t", recv, ".", v.Name, " = &", v.TypeName, "Postgres{}")
						g.P(indent, "\t", recv, ".", v.Name, ".FromDomain(", domainPath, ".", v.Name, ")")
						g.P(indent, "} else {")
						g.P(indent, "\t", recv, ".", v.Name, " = nil")
						g.P(indent, "}")
					} else {
						g.P(indent, recv, ".", v.Name, " = ", domainPath, ".", v.Name)
					}
				}
				continue
			}
			if isDomainFieldInlineNested(f) && f.Kind == FieldKindMessage && !f.Repeated && !f.IsMap {
				if nested := msgByName[f.MessageTypeName]; nested != nil && !visited[nested.Name] {
					nextVisited := make(map[string]bool, len(visited)+1)
					for k, v := range visited {
						nextVisited[k] = v
					}
					nextVisited[nested.Name] = true
					g.P(indent, "if ", domainPath, ".", f.PascalName, " != nil {")
					emitFromDomainFields(nested, domainPath+"."+f.PascalName, pascalPrefix+f.PascalName, nextVisited, indent+"\t")
					g.P(indent, "} else {")
					emitZeroFlattenedFields(nested, pascalPrefix+f.PascalName, nextVisited, indent+"\t")
					g.P(indent, "}")
					continue
				}
			}
			dstField := recv + "." + pascalPrefix + f.PascalName
			srcField := domainPath + "." + f.PascalName
			switch {
			case (f.Kind == FieldKindTimestamp || f.Kind == FieldKindDuration || f.Kind == FieldKindEnum) && f.Optional && !f.Repeated:
				g.P(indent, "if ", srcField, " != nil {")
				g.P(indent, "\t", dstField, " = *", srcField)
				g.P(indent, "} else {")
				zeroVar := "zero" + pascalPrefix + f.PascalName
				g.P(indent, "\tvar ", zeroVar, " ", goStorageFieldTypeFromIR(f, "Postgres"))
				g.P(indent, "\t", dstField, " = ", zeroVar)
				g.P(indent, "}")
			case f.Kind == FieldKindScalar && f.ScalarKind == protoreflect.BytesKind && !f.Repeated:
				if f.Optional {
					g.P(indent, "if ", srcField, " != nil {")
					g.P(indent, "\tb := make([]byte, len(*", srcField, "))")
					g.P(indent, "\tcopy(b, *", srcField, ")")
					g.P(indent, "\t", dstField, " = &b")
					g.P(indent, "} else {")
					g.P(indent, "\t", dstField, " = nil")
					g.P(indent, "}")
				} else {
					g.P(indent, "if ", srcField, " != nil {")
					g.P(indent, "\t", dstField, " = make([]byte, len(", srcField, "))")
					g.P(indent, "\tcopy(", dstField, ", ", srcField, ")")
					g.P(indent, "} else {")
					g.P(indent, "\t", dstField, " = nil")
					g.P(indent, "}")
				}
			case f.Kind == FieldKindMessage && !f.IsMap:
				if f.Repeated {
					g.P(indent, "if len(", srcField, ") > 0 {")
					g.P(indent, "\t", dstField, " = make([]*", f.MessageTypeName, "Postgres, len(", srcField, "))")
					g.P(indent, "\tfor i, v := range ", srcField, " {")
					g.P(indent, "\t\tif v != nil {")
					g.P(indent, "\t\t\telem := &", f.MessageTypeName, "Postgres{}")
					g.P(indent, "\t\t\telem.FromDomain(v)")
					g.P(indent, "\t\t\t", dstField, "[i] = elem")
					g.P(indent, "\t\t}")
					g.P(indent, "\t}")
					g.P(indent, "} else {")
					g.P(indent, "\t", dstField, " = nil")
					g.P(indent, "}")
				} else {
					g.P(indent, "if ", srcField, " != nil {")
					g.P(indent, "\t", dstField, " = &", f.MessageTypeName, "Postgres{}")
					g.P(indent, "\t", dstField, ".FromDomain(", srcField, ")")
					g.P(indent, "} else {")
					g.P(indent, "\t", dstField, " = nil")
					g.P(indent, "}")
				}
			default:
				g.P(indent, dstField, " = ", srcField)
			}
		}
	}

	// ToDomain
	g.P("// ToDomain converts to the domain type.")
	g.P("// Note: inline nested messages are flattened into parent columns without a synthetic")
	g.P("// presence column, so an absent inline nested message round-trips as a zero-valued struct.")
	g.P("func (", recv, " *", structName, ") ToDomain() *", dm.Name, " {")
	g.P("\tif ", recv, " == nil {")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\td := &", dm.Name, "{}")
	emitToDomainFields(dm, "d", "", map[string]bool{dm.Name: true})
	g.P("\treturn d")
	g.P("}")
	g.P()

	// FromDomain
	g.P("// FromDomain populates from the domain type.")
	g.P("func (", recv, " *", structName, ") FromDomain(d *", dm.Name, ") {")
	g.P("\tif d == nil {")
	g.P("\t\treturn")
	g.P("\t}")
	emitFromDomainFields(dm, "d", "", map[string]bool{dm.Name: true}, "\t")
	g.P("}")
	g.P()
}
