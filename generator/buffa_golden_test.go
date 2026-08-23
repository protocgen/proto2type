package generator

import (
	"os"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
)

// TestGenerateBuffaPrefixedGolden regenerates the golden file for user.proto with
// rust_buffa_oneof_prefix=__buffa. Skipped by default; set UPDATE_GOLDEN=1 to run.
func TestGenerateBuffaPrefixedGolden(t *testing.T) {
	if os.Getenv("UPDATE_GOLDEN") == "" {
		t.Skip("set UPDATE_GOLDEN=1 to regenerate golden files")
	}
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})

	opts := &Options{
		Lang:           "rust",
		Backend:        "buffa",
		BufModule:      "crate::proto",
		BufOneofPrefix: "__buffa",
	}

	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		if err := generateRustBuffa(gen, f, opts); err != nil {
			t.Fatalf("generateRustBuffa: %v", err)
		}
	}

	content := extractBuffaOutput(t, gen)

	goldenPath := "../testdata/golden/rust/gen/user_buffa_prefixed.type.rs"
	if err := os.WriteFile(goldenPath, []byte(content), 0644); err != nil {
		t.Fatalf("writing golden file: %v", err)
	}
	t.Logf("wrote golden file: %s (%d bytes)", goldenPath, len(content))
}

// TestGoldenBuffaPrefixedOutput verifies the prefixed golden file contains
// the expected __buffa::oneof:: paths and standard infrastructure.
func TestGoldenBuffaPrefixedOutput(t *testing.T) {
	content, err := os.ReadFile("../testdata/golden/rust/gen/user_buffa_prefixed.type.rs")
	if err != nil {
		t.Fatalf("golden file not found: %v", err)
	}
	src := string(content)

	// Must contain prefixed oneof paths.
	mustContain := []string{
		"__buffa_mod::__buffa::oneof::user::ContactMethod::ContactEmail",
		"__buffa_mod::__buffa::oneof::user::ContactMethod::ContactPhone",
	}
	for _, s := range mustContain {
		if !strings.Contains(src, s) {
			t.Errorf("expected golden file to contain %q", s)
		}
	}

	// Must NOT contain unprefixed oneof path (without the __buffa:: segment).
	if strings.Contains(src, "__buffa_mod::oneof::user::ContactMethod::") {
		t.Error("golden file should NOT contain unprefixed __buffa_mod::oneof::user::ContactMethod::")
	}

	// Must contain standard infrastructure.
	infrastructure := []string{
		"pub enum ConversionError",
		"impl TryFrom<&User>",
		"impl TryFrom<&__buffa_mod::User>",
	}
	for _, s := range infrastructure {
		if !strings.Contains(src, s) {
			t.Errorf("expected golden file to contain infrastructure %q", s)
		}
	}
}

// TestGoldenBuffaDefaultOutput verifies the existing (unprefixed) golden file
// does NOT contain the __buffa::oneof prefix.
func TestGoldenBuffaDefaultOutput(t *testing.T) {
	content, err := os.ReadFile("../testdata/golden/rust/gen/user_buffa.type.rs")
	if err != nil {
		t.Fatalf("golden file not found: %v", err)
	}
	src := string(content)

	// The default (no prefix) golden file must NOT contain __buffa::oneof.
	if strings.Contains(src, "__buffa::oneof") {
		t.Error("default golden file should NOT contain __buffa::oneof (prefixed version)")
	}

	// Sanity: it should contain the unprefixed oneof path.
	if !strings.Contains(src, "__buffa_mod::oneof::user::ContactMethod::") {
		t.Error("default golden file should contain __buffa_mod::oneof::user::ContactMethod::")
	}
}

