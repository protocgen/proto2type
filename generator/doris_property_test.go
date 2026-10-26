package generator

import (
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"hegel.dev/go/hegel"
)

// encodeFullTableMetaWire encodes all 13 fields of DorisTableMetaIR into extension extNum.
func encodeFullTableMetaWire(extNum protowire.Number, m DorisTableMetaIR, useEnumVarints bool) []byte {
	var b []byte
	if m.TableName != "" {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, m.TableName)
	}
	if m.DatabaseScope != "" {
		if useEnumVarints {
			var v uint64
			switch m.DatabaseScope {
			case "TENANT":
				v = 1
			case "GLOBAL_TERMINOLOGY":
				v = 2
			case "GLOBAL_TERMINOLOGY_RAG":
				v = 3
			case "GLOBAL_TELEMETRY":
				v = 4
			}
			if v != 0 {
				b = protowire.AppendTag(b, 2, protowire.VarintType)
				b = protowire.AppendVarint(b, v)
			}
		} else {
			b = protowire.AppendTag(b, 2, protowire.BytesType)
			b = protowire.AppendString(b, m.DatabaseScope)
		}
	}
	if m.SyncTier != "" {
		if useEnumVarints {
			var v uint64
			switch m.SyncTier {
			case "WHOLE_TABLE":
				v = 1
			case "MONTHLY_PARTITIONED":
				v = 2
			case "RELEASE_PARTITIONED":
				v = 3
			case "INTERNAL_ONLY":
				v = 4
			case "ICEBERG_ONLY":
				v = 5
			}
			if v != 0 {
				b = protowire.AppendTag(b, 3, protowire.VarintType)
				b = protowire.AppendVarint(b, v)
			}
		} else {
			b = protowire.AppendTag(b, 3, protowire.BytesType)
			b = protowire.AppendString(b, m.SyncTier)
		}
	}
	for _, s := range m.UniqueKeyColumns {
		b = protowire.AppendTag(b, 4, protowire.BytesType)
		b = protowire.AppendString(b, s)
	}
	for _, s := range m.PartitionByColumns {
		b = protowire.AppendTag(b, 5, protowire.BytesType)
		b = protowire.AppendString(b, s)
	}
	if m.ColocateGroup != "" {
		b = protowire.AppendTag(b, 6, protowire.BytesType)
		b = protowire.AppendString(b, m.ColocateGroup)
	}
	if m.SequenceCol != "" {
		b = protowire.AppendTag(b, 7, protowire.BytesType)
		b = protowire.AppendString(b, m.SequenceCol)
	}
	for _, s := range m.InvertedIndexColumns {
		b = protowire.AppendTag(b, 8, protowire.BytesType)
		b = protowire.AppendString(b, s)
	}
	for _, s := range m.NgramBfColumns {
		b = protowire.AppendTag(b, 9, protowire.BytesType)
		b = protowire.AppendString(b, s)
	}
	for _, s := range m.BloomFilterColumns {
		b = protowire.AppendTag(b, 10, protowire.BytesType)
		b = protowire.AppendString(b, s)
	}
	if m.DistributedByColumn != "" {
		b = protowire.AppendTag(b, 11, protowire.BytesType)
		b = protowire.AppendString(b, m.DistributedByColumn)
	}
	for _, s := range m.DuplicateKeyColumns {
		b = protowire.AppendTag(b, 12, protowire.BytesType)
		b = protowire.AppendString(b, s)
	}
	if m.FileCacheTTLSeconds != 0 {
		b = protowire.AppendTag(b, 13, protowire.VarintType)
		b = protowire.AppendVarint(b, uint64(int64(m.FileCacheTTLSeconds)))
	}
	if m.StorageVaultName != "" {
		b = protowire.AppendTag(b, 14, protowire.BytesType)
		b = protowire.AppendString(b, m.StorageVaultName)
	}
	if m.IcebergCatalog != "" {
		b = protowire.AppendTag(b, 15, protowire.BytesType)
		b = protowire.AppendString(b, m.IcebergCatalog)
	}
	return buildWireExtension(extNum, b)
}

func drawCleanSQLIdent(ht *hegel.T, fallback string) string {
	raw := hegel.Draw(ht, hegel.Text().MinSize(1).MaxSize(16))
	var b strings.Builder
	for i, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' {
			b.WriteRune(r)
		} else if i > 0 && r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return fallback
	}
	return b.String()
}

