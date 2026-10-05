package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SearchParamBundle is the top-level FHIR SearchParameter Bundle JSON structure.
type SearchParamBundle struct {
	Entry []SearchParamEntry `json:"entry"`
}

// SearchParamEntry is a single entry in the SearchParameter Bundle.
type SearchParamEntry struct {
	Resource SearchParam `json:"resource"`
}

// SearchParam represents a FHIR SearchParameter definition.
type SearchParam struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"` // token, string, date, reference, quantity, uri, number
	Expression string   `json:"expression"`
	Base       []string `json:"base"`
}

// SearchParamIndex maps resource type → list of SearchParams.
type SearchParamIndex map[string][]SearchParam

// LoadSearchParams reads a FHIR SearchParameter Bundle JSON file
// and returns an index keyed by resource type.
func LoadSearchParams(path string) (SearchParamIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("LoadSearchParams: %w", err)
	}

	var bundle SearchParamBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, fmt.Errorf("LoadSearchParams: %w", err)
	}

	index := make(SearchParamIndex)
	for _, entry := range bundle.Entry {
		sp := entry.Resource
		if sp.Expression == "" {
			continue
		}
		for _, base := range sp.Base {
			index[base] = append(index[base], sp)
		}
	}

	return index, nil
}

// FHIRPathSegment represents one step in a compiled FHIRPath expression.
// e.g. "Patient.name.family" → [Patient, name, family]
// e.g. "Patient.telecom.where(system='phone')" → [Patient, telecom{where: system=phone}]
type FHIRPathSegment struct {
	Field     string // proto field name (snake_case)
	GoGetter  string // Go getter method name (PascalCase)
	Repeated  bool   // true if this field is repeated
	IsMessage bool   // true if this field is a message type

	// Filter (from .where(field='value'))
	WhereField string // e.g. "system"
	WhereValue string // e.g. "phone"
}

// CompiledFHIRPath is a FHIRPath expression compiled to proto field accessors.
type CompiledFHIRPath struct {
	SearchParam SearchParam
	ResType     string            // e.g. "Patient"
	Segments    []FHIRPathSegment // after the resource type
	// Special patterns
	IsChoiceType bool   // (Patient.deceased as dateTime)
	ChoiceField  string // e.g. "deceased"
	ChoiceType   string // e.g. "dateTime"
	IsExists     bool   // Patient.deceased.exists()
}

// CompileFHIRPath parses a FHIRPath expression into proto accessor segments.
// It handles the patterns identified in Spike 2:
//   - Simple field: "Patient.birthDate"
//   - Nested: "Patient.name.family"
//   - Where filter: "Patient.telecom.where(system='phone')"
//   - Choice type: "(Patient.deceased as dateTime)"
//   - Exists: "Patient.deceased.exists()"
//   - Multi-resource: "Patient.name.family | Practitioner.name.family"
//     (only the segment matching resType is returned)
func CompileFHIRPath(sp SearchParam, resType string) (*CompiledFHIRPath, error) {
	expr := sp.Expression

	// Multi-resource expressions: split on " | " and find the one for resType
	parts := strings.Split(expr, " | ")
	var relevantExpr string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		// Handle "(Patient.x as Type)" pattern
		trimmed := strings.TrimPrefix(strings.TrimSuffix(p, ")"), "(")
		if strings.HasPrefix(trimmed, resType+".") || strings.HasPrefix(p, resType+".") {
			relevantExpr = p
			break
		}
	}
	if relevantExpr == "" {
		return nil, fmt.Errorf("no expression for resource %s in %q", resType, expr)
	}

	compiled := &CompiledFHIRPath{
		SearchParam: sp,
		ResType:     resType,
	}

	// Handle choice type: (Patient.deceased as dateTime)
	if strings.HasPrefix(relevantExpr, "(") && strings.Contains(relevantExpr, " as ") {
		inner := strings.TrimPrefix(strings.TrimSuffix(relevantExpr, ")"), "(")
		asParts := strings.SplitN(inner, " as ", 2)
		compiled.IsChoiceType = true
		compiled.ChoiceType = strings.TrimSpace(asParts[1])
		relevantExpr = strings.TrimSpace(asParts[0])

		segments := strings.Split(relevantExpr, ".")
		if len(segments) >= 2 {
			compiled.ChoiceField = segments[1]
		}
		// Build segments from the path (skip resource type)
		for _, seg := range segments[1:] {
			compiled.Segments = append(compiled.Segments, FHIRPathSegment{
				Field:    toSnakeCase(seg),
				GoGetter: "Get" + seg + "()",
			})
		}
		return compiled, nil
	}

	// Handle exists(): "Patient.deceased.exists() and Patient.deceased != false"
	if strings.Contains(relevantExpr, ".exists()") {
		compiled.IsExists = true
		// Extract the path before .exists()
		existsPath := strings.Split(relevantExpr, ".exists()")[0]
		segments := strings.Split(existsPath, ".")
		for _, seg := range segments[1:] {
			compiled.Segments = append(compiled.Segments, FHIRPathSegment{
				Field:    toSnakeCase(seg),
				GoGetter: "Get" + seg + "()",
			})
		}
		return compiled, nil
	}

	// Standard path: "Patient.name.family" or "Patient.telecom.where(system='phone')"
	path := strings.TrimPrefix(relevantExpr, resType+".")
	segments := splitFHIRPath(path)

	compiled.Segments = append(compiled.Segments, segments...)

	return compiled, nil
}

