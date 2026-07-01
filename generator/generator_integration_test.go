package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// buildFileDescriptorSet runs `buf build` on the testdata/proto directory and
// returns the compiled FileDescriptorSet. It skips the test if buf is not available.
func buildFileDescriptorSet(t *testing.T) *descriptorpb.FileDescriptorSet {
	t.Helper()

	tmpFile, err := os.CreateTemp(t.TempDir(), "fdset-*.bin")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("closing temp file: %v", err)
	}

	// Check if buf is available; skip if not (e.g. in CI build-and-test job)
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skip("buf not found in PATH; skipping integration test")
	}

	cmd := exec.Command("buf", "build", "-o", tmpFile.Name())
	cmd.Dir = filepath.Join("..", "testdata", "proto")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("buf build failed: %v\n%s", err, out)
	}

	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatalf("reading fdset: %v", err)
	}

	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(data, &fds); err != nil {
		t.Fatalf("unmarshalling fdset: %v", err)
	}
	return &fds
}

// newPlugin creates a protogen.Plugin from a FileDescriptorSet, requesting
// code generation for the given file names.
func newPlugin(t *testing.T, fds *descriptorpb.FileDescriptorSet, filesToGenerate []string) *protogen.Plugin {
	t.Helper()

	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: filesToGenerate,
		ProtoFile:      fds.File,
	}

	gen, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatalf("protogen.Options{}.New: %v", err)
	}
	return gen
}

// findMessage finds a top-level message by short name in the plugin's files.
func findMessage(t *testing.T, gen *protogen.Plugin, msgName string) *protogen.Message {
	t.Helper()
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		for _, msg := range f.Messages {
			if string(msg.Desc.Name()) == msgName {
				return msg
			}
		}
	}
	t.Fatalf("message %q not found", msgName)
	return nil
}

// findField finds a field by name within a message.
func findField(t *testing.T, msg *protogen.Message, fieldName string) *protogen.Field {
	t.Helper()
	for _, f := range msg.Fields {
		if string(f.Desc.Name()) == fieldName {
			return f
		}
	}
	t.Fatalf("field %q not found in message %q", fieldName, msg.Desc.Name())
	return nil
}

// findEnum finds a top-level enum by short name in the plugin's files.
func findEnum(t *testing.T, gen *protogen.Plugin, enumName string) *protogen.Enum {
	t.Helper()
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		for _, e := range f.Enums {
			if string(e.Desc.Name()) == enumName {
				return e
			}
		}
	}
	t.Fatalf("enum %q not found", enumName)
	return nil
}

// --- Integration Tests ---

func TestRustDomainFieldType_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	msg := findMessage(t, gen, "User")
	opts := &Options{Lang: "rust", Domain: true}

	for _, field := range msg.Fields {
		fieldName := string(field.Desc.Name())

		// Skip oneof members (they are handled as oneof groups)
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}

		t.Run(fieldName, func(t *testing.T) {
			got := rustDomainFieldType(field, opts)
			if got == "" {
				t.Errorf("rustDomainFieldType(%q) returned empty string", fieldName)
			}
			t.Logf("  %s -> %s", fieldName, got)
		})
	}
}

