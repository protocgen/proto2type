package generator

import "testing"

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"model_id", "ModelID"},
		{"input_per_million", "InputPerMillion"},
		{"display_name", "DisplayName"},
		{"enabled", "Enabled"},
		{"provider_model_id", "ProviderModelID"},
		{"context_window", "ContextWindow"},
		{"api_url", "APIURL"},
		{"http_endpoint", "HTTPEndpoint"},
		{"", ""},
		{"a", "A"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toPascalCase(tt.input)
			if got != tt.want {
				t.Errorf("toPascalCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestOutputFilename(t *testing.T) {
	tests := []struct {
		path   string
		suffix string
		want   string
	}{
		{"model_catalog.proto", ".type.go", "model_catalog.type.go"},
		{"candela/types/model_catalog.proto", "_firestore.type.go", "candela/types/model_catalog_firestore.type.go"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := outputFilename(tt.path, tt.suffix)
			if got != tt.want {
				t.Errorf("outputFilename(%q, %q) = %q, want %q", tt.path, tt.suffix, got, tt.want)
			}
		})
	}
}
