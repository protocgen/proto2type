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

// TestPostgresHardening_SameServiceForeignKeyPrefix verifies that intra-schema foreign keys
// referencing an unprefixed snake_case table name or message name automatically resolve to
// the target table's writer_service-prefixed physical table name in both DDL and HCL.
func TestPostgresHardening_SameServiceForeignKeyPrefix(t *testing.T) {
	orderTableOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(orderTableOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			WriterService: "order-service",
		},
	})

	itemTableOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(itemTableOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			WriterService: "order-service",
		},
	})

	fkBySnakeOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(fkBySnakeOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		ForeignKey: &proto2typepb.ForeignKey{
			RefTable:  "order",
			RefColumn: "id",
			OnDelete:  "CASCADE",
		},
	})

	fkByMsgNameOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(fkByMsgNameOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		ForeignKey: &proto2typepb.ForeignKey{
			RefTable:  "Order",
			RefColumn: "id",
			OnDelete:  "RESTRICT",
		},
	})

	fkExternalOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(fkExternalOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		ForeignKey: &proto2typepb.ForeignKey{
			RefTable:  "external_vendors",
			RefColumn: "id",
		},
	})

	fdp := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("orders.proto"),
		Package: proto.String("orders.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/orders/v1;ordersv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("Order"),
				Options: orderTableOpts,
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
			{
				Name:    proto.String("OrderItem"),
				Options: itemTableOpts,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("id"),
					},
					{
						Name:     proto.String("order_id"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("orderId"),
						Options:  fkBySnakeOpts,
					},
					{
						Name:     proto.String("parent_order_id"),
						Number:   proto.Int32(3),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("parentOrderId"),
						Options:  fkByMsgNameOpts,
					},
					{
						Name:     proto.String("vendor_id"),
						Number:   proto.Int32(4),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("vendorId"),
						Options:  fkExternalOpts,
					},
				},
			},
		},
	}

	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"orders.proto"})
	if err := NewRunner().GenerateFile(gen, gen.Files[0], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "all",
		Domain:       false,
	}); err != nil {
		t.Fatalf("GenerateFile failed: %v", err)
	}

	gotFiles := make(map[string]string)
	for _, f := range gen.Response().File {
		gotFiles[filepath.Base(f.GetName())] = f.GetContent()
	}

	ddl := gotFiles["orders_postgres.sql"]
	for _, wantSQL := range []string{
		`ALTER TABLE "order_service__order_item" ADD CONSTRAINT "fk_order_service__order_item_order_id" FOREIGN KEY ("order_id") REFERENCES "order_service__order" ("id") ON DELETE CASCADE;`,
		`ALTER TABLE "order_service__order_item" ADD CONSTRAINT "fk_order_service__order_item_parent_order_id" FOREIGN KEY ("parent_order_id") REFERENCES "order_service__order" ("id") ON DELETE RESTRICT;`,
		`ALTER TABLE "order_service__order_item" ADD CONSTRAINT "fk_order_service__order_item_vendor_id" FOREIGN KEY ("vendor_id") REFERENCES "external_vendors" ("id");`,
	} {
		if !strings.Contains(ddl, wantSQL) {
			t.Errorf("DDL missing expected FK statement %q:\n%s", wantSQL, ddl)
		}
	}

	hcl := gotFiles["orders_postgres.hcl"]
	for _, wantHCL := range []string{
		"  foreign_key \"fk_order_service__order_item_order_id\" {\n    columns     = [column.order_id]\n    ref_columns = [table.order_service__order.column.id]\n    on_delete   = CASCADE\n  }",
		"  foreign_key \"fk_order_service__order_item_parent_order_id\" {\n    columns     = [column.parent_order_id]\n    ref_columns = [table.order_service__order.column.id]\n    on_delete   = RESTRICT\n  }",
		"  foreign_key \"fk_order_service__order_item_vendor_id\" {\n    columns     = [column.vendor_id]\n    ref_columns = [table.external_vendors.column.id]\n  }",
	} {
		if !strings.Contains(hcl, wantHCL) {
			t.Errorf("HCL missing expected FK block %q:\n%s", wantHCL, hcl)
		}
	}
}

