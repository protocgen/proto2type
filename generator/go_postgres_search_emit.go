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
			emitStringExtraction(g, compiled, msg)
		case "token":
			emitTokenExtraction(g, compiled, msg)
		case "date":
			emitDateExtraction(g, compiled)
		case "reference":
			emitReferenceExtraction(g, compiled, msg, resType)
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
func emitStringExtraction(g *protogen.GeneratedFile, c *CompiledFHIRPath, msg *protogen.Message) {
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

		outerField := findProtoField(msg, outer.Field)
		outerIsList := false
		if outerField != nil {
			outerIsList = outerField.Desc.IsList()
		} else {
			outerIsList = isRepeatedFieldName(outer.Field)
		}

		isList := false
		isScalarString := false
		if outerField != nil && outerField.Message != nil {
			if innerField := findProtoField(outerField.Message, inner.Field); innerField != nil {
				isList = innerField.Desc.IsList()
				isScalarString = innerField.Desc.Kind().String() == "string"
			} else {
				isList = isRepeatedFieldName(inner.Field)
			}
		} else {
			isList = isRepeatedFieldName(inner.Field)
		}

		if outerIsList {
			g.P(fmt.Sprintf("\tfor _, outer := range r.%s {", outer.GoGetter))
			g.P("\t\tif outer == nil { continue }")
		} else {
			g.P(fmt.Sprintf("\tif outer := r.%s; outer != nil {", outer.GoGetter))
		}
		indent := "\t\t"

		if isList {
			// Inner is repeated (e.g., given names)
			g.P(fmt.Sprintf("%sfor _, v := range outer.%s {", indent, inner.GoGetter))
			if isScalarString {
				g.P(fmt.Sprintf("%s\tif v != \"\" {", indent))
				g.P(fmt.Sprintf("%s\t\tidx.Strings = append(idx.Strings, SpidxString{", indent))
				g.P(fmt.Sprintf("%s\t\t\tTenantID: tenantID,", indent))
				g.P(fmt.Sprintf("%s\t\t\tResType:  %q,", indent, c.ResType))
				g.P(fmt.Sprintf("%s\t\t\tResID:    resID,", indent))
				g.P(fmt.Sprintf("%s\t\t\tSpName:   %q,", indent, sp.Name))
				g.P(fmt.Sprintf("%s\t\t\tSpValue:  strings.ToLower(v),", indent))
				g.P(fmt.Sprintf("%s\t\t})", indent))
				g.P(fmt.Sprintf("%s\t}", indent))
			} else {
				g.P(fmt.Sprintf("%s\tif v != nil && v.GetValue() != \"\" {", indent))
				g.P(fmt.Sprintf("%s\t\tidx.Strings = append(idx.Strings, SpidxString{", indent))
				g.P(fmt.Sprintf("%s\t\t\tTenantID: tenantID,", indent))
				g.P(fmt.Sprintf("%s\t\t\tResType:  %q,", indent, c.ResType))
				g.P(fmt.Sprintf("%s\t\t\tResID:    resID,", indent))
				g.P(fmt.Sprintf("%s\t\t\tSpName:   %q,", indent, sp.Name))
				g.P(fmt.Sprintf("%s\t\t\tSpValue:  strings.ToLower(v.GetValue()),", indent))
				g.P(fmt.Sprintf("%s\t\t})", indent))
				g.P(fmt.Sprintf("%s\t}", indent))
			}
			g.P(fmt.Sprintf("%s}", indent))
		} else {
			// Inner is singular: Patient.name.family (family is singular on HumanName)
			if isScalarString {
				g.P(fmt.Sprintf("%sif v := outer.%s; v != \"\" {", indent, inner.GoGetter))
				g.P(fmt.Sprintf("%s\tidx.Strings = append(idx.Strings, SpidxString{", indent))
				g.P(fmt.Sprintf("%s\t\tTenantID: tenantID,", indent))
				g.P(fmt.Sprintf("%s\t\tResType:  %q,", indent, c.ResType))
				g.P(fmt.Sprintf("%s\t\tResID:    resID,", indent))
				g.P(fmt.Sprintf("%s\t\tSpName:   %q,", indent, sp.Name))
				g.P(fmt.Sprintf("%s\t\tSpValue:  strings.ToLower(v),", indent))
				g.P(fmt.Sprintf("%s\t})", indent))
				g.P(fmt.Sprintf("%s}", indent))
			} else {
				g.P(fmt.Sprintf("%sif v := outer.%s; v != nil && v.GetValue() != \"\" {", indent, inner.GoGetter))
				g.P(fmt.Sprintf("%s\tidx.Strings = append(idx.Strings, SpidxString{", indent))
				g.P(fmt.Sprintf("%s\t\tTenantID: tenantID,", indent))
				g.P(fmt.Sprintf("%s\t\tResType:  %q,", indent, c.ResType))
				g.P(fmt.Sprintf("%s\t\tResID:    resID,", indent))
				g.P(fmt.Sprintf("%s\t\tSpName:   %q,", indent, sp.Name))
				g.P(fmt.Sprintf("%s\t\tSpValue:  strings.ToLower(v.GetValue()),", indent))
				g.P(fmt.Sprintf("%s\t})", indent))
				g.P(fmt.Sprintf("%s}", indent))
			}
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

		case "code":
			// Code: extract value string (e.g. gender code)
			field := findProtoField(msg, seg.Field)
			isList := field != nil && field.Desc.IsList()
			isScalar := field != nil && field.Desc.Kind().String() == "string"
			if isList {
				g.P(fmt.Sprintf("\tfor _, code := range r.%s {", seg.GoGetter))
				if isScalar {
					g.P("\t\tif code != \"\" {")
					g.P("\t\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
					g.P("\t\t\t\tTenantID: tenantID,")
					g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
					g.P("\t\t\t\tResID:    resID,")
					g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
					g.P("\t\t\t\tSpValue:  code,")
					g.P("\t\t\t})")
					g.P("\t\t}")
				} else {
					g.P("\t\tif code != nil && code.GetValue() != \"\" {")
					g.P("\t\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
					g.P("\t\t\t\tTenantID: tenantID,")
					g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
					g.P("\t\t\t\tResID:    resID,")
					g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
					g.P("\t\t\t\tSpValue:  code.GetValue(),")
					g.P("\t\t\t})")
					g.P("\t\t}")
				}
				g.P("\t}")
			} else {
				if isScalar {
					g.P(fmt.Sprintf("\tif r.%s != \"\" {", seg.GoGetter))
					g.P("\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
					g.P("\t\t\tTenantID: tenantID,")
					g.P(fmt.Sprintf("\t\t\tResType:  %q,", c.ResType))
					g.P("\t\t\tResID:    resID,")
					g.P(fmt.Sprintf("\t\t\tSpName:   %q,", sp.Name))
					g.P(fmt.Sprintf("\t\t\tSpValue:  r.%s,", seg.GoGetter))
					g.P("\t\t})")
					g.P("\t}")
				} else {
					g.P(fmt.Sprintf("\tif r.%s != nil && r.%s.GetValue() != \"\" {", seg.GoGetter, seg.GoGetter))
					g.P("\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
					g.P("\t\t\tTenantID: tenantID,")
					g.P(fmt.Sprintf("\t\t\tResType:  %q,", c.ResType))
					g.P("\t\t\tResID:    resID,")
					g.P(fmt.Sprintf("\t\t\tSpName:   %q,", sp.Name))
					g.P(fmt.Sprintf("\t\t\tSpValue:  r.%s.GetValue(),", seg.GoGetter))
					g.P("\t\t})")
					g.P("\t}")
				}
			}

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
// We generate both extraction paths when supported by the proto descriptor.
func emitReferenceExtraction(g *protogen.GeneratedFile, c *CompiledFHIRPath, msg *protogen.Message, resType string) {
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
		field := findProtoField(msg, seg.Field)
		isList := field != nil && field.Desc.IsList()
		var refMsg *protogen.Message
		if field != nil {
			refMsg = field.Message
		}

		if isList {
			g.P(fmt.Sprintf("\tfor _, ref := range r.%s {", seg.GoGetter))
			g.P("\t\tif ref == nil { continue }")
			emitRefExtractionBody(g, c, sp, "ref", refMsg, typedRefTarget, "\t")
			g.P("\t}")
		} else {
			g.P(fmt.Sprintf("\tif ref := r.%s; ref != nil {", seg.GoGetter))
			emitRefExtractionBody(g, c, sp, "ref", refMsg, typedRefTarget, "")
			g.P("\t}")
		}
	} else if len(c.Segments) == 2 {
		outerSeg := c.Segments[0]
		g.P(fmt.Sprintf("\tfor _, outer := range r.%s {", outerSeg.GoGetter))
		g.P("\t\tif outer == nil { continue }")
		var refMsg *protogen.Message
		isList := false
		if outerField := findProtoField(msg, outerSeg.Field); outerField != nil && outerField.Message != nil {
			if innerField := findProtoField(outerField.Message, lastSeg.Field); innerField != nil {
				isList = innerField.Desc.IsList()
				refMsg = innerField.Message
			}
		}

		if isList {
			g.P(fmt.Sprintf("\t\tfor _, ref := range outer.%s {", lastSeg.GoGetter))
			g.P("\t\t\tif ref == nil { continue }")
			emitRefExtractionBody(g, c, sp, "ref", refMsg, typedRefTarget, "\t\t")
			g.P("\t\t}")
		} else {
			g.P(fmt.Sprintf("\t\tif ref := outer.%s; ref != nil {", lastSeg.GoGetter))
			emitRefExtractionBody(g, c, sp, "ref", refMsg, typedRefTarget, "\t")
			g.P("\t\t}")
		}
		g.P("\t}")
	}
	g.P()
}

func emitRefExtractionBody(g *protogen.GeneratedFile, c *CompiledFHIRPath, sp SearchParam, refVar string, refMsg *protogen.Message, typedTarget, indent string) {
	// Path 1: URI-based reference
	g.P(fmt.Sprintf("\t%s// URI-based reference: \"Organization/org-123\"", indent))
	g.P(fmt.Sprintf("\t%sif uri := %s.GetUri(); uri != nil && uri.GetValue() != \"\" {", indent, refVar))
	g.P(fmt.Sprintf("\t%s\tparts := strings.SplitN(uri.GetValue(), \"/\", 2)", indent))
	g.P(fmt.Sprintf("\t%s\tif len(parts) == 2 {", indent))
	g.P(fmt.Sprintf("\t%s\t\tidx.References = append(idx.References, SpidxReference{", indent))
	g.P(fmt.Sprintf("\t%s\t\t\tTenantID:   tenantID,", indent))
	g.P(fmt.Sprintf("\t%s\t\t\tResType:    %q,", indent, c.ResType))
	g.P(fmt.Sprintf("\t%s\t\t\tResID:      resID,", indent))
	g.P(fmt.Sprintf("\t%s\t\t\tSpName:     %q,", indent, sp.Name))
	g.P(fmt.Sprintf("\t%s\t\t\tTargetType: parts[0],", indent))
	g.P(fmt.Sprintf("\t%s\t\t\tTargetID:   parts[1],", indent))
	g.P(fmt.Sprintf("\t%s\t\t})", indent))
	g.P(fmt.Sprintf("\t%s\t}", indent))
	g.P(fmt.Sprintf("\t%s}", indent))

	// Path 2: Typed reference (google/fhir oneof)
	// Only emit if refMsg has the typed target field (e.g. organization_id)
	if typedTarget != "" && (refMsg == nil || hasProtoField(refMsg, toSnakeCase(typedTarget)+"_id")) {
		g.P(fmt.Sprintf("\t%s// Typed reference: Get%sId()", indent, typedTarget))
		g.P(fmt.Sprintf("\t%sif typedRef := %s.Get%sId(); typedRef != nil && typedRef.GetValue() != \"\" {", indent, refVar, typedTarget))
		g.P(fmt.Sprintf("\t%s\tidx.References = append(idx.References, SpidxReference{", indent))
		g.P(fmt.Sprintf("\t%s\t\tTenantID:   tenantID,", indent))
		g.P(fmt.Sprintf("\t%s\t\tResType:    %q,", indent, c.ResType))
		g.P(fmt.Sprintf("\t%s\t\tResID:      resID,", indent))
		g.P(fmt.Sprintf("\t%s\t\tSpName:     %q,", indent, sp.Name))
		g.P(fmt.Sprintf("\t%s\t\tTargetType: %q,", indent, typedTarget))
		g.P(fmt.Sprintf("\t%s\t\tTargetID:   typedRef.GetValue(),", indent))
		g.P(fmt.Sprintf("\t%s\t})", indent))
		g.P(fmt.Sprintf("\t%s}", indent))
	}
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
// of a field. Returns: "identifier", "boolean", "enum", "code", or "" (unknown).
func detectFieldType(msg *protogen.Message, fieldName string) string {
	if msg == nil {
		return ""
	}
	f := findProtoField(msg, fieldName)
	if f == nil {
		return ""
	}

	// Check message type name for known FHIR types
	if f.Message != nil {
		msgName := string(f.Message.Desc.Name())
		switch {
		case msgName == "Identifier" || strings.HasSuffix(msgName, ".Identifier"):
			return "identifier"
		case msgName == "Boolean":
			return "boolean"
		case msgName == "Code":
			return "code"
		}

		for _, subField := range f.Message.Fields {
			if string(subField.Desc.Name()) == "value" {
				if subField.Desc.Kind().String() == "enum" {
					return "enum"
				}
				if subField.Desc.Kind().String() == "string" {
					return "code"
				}
			}
		}
	}
	return ""
}

func findProtoField(msg *protogen.Message, name string) *protogen.Field {
	if msg == nil {
		return nil
	}
	targetSnake := toSnakeCase(name)
	targetLower := strings.ToLower(strings.ReplaceAll(name, "_", ""))
	for _, f := range msg.Fields {
		fieldName := string(f.Desc.Name())
		if fieldName == name || toSnakeCase(fieldName) == targetSnake {
			return f
		}
		if strings.ToLower(strings.ReplaceAll(fieldName, "_", "")) == targetLower {
			return f
		}
		if strings.EqualFold(f.GoName, name) {
			return f
		}
	}
	return nil
}

func hasProtoField(msg *protogen.Message, name string) bool {
	return findProtoField(msg, name) != nil
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