// TestBuffaOneofPrefix_Integration programmatically verifies that generateRustBuffa
// produces the correct oneof paths with and without BufOneofPrefix.
func TestBuffaOneofPrefix_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)

	t.Run("with_prefix", func(t *testing.T) {
		gen := newPlugin(t, fds, []string{"user.proto"})
		opts := &Options{
			Lang:           "rust",
			Backend:        "buffa",
			BufModule:      "crate::proto",
			BufOneofPrefix: "__buffa",
		}

		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			if err := generateRustBuffa(gen, f, opts); err != nil {
				t.Fatalf("generateRustBuffa: %v", err)
			}
		}

		src := extractBuffaOutput(t, gen)

		// Must contain prefixed paths.
		if !strings.Contains(src, "__buffa_mod::__buffa::oneof::user::ContactMethod::ContactEmail") {
			t.Error("expected prefixed oneof path for ContactEmail")
		}
		if !strings.Contains(src, "__buffa_mod::__buffa::oneof::user::ContactMethod::ContactPhone") {
			t.Error("expected prefixed oneof path for ContactPhone")
		}

		// Must NOT contain bare unprefixed oneof path.
		if strings.Contains(src, "__buffa_mod::oneof::user::") {
			t.Error("should NOT contain bare __buffa_mod::oneof::user:: (without prefix)")
		}
	})

	t.Run("without_prefix", func(t *testing.T) {
		gen := newPlugin(t, fds, []string{"user.proto"})
		opts := &Options{
			Lang:      "rust",
			Backend:   "buffa",
			BufModule: "crate::proto",
		}

		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			if err := generateRustBuffa(gen, f, opts); err != nil {
				t.Fatalf("generateRustBuffa: %v", err)
			}
		}

		src := extractBuffaOutput(t, gen)

		// Must contain unprefixed paths.
		if !strings.Contains(src, "__buffa_mod::oneof::user::ContactMethod::ContactEmail") {
			t.Error("expected unprefixed oneof path for ContactEmail")
		}

		// Must NOT contain prefixed paths.
		if strings.Contains(src, "__buffa_mod::__buffa::oneof::") {
			t.Error("should NOT contain __buffa_mod::__buffa::oneof:: (prefixed version)")
		}
	})
}

// TestBuffaDomainModule_Integration verifies that DomainModule controls the
// domain type import path in generated buffa output.
func TestBuffaDomainModule_Integration(t *testing.T) {
	fds := buildFileDescriptorSet(t)

	t.Run("with_domain_module", func(t *testing.T) {
		gen := newPlugin(t, fds, []string{"user.proto"})
		opts := &Options{
			Lang:         "rust",
			Backend:      "buffa",
			BufModule:    "crate::proto",
			DomainModule: "my_crate::domain",
		}

		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			if err := generateRustBuffa(gen, f, opts); err != nil {
				t.Fatalf("generateRustBuffa: %v", err)
			}
		}

		src := extractBuffaOutput(t, gen)

		if !strings.Contains(src, "use my_crate::domain::*;") {
			t.Error("expected 'use my_crate::domain::*;' in output")
		}
		if strings.Contains(src, "use super::*;") {
			t.Error("should NOT contain 'use super::*;' when domain_module is set")
		}
	})

	t.Run("without_domain_module", func(t *testing.T) {
		gen := newPlugin(t, fds, []string{"user.proto"})
		opts := &Options{
			Lang:      "rust",
			Backend:   "buffa",
			BufModule: "crate::proto",
		}

		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			if err := generateRustBuffa(gen, f, opts); err != nil {
				t.Fatalf("generateRustBuffa: %v", err)
			}
		}

		src := extractBuffaOutput(t, gen)

		if !strings.Contains(src, "use super::*;") {
			t.Error("expected 'use super::*;' in output (default)")
		}
	})
}

// extractBuffaOutput finds the buffa .type.rs file in the plugin response.
func extractBuffaOutput(t *testing.T, gen *protogen.Plugin) string {
	t.Helper()
	for _, gf := range gen.Response().File {
		if gf.GetName() != "" && strings.HasSuffix(gf.GetName(), "_buffa.type.rs") {
			return gf.GetContent()
		}
	}
	t.Fatal("no buffa output found in plugin response")
	return ""
}
