package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	proto2typepb "github.com/protocgen/proto2type/proto/proto2type"
)

// buildLegacy50201WithWriterWire encodes a legacy extension 50201 (candela.types.TableMeta)
// payload with writer_service at tag 14 and reader_services at tag 15.
func buildLegacy50201WithWriterWire(tableName string, writerService string, readerServices []string, uks []string) []byte {
	var b []byte
	if tableName != "" {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, tableName)
	}
	for _, uk := range uks {
		b = protowire.AppendTag(b, 4, protowire.BytesType)
		b = protowire.AppendString(b, uk)
	}
	if writerService != "" {
		b = protowire.AppendTag(b, 14, protowire.BytesType)
		b = protowire.AppendString(b, writerService)
	}
	for _, rs := range readerServices {
		b = protowire.AppendTag(b, 15, protowire.BytesType)
		b = protowire.AppendString(b, rs)
	}
	return buildWireExtension(legacyTableMetaExtensionNumber, b)
}

// build1307TableOptionsWire encodes an extension 1307 (proto2type.MessageOptions.table)
// payload on raw wire bytes with storage_vault_name=14, iceberg_catalog=15,
// writer_service=17, reader_services=18.
func build1307TableOptionsWire(tableName, vault, catalog, writerService string, readerServices []string, uks []string) []byte {
	var t []byte
	if tableName != "" {
		t = protowire.AppendTag(t, 1, protowire.BytesType)
		t = protowire.AppendString(t, tableName)
	}
	for _, uk := range uks {
		t = protowire.AppendTag(t, 4, protowire.BytesType)
		t = protowire.AppendString(t, uk)
	}
	if vault != "" {
		t = protowire.AppendTag(t, 14, protowire.BytesType)
		t = protowire.AppendString(t, vault)
	}
	if catalog != "" {
		t = protowire.AppendTag(t, 15, protowire.BytesType)
		t = protowire.AppendString(t, catalog)
	}
	if writerService != "" {
		t = protowire.AppendTag(t, 17, protowire.BytesType)
		t = protowire.AppendString(t, writerService)
	}
	for _, rs := range readerServices {
		t = protowire.AppendTag(t, 18, protowire.BytesType)
		t = protowire.AppendString(t, rs)
	}
	var m []byte
	m = protowire.AppendTag(m, 2, protowire.BytesType)
	m = protowire.AppendBytes(m, t)
	return buildWireExtension(proto2typeExtensionNumber, m)
}

// TestWriterAndReaderServices_1307And50201AndMerge verifies extraction and merging of
// writer_service and reader_services from extension 1307 and legacy extension 50201.
func TestWriterAndReaderServices_1307And50201AndMerge(t *testing.T) {
	// 1. Typed extension 1307 with writer_service (tag 17) and reader_services (tag 18)
	msgOpts1307 := &descriptorpb.MessageOptions{}
	proto.SetExtension(msgOpts1307, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			TableName:        "patient_encounters",
			StorageVaultName: "s3_vault",
			IcebergCatalog:   "lakehouse_cat",
			WriterService:    " patient-service ",
			ReaderServices:   []string{" analytics-service ", "", "reporting-service"},
		},
	})

	// 2. Legacy extension 50201 wire decoding with writer_service at tag 14 and reader_services at tag 15
	msgOpts50201 := &descriptorpb.MessageOptions{}
	msgOpts50201.ProtoReflect().SetUnknown(buildLegacy50201WithWriterWire(
		"",
		"order-service",
		[]string{"billing-service", "*"},
		[]string{"order_id"},
	))

	// 3. Combined 50201 + 1307 on the same message (verifying merge without duplicate slices)
	msgOptsMerged := &descriptorpb.MessageOptions{}
	proto.SetExtension(msgOptsMerged, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			StorageVaultName: "prod_vault",
			IcebergCatalog:   "prod_iceberg",
			UniqueKeyColumns: []string{"tenant_id", "claim_id"},
			ReaderServices:   []string{"analytics-service", "audit-service"},
		},
	})
	msgOptsMerged.ProtoReflect().SetUnknown(buildLegacy50201WithWriterWire(
		"claims",
		"claims-service",
		[]string{"analytics-service", "billing-service"},
		[]string{"tenant_id", "claim_id"},
	))

	fdp := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("service_meta.proto"),
		Package: proto.String("test.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/test/v1;testv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("EncounterRecord"),
				Options: msgOpts1307,
			},
			{
				Name:    proto.String("OrderRecord"),
				Options: msgOpts50201,
			},
			{
				Name:    proto.String("ClaimRecord"),
				Options: msgOptsMerged,
			},
		},
	}

	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"service_meta.proto"})

	encMsg := findMessage(t, gen, "EncounterRecord")
	meta1307 := extractTableMeta(encMsg)
	if meta1307 == nil {
		t.Fatal("extractTableMeta(EncounterRecord) returned nil")
	}
	if meta1307.WriterService != "patient-service" {
		t.Errorf("meta1307.WriterService = %q, want %q", meta1307.WriterService, "patient-service")
	}
	wantReaders1307 := []string{"analytics-service", "reporting-service"}
	if !reflect.DeepEqual(meta1307.ReaderServices, wantReaders1307) {
		t.Errorf("meta1307.ReaderServices = %v, want %v", meta1307.ReaderServices, wantReaders1307)
	}
	if meta1307.StorageVaultName != "s3_vault" || meta1307.IcebergCatalog != "lakehouse_cat" {
		t.Errorf("meta1307 vault/catalog = (%q, %q), want (s3_vault, lakehouse_cat)", meta1307.StorageVaultName, meta1307.IcebergCatalog)
	}

	ordMsg := findMessage(t, gen, "OrderRecord")
	meta50201 := extractTableMeta(ordMsg)
	if meta50201 == nil {
		t.Fatal("extractTableMeta(OrderRecord) returned nil")
	}
	if meta50201.WriterService != "order-service" {
		t.Errorf("meta50201.WriterService = %q, want %q", meta50201.WriterService, "order-service")
	}
	wantReaders50201 := []string{"billing-service", "*"}
	if !reflect.DeepEqual(meta50201.ReaderServices, wantReaders50201) {
		t.Errorf("meta50201.ReaderServices = %v, want %v", meta50201.ReaderServices, wantReaders50201)
	}
	if meta50201.StorageVaultName != "" || meta50201.IcebergCatalog != "" {
		t.Errorf("meta50201 should not populate vault/catalog from tags 14/15: %+v", meta50201)
	}

	claimMsg := findMessage(t, gen, "ClaimRecord")
	metaMerged := extractTableMeta(claimMsg)
	if metaMerged == nil {
		t.Fatal("extractTableMeta(ClaimRecord) returned nil")
	}
	if metaMerged.TableName != "claims" {
		t.Errorf("metaMerged.TableName = %q, want %q", metaMerged.TableName, "claims")
	}
	if metaMerged.WriterService != "claims-service" {
		t.Errorf("metaMerged.WriterService = %q, want %q", metaMerged.WriterService, "claims-service")
	}
	if metaMerged.StorageVaultName != "prod_vault" || metaMerged.IcebergCatalog != "prod_iceberg" {
		t.Errorf("metaMerged vault/catalog = (%q, %q)", metaMerged.StorageVaultName, metaMerged.IcebergCatalog)
	}
	wantMergedReaders := []string{"analytics-service", "audit-service", "billing-service"}
	if !reflect.DeepEqual(metaMerged.ReaderServices, wantMergedReaders) {
		t.Errorf("metaMerged.ReaderServices = %v, want %v", metaMerged.ReaderServices, wantMergedReaders)
	}
	wantMergedUKs := []string{"tenant_id", "claim_id"}
	if !reflect.DeepEqual(metaMerged.UniqueKeyColumns, wantMergedUKs) {
		t.Errorf("metaMerged.UniqueKeyColumns = %v, want %v", metaMerged.UniqueKeyColumns, wantMergedUKs)
	}

	// 4. Also test raw wire merge when both 50201 and 1307 are present on wire bytes
	var combinedWire []byte
	combinedWire = append(combinedWire, buildLegacy50201WithWriterWire("wire_claims", "wire-writer", []string{"svc-a", "svc-b"}, []string{"id"})...)
	combinedWire = append(combinedWire, build1307TableOptionsWire("", "wire_vault", "wire_cat", "", []string{"svc-b", "svc-c"}, []string{"id"})...)
	wireMerged := parseDorisTableMetaFromOptionsWire(combinedWire)
	if wireMerged == nil {
		t.Fatal("parseDorisTableMetaFromOptionsWire(combinedWire) returned nil")
	}
	if wireMerged.TableName != "wire_claims" || wireMerged.WriterService != "wire-writer" || wireMerged.StorageVaultName != "wire_vault" || wireMerged.IcebergCatalog != "wire_cat" {
		t.Errorf("unexpected wireMerged scalar fields: %+v", wireMerged)
	}
	wantWireReaders := []string{"svc-b", "svc-c", "svc-a"}
	if !reflect.DeepEqual(wireMerged.ReaderServices, wantWireReaders) {
		t.Errorf("wireMerged.ReaderServices = %v, want %v", wireMerged.ReaderServices, wantWireReaders)
	}
}

