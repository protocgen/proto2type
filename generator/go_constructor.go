package generator

import (
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

// generateGoConstructor emits a NewXxx constructor function that takes all
// required fields as parameters. This provides construction-time enforcement
// of required fields — callers cannot forget to set them.
//
// Fields marked OUTPUT_ONLY are excluded (server-set fields like id, created_at).
// Fields are ordered by proto field number.
func generateGoConstructor(g *protogen.GeneratedFile, dm *DomainMessage) {
	type requiredParam struct {
		paramName string // camelCase parameter name
		fieldName string // PascalCase struct field name
		fieldType string // Go type
	}

	var params []requiredParam
	for _, f := range dm.Fields {
		if f.IsOneof || f.FieldSkip {
			continue
		}
		if !fieldIsRequired(f) {
			continue
		}
		// Exclude OUTPUT_ONLY fields — they're set by the server, not the caller.
		if f.IsOutputOnly() {
			continue
		}
		params = append(params, requiredParam{
			paramName: toLowerCamel(f.PascalName),
			fieldName: f.PascalName,
			fieldType: goDomainFieldTypeFromIR(f),
		})
	}

	if len(params) == 0 {
		return
	}

	// Build parameter list: "email string, displayName string"
	var paramParts []string
	for _, p := range params {
		paramParts = append(paramParts, p.paramName+" "+p.fieldType)
	}

	g.P("// New", dm.Name, " creates a ", dm.Name, " with all required fields.")
	g.P("func New", dm.Name, "(", strings.Join(paramParts, ", "), ") *", dm.Name, " {")
	g.P("\treturn &", dm.Name, "{")
	for _, p := range params {
		g.P("\t\t", p.fieldName, ": ", p.paramName, ",")
	}
	g.P("\t}")
	g.P("}")
	g.P()
}

// fieldIsRequired returns true if a field is required via either
// google.api.field_behavior=REQUIRED or (buf.validate.field).required=true.
func fieldIsRequired(f *DomainField) bool {
	if f.IsRequired() {
		return true
	}
	if f.ValidateConstraints != nil && f.ValidateConstraints.Required {
		return true
	}
	return false
}

// toLowerCamel converts a PascalCase name to lowerCamelCase.
// e.g. "DisplayName" → "displayName", "ID" → "id", "Email" → "email"
func toLowerCamel(s string) string {
	if s == "" {
		return s
	}
	// Find the first lowercase letter to determine the prefix boundary.
	// "ID" → "id", "URL" → "url", "DisplayName" → "displayName"
	i := 0
	for i < len(s) && s[i] >= 'A' && s[i] <= 'Z' {
		i++
	}
	if i == 0 {
		return s
	}
	if i == len(s) {
		// All uppercase: "ID" → "id"
		return strings.ToLower(s)
	}
	if i == 1 {
		// Single uppercase: "Email" → "email"
		return strings.ToLower(s[:1]) + s[1:]
	}
	// Multiple uppercase followed by lowercase: "HTMLParser" → "htmlParser"
	return strings.ToLower(s[:i-1]) + s[i-1:]
}