func TestRustDomainFieldType_SpecificTypes(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	msg := findMessage(t, gen, "User")
	opts := &Options{Lang: "rust", Domain: true}

	tests := []struct {
		field string
		want  string
	}{
		{"email", "String"},
		{"created_at", "DateTime<Utc>"},
		{"active", "bool"},
		{"age", "i32"},
		{"roles", "Vec<String>"},
		{"metadata", "HashMap<String, String>"},
		{"address", "Option<Box<Address>>"},
		{"session_timeout", "i64"},
		{"phone", "Option<String>"},
		{"avatar", "Vec<u8>"},
		{"nickname", "Option<String>"},
		{"status", "UserStatus"},
		{"tags", "Vec<Tag>"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			field := findField(t, msg, tt.field)
			got := rustDomainFieldType(field, opts)
			if got != tt.want {
				t.Errorf("rustDomainFieldType(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

func TestRustEnumGeneration_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	msg := findMessage(t, gen, "User")
	opts := &Options{Lang: "rust", Domain: true}

	// status field should map to UserStatus (enum name), NOT bare i32
	statusField := findField(t, msg, "status")
	got := rustDomainFieldType(statusField, opts)
	if got == "i32" {
		t.Errorf("enum field 'status' mapped to bare i32, expected enum type name")
	}
	if got != "UserStatus" {
		t.Errorf("enum field 'status' = %q, want %q", got, "UserStatus")
	}

	// With EnumAsString option, should map to String
	optsStr := &Options{Lang: "rust", Domain: true, EnumAsString: true}
	gotStr := rustDomainFieldType(statusField, optsStr)
	if gotStr != "String" {
		t.Errorf("enum field 'status' with EnumAsString = %q, want %q", gotStr, "String")
	}

	// Verify the enum type name function directly
	enumTypeName := rustEnumTypeName(statusField)
	if enumTypeName != "UserStatus" {
		t.Errorf("rustEnumTypeName(status) = %q, want %q", enumTypeName, "UserStatus")
	}

	// Verify stripEnumPrefix on actual enum values
	enum := findEnum(t, gen, "UserStatus")
	for _, val := range enum.Values {
		stripped := stripEnumPrefix("UserStatus", string(val.Desc.Name()))
		if stripped == "" {
			t.Errorf("stripEnumPrefix(%q, %q) returned empty", "UserStatus", val.Desc.Name())
		}
		// Verify it doesn't retain the full prefix
		if strings.HasPrefix(stripped, "USER_STATUS_") {
			t.Errorf("stripEnumPrefix didn't strip prefix: got %q", stripped)
		}
		t.Logf("  %s -> %s", val.Desc.Name(), stripped)
	}
}

func TestRustOneofDetection_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	msg := findMessage(t, gen, "User")

	// Verify hasOneofFields detects the oneof in User
	if !hasOneofFields(msg) {
		t.Errorf("hasOneofFields(User) = false, want true")
	}

	// Verify oneof fields are detected correctly
	oneofFields := 0
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			oneofFields++
		}
	}
	if oneofFields == 0 {
		t.Errorf("no oneof fields found in User message")
	}
	if oneofFields != 2 {
		t.Errorf("expected 2 oneof fields (contact_email, contact_phone), got %d", oneofFields)
	}

	// Verify the oneof variant types
	opts := &Options{Lang: "rust", Domain: true}
	for _, field := range msg.Fields {
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			variantType := rustOneofVariantType(field, opts)
			if variantType == "" {
				t.Errorf("rustOneofVariantType(%q) returned empty", field.Desc.Name())
			}
			// Both contact_email and contact_phone are strings
			if variantType != "String" {
				t.Errorf("rustOneofVariantType(%q) = %q, want %q", field.Desc.Name(), variantType, "String")
			}
		}
	}

	// Verify Address does NOT have oneofs
	addr := findMessage(t, gen, "Address")
	if hasOneofFields(addr) {
		t.Errorf("hasOneofFields(Address) = true, want false")
	}
}

func TestRustMessageName_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto", "catalog.proto"})

	tests := []struct {
		msgName string
		want    string
	}{
		{"User", "User"},
		{"Address", "Address"},
		{"Tag", "Tag"},
		{"ModelCatalogEntry", "ModelCatalogEntry"},
	}

	for _, tt := range tests {
		t.Run(tt.msgName, func(t *testing.T) {
			msg := findMessage(t, gen, tt.msgName)
			got := rustMessageName(msg)
			if got != tt.want {
				t.Errorf("rustMessageName(%q) = %q, want %q", tt.msgName, got, tt.want)
			}
		})
	}
}

