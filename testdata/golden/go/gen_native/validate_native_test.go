package gen_native

import (
	"strings"
	"testing"
)

func TestNativeValidateEmail(t *testing.T) {
	u := &User{
		Email:       "not-an-email",
		DisplayName: "Alice",
		Age:         25,
	}
	err := u.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid email, got nil")
	}
	if !strings.Contains(err.Error(), "email") {
		t.Errorf("error should mention email, got: %v", err)
	}
}

func TestNativeValidateValidEmail(t *testing.T) {
	u := &User{
		Email:       "alice@example.com",
		DisplayName: "Alice",
		Age:         25,
		Roles:       []string{"admin"},
	}
	err := u.Validate()
	if err != nil {
		t.Errorf("unexpected error for valid user: %v", err)
	}
}

func TestNativeValidateDisplayNameTooShort(t *testing.T) {
	u := &User{
		Email:       "alice@example.com",
		DisplayName: "", // min_len: 1
		Age:         25,
	}
	err := u.Validate()
	if err == nil {
		t.Fatal("expected validation error for empty display_name, got nil")
	}
	if !strings.Contains(err.Error(), "display_name") {
		t.Errorf("error should mention display_name, got: %v", err)
	}
}

func TestNativeValidateDisplayNameTooLong(t *testing.T) {
	u := &User{
		Email:       "alice@example.com",
		DisplayName: strings.Repeat("a", 256), // max_len: 255
		Age:         25,
	}
	err := u.Validate()
	if err == nil {
		t.Fatal("expected validation error for display_name > 255 chars, got nil")
	}
}

func TestNativeValidateAgeTooHigh(t *testing.T) {
	u := &User{
		Email:       "alice@example.com",
		DisplayName: "Alice",
		Age:         200, // max: 150
	}
	err := u.Validate()
	if err == nil {
		t.Fatal("expected validation error for age > 150, got nil")
	}
	if !strings.Contains(err.Error(), "age") {
		t.Errorf("error should mention age, got: %v", err)
	}
}

func TestNativeValidateAgeNegative(t *testing.T) {
	u := &User{
		Email:       "alice@example.com",
		DisplayName: "Alice",
		Age:         -5, // min: 0
	}
	err := u.Validate()
	if err == nil {
		t.Fatal("expected validation error for negative age, got nil")
	}
}

func TestNativeValidateRolesMinItems(t *testing.T) {
	u := &User{
		Email:       "alice@example.com",
		DisplayName: "Alice",
		Age:         25,
		Roles:       []string{}, // min_items: 1
	}
	err := u.Validate()
	if err == nil {
		t.Fatal("expected validation error for empty roles, got nil")
	}
	if !strings.Contains(err.Error(), "roles") {
		t.Errorf("error should mention roles, got: %v", err)
	}
}

func TestNativeValidateRolesMaxItems(t *testing.T) {
	roles := make([]string, 11) // max_items: 10
	for i := range roles {
		roles[i] = "role"
	}
	u := &User{
		Email:       "alice@example.com",
		DisplayName: "Alice",
		Age:         25,
		Roles:       roles,
	}
	err := u.Validate()
	if err == nil {
		t.Fatal("expected validation error for > 10 roles, got nil")
	}
}

func TestNativeValidatePhonePattern(t *testing.T) {
	phone := "not-a-phone!"
	u := &User{
		Email:       "alice@example.com",
		DisplayName: "Alice",
		Age:         25,
		Phone:       &phone,
		Roles:       []string{"admin"},
	}
	err := u.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid phone, got nil")
	}
	if !strings.Contains(err.Error(), "phone") {
		t.Errorf("error should mention phone, got: %v", err)
	}
}

func TestNativeValidatePhoneValid(t *testing.T) {
	phone := "+1-555-1234"
	u := &User{
		Email:       "alice@example.com",
		DisplayName: "Alice",
		Age:         25,
		Phone:       &phone,
		Roles:       []string{"admin"},
	}
	err := u.Validate()
	if err != nil {
		t.Errorf("unexpected error for valid phone: %v", err)
	}
}

func TestNativeValidateNestedAddress(t *testing.T) {
	u := &User{
		Email:       "alice@example.com",
		DisplayName: "Alice",
		Age:         25,
		Roles:       []string{"admin"},
		Address: &Address{
			Street:  "", // min_len: 1
			City:    "SF",
			State:   "CA",
			Zip:     "94102",
			Country: "US",
		},
	}
	err := u.Validate()
	if err == nil {
		t.Fatal("expected validation error for empty street, got nil")
	}
	if !strings.Contains(err.Error(), "address") {
		t.Errorf("error should mention address path, got: %v", err)
	}
}

func TestNativeValidateAddressZipPattern(t *testing.T) {
	a := &Address{
		Street:  "123 Main St",
		City:    "SF",
		State:   "CA",
		Zip:     "BADZIP",
		Country: "US",
	}
	err := a.Validate()
	if err == nil {
		t.Fatal("expected validation error for bad zip, got nil")
	}
	if !strings.Contains(err.Error(), "zip") {
		t.Errorf("error should mention zip, got: %v", err)
	}
}

func TestNativeValidateAddressValid(t *testing.T) {
	a := &Address{
		Street:  "123 Main St",
		City:    "SF",
		State:   "CA",
		Zip:     "94102",
		Country: "US",
	}
	err := a.Validate()
	if err != nil {
		t.Errorf("unexpected error for valid address: %v", err)
	}
}

func TestNativeValidateOneofMutualExclusion(t *testing.T) {
	email := "a@b.com"
	phone := "+123"
	u := &User{
		Email:        "alice@example.com",
		DisplayName:  "Alice",
		Age:          25,
		Roles:        []string{"admin"},
		ContactEmail: &email,
		ContactPhone: &phone, // two oneof variants set
	}
	err := u.Validate()
	if err == nil {
		t.Fatal("expected error for multiple oneof variants, got nil")
	}
	if !strings.Contains(err.Error(), "oneof") {
		t.Errorf("expected oneof error, got: %v", err)
	}
}

func TestNativeValidateNilUser(t *testing.T) {
	var u *User
	err := u.Validate()
	if err != nil {
		t.Errorf("expected nil for nil receiver, got: %v", err)
	}
}

func TestNativeValidateTagNoConstraints(t *testing.T) {
	// Tag has no direct constraints — validate should pass.
	tag := &Tag{Key: "env", Value: "prod"}
	err := tag.Validate()
	if err != nil {
		t.Errorf("unexpected error for Tag with no constraints: %v", err)
	}
}

func TestNativeValidateRecursiveCategory(t *testing.T) {
	cat := &Category{
		Name: "root",
		Children: []*Category{
			{Name: "child1"},
			{Name: "child2", Children: []*Category{{Name: "grandchild"}}},
		},
	}
	err := cat.Validate()
	if err != nil {
		t.Errorf("unexpected error for valid category tree: %v", err)
	}
}