// TestPostgresTableNameResolution verifies physical PostgreSQL table name resolution rules.
func TestPostgresTableNameResolution(t *testing.T) {
	// 1. Default snake_case when neither table_name nor writer_service is set
	if got := PostgresPrefixedTableName(nil, "patient_record"); got != "patient_record" {
		t.Errorf("got %q, want patient_record", got)
	}
	if got := PostgresPrefixedTableName(&TableMetaConfig{}, "patient_record"); got != "patient_record" {
		t.Errorf("got %q, want patient_record", got)
	}

	// 2. Prefixed <norm_writer>__<snake_case> when writer_service is set and table_name is empty
	metaPrefixed := &TableMetaConfig{WriterService: "patient-service"}
	if got := PostgresPrefixedTableName(metaPrefixed, "patient_record"); got != "patient_service__patient_record" {
		t.Errorf("got %q, want patient_service__patient_record", got)
	}

	// 3. No double-prefixing if defaultName already starts with <norm_writer>__
	if got := PostgresPrefixedTableName(metaPrefixed, "patient_service__patient_record"); got != "patient_service__patient_record" {
		t.Errorf("got %q, want patient_service__patient_record", got)
	}

	// 4. Explicit table_name override takes precedence over writer_service prefixing
	metaExplicit := &TableMetaConfig{
		TableName:     "custom_patients",
		WriterService: "patient-service",
	}
	if got := PostgresPrefixedTableName(metaExplicit, "patient_record"); got != "custom_patients" {
		t.Errorf("got %q, want custom_patients", got)
	}
}