func drawStringSlice(ht *hegel.T, maxLen int) []string {
	n := hegel.Draw(ht, hegel.Integers(0, maxLen))
	if n == 0 {
		return nil
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = drawCleanSQLIdent(ht, "col")
	}
	return out
}

// Property: Wire option parsers never panic on arbitrary corrupted/random bytes.
func TestProperty_DorisWireOptions_NeverPanics(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		raw := []byte(hegel.Draw(ht, hegel.Text().MaxSize(256)))
		_ = parseDorisTableMetaFromOptionsWire(raw)
		_ = parseDorisColumnMetaFromOptionsWire(raw)

		var tm DorisTableMetaIR
		parseTableMetaBytes(raw, &tm)
		var cm DorisColumnMetaIR
		parseColumnMetaBytes(raw, &cm)
	})
}

// Property: Encoding any DorisTableMetaIR / DorisColumnMetaIR to wire 50201 / 50200
// and parsing back is a lossless round-trip (and returns nil iff all fields are zero).
func TestProperty_DorisWireOptions_RoundTrip(t *testing.T) {
	scopes := []string{"", "TENANT", "GLOBAL_TERMINOLOGY", "GLOBAL_TERMINOLOGY_RAG", "GLOBAL_TELEMETRY"}
	tiers := []string{"", "WHOLE_TABLE", "MONTHLY_PARTITIONED", "RELEASE_PARTITIONED", "INTERNAL_ONLY", "ICEBERG_ONLY"}

	hegel.Test(t, func(ht *hegel.T) {
		tm := DorisTableMetaIR{
			TableName:            hegel.Draw(ht, hegel.Text().MaxSize(24)),
			DatabaseScope:        scopes[hegel.Draw(ht, hegel.Integers(0, len(scopes)-1))],
			SyncTier:             tiers[hegel.Draw(ht, hegel.Integers(0, len(tiers)-1))],
			UniqueKeyColumns:     drawStringSlice(ht, 3),
			PartitionByColumns:   drawStringSlice(ht, 2),
			ColocateGroup:        hegel.Draw(ht, hegel.Text().MaxSize(16)),
			SequenceCol:          hegel.Draw(ht, hegel.Text().MaxSize(16)),
			InvertedIndexColumns: drawStringSlice(ht, 3),
			NgramBfColumns:       drawStringSlice(ht, 2),
			BloomFilterColumns:   drawStringSlice(ht, 2),
			DistributedByColumn:  hegel.Draw(ht, hegel.Text().MaxSize(16)),
			DuplicateKeyColumns:  drawStringSlice(ht, 2),
			FileCacheTTLSeconds:  int32(hegel.Draw(ht, hegel.Integers(-86400, 604800))),
		}
		useEnumVarints := hegel.Draw(ht, hegel.Booleans())
		wireBytes := encodeFullTableMetaWire(legacyTableMetaExtensionNumber, tm, useEnumVarints)
		gotTM := parseDorisTableMetaFromOptionsWire(wireBytes)
		if tm.isEmpty() {
			if gotTM != nil {
				ht.Fatalf("expected nil for empty DorisTableMetaIR, got %+v", gotTM)
			}
		} else {
			if gotTM == nil || !reflect.DeepEqual(*gotTM, tm) {
				ht.Fatalf("DorisTableMetaIR wire round-trip mismatch:\nwant: %+v\ngot:  %+v", tm, gotTM)
			}
		}

		cm := DorisColumnMetaIR{
			DorisType:       hegel.Draw(ht, hegel.Text().MaxSize(20)),
			IcebergType:     hegel.Draw(ht, hegel.Text().MaxSize(20)),
			InvertedIndex:   hegel.Draw(ht, hegel.Booleans()),
			IsUniqueKey:     hegel.Draw(ht, hegel.Booleans()),
			IsSequenceCol:   hegel.Draw(ht, hegel.Booleans()),
			InlineNested:    hegel.Draw(ht, hegel.Booleans()),
			IsPartitionKey:  hegel.Draw(ht, hegel.Booleans()),
			StripEnumPrefix: hegel.Draw(ht, hegel.Booleans()),
			ColumnName:      hegel.Draw(ht, hegel.Text().MaxSize(20)),
			DefaultValue:    hegel.Draw(ht, hegel.Text().MaxSize(20)),
		}
		colWire := buildAzraColumnMetaWire(cm)
		gotCM := parseDorisColumnMetaFromOptionsWire(colWire)
		if cm.isEmpty() {
			if gotCM != nil {
				ht.Fatalf("expected nil for empty DorisColumnMetaIR, got %+v", gotCM)
			}
		} else {
			if gotCM == nil || !reflect.DeepEqual(*gotCM, cm) {
				ht.Fatalf("DorisColumnMetaIR wire round-trip mismatch:\nwant: %+v\ngot:  %+v", cm, gotCM)
			}
		}
	})
}

