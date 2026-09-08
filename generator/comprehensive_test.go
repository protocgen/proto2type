package generator

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
)

// ---------------------------------------------------------------------------
// IR Service Tests
// ---------------------------------------------------------------------------

func TestBuildDomainFile_ServiceIR(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"service.proto"})

	var file *DomainFile
	var err error
	for _, f := range gen.Files {
		if f.Generate && f.Desc.Path() == "service.proto" {
			file, err = BuildDomainFile(f, &Options{Domain: true})
			if err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if file == nil {
		t.Fatal("service.proto not found in generated files")
	}

	// Should have exactly one service.
	if len(file.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(file.Services))
	}

	svc := file.Services[0]
	if svc.Name != "UserService" {
		t.Errorf("service name = %q, want %q", svc.Name, "UserService")
	}
	if svc.FullName != "test.v1.UserService" {
		t.Errorf("service full name = %q, want %q", svc.FullName, "test.v1.UserService")
	}
	if svc.Comment == "" {
		t.Error("service comment is empty, expected proto comment")
	}

	// Should have 3 methods.
	if len(svc.Methods) != 3 {
		t.Fatalf("expected 3 methods, got %d", len(svc.Methods))
	}

	wantMethods := []struct {
		name       string
		inputType  string
		outputType string
	}{
		{"GetUser", "GetUserRequest", "GetUserResponse"},
		{"ListUsers", "ListUsersRequest", "ListUsersResponse"},
		{"UpdateUser", "UpdateUserRequest", "GetUserResponse"},
	}

	for i, wm := range wantMethods {
		m := svc.Methods[i]
		if m.Name != wm.name {
			t.Errorf("method[%d].Name = %q, want %q", i, m.Name, wm.name)
		}
		if m.InputType != wm.inputType {
			t.Errorf("method[%d].InputType = %q, want %q", i, m.InputType, wm.inputType)
		}
		if m.OutputType != wm.outputType {
			t.Errorf("method[%d].OutputType = %q, want %q", i, m.OutputType, wm.outputType)
		}
		if m.ClientStreaming {
			t.Errorf("method[%d] should not be client-streaming", i)
		}
		if m.ServerStreaming {
			t.Errorf("method[%d] should not be server-streaming", i)
		}
	}
}

func TestBuildDomainFile_ServiceIR_NoSkip(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"service.proto"})

	var file *DomainFile
	for _, f := range gen.Files {
		if f.Generate && f.Desc.Path() == "service.proto" {
			var err error
			file, err = BuildDomainFile(f, &Options{Domain: true})
			if err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if file == nil {
		t.Fatal("service.proto not found")
	}

	svc := file.Services[0]
	if svc.Skip {
		t.Error("service should not be skipped")
	}
	for _, m := range svc.Methods {
		if m.Skip {
			t.Errorf("method %q should not be skipped", m.Name)
		}
	}
}

// TestBuildDomainFile_ServiceCrossFileRefs verifies that service request/response
// messages that reference types from other proto files have MessageSourcePath set.
func TestBuildDomainFile_ServiceCrossFileRefs(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"service.proto"})

	var file *DomainFile
	var err error
	for _, f := range gen.Files {
		if f.Generate && f.Desc.Path() == "service.proto" {
			file, err = BuildDomainFile(f, &Options{Domain: true})
			if err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if file == nil {
		t.Fatal("service.proto not found")
	}

	// Find GetUserResponse — it should have a 'user' field with MessageSourcePath = "user.proto"
	var resp *DomainMessage
	for _, m := range file.Messages {
		if m.Name == "GetUserResponse" {
			resp = m
			break
		}
	}
	if resp == nil {
		t.Fatal("GetUserResponse not found")
	}

	var userField *DomainField
	for _, f := range resp.Fields {
		if f.Name == "user" {
			userField = f
			break
		}
	}
	if userField == nil {
		t.Fatal("'user' field not found in GetUserResponse")
	}

	if userField.Kind != FieldKindMessage {
		t.Errorf("user field kind = %v, want FieldKindMessage", userField.Kind)
	}
	if userField.MessageTypeName != "User" {
		t.Errorf("user field MessageTypeName = %q, want %q", userField.MessageTypeName, "User")
	}
	if userField.MessageSourcePath != "user.proto" {
		t.Errorf("user field MessageSourcePath = %q, want %q", userField.MessageSourcePath, "user.proto")
	}
}

// ---------------------------------------------------------------------------
// Service Generation Smoke Tests
// ---------------------------------------------------------------------------

// extractPluginOutput searches the plugin response for a file matching the suffix.
func extractPluginOutput(t *testing.T, gen *protogen.Plugin, suffix string) string {
	t.Helper()
	for _, gf := range gen.Response().File {
		if gf.GetName() != "" && strings.HasSuffix(gf.GetName(), suffix) {
			return gf.GetContent()
		}
	}
	return ""
}

func TestServiceProto_GoGeneration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"service.proto"})
	runner := NewRunner()

	for _, f := range gen.Files {
		if f.Generate && f.Desc.Path() == "service.proto" {
			err := runner.GenerateFile(gen, f, &Options{Lang: "go", Domain: true, GoPackage: "github.com/protocgen/proto2type/testdata/golden/go/gen;gen"})
			if err != nil {
				t.Fatalf("Go generation failed: %v", err)
			}
		}
	}

	content := extractPluginOutput(t, gen, ".type.go")
	if content == "" {
		t.Fatal("no Go output generated for service.proto")
	}
	if !strings.Contains(content, "UserServiceHandler") {
		t.Error("Go output missing UserServiceHandler interface")
	}
	if !strings.Contains(content, "GetUser") {
		t.Error("Go output missing GetUser method")
	}
	if !strings.Contains(content, "context.Context") {
		t.Error("Go output missing context.Context parameter")
	}
	if !strings.Contains(content, "ListUsers") {
		t.Error("Go output missing ListUsers method")
	}
	if !strings.Contains(content, "UpdateUser") {
		t.Error("Go output missing UpdateUser method")
	}
}

