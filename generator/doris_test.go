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
	}))

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
		},
	}

	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"test_opts.proto"})
	nativeMsg := findMessage(t, gen, "NativeRecord")
	wireMsg := findMessage(t, gen, "WireRecord")

	tMeta1 := extractDorisTableMeta(nativeMsg)
	if tMeta1 == nil {
		t.Fatal("extractDorisTableMeta(NativeRecord) returned nil")
	}
	if tMeta1.TableName != "patients" || tMeta1.DatabaseScope != "TENANT" || tMeta1.SyncTier != "WHOLE_TABLE" {
		t.Errorf("unexpected NativeRecord table meta: %+v", tMeta1)
	}
	if len(tMeta1.UniqueKeyColumns) != 1 || tMeta1.UniqueKeyColumns[0] != "patient_id" {
		t.Errorf("unexpected UniqueKeyColumns: %v", tMeta1.UniqueKeyColumns)
	}

	cMeta1 := extractDorisColumnMeta(nativeMsg.Fields[0])
	if cMeta1 == nil {
		t.Fatal("extractDorisColumnMeta(NativeRecord.raw_site_term) returned nil")
	}
	if cMeta1.ColumnName != "raw_site_token" || !cMeta1.IsUniqueKey || !cMeta1.InvertedIndex || !cMeta1.StripEnumPrefix {
		t.Errorf("unexpected NativeRecord column meta: %+v", cMeta1)
	}

	tMeta2 := extractDorisTableMeta(wireMsg)
	if tMeta2 == nil {
		t.Fatal("extractDorisTableMeta(WireRecord) returned nil")
	}
	if tMeta2.TableName != "snomed_concepts" || tMeta2.DatabaseScope != "GLOBAL_TERMINOLOGY" || tMeta2.SyncTier != "RELEASE_PARTITIONED" {
		t.Errorf("unexpected WireRecord table meta: %+v", tMeta2)
	}
	if len(tMeta2.PartitionByColumns) != 1 || tMeta2.PartitionByColumns[0] != "release_id" {
		t.Errorf("unexpected PartitionByColumns: %v", tMeta2.PartitionByColumns)
	}

	cMeta2 := extractDorisColumnMeta(wireMsg.Fields[0])
	if cMeta2 == nil {
		t.Fatal("extractDorisColumnMeta(WireRecord.release_id) returned nil")
	}
	if cMeta2.DorisType != "VARCHAR(64)" || !cMeta2.IsPartitionKey {
		t.Errorf("unexpected WireRecord column meta: %+v", cMeta2)
	}
}

// buildComprehensiveDorisPlugin creates a FileDescriptorSet containing:
//   - google/protobuf/timestamp.proto
//   - google/type/date.proto
//   - clinical_lakehouse.proto (with EligibilityCriterionResult, inline_nested SkillProvenance & Citation,
//     enums with prefix stripping, column_name override, Date, Timestamp, repeated string, repeated float,
//     optional fields, plus a SnomedConceptRecord in GLOBAL_TERMINOLOGY and an INTERNAL_ONLY rollup table)
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
		},
		MessageType: []*descriptorpb.DescriptorProto{
			// Inline nested helper message (no table_meta of its own)
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
						Name:     proto.String("citation_bbox_coords"),
						Number:   proto.Int32(3),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_FLOAT.Enum(),
						JsonName: proto.String("citationBboxCoords"),
						Options:  colOpt(DorisColumnMetaIR{DorisType: "ARRAY<FLOAT>", IcebergType: "LIST<FLOAT>"}),
					},
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: proto.String("_skill_name")},
					{Name: proto.String("_skill_confidence")},
				},
			},
			// Tenant table with scalars, optional scalars, Date, Timestamp, repeated string, repeated float,
			// strip_enum_prefix enums, column_name override, and inline_nested SkillProvenance
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
						Options:  colOpt(DorisColumnMetaIR{DorisType: "VARCHAR(32)", IcebergType: "STRING", InvertedIndex: true, StripEnumPrefix: true}),
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
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: proto.String("_observation_age_days")},
					{Name: proto.String("_override_status")},
				},
			},
			// Global terminology table (RELEASE_PARTITIONED)
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
			// Internal-only rollup table (INTERNAL_ONLY -> omitted from Iceberg DDL)
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
		File: []*descriptorpb.FileDescriptorProto{tsFDP, dateFDP, clinicalFDP, nonTableFDP},
	}
	return newPlugin(t, fds, []string{"clinical_lakehouse.proto", "non_table.proto"})
}

