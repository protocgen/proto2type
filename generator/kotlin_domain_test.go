package generator

import (
	"os"
	"strings"
	"testing"
)

// --- Kotlin Integration Tests ---

func TestKotlinFieldType_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	opts := &Options{Lang: "kotlin", Domain: true}

	// Build IR for the user.proto file.
	var file *DomainFile
	for _, f := range gen.Files {
		if f.Generate {
			file = BuildDomainFile(f, opts)
			break
		}
	}
	if file == nil {
		t.Fatal("no generated file found")
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

	tests := []struct {
		field string
		want  string
	}{
		{"email", "String"},
		{"created_at", "Instant"},
		{"active", "Boolean"},
		{"age", "Int"},
		{"roles", "List<String>"},
		{"metadata", "Map<String, String>"},
		{"address", "Address?"},
		{"session_timeout", "Duration"},
		{"phone", "String?"},
		{"avatar", "ByteArray"},
		{"nickname", "String?"},
		{"status", "UserStatus"},
		{"tags", "List<Tag>"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			var df *DomainField
			for _, f := range user.Fields {
				if f.Name == tt.field {
					df = f
					break
				}
			}
			if df == nil {
				t.Fatalf("field %q not found in IR", tt.field)
			}
			got := kotlinFieldType(*df)
			if got != tt.want {
				t.Errorf("kotlinFieldType(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

func TestKotlinFieldDefault_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	opts := &Options{Lang: "kotlin", Domain: true}

	var file *DomainFile
	for _, f := range gen.Files {
		if f.Generate {
			file = BuildDomainFile(f, opts)
			break
		}
	}
	if file == nil {
		t.Fatal("no generated file found")
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

	tests := []struct {
		field string
		want  string
	}{
		{"email", `""`},
		{"active", "false"},
		{"age", "0"},
		{"roles", "emptyList()"},
		{"metadata", "emptyMap()"},
		{"address", "null"},
		{"created_at", "Instant.fromEpochSeconds(0)"},
		{"session_timeout", "Duration.ZERO"},
		{"phone", "null"},
		{"avatar", "byteArrayOf()"},
		{"nickname", "null"},
		{"status", "UserStatus.entries.first()"},
		{"tags", "emptyList()"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			var df *DomainField
			for _, f := range user.Fields {
				if f.Name == tt.field {
					df = f
					break
				}
			}
			if df == nil {
				t.Fatalf("field %q not found in IR", tt.field)
			}
			got := kotlinDefaultValue(*df)
			if got != tt.want {
				t.Errorf("kotlinDefaultValue(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

func TestKotlinEnumGeneration_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	opts := &Options{Lang: "kotlin", Domain: true}

	var file *DomainFile
	for _, f := range gen.Files {
		if f.Generate {
			file = BuildDomainFile(f, opts)
			break
		}
	}
	if file == nil {
		t.Fatal("no generated file found")
	}

	if len(file.Enums) == 0 {
		t.Fatal("no enums found")
	}

	var userStatus *DomainEnum
	for _, e := range file.Enums {
		if e.Name == "UserStatus" {
			userStatus = e
			break
		}
	}
	if userStatus == nil {
		t.Fatal("UserStatus enum not found")
	}

	// Check enum values are properly stripped and converted to UPPER_SNAKE.
	wantValues := []struct {
		name       string
		kotlinName string
	}{
		{"Unspecified", "UNSPECIFIED"},
		{"Active", "ACTIVE"},
		{"Suspended", "SUSPENDED"},
		{"Deleted", "DELETED"},
	}

	for i, want := range wantValues {
		if i >= len(userStatus.Values) {
			t.Fatalf("not enough values: got %d, want at least %d", len(userStatus.Values), i+1)
		}
		got := userStatus.Values[i]
		if got.Name != want.name {
			t.Errorf("value[%d].Name = %q, want %q", i, got.Name, want.name)
		}
		kotlinName := pascalToUpperSnake(got.Name)
		if kotlinName != want.kotlinName {
			t.Errorf("pascalToUpperSnake(%q) = %q, want %q", got.Name, kotlinName, want.kotlinName)
		}
	}
}

func TestKotlinOneofDetection_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	opts := &Options{Lang: "kotlin", Domain: true}

	var user *DomainMessage
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		df := BuildDomainFile(f, opts)
		for _, m := range df.Messages {
			if m.Name == "User" {
				user = m
			}
		}
	}
	if user == nil {
		t.Fatal("User message not found")
	}

	if len(user.Oneofs) != 1 {
		t.Fatalf("User has %d oneofs, want 1", len(user.Oneofs))
	}

	oneof := user.Oneofs[0]
	if oneof.Name != "UserContactMethod" {
		t.Errorf("oneof.Name = %q, want %q", oneof.Name, "UserContactMethod")
	}

	if len(oneof.Variants) != 2 {
		t.Fatalf("oneof has %d variants, want 2", len(oneof.Variants))
	}

	for _, v := range oneof.Variants {
		variantType := kotlinOneofVariantType(v)
		if variantType != "String" {
			t.Errorf("kotlinOneofVariantType(%q) = %q, want %q", v.ProtoName, variantType, "String")
		}
	}

	// Verify the oneof field type.
	oneofType := kotlinOneofFieldType(oneof.Name)
	if oneofType != "UserContactMethod?" {
		t.Errorf("kotlinOneofFieldType(%q) = %q, want %q", oneof.Name, oneofType, "UserContactMethod?")
	}
}

func TestKotlinKeywordEscaping_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"keywords.proto"})
	opts := &Options{Lang: "kotlin", Domain: true}

	var file *DomainFile
	for _, f := range gen.Files {
		if f.Generate {
			file = BuildDomainFile(f, opts)
			break
		}
	}
	if file == nil {
		t.Fatal("no generated file found")
	}

	var kf *DomainMessage
	for _, m := range file.Messages {
		if m.Name == "KeywordFields" {
			kf = m
			break
		}
	}
	if kf == nil {
		t.Fatal("KeywordFields message not found")
	}

	keywordTests := map[string]string{
		"type":  "type",  // not a hard Kotlin keyword
		"self":  "self",  // not a Kotlin keyword
		"match": "match", // not a Kotlin keyword
		"mod":   "mod",   // not a Kotlin keyword
		"ref":   "ref",   // not a Kotlin keyword
		"super": "`super`",
	}

	for protoName, wantKotlinName := range keywordTests {
		t.Run(protoName, func(t *testing.T) {
			var df *DomainField
			for _, f := range kf.Fields {
				if f.Name == protoName {
					df = f
					break
				}
			}
			if df == nil {
				t.Fatalf("field %q not found", protoName)
			}

			gotKotlinName := escapeKotlinKeyword(toCamelCase(df.Name))
			if gotKotlinName != wantKotlinName {
				t.Errorf("escapeKotlinKeyword(toCamelCase(%q)) = %q, want %q", protoName, gotKotlinName, wantKotlinName)
			}

			// Verify the field gets a valid Kotlin type.
			got := kotlinFieldType(*df)
			if got == "" {
				t.Errorf("kotlinFieldType(%q) returned empty", protoName)
			}
		})
	}
}

func TestKotlinCatalogFields_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"catalog.proto"})
	opts := &Options{Lang: "kotlin", Domain: true}

	var file *DomainFile
	for _, f := range gen.Files {
		if f.Generate {
			file = BuildDomainFile(f, opts)
			break
		}
	}
	if file == nil {
		t.Fatal("no generated file found")
	}

	var catalog *DomainMessage
	for _, m := range file.Messages {
		if m.Name == "ModelCatalogEntry" {
			catalog = m
			break
		}
	}
	if catalog == nil {
		t.Fatal("ModelCatalogEntry not found")
	}

	// Verify field types.
	tests := []struct {
		field string
		want  string
	}{
		{"model_id", "String"},
		{"provider", "String"},
		{"display_name", "String"},
		{"input_per_million", "Double"},
		{"output_per_million", "Double"},
		{"enabled", "Boolean"},
		{"category", "String"},
		{"context_window", "Long"},
		{"discount_percent", "Double"},
		{"aliases", "List<String>"},
		{"provider_model_id", "String"},
		{"created_at", "Instant"},
		{"updated_at", "Instant"},
		{"notes", "String"},
		{"region", "String"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			var df *DomainField
			for _, f := range catalog.Fields {
				if f.Name == tt.field {
					df = f
					break
				}
			}
			if df == nil {
				t.Fatalf("field %q not found", tt.field)
			}
			got := kotlinFieldType(*df)
			if got != tt.want {
				t.Errorf("kotlinFieldType(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

func TestKotlinSerialNameDecision_Integration(t *testing.T) {
	// Verify that @SerialName is added when camelCase differs from snake_case
	// and omitted when they are the same.
	tests := []struct {
		name        string
		needsRename bool
	}{
		{"id", false},
		{"email", false},
		{"display_name", true},
		{"active", false},
		{"age", false},
		{"roles", false},
		{"metadata", false},
		{"address", false},
		{"created_at", true},
		{"session_timeout", true},
		{"phone", false},
		{"avatar", false},
		{"nickname", false},
		{"status", false},
		{"tags", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			camel := escapeKotlinKeyword(toCamelCase(tt.name))
			needsRename := camel != tt.name
			if needsRename != tt.needsRename {
				t.Errorf("field %q: needsRename = %v (camel=%q), want %v", tt.name, needsRename, camel, tt.needsRename)
			}
		})
	}
}

func TestKotlinCamelCase_Integration(t *testing.T) {
	// Verify camelCase conversion matches what we see in golden files.
	tests := []struct {
		input string
		want  string
	}{
		{"display_name", "displayName"},
		{"created_at", "createdAt"},
		{"session_timeout", "sessionTimeout"},
		{"email", "email"},
		{"contact_method", "contactMethod"},
		{"model_id", "modelId"},
		{"input_per_million", "inputPerMillion"},
		{"provider_model_id", "providerModelId"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toCamelCase(tt.input)
			if got != tt.want {
				t.Errorf("toCamelCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestKotlinDurationImportDecision(t *testing.T) {
	// When a Duration field exists, needsDuration should be true.
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	opts := &Options{Lang: "kotlin", Domain: true}

	var file *DomainFile
	for _, f := range gen.Files {
		if f.Generate {
			file = BuildDomainFile(f, opts)
			break
		}
	}
	if file == nil {
		t.Fatal("no generated file found")
	}

	needsSerialName := false
	needsInstant := false
	needsDuration := false

	for _, m := range file.Messages {
		if m.Skip {
			continue
		}
		scanKotlinImports(m, &needsSerialName, &needsInstant, &needsDuration)
	}

	if !needsInstant {
		t.Error("needsInstant = false, want true (User has Timestamp)")
	}
	if !needsDuration {
		t.Error("needsDuration = false, want true (User has Duration)")
	}
	if !needsSerialName {
		t.Error("needsSerialName = false, want true (User has fields needing @SerialName)")
	}

	// Verify catalog doesn't need Duration.
	gen2 := newPlugin(t, fds, []string{"catalog.proto"})
	for _, f := range gen2.Files {
		if !f.Generate {
			continue
		}
		catFile := BuildDomainFile(f, opts)
		catSerialName := false
		catInstant := false
		catDuration := false
		for _, m := range catFile.Messages {
			if m.Skip {
				continue
			}
			scanKotlinImports(m, &catSerialName, &catInstant, &catDuration)
		}
		if catDuration {
			t.Error("catalog needsDuration = true, want false")
		}
		if !catInstant {
			t.Error("catalog needsInstant = false, want true")
		}
	}
}

// TestKotlinGoldenFiles verifies that golden Kotlin files contain expected patterns.
func TestKotlinGoldenFiles(t *testing.T) {
	files := []struct {
		path           string
		mustContain    []string
		mustNotContain []string
	}{
		{
			"../testdata/golden/kotlin/gen/user.type.kt",
			[]string{
				"// Code generated by proto2type. DO NOT EDIT.",
				"@Serializable",
				"data class User(",
				"data class Address(",
				"data class Tag(",
				"enum class UserStatus",
				"sealed class UserContactMethod",
				"import kotlinx.serialization.Serializable",
				"import kotlinx.datetime.Instant",
				"import kotlin.time.Duration",
				"package test.v1",
			},
			nil,
		},
		{
			"../testdata/golden/kotlin/gen/catalog.type.kt",
			[]string{
				"@Serializable",
				"data class ModelCatalogEntry(",
				"@SerialName(\"model_id\")",
				"@SerialName(\"display_name\")",
			},
			[]string{
				"import kotlin.time.Duration",
			},
		},
		{
			"../testdata/golden/kotlin/gen/keywords.type.kt",
			[]string{
				"@Serializable",
				"data class KeywordFields(",
				"`super`",
			},
			nil,
		},
	}

	for _, tt := range files {
		t.Run(tt.path, func(t *testing.T) {
			content, err := os.ReadFile(tt.path)
			if err != nil {
				t.Skipf("golden file not found: %s", tt.path)
				return
			}
			src := string(content)

			for _, want := range tt.mustContain {
				if !strings.Contains(src, want) {
					t.Errorf("golden file missing: %q", want)
				}
			}
			for _, notWant := range tt.mustNotContain {
				if strings.Contains(src, notWant) {
					t.Errorf("golden file should NOT contain: %q", notWant)
				}
			}
		})
	}
}