func TestServiceProto_TSGeneration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"service.proto"})

	for _, f := range gen.Files {
		if f.Generate && f.Desc.Path() == "service.proto" {
			err := generateTypeScript(gen, f, &Options{Domain: true})
			if err != nil {
				t.Fatalf("TS generation failed: %v", err)
			}
		}
	}

	content := extractPluginOutput(t, gen, ".type.ts")
	if content == "" {
		t.Fatal("no TS output generated for service.proto")
	}
	// TS should have the messages from service.proto at minimum.
	if !strings.Contains(content, "GetUserRequest") {
		t.Error("TS output missing GetUserRequest")
	}
}

func TestServiceProto_PythonGeneration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"service.proto"})

	for _, f := range gen.Files {
		if f.Generate && f.Desc.Path() == "service.proto" {
			err := generatePython(gen, f, &Options{Domain: true})
			if err != nil {
				t.Fatalf("Python generation failed: %v", err)
			}
		}
	}

	content := extractPluginOutput(t, gen, "_pydantic.py")
	if content == "" {
		t.Fatal("no Python output generated for service.proto")
	}
	if !strings.Contains(content, "GetUserRequest") {
		t.Error("Python output missing GetUserRequest")
	}
}

func TestServiceProto_RustGeneration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"service.proto"})

	for _, f := range gen.Files {
		if f.Generate && f.Desc.Path() == "service.proto" {
			err := generateRust(gen, f, &Options{Domain: true})
			if err != nil {
				t.Fatalf("Rust generation failed: %v", err)
			}
		}
	}

	content := extractPluginOutput(t, gen, ".type.rs")
	if content == "" {
		t.Fatal("no Rust output generated for service.proto")
	}
	if !strings.Contains(content, "GetUserRequest") {
		t.Error("Rust output missing GetUserRequest")
	}
}

func TestServiceProto_KotlinGeneration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"service.proto"})

	for _, f := range gen.Files {
		if f.Generate && f.Desc.Path() == "service.proto" {
			err := generateKotlin(gen, f, &Options{Domain: true})
			if err != nil {
				t.Fatalf("Kotlin generation failed: %v", err)
			}
		}
	}

	content := extractPluginOutput(t, gen, ".type.kt")
	if content == "" {
		t.Fatal("no Kotlin output generated for service.proto")
	}
	if !strings.Contains(content, "GetUserRequest") {
		t.Error("Kotlin output missing GetUserRequest")
	}
}

// ---------------------------------------------------------------------------
// Go Postgres Backend Tests
// ---------------------------------------------------------------------------

func TestGoPostgresBackendGeneration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	runner := NewRunner()

	for _, f := range gen.Files {
		if f.Generate {
			err := runner.GenerateFile(gen, f, &Options{
				Lang:    "go",
				Domain:  false,
				Backend: "postgres",
			})
			if err != nil {
				t.Fatalf("generateGo with backend=postgres failed: %v", err)
			}
			break
		}
	}

	content := extractPluginOutput(t, gen, "_postgres.type.go")
	if content == "" {
		t.Fatal("no _postgres.type.go file generated")
	}

	// Should contain db tags.
	if !strings.Contains(content, `db:"`) {
		t.Error("postgres output should contain db: tags")
	}
	// Should NOT contain bson tags.
	if strings.Contains(content, `bson:"`) {
		t.Error("postgres output should not contain bson: tags")
	}
	// Should contain Postgres struct types.
	if !strings.Contains(content, "UserPostgres") {
		t.Error("postgres output should contain UserPostgres struct")
	}
	// Should have ToDomain/FromDomain methods.
	if !strings.Contains(content, "ToDomain") {
		t.Error("postgres output should contain ToDomain method")
	}
	if !strings.Contains(content, "FromDomain") {
		t.Error("postgres output should contain FromDomain method")
	}
}