// TestPostgresHardening_AdversarialInjectionRejection verifies that malicious default_value,
// ServerDefault, sql_type, and foreign key inputs are rejected both during schema collection
// and when calling BuildPostgresCreateTableSQL or BuildPostgresAtlasHCL directly.
func TestPostgresHardening_AdversarialInjectionRejection(t *testing.T) {
	cases := []struct {
		name      string
		fieldType descriptorpb.FieldDescriptorProto_Type
		sqlType   string
		defVal    string
		wantErr   string
	}{
		{
			name:      "numeric_sql_injection_check",
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_INT32,
			defVal:    "0 CHECK (1=1)",
			wantErr:   "invalid numeric default_value",
		},
		{
			name:      "numeric_hcl_injection_braces",
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_INT64,
			defVal:    "0 } column pwn {",
			wantErr:   "invalid numeric default_value",
		},
		{
			name:      "boolean_sql_injection",
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_BOOL,
			defVal:    "true OR 1=1",
			wantErr:   "invalid boolean default_value",
		},
		{
			name:      "sql_type_references_injection",
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_STRING,
			sqlType:   "VARCHAR(10) REFERENCES other(id)",
			wantErr:   "invalid PostgreSQL sql_type",
		},
		{
			name:      "sql_type_check_injection",
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_STRING,
			sqlType:   "NUMERIC(10, 2) CHECK (val > 0)",
			wantErr:   "invalid PostgreSQL sql_type",
		},
		{
			name:      "string_default_newline_rejection",
			fieldType: descriptorpb.FieldDescriptorProto_TYPE_STRING,
			defVal:    "hello\nworld",
			wantErr:   "invalid default_value",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fOpts := &descriptorpb.FieldOptions{}
			proto.SetExtension(fOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
				Column: &proto2typepb.ColumnOptions{
					SqlType:      tc.sqlType,
					DefaultValue: tc.defVal,
				},
			})
			fdp := &descriptorpb.FileDescriptorProto{
				Syntax:  proto.String("proto3"),
				Name:    proto.String("adv.proto"),
				Package: proto.String("adv.v1"),
				Options: &descriptorpb.FileOptions{
					GoPackage: proto.String("example.com/adv/v1;advv1"),
				},
				MessageType: []*descriptorpb.DescriptorProto{
					{
						Name: proto.String("AdvRecord"),
						Field: []*descriptorpb.FieldDescriptorProto{
							{
								Name:     proto.String("val"),
								Number:   proto.Int32(1),
								Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:     tc.fieldType.Enum(),
								JsonName: proto.String("val"),
								Options:  fOpts,
							},
						},
					},
				},
			}
			gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"adv.proto"})
			err := NewRunner().GenerateFile(gen, gen.Files[0], &Options{
				Lang:         "go",
				Backend:      "postgres",
				PostgresMode: "both",
				Domain:       false,
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}

	// Direct descriptor calls must also reject malicious payloads
	badDescriptors := []PostgresTableSchemaDescriptor{
		{
			TableName: "valid_table",
			Columns: []PostgresColumnDescriptor{
				{ColumnName: "c1", SQLType: "INTEGER", HCLType: "integer", DefaultValue: "0 CHECK (1=1)"},
			},
		},
		{
			TableName: "valid_table",
			Columns: []PostgresColumnDescriptor{
				{ColumnName: "c1", SQLType: "TIMESTAMPTZ", HCLType: "timestamptz", ServerDefault: "NOW()); DROP TABLE x; --"},
			},
		},
		{
			TableName: "valid_table",
			Columns: []PostgresColumnDescriptor{
				{ColumnName: "c1", SQLType: "VARCHAR(10) REFERENCES t(id)", HCLType: "varchar(10)"},
			},
		},
		{
			TableName: "valid_table",
			Columns: []PostgresColumnDescriptor{
				{ColumnName: "c1", SQLType: "TEXT", HCLType: "text"},
			},
			ForeignKeys: []PostgresForeignKeyDescriptor{
				{
					Name:      "fk_valid",
					Columns:   []string{"c1"},
					RefTable:  strings.Repeat("a", 64),
					RefColumn: "id",
				},
			},
		},
		{
			TableName: "valid_table",
			Columns: []PostgresColumnDescriptor{
				{ColumnName: "c1", SQLType: "TEXT", HCLType: "text"},
			},
			ForeignKeys: []PostgresForeignKeyDescriptor{
				{
					Name:      "fk_valid",
					Columns:   []string{"c1"},
					RefTable:  "other_table",
					RefColumn: "id",
					OnDelete:  "CASCADE; DROP TABLE x",
				},
			},
		},
	}

	for i, desc := range badDescriptors {
		if sql := desc.BuildPostgresCreateTableSQL(); sql != "" && len(desc.ForeignKeys) == 0 {
			t.Errorf("case %d: expected empty SQL from BuildPostgresCreateTableSQL, got:\n%s", i, sql)
		}
		if len(desc.ForeignKeys) > 0 {
			if fkSQL := desc.BuildPostgresForeignKeysSQL(); len(fkSQL) != 0 {
				t.Errorf("case %d: expected empty FK SQL from BuildPostgresForeignKeysSQL, got: %v", i, fkSQL)
			}
		}
		if hcl := desc.BuildPostgresAtlasHCL(); hcl != "" {
			t.Errorf("case %d: expected empty HCL from BuildPostgresAtlasHCL, got:\n%s", i, hcl)
		}
	}
}

