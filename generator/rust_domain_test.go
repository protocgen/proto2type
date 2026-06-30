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
		{"self", "self_"},
		{"match", "r#match"},
		{"mod", "r#mod"},
		{"as", "r#as"},
		{"async", "r#async"},
		{"await", "r#await"},
		{"break", "r#break"},
		{"const", "r#const"},
		{"continue", "r#continue"},
		{"crate", "crate_"},
		{"dyn", "r#dyn"},
		{"else", "r#else"},
		{"enum", "r#enum"},
		{"extern", "r#extern"},
		{"false", "false_"},
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
		{"Self", "Self_"},
		{"static", "r#static"},
		{"super", "super_"},
		{"trait", "r#trait"},
		{"true", "true_"},
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
	// Verify every Rust keyword is handled by escapeRustKeyword.
	allKeywords := []string{
		"as", "async", "await", "break", "const", "continue", "crate", "dyn",
		"else", "enum", "extern", "false", "fn", "for", "if", "impl", "in",
		"let", "loop", "match", "mod", "move", "mut", "pub", "ref", "return",
		"self", "Self", "static", "struct", "super", "trait", "true", "type",
		"unsafe", "use", "where", "while", "yield",
		"abstract", "become", "box", "do", "final", "macro", "override",
		"priv", "try", "typeof", "unsized", "virtual",
	}
	for _, kw := range allKeywords {
		escaped := escapeRustKeyword(kw)
		if escaped == kw {
			t.Errorf("keyword %q was not escaped (returned unchanged)", kw)
		}
	}
	// Verify maps have the expected total count
	totalMapped := len(rustRawKeywords) + len(rustSpecialKeywords)
	if len(allKeywords) != totalMapped {
		t.Errorf("keyword maps have %d entries but test expects %d", totalMapped, len(allKeywords))
	}
}

func TestRustDomainSingularTypeFromIR_OptionalTimestamp(t *testing.T) {
	f := &DomainField{Kind: FieldKindTimestamp, Optional: true}
	got := rustDomainSingularTypeFromIR(f)
	want := "Option<DateTime<Utc>>"
	if got != want {
		t.Errorf("rustDomainSingularTypeFromIR(optional Timestamp) = %q, want %q", got, want)
	}
}

func TestRustDomainSingularTypeFromIR_NonOptionalTimestamp(t *testing.T) {
	f := &DomainField{Kind: FieldKindTimestamp, Optional: false}
	got := rustDomainSingularTypeFromIR(f)
	want := "DateTime<Utc>"
	if got != want {
		t.Errorf("rustDomainSingularTypeFromIR(non-optional Timestamp) = %q, want %q", got, want)
	}
}

func TestRustDomainSingularTypeFromIR_OptionalDuration(t *testing.T) {
	f := &DomainField{Kind: FieldKindDuration, Optional: true}
	got := rustDomainSingularTypeFromIR(f)
	want := "Option<i64>"
	if got != want {
		t.Errorf("rustDomainSingularTypeFromIR(optional Duration) = %q, want %q", got, want)
	}
}

func TestRustDomainSingularTypeFromIR_OptionalEnum(t *testing.T) {
	f := &DomainField{Kind: FieldKindEnum, EnumTypeName: "UserStatus", Optional: true}
	got := rustDomainSingularTypeFromIR(f)
	want := "Option<UserStatus>"
	if got != want {
		t.Errorf("rustDomainSingularTypeFromIR(optional Enum) = %q, want %q", got, want)
	}
}

func TestRustDomainSingularTypeFromIR_OptionalEnumAsString(t *testing.T) {
	f := &DomainField{Kind: FieldKindEnum, EnumTypeName: "UserStatus", Optional: true, EnumAsString: true}
	got := rustDomainSingularTypeFromIR(f)
	want := "Option<String>"
	if got != want {
		t.Errorf("rustDomainSingularTypeFromIR(optional Enum as string) = %q, want %q", got, want)
	}
}
