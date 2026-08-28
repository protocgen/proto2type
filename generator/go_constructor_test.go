package generator

import (
	"testing"

	"google.golang.org/genproto/googleapis/api/annotations"
)

func TestToLowerCamel(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Email", "email"},
		{"DisplayName", "displayName"},
		{"ID", "id"},
		{"URL", "url"},
		{"HTMLParser", "htmlParser"},
		{"", ""},
		{"a", "a"},
		{"A", "a"},
		{"Ab", "ab"},
	}
	for _, tt := range tests {
		got := toLowerCamel(tt.in)
		if got != tt.want {
			t.Errorf("toLowerCamel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSanitizeGoParam(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"type", "type_"},
		{"func", "func_"},
		{"map", "map_"},
		{"email", "email"},
		{"displayName", "displayName"},
	}
	for _, tt := range tests {
		got := sanitizeGoParam(tt.in)
		if got != tt.want {
			t.Errorf("sanitizeGoParam(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFieldIsRequired(t *testing.T) {
	tests := []struct {
		name string
		f    *DomainField
		want bool
	}{
		{
			name: "google.api.field_behavior REQUIRED",
			f: &DomainField{
				FieldBehaviors: []annotations.FieldBehavior{annotations.FieldBehavior_REQUIRED},
			},
			want: true,
		},
		{
			name: "buf.validate required",
			f: &DomainField{
				ValidateConstraints: &ValidateConstraints{Required: true},
			},
			want: true,
		},
		{
			name: "not required",
			f:    &DomainField{},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fieldIsRequired(tt.f)
			if got != tt.want {
				t.Errorf("fieldIsRequired() = %v, want %v", got, tt.want)
			}
		})
	}
}