// TestPostgresHardening_DefaultValueQuotingMatrix verifies default value formatting across
// CHAR(2), TIMESTAMP, TIMESTAMPTZ(6), INTERVAL, INET, TEXT[], BOOLEAN, and INTEGER in DDL and HCL.
func TestPostgresHardening_DefaultValueQuotingMatrix(t *testing.T) {
	mkField := func(name string, num int32, ft descriptorpb.FieldDescriptorProto_Type, label descriptorpb.FieldDescriptorProto_Label, sqlType, defVal string, serverTS bool) *descriptorpb.FieldDescriptorProto {
		opts := &descriptorpb.FieldOptions{}
		proto.SetExtension(opts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
			ServerTimestamp: serverTS,
			Column: &proto2typepb.ColumnOptions{
				SqlType:      sqlType,
				DefaultValue: defVal,
			},
		})
		return &descriptorpb.FieldDescriptorProto{
			Name:     proto.String(name),
			Number:   proto.Int32(num),
			Label:    label.Enum(),
			Type:     ft.Enum(),
			JsonName: proto.String(name),
			Options:  opts,
		}
	}

	fdp := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("defaults_matrix.proto"),
		Package: proto.String("matrix.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/matrix/v1;matrixv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("DefaultsMatrix"),
				Field: []*descriptorpb.FieldDescriptorProto{
					mkField("country_code", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL, "CHAR(2)", "US", false),
					mkField("legacy_ts", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL, "TIMESTAMP", "1970-01-01 00:00:00", false),
					mkField("precise_tstz", 3, descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL, "TIMESTAMPTZ(6)", "1970-01-01T00:00:00Z", false),
					mkField("auto_tstz", 4, descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL, "TIMESTAMPTZ(6)", "", true),
					mkField("lease_window", 5, descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL, "INTERVAL", "1 day", false),
					mkField("host_ip", 6, descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL, "INET", "127.0.0.1", false),
					mkField("labels", 7, descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_LABEL_REPEATED, "", "{}", false),
					mkField("is_enabled", 8, descriptorpb.FieldDescriptorProto_TYPE_BOOL, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL, "", "TRUE", false),
					mkField("retry_offset", 9, descriptorpb.FieldDescriptorProto_TYPE_INT32, descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL, "", "-42", false),
				},
			},
		},
	}

	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"defaults_matrix.proto"})
	if err := NewRunner().GenerateFile(gen, gen.Files[0], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "both",
		Domain:       false,
	}); err != nil {
		t.Fatalf("GenerateFile failed: %v", err)
	}

	gotFiles := make(map[string]string)
	for _, f := range gen.Response().File {
		gotFiles[filepath.Base(f.GetName())] = f.GetContent()
	}

	ddl := gotFiles["defaults_matrix_postgres.sql"]
	for _, wantSQL := range []string{
		`"country_code" CHAR(2) NOT NULL DEFAULT 'US'`,
		`"legacy_ts" TIMESTAMP NOT NULL DEFAULT '1970-01-01 00:00:00'`,
		`"precise_tstz" TIMESTAMPTZ(6) NOT NULL DEFAULT '1970-01-01T00:00:00Z'`,
		`"auto_tstz" TIMESTAMPTZ(6) NOT NULL DEFAULT NOW()`,
		`"lease_window" INTERVAL NOT NULL DEFAULT '1 day'`,
		`"host_ip" INET NOT NULL DEFAULT '127.0.0.1'`,
		`"labels" TEXT[] NOT NULL DEFAULT '{}'`,
		`"is_enabled" BOOLEAN NOT NULL DEFAULT TRUE`,
		`"retry_offset" INTEGER NOT NULL DEFAULT -42`,
	} {
		if !strings.Contains(ddl, wantSQL) {
			t.Errorf("DDL missing expected column definition %q:\n%s", wantSQL, ddl)
		}
	}

	hcl := gotFiles["defaults_matrix_postgres.hcl"]
	for _, wantHCL := range []string{
		"  column \"country_code\" {\n    null = false\n    type = char(2)\n    default = \"US\"\n  }",
		"  column \"legacy_ts\" {\n    null = false\n    type = timestamp\n    default = \"1970-01-01 00:00:00\"\n  }",
		"  column \"precise_tstz\" {\n    null = false\n    type = timestamptz(6)\n    default = \"1970-01-01T00:00:00Z\"\n  }",
		"  column \"auto_tstz\" {\n    null = false\n    type = timestamptz(6)\n    default = sql(\"now()\")\n  }",
		"  column \"lease_window\" {\n    null = false\n    type = interval\n    default = \"1 day\"\n  }",
		"  column \"host_ip\" {\n    null = false\n    type = inet\n    default = \"127.0.0.1\"\n  }",
		"  column \"labels\" {\n    null = false\n    type = sql(\"text[]\")\n    default = \"{}\"\n  }",
		"  column \"is_enabled\" {\n    null = false\n    type = boolean\n    default = true\n  }",
		"  column \"retry_offset\" {\n    null = false\n    type = integer\n    default = -42\n  }",
	} {
		if !strings.Contains(hcl, wantHCL) {
			t.Errorf("HCL missing expected column block %q:\n%s", wantHCL, hcl)
		}
	}
}

