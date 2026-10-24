package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	proto2typepb "github.com/protocgen/proto2type/proto/proto2type"
)

// buildWireExtension builds a raw protobuf wire field with tag extNum (BytesType)
// wrapping innerBytes.
func buildWireExtension(extNum protowire.Number, innerBytes []byte) []byte {
	var out []byte
	out = protowire.AppendTag(out, extNum, protowire.BytesType)
	out = protowire.AppendBytes(out, innerBytes)
	return out
}

// buildAzraTableMetaWire encodes an azra.common.v1.TableMeta wire payload (extension 50201)
// using enum varints for database_scope (field 2) and sync_tier (field 3).
func buildAzraTableMetaWire(
	tableName string,
	scopeEnum uint64,
	tierEnum uint64,
	uniqueKeys []string,
	partitionCols []string,
	colocateGroup string,
	sequenceCol string,
	invertedCols []string,
) []byte {
	var b []byte
	if tableName != "" {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, tableName)
	}
	if scopeEnum != 0 {
		b = protowire.AppendTag(b, 2, protowire.VarintType)
		b = protowire.AppendVarint(b, scopeEnum)
	}
	if tierEnum != 0 {
		b = protowire.AppendTag(b, 3, protowire.VarintType)
		b = protowire.AppendVarint(b, tierEnum)
	}
	for _, uk := range uniqueKeys {
		b = protowire.AppendTag(b, 4, protowire.BytesType)
		b = protowire.AppendString(b, uk)
	}
	for _, pk := range partitionCols {
		b = protowire.AppendTag(b, 5, protowire.BytesType)
		b = protowire.AppendString(b, pk)
	}
	if colocateGroup != "" {
		b = protowire.AppendTag(b, 6, protowire.BytesType)
		b = protowire.AppendString(b, colocateGroup)
	}
	if sequenceCol != "" {
		b = protowire.AppendTag(b, 7, protowire.BytesType)
		b = protowire.AppendString(b, sequenceCol)
	}
	for _, ic := range invertedCols {
		b = protowire.AppendTag(b, 8, protowire.BytesType)
		b = protowire.AppendString(b, ic)
	}
	return buildWireExtension(azraTableMetaExtensionNumber, b)
}

// buildAzraColumnMetaWire encodes an azra.common.v1.ColumnMeta wire payload (extension 50200).
func buildAzraColumnMetaWire(meta DorisColumnMetaIR) []byte {
	var b []byte
	if meta.DorisType != "" {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, meta.DorisType)
	}
	if meta.IcebergType != "" {
		b = protowire.AppendTag(b, 2, protowire.BytesType)
		b = protowire.AppendString(b, meta.IcebergType)
	}
	if meta.InvertedIndex {
		b = protowire.AppendTag(b, 3, protowire.VarintType)
		b = protowire.AppendVarint(b, 1)
	}
	if meta.IsUniqueKey {
		b = protowire.AppendTag(b, 4, protowire.VarintType)
		b = protowire.AppendVarint(b, 1)
	}
	if meta.IsSequenceCol {
		b = protowire.AppendTag(b, 5, protowire.VarintType)
		b = protowire.AppendVarint(b, 1)
	}
	if meta.InlineNested {
		b = protowire.AppendTag(b, 6, protowire.VarintType)
		b = protowire.AppendVarint(b, 1)
	}
	if meta.IsPartitionKey {
		b = protowire.AppendTag(b, 7, protowire.VarintType)
		b = protowire.AppendVarint(b, 1)
	}
	if meta.StripEnumPrefix {
		b = protowire.AppendTag(b, 8, protowire.VarintType)
		b = protowire.AppendVarint(b, 1)
	}
	if meta.ColumnName != "" {
		b = protowire.AppendTag(b, 9, protowire.BytesType)
		b = protowire.AppendString(b, meta.ColumnName)
	}
	if meta.DefaultValue != "" {
		b = protowire.AppendTag(b, 10, protowire.BytesType)
		b = protowire.AppendString(b, meta.DefaultValue)
	}
	return buildWireExtension(azraColumnMetaExtensionNumber, b)
}

// TestExtractDorisOptions_NativeAndWire50200_50201 verifies that table and column
// metadata can be extracted from both native proto2type extensions (1307) and
// wire-level Azra extensions (50200 / 50201).
func TestExtractDorisOptions_NativeAndWire50200_50201(t *testing.T) {
	// 1. Test native (proto2type.message).table and (proto2type.field).column (extension 1307)
	msgOptsNative := &descriptorpb.MessageOptions{}
	proto.SetExtension(msgOptsNative, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			TableName:            "patients",
			DatabaseScope:        "LAKEHOUSE_DATABASE_SCOPE_TENANT",
			SyncTier:             "LAKEHOUSE_SYNC_TIER_WHOLE_TABLE",
			UniqueKeyColumns:     []string{"patient_id"},
			ColocateGroup:        "patient_cg",
			SequenceCol:          "publication_generation",
			InvertedIndexColumns: []string{"mrn", "all_mrns"},
			DistributedByColumn:  "patient_id",
			DuplicateKeyColumns:  []string{"patient_id"},
			FileCacheTtlSeconds:  604800,
		},
	})

	fieldOptsNative := &descriptorpb.FieldOptions{}
	proto.SetExtension(fieldOptsNative, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		Column: &proto2typepb.ColumnOptions{
			DorisType:       "VARCHAR(64)",
			IcebergType:     "STRING",
			InvertedIndex:   true,
			IsUniqueKey:     true,
			StripEnumPrefix: true,
			ColumnName:      "raw_site_token",
			DefaultValue:    "UNKNOWN",
		},
	})

	// 2. Test wire-level 50201 (azra.common.v1.table_meta) and 50200 (azra.common.v1.column_meta)
	msgOptsWire := &descriptorpb.MessageOptions{}
	msgOptsWire.ProtoReflect().SetUnknown(buildAzraTableMetaWire(
		"snomed_concepts",
		2, // LAKEHOUSE_DATABASE_SCOPE_GLOBAL_TERMINOLOGY
		3, // LAKEHOUSE_SYNC_TIER_RELEASE_PARTITIONED
		nil,
		[]string{"release_id"},
		"",
		"",
		nil,
	))

	fieldOptsWire := &descriptorpb.FieldOptions{}
	fieldOptsWire.ProtoReflect().SetUnknown(buildAzraColumnMetaWire(DorisColumnMetaIR{
		DorisType:      "VARCHAR(64)",
		IcebergType:    "VARCHAR(64)",
		IsPartitionKey: true,
		DefaultValue:   "20260101",
	}))

	// 3. Test empty native TableOptions{} returns nil
	msgOptsEmpty := &descriptorpb.MessageOptions{}
	proto.SetExtension(msgOptsEmpty, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{},
	})

	fdp := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("test_opts.proto"),
		Package: proto.String("test.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/test/v1;testv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("NativeRecord"),
				Options: msgOptsNative,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("raw_site_term"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("rawSiteTerm"),
						Options:  fieldOptsNative,
					},
				},
			},
			{
				Name:    proto.String("WireRecord"),
				Options: msgOptsWire,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("release_id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("releaseId"),
						Options:  fieldOptsWire,
					},
				},
			},
			{
				Name:    proto.String("EmptyTableOptsRecord"),
				Options: msgOptsEmpty,
			},
		},
	}

	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"test_opts.proto"})
	nativeMsg := findMessage(t, gen, "NativeRecord")
	wireMsg := findMessage(t, gen, "WireRecord")
	emptyMsg := findMessage(t, gen, "EmptyTableOptsRecord")

	if extractDorisTableMeta(emptyMsg) != nil {
		t.Error("expected extractDorisTableMeta(EmptyTableOptsRecord) to return nil for empty TableOptions")
	}

	tMeta1 := extractDorisTableMeta(nativeMsg)
	if tMeta1 == nil {
		t.Fatal("extractDorisTableMeta(NativeRecord) returned nil")
	}
	if tMeta1.TableName != "patients" || tMeta1.DatabaseScope != "TENANT" || tMeta1.SyncTier != "WHOLE_TABLE" {
		t.Errorf("unexpected NativeRecord table meta: %+v", tMeta1)
	}
	if tMeta1.DistributedByColumn != "patient_id" || tMeta1.FileCacheTTLSeconds != 604800 || len(tMeta1.DuplicateKeyColumns) != 1 {
		t.Errorf("unexpected NativeRecord extended table meta: %+v", tMeta1)
	}

	cMeta1 := extractDorisColumnMeta(nativeMsg.Fields[0])
	if cMeta1 == nil {
		t.Fatal("extractDorisColumnMeta(NativeRecord.raw_site_term) returned nil")
	}
	if cMeta1.ColumnName != "raw_site_token" || cMeta1.DefaultValue != "UNKNOWN" || !cMeta1.IsUniqueKey || !cMeta1.InvertedIndex || !cMeta1.StripEnumPrefix {
		t.Errorf("unexpected NativeRecord column meta: %+v", cMeta1)
	}

	tMeta2 := extractDorisTableMeta(wireMsg)
	if tMeta2 == nil {
		t.Fatal("extractDorisTableMeta(WireRecord) returned nil")
	}
	if tMeta2.TableName != "snomed_concepts" || tMeta2.DatabaseScope != "GLOBAL_TERMINOLOGY" || tMeta2.SyncTier != "RELEASE_PARTITIONED" {
		t.Errorf("unexpected WireRecord table meta: %+v", tMeta2)
	}

	cMeta2 := extractDorisColumnMeta(wireMsg.Fields[0])
	if cMeta2 == nil {
		t.Fatal("extractDorisColumnMeta(WireRecord.release_id) returned nil")
	}
	if cMeta2.DorisType != "VARCHAR(64)" || !cMeta2.IsPartitionKey || cMeta2.DefaultValue != "20260101" {
		t.Errorf("unexpected WireRecord column meta: %+v", cMeta2)
	}
}

