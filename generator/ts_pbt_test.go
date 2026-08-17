package generator

import (
	"strings"
	"testing"
)

func FuzzTsSafeKey(f *testing.F) {
	f.Add("key")
	f.Add("__proto__")
	f.Add("constructor")
	f.Add("prototype")
	f.Add("valid_key")

	f.Fuzz(func(t *testing.T, s string) {
		out := tsSafeKey(s)
		if s == "__proto__" || s == "constructor" || s == "prototype" {
			if !strings.HasPrefix(out, "[") {
				t.Errorf("tsSafeKey(%q) = %q, expected bracket notation", s, out)
			}
		} else {
			if out == "__proto__" {
				t.Errorf("tsSafeKey(%q) = %q, never emit bare __proto__", s, out)
			}
		}
	})
}

func FuzzSanitizeJSDoc(f *testing.F) {
	f.Add("hello")
	f.Add("/* test */")
	f.Add("end */")

	f.Fuzz(func(t *testing.T, s string) {
		out := sanitizeJSDoc(s)
		if strings.Contains(out, "*/") {
			t.Errorf("sanitizeJSDoc(%q) = %q, should not contain */", s, out)
		}
	})
}

func FuzzTsJoinQuoted(f *testing.F) {
	f.Add("a,b,c")
	f.Add("")
	f.Add("foo,\"bar\",baz")

	f.Fuzz(func(t *testing.T, s string) {
		parts := strings.Split(s, ",")
		out := tsJoinQuoted(parts)
		// Property: output should have exactly len(parts) quoted elements.
		// Each element is wrapped in double quotes. Count quote pairs.
		quoteCount := strings.Count(out, "\"")
		// Each element contributes at least 2 quotes (opening + closing).
		if quoteCount < len(parts)*2 {
			t.Errorf("tsJoinQuoted(%q) = %q, expected at least %d quotes, got %d",
				parts, out, len(parts)*2, quoteCount)
		}
	})
}

func FuzzTsImportPath(f *testing.F) {
	f.Add("user.proto", "user.proto")
	f.Add("a/b/c.proto", "d/e/f.proto")
	f.Add("msg.proto", "other.proto")

	f.Fuzz(func(t *testing.T, curr, target string) {
		if curr == "" || target == "" {
			return
		}
		out := tsImportPath(curr, target)
		if len(out) > 0 {
			if !strings.HasPrefix(out, ".") {
				t.Errorf("tsImportPath(%q, %q) = %q, should start with .", curr, target, out)
			}
			if !strings.HasSuffix(out, ".type.js") {
				t.Errorf("tsImportPath(%q, %q) = %q, should end with .type.js", curr, target, out)
			}
		}
	})
}