// Property: SQL identifier, type syntax, default value, and tenant slug validators
// never panic, are deterministic, and reject all SQL injection metacharacters.
func TestProperty_DorisSQLValidators_NeverPanicAndRejectInjection(t *testing.T) {
	hegel.Test(t, func(ht *hegel.T) {
		s := hegel.Draw(ht, hegel.Text().MaxSize(100))

		id1 := isValidSQLIdentifier(s)
		id2 := isValidSQLIdentifier(s)
		if id1 != id2 {
			ht.Fatalf("isValidSQLIdentifier non-deterministic on %q", s)
		}

		ty1 := isValidSQLTypeSyntax(s)
		ty2 := isValidSQLTypeSyntax(s)
		if ty1 != ty2 {
			ht.Fatalf("isValidSQLTypeSyntax non-deterministic on %q", s)
		}

		dv1 := isValidDorisDefaultValue(s)
		dv2 := isValidDorisDefaultValue(s)
		if dv1 != dv2 {
			ht.Fatalf("isValidDorisDefaultValue non-deterministic on %q", s)
		}

		slug, slugOK := sanitizeDorisTenantSlug(s)
		slug2, slugOK2 := sanitizeDorisTenantSlug(s)
		if slug != slug2 || slugOK != slugOK2 {
			ht.Fatalf("sanitizeDorisTenantSlug non-deterministic on %q", s)
		}
		if slugOK && slug != "${TENANT_SLUG}" {
			if !isValidSQLIdentifier(slug) || strings.ContainsAny(slug, ";'\"`\\\n\r\x00") {
				ht.Fatalf("sanitizeDorisTenantSlug returned unsafe or invalid SQL identifier: %q -> %q", s, slug)
			}
		}

		hasInjectionChar := strings.ContainsAny(s, ";'\"`\\\n\r\x00")
		if hasInjectionChar {
			if id1 {
				ht.Fatalf("isValidSQLIdentifier accepted string with injection char: %q", s)
			}
			if ty1 {
				ht.Fatalf("isValidSQLTypeSyntax accepted string with injection char: %q", s)
			}
			if dv1 {
				ht.Fatalf("isValidDorisDefaultValue accepted string with injection char: %q", s)
			}
		}
	})
}