// TestServiceIdentifierValidation verifies service identifier validation across table metadata and backends.
func TestServiceIdentifierValidation(t *testing.T) {
	validServices := []string{"a", "patient-service", "billing_svc_2", "core-v1"}
	for _, s := range validServices {
		if !isValidServiceIdentifier(s) {
			t.Errorf("expected %q to be a valid service identifier", s)
		}
	}

	invalidServices := []string{
		"",
		"Patient-Service",
		"123-service",
		"-service",
		"_service",
		"patient service",
		"patient;DROP TABLE x",
		"patient'service",
		"*",
	}
	for _, s := range invalidServices {
		if isValidServiceIdentifier(s) {
			t.Errorf("expected %q to be rejected as invalid service identifier", s)
		}
	}

	// Verify validateTableServiceMeta accepts "*" in ReaderServices but rejects invalid entries
	if err := validateTableServiceMeta("test.Patient", &TableMetaConfig{
		WriterService:  "patient-service",
		ReaderServices: []string{"*", "analytics-service"},
	}); err != nil {
		t.Errorf("expected valid service meta, got error: %v", err)
	}

	if err := validateTableServiceMeta("test.Patient", &TableMetaConfig{
		WriterService: "Invalid_Upper",
	}); err == nil {
		t.Error("expected error for uppercase writer_service")
	}

	if err := validateTableServiceMeta("test.Patient", &TableMetaConfig{
		WriterService:  "patient-service",
		ReaderServices: []string{"analytics-service", "bad;service"},
	}); err == nil {
		t.Error("expected error for invalid reader_services entry")
	}

	// End-to-end Runner.GenerateFile test with Backend: "doris" and invalid writer_service
	badDorisOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(badDorisOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			TableName:        "patient_encounters",
			DatabaseScope:    "TENANT",
			SyncTier:         "WHOLE_TABLE",
			UniqueKeyColumns: []string{"id"},
			WriterService:    "Invalid_Upper",
		},
	})
	badDorisFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("bad_doris.proto"),
		Package: proto.String("test.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/test/v1;testv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("BadDorisTable"),
				Options: badDorisOpts,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("id"),
					},
				},
			},
		},
	}
	genBadDoris := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{badDorisFDP}}, []string{"bad_doris.proto"})
	runnerBadDoris := NewRunner()
	if err := runnerBadDoris.GenerateFile(genBadDoris, genBadDoris.Files[0], &Options{
		Lang:    "go",
		Backend: "doris",
		Domain:  false,
	}); err == nil || !strings.Contains(err.Error(), "invalid writer_service") {
		t.Errorf("expected Doris GenerateFile error for invalid writer_service, got %v", err)
	}
}

// TestDorisAndPostgresDescriptorsAndComments verifies Doris and Postgres table descriptors and DDL/HCL comments.
func TestDorisAndPostgresDescriptorsAndComments(t *testing.T) {
	msgOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(msgOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			TableName:        "patient_encounters",
			DatabaseScope:    "TENANT",
			SyncTier:         "WHOLE_TABLE",
			UniqueKeyColumns: []string{"encounter_id"},
			WriterService:    "patient-service",
			ReaderServices:   []string{"analytics-service", "reporting-service"},
		},
	})

	msgOptsPrefix := &descriptorpb.MessageOptions{}
	proto.SetExtension(msgOptsPrefix, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			WriterService:  "care-plan-service",
			ReaderServices: []string{"*"},
		},
	})

	fdp := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("clinical.proto"),
		Package: proto.String("clinical.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/clinical/v1;clinicalv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("PatientEncounter"),
				Options: msgOpts,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("encounter_id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("encounterId"),
					},
					{
						Name:     proto.String("patient_id"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("patientId"),
					},
				},
			},
			{
				Name:    proto.String("CarePlan"),
				Options: msgOptsPrefix,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("id"),
					},
				},
			},
		},
	}

	// 1. Verify Doris (doris_mode=all) emits WriterService, ReaderServices, and HCL/SQL comments,
	// and cleanly skips Postgres-only CarePlan (which has writer_service without Doris table_name).
	genDoris := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"clinical.proto"})
	runnerDoris := NewRunner()
	optsDoris := &Options{
		Lang:      "go",
		Backend:   "doris",
		DorisMode: "all",
		Domain:    false,
	}
	if err := runnerDoris.GenerateFile(genDoris, genDoris.Files[0], optsDoris); err != nil {
		t.Fatalf("Doris GenerateFile failed: %v", err)
	}

	dorisFiles := make(map[string]string)
	for _, f := range genDoris.Response().File {
		dorisFiles[filepath.Base(f.GetName())] = f.GetContent()
	}

	dorisGo := dorisFiles["clinical_doris.type.go"]
	if !strings.Contains(dorisGo, `WriterService:        "patient-service"`) {
		t.Errorf("expected WriterService in clinical_doris.type.go:\n%s", dorisGo)
	}
	if !strings.Contains(dorisGo, `ReaderServices:       []string{"analytics-service", "reporting-service"}`) {
		t.Errorf("expected ReaderServices in clinical_doris.type.go:\n%s", dorisGo)
	}

	for _, sqlFile := range []string{"clinical_doris.sql", "clinical_iceberg.sql", "clinical_starrocks.sql"} {
		content := dorisFiles[sqlFile]
		if !strings.Contains(content, "-- writer_service: patient-service") {
			t.Errorf("expected '-- writer_service: patient-service' in %s:\n%s", sqlFile, content)
		}
		if !strings.Contains(content, "-- reader_services: analytics-service, reporting-service") {
			t.Errorf("expected '-- reader_services: ...' in %s:\n%s", sqlFile, content)
		}
	}

	for _, hclFile := range []string{"clinical_doris.hcl", "clinical_iceberg.hcl", "clinical_starrocks.hcl"} {
		content := dorisFiles[hclFile]
		if !strings.Contains(content, "# writer_service: patient-service") {
			t.Errorf("expected '# writer_service: patient-service' in %s:\n%s", hclFile, content)
		}
		if !strings.Contains(content, "# reader_services: analytics-service, reporting-service") {
			t.Errorf("expected '# reader_services: ...' in %s:\n%s", hclFile, content)
		}
	}

	// 2. Verify Postgres (postgres_mode=both) emits PostgresTableDescriptor with explicit & prefixed table names
	genPG := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"clinical.proto"})
	runnerPG := NewRunner()
	optsPG := &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "both",
		Domain:       false,
	}
	if err := runnerPG.GenerateFile(genPG, genPG.Files[0], optsPG); err != nil {
		t.Fatalf("Postgres GenerateFile failed: %v", err)
	}

	pgFiles := make(map[string]string)
	for _, f := range genPG.Response().File {
		pgFiles[filepath.Base(f.GetName())] = f.GetContent()
	}

	pgGo := pgFiles["clinical_postgres.type.go"]
	if !strings.Contains(pgGo, "type PostgresTableDescriptor struct") {
		t.Errorf("expected PostgresTableDescriptor struct in clinical_postgres.type.go:\n%s", pgGo)
	}
	if !strings.Contains(pgGo, "func (x *PatientEncounterPostgres) PostgresTableDescriptor() PostgresTableDescriptor") {
		t.Errorf("expected PatientEncounterPostgres.PostgresTableDescriptor() in clinical_postgres.type.go:\n%s", pgGo)
	}
	if !strings.Contains(pgGo, `TableName:      "patient_encounters"`) {
		t.Errorf("expected TableName: \"patient_encounters\" for PatientEncounterPostgres:\n%s", pgGo)
	}
	if !strings.Contains(pgGo, "func (x *CarePlanPostgres) PostgresTableDescriptor() PostgresTableDescriptor") {
		t.Errorf("expected CarePlanPostgres.PostgresTableDescriptor() in clinical_postgres.type.go:\n%s", pgGo)
	}
	if !strings.Contains(pgGo, `TableName:      "care_plan_service__care_plan"`) {
		t.Errorf("expected prefixed TableName: \"care_plan_service__care_plan\" for CarePlanPostgres:\n%s", pgGo)
	}
	if !strings.Contains(pgGo, `WriterService:  "care-plan-service"`) {
		t.Errorf("expected WriterService: \"care-plan-service\" for CarePlanPostgres:\n%s", pgGo)
	}
	if !strings.Contains(pgGo, `ReaderServices: []string{"*"}`) {
		t.Errorf("expected ReaderServices: []string{\"*\"} for CarePlanPostgres:\n%s", pgGo)
	}
}

