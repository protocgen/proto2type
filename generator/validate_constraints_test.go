package generator

import (
	"testing"

	"google.golang.org/genproto/googleapis/api/annotations"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
)

func TestExtractNumericBounds(t *testing.T) {
	s := func(v string) *string { return &v }

	t.Run("extractUInt64Bounds", func(t *testing.T) {
		vc := &ValidateConstraints{}
		extractUInt64Bounds(&validate.UInt64Rules{
			GreaterThan: &validate.UInt64Rules_Gte{Gte: 10},
			LessThan:    &validate.UInt64Rules_Lt{Lt: 100},
		}, vc)
		assertBound(t, "Gte", vc.Gte, s("10"))
		assertBound(t, "Lt", vc.Lt, s("100"))
	})

	t.Run("extractSInt32Bounds", func(t *testing.T) {
		vc := &ValidateConstraints{}
		extractSInt32Bounds(&validate.SInt32Rules{
			GreaterThan: &validate.SInt32Rules_Gt{Gt: -5},
			LessThan:    &validate.SInt32Rules_Lte{Lte: 50},
		}, vc)
		assertBound(t, "Gt", vc.Gt, s("-5"))
		assertBound(t, "Lte", vc.Lte, s("50"))
	})

	t.Run("extractSInt64Bounds", func(t *testing.T) {
		vc := &ValidateConstraints{}
		extractSInt64Bounds(&validate.SInt64Rules{
			GreaterThan: &validate.SInt64Rules_Gte{Gte: 0},
			LessThan:    &validate.SInt64Rules_Lt{Lt: 9999},
		}, vc)
		assertBound(t, "Gte", vc.Gte, s("0"))
		assertBound(t, "Lt", vc.Lt, s("9999"))
	})

	t.Run("extractFixed32Bounds", func(t *testing.T) {
		vc := &ValidateConstraints{}
		extractFixed32Bounds(&validate.Fixed32Rules{
			GreaterThan: &validate.Fixed32Rules_Gt{Gt: 1},
			LessThan:    &validate.Fixed32Rules_Lte{Lte: 255},
		}, vc)
		assertBound(t, "Gt", vc.Gt, s("1"))
		assertBound(t, "Lte", vc.Lte, s("255"))
	})

	t.Run("extractFixed64Bounds", func(t *testing.T) {
		vc := &ValidateConstraints{}
		extractFixed64Bounds(&validate.Fixed64Rules{
			GreaterThan: &validate.Fixed64Rules_Gte{Gte: 0},
			LessThan:    &validate.Fixed64Rules_Lt{Lt: 1000000},
		}, vc)
		assertBound(t, "Gte", vc.Gte, s("0"))
		assertBound(t, "Lt", vc.Lt, s("1000000"))
	})

	t.Run("extractSFixed32Bounds", func(t *testing.T) {
		vc := &ValidateConstraints{}
		extractSFixed32Bounds(&validate.SFixed32Rules{
			GreaterThan: &validate.SFixed32Rules_Gt{Gt: -100},
			LessThan:    &validate.SFixed32Rules_Lte{Lte: 100},
		}, vc)
		assertBound(t, "Gt", vc.Gt, s("-100"))
		assertBound(t, "Lte", vc.Lte, s("100"))
	})

	t.Run("extractSFixed64Bounds", func(t *testing.T) {
		vc := &ValidateConstraints{}
		extractSFixed64Bounds(&validate.SFixed64Rules{
			GreaterThan: &validate.SFixed64Rules_Gte{Gte: -1},
			LessThan:    &validate.SFixed64Rules_Lt{Lt: 1},
		}, vc)
		assertBound(t, "Gte", vc.Gte, s("-1"))
		assertBound(t, "Lt", vc.Lt, s("1"))
	})

	t.Run("zero-value rules leave bounds nil", func(t *testing.T) {
		vc := &ValidateConstraints{}
		extractUInt64Bounds(&validate.UInt64Rules{}, vc)
		extractSInt32Bounds(&validate.SInt32Rules{}, vc)
		extractSInt64Bounds(&validate.SInt64Rules{}, vc)
		extractFixed32Bounds(&validate.Fixed32Rules{}, vc)
		extractFixed64Bounds(&validate.Fixed64Rules{}, vc)
		extractSFixed32Bounds(&validate.SFixed32Rules{}, vc)
		extractSFixed64Bounds(&validate.SFixed64Rules{}, vc)
		assertBound(t, "Gt", vc.Gt, nil)
		assertBound(t, "Gte", vc.Gte, nil)
		assertBound(t, "Lt", vc.Lt, nil)
		assertBound(t, "Lte", vc.Lte, nil)
	})
}

func assertBound(t *testing.T, name string, got, want *string) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Errorf("%s = %q, want nil", name, *got)
		}
		return
	}
	if got == nil {
		t.Errorf("%s = nil, want %q", name, *want)
		return
	}
	if *got != *want {
		t.Errorf("%s = %q, want %q", name, *got, *want)
	}
}

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
