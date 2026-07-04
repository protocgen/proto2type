package generator

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
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

	// Locate the tests/kotlin directory relative to this source file.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to determine test file location")
	}
	gradleDir := filepath.Join(filepath.Dir(thisFile), "..", "tests", "kotlin")

	cmd := exec.Command(gradleBin, "compileKotlin", "--no-daemon", "-q")
	cmd.Dir = gradleDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Kotlin golden file compilation failed:\n%s\n%v", out, err)
	}
}