// buildComprehensiveDorisPlugin creates a FileDescriptorSet containing:
//   - google/protobuf/timestamp.proto
//   - google/type/date.proto
//   - clinical_lakehouse.proto (with EligibilityCriterionResult, 2-level inline_nested SkillProvenance -> Citation -> DocumentCitationCoordinate,
//     all scalar/array/enum/optional/uint64/bytes types, unannotated hydrated fields,
//     SnomedConceptRecord in GLOBAL_TERMINOLOGY + RELEASE_PARTITIONED, and AnatomicSiteNormalizationRecord in INTERNAL_ONLY)
//   - secondary_table.proto (in the same Go package to verify package-level type deduplication)
//   - non_table.proto (a message without table_meta to verify clean skipping)
func buildComprehensiveDorisPlugin(t *testing.T) *protogen.Plugin {
	t.Helper()

	tsFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("google/protobuf/timestamp.proto"),
		Package: proto.String("google.protobuf"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("google.golang.org/protobuf/types/known/timestamppb"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Timestamp"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("seconds"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
						JsonName: proto.String("seconds"),
					},
					{
						Name:     proto.String("nanos"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						JsonName: proto.String("nanos"),
					},
				},
			},
		},
	}

	dateFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("google/type/date.proto"),
		Package: proto.String("google.type"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("google.golang.org/genproto/googleapis/type/date;date"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Date"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("year"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						JsonName: proto.String("year"),
					},
					{
						Name:     proto.String("month"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						JsonName: proto.String("month"),
					},
					{
						Name:     proto.String("day"),
						Number:   proto.Int32(3),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						JsonName: proto.String("day"),
					},
				},
			},
		},
	}

	colOpt := func(m DorisColumnMetaIR) *descriptorpb.FieldOptions {
		fo := &descriptorpb.FieldOptions{}
		fo.ProtoReflect().SetUnknown(buildAzraColumnMetaWire(m))
		return fo
	}

	tblOpt := func(name string, scope, tier uint64, uks, parts []string, colocate, seq string, inv []string) *descriptorpb.MessageOptions {
		mo := &descriptorpb.MessageOptions{}
		mo.ProtoReflect().SetUnknown(buildAzraTableMetaWire(name, scope, tier, uks, parts, colocate, seq, inv))
		return mo
	}

	clinicalFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("clinical_lakehouse.proto"),
		Package: proto.String("azra.clinical.v1"),
		Dependency: []string{
			"google/protobuf/timestamp.proto",
			"google/type/date.proto",
		},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/lakehouse/clinicalv1;clinicalv1"),
		},
		EnumType: []*descriptorpb.EnumDescriptorProto{
			{
				Name: proto.String("ClinicalRecordStatus"),
				Value: []*descriptorpb.EnumValueDescriptorProto{
					{Name: proto.String("CLINICAL_RECORD_STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
					{Name: proto.String("CLINICAL_RECORD_STATUS_ACTIVE"), Number: proto.Int32(1)},
					{Name: proto.String("CLINICAL_RECORD_STATUS_SUPERSEDED"), Number: proto.Int32(2)},
				},
			},
			{
				Name: proto.String("CriterionAiStatus"),
				Value: []*descriptorpb.EnumValueDescriptorProto{
					{Name: proto.String("CRITERION_AI_STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
					{Name: proto.String("CRITERION_AI_STATUS_MET"), Number: proto.Int32(1)},
					{Name: proto.String("CRITERION_AI_STATUS_EXCLUDED"), Number: proto.Int32(2)},
				},
			},
			{
				Name: proto.String("LungRadsCategory"),
				Value: []*descriptorpb.EnumValueDescriptorProto{
					{Name: proto.String("LUNG_RADS_CATEGORY_UNSPECIFIED"), Number: proto.Int32(0)},
					{Name: proto.String("LUNG_RADS_CATEGORY_4A"), Number: proto.Int32(1)},
					{Name: proto.String("LUNG_RADS_CATEGORY_4B"), Number: proto.Int32(2)},
				},
			},
			{
				Name: proto.String("HistologyType"),
				Value: []*descriptorpb.EnumValueDescriptorProto{
					{Name: proto.String("HISTOLOGY_TYPE_UNSPECIFIED"), Number: proto.Int32(0)},
					{Name: proto.String("HISTOLOGY_TYPE_UNSPECIFIED_NSCLC"), Number: proto.Int32(1)},
					{Name: proto.String("HISTOLOGY_TYPE_ADENOCARCINOMA"), Number: proto.Int32(2)},
				},
			},
		},
		MessageType: []*descriptorpb.DescriptorProto{
			// Level-2 nested helper message
			{
				Name: proto.String("DocumentCitationCoordinate"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("page_number"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						JsonName: proto.String("pageNumber"),
						Options:  colOpt(DorisColumnMetaIR{ColumnName: "citation_page_number", DorisType: "INT", IcebergType: "INT"}),
					},
					{
						Name:     proto.String("bbox_coords"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_FLOAT.Enum(),
						JsonName: proto.String("bboxCoords"),
						Options:  colOpt(DorisColumnMetaIR{ColumnName: "citation_bbox_coords", DorisType: "ARRAY<FLOAT>", IcebergType: "LIST<FLOAT>"}),
					},
				},
			},
			// Level-1 inline nested helper message (contains non-optional skill_version + level-2 inline_nested coord)
			{
				Name: proto.String("SkillProvenance"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:           proto.String("skill_name"),
						Number:         proto.Int32(1),
						Label:          descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:           descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						Proto3Optional: proto.Bool(true),
						OneofIndex:     proto.Int32(0),
						JsonName:       proto.String("skillName"),
						Options:        colOpt(DorisColumnMetaIR{DorisType: "VARCHAR(128)", IcebergType: "STRING"}),
					},
					{
						Name:           proto.String("skill_confidence"),
						Number:         proto.Int32(2),
						Label:          descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:           descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum(),
						Proto3Optional: proto.Bool(true),
						OneofIndex:     proto.Int32(1),
						JsonName:       proto.String("skillConfidence"),
						Options:        colOpt(DorisColumnMetaIR{DorisType: "DECIMALV3(5,4)", IcebergType: "DECIMAL(5,4)"}),
					},
					{
						Name:     proto.String("skill_version"),
						Number:   proto.Int32(3),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("skillVersion"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "VARCHAR(64)", IcebergType: "STRING"}),
					},
					{
						Name:     proto.String("coord"),
						Number:   proto.Int32(4),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".azra.clinical.v1.DocumentCitationCoordinate"),
						JsonName: proto.String("coord"),
						Options:  colOpt(DorisColumnMetaIR{InlineNested: true}),
					},
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: proto.String("_skill_name")},
					{Name: proto.String("_skill_confidence")},
				},
			},
			// Tenant table with all field types, 2-level inline_nested, and unannotated hydrated fields
			{
				Name: proto.String("EligibilityCriterionResult"),
				Options: tblOpt(
					"eligibility_criterion_results",
					1, // TENANT
					2, // MONTHLY_PARTITIONED
					[]string{"patient_id", "criterion_id"},
					[]string{"event_month"},
					"azra_patient_group",
					"publication_generation",
					[]string{"effective_status"},
				),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("patient_id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("patientId"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "VARCHAR(64)", IcebergType: "STRING", IsUniqueKey: true}),
					},
					{
						Name:     proto.String("criterion_id"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("criterionId"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "VARCHAR(64)", IcebergType: "STRING", IsUniqueKey: true}),
					},
					{
						Name:     proto.String("display_order"),
						Number:   proto.Int32(3),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						JsonName: proto.String("displayOrder"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "SMALLINT", IcebergType: "INT"}),
					},
					{
						Name:           proto.String("observation_age_days"),
						Number:         proto.Int32(4),
						Label:          descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:           descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						Proto3Optional: proto.Bool(true),
						OneofIndex:     proto.Int32(0),
						JsonName:       proto.String("observationAgeDays"),
						Options:        colOpt(DorisColumnMetaIR{DorisType: "INT", IcebergType: "INT"}),
					},
					{
						Name:     proto.String("is_dealbreaker"),
						Number:   proto.Int32(5),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
						JsonName: proto.String("isDealbreaker"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "BOOLEAN", IcebergType: "BOOLEAN"}),
					},
					{
						Name:     proto.String("all_mrns"),
						Number:   proto.Int32(6),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("allMrns"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "ARRAY<VARCHAR(64)>", IcebergType: "LIST<STRING>", InvertedIndex: true}),
					},
					{
						Name:     proto.String("effective_status"),
						Number:   proto.Int32(7),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(),
						TypeName: proto.String(".azra.clinical.v1.CriterionAiStatus"),
						JsonName: proto.String("effectiveStatus"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "VARCHAR(32)", IcebergType: "STRING", StripEnumPrefix: true}),
					},
					{
						Name:           proto.String("override_status"),
						Number:         proto.Int32(8),
						Label:          descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:           descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(),
						TypeName:       proto.String(".azra.clinical.v1.CriterionAiStatus"),
						Proto3Optional: proto.Bool(true),
						OneofIndex:     proto.Int32(1),
						JsonName:       proto.String("overrideStatus"),
						Options:        colOpt(DorisColumnMetaIR{DorisType: "VARCHAR(32)", IcebergType: "STRING", StripEnumPrefix: true}),
					},
					{
						Name:     proto.String("subword_count"),
						Number:   proto.Int32(9),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						JsonName: proto.String("subwordCount"),
						Options:  colOpt(DorisColumnMetaIR{ColumnName: "token_count", DorisType: "INT", IcebergType: "INT"}),
					},
					{
						Name:     proto.String("event_month"),
						Number:   proto.Int32(10),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".google.type.Date"),
						JsonName: proto.String("eventMonth"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "DATEV2", IcebergType: "DATE", IsPartitionKey: true}),
					},
					{
						Name:     proto.String("date_of_birth"),
						Number:   proto.Int32(11),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".google.type.Date"),
						JsonName: proto.String("dateOfBirth"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "DATEV2", IcebergType: "DATE"}),
					},
					{
						Name:     proto.String("verified_at"),
						Number:   proto.Int32(12),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".google.protobuf.Timestamp"),
						JsonName: proto.String("verifiedAt"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "DATETIMEV2(6)", IcebergType: "TIMESTAMPTZ"}),
					},
					{
						Name:     proto.String("skill_provenance"),
						Number:   proto.Int32(13),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".azra.clinical.v1.SkillProvenance"),
						JsonName: proto.String("skillProvenance"),
						Options:  colOpt(DorisColumnMetaIR{InlineNested: true}),
					},
					{
						Name:     proto.String("record_status"),
						Number:   proto.Int32(14),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(),
						TypeName: proto.String(".azra.clinical.v1.ClinicalRecordStatus"),
						JsonName: proto.String("recordStatus"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "VARCHAR(32)", IcebergType: "STRING", StripEnumPrefix: true}),
					},
					{
						Name:     proto.String("publication_generation"),
						Number:   proto.Int32(15),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
						JsonName: proto.String("publicationGeneration"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "BIGINT", IcebergType: "LONG", IsSequenceCol: true}),
					},
					{
						Name:     proto.String("lung_rads"),
						Number:   proto.Int32(16),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(),
						TypeName: proto.String(".azra.clinical.v1.LungRadsCategory"),
						JsonName: proto.String("lungRads"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "VARCHAR(32)", IcebergType: "STRING", StripEnumPrefix: true}),
					},
					{
						Name:     proto.String("histology"),
						Number:   proto.Int32(17),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(),
						TypeName: proto.String(".azra.clinical.v1.HistologyType"),
						JsonName: proto.String("histology"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "VARCHAR(64)", IcebergType: "STRING", StripEnumPrefix: true}),
					},
					{
						Name:     proto.String("fingerprint_hash"),
						Number:   proto.Int32(18),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_UINT64.Enum(),
						JsonName: proto.String("fingerprintHash"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "LARGEINT", IcebergType: "LONG"}),
					},
					{
						Name:           proto.String("optional_hash"),
						Number:         proto.Int32(19),
						Label:          descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:           descriptorpb.FieldDescriptorProto_TYPE_UINT64.Enum(),
						Proto3Optional: proto.Bool(true),
						OneofIndex:     proto.Int32(2),
						JsonName:       proto.String("optionalHash"),
						Options:        colOpt(DorisColumnMetaIR{DorisType: "LARGEINT", IcebergType: "LONG"}),
					},
					{
						Name:     proto.String("raw_payload"),
						Number:   proto.Int32(20),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
						JsonName: proto.String("rawPayload"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "STRING", IcebergType: "STRING"}),
					},
					{
						Name:     proto.String("cohort_years"),
						Number:   proto.Int32(21),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						JsonName: proto.String("cohortYears"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "ARRAY<INT>", IcebergType: "LIST<INT>"}),
					},
					{
						Name:     proto.String("cohort_ids"),
						Number:   proto.Int32(22),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
						JsonName: proto.String("cohortIds"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "ARRAY<BIGINT>", IcebergType: "LIST<LONG>"}),
					},
					{
						Name:     proto.String("thresholds"),
						Number:   proto.Int32(23),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum(),
						JsonName: proto.String("thresholds"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "ARRAY<DOUBLE>", IcebergType: "LIST<DOUBLE>"}),
					},
					{
						Name:     proto.String("status_history"),
						Number:   proto.Int32(24),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(),
						TypeName: proto.String(".azra.clinical.v1.CriterionAiStatus"),
						JsonName: proto.String("statusHistory"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "ARRAY<VARCHAR(32)>", IcebergType: "LIST<STRING>"}),
					},
					{
						Name:     proto.String("u64_hashes"),
						Number:   proto.Int32(25),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_UINT64.Enum(),
						JsonName: proto.String("u64Hashes"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "ARRAY<LARGEINT>", IcebergType: "LIST<LONG>"}),
					},
					// Unannotated read-time hydrated field: MUST be skipped!
					{
						Name:     proto.String("is_evidence_stale"),
						Number:   proto.Int32(30),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
						JsonName: proto.String("isEvidenceStale"),
					},
					{
						Name:     proto.String("hydrated_children"),
						Number:   proto.Int32(31),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".azra.clinical.v1.SkillProvenance"),
						JsonName: proto.String("hydratedChildren"),
					},
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: proto.String("_observation_age_days")},
					{Name: proto.String("_override_status")},
					{Name: proto.String("_optional_hash")},
				},
			},
			// Global terminology table with RELEASE_PARTITIONED and no unique keys -> Iceberg only
			{
				Name: proto.String("SnomedConceptRecord"),
				Options: tblOpt(
					"snomed_concepts",
					2, // GLOBAL_TERMINOLOGY
					3, // RELEASE_PARTITIONED
					nil,
					[]string{"release_id"},
					"",
					"",
					nil,
				),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("id"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "VARCHAR(64)", IcebergType: "VARCHAR(64)"}),
					},
					{
						Name:     proto.String("release_id"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("releaseId"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "VARCHAR(64)", IcebergType: "VARCHAR(64)", IsPartitionKey: true}),
					},
				},
			},
			// Internal-only rollup table -> Doris only, omitted from Iceberg DDL
			{
				Name: proto.String("AnatomicSiteNormalizationRecord"),
				Options: tblOpt(
					"anatomic_site_normalization",
					2, // GLOBAL_TERMINOLOGY
					4, // INTERNAL_ONLY
					[]string{"raw_site_token"},
					nil,
					"",
					"",
					nil,
				),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("raw_site_term"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("rawSiteTerm"),
						Options:  colOpt(DorisColumnMetaIR{ColumnName: "raw_site_token", DorisType: "VARCHAR(128)", IcebergType: "VARCHAR(128)", IsUniqueKey: true}),
					},
					{
						Name:     proto.String("system"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("system"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "VARCHAR(64)", IcebergType: "STRING"}),
					},
					{
						Name:     proto.String("embedding"),
						Number:   proto.Int32(3),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_FLOAT.Enum(),
						JsonName: proto.String("embedding"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "ARRAY<FLOAT>", IcebergType: "LIST<FLOAT>"}),
					},
					{
						Name:     proto.String("additive_offset"),
						Number:   proto.Int32(4),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum(),
						JsonName: proto.String("additiveOffset"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "DOUBLE", IcebergType: "DOUBLE"}),
					},
				},
			},
		},
	}

	secondaryFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("secondary_table.proto"),
		Package: proto.String("azra.clinical.v1"),
		Dependency: []string{
			"google/type/date.proto",
		},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/lakehouse/clinicalv1;clinicalv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("AuditMetadata"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("source_ip"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("sourceIp"),
					},
				},
			},
			{
				Name: proto.String("AuditEventRecord"),
				Options: tblOpt(
					"audit_events",
					4, // GLOBAL_TELEMETRY
					1, // WHOLE_TABLE
					[]string{"tenant_id", "event_id"},
					nil,
					"",
					"",
					nil,
				),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("tenant_id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("tenantId"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "VARCHAR(64)", IsUniqueKey: true}),
					},
					{
						Name:     proto.String("event_id"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("eventId"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "VARCHAR(64)", IsUniqueKey: true}),
					},
					{
						Name:     proto.String("event_date"),
						Number:   proto.Int32(3),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".google.type.Date"),
						JsonName: proto.String("eventDate"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "DATEV2"}),
					},
				},
			},
			{
				Name: proto.String("DefaultInferredAuditRecord"),
				Options: tblOpt(
					"default_inferred_audits",
					1, // TENANT
					1, // WHOLE_TABLE
					[]string{"record_id"},
					nil,
					"",
					"seq_hash",
					nil,
				),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:           proto.String("record_id"),
						Number:         proto.Int32(1),
						Label:          descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:           descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						Proto3Optional: proto.Bool(true),
						OneofIndex:     proto.Int32(1),
						JsonName:       proto.String("recordId"),
						Options:        colOpt(DorisColumnMetaIR{IsUniqueKey: true}),
					},
					{
						Name:           proto.String("seq_hash"),
						Number:         proto.Int32(2),
						Label:          descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:           descriptorpb.FieldDescriptorProto_TYPE_UINT64.Enum(),
						Proto3Optional: proto.Bool(true),
						OneofIndex:     proto.Int32(2),
						JsonName:       proto.String("seqHash"),
						Options:        colOpt(DorisColumnMetaIR{IsSequenceCol: true}),
					},
					{
						Name:     proto.String("actor_id"),
						Number:   proto.Int32(3),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("actorId"),
					},
					{
						Name:     proto.String("meta"),
						Number:   proto.Int32(4),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".azra.clinical.v1.AuditMetadata"),
						JsonName: proto.String("meta"),
						Options:  colOpt(DorisColumnMetaIR{InlineNested: true}),
					},
					{
						Name:       proto.String("numeric_value"),
						Number:     proto.Int32(5),
						Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:       descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum(),
						OneofIndex: proto.Int32(0),
						JsonName:   proto.String("numericValue"),
					},
					{
						Name:       proto.String("text_value"),
						Number:     proto.Int32(6),
						Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:       descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						OneofIndex: proto.Int32(0),
						JsonName:   proto.String("textValue"),
					},
					{
						Name:       proto.String("raw_bytes"),
						Number:     proto.Int32(8),
						Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:       descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
						OneofIndex: proto.Int32(0),
						JsonName:   proto.String("rawBytes"),
					},
					{
						Name:     proto.String("retry_count"),
						Number:   proto.Int32(7),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_UINT32.Enum(),
						JsonName: proto.String("retryCount"),
					},
					{
						Name:     proto.String("table_name"),
						Number:   proto.Int32(9),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("tableName"),
					},
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: proto.String("result_value")},
					{Name: proto.String("_record_id")},
					{Name: proto.String("_seq_hash")},
				},
			},
		},
	}

	nonTableFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("non_table.proto"),
		Package: proto.String("azra.clinical.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/lakehouse/clinicalv1;clinicalv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("PlainHelperMessage"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("note"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("note"),
					},
				},
			},
		},
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{tsFDP, dateFDP, clinicalFDP, secondaryFDP, nonTableFDP},
	}
	return newPlugin(t, fds, []string{"clinical_lakehouse.proto", "secondary_table.proto", "non_table.proto"})
}