func TestRustWKTDetection_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	msg := findMessage(t, gen, "User")

	// created_at is google.protobuf.Timestamp
	createdAt := findField(t, msg, "created_at")
	if !isWellKnownTimestamp(createdAt) {
		t.Errorf("isWellKnownTimestamp(created_at) = false, want true")
	}

	// session_timeout is google.protobuf.Duration
	sessionTimeout := findField(t, msg, "session_timeout")
	if !isWellKnownDuration(sessionTimeout) {
		t.Errorf("isWellKnownDuration(session_timeout) = false, want true")
	}

	// nickname is google.protobuf.StringValue
	nickname := findField(t, msg, "nickname")
	if !isWellKnownWrapper(nickname) {
		t.Errorf("isWellKnownWrapper(nickname) = false, want true")
	}

	// email is NOT a WKT
	email := findField(t, msg, "email")
	if isWellKnownTimestamp(email) {
		t.Errorf("isWellKnownTimestamp(email) = true, want false")
	}
	if isWellKnownDuration(email) {
		t.Errorf("isWellKnownDuration(email) = true, want false")
	}
	if isWellKnownWrapper(email) {
		t.Errorf("isWellKnownWrapper(email) = true, want false")
	}
}

func TestImportScanning_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	msg := findMessage(t, gen, "User")

	var needsChrono, needsHashMap, needsSerdeJSON bool
	scanRustImports(msg, &needsChrono, &needsHashMap, &needsSerdeJSON)

	if !needsChrono {
		t.Errorf("scanRustImports: needsChrono = false, want true (User has Timestamp)")
	}
	if !needsHashMap {
		t.Errorf("scanRustImports: needsHashMap = false, want true (User has map field)")
	}

	// Address has no Timestamps, maps, or JSON WKTs
	addr := findMessage(t, gen, "Address")
	var addrChrono, addrHashMap, addrSerdeJSON bool
	scanRustImports(addr, &addrChrono, &addrHashMap, &addrSerdeJSON)

	if addrChrono {
		t.Errorf("scanRustImports(Address): needsChrono = true, want false")
	}
	if addrHashMap {
		t.Errorf("scanRustImports(Address): needsHashMap = true, want false")
	}
	if addrSerdeJSON {
		t.Errorf("scanRustImports(Address): needsSerdeJSON = true, want false")
	}
}

func TestRustSqliteConversions_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	msg := findMessage(t, gen, "User")
	opts := &Options{Lang: "rust", Domain: true, Backend: "sqlite"}

	for _, field := range msg.Fields {
		fieldName := string(field.Desc.Name())

		// Skip oneof members
		if field.Oneof != nil && !field.Oneof.Desc.IsSynthetic() {
			continue
		}

		rustFieldName := escapeRustKeyword(toSnakeCase(fieldName))

		t.Run(fieldName+"/sqlite_type", func(t *testing.T) {
			got := rustSqliteFieldType(field, opts)
			if got == "" {
				t.Errorf("rustSqliteFieldType(%q) returned empty", fieldName)
			}
			t.Logf("  sqlite type: %s -> %s", fieldName, got)
		})

		t.Run(fieldName+"/to_domain", func(t *testing.T) {
			got := rustSqliteToDomainConversion(field, rustFieldName, opts)
			if got == "" {
				t.Errorf("rustSqliteToDomainConversion(%q) returned empty", fieldName)
			}
			t.Logf("  to_domain: %s -> %s", fieldName, got)
		})

		t.Run(fieldName+"/from_domain", func(t *testing.T) {
			got := rustSqliteFromDomainConversion(field, rustFieldName, opts)
			if got == "" {
				t.Errorf("rustSqliteFromDomainConversion(%q) returned empty", fieldName)
			}
			t.Logf("  from_domain: %s -> %s", fieldName, got)
		})

		t.Run(fieldName+"/into_domain", func(t *testing.T) {
			got := rustSqliteIntoDomainConversion(field, rustFieldName, opts)
			if got == "" {
				t.Errorf("rustSqliteIntoDomainConversion(%q) returned empty", fieldName)
			}
			t.Logf("  into_domain: %s -> %s", fieldName, got)
		})
	}
}

