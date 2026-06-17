package generator

import "testing"

func TestValidateFieldNameOverride(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Valid names
		{"simple", "model_id", false},
		{"camelCase", "modelId", false},
		{"with_numbers", "field_1", false},
		{"with_hyphens", "my-field", false},
		{"with_underscore", "_id", false},
		{"empty", "", false},

		// Invalid: contains dangerous characters
		{"dot", "path.nested", true},
		{"slash", "path/traversal", true},
		{"dollar", "$ref", true},
		{"open_bracket", "arr[0]", true},
		{"close_bracket", "arr]", true},
		{"null_byte", "field\x00name", true},
		{"dollar_prefix", "$set", true},
		{"mongo_operator", "$unset", true},
		{"firestore_path", "a.b.c", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateFieldNameOverride(tt.input)
			gotErr := result != ""
			if gotErr != tt.wantErr {
				if tt.wantErr {
					t.Errorf("validateFieldNameOverride(%q) = %q, want error", tt.input, result)
				} else {
					t.Errorf("validateFieldNameOverride(%q) = %q, want empty (valid)", tt.input, result)
				}
			}
		})
	}
}