// TestGenerateGoDoris_CodegenAndDDL verifies end-to-end Go struct generation and
// Doris/Iceberg DDL generation across tenant, global terminology, and internal-only tables.
func TestGenerateGoDoris_CodegenAndDDL(t *testing.T) {
	gen := buildComprehensiveDorisPlugin(t)
	runner := NewRunner()
	// Verify default CLI behavior: Domain is true by default, Backend is "doris", DorisMode is "both".
	// Should NOT generate conflicting .type.go domain files when GoPackage is empty.
	opts := &Options{
		Lang:      "go",
		Backend:   "doris",
		DorisMode: "both",
		Domain:    true,
	}

	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		if err := runner.GenerateFile(gen, f, opts); err != nil {
			t.Fatalf("GenerateFile(%s) failed: %v", f.Desc.Path(), err)
		}
	}

	resp := gen.Response()
	filesByName := make(map[string]string)
	for _, gf := range resp.File {
		filesByName[filepath.Base(gf.GetName())] = gf.GetContent()
	}

	// non_table.proto has no table_meta messages and MUST NOT generate any files.
	for name := range filesByName {
		if strings.Contains(name, "non_table") {
			t.Errorf("expected non_table.proto to be skipped cleanly, but generated %s", name)
		}
		if name == "clinical_lakehouse.type.go" {
			t.Errorf("expected Domain:true with Backend:doris and empty GoPackage not to emit conflicting clinical_lakehouse.type.go")
		}
	}

	goCode, ok := filesByName["clinical_lakehouse_doris.type.go"]
	if !ok {
		t.Fatalf("missing clinical_lakehouse_doris.type.go in output (got %v)", keysOf(filesByName))
	}
	secGoCode, ok := filesByName["secondary_table_doris.type.go"]
	if !ok {
		t.Fatalf("missing secondary_table_doris.type.go in output")
	}
	if strings.Contains(secGoCode, "type DorisScanner interface") || strings.Contains(secGoCode, "func parseDorisDateToProto") {
		t.Errorf("secondary_table_doris.type.go in the same Go package should not re-emit shared Doris runtime types or date helpers")
	}

	dorisSQL, ok := filesByName["clinical_lakehouse_doris.sql"]
	if !ok {
		t.Fatalf("missing clinical_lakehouse_doris.sql in output")
	}
	icebergSQL, ok := filesByName["clinical_lakehouse_iceberg.sql"]
	if !ok {
		t.Fatalf("missing clinical_lakehouse_iceberg.sql in output")
	}
	secDorisSQL, ok := filesByName["secondary_table_doris.sql"]
	if !ok {
		t.Fatalf("missing secondary_table_doris.sql in output")
	}
	// AuditEventRecord has UNIQUE KEY(tenant_id, event_id) -> DISTRIBUTED BY HASH(event_id) (skipping tenant_id skew!)
	if !strings.Contains(secDorisSQL, "DISTRIBUTED BY HASH(event_id) BUCKETS 8") {
		t.Errorf("expected secondary_table_doris.sql to distribute by uks[1] (event_id) when uks[0] == tenant_id, got:\n%s", secDorisSQL)
	}

	// Verify Go codegen contents:
	if strings.Contains(goCode, "type SkillProvenanceDoris struct") || strings.Contains(goCode, "type DocumentCitationCoordinateDoris struct") {
		t.Error("inline_nested helper messages should not generate standalone <Msg>Doris structs")
	}
	// Unannotated hydrated fields (is_evidence_stale, hydrated_children) must be skipped:
	if strings.Contains(goCode, "IsEvidenceStale") || strings.Contains(goCode, "HydratedChildren") {
		t.Error("unannotated read-time hydrated fields IsEvidenceStale and HydratedChildren must be skipped")
	}

	normGoCode := strings.Join(strings.Fields(goCode), " ")
	wantGoSnippets := []string{
		"type DorisScanner interface",
		"type DorisStringArray []string",
		"type DorisFloat32Array []float32",
		"type DorisFloat64Array []float64",
		"type DorisInt32Array []int32",
		"type DorisInt64Array []int64",
		"type DorisBoolArray []bool",
		"func EligibilityCriterionResultDorisTable() DorisTableDescriptor",
		"type EligibilityCriterionResultDoris struct",
		"SubwordCount int32 `db:\"token_count\" doris:\"INT\"`",
		"SkillName *string `db:\"skill_name\" doris:\"VARCHAR(128)\"`",
		"SkillConfidence *float64 `db:\"skill_confidence\" doris:\"DECIMALV3(5,4)\"`",
		"SkillVersion string `db:\"skill_version\" doris:\"VARCHAR(64)\"`",
		"PageNumber int32 `db:\"citation_page_number\" doris:\"INT\"`",
		"BboxCoords DorisFloat32Array `db:\"citation_bbox_coords\" doris:\"ARRAY<FLOAT>\"`",
		"AllMrns DorisStringArray `db:\"all_mrns\" doris:\"ARRAY<VARCHAR(64)>\"`",
		"EventMonth string `db:\"event_month\" doris:\"DATEV2\"`",
		"DateOfBirth string `db:\"date_of_birth\" doris:\"DATEV2\"`",
		"VerifiedAt time.Time `db:\"verified_at\" doris:\"DATETIMEV2(6)\"`",
		"FingerprintHash uint64 `db:\"fingerprint_hash\" doris:\"LARGEINT\"`",
		"OptionalHash *uint64 `db:\"optional_hash\" doris:\"LARGEINT\"`",
		"RawPayload []byte `db:\"raw_payload\" doris:\"STRING\"`",
		"CohortYears DorisInt32Array `db:\"cohort_years\" doris:\"ARRAY<INT>\"`",
		"CohortIds DorisInt64Array `db:\"cohort_ids\" doris:\"ARRAY<BIGINT>\"`",
		"Thresholds DorisFloat64Array `db:\"thresholds\" doris:\"ARRAY<DOUBLE>\"`",
		"StatusHistory DorisStringArray `db:\"status_history\" doris:\"ARRAY<VARCHAR(32)>\"`",
		"U64Hashes DorisUint64Array `db:\"u64_hashes\" doris:\"ARRAY<LARGEINT>\"`",
		"var ClinicalLakehouseDorisTables = []DorisTableDescriptor{",
	}
	for _, snippet := range wantGoSnippets {
		if !strings.Contains(normGoCode, snippet) {
			t.Errorf("clinical_lakehouse_doris.type.go missing expected snippet:\n  %s", snippet)
		}
	}

	// Verify Doris DDL contents:
	wantDorisSQL := []string{
		"CREATE TABLE IF NOT EXISTS internal.tenant_${TENANT_SLUG}.eligibility_criterion_results (",
		"display_order SMALLINT NOT NULL DEFAULT \"0\"",
		"is_dealbreaker BOOLEAN NOT NULL DEFAULT \"false\"",
		"token_count INT NOT NULL DEFAULT \"0\"",
		"skill_name VARCHAR(128)",
		"skill_confidence DECIMALV3(5,4)",
		"skill_version VARCHAR(64)",
		"citation_page_number INT",
		"citation_bbox_coords ARRAY<FLOAT> NOT NULL DEFAULT \"[]\"",
		"record_status VARCHAR(32) NOT NULL DEFAULT \"ACTIVE\"",
		"publication_generation BIGINT NOT NULL DEFAULT \"0\"",
		"INDEX idx_eligibility_criterion_results_all_mrns (all_mrns) USING INVERTED",
		"INDEX idx_eligibility_criterion_results_effective_status (effective_status) USING INVERTED",
		"UNIQUE KEY(patient_id, criterion_id)",
		"DISTRIBUTED BY HASH(patient_id) BUCKETS ${TENANT_BUCKETS}",
		`"function_column.sequence_col" = "publication_generation"`,
		`"colocate_with" = "azra_patient_group"`,
		"CREATE TABLE IF NOT EXISTS internal.terminology.anatomic_site_normalization (",
		"`system` VARCHAR(64) NOT NULL",
		"embedding ARRAY<FLOAT> NOT NULL,",
		"additive_offset DOUBLE NOT NULL DEFAULT \"0.0\"",
	}
	for _, snippet := range wantDorisSQL {
		if !strings.Contains(dorisSQL, snippet) {
			t.Errorf("clinical_lakehouse_doris.sql missing expected snippet:\n  %s\nFull SQL:\n%s", snippet, dorisSQL)
		}
	}
	// SnomedConceptRecord is GLOBAL_TERMINOLOGY + RELEASE_PARTITIONED with no unique/duplicate keys -> Iceberg only!
	if strings.Contains(dorisSQL, "snomed_concepts") {
		t.Errorf("Iceberg-only terminology table snomed_concepts must not be emitted in Doris SQL:\n%s", dorisSQL)
	}

	// Verify Iceberg DDL contents:
	wantIcebergSQL := []string{
		"CREATE TABLE IF NOT EXISTS azra_lake.tenant_${TENANT_SLUG}.eligibility_criterion_results (",
		"event_month DATE NOT NULL",
		"verified_at DATETIME",
		"skill_confidence DECIMAL(5, 4)",
		"fingerprint_hash DECIMAL(38, 0) NOT NULL",
		"u64_hashes ARRAY<DECIMAL(38, 0)> NOT NULL",
		"PARTITION BY LIST (event_month) ()",
		"CREATE TABLE IF NOT EXISTS azra_lake.terminology.snomed_concepts (",
		"PARTITION BY LIST (release_id) ()",
	}
	for _, snippet := range wantIcebergSQL {
		if !strings.Contains(icebergSQL, snippet) {
			t.Errorf("clinical_lakehouse_iceberg.sql missing expected snippet:\n  %s\nFull SQL:\n%s", snippet, icebergSQL)
		}
	}
	// AnatomicSiteNormalizationRecord is INTERNAL_ONLY and must NOT appear in Iceberg SQL.
	if strings.Contains(icebergSQL, "anatomic_site_normalization") {
		t.Errorf("INTERNAL_ONLY table anatomic_site_normalization must not be emitted in Iceberg SQL:\n%s", icebergSQL)
	}
}

