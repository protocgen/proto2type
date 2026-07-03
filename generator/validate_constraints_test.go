package generator

import (
	"testing"

	"google.golang.org/genproto/googleapis/api/annotations"
)

func TestValidateConstraints_HasConstraints(t *testing.T) {
	minLen := uint64(1)
	gt := "0"

	tests := []struct {
		name string
		vc   *ValidateConstraints
		want bool
	}{
		{"nil", nil, false},
		{"empty struct", &ValidateConstraints{}, false},
		{"required true", &ValidateConstraints{Required: true}, true},
		{"min length set", &ValidateConstraints{MinLength: &minLen}, true},
		{"gt set", &ValidateConstraints{Gt: &gt}, true},
		{"email true", &ValidateConstraints{Email: true}, true},
		{"uuid true", &ValidateConstraints{UUID: true}, true},
		{"uri true", &ValidateConstraints{URI: true}, true},
		{"pattern set", &ValidateConstraints{Pattern: "^[a-z]+$"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.vc.HasConstraints()
			if got != tt.want {
				t.Errorf("HasConstraints() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateConstraints_ToPydanticArgs(t *testing.T) {
	minLen := uint64(1)
	maxLen := uint64(100)
	minItems := uint64(1)
	maxItems := uint64(10)
	gt := "0"
	gte := "0"
	lte := "150"
	lt := "100"

	tests := []struct {
		name string
		vc   *ValidateConstraints
		want []string
	}{
		{
			name: "nil",
			vc:   nil,
			want: nil,
		},
		{
			name: "min and max length",
			vc:   &ValidateConstraints{MinLength: &minLen, MaxLength: &maxLen},
			want: []string{"min_length=1", "max_length=100"},
		},
		{
			name: "pattern",
			vc:   &ValidateConstraints{Pattern: "^[a-z]+$"},
			want: []string{"pattern='^[a-z]+$'"},
		},
		{
			name: "pattern_with_quotes",
			vc:   &ValidateConstraints{Pattern: `^[a-z']+$`},
			want: []string{`pattern='^[a-z\']+$'`},
		},
		{
			name: "pattern_with_backslash",
			vc:   &ValidateConstraints{Pattern: `^\d+$`},
			want: []string{`pattern='^\\d+$'`},
		},
		{
			name: "gt",
			vc:   &ValidateConstraints{Gt: &gt},
			want: []string{"gt=0"},
		},
		{
			name: "gte produces ge",
			vc:   &ValidateConstraints{Gte: &gte},
			want: []string{"ge=0"},
		},
		{
			name: "lte produces le",
			vc:   &ValidateConstraints{Lte: &lte},
			want: []string{"le=150"},
		},
		{
			name: "lt",
			vc:   &ValidateConstraints{Lt: &lt},
			want: []string{"lt=100"},
		},
		{
			name: "min and max items",
			vc:   &ValidateConstraints{MinItems: &minItems, MaxItems: &maxItems},
			want: []string{"min_length=1", "max_length=10"},
		},
		{
			name: "multiple constraints combined",
			vc: &ValidateConstraints{
				MinLength: &minLen,
				MaxLength: &maxLen,
				Gt:        &gt,
				Lte:       &lte,
			},
			want: []string{"min_length=1", "max_length=100", "gt=0", "le=150"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.vc.ToPydanticArgs()
			if tt.want == nil {
				if got != nil {
					t.Errorf("ToPydanticArgs() = %v, want nil", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ToPydanticArgs() returned %d args, want %d: got %v, want %v", len(got), len(tt.want), got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("ToPydanticArgs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDomainField_IsRequired(t *testing.T) {
	tests := []struct {
		name      string
		behaviors []annotations.FieldBehavior
		want      bool
	}{
		{
			name:      "required",
			behaviors: []annotations.FieldBehavior{annotations.FieldBehavior_REQUIRED},
			want:      true,
		},
		{
			name:      "no behaviors",
			behaviors: nil,
			want:      false,
		},
		{
			name:      "output only not required",
			behaviors: []annotations.FieldBehavior{annotations.FieldBehavior_OUTPUT_ONLY},
			want:      false,
		},
		{
			name:      "multiple with required",
			behaviors: []annotations.FieldBehavior{annotations.FieldBehavior_IMMUTABLE, annotations.FieldBehavior_REQUIRED},
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &DomainField{FieldBehaviors: tt.behaviors}
			if got := f.IsRequired(); got != tt.want {
				t.Errorf("IsRequired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDomainField_IsOutputOnly(t *testing.T) {
	tests := []struct {
		name      string
		behaviors []annotations.FieldBehavior
		want      bool
	}{
		{
			name:      "output only",
			behaviors: []annotations.FieldBehavior{annotations.FieldBehavior_OUTPUT_ONLY},
			want:      true,
		},
		{
			name:      "no behaviors",
			behaviors: nil,
			want:      false,
		},
		{
			name:      "required not output only",
			behaviors: []annotations.FieldBehavior{annotations.FieldBehavior_REQUIRED},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &DomainField{FieldBehaviors: tt.behaviors}
			if got := f.IsOutputOnly(); got != tt.want {
				t.Errorf("IsOutputOnly() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDomainField_IsInputOnly(t *testing.T) {
	tests := []struct {
		name      string
		behaviors []annotations.FieldBehavior
		want      bool
	}{
		{
			name:      "input only",
			behaviors: []annotations.FieldBehavior{annotations.FieldBehavior_INPUT_ONLY},
			want:      true,
		},
		{
			name:      "no behaviors",
			behaviors: nil,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &DomainField{FieldBehaviors: tt.behaviors}
			if got := f.IsInputOnly(); got != tt.want {
				t.Errorf("IsInputOnly() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDomainField_IsImmutable(t *testing.T) {
	tests := []struct {
		name      string
		behaviors []annotations.FieldBehavior
		want      bool
	}{
		{
			name:      "immutable",
			behaviors: []annotations.FieldBehavior{annotations.FieldBehavior_IMMUTABLE},
			want:      true,
		},
		{
			name:      "no behaviors",
			behaviors: nil,
			want:      false,
		},
		{
			name:      "multiple behaviors with immutable",
			behaviors: []annotations.FieldBehavior{annotations.FieldBehavior_REQUIRED, annotations.FieldBehavior_IMMUTABLE},
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &DomainField{FieldBehaviors: tt.behaviors}
			if got := f.IsImmutable(); got != tt.want {
				t.Errorf("IsImmutable() = %v, want %v", got, tt.want)
			}
		})
	}
}
