package generator

import (
	"strings"
	"unicode"
)

// toPascalCase converts a snake_case string to PascalCase.
// e.g. "model_id" -> "ModelID", "input_per_million" -> "InputPerMillion"
func toPascalCase(s string) string {
	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		// Handle common abbreviations
		upper := strings.ToUpper(part)
		if isCommonAbbreviation(upper) {
			b.WriteString(upper)
		} else {
			runes := []rune(part)
			runes[0] = unicode.ToUpper(runes[0])
			b.WriteString(string(runes))
		}
	}
	return b.String()
}

// isCommonAbbreviation returns true for common abbreviations that should be all-caps.
func isCommonAbbreviation(s string) bool {
	switch s {
	case "ID", "URL", "URI", "API", "HTTP", "HTTPS", "IP", "TCP", "UDP", "DNS", "TTL", "SSL", "TLS", "DB":
		return true
	}
	return false
}

// storageFieldName returns the storage field name for a proto field.
// By default this is the proto field name (snake_case).
func storageFieldName(protoName string) string {
	return protoName
}

// outputFilename returns the output filename for a given proto file.
func outputFilename(protoPath, suffix string) string {
	// Strip .proto extension
	base := strings.TrimSuffix(protoPath, ".proto")
	return base + suffix
}