// TestBuildDorisCreateTableSQL_TenantSlugValidationAndInjection verifies tenant slug
// normalization, SQL injection rejection, Iceberg DDL type mapping, and key column rules.
func TestBuildDorisCreateTableSQL_TenantSlugValidationAndInjection(t *testing.T) {
	desc := DorisTableDescriptor{
		TableName:        "patients",
		MessageName:      "Patient",
		DatabaseScope:    "TENANT",
		SyncTier:         "WHOLE_TABLE",
		UniqueKeyColumns: []string{"patient_id"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "patient_id", DorisType: "VARCHAR(64)", IcebergType: "STRING", IsUniqueKey: true},
		},
	}

	// 1. Valid hyphenated slug normalizes to underscore
	sqlValid := desc.BuildDorisCreateTableSQL("akron-general", 4)
	if !strings.Contains(sqlValid, "internal.tenant_akron_general.patients") {
		t.Errorf("expected akron-general to normalize to akron_general, got:\n%s", sqlValid)
	}
	iceValid := desc.BuildIcebergCreateTableSQL("akron-general")
	if !strings.Contains(iceValid, "azra_lake.tenant_akron_general.patients") {
		t.Errorf("expected akron-general to normalize to akron_general in Iceberg DDL, got:\n%s", iceValid)
	}

	// 2. Malicious SQL injection payloads in tenantSlug must be rejected (return "")
	maliciousSlugs := []string{
		"foo; DROP TABLE patients; --",
		"foo) PROPERTIES (\"x\"=\"y\"); --",
		"foo`bar",
		"foo'bar",
		"foo\"bar",
		"../etc/passwd",
		"tenant space",
		"123_starts_with_digit",
	}
	for _, bad := range maliciousSlugs {
		if got := desc.BuildDorisCreateTableSQL(bad, 4); got != "" {
			t.Errorf("expected BuildDorisCreateTableSQL(%q) to return empty string, got:\n%s", bad, got)
		}
		if got := desc.BuildIcebergCreateTableSQL(bad); got != "" {
			t.Errorf("expected BuildIcebergCreateTableSQL(%q) to return empty string, got:\n%s", bad, got)
		}
	}

	// 3. CanonicalIcebergSpecType and dorisColumnIcebergDDLType checks
	col := DorisColumnDescriptor{DorisType: "DECIMALV3(8,2)"}
	if got := col.CanonicalIcebergSpecType(); got != "DECIMAL(8,2)" {
		t.Errorf("CanonicalIcebergSpecType() = %q, want DECIMAL(8,2)", got)
	}
	if got := dorisColumnIcebergDDLType("", "LIST<STRING>"); got != "ARRAY<VARCHAR(64)>" {
		t.Errorf("dorisColumnIcebergDDLType(\"\", \"LIST<STRING>\") = %q, want ARRAY<VARCHAR(64)>", got)
	}
	if got := dorisColumnIcebergDDLType("", "TIMESTAMPTZ"); got != "DATETIME" {
		t.Errorf("dorisColumnIcebergDDLType(\"\", \"TIMESTAMPTZ\") = %q, want DATETIME", got)
	}
	if got := dorisColumnIcebergDDLType("LARGEINT", ""); got != "DECIMAL(38, 0)" {
		t.Errorf("dorisColumnIcebergDDLType(\"LARGEINT\", \"\") = %q, want DECIMAL(38, 0)", got)
	}
	if got := dorisColumnIcebergDDLType("largeint", ""); got != "DECIMAL(38, 0)" {
		t.Errorf("dorisColumnIcebergDDLType(\"largeint\", \"\") = %q, want DECIMAL(38, 0)", got)
	}
	if got := dorisColumnIcebergDDLType("ARRAY<LARGEINT>", ""); got != "ARRAY<DECIMAL(38, 0)>" {
		t.Errorf("dorisColumnIcebergDDLType(\"ARRAY<LARGEINT>\", \"\") = %q, want ARRAY<DECIMAL(38, 0)>", got)
	}
	if got := dorisColumnIcebergDDLType("array<largeint>", ""); got != "ARRAY<DECIMAL(38, 0)>" {
		t.Errorf("dorisColumnIcebergDDLType(\"array<largeint>\", \"\") = %q, want ARRAY<DECIMAL(38, 0)>", got)
	}
	if got := dorisColumnIcebergDDLType("datetimev2(0)", ""); got != "DATETIME" {
		t.Errorf("dorisColumnIcebergDDLType(\"datetimev2(0)\", \"\") = %q, want DATETIME", got)
	}
	if got := dorisColumnIcebergDDLType("ARRAY<DATEV2>", ""); got != "ARRAY<DATE>" {
		t.Errorf("dorisColumnIcebergDDLType(\"ARRAY<DATEV2>\", \"\") = %q, want ARRAY<DATE>", got)
	}
	if got := dorisColumnIcebergDDLType("ARRAY<DECIMALV3(10,2)>", ""); got != "ARRAY<DECIMAL(10, 2)>" {
		t.Errorf("dorisColumnIcebergDDLType(\"ARRAY<DECIMALV3(10,2)>\", \"\") = %q, want ARRAY<DECIMAL(10, 2)>", got)
	}
	if got := (DorisColumnDescriptor{DorisType: "DATETIMEV2(0)"}).CanonicalIcebergSpecType(); got != "TIMESTAMPTZ" {
		t.Errorf("CanonicalIcebergSpecType(DATETIMEV2(0)) = %q, want TIMESTAMPTZ", got)
	}
	if got := (DorisColumnDescriptor{DorisType: "ARRAY<DATEV2>"}).CanonicalIcebergSpecType(); got != "LIST<DATE>" {
		t.Errorf("CanonicalIcebergSpecType(ARRAY<DATEV2>) = %q, want LIST<DATE>", got)
	}
	for _, jsonType := range []string{"JSON", "JSONB", "VARIANT", "json", "jsonb", "variant"} {
		if got := dorisColumnIcebergDDLType(jsonType, ""); got != "STRING" {
			t.Errorf("dorisColumnIcebergDDLType(%q, \"\") = %q, want STRING", jsonType, got)
		}
		if got := dorisColumnIcebergDDLType("ARRAY<"+jsonType+">", ""); got != "ARRAY<STRING>" {
			t.Errorf("dorisColumnIcebergDDLType(\"ARRAY<%s>\", \"\") = %q, want ARRAY<STRING>", jsonType, got)
		}
		if got := (DorisColumnDescriptor{DorisType: jsonType}).CanonicalIcebergSpecType(); got != "STRING" {
			t.Errorf("CanonicalIcebergSpecType(%q) = %q, want STRING", jsonType, got)
		}
	}

	// 3b. Verify Faiss HNSW USING ANN index emission on clinical_concept_search (guaranteed 768-dim)
	// and verify that arbitrary DUPLICATE KEY tables with embedding ARRAY<FLOAT> do NOT get a hardcoded dim="768" HNSW index.
	conceptSearchDesc := DorisTableDescriptor{
		TableName:            "clinical_concept_search",
		MessageName:          "ClinicalConceptSearchRecord",
		DatabaseScope:        "GLOBAL_TERMINOLOGY_RAG",
		SyncTier:             "INTERNAL_ONLY",
		DuplicateKeyColumns:  []string{"concept_id", "code_system"},
		InvertedIndexColumns: []string{"preferred_term", "synonyms_text"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "concept_id", DorisType: "VARCHAR(64)", Nullable: false},
			{ColumnName: "code_system", DorisType: "VARCHAR(32)", Nullable: false},
			{ColumnName: "preferred_term", DorisType: "TEXT", Nullable: false, InvertedIndex: true},
			{ColumnName: "synonyms_text", DorisType: "TEXT", Nullable: true, InvertedIndex: true},
			{ColumnName: "embedding", DorisType: "ARRAY<FLOAT>", Nullable: false},
		},
	}
	conceptSearchDDL := conceptSearchDesc.BuildDorisCreateTableSQL("", 0)
	wantANN := `INDEX idx_embedding_hnsw (embedding) USING ANN PROPERTIES ("index_type" = "hnsw", "metric_type" = "l2_distance", "dim" = "768")`
	if !strings.Contains(conceptSearchDDL, wantANN) {
		t.Errorf("expected clinical_concept_search BuildDorisCreateTableSQL to emit Faiss HNSW USING ANN index:\n  want: %s\n  got:\n%s", wantANN, conceptSearchDDL)
	}
	arbitraryDupEmbeddingDesc := conceptSearchDesc
	arbitraryDupEmbeddingDesc.TableName = "custom_vector_events"
	arbitraryDupEmbeddingDDL := arbitraryDupEmbeddingDesc.BuildDorisCreateTableSQL("", 0)
	if strings.Contains(arbitraryDupEmbeddingDDL, "USING ANN") || strings.Contains(arbitraryDupEmbeddingDDL, `"dim" = "768"`) {
		t.Errorf("expected non-clinical_concept_search DUPLICATE KEY table with embedding ARRAY<FLOAT> not to emit hardcoded dim=768 HNSW index:\n%s", arbitraryDupEmbeddingDDL)
	}

	// 4. Runtime key-prefix reordering when UniqueKeyColumns is not first in t.Columns
	unsortedDesc := DorisTableDescriptor{
		TableName:        "events",
		DatabaseScope:    "TENANT",
		SyncTier:         "WHOLE_TABLE",
		UniqueKeyColumns: []string{"event_id"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "payload", DorisType: "TEXT", Nullable: true},
			{ColumnName: "event_id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
		},
	}
	reorderedSQL := unsortedDesc.BuildDorisCreateTableSQL("akron", 4)
	eventIdx := strings.Index(reorderedSQL, "event_id VARCHAR(64) NOT NULL")
	payloadIdx := strings.Index(reorderedSQL, "payload TEXT")
	if eventIdx < 0 || payloadIdx < 0 || eventIdx > payloadIdx {
		t.Errorf("expected BuildDorisCreateTableSQL to reorder key column event_id before payload:\n%s", reorderedSQL)
	}
	reorderedIcebergSQL := unsortedDesc.BuildIcebergCreateTableSQL("akron")
	iceEventIdx := strings.Index(reorderedIcebergSQL, "event_id VARCHAR(64) NOT NULL")
	icePayloadIdx := strings.Index(reorderedIcebergSQL, "payload TEXT")
	if iceEventIdx < 0 || icePayloadIdx < 0 || iceEventIdx > icePayloadIdx {
		t.Errorf("expected BuildIcebergCreateTableSQL to reorder key column event_id before payload:\n%s", reorderedIcebergSQL)
	}

	// 4b. Deduplication of UniqueKeyColumns, PartitionByColumns, NgramBfColumns, and BloomFilterColumns
	dupListsDesc := DorisTableDescriptor{
		TableName:          "events",
		DatabaseScope:      "TENANT",
		SyncTier:           "MONTHLY_PARTITIONED",
		UniqueKeyColumns:   []string{"event_id", "event_id"},
		PartitionByColumns: []string{"event_month", "event_month"},
		NgramBfColumns:     []string{"payload", "payload"},
		BloomFilterColumns: []string{"event_id", "event_id"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "event_id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
			{ColumnName: "event_month", DorisType: "DATEV2", IsPartitionKey: true},
			{ColumnName: "payload", DorisType: "TEXT", Nullable: true},
		},
	}
	dedupDorisSQL := dupListsDesc.BuildDorisCreateTableSQL("akron", 4)
	if !strings.Contains(dedupDorisSQL, "UNIQUE KEY(event_id)") {
		t.Errorf("expected deduplicated UNIQUE KEY(event_id), got:\n%s", dedupDorisSQL)
	}
	if strings.Count(dedupDorisSQL, "idx_ngram_events_payload") != 1 {
		t.Errorf("expected single idx_ngram_events_payload index, got:\n%s", dedupDorisSQL)
	}
	if !strings.Contains(dedupDorisSQL, `"bloom_filter_columns" = "event_id"`) {
		t.Errorf("expected deduplicated bloom_filter_columns, got:\n%s", dedupDorisSQL)
	}
	dedupIcebergSQL := dupListsDesc.BuildIcebergCreateTableSQL("akron")
	if !strings.Contains(dedupIcebergSQL, "PARTITION BY LIST (event_month) ()") {
		t.Errorf("expected deduplicated PARTITION BY LIST (event_month), got:\n%s", dedupIcebergSQL)
	}
	if strings.Count(dedupIcebergSQL, `"write.parquet.bloom-filter-enabled.column.event_id" = "true"`) != 1 {
		t.Errorf("expected single bloom-filter-enabled property for event_id, got:\n%s", dedupIcebergSQL)
	}

	// 4c. ICEBERG_ONLY table preserves Nullable: true on key columns after reordering
	icebergOnlyNullableKeyDesc := DorisTableDescriptor{
		TableName:        "staging_events",
		DatabaseScope:    "TENANT",
		SyncTier:         "ICEBERG_ONLY",
		UniqueKeyColumns: []string{"event_id"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "payload", DorisType: "TEXT", Nullable: true},
			{ColumnName: "event_id", DorisType: "VARCHAR(64)", Nullable: true, IsUniqueKey: true},
		},
	}
	icebergOnlySQL := icebergOnlyNullableKeyDesc.BuildIcebergCreateTableSQL("akron")
	if strings.Contains(icebergOnlySQL, "event_id VARCHAR(64) NOT NULL") || !strings.Contains(icebergOnlySQL, "event_id VARCHAR(64),") {
		t.Errorf("expected ICEBERG_ONLY BuildIcebergCreateTableSQL to keep nullable key column nullable after reordering, got:\n%s", icebergOnlySQL)
	}

	// 4d. Tenant table where t.Columns[0] is patient_id preserves patient_id bloom-filter fallback even when another key column moves ahead of it
	reorderedPatientDesc := DorisTableDescriptor{
		TableName:        "patient_events",
		DatabaseScope:    "TENANT",
		SyncTier:         "WHOLE_TABLE",
		UniqueKeyColumns: []string{"tenant_id", "patient_id"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "patient_id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
			{ColumnName: "tenant_id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
		},
	}
	reorderedPatientIceSQL := reorderedPatientDesc.BuildIcebergCreateTableSQL("akron")
	if !strings.Contains(reorderedPatientIceSQL, `"write.parquet.bloom-filter-enabled.column.patient_id" = "true"`) {
		t.Errorf("expected BuildIcebergCreateTableSQL to preserve patient_id bloom-filter fallback after key reordering, got:\n%s", reorderedPatientIceSQL)
	}

	// 5. Disallowed Doris key types must be rejected in BuildDorisCreateTableSQL (fallback, explicit UK/DK, and DistributedByColumn) and isDorisKeyEligibleType
	for _, badKeyType := range []string{
		"FLOAT", "DOUBLE", "STRING", "TEXT", "JSON", "JSONB", "VARIANT",
		"HLL", "BITMAP", "QUANTILE_STATE", "ARRAY<INT>", "MAP<VARCHAR(64), INT>", "STRUCT<INT>",
	} {
		if isDorisKeyEligibleType(badKeyType) {
			t.Errorf("expected isDorisKeyEligibleType(%q) = false, got true", badKeyType)
		}
		// 5a. Fallback key (Columns[0])
		badFallbackDesc := DorisTableDescriptor{
			TableName:     "bad_keys",
			DatabaseScope: "TENANT",
			SyncTier:      "WHOLE_TABLE",
			Columns: []DorisColumnDescriptor{
				{ColumnName: "k", DorisType: badKeyType},
			},
		}
		if got := badFallbackDesc.BuildDorisCreateTableSQL("akron", 4); got != "" {
			t.Errorf("expected BuildDorisCreateTableSQL with fallback key type %q to return empty string, got:\n%s", badKeyType, got)
		}
		// 5b. Explicit UniqueKeyColumns
		badUKDesc := DorisTableDescriptor{
			TableName:        "bad_keys",
			DatabaseScope:    "TENANT",
			SyncTier:         "WHOLE_TABLE",
			UniqueKeyColumns: []string{"id", "k"},
			Columns: []DorisColumnDescriptor{
				{ColumnName: "id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
				{ColumnName: "k", DorisType: badKeyType, IsUniqueKey: true},
			},
		}
		if got := badUKDesc.BuildDorisCreateTableSQL("akron", 4); got != "" {
			t.Errorf("expected BuildDorisCreateTableSQL with UNIQUE KEY type %q to return empty string, got:\n%s", badKeyType, got)
		}
		// 5c. Explicit DuplicateKeyColumns
		badDKDesc := DorisTableDescriptor{
			TableName:           "bad_keys",
			DatabaseScope:       "TENANT",
			SyncTier:            "WHOLE_TABLE",
			DuplicateKeyColumns: []string{"id", "k"},
			Columns: []DorisColumnDescriptor{
				{ColumnName: "id", DorisType: "VARCHAR(64)"},
				{ColumnName: "k", DorisType: badKeyType},
			},
		}
		if got := badDKDesc.BuildDorisCreateTableSQL("akron", 4); got != "" {
			t.Errorf("expected BuildDorisCreateTableSQL with DUPLICATE KEY type %q to return empty string, got:\n%s", badKeyType, got)
		}
		// 5d. Explicit DistributedByColumn
		badDistDesc := DorisTableDescriptor{
			TableName:           "bad_keys",
			DatabaseScope:       "TENANT",
			SyncTier:            "WHOLE_TABLE",
			UniqueKeyColumns:    []string{"id"},
			DistributedByColumn: "k",
			Columns: []DorisColumnDescriptor{
				{ColumnName: "id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
				{ColumnName: "k", DorisType: badKeyType},
			},
		}
		if got := badDistDesc.BuildDorisCreateTableSQL("akron", 4); got != "" {
			t.Errorf("expected BuildDorisCreateTableSQL with DistributedByColumn type %q to return empty string, got:\n%s", badKeyType, got)
		}
	}
	if isDorisKeyEligibleType("STRUCT<a:INT>") {
		t.Error("expected isDorisKeyEligibleType(\"STRUCT<a:INT>\") = false, got true")
	}

	// 5e. Empty string elements in column slices must be rejected by BuildDorisCreateTableSQL and BuildIcebergCreateTableSQL
	emptySliceElemDesc := DorisTableDescriptor{
		TableName:          "patients",
		DatabaseScope:      "TENANT",
		SyncTier:           "WHOLE_TABLE",
		UniqueKeyColumns:   []string{"patient_id"},
		PartitionByColumns: []string{"patient_id", ""},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "patient_id", DorisType: "VARCHAR(64)", IsUniqueKey: true},
		},
	}
	if got := emptySliceElemDesc.BuildDorisCreateTableSQL("akron", 4); got != "" {
		t.Errorf("expected BuildDorisCreateTableSQL to reject empty element in PartitionByColumns, got:\n%s", got)
	}
	if got := emptySliceElemDesc.BuildIcebergCreateTableSQL("akron"); got != "" {
		t.Errorf("expected BuildIcebergCreateTableSQL to reject empty element in PartitionByColumns, got:\n%s", got)
	}

	// 6. Malicious DefaultValue in runtime descriptor must be rejected
	for _, badDefault := range []string{`0" COMMENT "injected`, "foo'bar", "foo;bar", "foo\\bar", "foo\x00bar", "foo\nbar"} {
		badDefDesc := DorisTableDescriptor{
			TableName:        "patients",
			DatabaseScope:    "TENANT",
			SyncTier:         "WHOLE_TABLE",
			UniqueKeyColumns: []string{"patient_id"},
			Columns: []DorisColumnDescriptor{
				{ColumnName: "patient_id", DorisType: "VARCHAR(64)", IsUniqueKey: true, DefaultValue: badDefault},
			},
		}
		if got := badDefDesc.BuildDorisCreateTableSQL("akron", 4); got != "" {
			t.Errorf("expected BuildDorisCreateTableSQL with DefaultValue %q to return empty string, got:\n%s", badDefault, got)
		}
	}

	// 7. Verify isValidSQLTypeSyntax accepts parameterized generic types and rejects injected constraints
	for _, validType := range []string{"VARCHAR(64)", "DECIMALV3(18, 4)", "ARRAY<DECIMAL(10, 2)>", "MAP<VARCHAR(64), INT>"} {
		if !isValidSQLTypeSyntax(validType) {
			t.Errorf("expected isValidSQLTypeSyntax(%q) = true, got false", validType)
		}
	}
	for _, invalidType := range []string{"INT NOT NULL", "ARRAY<INT NOT NULL>", "DECIMAL(10, )", "MAP<STRING, >", "VARCHAR(64) DEFAULT 0"} {
		if isValidSQLTypeSyntax(invalidType) {
			t.Errorf("expected isValidSQLTypeSyntax(%q) = false, got true", invalidType)
		}
	}
}