func TestRustSqliteSpecificConversions_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	msg := findMessage(t, gen, "User")
	opts := &Options{Lang: "rust", Domain: true, Backend: "sqlite"}

	tests := []struct {
		field    string
		sqlType  string
		toDomain string // substring to check
	}{
		{"email", "String", "clone"},
		{"created_at", "i64", "epoch_ms_to_datetime"},
		{"active", "bool", "self.active"},
		{"age", "i32", "self.age"},
		{"roles", "String", "serde_json::from_str"},
		{"metadata", "String", "serde_json::from_str"},
		{"address", "Option<String>", "serde_json::from_str"},
		{"session_timeout", "i64", "self.session_timeout"},
		{"phone", "Option<String>", "self.phone"},
		{"avatar", "Vec<u8>", "clone"},
		{"nickname", "Option<String>", "clone"},
		{"status", "i32", "from_i32"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			field := findField(t, msg, tt.field)
			rustFieldName := escapeRustKeyword(toSnakeCase(tt.field))

			gotType := rustSqliteFieldType(field, opts)
			if gotType != tt.sqlType {
				t.Errorf("rustSqliteFieldType(%q) = %q, want %q", tt.field, gotType, tt.sqlType)
			}

			gotConv := rustSqliteToDomainConversion(field, rustFieldName, opts)
			if !strings.Contains(gotConv, tt.toDomain) {
				t.Errorf("rustSqliteToDomainConversion(%q) = %q, want substring %q", tt.field, gotConv, tt.toDomain)
			}
		})
	}
}

// --- Pure function tests (no protoc needed) ---

func TestStripEnumPrefix(t *testing.T) {
	tests := []struct {
		enumName  string
		valueName string
		want      string
	}{
		{"UserStatus", "USER_STATUS_UNSPECIFIED", "Unspecified"},
		{"UserStatus", "USER_STATUS_ACTIVE", "Active"},
		{"UserStatus", "USER_STATUS_SUSPENDED", "Suspended"},
		{"UserStatus", "USER_STATUS_DELETED", "Deleted"},
		// Edge cases
		{"Foo", "FOO_BAR", "Bar"},
		{"Foo", "UNRELATED_NAME", "UnrelatedName"},
		// No prefix match
		{"MyEnum", "SOME_VALUE", "SomeValue"},
		// Exact prefix match (value = prefix minus trailing _)
		{"A", "A_B", "B"},
	}

	for _, tt := range tests {
		name := tt.enumName + "/" + tt.valueName
		t.Run(name, func(t *testing.T) {
			got := stripEnumPrefix(tt.enumName, tt.valueName)
			if got != tt.want {
				t.Errorf("stripEnumPrefix(%q, %q) = %q, want %q", tt.enumName, tt.valueName, got, tt.want)
			}
		})
	}
}

func TestRustKeywordEscaping_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"keywords.proto"})
	msg := findMessage(t, gen, "KeywordFields")
	opts := &Options{Lang: "rust", Domain: true}

	keywordFields := map[string]string{
		"type":  "r#type",
		"self":  "self_",
		"match": "r#match",
		"mod":   "r#mod",
		"ref":   "r#ref",
		"super": "super_",
	}

	for protoName, wantRustName := range keywordFields {
		t.Run(protoName, func(t *testing.T) {
			field := findField(t, msg, protoName)
			gotRustName := escapeRustKeyword(toSnakeCase(string(field.Desc.Name())))
			if gotRustName != wantRustName {
				t.Errorf("escapeRustKeyword(%q) = %q, want %q", protoName, gotRustName, wantRustName)
			}

			// Also verify the field gets a valid Rust type
			got := rustDomainFieldType(field, opts)
			if got == "" {
				t.Errorf("rustDomainFieldType(%q) returned empty", protoName)
			}
		})
	}
}

