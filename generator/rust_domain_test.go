package generator

import "testing"

func TestEscapeRustKeyword(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Strict keywords
		{"type", "r#type"},
		{"struct", "r#struct"},
		{"self", "r#self"},
		{"match", "r#match"},
		{"mod", "r#mod"},
		{"as", "r#as"},
		{"async", "r#async"},
		{"await", "r#await"},
		{"break", "r#break"},
		{"const", "r#const"},
		{"continue", "r#continue"},
		{"crate", "r#crate"},
		{"dyn", "r#dyn"},
		{"else", "r#else"},
		{"enum", "r#enum"},
		{"extern", "r#extern"},
		{"false", "r#false"},
		{"fn", "r#fn"},
		{"for", "r#for"},
		{"if", "r#if"},
		{"impl", "r#impl"},
		{"in", "r#in"},
		{"let", "r#let"},
		{"loop", "r#loop"},
		{"move", "r#move"},
		{"mut", "r#mut"},
		{"pub", "r#pub"},
		{"ref", "r#ref"},
		{"return", "r#return"},
		{"Self", "r#Self"},
		{"static", "r#static"},
		{"super", "r#super"},
		{"trait", "r#trait"},
		{"true", "r#true"},
		{"unsafe", "r#unsafe"},
		{"use", "r#use"},
		{"where", "r#where"},
		{"while", "r#while"},
		{"yield", "r#yield"},

		// Reserved for future use
		{"abstract", "r#abstract"},
		{"become", "r#become"},
		{"box", "r#box"},
		{"do", "r#do"},
		{"final", "r#final"},
		{"macro", "r#macro"},
		{"override", "r#override"},
		{"priv", "r#priv"},
		{"try", "r#try"},
		{"typeof", "r#typeof"},
		{"unsized", "r#unsized"},
		{"virtual", "r#virtual"},

		// Non-keywords — should pass through unchanged
		{"name", "name"},
		{"email", "email"},
		{"user_id", "user_id"},
		{"display_name", "display_name"},
		{"Type", "Type"}, // PascalCase is not a keyword
		{"Struct", "Struct"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := escapeRustKeyword(tt.input)
			if got != tt.expected {
				t.Errorf("escapeRustKeyword(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestEscapeRustKeyword_Exhaustive(t *testing.T) {
	// Verify the full keyword set is handled.
	// Count how many keywords are in rustKeywords map.
	expectedKeywords := []string{
		"as", "async", "await", "break", "const", "continue", "crate", "dyn",
		"else", "enum", "extern", "false", "fn", "for", "if", "impl", "in",
		"let", "loop", "match", "mod", "move", "mut", "pub", "ref", "return",
		"self", "Self", "static", "struct", "super", "trait", "true", "type",
		"unsafe", "use", "where", "while", "yield",
		"abstract", "become", "box", "do", "final", "macro", "override",
		"priv", "try", "typeof", "unsized", "virtual",
	}
	for _, kw := range expectedKeywords {
		if !rustKeywords[kw] {
			t.Errorf("expected %q to be in rustKeywords map but it was not", kw)
		}
	}
	if len(expectedKeywords) != len(rustKeywords) {
		t.Errorf("rustKeywords map has %d entries but test expects %d",
			len(rustKeywords), len(expectedKeywords))
	}
}
