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
		"SkillVersion *string `db:\"skill_version\" doris:\"VARCHAR(64)\"`",
		"PageNumber *int32 `db:\"citation_page_number\" doris:\"INT\"`",
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
}

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
			name:      "inline_nested on scalar string field",
			tOpts:     &proto2typepb.TableOptions{TableName: "patients"},
			cOpts:     &proto2typepb.ColumnOptions{InlineNested: true},
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
}

func TestGenerateGoDoris_CompileAndRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("protoc"); err != nil {
		t.Skip("protoc not found in PATH; skipping compile-and-run test")
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

message AuditEventRecord {
  string tenant_id = 1;
  string event_id = 2;
  google.type.Date event_date = 3;
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
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	date "google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
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

	// 4. Test record_status default to "ACTIVE" when UNSPECIFIED on proto
	unspecStatus := &EligibilityCriterionResult{
		PatientId: "pat-4004",
	}
	var unspecRow EligibilityCriterionResultDoris
	unspecRow.FromProto(unspecStatus)
	if unspecRow.RecordStatus != "ACTIVE" {
		t.Fatalf("expected unset RecordStatus to default to ACTIVE in FromProto, got %q", unspecRow.RecordStatus)
	}

	// 5. Test unknown string on optional enum remains nil in ToProto
	unknownEnum := "BOGUS_UNKNOWN_STATUS"
	unspecRow.OverrideStatus = &unknownEnum
	if p := unspecRow.ToProto(); p.OverrideStatus != nil {
		t.Fatalf("expected unknown OverrideStatus string to remain nil in ToProto, got %v", *p.OverrideStatus)
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

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