// Property: BuildDorisCreateTableSQL and BuildIcebergCreateTableSQL are deterministic,
// never mutate receiver Columns in-place, and only emit balanced, injection-free DDL.
func TestProperty_DorisAndIcebergDDL_DeterminismImmutabilityAndSafety(t *testing.T) {
	scopes := []string{"", "TENANT", "GLOBAL_TERMINOLOGY", "GLOBAL_TERMINOLOGY_RAG", "GLOBAL_TELEMETRY", "BAD_SCOPE"}
	tiers := []string{"", "WHOLE_TABLE", "MONTHLY_PARTITIONED", "RELEASE_PARTITIONED", "INTERNAL_ONLY", "ICEBERG_ONLY", "BAD_TIER"}
	dorisTypes := []string{
		"VARCHAR(64)", "INT", "SMALLINT", "BIGINT", "LARGEINT", "BOOLEAN",
		"DATEV2", "DATETIMEV2(6)", "DECIMALV3(10,2)", "TEXT", "DOUBLE",
		"ARRAY<VARCHAR(64)>", "ARRAY<FLOAT>", "ARRAY<LARGEINT>", "BAD TYPE;",
	}

	hegel.Test(t, func(ht *hegel.T) {
		numCols := hegel.Draw(ht, hegel.Integers(1, 5))
		cols := make([]DorisColumnDescriptor, numCols)
		for i := 0; i < numCols; i++ {
			colName := drawCleanSQLIdent(ht, "col")
			if i == 0 && hegel.Draw(ht, hegel.Booleans()) {
				colName = "patient_id"
			}
			dt := dorisTypes[hegel.Draw(ht, hegel.Integers(0, len(dorisTypes)-1))]
			defVal := ""
			if hegel.Draw(ht, hegel.Booleans()) {
				defVal = drawCleanSQLIdent(ht, "DEF")
			}
			cols[i] = DorisColumnDescriptor{
				ColumnName:     colName,
				DorisType:      dt,
				Nullable:       hegel.Draw(ht, hegel.Booleans()),
				InvertedIndex:  hegel.Draw(ht, hegel.Booleans()),
				IsUniqueKey:    hegel.Draw(ht, hegel.Booleans()),
				IsSequenceCol:  i == numCols-1 && hegel.Draw(ht, hegel.Booleans()),
				IsPartitionKey: hegel.Draw(ht, hegel.Booleans()),
				DefaultValue:   defVal,
			}
		}

		origCols := make([]DorisColumnDescriptor, len(cols))
		copy(origCols, cols)

		desc := DorisTableDescriptor{
			TableName:     drawCleanSQLIdent(ht, "tbl"),
			MessageName:   "TestMsg",
			DatabaseScope: scopes[hegel.Draw(ht, hegel.Integers(0, len(scopes)-1))],
			SyncTier:      tiers[hegel.Draw(ht, hegel.Integers(0, len(tiers)-1))],
			Columns:       cols,
		}
		if hegel.Draw(ht, hegel.Booleans()) {
			desc.UniqueKeyColumns = []string{cols[len(cols)-1].ColumnName}
		}

		slug := hegel.Draw(ht, hegel.Text().MaxSize(20))
		buckets := hegel.Draw(ht, hegel.Integers(-2, 16))

		doris1 := desc.BuildDorisCreateTableSQL(slug, buckets)
		doris2 := desc.BuildDorisCreateTableSQL(slug, buckets)
		if doris1 != doris2 {
			ht.Fatalf("BuildDorisCreateTableSQL non-deterministic")
		}

		sr1 := desc.BuildStarRocksCreateTableSQL(slug, buckets)
		sr2 := desc.BuildStarRocksCreateTableSQL(slug, buckets)
		if sr1 != sr2 {
			ht.Fatalf("BuildStarRocksCreateTableSQL non-deterministic")
		}

		ice1 := desc.BuildIcebergCreateTableSQL(slug)
		ice2 := desc.BuildIcebergCreateTableSQL(slug)
		if ice1 != ice2 {
			ht.Fatalf("BuildIcebergCreateTableSQL non-deterministic")
		}

		dorisHCL1 := desc.BuildDorisAtlasHCL(slug, buckets)
		dorisHCL2 := desc.BuildDorisAtlasHCL(slug, buckets)
		if dorisHCL1 != dorisHCL2 {
			ht.Fatalf("BuildDorisAtlasHCL non-deterministic")
		}

		srHCL1 := desc.BuildStarRocksAtlasHCL(slug, buckets)
		srHCL2 := desc.BuildStarRocksAtlasHCL(slug, buckets)
		if srHCL1 != srHCL2 {
			ht.Fatalf("BuildStarRocksAtlasHCL non-deterministic")
		}

		iceHCL1 := desc.BuildIcebergAtlasHCL(slug)
		iceHCL2 := desc.BuildIcebergAtlasHCL(slug)
		if iceHCL1 != iceHCL2 {
			ht.Fatalf("BuildIcebergAtlasHCL non-deterministic")
		}

		if !reflect.DeepEqual(desc.Columns, origCols) {
			ht.Fatalf("DDL/HCL generation mutated receiver Columns in-place:\norig: %+v\ngot:  %+v", origCols, desc.Columns)
		}

		for _, sqlStr := range []string{doris1, sr1, ice1} {
			if sqlStr == "" {
				continue
			}
			if !strings.HasPrefix(sqlStr, "CREATE TABLE IF NOT EXISTS ") || !strings.HasSuffix(sqlStr, "\n);") {
				ht.Fatalf("malformed DDL envelope:\n%s", sqlStr)
			}
			withoutFinalSemi := sqlStr[:len(sqlStr)-1]
			if strings.Contains(withoutFinalSemi, ";") {
				ht.Fatalf("unexpected semicolon inside DDL:\n%s", sqlStr)
			}
			if strings.Count(sqlStr, "(") != strings.Count(sqlStr, ")") {
				ht.Fatalf("unbalanced parentheses in DDL:\n%s", sqlStr)
			}
		}

		for _, hclStr := range []string{dorisHCL1, srHCL1, iceHCL1} {
			if hclStr == "" {
				continue
			}
			if !strings.HasPrefix(hclStr, "table \"") || !strings.HasSuffix(hclStr, "}") {
				ht.Fatalf("malformed HCL envelope:\n%s", hclStr)
			}
			if strings.Count(hclStr, "{") != strings.Count(hclStr, "}") {
				ht.Fatalf("unbalanced braces in HCL:\n%s", hclStr)
			}
			parsed, err := ParseDorisAtlasHCL(hclStr)
			if err != nil {
				ht.Fatalf("ParseDorisAtlasHCL failed on generated HCL:\n%s\nerr: %v", hclStr, err)
			}
			if len(parsed) != 1 || parsed[0].TableName != desc.TableName {
				ht.Fatalf("ParseDorisAtlasHCL returned unexpected table descriptors: %+v", parsed)
			}
		}
	})
}