// TestPostgresModesAndComprehensiveHCLAndDDL verifies all postgres_mode values and rendered HCL/DDL output.
func TestPostgresModesAndComprehensiveHCLAndDDL(t *testing.T) {
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
				},
			},
		},
	}

	durFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("google/protobuf/duration.proto"),
		Package: proto.String("google.protobuf"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("google.golang.org/protobuf/types/known/durationpb"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Duration"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("seconds"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
						JsonName: proto.String("seconds"),
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
				},
			},
		},
	}

	wrappersFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("google/protobuf/wrappers.proto"),
		Package: proto.String("google.protobuf"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("google.golang.org/protobuf/types/known/wrapperspb"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("StringValue"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("value"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("value"),
					},
				},
			},
		},
	}

	tableOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(tableOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			WriterService:  "patient-service",
			ReaderServices: []string{"analytics-service", "*"},
			Indexes: []*proto2typepb.IndexDef{
				{
					Name:    "idx_patient_tenant_status",
					Columns: []string{"tenant_id", "status"},
				},
			},
		},
	})

	pkFieldOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(pkFieldOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		DocumentId: true,
		Column: &proto2typepb.ColumnOptions{
			SqlType: "UUID",
		},
	})

	fkFieldOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(fkFieldOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		Index: true,
		Column: &proto2typepb.ColumnOptions{
			Length: 64,
			ForeignKey: &proto2typepb.ForeignKey{
				RefTable:  "tenants",
				RefColumn: "id",
				OnDelete:  "CASCADE",
			},
		},
	})

	uqEmailOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(uqEmailOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		Unique: true,
		Column: &proto2typepb.ColumnOptions{
			Length: 255,
		},
	})

	statusEnumOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(statusEnumOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		EnumAsString: proto2typepb.OptionalBool_OPTIONAL_BOOL_TRUE,
		Column: &proto2typepb.ColumnOptions{
			DefaultValue: "ACTIVE",
		},
	})

	jsonbStringOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(jsonbStringOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		Column: &proto2typepb.ColumnOptions{
			Jsonb: true,
		},
	})

	serverTSOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(serverTSOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		ServerTimestamp: true,
	})

	mainFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("patient.proto"),
		Package: proto.String("patient.v1"),
		Dependency: []string{
			"google/protobuf/timestamp.proto",
			"google/protobuf/duration.proto",
			"google/type/date.proto",
			"google/protobuf/wrappers.proto",
		},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/patient/v1;patientv1"),
		},
		EnumType: []*descriptorpb.EnumDescriptorProto{
			{
				Name: proto.String("PatientStatus"),
				Value: []*descriptorpb.EnumValueDescriptorProto{
					{Name: proto.String("PATIENT_STATUS_UNSPECIFIED"), Number: proto.Int32(0)},
					{Name: proto.String("PATIENT_STATUS_ACTIVE"), Number: proto.Int32(1)},
				},
			},
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Address"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("city"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("city"),
					},
				},
			},
			{
				Name:    proto.String("PatientRecord"),
				Options: tableOpts,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("id"),
						Options:  pkFieldOpts,
					},
					{
						Name:     proto.String("tenant_id"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("tenantId"),
						Options:  fkFieldOpts,
					},
					{
						Name:     proto.String("email"),
						Number:   proto.Int32(3),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("email"),
						Options:  uqEmailOpts,
					},
					{
						Name:     proto.String("status"),
						Number:   proto.Int32(4),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(),
						TypeName: proto.String(".patient.v1.PatientStatus"),
						JsonName: proto.String("status"),
						Options:  statusEnumOpts,
					},
					{
						Name:     proto.String("age"),
						Number:   proto.Int32(5),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						JsonName: proto.String("age"),
					},
					{
						Name:     proto.String("visit_count"),
						Number:   proto.Int32(6),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
						JsonName: proto.String("visitCount"),
					},
					{
						Name:     proto.String("sequence_num"),
						Number:   proto.Int32(7),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_UINT64.Enum(),
						JsonName: proto.String("sequenceNum"),
					},
					{
						Name:     proto.String("risk_score"),
						Number:   proto.Int32(8),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum(),
						JsonName: proto.String("riskScore"),
					},
					{
						Name:     proto.String("active"),
						Number:   proto.Int32(9),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
						JsonName: proto.String("active"),
					},
					{
						Name:     proto.String("raw_payload"),
						Number:   proto.Int32(10),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
						JsonName: proto.String("rawPayload"),
					},
					{
						Name:     proto.String("fhir_bundle_json"),
						Number:   proto.Int32(11),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("fhirBundleJson"),
						Options:  jsonbStringOpts,
					},
					{
						Name:     proto.String("tags"),
						Number:   proto.Int32(12),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("tags"),
					},
					{
						Name:     proto.String("address"),
						Number:   proto.Int32(13),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".patient.v1.Address"),
						JsonName: proto.String("address"),
					},
					{
						Name:     proto.String("birth_date"),
						Number:   proto.Int32(14),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".google.type.Date"),
						JsonName: proto.String("birthDate"),
					},
					{
						Name:     proto.String("created_at"),
						Number:   proto.Int32(15),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".google.protobuf.Timestamp"),
						JsonName: proto.String("createdAt"),
						Options:  serverTSOpts,
					},
					{
						Name:     proto.String("processing_duration"),
						Number:   proto.Int32(16),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".google.protobuf.Duration"),
						JsonName: proto.String("processingDuration"),
					},
					{
						Name:     proto.String("nickname"),
						Number:   proto.Int32(17),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".google.protobuf.StringValue"),
						JsonName: proto.String("nickname"),
					},
				},
			},
		},
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{tsFDP, durFDP, dateFDP, wrappersFDP, mainFDP},
	}

	modeCases := []struct {
		mode    string
		wantORM bool
		wantHCL bool
		wantDDL bool
	}{
		{mode: "", wantORM: true, wantHCL: false, wantDDL: false},
		{mode: "orm", wantORM: true, wantHCL: false, wantDDL: false},
		{mode: "none", wantORM: false, wantHCL: false, wantDDL: false},
		{mode: "hcl", wantORM: false, wantHCL: true, wantDDL: false},
		{mode: "ddl", wantORM: false, wantHCL: false, wantDDL: true},
		{mode: "both", wantORM: true, wantHCL: true, wantDDL: true},
		{mode: "tables", wantORM: true, wantHCL: true, wantDDL: true},
		{mode: "all", wantORM: true, wantHCL: true, wantDDL: true},
	}

	for _, tc := range modeCases {
		t.Run("postgres_mode_"+tc.mode, func(t *testing.T) {
			gen := newPlugin(t, fds, []string{"patient.proto"})
			var targetFile *descriptorpb.FileDescriptorProto
			_ = targetFile
			runner := NewRunner()
			opts := &Options{
				Lang:         "go",
				Backend:      "postgres",
				PostgresMode: tc.mode,
				Domain:       false,
			}
			for _, f := range gen.Files {
				if f.Generate {
					if err := runner.GenerateFile(gen, f, opts); err != nil {
						t.Fatalf("GenerateFile(mode=%q) failed: %v", tc.mode, err)
					}
				}
			}

			gotFiles := make(map[string]string)
			for _, f := range gen.Response().File {
				gotFiles[filepath.Base(f.GetName())] = f.GetContent()
			}

			if _, ok := gotFiles["patient_postgres.type.go"]; ok != tc.wantORM {
				t.Errorf("mode %q: patient_postgres.type.go present=%v, want %v", tc.mode, ok, tc.wantORM)
			}
			if _, ok := gotFiles["patient_postgres.hcl"]; ok != tc.wantHCL {
				t.Errorf("mode %q: patient_postgres.hcl present=%v, want %v", tc.mode, ok, tc.wantHCL)
			}
			if _, ok := gotFiles["patient_postgres.sql"]; ok != tc.wantDDL {
				t.Errorf("mode %q: patient_postgres.sql present=%v, want %v", tc.mode, ok, tc.wantDDL)
			}

			if tc.wantHCL {
				hcl := gotFiles["patient_postgres.hcl"]
				wantHCLSnippets := []string{
					`schema "tenant" {}`,
					`# PatientRecord (patient_service__patient_record)`,
					`# writer_service: patient-service`,
					`# reader_services: analytics-service, *`,
					`table "patient_service__patient_record" {`,
					`  schema = schema.tenant`,
					"  column \"id\" {\n    null = false\n    type = uuid\n  }",
					"  column \"tenant_id\" {\n    null = false\n    type = varchar(64)\n  }",
					"  column \"email\" {\n    null = false\n    type = varchar(255)\n  }",
					"  column \"status\" {\n    null = false\n    type = text\n    default = \"ACTIVE\"\n  }",
					"  column \"age\" {\n    null = false\n    type = integer\n  }",
					"  column \"visit_count\" {\n    null = false\n    type = bigint\n  }",
					"  column \"sequence_num\" {\n    null = false\n    type = numeric(20,0)\n  }",
					"  column \"risk_score\" {\n    null = false\n    type = double_precision\n  }",
					"  column \"active\" {\n    null = false\n    type = boolean\n  }",
					"  column \"raw_payload\" {\n    null = false\n    type = bytea\n  }",
					"  column \"fhir_bundle_json\" {\n    null = false\n    type = jsonb\n  }",
					"  column \"tags\" {\n    null = false\n    type = sql(\"text[]\")\n  }",
					"  column \"address\" {\n    null = true\n    type = jsonb\n  }",
					"  column \"birth_date\" {\n    null = true\n    type = date\n  }",
					"  column \"created_at\" {\n    null = false\n    type = timestamptz\n    default = sql(\"now()\")\n  }",
					"  column \"processing_duration\" {\n    null = false\n    type = bigint\n  }",
					"  column \"nickname\" {\n    null = true\n    type = text\n  }",
					"  primary_key {\n    columns = [column.id]\n  }",
					"  index \"idx_patient_service__patient_record_tenant_id\" {\n    columns = [column.tenant_id]\n  }",
					"  index \"uq_patient_service__patient_record_email\" {\n    unique  = true\n    columns = [column.email]\n  }",
					"  index \"idx_patient_tenant_status\" {\n    columns = [column.tenant_id, column.status]\n  }",
					"  foreign_key \"fk_patient_service__patient_record_tenant_id\" {\n    columns     = [column.tenant_id]\n    ref_columns = [table.tenants.column.id]\n    on_delete   = CASCADE\n  }",
				}
				for _, snip := range wantHCLSnippets {
					if !strings.Contains(hcl, snip) {
						t.Errorf("HCL missing snippet %q:\n%s", snip, hcl)
					}
				}
				// Verify embedded Address sub-message is NOT emitted as its own top-level table in HCL
				if strings.Contains(hcl, `table "address"`) {
					t.Errorf("embedded message Address should not be emitted as a separate table in HCL:\n%s", hcl)
				}
			}

			if tc.wantDDL {
				ddl := gotFiles["patient_postgres.sql"]
				wantDDLSnippets := []string{
					`-- PatientRecord (patient_service__patient_record)`,
					`-- writer_service: patient-service`,
					`-- reader_services: analytics-service, *`,
					`CREATE TABLE IF NOT EXISTS "patient_service__patient_record" (`,
					`"id" UUID NOT NULL`,
					`"tenant_id" VARCHAR(64) NOT NULL`,
					`"email" VARCHAR(255) NOT NULL`,
					`"status" TEXT NOT NULL DEFAULT 'ACTIVE'`,
					`"age" INTEGER NOT NULL`,
					`"visit_count" BIGINT NOT NULL`,
					`"sequence_num" NUMERIC(20,0) NOT NULL`,
					`"risk_score" DOUBLE PRECISION NOT NULL`,
					`"active" BOOLEAN NOT NULL`,
					`"raw_payload" BYTEA NOT NULL`,
					`"fhir_bundle_json" JSONB NOT NULL`,
					`"tags" TEXT[] NOT NULL`,
					`"address" JSONB`,
					`"birth_date" DATE`,
					`"created_at" TIMESTAMPTZ NOT NULL DEFAULT NOW()`,
					`"processing_duration" BIGINT NOT NULL`,
					`"nickname" TEXT`,
					`PRIMARY KEY ("id")`,
					`ALTER TABLE "patient_service__patient_record" ADD CONSTRAINT "fk_patient_service__patient_record_tenant_id" FOREIGN KEY ("tenant_id") REFERENCES "tenants" ("id") ON DELETE CASCADE;`,
					`CREATE INDEX IF NOT EXISTS "idx_patient_service__patient_record_tenant_id" ON "patient_service__patient_record" ("tenant_id");`,
					`CREATE UNIQUE INDEX IF NOT EXISTS "uq_patient_service__patient_record_email" ON "patient_service__patient_record" ("email");`,
					`CREATE INDEX IF NOT EXISTS "idx_patient_tenant_status" ON "patient_service__patient_record" ("tenant_id", "status");`,
				}
				for _, snip := range wantDDLSnippets {
					if !strings.Contains(ddl, snip) {
						t.Errorf("DDL missing snippet %q:\n%s", snip, ddl)
					}
				}
			}
		})
	}

	// Verify Domain: true with postgres_mode=hcl still emits .type.go alongside _postgres.hcl
	genDomainHCL := newPlugin(t, fds, []string{"patient.proto"})
	runnerDomainHCL := NewRunner()
	if err := runnerDomainHCL.GenerateFile(genDomainHCL, genDomainHCL.Files[len(genDomainHCL.Files)-1], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "hcl",
		Domain:       true,
		GoPackage:    "example.com/patient/v1/domain;domain",
	}); err != nil {
		t.Fatalf("GenerateFile(Domain=true, mode=hcl) failed: %v", err)
	}
	domainHCLFiles := make(map[string]bool)
	for _, f := range genDomainHCL.Response().File {
		domainHCLFiles[filepath.Base(f.GetName())] = true
	}
	if !domainHCLFiles["patient.type.go"] || !domainHCLFiles["patient_postgres.hcl"] {
		t.Errorf("expected both patient.type.go and patient_postgres.hcl when Domain=true, got %v", domainHCLFiles)
	}

	// Verify unknown postgres_mode returns an error
	genInvalid := newPlugin(t, fds, []string{"patient.proto"})
	runnerInvalid := NewRunner()
	err := runnerInvalid.GenerateFile(genInvalid, genInvalid.Files[len(genInvalid.Files)-1], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "invalid_mode",
		Domain:       false,
	})
	if err == nil || !strings.Contains(err.Error(), "unknown postgres_mode") {
		t.Errorf("expected error for unknown postgres_mode, got %v", err)
	}
}

