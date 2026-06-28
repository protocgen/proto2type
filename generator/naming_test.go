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

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"modelId", "model_id"},
		{"DisplayName", "display_name"},
		{"HTMLParser", "html_parser"},
		{"inputPerMillion", "input_per_million"},
		{"ID", "id"},
		{"contextWindow", "context_window"},
		{"", ""},
		{"a", "a"},
		{"A", "a"},
		{"already_snake", "already_snake"},
		{"XMLHttpRequest", "xml_http_request"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := toSnakeCase(tt.input)
			if got != tt.want {
				t.Errorf("toSnakeCase(%q) = %q, want %q", tt.input, got, tt.want)
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

func TestParseGoPackage(t *testing.T) {
	tests := []struct {
		input       string
		wantImport  string
		wantPackage string
	}{
		// Standard format: import_path;package_name
		{"github.com/foo/bar;bar", "github.com/foo/bar", "bar"},
		{"github.com/foo/bar/gen;gen", "github.com/foo/bar/gen", "gen"},
		// Semicolon with different package name
		{"github.com/foo/bar/pb;models", "github.com/foo/bar/pb", "models"},
		// No semicolon: package name is last path element
		{"github.com/foo/bar", "github.com/foo/bar", "bar"},
		{"github.com/foo/bar/v2", "github.com/foo/bar/v2", "v2"},
		// Single element (no / or ;)
		{"mypackage", "mypackage", "mypackage"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotImport, gotPkg := parseGoPackage(tt.input)
			if gotImport != tt.wantImport {
				t.Errorf("parseGoPackage(%q) importPath = %q, want %q", tt.input, gotImport, tt.wantImport)
			}
			if gotPkg != tt.wantPackage {
				t.Errorf("parseGoPackage(%q) packageName = %q, want %q", tt.input, gotPkg, tt.wantPackage)
			}
		})
	}
}

func TestReceiverName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"User", "u"},
		{"ModelCatalogEntry", "m"},
		{"Address", "a"},
		{"UserFirestore", "u"},
		{"", "x"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := receiverName(tt.input)
			if got != tt.want {
				t.Errorf("receiverName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