// Property: CanonicalIcebergSpecType, dorisColumnIcebergDDLType, dorisColumnStarRocksDDLType,
// and dorisColumnStarRocksHCLType never panic on arbitrary or nested ARRAY<...> types and map
// LARGEINT / DATEV2 / DATETIMEV2 / DECIMALV3 / TEXT consistently at any depth.
func TestProperty_CanonicalIcebergAndDDLTypeMapping_Totality(t *testing.T) {
	leafTypes := []string{"DATEV2", "DATETIMEV2(6)", "LARGEINT", "BIGINT", "SMALLINT", "TINYINT", "INT", "BOOLEAN", "FLOAT", "DOUBLE", "VARCHAR(64)", "TEXT", "DECIMALV3(12,4)"}

	hegel.Test(t, func(ht *hegel.T) {
		arbDoris := hegel.Draw(ht, hegel.Text().MaxSize(40))
		arbIce := hegel.Draw(ht, hegel.Text().MaxSize(40))
		_ = (DorisColumnDescriptor{DorisType: arbDoris, IcebergType: arbIce}).CanonicalIcebergSpecType()
		_ = dorisColumnIcebergDDLType(arbDoris, arbIce)
		_ = dorisColumnStarRocksDDLType(arbDoris, arbIce)
		_ = dorisColumnStarRocksHCLType(arbDoris, arbIce)

		depth := hegel.Draw(ht, hegel.Integers(0, 3))
		dt := leafTypes[hegel.Draw(ht, hegel.Integers(0, len(leafTypes)-1))]
		for i := 0; i < depth; i++ {
			dt = "ARRAY<" + dt + ">"
		}
		spec := (DorisColumnDescriptor{DorisType: dt}).CanonicalIcebergSpecType()
		ddl := dorisColumnIcebergDDLType(dt, "")
		srDDL := dorisColumnStarRocksDDLType(dt, "")
		srHCL := dorisColumnStarRocksHCLType(dt, "")
		if spec == "" || ddl == "" || srDDL == "" || srHCL == "" {
			ht.Fatalf("empty type mapping for %q: spec=%q ddl=%q srDDL=%q srHCL=%q", dt, spec, ddl, srDDL, srHCL)
		}
		if strings.Contains(spec, "DATEV2") || strings.Contains(spec, "LARGEINT") || strings.Contains(spec, "DECIMALV3") {
			ht.Fatalf("CanonicalIcebergSpecType(%q) leaked Doris token in %q", dt, spec)
		}
		if strings.Contains(ddl, "DATEV2") || strings.Contains(ddl, "LARGEINT") || strings.Contains(ddl, "DECIMALV3") {
			ht.Fatalf("dorisColumnIcebergDDLType(%q) leaked Doris-only token in %q", dt, ddl)
		}
		if strings.Contains(srDDL, "DATEV2") || strings.Contains(srDDL, "DATETIMEV2") || strings.Contains(srDDL, "DECIMALV3") || strings.Contains(srDDL, "TEXT") {
			ht.Fatalf("dorisColumnStarRocksDDLType(%q) leaked Doris-only token in %q", dt, srDDL)
		}
		if strings.Contains(srHCL, "datev2") || strings.Contains(srHCL, "datetimev2") || strings.Contains(srHCL, "decimalv3") || strings.Contains(srHCL, "text") {
			ht.Fatalf("dorisColumnStarRocksHCLType(%q) leaked Doris-only token in %q", dt, srHCL)
		}
	})
}

