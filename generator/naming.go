package generator

import (
	"fmt"
	"path"
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

// toSnakeCase converts a PascalCase or camelCase string to snake_case.
// e.g., "modelId" → "model_id", "DisplayName" → "display_name"
// Handles consecutive uppercase: "HTMLParser" → "html_parser"
func toSnakeCase(s string) string {
	if s == "" {
		return s
	}

	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				// Insert underscore before uppercase if:
				// - previous char is lowercase, OR
				// - next char is lowercase (handles "HTMLParser" -> "html_parser")
				prev := runes[i-1]
				if unicode.IsLower(prev) {
					b.WriteRune('_')
				} else if i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
					b.WriteRune('_')
				}
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
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

// receiverName returns the lowercase first letter of a type name for use as a method receiver.
// e.g., "UserFirestore" -> "u", "Address" -> "a"
func receiverName(typeName string) string {
	if len(typeName) == 0 {
		return "x"
	}
	r := []rune(typeName)
	return string(unicode.ToLower(r[0]))
}

// outputFilename returns the output filename for a given proto file.
// It validates that the resulting path contains no path traversal components.
func outputFilename(protoPath, suffix string) string {
	// Strip .proto extension
	base := strings.TrimSuffix(protoPath, ".proto")
	result := base + suffix

	// Defense-in-depth: reject paths with ".." components that could escape
	// the output directory. Protoc validates this upstream, but we guard here
	// to prevent directory traversal if the function is called with untrusted input.
	// Normalize backslashes to forward slashes for cross-platform safety,
	// then use path (POSIX) package since protoc paths are always forward-slash.
	normalized := strings.ReplaceAll(result, "\\", "/")
	cleaned := path.Clean(normalized)
	// Detect Windows drive-letter absolute paths (e.g. "C:/foo") which
	// path.IsAbs does not catch since it's POSIX-only.
	isWindowsAbs := len(cleaned) >= 3 &&
		((cleaned[0] >= 'A' && cleaned[0] <= 'Z') || (cleaned[0] >= 'a' && cleaned[0] <= 'z')) &&
		cleaned[1] == ':' && cleaned[2] == '/'
	if path.IsAbs(cleaned) || isWindowsAbs || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		panic(fmt.Sprintf("proto2type: path traversal detected in output filename: %q", result))
	}

	return cleaned
}

// parseGoPackage parses a go_package string in the format "import/path;package_name"
// or just "import/path" (in which case the package name is the last path element).
func parseGoPackage(pkg string) (importPath, packageName string) {
	if i := strings.Index(pkg, ";"); i >= 0 {
		return pkg[:i], pkg[i+1:]
	}
	// Use last path element as package name
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		return pkg, pkg[i+1:]
	}
	return pkg, pkg
}

// adjustSubdirFilename adjusts the output filename when go_package points to a
// subdirectory of the proto's import path. For example, if proto is in
// "github.com/.../types" and go_package overrides to "github.com/.../types/domain",
// we need to output to "types/domain/" instead of "types/".
func adjustSubdirFilename(filename, protoImport, importPath string) string {
	if strings.HasPrefix(importPath, protoImport+"/") {
		subdir := strings.TrimPrefix(importPath, protoImport+"/")
		idx := strings.LastIndex(filename, "/")
		if idx < 0 {
			return subdir + "/" + filename
		}
		return filename[:idx+1] + subdir + "/" + filename[idx+1:]
	}
	return filename
}

// rustKeywords is the set of Rust reserved keywords (including weak/reserved-for-future-use).
// rustRawKeywords are keywords that can be escaped with r# prefix.
var rustRawKeywords = map[string]bool{
	"as": true, "async": true, "await": true, "break": true, "const": true,
	"continue": true, "dyn": true, "else": true, "enum": true,
	"extern": true, "fn": true, "for": true, "if": true,
	"impl": true, "in": true, "let": true, "loop": true, "match": true,
	"mod": true, "move": true, "mut": true, "pub": true, "ref": true,
	"return": true, "static": true, "struct": true,
	"trait": true, "type": true, "unsafe": true,
	"use": true, "where": true, "while": true, "yield": true,
	"abstract": true, "become": true, "box": true, "do": true, "final": true,
	"macro": true, "override": true, "priv": true, "try": true,
	"typeof": true, "unsized": true, "virtual": true,
}

// rustSpecialKeywords are keywords that CANNOT use r# (self, Self, super, crate, true, false).
// These are escaped by appending an underscore.
var rustSpecialKeywords = map[string]bool{
	"self": true, "Self": true, "super": true, "crate": true,
	"true": true, "false": true,
}

// escapeRustKeyword escapes Rust reserved keywords to produce valid identifiers.
// Most keywords use the r# prefix; self/Self/super/crate/true/false use a _ suffix.
func escapeRustKeyword(name string) string {
	if rustSpecialKeywords[name] {
		return name + "_"
	}
	if rustRawKeywords[name] {
		return "r#" + name
	}
	return name
}

// toCamelCase converts a snake_case string to lowerCamelCase.
// e.g. "display_name" -> "displayName", "model_id" -> "modelId"
func toCamelCase(s string) string {
	parts := strings.Split(s, "_")
	if len(parts) == 0 {
		return s
	}
	var b strings.Builder
	first := true
	for _, part := range parts {
		if part == "" {
			continue
		}
		if first {
			// First non-empty segment stays lowercase.
			b.WriteString(strings.ToLower(part))
			first = false
		} else {
			// Subsequent segments get capitalised first letter.
			runes := []rune(part)
			runes[0] = unicode.ToUpper(runes[0])
			b.WriteString(string(runes))
		}
	}
	return b.String()
}

// kotlinKeywords is the set of Kotlin hard and soft keywords that must be escaped
// with backticks to be used as val parameter names.
var kotlinKeywords = map[string]bool{
	// Hard keywords
	"as": true, "break": true, "class": true, "continue": true,
	"do": true, "else": true, "false": true, "for": true,
	"fun": true, "if": true, "in": true, "interface": true,
	"is": true, "null": true, "object": true, "package": true,
	"return": true, "super": true, "this": true, "throw": true,
	"true": true, "try": true, "typealias": true, "typeof": true,
	"val": true, "var": true, "when": true, "while": true,
	// Soft/modifier keywords that cannot be used as val parameter names (KT-4)
	"abstract": true, "actual": true, "companion": true, "data": true,
	"enum": true, "expect": true, "external": true, "inner": true,
	"inline": true, "internal": true, "open": true, "operator": true,
	"override": true, "private": true, "protected": true, "public": true,
	"sealed": true, "suspend": true,
}

// escapeKotlinKeyword wraps Kotlin reserved words in backticks.
// e.g. "val" -> "`val`", "name" -> "name" (unchanged).
func escapeKotlinKeyword(name string) string {
	if kotlinKeywords[name] {
		return "`" + name + "`"
	}
	return name
}