// splitFHIRPath splits a FHIRPath after the resource type into segments,
// handling .where() filters.
// e.g. "telecom.where(system='phone')" → [{telecom, where:system=phone}]
// e.g. "name.family" → [{name}, {family}]
func splitFHIRPath(path string) []FHIRPathSegment {
	var segments []FHIRPathSegment

	// Split on "." but not inside parentheses
	parts := splitDotRespectingParens(path)

	for i := 0; i < len(parts); i++ {
		part := parts[i]
		seg := FHIRPathSegment{}

		// Check if the NEXT part is "where(...)" — if so, attach it to this field
		if i+1 < len(parts) && strings.HasPrefix(parts[i+1], "where(") {
			seg.Field = toSnakeCase(part)
			seg.GoGetter = "Get" + part + "()"
			whereInner := parts[i+1][6 : len(parts[i+1])-1] // strip "where(" and ")"
			eqParts := strings.SplitN(whereInner, "='", 2)
			if len(eqParts) == 2 {
				seg.WhereField = eqParts[0]
				seg.WhereValue = strings.TrimSuffix(eqParts[1], "'")
			}
			segments = append(segments, seg)
			i++ // skip the where() part
			continue
		}

		// Handle "field.where(condition)" already combined (shouldn't happen with splitDot)
		if idx := strings.Index(part, ".where("); idx >= 0 {
			seg.Field = toSnakeCase(part[:idx])
			seg.GoGetter = "Get" + part[:idx] + "()"
			whereInner := part[idx+7 : len(part)-1]
			eqParts := strings.SplitN(whereInner, "='", 2)
			if len(eqParts) == 2 {
				seg.WhereField = eqParts[0]
				seg.WhereValue = strings.TrimSuffix(eqParts[1], "'")
			}
			segments = append(segments, seg)
			continue
		}

		// Skip standalone "where(...)" (already consumed above)
		if strings.HasPrefix(part, "where(") {
			continue
		}

		seg.Field = toSnakeCase(part)
		seg.GoGetter = "Get" + part + "()"
		segments = append(segments, seg)
	}

	return segments
}

// splitDotRespectingParens splits on "." but preserves dots inside parentheses.
func splitDotRespectingParens(s string) []string {
	var parts []string
	depth := 0
	start := 0

	for i, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case '.':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}

	return parts
}

// toSnakeCase is defined in naming.go — reused here for FHIR field names.

// protoFieldToGoGetter converts a proto field name (snake_case) to Go getter.
// e.g. "birth_date" → "GetBirthDate()"
func protoFieldToGoGetter(snakeField string) string {
	parts := strings.Split(snakeField, "_")
	var result strings.Builder
	result.WriteString("Get")
	for _, p := range parts {
		if len(p) > 0 {
			result.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	result.WriteString("()")
	return result.String()
}