// TestPostgresHardening_MapAndNestedMessagesNotStandaloneTables verifies that map<string, SubMeta>
// value messages and unannotated nested messages are not emitted as standalone tables, while
// nested messages with explicit TableOptions are emitted as standalone tables.
func TestPostgresHardening_MapAndNestedMessagesNotStandaloneTables(t *testing.T) {
	annotatedNestedOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(annotatedNestedOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			TableName: "explicit_nested_events",
		},
	})

	fdp := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("maps_nested.proto"),
		Package: proto.String("mapsnested.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/mapsnested/v1;mapsnestedv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("SubMeta"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("label"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("label"),
					},
				},
			},
			{
				Name: proto.String("MainContainer"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("id"),
					},
					{
						Name:     proto.String("meta_by_key"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						TypeName: proto.String(".mapsnested.v1.MainContainer.MetaByKeyEntry"),
						JsonName: proto.String("metaByKey"),
					},
				},
				NestedType: []*descriptorpb.DescriptorProto{
					{
						Name: proto.String("MetaByKeyEntry"),
						Options: &descriptorpb.MessageOptions{
							MapEntry: proto.Bool(true),
						},
						Field: []*descriptorpb.FieldDescriptorProto{
							{
								Name:     proto.String("key"),
								Number:   proto.Int32(1),
								Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
								JsonName: proto.String("key"),
							},
							{
								Name:     proto.String("value"),
								Number:   proto.Int32(2),
								Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
								TypeName: proto.String(".mapsnested.v1.SubMeta"),
								JsonName: proto.String("value"),
							},
						},
					},
					{
						Name: proto.String("UnannotatedNestedHelper"),
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
					{
						Name:    proto.String("AnnotatedNestedEvent"),
						Options: annotatedNestedOpts,
						Field: []*descriptorpb.FieldDescriptorProto{
							{
								Name:     proto.String("event_id"),
								Number:   proto.Int32(1),
								Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
								JsonName: proto.String("eventId"),
							},
						},
					},
				},
			},
		},
	}

	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"maps_nested.proto"})
	if err := NewRunner().GenerateFile(gen, gen.Files[0], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "both",
		Domain:       false,
	}); err != nil {
		t.Fatalf("GenerateFile failed: %v", err)
	}

	gotFiles := make(map[string]string)
	for _, f := range gen.Response().File {
		gotFiles[filepath.Base(f.GetName())] = f.GetContent()
	}

	ddl := gotFiles["maps_nested_postgres.sql"]
	hcl := gotFiles["maps_nested_postgres.hcl"]

	if !strings.Contains(ddl, `CREATE TABLE IF NOT EXISTS "main_container"`) || !strings.Contains(ddl, `"meta_by_key" JSONB NOT NULL`) {
		t.Errorf("expected main_container with meta_by_key JSONB in DDL:\n%s", ddl)
	}
	if !strings.Contains(ddl, `CREATE TABLE IF NOT EXISTS "explicit_nested_events"`) {
		t.Errorf("expected explicit_nested_events in DDL:\n%s", ddl)
	}
	if !strings.Contains(hcl, `table "explicit_nested_events"`) {
		t.Errorf("expected explicit_nested_events in HCL:\n%s", hcl)
	}
	if strings.Contains(ddl, `"sub_meta"`) || strings.Contains(hcl, `table "sub_meta"`) {
		t.Errorf("map value message SubMeta must not be emitted as a standalone table:\nDDL:\n%s\nHCL:\n%s", ddl, hcl)
	}
	if strings.Contains(ddl, `"unannotated_nested_helper"`) || strings.Contains(hcl, `table "unannotated_nested_helper"`) {
		t.Errorf("unannotated nested message UnannotatedNestedHelper must not be emitted as a standalone table:\nDDL:\n%s\nHCL:\n%s", ddl, hcl)
	}
}