// TestPostgresGeneratedDescriptorCompiles verifies that generated PostgresTableDescriptor methods compile and execute cleanly.
func TestPostgresGeneratedDescriptorCompiles(t *testing.T) {
	msgOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(msgOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			WriterService:  "patient-service",
			ReaderServices: []string{"analytics-service", "*"},
		},
	})

	fdp := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("compile_pg.proto"),
		Package: proto.String("compilepg.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/compilepg/v1;compilepgv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("PatientRecord"),
				Options: msgOpts,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("patient_name"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("patientName"),
					},
				},
			},
		},
	}

	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"compile_pg.proto"})
	runner := NewRunner()
	if err := runner.GenerateFile(gen, gen.Files[0], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "orm",
		Domain:       false,
	}); err != nil {
		t.Fatalf("GenerateFile: %v", err)
	}

	var pgCode string
	for _, f := range gen.Response().File {
		if filepath.Base(f.GetName()) == "compile_pg_postgres.type.go" {
			pgCode = f.GetContent()
		}
	}
	if pgCode == "" {
		t.Fatal("compile_pg_postgres.type.go was not generated")
	}

	tmpDir, err := os.MkdirTemp(".", "pg_desc_compile_test_*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	pbStub := `package compilepgv1

type PatientRecord struct {
	PatientName string
}
`
	testFile := `package compilepgv1

import "testing"

func TestDescriptor(t *testing.T) {
	var m PatientRecordPostgres
	if got := m.TableName(); got != "patient_service__patient_record" {
		t.Fatalf("TableName() = %q, want patient_service__patient_record", got)
	}
	desc := m.PostgresTableDescriptor()
	if desc.TableName != "patient_service__patient_record" || desc.WriterService != "patient-service" || len(desc.ReaderServices) != 2 {
		t.Fatalf("unexpected descriptor: %+v", desc)
	}
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "compile_pg.pb.go"), []byte(pbStub), 0o644); err != nil {
		t.Fatalf("write pb stub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "compile_pg_postgres.type.go"), []byte(pgCode), 0o644); err != nil {
		t.Fatalf("write postgres type: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "compile_pg_test.go"), []byte(testFile), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	cmd := exec.Command("go", "test", "./"+filepath.Base(tmpDir))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compiled Postgres test failed: %v\n%s", err, string(out))
	}
}

