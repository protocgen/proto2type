package gen

import (
	"strings"
	"testing"
)

func TestValidateProtovalidateEmail(t *testing.T) {
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

func TestValidateProtovalidateValidEmail(t *testing.T) {
	u := &User{
		Email:       "alice@example.com",
		DisplayName: "Alice",
		Age:         25,
	}
	err := u.Validate()
	if err != nil {
		t.Errorf("unexpected error for valid user: %v", err)
	}
}

func TestValidateProtovalidateDisplayNameTooShort(t *testing.T) {
	u := &User{
		Email:       "alice@example.com",
		DisplayName: "", // min_len: 1
		Age:         25,
	}
	err := u.Validate()
	if err == nil {
		t.Fatal("expected validation error for empty display_name, got nil")
	}
}

func TestValidateProtovalidateAgeTooHigh(t *testing.T) {
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

func TestValidateProtovalidateAgeNegative(t *testing.T) {
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

func TestValidateProtovalidateAndOneofCombined(t *testing.T) {
	// Both oneof violation AND proto constraint violation.
	email := "a@b.com"
	phone := "+123"
	u := &User{
		Email:        "not-an-email",
		DisplayName:  "Alice",
		Age:          25,
		ContactEmail: &email,
		ContactPhone: &phone, // two oneof variants set
	}
	err := u.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Oneof check runs first, so should get oneof error.
	if !strings.Contains(err.Error(), "oneof") {
		t.Errorf("expected oneof error first, got: %v", err)
	}
}

func TestValidateProtovalidateNoAnnotations(t *testing.T) {
	// Address has no buf.validate annotations — protovalidate passes.
	a := &Address{Street: "123 Main St"}
	err := a.Validate()
	if err != nil {
		t.Errorf("unexpected error for Address with no constraints: %v", err)
	}
}