func TestCatalogDocumentID_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"catalog.proto"})
	msg := findMessage(t, gen, "ModelCatalogEntry")

	// model_id should be detected as document_id
	modelID := findField(t, msg, "model_id")
	if !isDocumentID(modelID) {
		t.Errorf("isDocumentID(model_id) = false, want true")
	}

	// provider should NOT be document_id
	provider := findField(t, msg, "provider")
	if isDocumentID(provider) {
		t.Errorf("isDocumentID(provider) = true, want false")
	}

	// Verify all fields get valid types
	opts := &Options{Lang: "rust", Domain: true}
	for _, field := range msg.Fields {
		fieldName := string(field.Desc.Name())
		t.Run(fieldName, func(t *testing.T) {
			got := rustDomainFieldType(field, opts)
			if got == "" {
				t.Errorf("rustDomainFieldType(%q) returned empty", fieldName)
			}
		})
	}
}

func TestRustSqliteCatalogConversions_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"catalog.proto"})
	msg := findMessage(t, gen, "ModelCatalogEntry")
	opts := &Options{Lang: "rust", Domain: true, Backend: "sqlite"}

	for _, field := range msg.Fields {
		fieldName := string(field.Desc.Name())
		rustFieldName := escapeRustKeyword(toSnakeCase(fieldName))

		t.Run(fieldName, func(t *testing.T) {
			sqlType := rustSqliteFieldType(field, opts)
			if sqlType == "" {
				t.Errorf("rustSqliteFieldType(%q) returned empty", fieldName)
			}

			// Document ID fields are excluded from SQLite conversions but
			// we can still test the functions don't panic
			toDomain := rustSqliteToDomainConversion(field, rustFieldName, opts)
			if toDomain == "" {
				t.Errorf("rustSqliteToDomainConversion(%q) returned empty", fieldName)
			}

			fromDomain := rustSqliteFromDomainConversion(field, rustFieldName, opts)
			if fromDomain == "" {
				t.Errorf("rustSqliteFromDomainConversion(%q) returned empty", fieldName)
			}

			intoDomain := rustSqliteIntoDomainConversion(field, rustFieldName, opts)
			if intoDomain == "" {
				t.Errorf("rustSqliteIntoDomainConversion(%q) returned empty", fieldName)
			}
		})
	}
}

func TestRustNestedMessageDetection_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	msg := findMessage(t, gen, "User")

	// address is a nested message (non-WKT)
	address := findField(t, msg, "address")
	if !isNestedMessage(address) {
		t.Errorf("isNestedMessage(address) = false, want true")
	}

	// created_at is a Timestamp WKT, NOT a "nested message"
	createdAt := findField(t, msg, "created_at")
	if isNestedMessage(createdAt) {
		t.Errorf("isNestedMessage(created_at) = true, want false (it's a WKT)")
	}

	// email is a scalar, not a nested message
	email := findField(t, msg, "email")
	if isNestedMessage(email) {
		t.Errorf("isNestedMessage(email) = true, want false (it's a scalar)")
	}

	// tags is repeated message, not a "nested message" (isNestedMessage checks singular)
	tags := findField(t, msg, "tags")
	if isNestedMessage(tags) {
		t.Errorf("isNestedMessage(tags) = true, want false (it's repeated)")
	}
}

// --- IR-based Integration Tests ---

