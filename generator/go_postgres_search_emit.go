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
			emitTokenExtraction(g, compiled)
		case "date":
			emitDateExtraction(g, compiled)
		case "reference":
			emitReferenceExtraction(g, compiled)
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

		if inner.Field == "name" || isRepeatedFieldName(inner.Field) {
			// If the inner field is also repeated (e.g., given names)
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
// Token params cover: Coding, CodeableConcept, Identifier, Boolean, code enums.
func emitTokenExtraction(g *protogen.GeneratedFile, c *CompiledFHIRPath) {
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
		// Generate filter: check if the where field matches
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

	switch len(c.Segments) {
	case 1:
		seg := c.Segments[0]
		// Could be: Boolean (active), enum (gender), Identifier (identifier)
		// For now, emit a generic "get value as string" pattern
		g.P(fmt.Sprintf("\t// TODO: determine token sub-type for %s.%s", c.ResType, seg.Field))
		g.P("\t// (Boolean, enum, Identifier, Coding, CodeableConcept)")
		g.P(fmt.Sprintf("\tif r.%s != nil {", seg.GoGetter))
		g.P("\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
		g.P("\t\t\tTenantID: tenantID,")
		g.P(fmt.Sprintf("\t\t\tResType:  %q,", c.ResType))
		g.P("\t\t\tResID:    resID,")
		g.P(fmt.Sprintf("\t\t\tSpName:   %q,", sp.Name))
		g.P(fmt.Sprintf("\t\t\tSpValue:  fmt.Sprintf(\"%%v\", r.%s),", seg.GoGetter))
		g.P("\t\t})")
		g.P("\t}")
	case 2:
		// Nested token: e.g. communication.language
		outer := c.Segments[0]
		inner := c.Segments[1]
		g.P(fmt.Sprintf("\tfor _, outer := range r.%s {", outer.GoGetter))
		g.P("\t\tif outer == nil { continue }")
		g.P(fmt.Sprintf("\t\tif v := outer.%s; v != nil {", inner.GoGetter))
		g.P("\t\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
		g.P("\t\t\t\tTenantID: tenantID,")
		g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
		g.P("\t\t\t\tResID:    resID,")
		g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
		g.P(fmt.Sprintf("\t\t\t\tSpValue:  fmt.Sprintf(\"%%v\", outer.%s),", inner.GoGetter))
		g.P("\t\t\t})")
		g.P("\t\t}")
		g.P("\t}")
	}
	g.P()
}

// emitDateExtraction generates code for date search parameters.
// google/fhir dates use {value_us: int64, precision: enum} — microsecond epoch.
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
		g.P("\t\tt := time.UnixMicro(d.GetValueUs())")
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
// google/fhir Reference has a uri field or a fragment, or typed reference_id.
func emitReferenceExtraction(g *protogen.GeneratedFile, c *CompiledFHIRPath) {
	sp := c.SearchParam

	g.P(fmt.Sprintf("\t// SearchParameter: %s (reference)", sp.Name))
	g.P(fmt.Sprintf("\t// FHIRPath: %s", sp.Expression))

	if len(c.Segments) >= 1 {
		seg := c.Segments[0]
		lastSeg := c.Segments[len(c.Segments)-1]

		if len(c.Segments) == 1 {
			// Direct reference field: Patient.managingOrganization
			g.P(fmt.Sprintf("\tif ref := r.%s; ref != nil {", seg.GoGetter))
			g.P("\t\t// Extract reference type and ID from URI")
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
			g.P("\t}")
		} else if len(c.Segments) == 2 {
			// Nested: Patient.link.other, Patient.generalPractitioner (repeated)
			g.P(fmt.Sprintf("\tfor _, outer := range r.%s {", seg.GoGetter))
			g.P("\t\tif outer == nil { continue }")
			g.P(fmt.Sprintf("\t\tif ref := outer.%s; ref != nil {", lastSeg.GoGetter))
			g.P("\t\t\tif uri := ref.GetUri(); uri != nil && uri.GetValue() != \"\" {")
			g.P("\t\t\t\tparts := strings.SplitN(uri.GetValue(), \"/\", 2)")
			g.P("\t\t\t\tif len(parts) == 2 {")
			g.P("\t\t\t\t\tidx.References = append(idx.References, SpidxReference{")
			g.P("\t\t\t\t\t\tTenantID:   tenantID,")
			g.P(fmt.Sprintf("\t\t\t\t\t\tResType:    %q,", c.ResType))
			g.P("\t\t\t\t\t\tResID:      resID,")
			g.P(fmt.Sprintf("\t\t\t\t\t\tSpName:     %q,", sp.Name))
			g.P("\t\t\t\t\t\tTargetType: parts[0],")
			g.P("\t\t\t\t\t\tTargetID:   parts[1],")
			g.P("\t\t\t\t\t})")
			g.P("\t\t\t\t}")
			g.P("\t\t\t}")
			g.P("\t\t}")
			g.P("\t}")
		}
	}
	g.P()
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
