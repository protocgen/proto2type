package generator

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
)

// emitSearchIndexExtraction generates Go code that extracts search index rows
// from a proto message using compiled FHIRPath expressions.
//
// This is called by generateGoPostgresSearchIndex when postgres_mode=search_index.
// It replaces the naive field-walking approach with SearchParameter-driven code
// that correctly handles nested paths, .where() filters, and choice types.
func emitSearchIndexExtraction(g *protogen.GeneratedFile, msg *protogen.Message, params []SearchParam, resType string) error {
	for _, sp := range params {
		compiled, err := CompileFHIRPath(sp, resType)
		if err != nil {
			// Skip params that don't apply to this resource
			continue
		}

		switch sp.Type {
		case "string":
			emitStringExtraction(g, compiled)
		case "token":
			emitTokenExtraction(g, compiled, msg)
		case "date":
			emitDateExtraction(g, compiled)
		case "reference":
			emitReferenceExtraction(g, compiled, resType)
		case "quantity":
			emitQuantityExtraction(g, compiled)
		case "uri":
			emitURIExtraction(g, compiled)
		}
	}

	return nil
}

// emitStringExtraction generates code to extract a string search parameter.
// Examples:
//   - Patient.name.family → nested loop over name, get family
//   - Patient.address.city → nested loop over address, get city
func emitStringExtraction(g *protogen.GeneratedFile, c *CompiledFHIRPath) {
	sp := c.SearchParam
	g.P(fmt.Sprintf("\t// SearchParameter: %s (string)", sp.Name))
	g.P(fmt.Sprintf("\t// FHIRPath: %s", sp.Expression))

	switch len(c.Segments) {
	case 1:
		// Simple: Patient.family (shouldn't happen for string, but handle it)
		seg := c.Segments[0]
		g.P(fmt.Sprintf("\tif v := r.%s; v != nil && v.GetValue() != \"\" {", seg.GoGetter))
		g.P("\t\tidx.Strings = append(idx.Strings, SpidxString{")
		g.P("\t\t\tTenantID: tenantID,")
		g.P(fmt.Sprintf("\t\t\tResType:  %q,", c.ResType))
		g.P("\t\t\tResID:    resID,")
		g.P(fmt.Sprintf("\t\t\tSpName:   %q,", sp.Name))
		g.P("\t\t\tSpValue:  strings.ToLower(v.GetValue()),")
		g.P("\t\t})")
		g.P("\t}")
	case 2:
		// Nested: Patient.name.family, Patient.address.city
		outer := c.Segments[0]
		inner := c.Segments[1]
		g.P(fmt.Sprintf("\tfor _, outer := range r.%s {", outer.GoGetter))
		g.P("\t\tif outer == nil { continue }")

		if isRepeatedFieldName(inner.Field) {
			// Inner is also repeated (e.g., given names)
			g.P(fmt.Sprintf("\t\tfor _, v := range outer.%s {", inner.GoGetter))
			g.P("\t\t\tif v != nil && v.GetValue() != \"\" {")
			g.P("\t\t\t\tidx.Strings = append(idx.Strings, SpidxString{")
			g.P("\t\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\t\tSpValue:  strings.ToLower(v.GetValue()),")
			g.P("\t\t\t\t})")
			g.P("\t\t\t}")
			g.P("\t\t}")
		} else {
			// Inner is singular: Patient.name.family (family is singular on HumanName)
			g.P(fmt.Sprintf("\t\tif v := outer.%s; v != nil && v.GetValue() != \"\" {", inner.GoGetter))
			g.P("\t\t\tidx.Strings = append(idx.Strings, SpidxString{")
			g.P("\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\tSpValue:  strings.ToLower(v.GetValue()),")
			g.P("\t\t\t})")
			g.P("\t\t}")
		}
		g.P("\t}")
	}
	g.P()
}

