package generator

import (
	"os"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
)

func TestTSGoldenUpdate(t *testing.T) {
	if os.Getenv("UPDATE_GOLDEN") == "" {
		t.Skip("set UPDATE_GOLDEN=1 to regenerate golden files")
	}
	fds := buildFileDescriptorSet(t)

	testCases := []struct {
		name string
		opts *Options
		out  string
	}{
		{"default", &Options{Lang: "ts", Domain: true}, "../testdata/golden/ts/gen/user.type.ts"},
		{"bigint", &Options{Lang: "ts", Domain: true, TSInt64Style: "bigint"}, "../testdata/golden/ts/gen/user_bigint.type.ts"},
		{"validate", &Options{Lang: "ts", Domain: true, Validate: "true"}, "../testdata/golden/ts/gen/user_validate.type.ts"},
		{"types_only", &Options{Lang: "ts", Domain: true, TSTypesOnly: true}, "../testdata/golden/ts/gen/user_types_only.type.ts"},
	}

	if err := os.MkdirAll("../testdata/golden/ts/gen", 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gen := newPlugin(t, fds, []string{"user.proto"})
			for _, f := range gen.Files {
				if !f.Generate {
					continue
				}
				if err := generateTypeScript(gen, f, tc.opts); err != nil {
					t.Fatalf("generateTypeScript: %v", err)
				}
			}
			content := extractTSOutput(t, gen)
			if err := os.WriteFile(tc.out, []byte(content), 0644); err != nil {
				t.Fatalf("writing golden file: %v", err)
			}
			t.Logf("wrote golden file: %s (%d bytes)", tc.out, len(content))
		})
	}
}

func extractTSOutput(t *testing.T, gen *protogen.Plugin) string {
	t.Helper()
	for _, gf := range gen.Response().File {
		if gf.GetName() != "" && strings.HasSuffix(gf.GetName(), ".type.ts") {
			return gf.GetContent()
		}
	}
	t.Fatal("no TS output found in plugin response")
	return ""
}

func TestTSGoldenMatch(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	opts := &Options{Lang: "ts", Domain: true}
	for _, f := range gen.Files {
		if f.Generate {
			if err := generateTypeScript(gen, f, opts); err != nil {
				t.Fatalf("generateTypeScript: %v", err)
			}
		}
	}
	content := extractTSOutput(t, gen)
	golden, err := os.ReadFile("../testdata/golden/ts/gen/user.type.ts")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if content != string(golden) {
		t.Errorf("output does not match golden file")
	}
}

func TestTSGoldenBigInt(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	opts := &Options{Lang: "ts", Domain: true, TSInt64Style: "bigint"}
	for _, f := range gen.Files {
		if f.Generate {
			if err := generateTypeScript(gen, f, opts); err != nil {
				t.Fatalf("generateTypeScript: %v", err)
			}
		}
	}
	content := extractTSOutput(t, gen)
	golden, err := os.ReadFile("../testdata/golden/ts/gen/user_bigint.type.ts")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if content != string(golden) {
		t.Errorf("output does not match golden file")
	}
}

func TestTSGoldenValidate(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	opts := &Options{Lang: "ts", Domain: true, Validate: "true"}
	for _, f := range gen.Files {
		if f.Generate {
			if err := generateTypeScript(gen, f, opts); err != nil {
				t.Fatalf("generateTypeScript: %v", err)
			}
		}
	}
	content := extractTSOutput(t, gen)
	golden, err := os.ReadFile("../testdata/golden/ts/gen/user_validate.type.ts")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if content != string(golden) {
		t.Errorf("output does not match golden file")
	}
}

func TestTSGoldenTypesOnly(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	opts := &Options{Lang: "ts", Domain: true, TSTypesOnly: true}
	for _, f := range gen.Files {
		if f.Generate {
			if err := generateTypeScript(gen, f, opts); err != nil {
				t.Fatalf("generateTypeScript: %v", err)
			}
		}
	}
	content := extractTSOutput(t, gen)
	golden, err := os.ReadFile("../testdata/golden/ts/gen/user_types_only.type.ts")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if content != string(golden) {
		t.Errorf("output does not match golden file")
	}
	// Verify zero Zod references.
	if strings.Contains(content, "import { z }") || strings.Contains(content, "z.object") || strings.Contains(content, "z.enum") {
		t.Errorf("types-only output must not contain Zod references")
	}
	if strings.Contains(content, "Schema") {
		t.Errorf("types-only output must not contain Schema references")
	}
}

func TestTSTypesOnlyValidateConflict(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})
	opts := &Options{Lang: "ts", Domain: true, TSTypesOnly: true, Validate: "true"}
	for _, f := range gen.Files {
		if f.Generate {
			err := generateTypeScript(gen, f, opts)
			if err == nil {
				t.Fatal("expected error for validate + ts_types_only, got nil")
			}
			if !strings.Contains(err.Error(), "ts_types_only") {
				t.Errorf("error should mention ts_types_only, got: %v", err)
			}
		}
	}
}
