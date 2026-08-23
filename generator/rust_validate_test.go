package generator

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func stringPtr(v string) *string { return &v }

func TestRustValidateAttrs_StringConstraints(t *testing.T) {
	tests := []struct {
		name     string
		vc       *ValidateConstraints
		expected []string
	}{
		{
			name: "MinLength",
			vc: &ValidateConstraints{
				MinLength: uint64Ptr(3),
			},
			expected: []string{"length(min = 3)"},
		},
		{
			name: "MaxLength",
			vc: &ValidateConstraints{
				MaxLength: uint64Ptr(100),
			},
			expected: []string{"length(max = 100)"},
		},
		{
			name: "MinAndMaxLength",
			vc: &ValidateConstraints{
				MinLength: uint64Ptr(3),
				MaxLength: uint64Ptr(100),
			},
			expected: []string{"length(min = 3, max = 100)"},
		},
		{
			name: "Email",
			vc: &ValidateConstraints{
				Email: true,
			},
			expected: []string{"email"},
		},
		{
			name: "Pattern",
			vc: &ValidateConstraints{
				Pattern: "^test$",
			},
			expected: []string{"regex(path = *RE_TEST_FIELD_PATTERN)"},
		},
		{
			name: "UUID",
			vc: &ValidateConstraints{
				UUID: true,
			},
			expected: []string{"regex(path = *RE_TEST_FIELD_UUID)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &DomainField{
				Name:                "test_field",
				ValidateConstraints: tt.vc,
			}
			attrs := rustValidateAttrs(f)

			if len(attrs) != len(tt.expected) {
				t.Fatalf("expected %d attrs, got %d: %v", len(tt.expected), len(attrs), attrs)
			}

			for i, exp := range tt.expected {
				if attrs[i] != exp {
					t.Errorf("expected attr[%d] = %q, got %q", i, exp, attrs[i])
				}
			}
		})
	}
}

func TestRustValidateAttrs_NumericConstraints(t *testing.T) {
	tests := []struct {
		name     string
		vc       *ValidateConstraints
		expected []string
	}{
		{
			name: "GteLte",
			vc: &ValidateConstraints{
				Gte: stringPtr("0"),
				Lte: stringPtr("150"),
			},
			expected: []string{"range(min = 0, max = 150)"},
		},
		{
			name: "Gt",
			vc: &ValidateConstraints{
				Gt: stringPtr("0"),
			},
			expected: []string{"range(exclusive_min = 0)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &DomainField{
				Name:                "numeric_field",
				Kind:                FieldKindScalar,
				ScalarKind:          protoreflect.Int32Kind,
				ValidateConstraints: tt.vc,
			}
			attrs := rustValidateAttrs(f)

			if len(attrs) != len(tt.expected) {
				t.Fatalf("expected %d attrs, got %d: %v", len(tt.expected), len(attrs), attrs)
			}

			for i, exp := range tt.expected {
				if attrs[i] != exp {
					t.Errorf("expected attr[%d] = %q, got %q", i, exp, attrs[i])
				}
			}
		})
	}
}

func TestRustValidateAttrs_RepeatedConstraints(t *testing.T) {
	f := &DomainField{
		Name: "items",
		ValidateConstraints: &ValidateConstraints{
			MinItems: uint64Ptr(1),
			MaxItems: uint64Ptr(5),
		},
	}
	attrs := rustValidateAttrs(f)

	if len(attrs) != 1 {
		t.Fatalf("expected 1 attr, got %d: %v", len(attrs), attrs)
	}

	if attrs[0] != "length(min = 1, max = 5)" {
		t.Errorf("expected length(min = 1, max = 5), got %q", attrs[0])
	}
}

func TestRustRawStringDelimiter(t *testing.T) {
	tests := []struct {
		pattern  string
		expected string
	}{
		{"abc", "#"},
		{"a#bc", "##"},
		{"a##bc", "###"},
		{"#", "##"},
		{"##", "###"},
		{"###", "####"},
		{"no hash", "#"},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			delim := rustRawStringDelimiter(tt.pattern)
			if delim != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, delim)
			}
		})
	}
}

func TestRustRegexConstName(t *testing.T) {
	f := &DomainField{
		Name: "my_email_address",
	}
	name := rustRegexConstName(f, "PATTERN")
	expected := "RE_MY_EMAIL_ADDRESS_PATTERN"
	if name != expected {
		t.Errorf("expected %q, got %q", expected, name)
	}
}
