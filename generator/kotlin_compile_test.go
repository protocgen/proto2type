package generator

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestKotlinGoldenCompile compiles the Kotlin golden files using the Gradle
// project in tests/kotlin/. This catches type errors in generated Kotlin code
// that unit tests would miss (Issue #107).
func TestKotlinGoldenCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Kotlin compilation test in short mode")
	}

	// Skip if gradle isn't available (requires nix dev shell).
	gradleBin, err := exec.LookPath("gradle")
	if err != nil {
		t.Skip("gradle not found in PATH; skipping Kotlin compilation test (run inside nix develop)")
	}

	// Skip if java isn't available (required by gradle).
	if _, err := exec.LookPath("java"); err != nil {
		t.Skip("java not found in PATH; skipping Kotlin compilation test")
	}

	// Locate the tests/kotlin directory relative to this source file.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to determine test file location")
	}
	gradleDir := filepath.Join(filepath.Dir(thisFile), "..", "tests", "kotlin")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	//nolint:gosec // gradleBin is from LookPath, not user-controlled
	cmd := exec.CommandContext(ctx, gradleBin, "compileKotlin", "--no-daemon", "-q")
	cmd.Dir = gradleDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Kotlin golden file compilation failed:\n%s\n%v", out, err)
	}
}
