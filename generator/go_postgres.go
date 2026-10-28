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
	mode := ""
	if opts != nil {
		mode = opts.PostgresMode
	}
	switch mode {
	case "none":
		return nil
	case "search_index":
		return gg.generateGoPostgresSearchIndex(gen, file, opts)
	case "", "orm", "hcl", "ddl", "both", "tables", "all":
	default:
		return fmt.Errorf("proto2type: unknown postgres_mode %q (supported: '', 'orm', 'none', 'hcl', 'ddl', 'both', 'tables', 'all', 'search_index')", mode)
	}

	if err := gg.validatePostgresTableNamesAcrossPlugin(gen); err != nil {
		return err
	}

	df, err := BuildDomainFile(file, opts)
	if err != nil {
		return err
	}
	if err := validateDomainMessagesTableMeta(df.Messages, gg.seenPostgresTables); err != nil {
		return err
	}

	emitORM := mode == "" || mode == "orm" || mode == "both" || mode == "tables" || mode == "all"
	emitHCL := mode == "hcl" || mode == "both" || mode == "tables" || mode == "all"
	emitDDL := mode == "ddl" || mode == "both" || mode == "tables" || mode == "all"

	if emitORM {
		if err := gg.generateGoPostgresORMFile(gen, file, df, opts); err != nil {
			return err
		}
	}
	if emitHCL || emitDDL {
		tables, err := collectPostgresTableSchemas(df, opts)
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
		if emitHCL {
			if err := generatePostgresHCLFile(gen, file, tables); err != nil {
				return err
			}
		}
		if emitDDL {
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
		generateGoPostgresMessage(gen, g, dm, opts)
	}

	return nil
}

// generateGoPostgresMessage generates a PostgreSQL storage struct for a message.
func generateGoPostgresMessage(gen *protogen.Plugin, g *protogen.GeneratedFile, dm *DomainMessage, opts *Options) {
	if dm.Skip {
		return
	}

	name := dm.Name + "Postgres"

	g.P("// ", name, " is the PostgreSQL storage representation of ", dm.FullName, ".")
	g.P("type ", name, " struct {")

	for _, f := range dm.Fields {
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
		} else if f.Inline {
			// PostgreSQL doesn't support inline; flatten manually.
			dbTag = storageFieldName(f.Name)
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

	// Generate ToProto and FromProto converters
	generateConverters(g, dm, "Postgres", opts)

	// Generate ToDomain and FromDomain converters
	generateDomainConverters(g, dm, "Postgres")

	// Nested messages
	for _, nested := range dm.NestedMessages {
		generateGoPostgresMessage(gen, g, nested, opts)
	}
}
