package gen

import (
	"testing"
)

func TestActiveFieldNilReceiver(t *testing.T) {
	var u *User
	if got := u.ActiveContactMethod(); got != "" {
		t.Errorf("nil receiver: got %q, want \"\"", got)
	}
}

func TestActiveFieldNoneSet(t *testing.T) {
	u := &User{DisplayName: "Alice"}
	if got := u.ActiveContactMethod(); got != "" {
		t.Errorf("no variant set: got %q, want \"\"", got)
	}
}

func TestActiveFieldEmailSet(t *testing.T) {
	email := "alice@example.com"
	u := &User{ContactEmail: &email}
	if got := u.ActiveContactMethod(); got != "contact_email" {
		t.Errorf("email set: got %q, want %q", got, "contact_email")
	}
}

func TestActiveFieldPhoneSet(t *testing.T) {
	phone := "+1234567890"
	u := &User{ContactPhone: &phone}
	if got := u.ActiveContactMethod(); got != "contact_phone" {
		t.Errorf("phone set: got %q, want %q", got, "contact_phone")
	}
}

func TestActiveFieldMultiOneof(t *testing.T) {
	// Notification has two oneofs: channel and content.
	email := "a@b.com"
	text := "hello"
	n := &Notification{
		Email:     &email,
		PlainText: &text,
	}
	if got := n.ActiveChannel(); got != "email" {
		t.Errorf("channel: got %q, want %q", got, "email")
	}
	if got := n.ActiveContent(); got != "plain_text" {
		t.Errorf("content: got %q, want %q", got, "plain_text")
	}
}

func TestValidateNilReceiver(t *testing.T) {
	var u *User
	if err := u.Validate(); err != nil {
		t.Errorf("nil receiver: unexpected error: %v", err)
	}
}

func TestValidateZeroVariants(t *testing.T) {
	u := &User{DisplayName: "Alice"}
	if err := u.Validate(); err != nil {
		t.Errorf("zero variants: unexpected error: %v", err)
	}
}

func TestValidateOneVariant(t *testing.T) {
	email := "alice@example.com"
	u := &User{ContactEmail: &email}
	if err := u.Validate(); err != nil {
		t.Errorf("one variant: unexpected error: %v", err)
	}
}

func TestValidateMutualExclusionViolation(t *testing.T) {
	email := "alice@example.com"
	phone := "+1234567890"
	u := &User{
		ContactEmail: &email,
		ContactPhone: &phone,
	}
	err := u.Validate()
	if err == nil {
		t.Fatal("expected error when two oneof variants set, got nil")
	}
	want := "oneof contact_method: 2 variants set, expected at most 1"
	if err.Error() != want {
		t.Errorf("error message: got %q, want %q", err.Error(), want)
	}
}

func TestValidateMultiOneofPartialViolation(t *testing.T) {
	// Notification: set 2 channel variants but only 1 content variant.
	email := "a@b.com"
	sms := "+123"
	text := "hello"
	n := &Notification{
		Email:     &email,
		Sms:       &sms,
		PlainText: &text,
	}
	err := n.Validate()
	if err == nil {
		t.Fatal("expected error for channel oneof violation, got nil")
	}
}