func TestRustDomainFieldTypeFromIR_SpecificTypes(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	opts := &Options{Lang: "rust", Domain: true}
	user := irFindDomainMessageInPlugin(t, gen, opts, "User")

	tests := []struct {
		field string
		want  string
	}{
		{"email", "String"},
		{"created_at", "DateTime<Utc>"},
		{"active", "bool"},
		{"age", "i32"},
		{"roles", "Vec<String>"},
		{"metadata", "HashMap<String, String>"},
		{"address", "Option<Box<Address>>"},
		{"session_timeout", "i64"},
		{"phone", "Option<String>"},
		{"avatar", "Vec<u8>"},
		{"nickname", "Option<String>"},
		{"status", "UserStatus"},
		{"tags", "Vec<Tag>"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			df := irFindField(t, user, tt.field)
			got := rustDomainFieldTypeFromIR(df)
			if got != tt.want {
				t.Errorf("rustDomainFieldTypeFromIR(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

func TestIRScanRustImports_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	opts := &Options{Lang: "rust", Domain: true}

	// User has Timestamp and map fields.
	user := irFindDomainMessageInPlugin(t, gen, opts, "User")
	var needsChrono, needsHashMap bool
	irScanRustImports(user, &needsChrono, &needsHashMap)

	if !needsChrono {
		t.Errorf("irScanRustImports(User): needsChrono = false, want true (User has Timestamp)")
	}
	if !needsHashMap {
		t.Errorf("irScanRustImports(User): needsHashMap = false, want true (User has map field)")
	}

	// Address has no Timestamps, maps, or JSON WKTs — all should be false.
	addr := irFindDomainMessageInPlugin(t, gen, opts, "Address")
	var addrChrono, addrHashMap bool
	irScanRustImports(addr, &addrChrono, &addrHashMap)

	if addrChrono {
		t.Errorf("irScanRustImports(Address): needsChrono = true, want false")
	}
	if addrHashMap {
		t.Errorf("irScanRustImports(Address): needsHashMap = true, want false")
	}
}

func TestRustOneofVariantTypeFromIR_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	opts := &Options{Lang: "rust", Domain: true}
	user := irFindDomainMessageInPlugin(t, gen, opts, "User")

	if len(user.Oneofs) != 1 {
		t.Fatalf("expected 1 DomainOneof in User, got %d", len(user.Oneofs))
	}

	oneof := user.Oneofs[0]
	for _, v := range oneof.Variants {
		t.Run(v.Name, func(t *testing.T) {
			got := rustOneofVariantTypeFromIR(v)
			// Both contact_email and contact_phone are strings.
			if got != "String" {
				t.Errorf("rustOneofVariantTypeFromIR(%q) = %q, want %q", v.Name, got, "String")
			}
		})
	}
}

func TestRustDomainFieldTypeFromIR_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	opts := &Options{Lang: "rust", Domain: true}
	user := irFindDomainMessageInPlugin(t, gen, opts, "User")

	for _, f := range user.Fields {
		// Skip collapsed oneof placeholders.
		if f.IsOneof {
			continue
		}

		t.Run(f.Name, func(t *testing.T) {
			got := rustDomainFieldTypeFromIR(f)
			if got == "" {
				t.Errorf("rustDomainFieldTypeFromIR(%q) returned empty string", f.Name)
			}
			t.Logf("  %s -> %s", f.Name, got)
		})
	}
}

func TestRustExhaustiveOption_ZeroValue(t *testing.T) {
	// Verify that the zero-value Options{} defaults to RustExhaustive=false,
	// which means #[non_exhaustive] IS emitted (the safe default).
	opts := Options{}
	if opts.RustExhaustive {
		t.Error("Options{}.RustExhaustive should be false (zero value), so #[non_exhaustive] is emitted by default")
	}
}

func TestRustExhaustiveOption_GeneratedOutput(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})

	tests := []struct {
		name          string
		exhaustive    bool
		wantAttribute bool
	}{
		{"default emits non_exhaustive", false, true},
		{"exhaustive omits non_exhaustive", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := &Options{Lang: "rust", Domain: true, RustExhaustive: tt.exhaustive}
			for _, f := range gen.Files {
				if !f.Generate {
					continue
				}
				g := gen.NewGeneratedFile("test_"+tt.name+".rs", f.GoImportPath)
				df := BuildDomainFile(f, opts)
				for _, dm := range df.Messages {
					err := generateRustDomainMessageFromIR(g, dm, opts)
					if err != nil {
						t.Fatalf("generateRustDomainMessageFromIR: %v", err)
					}
				}
				content, err := g.Content()
				if err != nil {
					t.Fatalf("g.Content(): %v", err)
				}
				got := strings.Contains(string(content), "#[non_exhaustive]")
				if got != tt.wantAttribute {
					t.Errorf("#[non_exhaustive] present = %v, want %v (RustExhaustive=%v)", got, tt.wantAttribute, tt.exhaustive)
				}
			}
		})
	}
}
