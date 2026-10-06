package generator

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "overwrite golden files with current output")

const goldenDir = "../testdata/golden"

// TestSearchIndexGeneration_Golden generates the search index code and compares
// against a checked-in golden file. Run with -update-golden to regenerate:
//
//	go test ./generator/ -run Golden -update-golden
func TestSearchIndexGeneration_Golden(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"fhir_patient.proto"})

	opts := &Options{
		Lang:                 "go",
		Backend:              "postgres",
		PostgresMode:         "search_index",
		PostgresSearchParams: "../testdata/proto/fhir_search_params.json",
		Domain:               false,
	}

	runner := NewRunner()
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		if err := runner.GenerateFile(gen, f, opts); err != nil {
			t.Fatalf("GenerateFile: %v", err)
		}
	}

	resp := gen.Response()
	if len(resp.File) == 0 {
		t.Fatal("no files generated")
	}

	// Each generated file gets its own golden file
	for _, f := range resp.File {
		goldenName := filepath.Base(f.GetName())
		goldenPath := filepath.Join(goldenDir, goldenName)
		content := f.GetContent()

		if *updateGolden {
			if err := os.MkdirAll(goldenDir, 0o755); err != nil {
				t.Fatalf("mkdir golden: %v", err)
			}
			if err := os.WriteFile(goldenPath, []byte(content), 0o644); err != nil {
				t.Fatalf("write golden %s: %v", goldenPath, err)
			}
			t.Logf("Updated golden file: %s (%d bytes)", goldenPath, len(content))
			continue
		}

		expected, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatalf("read golden %s: %v\nRun with -update-golden to create it", goldenPath, err)
		}

		if content != string(expected) {
			// Write actual to temp for diffing
			actualPath := goldenPath + ".actual"
			if err := os.WriteFile(actualPath, []byte(content), 0o644); err != nil {
				t.Fatalf("write actual %s: %v", actualPath, err)
			}
			t.Errorf("golden file mismatch: %s\n  diff %s %s\nRun with -update-golden to accept changes",
				goldenName, goldenPath, actualPath)
		} else {
			t.Logf("Golden match: %s (%d bytes)", goldenName, len(content))
		}
	}
}