// Property: GoFieldName disambiguation in buildDorisTableIR always produces unique Go
// struct field names and never collides with reserved struct method names, even when
// root fields and inline_nested child fields share colliding names or overrides.
func TestProperty_GoFieldNameDisambiguation_AlwaysUniqueAndNeverReserved(t *testing.T) {
	candidateNames := []string{
		"id", "meta_id", "table_name", "name", "columns", "values",
		"scan_row", "to_proto", "from_proto", "descriptor", "status",
	}
	parentNames := []string{"meta", "table", " nested_table", "values"}
	reservedMethods := map[string]bool{
		"TableName":  true,
		"Columns":    true,
		"Values":     true,
		"ScanRow":    true,
		"ToProto":    true,
		"FromProto":  true,
		"Descriptor": true,
	}

	hegel.Test(t, func(ht *hegel.T) {
		parentName := strings.TrimSpace(parentNames[hegel.Draw(ht, hegel.Integers(0, len(parentNames)-1))])
		nestedField1 := candidateNames[hegel.Draw(ht, hegel.Integers(0, len(candidateNames)-1))]
		rootField1 := candidateNames[hegel.Draw(ht, hegel.Integers(0, len(candidateNames)-1))]
		if rootField1 == parentName {
			rootField1 = rootField1 + "_r1"
		}
		rootField2 := candidateNames[hegel.Draw(ht, hegel.Integers(0, len(candidateNames)-1))]
		if rootField2 == rootField1 || rootField2 == parentName {
			rootField2 = rootField2 + "_alt"
		}

		// Ensure SQL column names are distinct so we specifically test GoFieldName collisions.
		nestedColOverride := "nested_" + nestedField1 + "_col"
		root1ColOverride := "root1_" + rootField1 + "_col"
		root2ColOverride := "root2_" + rootField2 + "_col"

		colOpt := func(m DorisColumnMetaIR) *descriptorpb.FieldOptions {
			fo := &descriptorpb.FieldOptions{}
			fo.ProtoReflect().SetUnknown(buildAzraColumnMetaWire(m))
			return fo
		}
		tblOpt := &descriptorpb.MessageOptions{}
		tblOpt.ProtoReflect().SetUnknown(buildAzraTableMetaWire("test_collision_tbl", 1, 1, []string{root1ColOverride}, nil, "", "", nil))

		fdp := &descriptorpb.FileDescriptorProto{
			Syntax:  proto.String("proto3"),
			Name:    proto.String("collision_pbt.proto"),
			Package: proto.String("test.pbt.v1"),
			Options: &descriptorpb.FileOptions{
				GoPackage: proto.String("example.com/test/pbtv1;pbtv1"),
			},
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name: proto.String("NestedSub"),
					Field: []*descriptorpb.FieldDescriptorProto{
						{
							Name:     proto.String(nestedField1),
							Number:   proto.Int32(1),
							Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
							Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							JsonName: proto.String("f1"),
							Options:  colOpt(DorisColumnMetaIR{ColumnName: nestedColOverride, DorisType: "VARCHAR(64)"}),
						},
					},
				},
				{
					Name:    proto.String("RootTable"),
					Options: tblOpt,
					Field: []*descriptorpb.FieldDescriptorProto{
						{
							Name:     proto.String(rootField1),
							Number:   proto.Int32(1),
							Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
							Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							JsonName: proto.String("r1"),
							Options:  colOpt(DorisColumnMetaIR{ColumnName: root1ColOverride, DorisType: "VARCHAR(64)", IsUniqueKey: true}),
						},
						{
							Name:     proto.String(parentName),
							Number:   proto.Int32(2),
							Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
							Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
							TypeName: proto.String(".test.pbt.v1.NestedSub"),
							JsonName: proto.String("p1"),
							Options:  colOpt(DorisColumnMetaIR{InlineNested: true}),
						},
						{
							Name:     proto.String(rootField2),
							Number:   proto.Int32(3),
							Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
							Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							JsonName: proto.String("r2"),
							Options:  colOpt(DorisColumnMetaIR{ColumnName: root2ColOverride, DorisType: "VARCHAR(64)"}),
						},
					},
				},
			},
		}

		gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"collision_pbt.proto"})
		msg := findMessage(t, gen, "RootTable")
		meta := extractDorisTableMeta(msg)
		ir, err := buildDorisTableIR(msg, meta, &Options{Lang: "go", Backend: "doris", DorisMode: "both", Domain: true})
		if err != nil {
			ht.Fatalf("buildDorisTableIR failed: %v", err)
		}

		seenGo := make(map[string]bool, len(ir.Columns))
		for _, c := range ir.Columns {
			if reservedMethods[c.GoFieldName] {
				ht.Fatalf("field %s produced reserved method GoFieldName %q", c.Field.Desc.FullName(), c.GoFieldName)
			}
			if seenGo[c.GoFieldName] {
				ht.Fatalf("duplicate GoFieldName %q generated in RootTable", c.GoFieldName)
			}
			seenGo[c.GoFieldName] = true
		}
	})
}