// TestPostgresHardening_Identifier63ByteCompactionAndValidation verifies deterministic 63-byte
// identifier compaction with SHA-256 suffix for auto-generated index/unique/FK names, and
// strict 63-byte rejection for explicit user-supplied identifiers.
func TestPostgresHardening_Identifier63ByteCompactionAndValidation(t *testing.T) {
	tblOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(tblOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			WriterService: "claims-processing-service",
		},
	})

	col1Opts := &descriptorpb.FieldOptions{}
	proto.SetExtension(col1Opts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		Index:  true,
		Unique: true,
		ForeignKey: &proto2typepb.ForeignKey{
			RefTable:  "tenants",
			RefColumn: "id",
		},
	})

	col2Opts := &descriptorpb.FieldOptions{}
	proto.SetExtension(col2Opts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		Index:  true,
		Unique: true,
		ForeignKey: &proto2typepb.ForeignKey{
			RefTable:  "tenants",
			RefColumn: "id",
		},
	})

	fdp := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("long_names.proto"),
		Package: proto.String("longnames.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/longnames/v1;longnamesv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("AdjudicationBatchRecord"),
				Options: tblOpts,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("primary_organization_hierarchical_reference_identifier_alpha"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("colAlpha"),
						Options:  col1Opts,
					},
					{
						Name:     proto.String("primary_organization_hierarchical_reference_identifier_bravo"),
						Number:   proto.Int32(2),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("colBravo"),
						Options:  col2Opts,
					},
				},
			},
		},
	}

	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"long_names.proto"})
	if err := NewRunner().GenerateFile(gen, gen.Files[0], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "both",
		Domain:       false,
	}); err != nil {
		t.Fatalf("GenerateFile failed: %v", err)
	}
	gotFiles := make(map[string]string)
	for _, f := range gen.Response().File {
		gotFiles[filepath.Base(f.GetName())] = f.GetContent()
	}
	ddl := gotFiles["long_names_postgres.sql"]
	hcl := gotFiles["long_names_postgres.hcl"]
	if n := strings.Count(ddl, "CREATE INDEX IF NOT EXISTS \"idx_"); n != 2 {
		t.Fatalf("expected 2 compacted idx_ indexes in DDL, got %d:\n%s", n, ddl)
	}
	if n := strings.Count(ddl, "CREATE UNIQUE INDEX IF NOT EXISTS \"uq_"); n != 2 {
		t.Fatalf("expected 2 compacted uq_ indexes in DDL, got %d:\n%s", n, ddl)
	}
	if !strings.Contains(ddl, "ADD CONSTRAINT \"fk_") {
		t.Fatalf("expected compacted fk_ constraint in DDL:\n%s", ddl)
	}
	for _, line := range strings.Split(ddl, "\n") {
		for _, pfx := range []string{`"idx_`, `"uq_`, `"fk_`} {
			if pos := strings.Index(line, pfx); pos >= 0 {
				rest := line[pos+1:]
				if end := strings.IndexByte(rest, '"'); end >= 0 {
					id := rest[:end]
					if len(id) > maxPostgresIdentifierBytes {
						t.Errorf("compacted identifier %q length %d exceeds 63 bytes", id, len(id))
					}
				}
			}
		}
	}
	if !strings.Contains(hcl, `index "idx_`) || !strings.Contains(hcl, `index "uq_`) || !strings.Contains(hcl, `foreign_key "fk_`) {
		t.Fatalf("expected compacted idx_, uq_, and fk_ identifiers in HCL:\n%s", hcl)
	}

	// Verify explicit index name > 63 bytes is rejected
	explicitLongIdxOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(explicitLongIdxOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			Indexes: []*proto2typepb.IndexDef{
				{
					Name:    "idx_" + strings.Repeat("x", 60),
					Columns: []string{"id"},
				},
			},
		},
	})
	badIdxFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("bad_idx_len.proto"),
		Package: proto.String("longnames.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/longnames/v1;longnamesv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("BadIdxLen"),
				Options: explicitLongIdxOpts,
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
	genBadIdx := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{badIdxFDP}}, []string{"bad_idx_len.proto"})
	if err := NewRunner().GenerateFile(genBadIdx, genBadIdx.Files[0], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "ddl",
		Domain:       false,
	}); err == nil || !strings.Contains(err.Error(), "exceeds 63-byte identifier limit") {
		t.Errorf("expected error for >63-byte explicit index name, got %v", err)
	}
}