func TestGoPostgresBackendGeneration_Catalog(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"catalog.proto"})
	runner := NewRunner()

	for _, f := range gen.Files {
		if f.Generate && f.Desc.Path() == "catalog.proto" {
			err := runner.GenerateFile(gen, f, &Options{
				Lang:    "go",
				Domain:  false,
				Backend: "postgres",
			})
			if err != nil {
				t.Fatalf("generateGo with backend=postgres failed: %v", err)
			}
		}
	}

	content := extractPluginOutput(t, gen, "_postgres.type.go")
	if content == "" {
		t.Fatal("no _postgres.type.go file generated for catalog")
	}
	if !strings.Contains(content, "ModelCatalogEntryPostgres") {
		t.Error("postgres output should contain ModelCatalogEntryPostgres")
	}
}

// ---------------------------------------------------------------------------
// Cross-Language FieldMask Consistency
// ---------------------------------------------------------------------------

func TestFieldMask_GoAndTSConsistency(t *testing.T) {
	fds := buildFileDescriptorSet(t)

	// Go FieldMask.
	goGen := newPlugin(t, fds, []string{"user.proto"})
	runner := NewRunner()
	for _, f := range goGen.Files {
		if f.Generate && f.Desc.Path() == "user.proto" {
			err := runner.GenerateFile(goGen, f, &Options{Lang: "go", Domain: true, GoPackage: "github.com/protocgen/proto2type/testdata/golden/go/gen;gen"})
			if err != nil {
				t.Fatalf("Go generation failed: %v", err)
			}
		}
	}
	goContent := extractPluginOutput(t, goGen, ".type.go")
	if !strings.Contains(goContent, "ApplyFieldMask") {
		t.Error("Go output missing ApplyFieldMask")
	}

	// TS FieldMask.
	tsGen := newPlugin(t, fds, []string{"user.proto"})
	for _, f := range tsGen.Files {
		if f.Generate && f.Desc.Path() == "user.proto" {
			err := generateTypeScript(tsGen, f, &Options{Domain: true})
			if err != nil {
				t.Fatalf("TS generation failed: %v", err)
			}
		}
	}
	tsContent := extractPluginOutput(t, tsGen, ".type.ts")
	if !strings.Contains(tsContent, "applyFieldMask") {
		t.Error("TS output missing applyFieldMask")
	}
}

// ---------------------------------------------------------------------------
// IR Completeness Tests
// ---------------------------------------------------------------------------