// TestPostgresCodeRabbitReviewFixes covers edge cases in Doris/Postgres option separation,
// legacy 50200 + 1307 option merging, table name length and cross-file uniqueness,
// oneof variant column metadata, wrapper arrays, CHARACTER VARYING defaults,
// TIMESTAMP WITH TIME ZONE server defaults, nested tables, conflicting index names,
// and deferred ALTER TABLE foreign key constraints.
func TestPostgresCodeRabbitReviewFixes(t *testing.T) {
	// 1. Verify DorisColumnMetaIR.isEmpty() ignores Postgres-only fields while isAllEmpty() checks all fields
	pgOnlyCol := &DorisColumnMetaIR{SQLType: "UUID", PrimaryKey: true}
	if !pgOnlyCol.isEmpty() {
		t.Error("expected DorisColumnMetaIR with only Postgres fields to report isEmpty() == true for Doris")
	}
	if pgOnlyCol.isAllEmpty() {
		t.Error("expected DorisColumnMetaIR with Postgres fields to report isAllEmpty() == false")
	}

	// 2. Verify legacy 50200 wire column_meta merges with top-level (proto2type.field) options without shadowing
	legacy50200 := func() []byte {
		var c []byte
		c = protowire.AppendTag(c, 1, protowire.BytesType)
		c = protowire.AppendString(c, "VARCHAR(128)")
		c = protowire.AppendTag(c, 9, protowire.BytesType)
		c = protowire.AppendString(c, "custom_col_name")
		return buildWireExtension(legacyColumnMetaExtensionNumber, c)
	}()
	mixedFieldOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(mixedFieldOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		ServerTimestamp: true,
	})
	mixedFieldOpts.ProtoReflect().SetUnknown(legacy50200)

	// 3. Build a comprehensive descriptor set testing oneof variant column options,
	// CHARACTER VARYING defaults, TIMESTAMP WITH TIME ZONE server_timestamp,
	// repeated BoolValue/UInt64Value arrays, nested table message, and forward FK reference.
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
				},
			},
		},
	}
	wrappersFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("google/protobuf/wrappers.proto"),
		Package: proto.String("google.protobuf"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("google.golang.org/protobuf/types/known/wrapperspb"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("BoolValue"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("value"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum(),
						JsonName: proto.String("value"),
					},
				},
			},
			{
				Name: proto.String("UInt64Value"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("value"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_UINT64.Enum(),
						JsonName: proto.String("value"),
					},
				},
			},
		},
	}

	charVaryingOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(charVaryingOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		Column: &proto2typepb.ColumnOptions{
			ColumnName:   "record_code",
			SqlType:      "CHARACTER VARYING(32)",
			DefaultValue: "NEW_CODE",
			Index:        true,
		},
	})

	tsTzOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(tsTzOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		ServerTimestamp: true,
		Column: &proto2typepb.ColumnOptions{
			SqlType: "TIMESTAMP WITH TIME ZONE",
		},
	})

	oneofVarOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(oneofVarOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		Column: &proto2typepb.ColumnOptions{
			ColumnName: "contact_email_addr",
			Length:     120,
			Unique:     true,
		},
	})

	fkForwardOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(fkForwardOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		ForeignKey: &proto2typepb.ForeignKey{
			RefTable:  "second_table",
			RefColumn: "id",
			OnDelete:  "CASCADE",
		},
	})

	nestedTableOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(nestedTableOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			TableName:     "nested_audit_log",
			WriterService: "audit-service",
		},
	})

	edgeFDP := &descriptorpb.FileDescriptorProto{
		Syntax:     proto.String("proto3"),
		Name:       proto.String("edge_pg.proto"),
		Package:    proto.String("edgepg.v1"),
		Dependency: []string{"google/protobuf/timestamp.proto", "google/protobuf/wrappers.proto"},
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/edgepg/v1;edgepgv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("FirstTable"),
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: proto.String("contact")},
				},
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("id"),
					},
					{
						Name:     proto.String("second_id"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("secondId"),
						Options:  fkForwardOpts,
					},
					{
						Name:     proto.String("code"),
						Number:   proto.Int32(3),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("code"),
						Options:  charVaryingOpts,
					},
					{
						Name:     proto.String("updated_at"),
						Number:   proto.Int32(4),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".google.protobuf.Timestamp"),
						JsonName: proto.String("updatedAt"),
						Options:  tsTzOpts,
					},
					{
						Name:     proto.String("flags"),
						Number:   proto.Int32(5),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".google.protobuf.BoolValue"),
						JsonName: proto.String("flags"),
					},
					{
						Name:     proto.String("counters"),
						Number:   proto.Int32(6),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".google.protobuf.UInt64Value"),
						JsonName: proto.String("counters"),
					},
					{
						Name:     proto.String("legacy_mixed"),
						Number:   proto.Int32(7),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("legacyMixed"),
						Options:  mixedFieldOpts,
					},
					{
						Name:       proto.String("contact_email"),
						Number:     proto.Int32(8),
						Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:       descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName:   proto.String("contactEmail"),
						OneofIndex: proto.Int32(0),
						Options:    oneofVarOpts,
					},
				},
				NestedType: []*descriptorpb.DescriptorProto{
					{
						Name:    proto.String("NestedAudit"),
						Options: nestedTableOpts,
						Field: []*descriptorpb.FieldDescriptorProto{
							{
								Name:     proto.String("id"),
								Number:   proto.Int32(1),
								Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
								JsonName: proto.String("id"),
							},
						},
					},
				},
			},
			{
				Name: proto.String("SecondTable"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("id"),
					},
				},
			},
		},
	}

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{tsFDP, wrappersFDP, edgeFDP},
	}
	gen := newPlugin(t, fds, []string{"edge_pg.proto"})
	runner := NewRunner()
	if err := runner.GenerateFile(gen, gen.Files[len(gen.Files)-1], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "all",
		Domain:       false,
	}); err != nil {
		t.Fatalf("GenerateFile(all) failed: %v", err)
	}

	gotFiles := make(map[string]string)
	for _, f := range gen.Response().File {
		gotFiles[filepath.Base(f.GetName())] = f.GetContent()
	}

	ormGo := gotFiles["edge_pg_postgres.type.go"]
	for _, wantGo := range []string{
		"`db:\"record_code\"`",
		"`db:\"custom_col_name\"`",
		"`db:\"contact_email_addr\"`",
	} {
		if !strings.Contains(ormGo, wantGo) {
			t.Errorf("expected %s in edge_pg_postgres.type.go:\n%s", wantGo, ormGo)
		}
	}

	hcl := gotFiles["edge_pg_postgres.hcl"]
	for _, wantHCL := range []string{
		`table "nested_audit_log"`,
		`type = sql("character varying(32)")`,
		`default = "NEW_CODE"`,
		`type = sql("timestamp with time zone")`,
		`default = sql("now()")`,
		`type = sql("boolean[]")`,
		`type = sql("numeric(20,0)[]")`,
		"column \"contact_email_addr\" {\n    null = true\n    type = varchar(120)\n  }",
		`index "uq_first_table_contact_email_addr"`,
	} {
		if !strings.Contains(hcl, wantHCL) {
			t.Errorf("expected %s in edge_pg_postgres.hcl:\n%s", wantHCL, hcl)
		}
	}

	ddl := gotFiles["edge_pg_postgres.sql"]
	for _, wantDDL := range []string{
		`"record_code" CHARACTER VARYING(32) NOT NULL DEFAULT 'NEW_CODE'`,
		`"updated_at" TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()`,
		`"flags" BOOLEAN[] NOT NULL`,
		`"counters" NUMERIC(20,0)[] NOT NULL`,
		`"contact_email_addr" VARCHAR(120)`,
		`CREATE TABLE IF NOT EXISTS "nested_audit_log"`,
		`CREATE TABLE IF NOT EXISTS "second_table"`,
		`ALTER TABLE "first_table" ADD CONSTRAINT "fk_first_table_second_id" FOREIGN KEY ("second_id") REFERENCES "second_table" ("id") ON DELETE CASCADE;`,
	} {
		if !strings.Contains(ddl, wantDDL) {
			t.Errorf("expected %s in edge_pg_postgres.sql:\n%s", wantDDL, ddl)
		}
	}
	// Verify ALTER TABLE appears AFTER CREATE TABLE IF NOT EXISTS "second_table"
	secondIdx := strings.Index(ddl, `CREATE TABLE IF NOT EXISTS "second_table"`)
	alterIdx := strings.Index(ddl, `ALTER TABLE "first_table" ADD CONSTRAINT`)
	if secondIdx < 0 || alterIdx < 0 || alterIdx < secondIdx {
		t.Errorf("expected ALTER TABLE foreign key constraint after all CREATE TABLE statements:\n%s", ddl)
	}

	// 4. Verify 63-byte table name limit is enforced
	longTableOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(longTableOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			TableName: strings.Repeat("a", 64),
		},
	})
	longFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("long_table.proto"),
		Package: proto.String("test.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/test/v1;testv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("LongTable"),
				Options: longTableOpts,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("id"),
					},
				},
			},
		},
	}
	genLong := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{longFDP}}, []string{"long_table.proto"})
	if err := NewRunner().GenerateFile(genLong, genLong.Files[0], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "ddl",
		Domain:       false,
	}); err == nil || !strings.Contains(err.Error(), "exceeds 63-byte identifier limit") {
		t.Errorf("expected 63-byte identifier limit error, got %v", err)
	}

	// 5. Verify conflicting duplicate index definitions with the same name return an error
	conflictIdxOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(conflictIdxOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			Indexes: []*proto2typepb.IndexDef{
				{Name: "idx_shared", Columns: []string{"id"}, Unique: false},
				{Name: "idx_shared", Columns: []string{"id"}, Unique: true},
			},
		},
	})
	conflictIdxFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("conflict_idx.proto"),
		Package: proto.String("test.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/test/v1;testv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("ConflictIdx"),
				Options: conflictIdxOpts,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("id"),
					},
				},
			},
		},
	}
	genConflictIdx := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{conflictIdxFDP}}, []string{"conflict_idx.proto"})
	if err := NewRunner().GenerateFile(genConflictIdx, genConflictIdx.Files[0], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "ddl",
		Domain:       false,
	}); err == nil || !strings.Contains(err.Error(), "conflicting definitions for index") {
		t.Errorf("expected conflicting index error, got %v", err)
	}

	// 6. Verify cross-file PostgreSQL table name collision is rejected
	dupOpts1 := &descriptorpb.MessageOptions{}
	proto.SetExtension(dupOpts1, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			TableName: "encounter_service__patient",
		},
	})
	dupOpts2 := &descriptorpb.MessageOptions{}
	proto.SetExtension(dupOpts2, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			WriterService: "encounter-service",
		},
	})
	dupFDP1 := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("dup1.proto"),
		Package: proto.String("dup1.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/dup1/v1;dup1v1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("LegacyPatient"),
				Options: dupOpts1,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("id"),
					},
				},
			},
		},
	}
	dupFDP2 := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("dup2.proto"),
		Package: proto.String("dup2.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/dup2/v1;dup2v1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("Patient"),
				Options: dupOpts2,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("id"),
					},
				},
			},
		},
	}
	genDup := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{dupFDP1, dupFDP2}}, []string{"dup1.proto", "dup2.proto"})
	if err := NewRunner().GenerateFile(genDup, genDup.Files[0], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "ddl",
		Domain:       false,
	}); err == nil || !strings.Contains(err.Error(), "duplicate PostgreSQL table name") {
		t.Errorf("expected cross-file duplicate PostgreSQL table name error, got %v", err)
	}
}
