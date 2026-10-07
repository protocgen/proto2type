package generator

import (
	"testing"
)

func TestCompileFHIRPath_SimpleField(t *testing.T) {
	sp := SearchParam{Name: "birthdate", Type: "date", Expression: "Patient.birthDate"}
	compiled, err := CompileFHIRPath(sp, "Patient")
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(compiled.Segments))
	}
	if compiled.Segments[0].Field != "birth_date" {
		t.Errorf("expected field 'birth_date', got %q", compiled.Segments[0].Field)
	}
	if compiled.Segments[0].GoGetter != "GetBirthDate()" {
		t.Errorf("expected GoGetter 'GetBirthDate()', got %q", compiled.Segments[0].GoGetter)
	}
}

func TestCompileFHIRPath_NestedField(t *testing.T) {
	sp := SearchParam{Name: "family", Type: "string", Expression: "Patient.name.family | Practitioner.name.family"}
	compiled, err := CompileFHIRPath(sp, "Patient")
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d: %+v", len(compiled.Segments), compiled.Segments)
	}
	if compiled.Segments[0].Field != "name" {
		t.Errorf("expected first segment 'name', got %q", compiled.Segments[0].Field)
	}
	if compiled.Segments[0].GoGetter != "GetName()" {
		t.Errorf("expected first segment GoGetter 'GetName()', got %q", compiled.Segments[0].GoGetter)
	}
	if compiled.Segments[1].Field != "family" {
		t.Errorf("expected second segment 'family', got %q", compiled.Segments[1].Field)
	}
	if compiled.Segments[1].GoGetter != "GetFamily()" {
		t.Errorf("expected second segment GoGetter 'GetFamily()', got %q", compiled.Segments[1].GoGetter)
	}
}

func TestCompileFHIRPath_WhereFilter(t *testing.T) {
	sp := SearchParam{
		Name:       "email",
		Type:       "token",
		Expression: "Patient.telecom.where(system='email') | Person.telecom.where(system='email')",
	}
	compiled, err := CompileFHIRPath(sp, "Patient")
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Segments) != 1 {
		t.Fatalf("expected 1 segment (telecom with where), got %d: %+v", len(compiled.Segments), compiled.Segments)
	}
	seg := compiled.Segments[0]
	if seg.Field != "telecom" {
		t.Errorf("expected field 'telecom', got %q", seg.Field)
	}
	if seg.WhereField != "system" {
		t.Errorf("expected where field 'system', got %q", seg.WhereField)
	}
	if seg.WhereValue != "email" {
		t.Errorf("expected where value 'email', got %q", seg.WhereValue)
	}
}

func TestCompileFHIRPath_ChoiceType(t *testing.T) {
	sp := SearchParam{
		Name:       "death-date",
		Type:       "date",
		Expression: "(Patient.deceased as dateTime)",
	}
	compiled, err := CompileFHIRPath(sp, "Patient")
	if err != nil {
		t.Fatal(err)
	}
	if !compiled.IsChoiceType {
		t.Error("expected IsChoiceType=true")
	}
	if compiled.ChoiceField != "deceased" {
		t.Errorf("expected choice field 'deceased', got %q", compiled.ChoiceField)
	}
	if compiled.ChoiceType != "dateTime" {
		t.Errorf("expected choice type 'dateTime', got %q", compiled.ChoiceType)
	}

	// Test .as(Type) syntax
	sp2 := SearchParam{
		Name:       "abatement-string",
		Type:       "string",
		Expression: "Condition.abatement.as(string)",
	}
	compiled2, err := CompileFHIRPath(sp2, "Condition")
	if err != nil {
		t.Fatal(err)
	}
	if !compiled2.IsChoiceType {
		t.Error("expected IsChoiceType=true")
	}
	if compiled2.ChoiceField != "abatement" {
		t.Errorf("expected choice field 'abatement', got %q", compiled2.ChoiceField)
	}
	if compiled2.ChoiceType != "string" {
		t.Errorf("expected choice type 'string', got %q", compiled2.ChoiceType)
	}
	if len(compiled2.Segments) != 1 || compiled2.Segments[0].Field != "abatement" {
		t.Fatalf("expected segments [abatement], got %+v", compiled2.Segments)
	}

	// Test nested .as(Type) syntax
	sp3 := SearchParam{
		Name:       "component-value-concept",
		Type:       "token",
		Expression: "Observation.component.value.as(CodeableConcept)",
	}
	compiled3, err := CompileFHIRPath(sp3, "Observation")
	if err != nil {
		t.Fatal(err)
	}
	if !compiled3.IsChoiceType {
		t.Error("expected IsChoiceType=true")
	}
	if compiled3.ChoiceField != "value" {
		t.Errorf("expected choice field 'value', got %q", compiled3.ChoiceField)
	}
	if compiled3.ChoiceType != "CodeableConcept" {
		t.Errorf("expected choice type 'CodeableConcept', got %q", compiled3.ChoiceType)
	}
	if len(compiled3.Segments) != 2 || compiled3.Segments[0].Field != "component" || compiled3.Segments[1].Field != "value" {
		t.Fatalf("expected segments [component, value], got %+v", compiled3.Segments)
	}
}

func TestCompileFHIRPath_Exists(t *testing.T) {
	sp := SearchParam{
		Name:       "deceased",
		Type:       "token",
		Expression: "Patient.deceased.exists() and Patient.deceased != false",
	}
	compiled, err := CompileFHIRPath(sp, "Patient")
	if err != nil {
		t.Fatal(err)
	}
	if !compiled.IsExists {
		t.Error("expected IsExists=true")
	}
	if len(compiled.Segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(compiled.Segments))
	}
	if compiled.Segments[0].Field != "deceased" {
		t.Errorf("expected field 'deceased', got %q", compiled.Segments[0].Field)
	}
}

func TestCompileFHIRPath_MultiResource_SelectsCorrectBase(t *testing.T) {
	sp := SearchParam{
		Name:       "address-city",
		Type:       "string",
		Expression: "Patient.address.city | Person.address.city | Practitioner.address.city",
	}
	// Should pick Patient path
	compiled, err := CompileFHIRPath(sp, "Patient")
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(compiled.Segments))
	}
	if compiled.Segments[0].Field != "address" {
		t.Errorf("expected 'address', got %q", compiled.Segments[0].Field)
	}
	if compiled.Segments[1].Field != "city" {
		t.Errorf("expected 'city', got %q", compiled.Segments[1].Field)
	}

	// Should pick Practitioner path
	compiled2, err := CompileFHIRPath(sp, "Practitioner")
	if err != nil {
		t.Fatal(err)
	}
	if compiled2.ResType != "Practitioner" {
		t.Errorf("expected ResType 'Practitioner', got %q", compiled2.ResType)
	}
}

func TestFHIRToSnakeCase(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"birthDate", "birth_date"},
		{"postalCode", "postal_code"},
		{"managingOrganization", "managing_organization"},
		{"name", "name"},
		{"id", "id"},
		{"generalPractitioner", "general_practitioner"},
	}
	for _, tt := range tests {
		got := toSnakeCase(tt.input)
		if got != tt.want {
			t.Errorf("toSnakeCase(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestProtoFieldToGoGetter(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"birth_date", "GetBirthDate()"},
		{"name", "GetName()"},
		{"managing_organization", "GetManagingOrganization()"},
	}
	for _, tt := range tests {
		got := protoFieldToGoGetter(tt.input)
		if got != tt.want {
			t.Errorf("protoFieldToGoGetter(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
