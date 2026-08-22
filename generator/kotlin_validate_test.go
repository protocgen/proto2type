package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKotlinValidateConstraints_Coverage(t *testing.T) {
	goldenFile := filepath.Join("..", "testdata", "golden", "kotlin", "gen", "user.type.kt")
	content, err := os.ReadFile(goldenFile)
	if err != nil {
		t.Fatalf("Failed to read golden file: %v", err)
	}

	contentStr := string(content)

	expectedPatterns := []string{
		"codePointCount",
		"RE_USER_EMAIL_EMAIL",
		"RE_USER_PHONE_PATTERN",
		"errors.add(",
		"validateOrThrow()",
	}

	for _, pattern := range expectedPatterns {
		if !strings.Contains(contentStr, pattern) {
			t.Errorf("Expected golden file to contain pattern %q, but it did not", pattern)
		}
	}
}