// TestGenerateGoDoris_OptionIdentifierValidation verifies that invalid SQL identifiers,
// disallowed key types, and unsafe SQL type or default value strings are rejected at codegen time.
func TestGenerateGoDoris_OptionIdentifierValidation(t *testing.T) {
	makePluginWithTable := func(tOpts *proto2typepb.TableOptions, cOpts *proto2typepb.ColumnOptions, fieldType descriptorpb.FieldDescriptorProto_Type) (*protogen.Plugin, *protogen.File) {
		mo := &descriptorpb.MessageOptions{}
		proto.SetExtension(mo, proto2typepb.E_Message, &proto2typepb.MessageOptions{Table: tOpts})
		var fo *descriptorpb.FieldOptions
		if cOpts != nil {
			fo = &descriptorpb.FieldOptions{}
			proto.SetExtension(fo, proto2typepb.E_Field, &proto2typepb.FieldOptions{Column: cOpts})
		}
		fdp := &descriptorpb.FileDescriptorProto{
			Syntax:  proto.String("proto3"),
			Name:    proto.String("bad.proto"),
			Package: proto.String("test.v1"),
			Options: &descriptorpb.FileOptions{GoPackage: proto.String("example.com/test/v1;testv1")},
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name:    proto.String("BadRecord"),
					Options: mo,
					Field: []*descriptorpb.FieldDescriptorProto{
						{
							Name:     proto.String("id"),
							Number:   proto.Int32(1),
							Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
							Type:     fieldType.Enum(),
							JsonName: proto.String("id"),
							Options:  fo,
						},
					},
				},
			},
		}
		gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"bad.proto"})
		return gen, gen.Files[0]
	}

	runner := NewRunner()
	opts := &Options{Lang: "go", Backend: "doris", DorisMode: "both"}

	cases := []struct {
		name      string
		tOpts     *proto2typepb.TableOptions
		cOpts     *proto2typepb.ColumnOptions
		fieldType descriptorpb.FieldDescriptorProto_Type
	}{
		{
			name:      "SQL injection in table_name",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients; DROP TABLE x; --"},
			cOpts:     &proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_STRING,
		},
		{
			name:      "Invalid database_scope",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients", DatabaseScope: "BOGUS_SCOPE"},
			cOpts:     &proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_STRING,
		},
		{
			name:      "Invalid sync_tier",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients", SyncTier: "BOGUS_TIER"},
			cOpts:     &proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_STRING,
		},
		{
			name:      "Invalid unique_key_columns identifier",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients", UniqueKeyColumns: []string{"id) BUCKETS 1; --"}},
			cOpts:     &proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_STRING,
		},
		{
			name:      "Invalid bloom_filter_columns identifier",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients", BloomFilterColumns: []string{`id" = "true"`}},
			cOpts:     &proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_STRING,
		},
		{
			name:      "Invalid doris_type syntax with semicolon",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients"},
			cOpts:     &proto2typepb.ColumnOptions{DorisType: "VARCHAR(64); DROP TABLE x"},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_STRING,
		},
		{
			name:      "Invalid doris_type syntax with top-level comma",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients"},
			cOpts:     &proto2typepb.ColumnOptions{DorisType: "INT, x STRING"},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_INT32,
		},
		{
			name:      "Invalid doris_type syntax with unbalanced parens",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients"},
			cOpts:     &proto2typepb.ColumnOptions{DorisType: "VARCHAR(64))"},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_STRING,
		},
		{
			name:      "Invalid doris_type syntax with NOT NULL constraint",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients"},
			cOpts:     &proto2typepb.ColumnOptions{DorisType: "INT NOT NULL"},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_INT32,
		},
		{
			name:      "Unknown column in unique_key_columns",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients", UniqueKeyColumns: []string{"nonexistent_col"}},
			cOpts:     &proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_STRING,
		},
		{
			name:      "Unknown column in inverted_index_columns",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients", InvertedIndexColumns: []string{"nonexistent_col"}},
			cOpts:     &proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_STRING,
		},
		{
			name:      "inline_nested on scalar string field",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients"},
			cOpts:     &proto2typepb.ColumnOptions{InlineNested: true},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_STRING,
		},
		{
			name:      "Invalid doris_type syntax with constraint inside angle brackets",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients"},
			cOpts:     &proto2typepb.ColumnOptions{DorisType: "ARRAY<INT NOT NULL>"},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_INT32,
		},
		{
			name:      "Disallowed Doris key type TEXT on UNIQUE KEY column",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients", UniqueKeyColumns: []string{"id"}},
			cOpts:     &proto2typepb.ColumnOptions{DorisType: "TEXT", IsUniqueKey: true},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_STRING,
		},
		{
			name:      "Disallowed Doris key type DOUBLE on fallback DUPLICATE KEY column",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients"},
			cOpts:     &proto2typepb.ColumnOptions{DorisType: "DOUBLE"},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_DOUBLE,
		},
		{
			name:      "Invalid default_value with quote injection",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients"},
			cOpts:     &proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)", DefaultValue: `active" COMMENT "pwn`},
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_STRING,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gen, f := makePluginWithTable(tc.tOpts, tc.cOpts, tc.fieldType)
			if err := runner.GenerateFile(gen, f, opts); err == nil {
				t.Errorf("expected error for %s, but GenerateFile succeeded", tc.name)
			}
		})
	}

	// Also verify invalid doris_mode returns an error:
	gen, f := makePluginWithTable(
		&proto2typepb.TableOptions{TableName: "patients"},
		&proto2typepb.ColumnOptions{DorisType: "VARCHAR(64)"},
		descriptorpb.FieldDescriptorProto_TYPE_STRING,
	)
	badModeOpts := &Options{Lang: "go", Backend: "doris", DorisMode: "invalid_mode"}
	if err := runner.GenerateFile(gen, f, badModeOpts); err == nil {
		t.Error("expected error for invalid doris_mode, got nil")
	}

	// Verify fallback key selection rejects a table whose only columns are real oneof variants,
	// and promotes the first non-oneof column when a real oneof variant appears first.
	mo := &descriptorpb.MessageOptions{}
	proto.SetExtension(mo, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{TableName: "oneof_only"},
	})
	oneofOnlyFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("oneof_only.proto"),
		Package: proto.String("test.v1"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String("example.com/test/v1;testv1")},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("OneofOnlyRecord"),
				Options: mo,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:       proto.String("variant_a"),
						Number:     proto.Int32(1),
						Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:       descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						OneofIndex: proto.Int32(0),
						JsonName:   proto.String("variantA"),
					},
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: proto.String("choice")},
				},
			},
		},
	}
	genOneofOnly := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{oneofOnlyFDP}}, []string{"oneof_only.proto"})
	if err := runner.GenerateFile(genOneofOnly, genOneofOnly.Files[0], opts); err == nil {
		t.Error("expected error when all columns are oneof variants and no keys are configured, got nil")
	}

	oneofFirstFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("oneof_first.proto"),
		Package: proto.String("test.v1"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String("example.com/test/v1;testv1")},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("OneofFirstRecord"),
				Options: mo,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:       proto.String("variant_a"),
						Number:     proto.Int32(1),
						Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:       descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						OneofIndex: proto.Int32(0),
						JsonName:   proto.String("variantA"),
					},
					{
						Name:     proto.String("record_id"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
						JsonName: proto.String("recordId"),
					},
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: proto.String("choice")},
				},
			},
		},
	}
	genOneofFirst := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{oneofFirstFDP}}, []string{"oneof_first.proto"})
	tables, err := buildDorisTablesForFile(genOneofFirst.Files[0], opts)
	if err != nil {
		t.Fatalf("expected OneofFirstRecord to succeed by promoting record_id to fallback key, got: %v", err)
	}
	if len(tables) != 1 || tables[0].Descriptor.Columns[0].ColumnName != "record_id" || tables[0].Descriptor.Columns[0].Nullable {
		t.Fatalf("expected record_id to be promoted to non-null cols[0] fallback key, got %+v", tables[0].Descriptor.Columns)
	}
	if !tables[0].Descriptor.Columns[1].Nullable {
		t.Fatalf("expected oneof variant_a to remain nullable after fallback key promotion, got %+v", tables[0].Descriptor.Columns[1])
	}
}

// TestGenerateGoDoris_CompileAndRoundTrip compiles the generated Go Doris code with protoc
// and runs round-trip value/scan/proto conversion tests in an isolated subpackage.
func TestGenerateGoDoris_CompileAndRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("protoc"); err != nil {
		t.Skip("protoc not found in PATH; skipping compile-and-run test")
	}
	if _, err := exec.LookPath("protoc-gen-go"); err != nil {
		t.Skip("protoc-gen-go not found in PATH; skipping compile-and-run test")
	}

	gen := buildComprehensiveDorisPlugin(t)
	runner := NewRunner()
	opts := &Options{
		Lang:      "go",
		Backend:   "doris",
		DorisMode: "go",
		Domain:    true,
	}

	for _, f := range gen.Files {
		if f.Generate {
			if err := runner.GenerateFile(gen, f, opts); err != nil {
				t.Fatalf("GenerateFile(%s) failed: %v", f.Desc.Path(), err)
			}
		}
	}

	resp := gen.Response()
	filesByName := make(map[string]string)
	for _, gf := range resp.File {
		filesByName[filepath.Base(gf.GetName())] = gf.GetContent()
	}

	dorisGoCode := filesByName["clinical_lakehouse_doris.type.go"]
	secGoCode := filesByName["secondary_table_doris.type.go"]
	if dorisGoCode == "" || secGoCode == "" {
		t.Fatalf("missing generated _doris.type.go files (got %v)", keysOf(filesByName))
	}

	// Create a temporary package inside the repository module so it shares go.mod dependencies.
	tmpPkgDir, err := os.MkdirTemp(".", "doris_compile_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpPkgDir)
	}()

	protoSrc := `syntax = "proto3";
package azra.clinical.v1;

import "google/protobuf/timestamp.proto";
import "google/type/date.proto";

option go_package = "example.com/lakehouse/clinicalv1;clinicalv1";

enum ClinicalRecordStatus {
  CLINICAL_RECORD_STATUS_UNSPECIFIED = 0;
  CLINICAL_RECORD_STATUS_ACTIVE = 1;
  CLINICAL_RECORD_STATUS_SUPERSEDED = 2;
}

enum CriterionAiStatus {
  CRITERION_AI_STATUS_UNSPECIFIED = 0;
  CRITERION_AI_STATUS_MET = 1;
  CRITERION_AI_STATUS_EXCLUDED = 2;
}

enum LungRadsCategory {
  LUNG_RADS_CATEGORY_UNSPECIFIED = 0;
  LUNG_RADS_CATEGORY_4A = 1;
  LUNG_RADS_CATEGORY_4B = 2;
}

enum HistologyType {
  HISTOLOGY_TYPE_UNSPECIFIED = 0;
  HISTOLOGY_TYPE_UNSPECIFIED_NSCLC = 1;
  HISTOLOGY_TYPE_ADENOCARCINOMA = 2;
}

message DocumentCitationCoordinate {
  int32 page_number = 1;
  repeated float bbox_coords = 2;
}

message SkillProvenance {
  optional string skill_name = 1;
  optional double skill_confidence = 2;
  string skill_version = 3;
  DocumentCitationCoordinate coord = 4;
}

message EligibilityCriterionResult {
  string patient_id = 1;
  string criterion_id = 2;
  int32 display_order = 3;
  optional int32 observation_age_days = 4;
  bool is_dealbreaker = 5;
  repeated string all_mrns = 6;
  CriterionAiStatus effective_status = 7;
  optional CriterionAiStatus override_status = 8;
  int32 subword_count = 9;
  google.type.Date event_month = 10;
  google.type.Date date_of_birth = 11;
  google.protobuf.Timestamp verified_at = 12;
  SkillProvenance skill_provenance = 13;
  ClinicalRecordStatus record_status = 14;
  int64 publication_generation = 15;
  LungRadsCategory lung_rads = 16;
  HistologyType histology = 17;
  uint64 fingerprint_hash = 18;
  optional uint64 optional_hash = 19;
  bytes raw_payload = 20;
  repeated int32 cohort_years = 21;
  repeated int64 cohort_ids = 22;
  repeated double thresholds = 23;
  repeated CriterionAiStatus status_history = 24;
  repeated uint64 u64_hashes = 25;
  bool is_evidence_stale = 30;
  repeated SkillProvenance hydrated_children = 31;
}

message SnomedConceptRecord {
  string id = 1;
  string release_id = 2;
}

message AnatomicSiteNormalizationRecord {
  string raw_site_term = 1;
  string system = 2;
  repeated float embedding = 3;
  double additive_offset = 4;
}
`
	secProtoSrc := `syntax = "proto3";
package azra.clinical.v1;

import "google/type/date.proto";

option go_package = "example.com/lakehouse/clinicalv1;clinicalv1";

message AuditMetadata {
  string source_ip = 1;
}

message AuditEventRecord {
  string tenant_id = 1;
  string event_id = 2;
  google.type.Date event_date = 3;
}

message DefaultInferredAuditRecord {
  optional string record_id = 1;
  optional uint64 seq_hash = 2;
  string actor_id = 3;
  AuditMetadata meta = 4;
  oneof result_value {
    double numeric_value = 5;
    string text_value = 6;
    bytes raw_bytes = 8;
  }
  uint32 retry_count = 7;
  string table_name = 9;
}
`
	if err := os.WriteFile(filepath.Join(tmpPkgDir, "clinical_lakehouse.proto"), []byte(protoSrc), 0o644); err != nil {
		t.Fatalf("write proto: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpPkgDir, "secondary_table.proto"), []byte(secProtoSrc), 0o644); err != nil {
		t.Fatalf("write secondary proto: %v", err)
	}

	googleTypeDir := filepath.Join(tmpPkgDir, "google", "type")
	if err := os.MkdirAll(googleTypeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	dateProtoSrc := `syntax = "proto3";
package google.type;
option go_package = "google.golang.org/genproto/googleapis/type/date;date";
message Date {
  int32 year = 1;
  int32 month = 2;
  int32 day = 3;
}
`
	if err := os.WriteFile(filepath.Join(googleTypeDir, "date.proto"), []byte(dateProtoSrc), 0o644); err != nil {
		t.Fatalf("write date.proto: %v", err)
	}

	cmd := exec.Command("protoc",
		"-I", tmpPkgDir,
		"--go_out="+tmpPkgDir,
		"--go_opt=paths=source_relative",
		"clinical_lakehouse.proto",
		"secondary_table.proto",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("protoc failed: %v\n%s", err, out)
	}

	if err := os.WriteFile(filepath.Join(tmpPkgDir, "clinical_lakehouse_doris.type.go"), []byte(dorisGoCode), 0o644); err != nil {
		t.Fatalf("write generated doris file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpPkgDir, "secondary_table_doris.type.go"), []byte(secGoCode), 0o644); err != nil {
		t.Fatalf("write secondary generated doris file: %v", err)
	}

	roundTripTestSrc := `package clinicalv1

import (
	"database/sql"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	date "google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"hegel.dev/go/hegel"
)

func TestDorisRoundTrip_FullAndNulls(t *testing.T) {
	obsAge := int32(14)
	overrideStat := CriterionAiStatus_CRITERION_AI_STATUS_EXCLUDED
	skillName := "blvr_fissure_evaluator"
	skillConf := 0.9425
	optHash := uint64(math.MaxUint64)
	verifiedTime := time.Date(2026, 10, 21, 15, 30, 45, 123456000, time.UTC)

	orig := &EligibilityCriterionResult{
		PatientId:          "pat-1001",
		CriterionId:        "crit-fev1",
		DisplayOrder:       2,
		ObservationAgeDays: &obsAge,
		IsDealbreaker:      true,
		AllMrns:            []string{"MRN-1", "Gilbert's syndrome", "a\"b"},
		EffectiveStatus:    CriterionAiStatus_CRITERION_AI_STATUS_MET,
		OverrideStatus:     &overrideStat,
		SubwordCount:       42,
		EventMonth:         &date.Date{Year: 2026, Month: 10, Day: 1},
		DateOfBirth:        &date.Date{Year: 1958, Month: 4, Day: 19},
		VerifiedAt:         timestamppb.New(verifiedTime),
		SkillProvenance: &SkillProvenance{
			SkillName:       &skillName,
			SkillConfidence: &skillConf,
			SkillVersion:    "v2.1.0",
			Coord: &DocumentCitationCoordinate{
				PageNumber: 7,
				BboxCoords: []float32{0.12, 0.34, 0.56, 0.78},
			},
		},
		RecordStatus:          ClinicalRecordStatus_CLINICAL_RECORD_STATUS_ACTIVE,
		PublicationGeneration: 99,
		LungRads:              LungRadsCategory_LUNG_RADS_CATEGORY_4A,
		Histology:             HistologyType_HISTOLOGY_TYPE_UNSPECIFIED_NSCLC,
		FingerprintHash:       math.MaxUint64 - 42,
		OptionalHash:          &optHash,
		RawPayload:            []byte{0xde, 0xad, 0xbe, 0xef},
		CohortYears:           []int32{2024, 2025, 2026},
		CohortIds:             []int64{100001, 100002},
		Thresholds:            []float64{0.25, 0.75},
		StatusHistory: []CriterionAiStatus{
			CriterionAiStatus_CRITERION_AI_STATUS_MET,
			CriterionAiStatus_CRITERION_AI_STATUS_EXCLUDED,
		},
		U64Hashes: []uint64{0, 42, uint64(math.MaxInt64) + 1, math.MaxUint64},
	}

	// 1. FromProto -> Values -> ScanRow -> ToProto
	var row EligibilityCriterionResultDoris
	row.FromProto(orig)

	if row.TableName() != "eligibility_criterion_results" {
		t.Fatalf("TableName() = %q, want eligibility_criterion_results", row.TableName())
	}
	if row.LungRads != "4A" {
		t.Fatalf("LungRads = %q, want 4A", row.LungRads)
	}
	if row.Histology != "UNSPECIFIED_NSCLC" {
		t.Fatalf("Histology = %q, want UNSPECIFIED_NSCLC", row.Histology)
	}
	if len(row.StatusHistory) != 2 || row.StatusHistory[0] != "MET" || row.StatusHistory[1] != "EXCLUDED" {
		t.Fatalf("StatusHistory = %v, want [MET EXCLUDED]", row.StatusHistory)
	}

	vals := EligibilityCriterionResultProtoValues(orig)
	if len(vals) != len(row.Columns()) {
		t.Fatalf("len(Values) = %d, want %d", len(vals), len(row.Columns()))
	}

	got, err := ScanEligibilityCriterionResultProto(NewDorisValueScanner(vals))
	if err != nil {
		t.Fatalf("ScanEligibilityCriterionResultProto failed: %v", err)
	}
	if !proto.Equal(orig, got) {
		t.Fatalf("round-trip mismatch:\norig: %+v\ngot:  %+v", orig, got)
	}

	// 2. Test NULL optional fields, unset Date/Timestamp, and nil 2-level inline_nested SkillProvenance
	minimal := &EligibilityCriterionResult{
		PatientId:             "pat-2002",
		CriterionId:           "crit-ct",
		DisplayOrder:          1,
		EffectiveStatus:       CriterionAiStatus_CRITERION_AI_STATUS_MET,
		EventMonth:            &date.Date{Year: 2026, Month: 10, Day: 1},
		RecordStatus:          ClinicalRecordStatus_CLINICAL_RECORD_STATUS_SUPERSEDED,
		PublicationGeneration: 1,
	}
	minVals := EligibilityCriterionResultProtoValues(minimal)
	gotMin, err := ScanEligibilityCriterionResultProto(NewDorisValueScanner(minVals))
	if err != nil {
		t.Fatalf("ScanEligibilityCriterionResultProto(minimal) failed: %v", err)
	}
	if !proto.Equal(minimal, gotMin) {
		t.Fatalf("minimal round-trip mismatch:\norig: %+v\ngot:  %+v", minimal, gotMin)
	}
	if gotMin.SkillProvenance != nil {
		t.Fatalf("expected nil SkillProvenance when all inline_nested columns are NULL, got %+v", gotMin.SkillProvenance)
	}

	// 3. Test 2-level inline_nested when ONLY a level-2 leaf field (Coord.PageNumber) is set!
	onlyLevel2 := &EligibilityCriterionResult{
		PatientId:       "pat-3003",
		CriterionId:     "crit-lvl2",
		EffectiveStatus: CriterionAiStatus_CRITERION_AI_STATUS_MET,
		EventMonth:      &date.Date{Year: 2026, Month: 10, Day: 1},
		RecordStatus:    ClinicalRecordStatus_CLINICAL_RECORD_STATUS_ACTIVE,
		SkillProvenance: &SkillProvenance{
			Coord: &DocumentCitationCoordinate{
				PageNumber: 12,
			},
		},
	}
	lvl2Vals := EligibilityCriterionResultProtoValues(onlyLevel2)
	gotLvl2, err := ScanEligibilityCriterionResultProto(NewDorisValueScanner(lvl2Vals))
	if err != nil {
		t.Fatalf("Scan(onlyLevel2) failed: %v", err)
	}
	if !proto.Equal(onlyLevel2, gotLvl2) {
		t.Fatalf("2-level inline_nested mismatch:\norig: %+v\ngot:  %+v", onlyLevel2, gotLvl2)
	}

	// 4. Test record_status default to "ACTIVE" when UNSPECIFIED on proto, and unset NOT NULL date (EventMonth == nil) round-trip
	unspecStatus := &EligibilityCriterionResult{
		PatientId: "pat-4004",
	}
	var unspecRow EligibilityCriterionResultDoris
	unspecRow.FromProto(unspecStatus)
	if unspecRow.RecordStatus != "ACTIVE" {
		t.Fatalf("expected unset RecordStatus to default to ACTIVE in FromProto, got %q", unspecRow.RecordStatus)
	}
	unspecVals := unspecRow.Values()
	gotUnspec, err := ScanEligibilityCriterionResultProto(NewDorisValueScanner(unspecVals))
	if err != nil {
		t.Fatalf("ScanEligibilityCriterionResultProto(unspecStatus) failed: %v", err)
	}
	if gotUnspec.EventMonth != nil {
		t.Fatalf("expected unset EventMonth to remain nil after Values -> ScanRow -> ToProto, got %+v", gotUnspec.EventMonth)
	}

	// 5. Test unknown string on optional enum remains nil in ToProto
	unknownEnum := "BOGUS_UNKNOWN_STATUS"
	unspecRow.OverrideStatus = &unknownEnum
	if p := unspecRow.ToProto(); p.OverrideStatus != nil {
		t.Fatalf("expected unknown OverrideStatus string to remain nil in ToProto, got %v", *p.OverrideStatus)
	}

	// 6. Test explicit zero-value (UNSPECIFIED) optional enum preserves presence across round-trip
	zeroEnum := CriterionAiStatus_CRITERION_AI_STATUS_UNSPECIFIED
	withZeroOptEnum := &EligibilityCriterionResult{
		PatientId:       "pat-5005",
		CriterionId:     "crit-zero-enum",
		EffectiveStatus: CriterionAiStatus_CRITERION_AI_STATUS_MET,
		OverrideStatus:  &zeroEnum,
		EventMonth:      &date.Date{Year: 2026, Month: 10, Day: 1},
		RecordStatus:    ClinicalRecordStatus_CLINICAL_RECORD_STATUS_ACTIVE,
	}
	zeroEnumVals := EligibilityCriterionResultProtoValues(withZeroOptEnum)
	gotZeroEnum, err := ScanEligibilityCriterionResultProto(NewDorisValueScanner(zeroEnumVals))
	if err != nil {
		t.Fatalf("Scan(withZeroOptEnum) failed: %v", err)
	}
	if gotZeroEnum.OverrideStatus == nil || *gotZeroEnum.OverrideStatus != CriterionAiStatus_CRITERION_AI_STATUS_UNSPECIFIED {
		t.Fatalf("expected explicit UNSPECIFIED optional enum to preserve presence, got %v", gotZeroEnum.OverrideStatus)
	}
	if !proto.Equal(withZeroOptEnum, gotZeroEnum) {
		t.Fatalf("withZeroOptEnum round-trip mismatch:\norig: %+v\ngot:  %+v", withZeroOptEnum, gotZeroEnum)
	}
}

func TestDorisStringArray_ScanEdgeCases(t *testing.T) {
	cases := []struct {
		name    string
		input   any
		want    []string
		wantErr bool
	}{
		{
			name:  "Doris single-quoted with escaped apostrophes",
			input: "['Gilbert\\'s syndrome', 'Non-Hodgkin\\'s lymphoma']",
			want:  []string{"Gilbert's syndrome", "Non-Hodgkin's lymphoma"},
		},
		{
			name:  "Doris single-quoted with doubled single quotes",
			input: "['Crohn''s disease', 'Bells palsy']",
			want:  []string{"Crohn's disease", "Bells palsy"},
		},
		{
			name:  "Doris single-quoted with embedded double quotes",
			input: "['a\"b', 'c']",
			want:  []string{"a\"b", "c"},
		},
		{
			name:  "Standard JSON array with apostrophes",
			input: "[\"Gilbert's syndrome\", \"a\\\"b\"]",
			want:  []string{"Gilbert's syndrome", "a\"b"},
		},
		{
			name:  "Empty array literal",
			input: "[]",
			want:  []string{},
		},
		{
			name:    "Malformed non-array literal",
			input:   "not an array",
			wantErr: true,
		},
		{
			name:    "Unterminated single-quoted element",
			input:   "['unterminated]",
			wantErr: true,
		},
		{
			name:    "JSON string array with null element",
			input:   "[\"a\", null, \"b\"]",
			wantErr: true,
		},
		{
			name:    "Single-quoted string array with null element",
			input:   "['a', null, 'b']",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var arr DorisStringArray
			err := arr.Scan(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error scanning %v, got nil (arr=%v)", tc.input, arr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual([]string(arr), tc.want) {
				t.Fatalf("got %#v, want %#v", []string(arr), tc.want)
			}
		})
	}
}

