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
			emitDateExtraction(g, compiled, msg)
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
//   - Patient.address → constituent fields of address
//   - Patient.name → constituent fields of name
//   - Condition.abatement.as(string) → choice type string variant
func emitStringExtraction(g *protogen.GeneratedFile, c *CompiledFHIRPath, msg *protogen.Message) {
	sp := c.SearchParam
	g.P(fmt.Sprintf("\t// SearchParameter: %s (string)", sp.Name))
	g.P(fmt.Sprintf("\t// FHIRPath: %s", sp.Expression))

	switch len(c.Segments) {
	case 1:
		seg := c.Segments[0]
		field := findProtoField(msg, seg.Field)
		getter := segmentGetter(msg, seg)

		if c.IsChoiceType || (field != nil && isChoiceTypeMessage(field.Message)) {
			choiceGetter := "GetStringValue()"
			if c.ChoiceType != "" {
				choiceGetter = choiceTypeGetter(c.ChoiceType) + "()"
			}
			g.P(fmt.Sprintf("\tif outer := r.%s; outer != nil {", getter))
			g.P(fmt.Sprintf("\t\tif v := outer.%s; v != nil && v.GetValue() != \"\" {", choiceGetter))
			g.P("\t\t\tidx.Strings = append(idx.Strings, SpidxString{")
			g.P("\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\tSpValue:  strings.ToLower(v.GetValue()),")
			g.P("\t\t\t})")
			g.P("\t\t}")
			g.P("\t}")
		} else if field != nil && field.Message != nil && string(field.Message.Desc.Name()) == "Address" {
			g.P(fmt.Sprintf("\tfor _, addr := range r.%s {", getter))
			g.P("\t\tif addr == nil { continue }")
			g.P("\t\tfor _, line := range addr.GetLine() {")
			g.P("\t\t\tif line != nil && line.GetValue() != \"\" {")
			g.P("\t\t\t\tidx.Strings = append(idx.Strings, SpidxString{")
			g.P("\t\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\t\tSpValue:  strings.ToLower(line.GetValue()),")
			g.P("\t\t\t\t})")
			g.P("\t\t\t}")
			g.P("\t\t}")
			g.P("\t\tif city := addr.GetCity(); city != nil && city.GetValue() != \"\" {")
			g.P("\t\t\tidx.Strings = append(idx.Strings, SpidxString{")
			g.P("\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\tSpValue:  strings.ToLower(city.GetValue()),")
			g.P("\t\t\t})")
			g.P("\t\t}")
			g.P("\t\tif district := addr.GetDistrict(); district != nil && district.GetValue() != \"\" {")
			g.P("\t\t\tidx.Strings = append(idx.Strings, SpidxString{")
			g.P("\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\tSpValue:  strings.ToLower(district.GetValue()),")
			g.P("\t\t\t})")
			g.P("\t\t}")
			g.P("\t\tif state := addr.GetState(); state != nil && state.GetValue() != \"\" {")
			g.P("\t\t\tidx.Strings = append(idx.Strings, SpidxString{")
			g.P("\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\tSpValue:  strings.ToLower(state.GetValue()),")
			g.P("\t\t\t})")
			g.P("\t\t}")
			g.P("\t\tif postalCode := addr.GetPostalCode(); postalCode != nil && postalCode.GetValue() != \"\" {")
			g.P("\t\t\tidx.Strings = append(idx.Strings, SpidxString{")
			g.P("\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\tSpValue:  strings.ToLower(postalCode.GetValue()),")
			g.P("\t\t\t})")
			g.P("\t\t}")
			g.P("\t\tif country := addr.GetCountry(); country != nil && country.GetValue() != \"\" {")
			g.P("\t\t\tidx.Strings = append(idx.Strings, SpidxString{")
			g.P("\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\tSpValue:  strings.ToLower(country.GetValue()),")
			g.P("\t\t\t})")
			g.P("\t\t}")
			g.P("\t\tif text := addr.GetText(); text != nil && text.GetValue() != \"\" {")
			g.P("\t\t\tidx.Strings = append(idx.Strings, SpidxString{")
			g.P("\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\tSpValue:  strings.ToLower(text.GetValue()),")
			g.P("\t\t\t})")
			g.P("\t\t}")
			g.P("\t}")
		} else if field != nil && field.Message != nil && string(field.Message.Desc.Name()) == "HumanName" {
			g.P(fmt.Sprintf("\tfor _, name := range r.%s {", getter))
			g.P("\t\tif name == nil { continue }")
			g.P("\t\tif family := name.GetFamily(); family != nil && family.GetValue() != \"\" {")
			g.P("\t\t\tidx.Strings = append(idx.Strings, SpidxString{")
			g.P("\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\tSpValue:  strings.ToLower(family.GetValue()),")
			g.P("\t\t\t})")
			g.P("\t\t}")
			g.P("\t\tfor _, given := range name.GetGiven() {")
			g.P("\t\t\tif given != nil && given.GetValue() != \"\" {")
			g.P("\t\t\t\tidx.Strings = append(idx.Strings, SpidxString{")
			g.P("\t\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\t\tSpValue:  strings.ToLower(given.GetValue()),")
			g.P("\t\t\t})")
			g.P("\t\t}")
			g.P("\t\t}")
			g.P("\t\tif text := name.GetText(); text != nil && text.GetValue() != \"\" {")
			g.P("\t\t\tidx.Strings = append(idx.Strings, SpidxString{")
			g.P("\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\tSpValue:  strings.ToLower(text.GetValue()),")
			g.P("\t\t\t})")
			g.P("\t\t}")
			g.P("\t}")
		} else {
			isList := field != nil && field.Desc.IsList()
			if isList {
				g.P(fmt.Sprintf("\tfor _, v := range r.%s {", getter))
				g.P("\t\tif v != nil && v.GetValue() != \"\" {")
				g.P("\t\t\tidx.Strings = append(idx.Strings, SpidxString{")
				g.P("\t\t\t\tTenantID: tenantID,")
				g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
				g.P("\t\t\t\tResID:    resID,")
				g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
				g.P("\t\t\t\tSpValue:  strings.ToLower(v.GetValue()),")
				g.P("\t\t\t})")
				g.P("\t\t}")
				g.P("\t}")
			} else {
				g.P(fmt.Sprintf("\tif v := r.%s; v != nil && v.GetValue() != \"\" {", getter))
				g.P("\t\tidx.Strings = append(idx.Strings, SpidxString{")
				g.P("\t\t\tTenantID: tenantID,")
				g.P(fmt.Sprintf("\t\t\tResType:  %q,", c.ResType))
				g.P("\t\t\tResID:    resID,")
				g.P(fmt.Sprintf("\t\t\tSpName:   %q,", sp.Name))
				g.P("\t\t\tSpValue:  strings.ToLower(v.GetValue()),")
				g.P("\t\t})")
				g.P("\t}")
			}
		}

	case 2:
		outer := c.Segments[0]
		inner := c.Segments[1]
		outerField := findProtoField(msg, outer.Field)
		outerGetter := segmentGetter(msg, outer)
		outerIsList := false
		if outerField != nil {
			outerIsList = outerField.Desc.IsList()
		} else {
			outerIsList = isRepeatedFieldName(outer.Field)
		}

		var outerMsg *protogen.Message
		if outerField != nil {
			outerMsg = outerField.Message
		}
		innerField := findProtoField(outerMsg, inner.Field)
		innerGetter := segmentGetter(outerMsg, inner)

		if outerIsList {
			g.P(fmt.Sprintf("\tfor _, outer := range r.%s {", outerGetter))
			g.P("\t\tif outer == nil { continue }")
		} else {
			g.P(fmt.Sprintf("\tif outer := r.%s; outer != nil {", outerGetter))
		}
		indent := "\t\t"

		if c.IsChoiceType || (innerField != nil && isChoiceTypeMessage(innerField.Message)) {
			choiceGetter := "GetStringValue()"
			if c.ChoiceType != "" {
				choiceGetter = choiceTypeGetter(c.ChoiceType) + "()"
			}
			g.P(fmt.Sprintf("%sif v := outer.%s; v != nil {", indent, innerGetter))
			g.P(fmt.Sprintf("%s\tif s := v.%s; s != nil && s.GetValue() != \"\" {", indent, choiceGetter))
			g.P(fmt.Sprintf("%s\t\tidx.Strings = append(idx.Strings, SpidxString{", indent))
			g.P(fmt.Sprintf("%s\t\t\tTenantID: tenantID,", indent))
			g.P(fmt.Sprintf("%s\t\t\tResType:  %q,", indent, c.ResType))
			g.P(fmt.Sprintf("%s\t\t\tResID:    resID,", indent))
			g.P(fmt.Sprintf("%s\t\t\tSpName:   %q,", indent, sp.Name))
			g.P(fmt.Sprintf("%s\t\t\tSpValue:  strings.ToLower(s.GetValue()),", indent))
			g.P(fmt.Sprintf("%s\t\t})", indent))
			g.P(fmt.Sprintf("%s\t}", indent))
			g.P(fmt.Sprintf("%s}", indent))
		} else {
			isList := false
			isScalarString := false
			if innerField != nil {
				isList = innerField.Desc.IsList()
				isScalarString = innerField.Desc.Kind().String() == "string"
			} else {
				isList = isRepeatedFieldName(inner.Field)
			}

			if isList {
				g.P(fmt.Sprintf("%sfor _, v := range outer.%s {", indent, innerGetter))
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
				if isScalarString {
					g.P(fmt.Sprintf("%sif v := outer.%s; v != \"\" {", indent, innerGetter))
					g.P(fmt.Sprintf("%s\tidx.Strings = append(idx.Strings, SpidxString{", indent))
					g.P(fmt.Sprintf("%s\t\tTenantID: tenantID,", indent))
					g.P(fmt.Sprintf("%s\t\tResType:  %q,", indent, c.ResType))
					g.P(fmt.Sprintf("%s\t\tResID:    resID,", indent))
					g.P(fmt.Sprintf("%s\t\tSpName:   %q,", indent, sp.Name))
					g.P(fmt.Sprintf("%s\t\tSpValue:  strings.ToLower(v),", indent))
					g.P(fmt.Sprintf("%s\t})", indent))
					g.P(fmt.Sprintf("%s}", indent))
				} else {
					g.P(fmt.Sprintf("%sif v := outer.%s; v != nil && v.GetValue() != \"\" {", indent, innerGetter))
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
		getter := segmentGetter(msg, seg)
		g.P("\t// exists() check — index whether field is present")
		g.P(fmt.Sprintf("\tif r.%s != nil {", getter))
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
		field := findProtoField(msg, seg.Field)
		getter := segmentGetter(msg, seg)

		isEnum := false
		if field != nil && field.Message != nil {
			whereFieldObj := findProtoField(field.Message, seg.WhereField)
			if whereFieldObj != nil && whereFieldObj.Message != nil {
				valField := findProtoField(whereFieldObj.Message, "value")
				if valField != nil && valField.Desc.Kind().String() == "enum" {
					isEnum = true
				}
			}
		}

		g.P(fmt.Sprintf("\tfor _, item := range r.%s {", getter))
		g.P("\t\tif item == nil { continue }")
		if isEnum {
			g.P(fmt.Sprintf("\t\tif item.Get%s() == nil || !strings.EqualFold(item.Get%s().GetValue().String(), %q) { continue }",
				capitalize(seg.WhereField), capitalize(seg.WhereField), seg.WhereValue))
		} else {
			g.P(fmt.Sprintf("\t\tif item.Get%s() == nil || item.Get%s().GetValue() != %q { continue }",
				capitalize(seg.WhereField), capitalize(seg.WhereField), seg.WhereValue))
		}
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
		field := findProtoField(msg, seg.Field)
		getter := segmentGetter(msg, seg)

		if c.IsChoiceType || (field != nil && isChoiceTypeMessage(field.Message)) {
			// Choice type token (e.g. Observation.value as CodeableConcept, or value-concept)
			g.P(fmt.Sprintf("\tif v := r.%s; v != nil {", getter))
			g.P("\t\tif cc := v.GetCodeableConcept(); cc != nil {")
			g.P("\t\t\tfor _, coding := range cc.GetCoding() {")
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
			g.P()
			return
		}

		fieldType := detectFieldType(msg, seg.Field)
		isList := field != nil && field.Desc.IsList()

		switch fieldType {
		case "codeable_concept":
			if isList {
				g.P(fmt.Sprintf("\tfor _, cc := range r.%s {", getter))
				g.P("\t\tif cc == nil { continue }")
				g.P("\t\tfor _, coding := range cc.GetCoding() {")
				g.P("\t\t\tif coding == nil { continue }")
				g.P("\t\t\tsystem := \"\"")
				g.P("\t\t\tif coding.GetSystem() != nil { system = coding.GetSystem().GetValue() }")
				g.P("\t\t\tcode := \"\"")
				g.P("\t\t\tif coding.GetCode() != nil { code = coding.GetCode().GetValue() }")
				g.P("\t\t\tif code != \"\" {")
				g.P("\t\t\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
				g.P("\t\t\t\t\tTenantID: tenantID,")
				g.P(fmt.Sprintf("\t\t\t\t\tResType:  %q,", c.ResType))
				g.P("\t\t\t\t\tResID:    resID,")
				g.P(fmt.Sprintf("\t\t\t\t\tSpName:   %q,", sp.Name))
				g.P("\t\t\t\t\tSpSystem: system,")
				g.P("\t\t\t\t\tSpValue:  code,")
				g.P("\t\t\t\t})")
				g.P("\t\t\t}")
				g.P("\t\t}")
				g.P("\t}")
			} else {
				g.P(fmt.Sprintf("\tif cc := r.%s; cc != nil {", getter))
				g.P("\t\tfor _, coding := range cc.GetCoding() {")
				g.P("\t\t\tif coding == nil { continue }")
				g.P("\t\t\tsystem := \"\"")
				g.P("\t\t\tif coding.GetSystem() != nil { system = coding.GetSystem().GetValue() }")
				g.P("\t\t\tcode := \"\"")
				g.P("\t\t\tif coding.GetCode() != nil { code = coding.GetCode().GetValue() }")
				g.P("\t\t\tif code != \"\" {")
				g.P("\t\t\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
				g.P("\t\t\t\t\tTenantID: tenantID,")
				g.P(fmt.Sprintf("\t\t\t\t\tResType:  %q,", c.ResType))
				g.P("\t\t\t\t\tResID:    resID,")
				g.P(fmt.Sprintf("\t\t\t\t\tSpName:   %q,", sp.Name))
				g.P("\t\t\t\t\tSpSystem: system,")
				g.P("\t\t\t\t\tSpValue:  code,")
				g.P("\t\t\t\t})")
				g.P("\t\t\t}")
				g.P("\t\t}")
				g.P("\t}")
			}

		case "identifier":
			g.P(fmt.Sprintf("\tfor _, ident := range r.%s {", getter))
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
			g.P(fmt.Sprintf("\tif r.%s != nil {", getter))
			g.P("\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
			g.P("\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\tSpName:   %q,", sp.Name))
			g.P(fmt.Sprintf("\t\t\tSpValue:  fmt.Sprintf(\"%%v\", r.%s.GetValue()),", getter))
			g.P("\t\t})")
			g.P("\t}")

		case "code":
			isScalar := field != nil && field.Desc.Kind().String() == "string"
			if isList {
				g.P(fmt.Sprintf("\tfor _, code := range r.%s {", getter))
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
					g.P(fmt.Sprintf("\tif r.%s != \"\" {", getter))
					g.P("\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
					g.P("\t\t\tTenantID: tenantID,")
					g.P(fmt.Sprintf("\t\t\tResType:  %q,", c.ResType))
					g.P("\t\t\tResID:    resID,")
					g.P(fmt.Sprintf("\t\t\tSpName:   %q,", sp.Name))
					g.P(fmt.Sprintf("\t\t\tSpValue:  r.%s,", getter))
					g.P("\t\t})")
					g.P("\t}")
				} else {
					g.P(fmt.Sprintf("\tif r.%s != nil && r.%s.GetValue() != \"\" {", getter, getter))
					g.P("\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
					g.P("\t\t\tTenantID: tenantID,")
					g.P(fmt.Sprintf("\t\t\tResType:  %q,", c.ResType))
					g.P("\t\t\tResID:    resID,")
					g.P(fmt.Sprintf("\t\t\tSpName:   %q,", sp.Name))
					g.P(fmt.Sprintf("\t\t\tSpValue:  r.%s.GetValue(),", getter))
					g.P("\t\t})")
					g.P("\t}")
				}
			}

		case "enum":
			g.P(fmt.Sprintf("\tif r.%s != nil {", getter))
			g.P("\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
			g.P("\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\tSpName:   %q,", sp.Name))
			g.P(fmt.Sprintf("\t\t\tSpValue:  r.%s.GetValue().String(),", getter))
			g.P("\t\t})")
			g.P("\t}")

		case "coding":
			g.P(fmt.Sprintf("\tif c := r.%s; c != nil {", getter))
			g.P("\t\tsystem := \"\"")
			g.P("\t\tif c.GetSystem() != nil { system = c.GetSystem().GetValue() }")
			g.P("\t\tcode := \"\"")
			g.P("\t\tif c.GetCode() != nil { code = c.GetCode().GetValue() }")
			g.P("\t\tif code != \"\" {")
			g.P("\t\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
			g.P("\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\tSpSystem: system,")
			g.P("\t\t\t\tSpValue:  code,")
			g.P("\t\t\t})")
			g.P("\t\t}")
			g.P("\t}")

		default:
			g.P(fmt.Sprintf("\tif r.%s != nil {", getter))
			g.P("\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
			g.P("\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\tSpName:   %q,", sp.Name))
			g.P(fmt.Sprintf("\t\t\tSpValue:  fmt.Sprintf(\"%%v\", r.%s),", getter))
			g.P("\t\t})")
			g.P("\t}")
		}
	} else {
		// Nested token (2 segments): e.g. communication.language, encounter.participant.type
		outer := c.Segments[0]
		inner := c.Segments[1]
		outerField := findProtoField(msg, outer.Field)
		outerGetter := segmentGetter(msg, outer)
		outerIsList := false
		if outerField != nil {
			outerIsList = outerField.Desc.IsList()
		} else {
			outerIsList = isRepeatedFieldName(outer.Field)
		}

		var outerMsg *protogen.Message
		if outerField != nil {
			outerMsg = outerField.Message
		}
		innerField := findProtoField(outerMsg, inner.Field)
		innerGetter := segmentGetter(outerMsg, inner)
		innerIsList := false
		if innerField != nil {
			innerIsList = innerField.Desc.IsList()
		} else {
			innerIsList = isRepeatedFieldName(inner.Field)
		}

		if outerIsList {
			g.P(fmt.Sprintf("\tfor _, outer := range r.%s {", outerGetter))
			g.P("\t\tif outer == nil { continue }")
		} else {
			g.P(fmt.Sprintf("\tif outer := r.%s; outer != nil {", outerGetter))
		}

		if c.IsChoiceType || (innerField != nil && isChoiceTypeMessage(innerField.Message)) {
			g.P(fmt.Sprintf("\t\tif v := outer.%s; v != nil {", innerGetter))
			g.P("\t\t\tif cc := v.GetCodeableConcept(); cc != nil {")
			g.P("\t\t\t\tfor _, coding := range cc.GetCoding() {")
			g.P("\t\t\t\t\tif coding == nil { continue }")
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
			g.P("\t\t\t\t}")
			g.P("\t\t\t}")
			g.P("\t\t}")
		} else if innerField != nil && innerField.Message != nil && (string(innerField.Message.Desc.Name()) == "CodeableConcept" || hasProtoField(innerField.Message, "coding")) {
			// CodeableConcept
			if innerIsList {
				g.P(fmt.Sprintf("\t\tfor _, cc := range outer.%s {", innerGetter))
				g.P("\t\t\tif cc == nil { continue }")
				g.P("\t\t\tfor _, coding := range cc.GetCoding() {")
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
			} else {
				g.P(fmt.Sprintf("\t\tif v := outer.%s; v != nil {", innerGetter))
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
			}
		} else if innerField != nil && innerField.Message != nil && (string(innerField.Message.Desc.Name()) == "Coding" || (hasProtoField(innerField.Message, "system") && hasProtoField(innerField.Message, "code"))) {
			// Coding
			if innerIsList {
				g.P(fmt.Sprintf("\t\tfor _, coding := range outer.%s {", innerGetter))
				g.P("\t\t\tif coding == nil { continue }")
				g.P("\t\t\tsystem := \"\"")
				g.P("\t\t\tif coding.GetSystem() != nil { system = coding.GetSystem().GetValue() }")
				g.P("\t\t\tcode := \"\"")
				g.P("\t\t\tif coding.GetCode() != nil { code = coding.GetCode().GetValue() }")
				g.P("\t\t\tif code != \"\" {")
				g.P("\t\t\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
				g.P("\t\t\t\t\tTenantID: tenantID,")
				g.P(fmt.Sprintf("\t\t\t\t\tResType:  %q,", c.ResType))
				g.P("\t\t\t\t\tResID:    resID,")
				g.P(fmt.Sprintf("\t\t\t\t\tSpName:   %q,", sp.Name))
				g.P("\t\t\t\t\tSpSystem: system,")
				g.P("\t\t\t\t\tSpValue:  code,")
				g.P("\t\t\t\t})")
				g.P("\t\t\t}")
				g.P("\t\t}")
			} else {
				g.P(fmt.Sprintf("\t\tif coding := outer.%s; coding != nil {", innerGetter))
				g.P("\t\t\tsystem := \"\"")
				g.P("\t\t\tif coding.GetSystem() != nil { system = coding.GetSystem().GetValue() }")
				g.P("\t\t\tcode := \"\"")
				g.P("\t\t\tif coding.GetCode() != nil { code = coding.GetCode().GetValue() }")
				g.P("\t\t\tif code != \"\" {")
				g.P("\t\t\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
				g.P("\t\t\t\t\tTenantID: tenantID,")
				g.P(fmt.Sprintf("\t\t\t\t\tResType:  %q,", c.ResType))
				g.P("\t\t\t\t\tResID:    resID,")
				g.P(fmt.Sprintf("\t\t\t\t\tSpName:   %q,", sp.Name))
				g.P("\t\t\t\t\tSpSystem: system,")
				g.P("\t\t\t\t\tSpValue:  code,")
				g.P("\t\t\t\t})")
				g.P("\t\t\t}")
				g.P("\t\t}")
			}
		} else if innerField != nil && innerField.Message != nil && hasProtoField(innerField.Message, "value") {
			// Code wrapper / Enum wrapper (e.g. Address.use -> UseCode)
			valField := findProtoField(innerField.Message, "value")
			isEnum := valField != nil && valField.Desc.Kind().String() == "enum"
			if isEnum {
				g.P(fmt.Sprintf("\t\tif v := outer.%s; v != nil {", innerGetter))
				g.P("\t\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
				g.P("\t\t\t\tTenantID: tenantID,")
				g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
				g.P("\t\t\t\tResID:    resID,")
				g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
				g.P("\t\t\t\tSpValue:  v.GetValue().String(),")
				g.P("\t\t\t})")
				g.P("\t\t}")
			} else {
				g.P(fmt.Sprintf("\t\tif v := outer.%s; v != nil && v.GetValue() != \"\" {", innerGetter))
				g.P("\t\t\tidx.Tokens = append(idx.Tokens, SpidxToken{")
				g.P("\t\t\t\tTenantID: tenantID,")
				g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
				g.P("\t\t\t\tResID:    resID,")
				g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
				g.P("\t\t\t\tSpValue:  v.GetValue(),")
				g.P("\t\t\t})")
				g.P("\t\t}")
			}
		} else {
			// Generic fallback: treat as CodeableConcept
			if innerIsList {
				g.P(fmt.Sprintf("\t\tfor _, v := range outer.%s {", innerGetter))
				g.P("\t\t\tif v == nil { continue }")
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
			} else {
				g.P(fmt.Sprintf("\t\tif v := outer.%s; v != nil {", innerGetter))
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
			}
		}
		g.P("\t}")
	}
	g.P()
}

// emitDateExtraction generates code for date search parameters.
// google/fhir dates use {value_us: int64, precision: enum} — microsecond epoch.
// Also handles Period (start/end) and choice types (EffectiveX, OnsetX, AbatementX).
// Spike 4 finding: always use UTC for consistency.
func emitDateExtraction(g *protogen.GeneratedFile, c *CompiledFHIRPath, msg *protogen.Message) {
	sp := c.SearchParam

	g.P(fmt.Sprintf("\t// SearchParameter: %s (date)", sp.Name))
	g.P(fmt.Sprintf("\t// FHIRPath: %s", sp.Expression))

	if c.IsChoiceType {
		g.P(fmt.Sprintf("\t// Choice type: %s as %s", c.ChoiceField, c.ChoiceType))
	}

	if len(c.Segments) == 1 {
		seg := c.Segments[0]
		field := findProtoField(msg, seg.Field)
		getter := segmentGetter(msg, seg)

		isChoice := c.IsChoiceType || (field != nil && isChoiceTypeMessage(field.Message))
		isPeriod := field != nil && field.Message != nil && string(field.Message.Desc.Name()) == "Period"

		if isChoice {
			if field != nil && isChoiceTypeMessage(field.Message) {
				g.P(fmt.Sprintf("\tif d := r.%s; d != nil {", getter))
				// Extract DateTime variant if present
				if hasProtoField(field.Message, "date_time") {
					g.P("\t\tif dt := d.GetDateTime(); dt != nil {")
					g.P("\t\t\tt := time.UnixMicro(dt.GetValueUs()).UTC()")
					g.P("\t\t\tidx.Dates = append(idx.Dates, SpidxDate{")
					g.P("\t\t\t\tTenantID: tenantID,")
					g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
					g.P("\t\t\t\tResID:    resID,")
					g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
					g.P("\t\t\t\tSpLow:    t,")
					g.P("\t\t\t\tSpHigh:   t,")
					g.P("\t\t\t})")
					g.P("\t\t}")
				}
				// Extract Period variant if present
				if hasProtoField(field.Message, "period") {
					g.P("\t\tif p := d.GetPeriod(); p != nil {")
					g.P("\t\t\tvar low, high time.Time")
					g.P("\t\t\tif p.GetStart() != nil { low = time.UnixMicro(p.GetStart().GetValueUs()).UTC() }")
					g.P("\t\t\tif p.GetEnd() != nil { high = time.UnixMicro(p.GetEnd().GetValueUs()).UTC() }")
					g.P("\t\t\tif !low.IsZero() || !high.IsZero() {")
					g.P("\t\t\t\tif low.IsZero() { low = high }")
					g.P("\t\t\t\tif high.IsZero() { high = low }")
					g.P("\t\t\t\tidx.Dates = append(idx.Dates, SpidxDate{")
					g.P("\t\t\t\t\tTenantID: tenantID,")
					g.P(fmt.Sprintf("\t\t\t\t\tResType:  %q,", c.ResType))
					g.P("\t\t\t\t\tResID:    resID,")
					g.P(fmt.Sprintf("\t\t\t\t\tSpName:   %q,", sp.Name))
					g.P("\t\t\t\t\tSpLow:    low,")
					g.P("\t\t\t\t\tSpHigh:   high,")
					g.P("\t\t\t\t})")
					g.P("\t\t\t}")
					g.P("\t\t}")
				}
				// Extract Instant variant if present
				if hasProtoField(field.Message, "instant") {
					g.P("\t\tif inst := d.GetInstant(); inst != nil {")
					g.P("\t\t\tt := time.UnixMicro(inst.GetValueUs()).UTC()")
					g.P("\t\t\tidx.Dates = append(idx.Dates, SpidxDate{")
					g.P("\t\t\t\tTenantID: tenantID,")
					g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
					g.P("\t\t\t\tResID:    resID,")
					g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
					g.P("\t\t\t\tSpLow:    t,")
					g.P("\t\t\t\tSpHigh:   t,")
					g.P("\t\t\t})")
					g.P("\t\t}")
				}
				g.P("\t}")
			} else {
				g.P(fmt.Sprintf("\t// TODO: handle oneof %s — check for %s variant", c.ChoiceField, c.ChoiceType))
			}
		} else if isPeriod {
			g.P(fmt.Sprintf("\tif d := r.%s; d != nil {", getter))
			g.P("\t\tvar low, high time.Time")
			g.P("\t\tif d.GetStart() != nil { low = time.UnixMicro(d.GetStart().GetValueUs()).UTC() }")
			g.P("\t\tif d.GetEnd() != nil { high = time.UnixMicro(d.GetEnd().GetValueUs()).UTC() }")
			g.P("\t\tif !low.IsZero() || !high.IsZero() {")
			g.P("\t\t\tif low.IsZero() { low = high }")
			g.P("\t\t\tif high.IsZero() { high = low }")
			g.P("\t\t\tidx.Dates = append(idx.Dates, SpidxDate{")
			g.P("\t\t\t\tTenantID: tenantID,")
			g.P(fmt.Sprintf("\t\t\t\tResType:  %q,", c.ResType))
			g.P("\t\t\t\tResID:    resID,")
			g.P(fmt.Sprintf("\t\t\t\tSpName:   %q,", sp.Name))
			g.P("\t\t\t\tSpLow:    low,")
			g.P("\t\t\t\tSpHigh:   high,")
			g.P("\t\t\t})")
			g.P("\t\t}")
			g.P("\t}")
		} else {
			g.P(fmt.Sprintf("\tif d := r.%s; d != nil {", getter))
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

	// Determine target resource type from SearchParam target, expression, or field name
	lastSeg := c.Segments[len(c.Segments)-1]
	var targets []string
	if len(sp.Target) > 0 {
		targets = sp.Target
	} else if idx := strings.Index(sp.Expression, "resolve() is "); idx >= 0 {
		after := sp.Expression[idx+len("resolve() is "):]
		fields := strings.FieldsFunc(after, func(r rune) bool {
			return r == ')' || r == ' ' || r == '|'
		})
		if len(fields) > 0 && fields[0] != "" {
			targets = []string{fields[0]}
		}
	}
	if len(targets) == 0 {
		if t := inferRefTargetType(lastSeg.Field); t != "" {
			targets = []string{t}
		} else if t := inferRefTargetType(sp.Name); t != "" {
			targets = []string{t}
		}
	}

	if len(c.Segments) == 1 {
		seg := c.Segments[0]
		field := findProtoField(msg, seg.Field)
		getter := segmentGetter(msg, seg)
		isList := field != nil && field.Desc.IsList()
		var refMsg *protogen.Message
		if field != nil {
			refMsg = field.Message
		}

		if isList {
			g.P(fmt.Sprintf("\tfor _, ref := range r.%s {", getter))
			g.P("\t\tif ref == nil { continue }")
			emitRefExtractionBody(g, c, sp, "ref", refMsg, targets, "\t")
			g.P("\t}")
		} else {
			g.P(fmt.Sprintf("\tif ref := r.%s; ref != nil {", getter))
			emitRefExtractionBody(g, c, sp, "ref", refMsg, targets, "")
			g.P("\t}")
		}
	} else if len(c.Segments) == 2 {
		outerSeg := c.Segments[0]
		outerField := findProtoField(msg, outerSeg.Field)
		outerGetter := segmentGetter(msg, outerSeg)
		outerIsList := false
		if outerField != nil {
			outerIsList = outerField.Desc.IsList()
		} else {
			outerIsList = isRepeatedFieldName(outerSeg.Field)
		}

		var refMsg *protogen.Message
		isList := false
		var innerGetter string
		if outerField != nil && outerField.Message != nil {
			if innerField := findProtoField(outerField.Message, lastSeg.Field); innerField != nil {
				isList = innerField.Desc.IsList()
				refMsg = innerField.Message
				innerGetter = segmentGetter(outerField.Message, lastSeg)
			}
		}
		if innerGetter == "" {
			innerGetter = lastSeg.GoGetter
		}

		if outerIsList {
			g.P(fmt.Sprintf("\tfor _, outer := range r.%s {", outerGetter))
			g.P("\t\tif outer == nil { continue }")
		} else {
			g.P(fmt.Sprintf("\tif outer := r.%s; outer != nil {", outerGetter))
		}

		if isList {
			g.P(fmt.Sprintf("\t\tfor _, ref := range outer.%s {", innerGetter))
			g.P("\t\t\tif ref == nil { continue }")
			emitRefExtractionBody(g, c, sp, "ref", refMsg, targets, "\t\t")
			g.P("\t\t}")
		} else {
			g.P(fmt.Sprintf("\t\tif ref := outer.%s; ref != nil {", innerGetter))
			emitRefExtractionBody(g, c, sp, "ref", refMsg, targets, "\t")
			g.P("\t\t}")
		}
		g.P("\t}")
	}
	g.P()
}

func emitRefExtractionBody(g *protogen.GeneratedFile, c *CompiledFHIRPath, sp SearchParam, refVar string, refMsg *protogen.Message, targets []string, indent string) {
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
	for _, typedTarget := range targets {
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
// of a field. Returns: "identifier", "boolean", "enum", "code", "coding", or "" (unknown).
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
		case msgName == "Coding" || strings.HasSuffix(msgName, ".Coding"):
			return "coding"
		case msgName == "CodeableConcept" || strings.HasSuffix(msgName, ".CodeableConcept") || hasProtoField(f.Message, "coding"):
			return "codeable_concept"
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
		if string(f.Desc.JSONName()) == name {
			return f
		}
		if fieldName == name+"_value" || toSnakeCase(fieldName) == targetSnake+"_value" {
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

func segmentGetter(msg *protogen.Message, seg FHIRPathSegment) string {
	if msg != nil {
		if f := findProtoField(msg, seg.Field); f != nil {
			return "Get" + f.GoName + "()"
		}
	}
	return seg.GoGetter
}

func isChoiceTypeMessage(m *protogen.Message) bool {
	if m == nil {
		return false
	}
	if strings.HasSuffix(string(m.Desc.Name()), "X") {
		return true
	}
	if m.Desc.Oneofs().ByName("choice") != nil {
		return true
	}
	return false
}

func choiceTypeGetter(choiceType string) string {
	switch strings.ToLower(choiceType) {
	case "string":
		return "GetStringValue"
	case "datetime":
		return "GetDateTime"
	case "date":
		return "GetDate"
	case "period":
		return "GetPeriod"
	case "quantity":
		return "GetQuantity"
	case "codeableconcept":
		return "GetCodeableConcept"
	case "boolean":
		return "GetBoolean"
	case "integer":
		return "GetInteger"
	case "range":
		return "GetRange"
	case "ratio":
		return "GetRatio"
	case "sampleddata":
		return "GetSampledData"
	case "time":
		return "GetTime"
	case "instant":
		return "GetInstant"
	case "age":
		return "GetAge"
	default:
		return "Get" + capitalize(choiceType)
	}
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
