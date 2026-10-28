package generator

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
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
	hasInline := false
	for _, f := range dm.Fields {
		if isDomainFieldInlineNested(f) && f.Kind == FieldKindMessage && !f.Repeated && !f.IsMap && msgByName[f.MessageTypeName] != nil {
			hasInline = true
			break
		}
	}
	if !hasInline {
		return dm, false
	}
	cp := *dm
	var flatFields []*DomainField
	for _, f := range dm.Fields {
		if isDomainFieldInlineNested(f) && f.Kind == FieldKindMessage && !f.Repeated && !f.IsMap {
			if nested := msgByName[f.MessageTypeName]; nested != nil {
				parentPrefixSnake := storageFieldName(f.Name)
				if f.NameOverride != "" {
					parentPrefixSnake = f.NameOverride
				}
				for _, cf := range nested.Fields {
					if cf.IsOneof {
						continue
					}
					cfCopy := *cf
					cfCopy.PascalName = f.PascalName + cf.PascalName
					if cf.NameOverride != "" {
						cfCopy.NameOverride = cf.NameOverride
					} else {
						cfCopy.NameOverride = parentPrefixSnake + "_" + storageFieldName(cf.Name)
					}
					flatFields = append(flatFields, &cfCopy)
				}
				continue
			}
		}
		flatFields = append(flatFields, f)
	}
	cp.Fields = flatFields
	return &cp, true
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

	// ToDomain
	g.P("// ToDomain converts to the domain type.")
	g.P("func (", recv, " *", structName, ") ToDomain() *", dm.Name, " {")
	g.P("\tif ", recv, " == nil {")
	g.P("\t\treturn nil")
	g.P("\t}")
	g.P("\td := &", dm.Name, "{}")
	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		if f.Inline && f.Kind == FieldKindMessage && !f.Repeated && !f.IsMap {
			if nested := msgByName[f.MessageTypeName]; nested != nil {
				g.P("\td.", f.PascalName, " = &", nested.Name, "{")
				for _, cf := range nested.Fields {
					if cf.IsOneof {
						continue
					}
					g.P("\t\t", cf.PascalName, ": ", recv, ".", f.PascalName, cf.PascalName, ",")
				}
				g.P("\t}")
				continue
			}
		}
		g.P("\td.", f.PascalName, " = ", recv, ".", f.PascalName)
	}
	g.P("\treturn d")
	g.P("}")
	g.P()

	// FromDomain
	g.P("// FromDomain populates from the domain type.")
	g.P("func (", recv, " *", structName, ") FromDomain(d *", dm.Name, ") {")
	g.P("\tif d == nil {")
	g.P("\t\treturn")
	g.P("\t}")
	for _, f := range dm.Fields {
		if f.IsOneof {
			continue
		}
		if f.Inline && f.Kind == FieldKindMessage && !f.Repeated && !f.IsMap {
			if nested := msgByName[f.MessageTypeName]; nested != nil {
				g.P("\tif d.", f.PascalName, " != nil {")
				for _, cf := range nested.Fields {
					if cf.IsOneof {
						continue
					}
					g.P("\t\t", recv, ".", f.PascalName, cf.PascalName, " = d.", f.PascalName, ".", cf.PascalName)
				}
				g.P("\t}")
				continue
			}
		}
		g.P("\t", recv, ".", f.PascalName, " = d.", f.PascalName)
	}
	g.P("}")
	g.P()
}