func TestDorisFloat32Array_ValueAndScanEdgeCases(t *testing.T) {
	// 1. NaN and Inf must be rejected by Value() and propagated by NewDorisValueScanner
	nanArr := DorisFloat32Array{0.5, float32(math.NaN())}
	if _, err := nanArr.Value(); err == nil {
		t.Fatal("expected error from DorisFloat32Array.Value() with NaN, got nil")
	}
	infArr := DorisFloat32Array{float32(math.Inf(1))}
	if _, err := infArr.Value(); err == nil {
		t.Fatal("expected error from DorisFloat32Array.Value() with Inf, got nil")
	}

	var dest DorisFloat32Array
	if err := NewDorisValueScanner([]any{nanArr}).Scan(&dest); err == nil {
		t.Fatal("expected NewDorisValueScanner to propagate Valuer error on NaN")
	}

	// 2. Null element inside numeric array must return an error
	var scanTarget DorisFloat32Array
	if err := scanTarget.Scan("[1.0, null, 2.0]"); err == nil {
		t.Fatal("expected error scanning [1.0, null, 2.0] into DorisFloat32Array, got nil")
	}
}

func TestDorisBoolAndUint64Array_WireEdgeCases(t *testing.T) {
	// 1. DorisBoolArray: JSON booleans, Doris TINYINT(1) [1, 0, 1], null element error, and out-of-range int error
	var boolArr DorisBoolArray
	if err := boolArr.Scan("[true, false, true]"); err != nil || !reflect.DeepEqual([]bool(boolArr), []bool{true, false, true}) {
		t.Fatalf("DorisBoolArray.Scan([true, false, true]) = (%v, %v)", boolArr, err)
	}
	if err := boolArr.Scan("[1, 0, 1]"); err != nil || !reflect.DeepEqual([]bool(boolArr), []bool{true, false, true}) {
		t.Fatalf("DorisBoolArray.Scan([1, 0, 1]) = (%v, %v)", boolArr, err)
	}
	if err := boolArr.Scan("[1, null, 0]"); err == nil {
		t.Fatal("expected error scanning [1, null, 0] into DorisBoolArray, got nil")
	}
	if err := boolArr.Scan("[1, 2, 0]"); err == nil {
		t.Fatal("expected error scanning [1, 2, 0] into DorisBoolArray, got nil")
	}

	// 2. DorisUint64Array: unquoted numbers, JSON string numbers, Doris single-quoted numbers, null error, negative error
	var u64Arr DorisUint64Array
	wantU64 := []uint64{42, uint64(math.MaxInt64) + 1, math.MaxUint64}
	if err := u64Arr.Scan("[42, 9223372036854775808, 18446744073709551615]"); err != nil || !reflect.DeepEqual([]uint64(u64Arr), wantU64) {
		t.Fatalf("DorisUint64Array.Scan(unquoted) = (%v, %v)", u64Arr, err)
	}
	if err := u64Arr.Scan("[\"42\", \"9223372036854775808\", \"18446744073709551615\"]"); err != nil || !reflect.DeepEqual([]uint64(u64Arr), wantU64) {
		t.Fatalf("DorisUint64Array.Scan(json strings) = (%v, %v)", u64Arr, err)
	}
	if err := u64Arr.Scan("['42', '9223372036854775808', '18446744073709551615']"); err != nil || !reflect.DeepEqual([]uint64(u64Arr), wantU64) {
		t.Fatalf("DorisUint64Array.Scan(single-quoted strings) = (%v, %v)", u64Arr, err)
	}
	if err := u64Arr.Scan("[42, \"9223372036854775808\", 18446744073709551615]"); err != nil || !reflect.DeepEqual([]uint64(u64Arr), wantU64) {
		t.Fatalf("DorisUint64Array.Scan(mixed number and string) = (%v, %v)", u64Arr, err)
	}
	if err := u64Arr.Scan("[42, null]"); err == nil {
		t.Fatal("expected error scanning [42, null] into DorisUint64Array, got nil")
	}
	if err := u64Arr.Scan("[\"-1\"]"); err == nil {
		t.Fatal("expected error scanning [\"-1\"] into DorisUint64Array, got nil")
	}

	// 3. scanDorisUint64 must reject negative int64 and sql.NullInt64 values
	if _, _, err := scanDorisUint64(int64(-1)); err == nil {
		t.Fatal("expected scanDorisUint64(int64(-1)) to return error, got nil")
	}
	if _, _, err := scanDorisUint64(sql.NullInt64{Int64: -42, Valid: true}); err == nil {
		t.Fatal("expected scanDorisUint64(sql.NullInt64{-42}) to return error, got nil")
	}
}

