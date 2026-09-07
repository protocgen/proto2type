package integration

import (
	"testing"
	"time"

	gen "github.com/protocgen/proto2type/testdata/golden/go/gen"
	pb "github.com/protocgen/proto2type/testdata/golden/go/pb"
)

func TestNilReceiverSafety(t *testing.T) {
	if pbUser := (*gen.User)(nil).ToProto(); pbUser != nil {
		t.Errorf("expected nil from (*gen.User)(nil).ToProto(), got %v", pbUser)
	}

	if d := (*gen.UserFirestore)(nil).ToDomain(); d != nil {
		t.Errorf("expected nil from (*gen.UserFirestore)(nil).ToDomain(), got %v", d)
	}

	if d := (*gen.UserMongo)(nil).ToDomain(); d != nil {
		t.Errorf("expected nil from (*gen.UserMongo)(nil).ToDomain(), got %v", d)
	}

	if e := (*gen.EmptyMessage)(nil).ToProto(); e != nil {
		t.Errorf("expected nil from (*gen.EmptyMessage)(nil).ToProto(), got %v", e)
	}

	var u gen.User
	u.FromProto(nil) // should not panic, u unchanged

	var fs gen.UserFirestore
	fs.FromDomain(nil) // should not panic
}

func makeFullUser() *gen.User {
	strPtr := func(s string) *string { return &s }
	int32Ptr := func(i int32) *int32 { return &i }
	timePtr := func(t time.Time) *time.Time { return &t }

	return &gen.User{
		ID:              "id-123",
		Email:           "test@example.com",
		DisplayName:     "Test User",
		Active:          true,
		Age:             25,
		Roles:           []string{"admin"},
		Metadata:        map[string]string{"k": "v"},
		Address:         &gen.Address{Street: "123 St"},
		CreatedAt:       time.Now(),
		Phone:           strPtr("555-1234"),
		Avatar:          []byte{1, 2, 3},
		Nickname:        strPtr("testy"),
		Status:          1,
		Tags:            []*gen.Tag{{Key: "tag1", Value: "val1"}},
		DeletedAt:       timePtr(time.Now()),
		PreviousStatus:  int32Ptr(2),
		UpdateMask:      []string{"email"},
		AvatarThumbnail: &[]byte{4, 5, 6},
		BigNumber:       9999,
		Handle:          "test_handle",
	}
}

func TestReceiverReuseFieldClearing(t *testing.T) {
	u := makeFullUser()
	u.FromProto(&pb.User{})

	if u.Roles != nil {
		t.Errorf("expected Roles to be nil, got %v", u.Roles)
	}
	if u.Metadata != nil {
		t.Errorf("expected Metadata to be nil, got %v", u.Metadata)
	}
	if u.Address != nil {
		t.Errorf("expected Address to be nil, got %v", u.Address)
	}
	if u.Phone != nil {
		t.Errorf("expected Phone to be nil, got %v", u.Phone)
	}
	if u.Avatar != nil {
		t.Errorf("expected Avatar to be nil, got %v", u.Avatar)
	}
	if u.DeletedAt != nil {
		t.Errorf("expected DeletedAt to be nil, got %v", u.DeletedAt)
	}
	if u.Tags != nil {
		t.Errorf("expected Tags to be nil, got %v", u.Tags)
	}

	strPtr := func(s string) *string { return &s }
	u.ContactEmail = strPtr("oneof@example.com")
	u.FromProto(&pb.User{
		ContactMethod: &pb.User_ContactPhone{ContactPhone: "12345"},
	})
	if u.ContactEmail != nil {
		t.Errorf("expected ContactEmail to be nil after setting ContactPhone, got %v", *u.ContactEmail)
	}
	if u.ContactPhone == nil || *u.ContactPhone != "12345" {
		t.Errorf("expected ContactPhone to be 12345")
	}
}

func TestDefensiveCopyBytes(t *testing.T) {
	u := &gen.User{
		Avatar: []byte{1, 2, 3},
	}
	pbUser := u.ToProto()
	u.Avatar[0] = 0xFF
	if pbUser.Avatar[0] != 1 {
		t.Errorf("expected pbUser.Avatar[0] to remain 1, got %v", pbUser.Avatar[0])
	}

	pbIn := &pb.User{
		Avatar: []byte{1, 2, 3},
	}
	u2 := &gen.User{}
	u2.FromProto(pbIn)
	pbIn.Avatar[0] = 0xFF
	if u2.Avatar[0] != 1 {
		t.Errorf("expected u2.Avatar[0] to remain 1, got %v", u2.Avatar[0])
	}
}

func TestStorageOptionalZeroVsUnset(t *testing.T) {
	fsZero := gen.UserFirestore{
		DeletedAt:      time.Time{},
		PreviousStatus: 0,
	}
	dZero := fsZero.ToDomain()
	if dZero.DeletedAt != nil {
		t.Errorf("expected DeletedAt to be nil, got %v", dZero.DeletedAt)
	}
	if dZero.PreviousStatus != nil {
		t.Errorf("expected PreviousStatus to be nil, got %v", dZero.PreviousStatus)
	}

	now := time.Now()
	fsValid := gen.UserFirestore{
		DeletedAt:      now,
		PreviousStatus: 2,
	}
	dValid := fsValid.ToDomain()
	if dValid.DeletedAt == nil || !dValid.DeletedAt.Equal(now) {
		t.Errorf("expected DeletedAt to be %v, got %v", now, dValid.DeletedAt)
	}
	if dValid.PreviousStatus == nil || *dValid.PreviousStatus != 2 {
		t.Errorf("expected PreviousStatus to be 2, got %v", dValid.PreviousStatus)
	}
}

func TestNilRepeatedElements(t *testing.T) {
	pbIn := &pb.User{
		Tags: []*pb.Tag{nil, {Key: "k", Value: "v"}},
	}
	var u gen.User
	u.FromProto(pbIn)
}

func TestProto3OptionalPresence(t *testing.T) {
	strVal := ""
	int32Val := int32(0)
	boolVal := false

	domain := &gen.AllOptionalScalars{
		OptString: &strVal,
		OptInt32:  &int32Val,
		OptBool:   &boolVal,
	}

	pbMsg := domain.ToProto()

	domain2 := &gen.AllOptionalScalars{}
	domain2.FromProto(pbMsg)

	if domain2.OptString == nil || *domain2.OptString != "" {
		t.Errorf("expected OptString to be non-nil empty string, got %v", domain2.OptString)
	}
	if domain2.OptInt32 == nil || *domain2.OptInt32 != 0 {
		t.Errorf("expected OptInt32 to be non-nil 0, got %v", domain2.OptInt32)
	}
	if domain2.OptBool == nil || *domain2.OptBool != false {
		t.Errorf("expected OptBool to be non-nil false, got %v", domain2.OptBool)
	}
}
