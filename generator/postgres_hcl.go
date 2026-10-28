package generator

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
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