func TestDorisDateAndTimeHelpers_EdgeCases(t *testing.T) {
	invalidDates := []string{
		"2026-02-31",
		"2026-13-45",
		"9999999999-01-01",
		"0000-00-00",
		"not-a-date",
	}
	for _, bad := range invalidDates {
		if dt := parseDorisDateToProto(bad); dt != nil {
			t.Errorf("expected parseDorisDateToProto(%q) to return nil, got %+v", bad, dt)
		}
	}

	if got := formatProtoDateToDoris(&date.Date{Year: 2026, Month: 2, Day: 31}); got != "" {
		t.Errorf("expected formatProtoDateToDoris(2026-02-31) to return empty string, got %q", got)
	}
	if got := formatProtoDateToDoris(&date.Date{Year: 2026, Month: 2, Day: 28}); got != "2026-02-28" {
		t.Errorf("expected formatProtoDateToDoris(2026-02-28) = 2026-02-28, got %q", got)
	}

	if tm, ok := parseDorisTimeString("0001-01-01 00:00:00"); ok || !tm.IsZero() {
		t.Errorf("expected zero time string to return (_, false), got (%v, %v)", tm, ok)
	}
	if tm, ok := parseDorisTimeString("2026-10-21 15:30:45.123456"); !ok || tm.Year() != 2026 {
		t.Errorf("expected valid datetime string to parse, got (%v, %v)", tm, ok)
	}
	if tm, ok := parseDorisTimeString("2026-10-21 15:30:45.123456789"); !ok || tm.Nanosecond() != 123456789 {
		t.Errorf("expected 9-digit nanosecond datetime string to parse, got (%v, %v)", tm, ok)
	}
	if _, _, err := scanDorisTime("not-a-valid-timestamp"); err == nil {
		t.Error("expected scanDorisTime to return error on corrupted timestamp string")
	}
	if _, err := scanDorisDateString("2026-02-31"); err == nil {
		t.Error("expected scanDorisDateString to return error on invalid calendar date")
	}

	// Verify secondary table in the same Go package compiles and round-trips
	audit := &AuditEventRecord{
		TenantId:  "akron_general",
		EventId:   "evt-1",
		EventDate: &date.Date{Year: 2026, Month: 10, Day: 21},
	}
	auditVals := AuditEventRecordProtoValues(audit)
	gotAudit, err := ScanAuditEventRecordProto(NewDorisValueScanner(auditVals))
	if err != nil || !proto.Equal(audit, gotAudit) {
		t.Fatalf("AuditEventRecord round-trip failed: err=%v, got=%+v", err, gotAudit)
	}
	if !strings.Contains(AuditEventRecordDorisTable().BuildDorisCreateTableSQL("", 0), "DISTRIBUTED BY HASH(event_id)") {
		t.Fatal("expected AuditEventRecordDorisTable() to distribute by event_id")
	}

	// Verify generated dorisColumnIcebergDDLType maps JSON/JSONB/VARIANT -> STRING and BuildDorisCreateTableSQL emits USING ANN on DUPLICATE KEY embedding
	for _, jt := range []string{"JSON", "JSONB", "VARIANT"} {
		if got := dorisColumnIcebergDDLType(jt, ""); got != "STRING" {
			t.Fatalf("generated dorisColumnIcebergDDLType(%q, \"\") = %q, want STRING", jt, got)
		}
	}
	ragDesc := DorisTableDescriptor{
		TableName:           "clinical_concept_search",
		DatabaseScope:       "GLOBAL_TERMINOLOGY_RAG",
		SyncTier:            "INTERNAL_ONLY",
		DuplicateKeyColumns: []string{"concept_id", "code_system"},
		Columns: []DorisColumnDescriptor{
			{ColumnName: "concept_id", DorisType: "VARCHAR(64)", Nullable: false},
			{ColumnName: "code_system", DorisType: "VARCHAR(32)", Nullable: false},
			{ColumnName: "embedding", DorisType: "ARRAY<FLOAT>", Nullable: false},
		},
	}
	if !strings.Contains(ragDesc.BuildDorisCreateTableSQL("", 0), "INDEX idx_embedding_hnsw (embedding) USING ANN PROPERTIES (\"index_type\" = \"hnsw\", \"metric_type\" = \"l2_distance\", \"dim\" = \"768\")") {
		t.Fatalf("generated BuildDorisCreateTableSQL missing USING ANN index:\n%s", ragDesc.BuildDorisCreateTableSQL("", 0))
	}
	nonConceptRagDesc := ragDesc
	nonConceptRagDesc.TableName = "custom_vector_events"
	if strings.Contains(nonConceptRagDesc.BuildDorisCreateTableSQL("", 0), "USING ANN") {
		t.Fatalf("generated BuildDorisCreateTableSQL should not emit hardcoded dim=768 USING ANN index on non-clinical_concept_search table:\n%s", nonConceptRagDesc.BuildDorisCreateTableSQL("", 0))
	}

	// Verify DefaultInferredAuditRecord:
	// 1) Unannotated sibling field ActorId and inline_nested Meta.SourceIp are preserved
	// 2) Unset proto3 optional NOT NULL columns (record_id, seq_hash) emit non-nil zero values in Values()
	// 3) Real proto3 oneof (result_value: numeric_value | text_value) and uint32 (retry_count) round-trip cleanly
	emptyDef := &DefaultInferredAuditRecord{
		ActorId:    "dr-smith",
		Meta:       &AuditMetadata{SourceIp: "10.0.0.1"},
		RetryCount: 3,
		TableName:  "patients",
	}
	defVals := DefaultInferredAuditRecordProtoValues(emptyDef)
	if len(defVals) != 9 {
		t.Fatalf("len(DefaultInferredAuditRecordProtoValues) = %d, want 9", len(defVals))
	}
	if defVals[0] != "" {
		t.Fatalf("expected unset optional NOT NULL record_id to emit empty string, got %#v", defVals[0])
	}
	if defVals[1] != int64(0) {
		t.Fatalf("expected unset optional NOT NULL seq_hash to emit int64(0), got %#v", defVals[1])
	}
	gotDef, err := ScanDefaultInferredAuditRecordProto(NewDorisValueScanner(defVals))
	if err != nil {
		t.Fatalf("ScanDefaultInferredAuditRecordProto failed: %v", err)
	}
	if gotDef.GetActorId() != "dr-smith" || gotDef.GetMeta().GetSourceIp() != "10.0.0.1" || gotDef.GetRetryCount() != 3 || gotDef.GetTableName() != "patients" {
		t.Fatalf("DefaultInferredAuditRecord round-trip mismatch: %+v", gotDef)
	}
	if gotDef.RecordId == nil || *gotDef.RecordId != "" || gotDef.SeqHash == nil || *gotDef.SeqHash != 0 {
		t.Fatalf("expected NOT NULL optional key/seq columns to scan back non-nil zero values, got %+v", gotDef)
	}
	if gotDef.GetResultValue() != nil {
		t.Fatalf("expected unset oneof ResultValue to remain nil, got %+v", gotDef.GetResultValue())
	}

	// Test real oneof active numeric_value (including 0.0), text_value, and raw_bytes (including empty []byte{}) round-trips
	recID := "rec-100"
	seqH := uint64(7)
	for _, oneofCase := range []*DefaultInferredAuditRecord{
		{
			RecordId:    &recID,
			SeqHash:     &seqH,
			ResultValue: &DefaultInferredAuditRecord_NumericValue{NumericValue: 98.6},
		},
		{
			RecordId:    &recID,
			SeqHash:     &seqH,
			ResultValue: &DefaultInferredAuditRecord_NumericValue{NumericValue: 0.0},
		},
		{
			RecordId:    &recID,
			SeqHash:     &seqH,
			ResultValue: &DefaultInferredAuditRecord_TextValue{TextValue: "NEGATIVE"},
		},
		{
			RecordId:    &recID,
			SeqHash:     &seqH,
			ResultValue: &DefaultInferredAuditRecord_RawBytes{RawBytes: []byte{0xca, 0xfe}},
		},
		{
			RecordId:    &recID,
			SeqHash:     &seqH,
			ResultValue: &DefaultInferredAuditRecord_RawBytes{RawBytes: []byte{}},
		},
	} {
		ovVals := DefaultInferredAuditRecordProtoValues(oneofCase)
		gotOV, err := ScanDefaultInferredAuditRecordProto(NewDorisValueScanner(ovVals))
		if err != nil {
			t.Fatalf("ScanDefaultInferredAuditRecordProto(oneofCase) failed: %v", err)
		}
		if !proto.Equal(oneofCase, gotOV) {
			t.Fatalf("oneof round-trip mismatch:\norig: %+v\ngot:  %+v", oneofCase, gotOV)
		}
	}

	// Test int32 and uint32 overflow/negative rejection in ScanRow
	if _, _, err := scanDorisInt32(sql.NullInt64{Int64: math.MaxInt32 + 1, Valid: true}); err == nil {
		t.Fatal("expected scanDorisInt32(MaxInt32+1) to return overflow error")
	}
	if _, _, err := scanDorisInt32(sql.NullInt64{Int64: math.MinInt32 - 1, Valid: true}); err == nil {
		t.Fatal("expected scanDorisInt32(MinInt32-1) to return overflow error")
	}
	if _, _, err := scanDorisUint32(sql.NullInt64{Int64: -1, Valid: true}); err == nil {
		t.Fatal("expected scanDorisUint32(-1) to return error")
	}
	if _, _, err := scanDorisUint32(sql.NullInt64{Int64: math.MaxUint32 + 1, Valid: true}); err == nil {
		t.Fatal("expected scanDorisUint32(MaxUint32+1) to return overflow error")
	}
	badUint32Vals := append([]any(nil), defVals...)
	badUint32Vals[7] = int64(-5)
	if _, err := ScanDefaultInferredAuditRecordProto(NewDorisValueScanner(badUint32Vals)); err == nil {
		t.Fatal("expected ScanDefaultInferredAuditRecordProto to reject negative uint32 retry_count")
	}
}