// emitTokenExtraction generates code for token search parameters.
// Spike 4 findings applied: handles these FHIR token sub-types:
//   - Identifier: repeated, extract system + value
//   - Boolean: wrapped bool → "true" / "false"
//   - Code enum: GetValue().String() for enum name
//   - CodeableConcept: nested coding → system + code
//   - .where() filter: telecom.where(system='email')
//   - .exists(): deceased.exists()
func emitTokenExtraction(g *protogen.GeneratedFile, c *CompiledFHIRPath, msg *protogen.Message) {
	sp := c.SearchParam

	g.P(fmt.Sprintf("\t// SearchParameter: %s (token)", sp.Name))
	g.P(fmt.Sprintf("\t// FHIRPath: %s", sp.Expression))

	// Handle .exists() pattern
	if c.IsExists {
		seg := c.Segments[0]
		g.P("\t// exists() check — index whether field is present")
		g.P(fmt.Sprintf("\tif r.%s != nil {", seg.GoGetter))
		g.P("\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
		g.P("\t\t\tTenantID: tenantID,")
		g.P(fmt.Sprintf("\t\t\tResType:  %q,", c.ResType))
		g.P("\t\t\tResID:    resID,")
		g.P(fmt.Sprintf("\t\t\tSpName:   %q,", sp.Name))
		g.P("\t\t\tSpValue:  \"true\",")
		g.P("\t\t})")
		g.P("\t}")
		g.P()
		return
	}

	// Handle .where() filter (e.g. telecom.where(system='email'))
	if len(c.Segments) > 0 && c.Segments[0].WhereField != "" {
		seg := c.Segments[0]
		g.P(fmt.Sprintf("\tfor _, item := range r.%s {", seg.GoGetter))
		g.P("\t\tif item == nil { continue }")
		g.P(fmt.Sprintf("\t\tif item.Get%s() == nil || item.Get%s().GetValue() != %q { continue }",
			capitalize(seg.WhereField), capitalize(seg.WhereField), seg.WhereValue))
		g.P("\t\tif item.GetValue() != nil && item.GetValue().GetValue() != \"\" {")
		g.P("\t\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
		g.P("\t\t\t\tTenantID: tenantID,")
		g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
		g.P("\t\t\t\tResID:    resID,")
		g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
		g.P(fmt.Sprintf("\t\t\t\tSpSystem: %q,", seg.WhereValue))
		g.P("\t\t\t\tSpValue:  item.GetValue().GetValue(),")
		g.P("\t\t\t})")
		g.P("\t\t}")
		g.P("\t}")
		g.P()
		return
	}

	if len(c.Segments) != 1 && len(c.Segments) != 2 {
		g.P("\t// TODO: unsupported token path depth")
		g.P()
		return
	}

	// Single-segment tokens: detect field type from proto descriptor
	if len(c.Segments) == 1 {
		seg := c.Segments[0]
		fieldName := seg.Field

		// Detect the field type from the proto message if available
		fieldType := detectFieldType(msg, fieldName)

		switch fieldType {
		case "identifier":
			// Repeated Identifier: system + value (Spike 4 validated)
			g.P(fmt.Sprintf("\tfor _, ident := range r.%s {", seg.GoGetter))
			g.P("\t\tif ident == nil { continue }")
			g.P("\t\tsystem := \"\"")
			g.P("\t\tif ident.GetSystem() != nil { system = ident.GetSystem().GetValue() }")
			g.P("\t\tvalue := \"\"")
			g.P("\t\tif ident.GetValue() != nil { value = ident.GetValue().GetValue() }")
			g.P("\t\tif value != \"\" {")
			g.P("\t\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
			g.P("\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\tSpSystem: system,")
			g.P("\t\t\t\tSpValue:  value,")
			g.P("\t\t\t})")
			g.P("\t\t}")
			g.P("\t}")

		case "boolean":
			// Boolean: extract true/false as string (Spike 4 validated)
			g.P(fmt.Sprintf("\tif r.%s != nil {", seg.GoGetter))
			g.P("\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
			g.P("\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\tSpName:   %q,", sp.Name))
			g.P(fmt.Sprintf("\t\t\tSpValue:  fmt.Sprintf(\"%%v\", r.%s.GetValue()),", seg.GoGetter))
			g.P("\t\t})")
			g.P("\t}")

		case "enum":
			// Enum (code): extract value enum name as string (Spike 4 validated)
			g.P(fmt.Sprintf("\tif r.%s != nil {", seg.GoGetter))
			g.P("\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
			g.P("\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\tSpName:   %q,", sp.Name))
			g.P(fmt.Sprintf("\t\t\tSpValue:  r.%s.GetValue().String(),", seg.GoGetter))
			g.P("\t\t})")
			g.P("\t}")

		default:
			// Generic fallback
			g.P(fmt.Sprintf("\tif r.%s != nil {", seg.GoGetter))
			g.P("\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
			g.P("\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\tSpName:   %q,", sp.Name))
			g.P(fmt.Sprintf("\t\t\tSpValue:  fmt.Sprintf(\"%%v\", r.%s),", seg.GoGetter))
			g.P("\t\t})")
			g.P("\t}")
		}
	} else {
		// Nested token (2 segments): e.g. communication.language
		outer := c.Segments[0]
		inner := c.Segments[1]
		g.P(fmt.Sprintf("\tfor _, outer := range r.%s {", outer.GoGetter))
		g.P("\t\tif outer == nil { continue }")
		g.P(fmt.Sprintf("\t\tif v := outer.%s; v != nil {", inner.GoGetter))
		// CodeableConcept: extract coding system + code
		g.P("\t\t\t// CodeableConcept → extract codings")
		g.P("\t\t\tfor _, coding := range v.GetCoding() {")
		g.P("\t\t\t\tif coding == nil { continue }")
		g.P("\t\t\t\tsystem := \"\"")
		g.P("\t\t\t\tif coding.GetSystem() != nil { system = coding.GetSystem().GetValue() }")
		g.P("\t\t\t\tcode := \"\"")
		g.P("\t\t\t\tif coding.GetCode() != nil { code = coding.GetCode().GetValue() }")
		g.P("\t\t\t\tif code != \"\" {")
		g.P("\t\t\t\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
		g.P("\t\t\t\t\t\tTenantID: tenantID,")
		g.P(fmt.Sprintf("\t\t\t\t\t\tResType:  %q,", c.ResType))
		g.P("\t\t\t\t\t\tResID:    resID,")
		g.P(fmt.Sprintf("\t\t\t\t\t\tSpName:   %q,", sp.Name))
		g.P("\t\t\t\t\t\tSpSystem: system,")
		g.P("\t\t\t\t\t\tSpValue:  code,")
		g.P("\t\t\t\t\t})")
		g.P("\t\t\t\t}")
		g.P("\t\t\t}")
		g.P("\t\t}")
		g.P("\t}")
	}
	g.P()
}

// emitDateExtraction generates code for date search parameters.
// google/fhir dates use {value_us: int64, precision: enum} — microsecond epoch.
// Spike 4 finding: always use UTC for consistency.
func emitDateExtraction(g *protogen.GeneratedFile, c *CompiledFHIRPath) {
	sp := c.SearchParam

	g.P(fmt.Sprintf("\t// SearchParameter: %s (date)", sp.Name))
	g.P(fmt.Sprintf("\t// FHIRPath: %s", sp.Expression))

	if c.IsChoiceType {
		// (Patient.deceased as dateTime) — choice type
		g.P(fmt.Sprintf("\t// Choice type: %s as %s", c.ChoiceField, c.ChoiceType))
		g.P(fmt.Sprintf("\t// TODO: handle oneof %s — check for %s variant", c.ChoiceField, c.ChoiceType))
		g.P()
		return
	}

	if len(c.Segments) == 1 {
		seg := c.Segments[0]
		g.P(fmt.Sprintf("\tif d := r.%s; d != nil {", seg.GoGetter))
		g.P("\t\tt := time.UnixMicro(d.GetValueUs()).UTC()")
		g.P("\t\tidx.Dates = append(idx.Dates, SpidxDate{")
		g.P("\t\t\tTenantID: tenantID,")
		g.P(fmt.Sprintf("\t\t\tResType:  %q,", c.ResType))
		g.P("\t\t\tResID:    resID,")
		g.P(fmt.Sprintf("\t\t\tSpName:   %q,", sp.Name))
		g.P("\t\t\tSpLow:    t,")
		g.P("\t\t\tSpHigh:   t,")
		g.P("\t\t})")
		g.P("\t}")
	}
	g.P()
}

// emitReferenceExtraction generates code for reference search parameters.
// google/fhir Reference uses a oneof with:
//   - URI-based: ref.GetUri() → "ResourceType/id" string
//   - Typed: ref.Get<Type>Id() → ReferenceId{Value: "id"} (Spike 4 discovery)
//
// We generate both extraction paths.
func emitReferenceExtraction(g *protogen.GeneratedFile, c *CompiledFHIRPath, resType string) {
	sp := c.SearchParam

	g.P(fmt.Sprintf("\t// SearchParameter: %s (reference)", sp.Name))
	g.P(fmt.Sprintf("\t// FHIRPath: %s", sp.Expression))
	g.P("\t// google/fhir Reference: try URI first, then typed reference ID")

	if len(c.Segments) == 0 {
		g.P()
		return
	}

	// Determine target resource type from the field name for typed reference
	// e.g. "managing_organization" → "Organization"
	lastSeg := c.Segments[len(c.Segments)-1]
	typedRefTarget := inferRefTargetType(lastSeg.Field)

	if len(c.Segments) == 1 {
		seg := c.Segments[0]
		emitSingleRefExtraction(g, c, sp, seg.GoGetter, "r", typedRefTarget)
	} else if len(c.Segments) == 2 {
		seg := c.Segments[0]
		// Repeated field containing references
		g.P(fmt.Sprintf("\tfor _, outer := range r.%s {", seg.GoGetter))
		g.P("\t\tif outer == nil { continue }")
		emitSingleRefExtraction(g, c, sp, lastSeg.GoGetter, "outer", typedRefTarget)
		g.P("\t}")
	}
	g.P()
}

// emitSingleRefExtraction emits code to extract one Reference field.
// varName is the variable holding the parent (e.g. "r" or "outer").
func emitSingleRefExtraction(g *protogen.GeneratedFile, c *CompiledFHIRPath, sp SearchParam, getter, varName, typedTarget string) {
	g.P(fmt.Sprintf("\tif ref := %s.%s; ref != nil {", varName, getter))
	// Path 1: URI-based reference
	g.P("\t\t// URI-based reference: \"Organization/org-123\"")
	g.P("\t\tif uri := ref.GetUri(); uri != nil && uri.GetValue() != \"\" {")
	g.P("\t\t\tparts := strings.SplitN(uri.GetValue(), \"/\", 2)")
	g.P("\t\t\tif len(parts) == 2 {")
	g.P("\t\t\t\tidx.References = append(idx.References, SpidxReference{")
	g.P("\t\t\t\t\tTenantID:   tenantID,")
	g.P(fmt.Sprintf("\t\t\t\t\tResType:    %q,", c.ResType))
	g.P("\t\t\t\t\tResID:      resID,")
	g.P(fmt.Sprintf("\t\t\t\t\tSpName:     %q,", sp.Name))
	g.P("\t\t\t\t\tTargetType: parts[0],")
	g.P("\t\t\t\t\tTargetID:   parts[1],")
	g.P("\t\t\t\t})")
	g.P("\t\t\t}")
	g.P("\t\t}")

	// Path 2: Typed reference (google/fhir oneof)
	if typedTarget != "" {
		g.P(fmt.Sprintf("\t\t// Typed reference: Get%sId()", typedTarget))
		g.P(fmt.Sprintf("\t\tif typedRef := ref.Get%sId(); typedRef != nil && typedRef.GetValue() != \"\" {", typedTarget))
		g.P("\t\t\tidx.References = append(idx.References, SpidxReference{")
		g.P("\t\t\t\tTenantID:   tenantID,")
		g.P(fmt.Sprintf("\t\t\t\tResType:    %q,", c.ResType))
		g.P("\t\t\t\tResID:      resID,")
		g.P(fmt.Sprintf("\t\t\t\tSpName:     %q,", sp.Name))
		g.P(fmt.Sprintf("\t\t\t\tTargetType: %q,", typedTarget))
		g.P("\t\t\t\tTargetID:   typedRef.GetValue(),")
		g.P("\t\t\t})")
		g.P("\t\t}")
	}

	g.P("\t}")
}

// emitQuantityExtraction generates code for quantity search parameters.
func emitQuantityExtraction(g *protogen.GeneratedFile, c *CompiledFHIRPath) {
	sp := c.SearchParam
	g.P(fmt.Sprintf("\t// SearchParameter: %s (quantity)", sp.Name))
	g.P(fmt.Sprintf("\t// FHIRPath: %s", sp.Expression))
	g.P("\t// TODO: implement quantity extraction")
	g.P()
}

// emitURIExtraction generates code for URI search parameters.
func emitURIExtraction(g *protogen.GeneratedFile, c *CompiledFHIRPath) {
	sp := c.SearchParam
	g.P(fmt.Sprintf("\t// SearchParameter: %s (uri)", sp.Name))
	g.P(fmt.Sprintf("\t// FHIRPath: %s", sp.Expression))
	g.P("\t// TODO: implement URI extraction")
	g.P()
}

// capitalize returns s with the first letter uppercased.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// isRepeatedFieldName returns true for FHIR fields known to be repeated.
// This is a heuristic — the proto descriptor is the source of truth,
// but during code generation from FHIRPath we don't have proto descriptors.
func isRepeatedFieldName(field string) bool {
	switch field {
	case "given", "prefix", "suffix", "line", "coding", "extension":
		return true
	}
	return false
}

// detectFieldType uses proto message descriptors to determine the FHIR type
// of a field. Returns: "identifier", "boolean", "enum", or "" (unknown).
func detectFieldType(msg *protogen.Message, fieldName string) string {
	if msg == nil {
		return ""
	}
	for _, f := range msg.Fields {
		if toSnakeCase(string(f.Desc.Name())) != fieldName && string(f.Desc.Name()) != fieldName {
			continue
		}
		// Check message type name for known FHIR types
		if f.Message != nil {
			msgName := string(f.Message.Desc.Name())
			switch {
			case msgName == "Identifier" || strings.HasSuffix(msgName, ".Identifier"):
				return "identifier"
			case msgName == "Boolean":
				return "boolean"
			}
		}
		// Check if the field is a message wrapping an enum (e.g. GenderCode)
		if f.Message != nil {
			for _, subField := range f.Message.Fields {
				if string(subField.Desc.Name()) == "value" && subField.Desc.Kind().String() == "enum" {
					return "enum"
				}
			}
		}
		return ""
	}
	return ""
}

// inferRefTargetType infers the target resource type from a reference field name.
// e.g. "managing_organization" → "Organization"
// e.g. "general_practitioner" → "Practitioner"
// e.g. "other" → "" (can't infer)
func inferRefTargetType(fieldName string) string {
	knownRefs := map[string]string{
		"managing_organization": "Organization",
		"managingorganization":  "Organization",
		"general_practitioner":  "Practitioner",
		"generalpractitioner":   "Practitioner",
		"organization":          "Organization",
		"subject":               "", // ambiguous
		"patient":               "Patient",
		"encounter":             "Encounter",
		"practitioner":          "Practitioner",
		"location":              "Location",
		"performer":             "", // ambiguous
		"asserter":              "", // ambiguous
		"recorder":              "", // ambiguous
		"requester":             "", // ambiguous
		"author":                "", // ambiguous
		"other":                 "", // ambiguous
		"link":                  "", // ambiguous
		"custodian":             "Organization",
		"insurer":               "Organization",
		"provider":              "", // ambiguous
		"service_provider":      "Organization",
		"serviceprovider":       "Organization",
		"part_of":               "", // ambiguous
		"partof":                "", // ambiguous
		"based_on":              "", // ambiguous
		"basedon":               "", // ambiguous
		"source":                "", // ambiguous
		"focus":                 "", // ambiguous
		"context":               "", // ambiguous
		"replaces":              "", // ambiguous
	}

	if target, ok := knownRefs[fieldName]; ok {
		return target
	}
	return ""
}