// TestPostgresHardening_CompositeAndOneofPKAndRedundantUniqueIndex verifies that composite
// primary keys and oneof variant primary keys are marked NOT NULL, that single-column PKs
// skip redundant uq_<table>_<pk> indexes while composite PK columns with unique: true keep them,
// and that negative column length is rejected.
func TestPostgresHardening_CompositeAndOneofPKAndRedundantUniqueIndex(t *testing.T) {
	compTableOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(compTableOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			PrimaryKeyFields: []string{"tenant_id", "oneof_pk_code"},
		},
	})

	tenantFieldOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(tenantFieldOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		Unique: true,
	})

	singlePKFieldOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(singlePKFieldOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		PrimaryKey: true,
		Unique:     true,
		Index:      true,
	})

	fdp := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("pk_test.proto"),
		Package: proto.String("pktest.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/pktest/v1;pktestv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("CompositeTable"),
				Options: compTableOpts,
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: proto.String("pk_choice")},
				},
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("tenant_id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("tenantId"),
						Options:  tenantFieldOpts,
					},
					{
						Name:       proto.String("oneof_pk_code"),
						Number:     proto.Int32(2),
						Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:       descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName:   proto.String("oneofPkCode"),
						OneofIndex: proto.Int32(0),
					},
				},
			},
			{
				Name: proto.String("SinglePKTable"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("id"),
						Options:  singlePKFieldOpts,
					},
				},
			},
		},
	}

	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"pk_test.proto"})
	if err := NewRunner().GenerateFile(gen, gen.Files[0], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "both",
		Domain:       false,
	}); err != nil {
		t.Fatalf("GenerateFile failed: %v", err)
	}

	gotFiles := make(map[string]string)
	for _, f := range gen.Response().File {
		gotFiles[filepath.Base(f.GetName())] = f.GetContent()
	}

	ddl := gotFiles["pk_test_postgres.sql"]
	hcl := gotFiles["pk_test_postgres.hcl"]

	// Oneof variant in primary key must be NOT NULL
	if !strings.Contains(ddl, `"oneof_pk_code" TEXT NOT NULL`) {
		t.Errorf("expected oneof PK column oneof_pk_code to be NOT NULL in DDL:\n%s", ddl)
	}
	if !strings.Contains(hcl, "  column \"oneof_pk_code\" {\n    null = false\n    type = text\n  }") {
		t.Errorf("expected oneof PK column oneof_pk_code to have null = false in HCL:\n%s", hcl)
	}
	// Composite PK column with unique: true still gets its single-column unique index
	if !strings.Contains(ddl, `uq_composite_table_tenant_id`) {
		t.Errorf("expected uq_composite_table_tenant_id on composite PK member in DDL:\n%s", ddl)
	}
	// Single-column PK with unique: true must NOT emit redundant uq_single_pk_table_id
	if strings.Contains(ddl, `uq_single_pk_table_id`) || strings.Contains(hcl, `uq_single_pk_table_id`) {
		t.Errorf("expected redundant single-column PK unique index uq_single_pk_table_id to be skipped:\nDDL:\n%s\nHCL:\n%s", ddl, hcl)
	}

	// Negative length must be rejected
	negLenOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(negLenOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		Column: &proto2typepb.ColumnOptions{
			Length: -5,
		},
	})
	negFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("neg_len.proto"),
		Package: proto.String("pktest.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/pktest/v1;pktestv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("NegLenTable"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("code"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("code"),
						Options:  negLenOpts,
					},
				},
			},
		},
	}
	genNeg := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{negFDP}}, []string{"neg_len.proto"})
	if err := NewRunner().GenerateFile(genNeg, genNeg.Files[0], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "ddl",
		Domain:       false,
	}); err == nil || !strings.Contains(err.Error(), "column length cannot be negative") {
		t.Errorf("expected negative column length error, got %v", err)
	}
}

