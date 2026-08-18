package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTypeScriptRuntimeIntegration(t *testing.T) {
	// Find the ts golden directory
	tsDir := filepath.Join("..", "..", "testdata", "golden", "ts")
	if _, err := os.Stat(tsDir); os.IsNotExist(err) {
		t.Skip("testdata/golden/ts not found")
	}

	// Check if npm is available
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not found, skipping TypeScript runtime test")
	}

	// Copy to temp dir to avoid mutating testdata (exclude node_modules/dist)
	tmpDir := t.TempDir()
	cpCmd := exec.Command("rsync", "-a", "--exclude=node_modules", "--exclude=dist", tsDir+"/", tmpDir+"/")
	if out, err := cpCmd.CombinedOutput(); err != nil {
		t.Fatalf("copy testdata: %v\n%s", err, out)
	}

	// Install dependencies
	npmInstall := exec.Command("npm", "install", "--silent")
	npmInstall.Dir = tmpDir
	if out, err := npmInstall.CombinedOutput(); err != nil {
		t.Fatalf("npm install failed: %v\n%s", err, out)
	}

	// Step 1: Type-check with tsc --noEmit
	t.Run("tsc_typecheck", func(t *testing.T) {
		cmd := exec.Command("npx", "tsc", "--noEmit", "--project", "tsconfig.json")
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tsc type-check failed:\n%s", out)
		}
	})

	// Step 2: Compile and run validation tests
	t.Run("zod_validation", func(t *testing.T) {
		// Compile
		tscCmd := exec.Command("npx", "tsc")
		tscCmd.Dir = tmpDir
		if out, err := tscCmd.CombinedOutput(); err != nil {
			t.Fatalf("tsc compilation failed:\n%s", out)
		}

		// Run tests
		nodeCmd := exec.Command("node", "dist/test/validate_test.js")
		nodeCmd.Dir = tmpDir
		out, err := nodeCmd.CombinedOutput()
		t.Logf("TypeScript test output:\n%s", out)
		if err != nil {
			t.Fatalf("TypeScript validation tests failed: %v", err)
		}
		if strings.Contains(string(out), "not ok") {
			t.Fatalf("TypeScript validation tests had failures")
		}
	})
}
