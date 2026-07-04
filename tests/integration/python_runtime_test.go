package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPythonRuntimeIntegration validates that generated Python/Pydantic models
// work at runtime: import, instantiate, serialize, enforce constraints.
//
// Prerequisites: uv (https://docs.astral.sh/uv/) must be on $PATH.
// The test creates a temporary venv via "uv run" with pydantic as an inline
// dependency — no persistent venv or pip install needed.
func TestPythonRuntimeIntegration(t *testing.T) {
	// Skip if uv is not available.
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not found on PATH, skipping Python runtime test")
	}

	// Locate the test script relative to the project root.
	root := findProjectRoot(t)
	testScript := filepath.Join(root, "tests", "python", "test_runtime.py")
	if _, err := os.Stat(testScript); os.IsNotExist(err) {
		t.Fatalf("test script not found: %s", testScript)
	}

	// Run via "uv run --with pydantic" with a 2-minute timeout to prevent
	// hanging on network stalls or unresponsive Python processes.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "uv", "run", "--with", "pydantic>=2", "--python", "3.11", testScript)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")

	out, err := cmd.CombinedOutput()
	t.Logf("Python test output:\n%s", string(out))

	// Parse TAP output for specific failures.
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "not ok") {
			t.Errorf("Python: %s", line)
		}
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			t.Fatalf("Python runtime tests timed out after 2 minutes")
		}
		t.Fatalf("Python runtime tests failed: %v", err)
	}
}

// findProjectRoot walks up from the test file to find the project root
// (directory containing go.mod).
func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root (go.mod)")
		}
		dir = parent
	}
}