func TestProperty_GeneratedDorisRuntime_ArraysDatesAndProtoRoundTrip(t *testing.T) {
	// 1. Property: DorisStringArray, DorisInt32Array, DorisInt64Array, DorisUint64Array,
	//    DorisFloat32Array, DorisFloat64Array, and DorisBoolArray Value() <-> Scan() round-trip.
	hegel.Test(t, func(ht *hegel.T) {
		n := hegel.Draw(ht, hegel.Integers(0, 6))
		strs := make(DorisStringArray, n)
		var sqParts []string
		for i := 0; i < n; i++ {
			s := hegel.Draw(ht, hegel.Text().MaxSize(24))
			strs[i] = s
			escaped := strings.ReplaceAll(s, "\\", "\\\\")
			escaped = strings.ReplaceAll(escaped, "'", "\\'")
			escaped = strings.ReplaceAll(escaped, "\n", "\\n")
			escaped = strings.ReplaceAll(escaped, "\r", "\\r")
			escaped = strings.ReplaceAll(escaped, "\t", "\\t")
			sqParts = append(sqParts, "'"+escaped+"'")
		}
		v, err := strs.Value()
		if err != nil {
			ht.Fatalf("DorisStringArray.Value failed: %v", err)
		}
		var gotJSON DorisStringArray
		if err := gotJSON.Scan(v); err != nil {
			ht.Fatalf("DorisStringArray.Scan(JSON) failed: %v", err)
		}
		if !reflect.DeepEqual([]string(strs), []string(gotJSON)) {
			ht.Fatalf("DorisStringArray JSON round-trip mismatch: %#v vs %#v", strs, gotJSON)
		}
		sqLiteral := "[" + strings.Join(sqParts, ", ") + "]"
		var gotSQ DorisStringArray
		if err := gotSQ.Scan(sqLiteral); err != nil {
			ht.Fatalf("DorisStringArray.Scan(single-quoted %q) failed: %v", sqLiteral, err)
		}
		if !reflect.DeepEqual([]string(strs), []string(gotSQ)) {
			ht.Fatalf("DorisStringArray single-quoted round-trip mismatch: %#v vs %#v", strs, gotSQ)
		}

		u64s := make(DorisUint64Array, n)
		for i := 0; i < n; i++ {
			lo := uint64(hegel.Draw(ht, hegel.Integers(0, math.MaxInt32)))
			if hegel.Draw(ht, hegel.Booleans()) {
				u64s[i] = uint64(math.MaxInt64) + 1 + lo
			} else {
				u64s[i] = lo
			}
		}
		uv, err := u64s.Value()
		if err != nil {
			ht.Fatalf("DorisUint64Array.Value failed: %v", err)
		}
		var gotU64 DorisUint64Array
		if err := gotU64.Scan(uv); err != nil || !reflect.DeepEqual([]uint64(u64s), []uint64(gotU64)) {
			ht.Fatalf("DorisUint64Array round-trip mismatch: %v vs %v (err=%v)", u64s, gotU64, err)
		}
	})

	// 2. Property: formatDorisDateParts <-> parseDorisDateParts bijection on valid calendar dates.
	hegel.Test(t, func(ht *hegel.T) {
		year := int32(hegel.Draw(ht, hegel.Integers(2, 9999)))
		month := int32(hegel.Draw(ht, hegel.Integers(1, 12)))
		day := int32(hegel.Draw(ht, hegel.Integers(1, 31)))

		formatted := formatDorisDateParts(year, month, day)
		ref := time.Date(int(year), time.Month(month), int(day), 0, 0, 0, 0, time.UTC)
		isValidCalendar := ref.Year() == int(year) && int32(ref.Month()) == month && int32(ref.Day()) == day

		if !isValidCalendar {
			if formatted != "" {
				ht.Fatalf("expected invalid calendar date %04d-%02d-%02d to format as empty, got %q", year, month, day, formatted)
			}
			return
		}
		gy, gm, gd, ok := parseDorisDateParts(formatted)
		if !ok || gy != year || gm != month || gd != day {
			ht.Fatalf("date round-trip failed for %04d-%02d-%02d -> %q -> (%d,%d,%d,%v)", year, month, day, formatted, gy, gm, gd, ok)
		}
	})

	// 3. Property: Full Proto <-> Doris Row (FromProto -> Values -> ScanRow -> ToProto) round-trip.
	hegel.Test(t, func(ht *hegel.T) {
		recID := fmt.Sprintf("rec-%d", hegel.Draw(ht, hegel.Integers(1, 100000)))
		seqH := uint64(hegel.Draw(ht, hegel.Integers(0, 100000)))
		if hegel.Draw(ht, hegel.Booleans()) {
			seqH += uint64(math.MaxInt64)
		}
		orig := &DefaultInferredAuditRecord{
			RecordId:   &recID,
			SeqHash:    &seqH,
			ActorId:    hegel.Draw(ht, hegel.Text().MaxSize(20)),
			RetryCount: uint32(hegel.Draw(ht, hegel.Integers(0, 1000))),
			TableName:  hegel.Draw(ht, hegel.Text().MaxSize(20)),
		}
		if srcIP := hegel.Draw(ht, hegel.Text().MaxSize(16)); srcIP != "" {
			orig.Meta = &AuditMetadata{SourceIp: srcIP}
		}
		switch hegel.Draw(ht, hegel.Integers(0, 3)) {
		case 1:
			orig.ResultValue = &DefaultInferredAuditRecord_NumericValue{
				NumericValue: float64(hegel.Draw(ht, hegel.Integers(-1000, 1000))) / 10.0,
			}
		case 2:
			orig.ResultValue = &DefaultInferredAuditRecord_TextValue{
				TextValue: hegel.Draw(ht, hegel.Text().MaxSize(20)),
			}
		case 3:
			orig.ResultValue = &DefaultInferredAuditRecord_RawBytes{
				RawBytes: []byte(hegel.Draw(ht, hegel.Text().MaxSize(20))),
			}
		}

		vals := DefaultInferredAuditRecordProtoValues(orig)
		got, err := ScanDefaultInferredAuditRecordProto(NewDorisValueScanner(vals))
		if err != nil {
			ht.Fatalf("ScanDefaultInferredAuditRecordProto failed: %v", err)
		}
		if !proto.Equal(orig, got) {
			ht.Fatalf("property round-trip mismatch:\norig: %+v\ngot:  %+v", orig, got)
		}
	})
}
`
	if err := os.WriteFile(filepath.Join(tmpPkgDir, "roundtrip_test.go"), []byte(roundTripTestSrc), 0o644); err != nil {
		t.Fatalf("write roundtrip_test.go: %v", err)
	}

	testCmd := exec.Command("go", "test", "-v", "./"+filepath.Base(tmpPkgDir))
	if out, err := testCmd.CombinedOutput(); err != nil {
		t.Fatalf("compiled roundtrip go test failed: %v\n%s", err, out)
	}
}

// TestGenerateGoDoris_RealPlatformProtos validates Doris code and DDL generation against
// the production Azra platform protobuf definitions when present in the workspace.
func TestGenerateGoDoris_RealPlatformProtos(t *testing.T) {
	if _, err := exec.LookPath("protoc"); err != nil {
		t.Skip("protoc not found in PATH; skipping real platform proto test")
	}
	platformProtoDir := filepath.Clean("../../platform/proto")
	if _, err := os.Stat(filepath.Join(platformProtoDir, "azra/clinical/v1/patient_profile.proto")); err != nil {
		t.Skipf("platform/proto not found at %s; skipping", platformProtoDir)
	}

	tmpDir := t.TempDir()
	// Write minimal google/type/date.proto and buf/validate/validate.proto stubs for protoc.
	if err := os.MkdirAll(filepath.Join(tmpDir, "google/type"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "buf/validate"), 0o755); err != nil {
		t.Fatal(err)
	}
	dateProto := `syntax = "proto3";
package google.type;
option go_package = "google.golang.org/genproto/googleapis/type/date;date";
message Date {
  int32 year = 1;
  int32 month = 2;
  int32 day = 3;
}
`
	validateProto := `syntax = "proto3";
package buf.validate;
import "google/protobuf/descriptor.proto";
option go_package = "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate";

message StringRules {
  optional uint64 len = 19;
  optional uint64 min_len = 1;
  optional uint64 max_len = 2;
  optional string pattern = 6;
  repeated string in = 10;
  optional bool uuid = 22;
}
message Int32Rules {
  optional int32 lt = 2;
  optional int32 lte = 3;
  optional int32 gt = 4;
  optional int32 gte = 5;
  repeated int32 in = 6;
  repeated int32 not_in = 7;
}
message Int64Rules {
  optional int64 lt = 2;
  optional int64 lte = 3;
  optional int64 gt = 4;
  optional int64 gte = 5;
  repeated int64 in = 6;
  repeated int64 not_in = 7;
}
message FloatRules {
  optional float lt = 2;
  optional float lte = 3;
  optional float gt = 4;
  optional float gte = 5;
}
message DoubleRules {
  optional double lt = 2;
  optional double lte = 3;
  optional double gt = 4;
  optional double gte = 5;
}
message EnumRules {
  optional bool defined_only = 2;
  repeated int32 in = 3;
  repeated int32 not_in = 4;
}
message RepeatedRules {
  optional uint64 min_items = 1;
  optional uint64 max_items = 2;
  optional bool unique = 3;
  optional FieldConstraints items = 4;
}
message Rule {
  string id = 1;
  string message = 2;
  string expression = 3;
}
message MessageConstraints {
  repeated Rule cel = 3;
}
message FieldConstraints {
  bool required = 25;
  repeated Rule cel = 23;
  FloatRules float = 1;
  DoubleRules double = 2;
  Int32Rules int32 = 3;
  Int64Rules int64 = 4;
  StringRules string = 14;
  EnumRules enum = 16;
  RepeatedRules repeated = 18;
}
extend google.protobuf.MessageOptions {
  MessageConstraints message = 1159;
}
extend google.protobuf.FieldOptions {
  FieldConstraints field = 1159;
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "google/type/date.proto"), []byte(dateProto), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "buf/validate/validate.proto"), []byte(validateProto), 0o644); err != nil {
		t.Fatal(err)
	}

	descPath := filepath.Join(tmpDir, "platform.binpb")
	targetFiles := []string{
		"azra/clinical/v1/patient_profile.proto",
		"azra/clinical/eligibility/v1/eligibility.proto",
		"azra/terminology/v1/terminology_tables.proto",
	}
	args := append([]string{
		"-I", platformProtoDir,
		"-I", tmpDir,
		"--include_imports",
		"--descriptor_set_out=" + descPath,
	}, targetFiles...)

	cmd := exec.Command("protoc", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("protoc failed on platform/proto: %v\n%s", err, out)
	}

	rawBytes, err := os.ReadFile(descPath)
	if err != nil {
		t.Fatal(err)
	}
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(rawBytes, &fds); err != nil {
		t.Fatal(err)
	}
	for _, fdp := range fds.File {
		if fdp.Options == nil {
			fdp.Options = &descriptorpb.FileOptions{}
		}
		if fdp.Options.GetGoPackage() == "" {
			pkg := strings.ReplaceAll(fdp.GetPackage(), ".", "/")
			fdp.Options.GoPackage = proto.String("gitlab.com/invenero/engineering/platform/proto/gen/go/" + pkg)
		}
	}

	gen := newPlugin(t, &fds, targetFiles)
	runner := NewRunner()
	opts := &Options{
		Lang:      "go",
		Backend:   "doris",
		DorisMode: "both",
		Domain:    true,
	}

	tableCounts := make(map[string]int)
	colCountsByTable := make(map[string]int)
	tenantDescsByTable := make(map[string]DorisTableDescriptor)
	terminologyDescsByTable := make(map[string]DorisTableDescriptor)
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		tables, err := buildDorisTablesForFile(f, opts)
		if err != nil {
			t.Fatalf("buildDorisTablesForFile(%s) failed: %v", f.Desc.Path(), err)
		}
		tableCounts[f.Desc.Path()] = len(tables)
		for _, tbl := range tables {
			colCountsByTable[tbl.Descriptor.TableName] = len(tbl.Descriptor.Columns)
			if f.Desc.Path() != "azra/terminology/v1/terminology_tables.proto" {
				tenantDescsByTable[tbl.Descriptor.TableName] = tbl.Descriptor
			} else {
				terminologyDescsByTable[tbl.Descriptor.TableName] = tbl.Descriptor
			}
		}
		if err := runner.GenerateFile(gen, f, opts); err != nil {
			t.Fatalf("GenerateFile(%s) failed: %v", f.Desc.Path(), err)
		}
	}

	if conceptSearch, ok := terminologyDescsByTable["clinical_concept_search"]; !ok {
		t.Errorf("missing clinical_concept_search in terminology_tables.proto")
	} else {
		conceptSQL := conceptSearch.BuildDorisCreateTableSQL("", 0)
		wantANN := `INDEX idx_embedding_hnsw (embedding) USING ANN PROPERTIES ("index_type" = "hnsw", "metric_type" = "l2_distance", "dim" = "768")`
		if !strings.Contains(conceptSQL, wantANN) {
			t.Errorf("clinical_concept_search missing Faiss HNSW USING ANN index:\n  want: %s\n  got:\n%s", wantANN, conceptSQL)
		}
	}

	if got := tableCounts["azra/clinical/v1/patient_profile.proto"]; got != 52 {
		t.Errorf("patient_profile.proto table count = %d, want 52", got)
	}
	if got := tableCounts["azra/clinical/eligibility/v1/eligibility.proto"]; got != 2 {
		t.Errorf("eligibility.proto table count = %d, want 2", got)
	}
	if got := tableCounts["azra/terminology/v1/terminology_tables.proto"]; got != 41 {
		t.Errorf("terminology_tables.proto table count = %d, want 41", got)
	}
	if got := colCountsByTable["eligibility_criterion_results"]; got != 31 {
		t.Errorf("eligibility_criterion_results column count = %d, want 31", got)
	}
	if got := colCountsByTable["eligibility_evaluations"]; got != 34 {
		t.Errorf("eligibility_evaluations column count = %d, want 34", got)
	}

	if len(tenantDescsByTable) != 54 {
		t.Fatalf("total tenant clinical tables = %d, want 54", len(tenantDescsByTable))
	}
	for tblName, desc := range tenantDescsByTable {
		dorisSQL := desc.BuildDorisCreateTableSQL("", 0)
		icebergSQL := desc.BuildIcebergCreateTableSQL("")
		wantDorisHeader := "CREATE TABLE IF NOT EXISTS internal.tenant_${TENANT_SLUG}." + tblName + " ("
		wantIceHeader := "CREATE TABLE IF NOT EXISTS azra_lake.tenant_${TENANT_SLUG}." + tblName + " ("
		if !strings.Contains(dorisSQL, wantDorisHeader) {
			t.Errorf("table %s missing Doris header %q in:\n%s", tblName, wantDorisHeader, dorisSQL)
		}
		if !strings.Contains(icebergSQL, wantIceHeader) {
			t.Errorf("table %s missing Iceberg header %q in:\n%s", tblName, wantIceHeader, icebergSQL)
		}
		if tblName == "patient_note_chunks" {
			wantBM25 := `INDEX idx_chunk_text_bm25 (chunk_text) USING INVERTED PROPERTIES ("parser" = "english", "support_phrase" = "true", "lower_case" = "true")`
			if !strings.Contains(dorisSQL, wantBM25) {
				t.Errorf("patient_note_chunks missing BM25 inverted index:\n  want: %s\n  got:\n%s", wantBM25, dorisSQL)
			}
		}
	}

	// If platform/schemas/iceberg/doris_tenant_profile_ddl.sql is present, verify 1:1 column count parity across all 38 tables.
	schemaDDLPath := filepath.Clean("../../platform/schemas/iceberg/doris_tenant_profile_ddl.sql")
	if rawDDL, err := os.ReadFile(schemaDDLPath); err == nil {
		ddlCols := parseDorisDDLColumnCounts(string(rawDDL))
		for tblName, wantCols := range ddlCols {
			gotCols, ok := colCountsByTable[tblName]
			if !ok {
				t.Errorf("table %s from doris_tenant_profile_ddl.sql missing in proto descriptors", tblName)
				continue
			}
			if gotCols != wantCols {
				t.Errorf("table %s column count = %d, want %d from doris_tenant_profile_ddl.sql", tblName, gotCols, wantCols)
			}
		}
	}
}

// parseDorisDDLColumnCounts counts physical column definitions per table in a Doris SQL DDL file.
func parseDorisDDLColumnCounts(sql string) map[string]int {
	counts := make(map[string]int)
	prefix := "CREATE TABLE IF NOT EXISTS internal.tenant_${TENANT_SLUG}."
	for _, block := range strings.Split(sql, prefix)[1:] {
		openIdx := strings.IndexByte(block, '(')
		if openIdx < 0 {
			continue
		}
		tableName := strings.TrimSpace(block[:openIdx])
		body := block[openIdx+1:]
		colCount := 0
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, ")") {
				break
			}
			if trimmed == "" || strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "INDEX ") || strings.HasPrefix(trimmed, `"`) {
				continue
			}
			colCount++
		}
		counts[tableName] = colCount
	}
	return counts
}

// keysOf returns all map keys as a slice for diagnostic test output.
func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