// TestPostgresHardening_Wire1307ShorthandAndNegativeLength verifies raw wire decoding of
// extension 1307 FieldOptions shorthand tags 11-14 (primary_key, index, unique, foreign_key),
// negative two's-complement varint length at tag 13 in ColumnOptions, and mixed 1307+50201 Doris eligibility.
func TestPostgresHardening_Wire1307ShorthandAndNegativeLength(t *testing.T) {
	// 1. Build raw wire bytes for extension 1307 FieldOptions with tags 10 (column), 11 (pk), 12 (index), 13 (unique), 14 (fk)
	var colBytes []byte
	colBytes = protowire.AppendTag(colBytes, 11, protowire.BytesType)
	colBytes = protowire.AppendString(colBytes, "VARCHAR(64)")

	var fkBytes []byte
	fkBytes = protowire.AppendTag(fkBytes, 1, protowire.BytesType)
	fkBytes = protowire.AppendString(fkBytes, "orgs")
	fkBytes = protowire.AppendTag(fkBytes, 2, protowire.BytesType)
	fkBytes = protowire.AppendString(fkBytes, "org_id")
	fkBytes = protowire.AppendTag(fkBytes, 3, protowire.BytesType)
	fkBytes = protowire.AppendString(fkBytes, "CASCADE")

	var fieldOptsPayload []byte
	fieldOptsPayload = protowire.AppendTag(fieldOptsPayload, 10, protowire.BytesType)
	fieldOptsPayload = protowire.AppendBytes(fieldOptsPayload, colBytes)
	fieldOptsPayload = protowire.AppendTag(fieldOptsPayload, 11, protowire.VarintType)
	fieldOptsPayload = protowire.AppendVarint(fieldOptsPayload, 1)
	fieldOptsPayload = protowire.AppendTag(fieldOptsPayload, 12, protowire.VarintType)
	fieldOptsPayload = protowire.AppendVarint(fieldOptsPayload, 1)
	fieldOptsPayload = protowire.AppendTag(fieldOptsPayload, 13, protowire.VarintType)
	fieldOptsPayload = protowire.AppendVarint(fieldOptsPayload, 1)
	fieldOptsPayload = protowire.AppendTag(fieldOptsPayload, 14, protowire.BytesType)
	fieldOptsPayload = protowire.AppendBytes(fieldOptsPayload, fkBytes)

	wire1307 := buildWireExtension(proto2typeExtensionNumber, fieldOptsPayload)
	parsed := parseDorisColumnMetaFromOptionsWire(wire1307)
	if parsed == nil {
		t.Fatal("parseDorisColumnMetaFromOptionsWire returned nil")
	}
	if parsed.SQLType != "VARCHAR(64)" || !parsed.PrimaryKey || !parsed.Index || !parsed.Unique {
		t.Errorf("unexpected parsed shorthand fields: %+v", parsed)
	}
	if parsed.ForeignKey == nil || parsed.ForeignKey.RefTable != "orgs" || parsed.ForeignKey.RefColumn != "org_id" || parsed.ForeignKey.OnDelete != "CASCADE" {
		t.Errorf("unexpected parsed ForeignKey: %+v", parsed.ForeignKey)
	}

	// 2. Verify negative varint for length (tag 13 inside ColumnOptions) is decoded as negative int32
	var negColBytes []byte
	negColBytes = protowire.AppendTag(negColBytes, 13, protowire.VarintType)
	negTen := int64(-10)
	negColBytes = protowire.AppendVarint(negColBytes, uint64(negTen))
	var negFieldPayload []byte
	negFieldPayload = protowire.AppendTag(negFieldPayload, 10, protowire.BytesType)
	negFieldPayload = protowire.AppendBytes(negFieldPayload, negColBytes)
	negParsed := parseDorisColumnMetaFromOptionsWire(buildWireExtension(proto2typeExtensionNumber, negFieldPayload))
	if negParsed == nil || negParsed.Length != -10 {
		t.Errorf("expected negative wire length -10, got %+v", negParsed)
	}

	// 3. Verify Doris eligibility when a message has 1307 writer_service + 50201 table_name + 50200 column SQLType
	mixedMsgOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(mixedMsgOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			WriterService: "analytics-writer",
		},
	})
	mixedMsgOpts.ProtoReflect().SetUnknown(buildLegacy50201WithWriterWire("legacy_events", "", nil, nil))

	var col50200 []byte
	col50200 = protowire.AppendTag(col50200, 1, protowire.BytesType)
	col50200 = protowire.AppendString(col50200, "VARCHAR(64)")
	fieldOpts50200 := &descriptorpb.FieldOptions{}
	fieldOpts50200.ProtoReflect().SetUnknown(buildWireExtension(legacyColumnMetaExtensionNumber, col50200))

	dorisFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("mixed_doris.proto"),
		Package: proto.String("mixed.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/mixed/v1;mixedv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("LegacyEvent"),
				Options: mixedMsgOpts,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("event_id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("eventId"),
						Options:  fieldOpts50200,
					},
				},
			},
		},
	}
	genDoris := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{dorisFDP}}, []string{"mixed_doris.proto"})
	tables, err := buildDorisTablesForFile(genDoris.Files[0], nil)
	if err != nil {
		t.Fatalf("buildDorisTablesForFile failed: %v", err)
	}
	if len(tables) != 1 || tables[0].Meta.TableName != "legacy_events" || tables[0].Meta.WriterService != "analytics-writer" {
		t.Errorf("expected LegacyEvent to be Doris-eligible with merged table_name and writer_service, got %+v", tables)
	}
}

// TestPostgresHardening_CrossFileDuplicateTableNamesAllModes verifies that cross-file
// physical PostgreSQL table name collisions are rejected across orm, ddl, hcl, and all modes.
func TestPostgresHardening_CrossFileDuplicateTableNamesAllModes(t *testing.T) {
	opts1 := &descriptorpb.MessageOptions{}
	proto.SetExtension(opts1, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			TableName: "billing_service__invoice",
		},
	})
	opts2 := &descriptorpb.MessageOptions{}
	proto.SetExtension(opts2, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			WriterService: "billing-service",
		},
	})

	fdp1 := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("invoice_a.proto"),
		Package: proto.String("billinga.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/billinga/v1;billingav1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("LegacyInvoice"),
				Options: opts1,
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
	fdp2 := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("invoice_b.proto"),
		Package: proto.String("billingb.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/billingb/v1;billingbv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("Invoice"),
				Options: opts2,
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

	for _, mode := range []string{"orm", "ddl", "hcl", "all"} {
		t.Run("mode_"+mode, func(t *testing.T) {
			gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp1, fdp2}}, []string{"invoice_a.proto", "invoice_b.proto"})
			err := NewRunner().GenerateFile(gen, gen.Files[0], &Options{
				Lang:         "go",
				Backend:      "postgres",
				PostgresMode: mode,
				Domain:       false,
			})
			if err == nil || !strings.Contains(err.Error(), "duplicate PostgreSQL table name") {
				t.Errorf("mode %q: expected duplicate PostgreSQL table name error, got %v", mode, err)
			}
		})
	}
}