func TestGenerateGoDoris_CodegenAndDDL(t *testing.T) {
	gen := buildComprehensiveDorisPlugin(t)
	runner := NewRunner()
	opts := &Options{
		Lang:      "go",
		Backend:   "doris",
		DorisMode: "both",
		Domain:    false,
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
	}

	goCode, ok := filesByName["clinical_lakehouse_doris.type.go"]
	if !ok {
		t.Fatalf("missing clinical_lakehouse_doris.type.go in output (got %v)", keysOf(filesByName))
	}
	dorisSQL, ok := filesByName["clinical_lakehouse_doris.sql"]
	if !ok {
		t.Fatalf("missing clinical_lakehouse_doris.sql in output")
	}
	icebergSQL, ok := filesByName["clinical_lakehouse_iceberg.sql"]
	if !ok {
		t.Fatalf("missing clinical_lakehouse_iceberg.sql in output")
	}

	// Verify Go codegen contents:
	// - SkillProvenance has no table_meta so it must NOT have SkillProvenanceDoris,
	//   instead its fields are flattened inline into EligibilityCriterionResultDoris.
	if strings.Contains(goCode, "type SkillProvenanceDoris struct") {
		t.Error("SkillProvenance has no table_meta and should not generate SkillProvenanceDoris")
	}
	wantGoSnippets := []string{
		"type DorisScanner interface",
		"type DorisStringArray []string",
		"type DorisFloat32Array []float32",
		"func EligibilityCriterionResultDorisTable() DorisTableDescriptor",
		"type EligibilityCriterionResultDoris struct",
		"SubwordCount int32 `db:\"token_count\" doris:\"INT\"`",
		"SkillName *string `db:\"skill_name\" doris:\"VARCHAR(128)\"`",
		"SkillConfidence *float64 `db:\"skill_confidence\" doris:\"DECIMALV3(5,4)\"`",
		"CitationBboxCoords DorisFloat32Array `db:\"citation_bbox_coords\" doris:\"ARRAY<FLOAT>\"`",
		"AllMrns DorisStringArray `db:\"all_mrns\" doris:\"ARRAY<VARCHAR(64)>\"`",
		"EventMonth string `db:\"event_month\" doris:\"DATEV2\"`",
		"DateOfBirth string `db:\"date_of_birth\" doris:\"DATEV2\"`",
		"VerifiedAt time.Time `db:\"verified_at\" doris:\"DATETIMEV2(6)\"`",
		"func (r *EligibilityCriterionResultDoris) Values() []any",
		"func (r *EligibilityCriterionResultDoris) ScanRow(scanner DorisScanner) error",
		"func (r *EligibilityCriterionResultDoris) ToProto() *EligibilityCriterionResult",
		"func (r *EligibilityCriterionResultDoris) FromProto(m *EligibilityCriterionResult)",
		"func ScanEligibilityCriterionResultProto(scanner DorisScanner) (*EligibilityCriterionResult, error)",
		"func EligibilityCriterionResultProtoValues(m *EligibilityCriterionResult) []any",
		"var ClinicalLakehouseDorisTables = []DorisTableDescriptor{",
	}
	normGoCode := strings.Join(strings.Fields(goCode), " ")
	for _, snippet := range wantGoSnippets {
		if !strings.Contains(normGoCode, snippet) {
			t.Errorf("clinical_lakehouse_doris.type.go missing expected snippet:\n  %s", snippet)
		}
	}

	// Verify Doris DDL contents:
	wantDorisSQL := []string{
		"CREATE TABLE IF NOT EXISTS internal.tenant_${TENANT_SLUG}.eligibility_criterion_results (",
		"token_count INT NOT NULL",
		"skill_name VARCHAR(128)",
		"skill_confidence DECIMALV3(5,4)",
		"citation_bbox_coords ARRAY<FLOAT> NOT NULL DEFAULT \"[]\"",
		"INDEX idx_eligibility_criterion_results_all_mrns (all_mrns) USING INVERTED",
		"INDEX idx_eligibility_criterion_results_effective_status (effective_status) USING INVERTED",
		"UNIQUE KEY(patient_id, criterion_id)",
		"DISTRIBUTED BY HASH(patient_id) BUCKETS ${TENANT_BUCKETS}",
		`"function_column.sequence_col" = "publication_generation"`,
		`"colocate_with" = "azra_patient_group"`,
		"CREATE TABLE IF NOT EXISTS internal.terminology.snomed_concepts (",
		"CREATE TABLE IF NOT EXISTS internal.terminology.anatomic_site_normalization (",
	}
	for _, snippet := range wantDorisSQL {
		if !strings.Contains(dorisSQL, snippet) {
			t.Errorf("clinical_lakehouse_doris.sql missing expected snippet:\n  %s\nFull SQL:\n%s", snippet, dorisSQL)
		}
	}

	// Verify Iceberg DDL contents:
	wantIcebergSQL := []string{
		"CREATE TABLE IF NOT EXISTS azra_lake.tenant_${TENANT_SLUG}.eligibility_criterion_results (",
		"event_month DATE NOT NULL",
		"verified_at DATETIME",
		"skill_confidence DECIMAL(5,4)",
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
		Domain:    false,
	}

	for _, f := range gen.Files {
		if f.Generate && f.Desc.Path() == "clinical_lakehouse.proto" {
			if err := runner.GenerateFile(gen, f, opts); err != nil {
				t.Fatalf("GenerateFile failed: %v", err)
			}
		}
	}

	dorisGoCode := extractPluginOutput(t, gen, "_doris.type.go")
	if dorisGoCode == "" {
		t.Fatal("no _doris.type.go generated")
	}

	// Create a temporary package inside the repository module so it shares go.mod dependencies.
	tmpPkgDir, err := os.MkdirTemp(".", "doris_compile_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(tmpPkgDir)

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

message SkillProvenance {
  optional string skill_name = 1;
  optional double skill_confidence = 2;
  repeated float citation_bbox_coords = 3;
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
}

message SnomedConceptRecord {
  string id = 1;
  string release_id = 2;
}

message AnatomicSiteNormalizationRecord {
  string raw_site_term = 1;
}
`
	protoPath := filepath.Join(tmpPkgDir, "clinical_lakehouse.proto")
	if err := os.WriteFile(protoPath, []byte(protoSrc), 0o644); err != nil {
		t.Fatalf("write proto: %v", err)
	}

	// Locate google/type/date.proto from platform/proto or write a minimal copy in tmpPkgDir.
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
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("protoc failed: %v\n%s", err, out)
	}

	if err := os.WriteFile(filepath.Join(tmpPkgDir, "clinical_lakehouse_doris.type.go"), []byte(dorisGoCode), 0o644); err != nil {
		t.Fatalf("write generated doris file: %v", err)
	}

	roundTripTestSrc := `package clinicalv1

import (
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
	verifiedTime := time.Date(2026, 10, 21, 15, 30, 45, 123456000, time.UTC)

	orig := &EligibilityCriterionResult{
		PatientId:          "pat-1001",
		CriterionId:        "crit-fev1",
		DisplayOrder:       2,
		ObservationAgeDays: &obsAge,
		IsDealbreaker:      true,
		AllMrns:            []string{"MRN-1", "MRN-2"},
		EffectiveStatus:    CriterionAiStatus_CRITERION_AI_STATUS_MET,
		OverrideStatus:     &overrideStat,
		SubwordCount:       42,
		EventMonth:         &date.Date{Year: 2026, Month: 10, Day: 1},
		DateOfBirth:        &date.Date{Year: 1958, Month: 4, Day: 19},
		VerifiedAt:         timestamppb.New(verifiedTime),
		SkillProvenance: &SkillProvenance{
			SkillName:          &skillName,
			SkillConfidence:    &skillConf,
			CitationBboxCoords: []float32{0.12, 0.34, 0.56, 0.78},
		},
		RecordStatus:          ClinicalRecordStatus_CLINICAL_RECORD_STATUS_ACTIVE,
		PublicationGeneration: 99,
	}

	// 1. FromProto -> Values -> ScanRow -> ToProto
	var row EligibilityCriterionResultDoris
	row.FromProto(orig)

	if row.TableName() != "eligibility_criterion_results" {
		t.Fatalf("TableName() = %q, want eligibility_criterion_results", row.TableName())
	}
	if row.EffectiveStatus != "MET" {
		t.Fatalf("EffectiveStatus = %q, want prefix-stripped MET", row.EffectiveStatus)
	}
	if row.OverrideStatus == nil || *row.OverrideStatus != "EXCLUDED" {
		t.Fatalf("OverrideStatus = %v, want prefix-stripped EXCLUDED", row.OverrideStatus)
	}
	if row.RecordStatus != "ACTIVE" {
		t.Fatalf("RecordStatus = %q, want prefix-stripped ACTIVE", row.RecordStatus)
	}
	if row.EventMonth != "2026-10-01" || row.DateOfBirth != "1958-04-19" {
		t.Fatalf("unexpected dates: EventMonth=%q, DateOfBirth=%q", row.EventMonth, row.DateOfBirth)
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

	// 2. Test NULL optional fields, unset Date/Timestamp, and nil inline_nested SkillProvenance
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

	// 3. Test scanning string/[]byte formatted dates and timestamps from MySQL wire driver
	minVals[10] = []byte("1970-12-31")                  // DateOfBirth as []byte
	minVals[11] = "2026-10-21 15:30:45.123456"          // VerifiedAt as MySQL DATETIME string
	gotStrTime, err := ScanEligibilityCriterionResultProto(NewDorisValueScanner(minVals))
	if err != nil {
		t.Fatalf("Scan with string/[]byte date/time failed: %v", err)
	}
	if gotStrTime.GetDateOfBirth().GetYear() != 1970 || gotStrTime.GetDateOfBirth().GetMonth() != 12 || gotStrTime.GetDateOfBirth().GetDay() != 31 {
		t.Fatalf("unexpected DateOfBirth from []byte: %+v", gotStrTime.GetDateOfBirth())
	}
	if !gotStrTime.GetVerifiedAt().AsTime().Equal(verifiedTime) {
		t.Fatalf("unexpected VerifiedAt from string: got %v, want %v", gotStrTime.GetVerifiedAt().AsTime(), verifiedTime)
	}

	// 4. Verify runtime DDL builders on EligibilityCriterionResultDorisTable()
	tblDesc := EligibilityCriterionResultDorisTable()
	dorisDDL := tblDesc.BuildDorisCreateTableSQL("akron_general", 4)
	if !strings.Contains(dorisDDL, "CREATE TABLE IF NOT EXISTS internal.tenant_akron_general.eligibility_criterion_results") ||
		!strings.Contains(dorisDDL, "BUCKETS 4") {
		t.Fatalf("unexpected runtime Doris DDL:\n%s", dorisDDL)
	}
	iceDDL := tblDesc.BuildIcebergCreateTableSQL("akron_general")
	if !strings.Contains(iceDDL, "CREATE TABLE IF NOT EXISTS azra_lake.tenant_akron_general.eligibility_criterion_results") {
		t.Fatalf("unexpected runtime Iceberg DDL:\n%s", iceDDL)
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