func TestIR_AllUserFieldsCovered(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})

	var file *DomainFile
	for _, f := range gen.Files {
		if f.Generate && f.Desc.Path() == "user.proto" {
			var err error
			file, err = BuildDomainFile(f, &Options{Domain: true})
			if err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if file == nil {
		t.Fatal("user.proto not found")
	}

	var user *DomainMessage
	for _, m := range file.Messages {
		if m.Name == "User" {
			user = m
			break
		}
	}
	if user == nil {
		t.Fatal("User message not found")
	}

	// Basic sanity: User should have > 10 fields.
	if len(user.Fields) < 10 {
		t.Errorf("User should have at least 10 fields, got %d", len(user.Fields))
	}

	// All fields should have a valid Name.
	for _, f := range user.Fields {
		if f.Name == "" {
			t.Error("found a field with empty Name")
		}
	}

	// Check specific well-known types are correctly mapped.
	fieldKinds := make(map[string]FieldKind)
	for _, f := range user.Fields {
		fieldKinds[f.Name] = f.Kind
	}

	checks := map[string]FieldKind{
		"created_at":      FieldKindTimestamp,
		"session_timeout": FieldKindDuration,
		"address":         FieldKindMessage,
		"metadata":        FieldKindMessage, // maps are messages
	}

	for name, wantKind := range checks {
		got, ok := fieldKinds[name]
		if !ok {
			t.Errorf("field %q not found in IR", name)
			continue
		}
		if got != wantKind {
			t.Errorf("field %q kind = %v, want %v", name, got, wantKind)
		}
	}
}

func TestIR_ComplexProtoMessages(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"complex.proto"})

	var file *DomainFile
	for _, f := range gen.Files {
		if f.Generate && f.Desc.Path() == "complex.proto" {
			var err error
			file, err = BuildDomainFile(f, &Options{Domain: true})
			if err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if file == nil {
		t.Fatal("complex.proto not found")
	}

	if len(file.Messages) == 0 {
		t.Fatal("complex.proto should produce at least one message")
	}

	// Verify all messages have non-empty FullName.
	for _, m := range file.Messages {
		if m.FullName == "" {
			t.Errorf("message %q has empty FullName", m.Name)
		}
	}
}

func TestIR_EdgeCasesMessages(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"edge_cases.proto"})

	var file *DomainFile
	for _, f := range gen.Files {
		if f.Generate && f.Desc.Path() == "edge_cases.proto" {
			var err error
			file, err = BuildDomainFile(f, &Options{Domain: true})
			if err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if file == nil {
		t.Fatal("edge_cases.proto not found")
	}

	if len(file.Messages) == 0 {
		t.Fatal("edge_cases.proto should produce at least one message")
	}
}

// ---------------------------------------------------------------------------
// Multi-Backend Generation Tests
// ---------------------------------------------------------------------------

func TestGoAllBackends_Generate(t *testing.T) {
	backends := []string{"firestore", "mongo", "postgres"}
	fds := buildFileDescriptorSet(t)

	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			gen := newPlugin(t, fds, []string{"user.proto"})
			runner := NewRunner()
			for _, f := range gen.Files {
				if f.Generate && f.Desc.Path() == "user.proto" {
					err := runner.GenerateFile(gen, f, &Options{
						Lang:    "go",
						Domain:  false,
						Backend: backend,
					})
					if err != nil {
						t.Fatalf("generateGo with backend=%s failed: %v", backend, err)
					}
				}
			}
			// Verify at least one file was generated.
			resp := gen.Response()
			if len(resp.File) == 0 {
				t.Fatalf("no files generated for backend=%s", backend)
			}
		})
	}
}

func TestRustAllBackends_Generate(t *testing.T) {
	backends := []struct {
		name string
		opts *Options
	}{
		{"sqlite", &Options{Lang: "rust", Domain: false, Backend: "sqlite"}},
		{"buffa", &Options{Lang: "rust", Domain: false, Backend: "buffa", BufModule: "crate::proto"}},
	}
	fds := buildFileDescriptorSet(t)

	for _, backend := range backends {
		t.Run(backend.name, func(t *testing.T) {
			gen := newPlugin(t, fds, []string{"user.proto"})
			runner := NewRunner()
			for _, f := range gen.Files {
				if f.Generate && f.Desc.Path() == "user.proto" {
					err := runner.GenerateFile(gen, f, backend.opts)
					if err != nil {
						t.Fatalf("generateRust with backend=%s failed: %v", backend.name, err)
					}
				}
			}
			resp := gen.Response()
			if len(resp.File) == 0 {
				t.Fatalf("no files generated for backend=%s", backend.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Domain Generation Tests
// ---------------------------------------------------------------------------

func TestAllLanguages_DomainGeneration(t *testing.T) {
	langs := []struct {
		name   string
		lang   string
		suffix string
	}{
		{"Go", "go", ".type.go"},
		{"TypeScript", "typescript", ".type.ts"},
		{"Python", "python", "_pydantic.py"},
		{"Rust", "rust", ".type.rs"},
		{"Kotlin", "kotlin", ".type.kt"},
	}

	fds := buildFileDescriptorSet(t)

	for _, lang := range langs {
		t.Run(lang.name, func(t *testing.T) {
			gen := newPlugin(t, fds, []string{"user.proto"})
			runner := NewRunner()
			for _, f := range gen.Files {
				if f.Generate && f.Desc.Path() == "user.proto" {
					opts := &Options{
						Lang:   lang.lang,
						Domain: true,
					}
					// Go requires a separate go_package to avoid collision with proto types.
					if lang.lang == "go" {
						opts.GoPackage = "github.com/protocgen/proto2type/testdata/golden/go/gen;gen"
					}
					err := runner.GenerateFile(gen, f, opts)
					if err != nil {
						t.Fatalf("%s generation failed: %v", lang.name, err)
					}
				}
			}

			content := extractPluginOutput(t, gen, lang.suffix)
			if content == "" {
				t.Fatalf("no %s output for user.proto (suffix=%s)", lang.name, lang.suffix)
			}
			// All domain outputs should contain User type name.
			if !strings.Contains(content, "User") {
				t.Errorf("%s output missing User type", lang.name)
			}
		})
	}
}