// TestPostgresHardening_ExplicitIndexNameAmbiguousFKAndHCLTemplateEscape verifies that:
// 1. Explicitly named indexes override auto-generated indexes on the same column;
// 2. HCL template interpolation openers (${ and %{) in default values are escaped;
// 3. Long message-name aliases (>63 bytes) in ref_table resolve when the target has a valid table_name;
// 4. Ambiguous unprefixed ref_table targets across multiple writer services are rejected.
func TestPostgresHardening_ExplicitIndexNameAmbiguousFKAndHCLTemplateEscape(t *testing.T) {
	shortTargetOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(shortTargetOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			TableName: "short_target",
		},
	})

	longAliasName := "OrganizationEnterpriseHierarchicalDepartmentalConfigurationTarget" // 65 bytes
	fkColOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(fkColOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		Index: true,
		Column: &proto2typepb.ColumnOptions{
			DefaultValue: "tpl-${env}-%{region}",
		},
		ForeignKey: &proto2typepb.ForeignKey{
			RefTable:  longAliasName,
			RefColumn: "id",
		},
	})

	refTblOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(refTblOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{
			Indexes: []*proto2typepb.IndexDef{
				{
					Name:    "idx_custom_lookup",
					Columns: []string{"target_id"},
				},
			},
		},
	})

	fdp := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("explicit_idx_fk.proto"),
		Package: proto.String("explicitidxfk.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/explicitidxfk/v1;explicitidxfkv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String(longAliasName),
				Options: shortTargetOpts,
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
			{
				Name:    proto.String("RefHolder"),
				Options: refTblOpts,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("target_id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("targetId"),
						Options:  fkColOpts,
					},
				},
			},
		},
	}

	gen := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{fdp}}, []string{"explicit_idx_fk.proto"})
	if err := NewRunner().GenerateFile(gen, gen.Files[0], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "both",
		Domain:       false,
	}); err != nil {
		t.Fatalf("GenerateFile failed: %v", err)
	}
	gotFiles := make(map[string]string)
	for _, f := range gen.Response().File {
		gotFiles[filepath.Base(f.GetName())] = f.GetContent()
	}
	ddl := gotFiles["explicit_idx_fk_postgres.sql"]
	hcl := gotFiles["explicit_idx_fk_postgres.hcl"]

	if !strings.Contains(ddl, `"idx_custom_lookup"`) || strings.Contains(ddl, `"idx_ref_holder_target_id"`) {
		t.Errorf("expected explicit index idx_custom_lookup to replace auto-named index in DDL:\n%s", ddl)
	}
	if !strings.Contains(hcl, `index "idx_custom_lookup"`) || strings.Contains(hcl, `index "idx_ref_holder_target_id"`) {
		t.Errorf("expected explicit index idx_custom_lookup to replace auto-named index in HCL:\n%s", hcl)
	}
	if !strings.Contains(ddl, `REFERENCES "short_target" ("id")`) {
		t.Errorf("expected >63-byte message alias to resolve to short_target in DDL:\n%s", ddl)
	}
	if !strings.Contains(hcl, `default = "tpl-$${env}-%%{region}"`) {
		t.Errorf("expected HCL template interpolation sequences to be escaped in HCL:\n%s", hcl)
	}

	// Verify ambiguous unprefixed ref_table across two writer services is rejected for a third writer service.
	billingOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(billingOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{WriterService: "billing"},
	})
	shippingOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(shippingOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{WriterService: "shipping"},
	})
	auditOpts := &descriptorpb.MessageOptions{}
	proto.SetExtension(auditOpts, proto2typepb.E_Message, &proto2typepb.MessageOptions{
		Table: &proto2typepb.TableOptions{WriterService: "audit"},
	})
	ambigFKOpts := &descriptorpb.FieldOptions{}
	proto.SetExtension(ambigFKOpts, proto2typepb.E_Field, &proto2typepb.FieldOptions{
		ForeignKey: &proto2typepb.ForeignKey{
			RefTable:  "order",
			RefColumn: "id",
		},
	})

	ambigFDP := &descriptorpb.FileDescriptorProto{
		Syntax:  proto.String("proto3"),
		Name:    proto.String("ambig_fk.proto"),
		Package: proto.String("ambigfk.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("example.com/ambigfk/v1;ambigfkv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("BillingScope"),
				NestedType: []*descriptorpb.DescriptorProto{
					{
						Name:    proto.String("Order"),
						Options: billingOpts,
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
				Name: proto.String("ShippingScope"),
				NestedType: []*descriptorpb.DescriptorProto{
					{
						Name:    proto.String("Order"),
						Options: shippingOpts,
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
				Name:    proto.String("AuditEntry"),
				Options: auditOpts,
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:     proto.String("order_id"),
						Number:   proto.Int32(1),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						JsonName: proto.String("orderId"),
						Options:  ambigFKOpts,
					},
				},
			},
		},
	}

	genAmbig := newPlugin(t, &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{ambigFDP}}, []string{"ambig_fk.proto"})
	if err := NewRunner().GenerateFile(genAmbig, genAmbig.Files[0], &Options{
		Lang:         "go",
		Backend:      "postgres",
		PostgresMode: "ddl",
		Domain:       false,
	}); err == nil || !strings.Contains(err.Error(), "ambiguous unprefixed ref_table") {
		t.Errorf("expected ambiguous unprefixed ref_table error, got %v", err)
	}
}
