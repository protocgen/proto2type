package generator

import (
	"testing"
)

// ---------------------------------------------------------------------------
// IR integration tests – reuse the same buildFileDescriptorSet / newPlugin
// helpers from generator_integration_test.go.
// ---------------------------------------------------------------------------

func TestBuildDomainFile_UserFieldKinds(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})

	var file *DomainFile
	for _, f := range gen.Files {
		if f.Generate {
			file = BuildDomainFile(f, &Options{Domain: true})
			break
		}
	}
	if file == nil {
		t.Fatal("no generated file found")
	}

	if file.SourcePath != "user.proto" {
		t.Errorf("SourcePath = %q, want %q", file.SourcePath, "user.proto")
	}

	// Find the User message.
	var user *DomainMessage
	for _, m := range file.Messages {
		if m.Name == "User" {
			user = m
			break
		}
	}
	if user == nil {
		t.Fatal("User message not found in IR")
	}

	// Expected field kinds (excludes oneof members).
	wantFields := map[string]FieldKind{
		"id":              FieldKindScalar,
		"email":           FieldKindScalar,
		"display_name":    FieldKindScalar,
		"active":          FieldKindScalar,
		"age":             FieldKindScalar,
		"roles":           FieldKindScalar,  // repeated scalar
		"metadata":        FieldKindMessage, // map (outer kind is Message)
		"address":         FieldKindMessage,
		"created_at":      FieldKindTimestamp,
		"session_timeout": FieldKindDuration,
		"phone":           FieldKindScalar, // optional string
		"avatar":          FieldKindScalar, // bytes
		"nickname":        FieldKindWrapperString,
		"status":          FieldKindEnum,
		"tags":            FieldKindMessage,    // repeated message
		"deleted_at":      FieldKindTimestamp,   // optional timestamp
		"previous_status": FieldKindEnum,        // optional enum
		"update_mask":      FieldKindFieldMask,  // WKT reference
		"extra_metadata":   FieldKindStruct,     // WKT reference
		"preferences":      FieldKindListValue,  // WKT reference
		"avatar_thumbnail": FieldKindScalar,     // optional bytes
	}

	for _, f := range user.Fields {
		if f.IsOneof {
			// Collapsed oneof placeholders are not checked for Kind.
			continue
		}
		want, ok := wantFields[f.Name]
		if !ok {
			t.Errorf("unexpected field %q in IR", f.Name)
			continue
		}
		if f.Kind != want {
			t.Errorf("field %q: Kind = %v, want %v", f.Name, f.Kind, want)
		}
		delete(wantFields, f.Name)
	}
	for missing := range wantFields {
		t.Errorf("expected field %q not found in IR", missing)
	}
}

func TestBuildDomainFile_FieldFlags(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})

	var user *DomainMessage
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		df := BuildDomainFile(f, &Options{Domain: true})
		for _, m := range df.Messages {
			if m.Name == "User" {
				user = m
			}
		}
	}
	if user == nil {
		t.Fatal("User message not found")
	}

	findField := func(name string) *DomainField {
		for _, f := range user.Fields {
			if f.Name == name {
				return f
			}
		}
		t.Fatalf("field %q not found", name)
		return nil
	}

	// phone is optional string
	phone := findField("phone")
	if !phone.Optional {
		t.Error("phone.Optional = false, want true")
	}

	// roles is repeated
	roles := findField("roles")
	if !roles.Repeated {
		t.Error("roles.Repeated = false, want true")
	}

	// metadata is a map
	meta := findField("metadata")
	if !meta.IsMap {
		t.Error("metadata.IsMap = false, want true")
	}
	if meta.MapKey == nil {
		t.Fatal("metadata.MapKey is nil")
	}
	if meta.MapValue == nil {
		t.Fatal("metadata.MapValue is nil")
	}

	// tags is repeated message
	tags := findField("tags")
	if !tags.Repeated {
		t.Error("tags.Repeated = false, want true")
	}
	if tags.MessageTypeName != "Tag" {
		t.Errorf("tags.MessageTypeName = %q, want %q", tags.MessageTypeName, "Tag")
	}
}

func TestBuildDomainFile_EnumValues(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})

	var file *DomainFile
	for _, f := range gen.Files {
		if f.Generate {
			file = BuildDomainFile(f, &Options{Domain: true})
			break
		}
	}
	if file == nil {
		t.Fatal("no generated file found")
	}

	// Top-level enum UserStatus.
	if len(file.Enums) == 0 {
		t.Fatal("no top-level enums found")
	}

	var userStatus *DomainEnum
	for _, e := range file.Enums {
		if e.Name == "UserStatus" {
			userStatus = e
		}
	}
	if userStatus == nil {
		t.Fatal("UserStatus enum not found")
	}

	wantValues := []struct {
		name      string
		protoName string
		number    int32
		isDefault bool
	}{
		{"Unspecified", "USER_STATUS_UNSPECIFIED", 0, true},
		{"Active", "USER_STATUS_ACTIVE", 1, false},
		{"Suspended", "USER_STATUS_SUSPENDED", 2, false},
		{"Deleted", "USER_STATUS_DELETED", 3, false},
	}

	if len(userStatus.Values) != len(wantValues) {
		t.Fatalf("UserStatus has %d values, want %d", len(userStatus.Values), len(wantValues))
	}

	for i, want := range wantValues {
		got := userStatus.Values[i]
		if got.Name != want.name {
			t.Errorf("value[%d].Name = %q, want %q", i, got.Name, want.name)
		}
		if got.ProtoName != want.protoName {
			t.Errorf("value[%d].ProtoName = %q, want %q", i, got.ProtoName, want.protoName)
		}
		if got.Number != want.number {
			t.Errorf("value[%d].Number = %d, want %d", i, got.Number, want.number)
		}
		if got.IsDefault != want.isDefault {
			t.Errorf("value[%d].IsDefault = %v, want %v", i, got.IsDefault, want.isDefault)
		}
	}
}

func TestBuildDomainFile_OneofDetection(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})

	var user *DomainMessage
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		df := BuildDomainFile(f, &Options{Domain: true})
		for _, m := range df.Messages {
			if m.Name == "User" {
				user = m
			}
		}
	}
	if user == nil {
		t.Fatal("User message not found")
	}

	if len(user.Oneofs) != 1 {
		t.Fatalf("User has %d oneofs, want 1", len(user.Oneofs))
	}

	oneof := user.Oneofs[0]
	if oneof.Name != "UserContactMethod" {
		t.Errorf("oneof.Name = %q, want %q", oneof.Name, "UserContactMethod")
	}
	if oneof.FieldName != "contact_method" {
		t.Errorf("oneof.FieldName = %q, want %q", oneof.FieldName, "contact_method")
	}
	if len(oneof.Variants) != 2 {
		t.Fatalf("oneof has %d variants, want 2", len(oneof.Variants))
	}

	// Both variants are strings.
	for _, v := range oneof.Variants {
		if v.Kind != FieldKindScalar {
			t.Errorf("variant %q: Kind = %v, want Scalar", v.ProtoName, v.Kind)
		}
	}

	// Check variant names.
	if oneof.Variants[0].Name != "ContactEmail" {
		t.Errorf("variant[0].Name = %q, want %q", oneof.Variants[0].Name, "ContactEmail")
	}
	if oneof.Variants[1].Name != "ContactPhone" {
		t.Errorf("variant[1].Name = %q, want %q", oneof.Variants[1].Name, "ContactPhone")
	}
}

func TestBuildDomainFile_CatalogAnnotations(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"catalog.proto"})

	var catalog *DomainMessage
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		df := BuildDomainFile(f, &Options{Domain: true})
		for _, m := range df.Messages {
			if m.Name == "ModelCatalogEntry" {
				catalog = m
			}
		}
	}
	if catalog == nil {
		t.Fatal("ModelCatalogEntry message not found")
	}

	// HasDocID should be true.
	if !catalog.HasDocID {
		t.Error("ModelCatalogEntry.HasDocID = false, want true")
	}

	findField := func(name string) *DomainField {
		for _, f := range catalog.Fields {
			if f.Name == name {
				return f
			}
		}
		t.Fatalf("field %q not found", name)
		return nil
	}

	// model_id should be DocID.
	modelID := findField("model_id")
	if !modelID.DocID {
		t.Error("model_id.DocID = false, want true")
	}

	// updated_at should be ServerTimestamp.
	updatedAt := findField("updated_at")
	if !updatedAt.ServerTimestamp {
		t.Error("updated_at.ServerTimestamp = false, want true")
	}

	// notes should have Omitempty = true.
	notes := findField("notes")
	if !notes.Omitempty {
		t.Error("notes.Omitempty = false, want true")
	}

	// provider should NOT be DocID.
	provider := findField("provider")
	if provider.DocID {
		t.Error("provider.DocID = true, want false")
	}
}

func TestBuildDomainFile_NestedMessages(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})

	var file *DomainFile
	for _, f := range gen.Files {
		if f.Generate {
			file = BuildDomainFile(f, &Options{Domain: true})
			break
		}
	}
	if file == nil {
		t.Fatal("no generated file found")
	}

	// user.proto has User, Address, Tag at top level.
	names := map[string]bool{}
	for _, m := range file.Messages {
		names[m.Name] = true
	}

	for _, want := range []string{"User", "Address", "Tag"} {
		if !names[want] {
			t.Errorf("top-level message %q not found in IR", want)
		}
	}
}

func TestBuildDomainFile_PascalAndCamelNames(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})

	var user *DomainMessage
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		df := BuildDomainFile(f, &Options{Domain: true})
		for _, m := range df.Messages {
			if m.Name == "User" {
				user = m
			}
		}
	}
	if user == nil {
		t.Fatal("User message not found")
	}

	wantNames := map[string]struct {
		pascal string
		camel  string
	}{
		"display_name":    {"DisplayName", "displayName"},
		"created_at":      {"CreatedAt", "createdAt"},
		"session_timeout": {"SessionTimeout", "sessionTimeout"},
		"email":           {"Email", "email"},
	}

	for _, f := range user.Fields {
		want, ok := wantNames[f.Name]
		if !ok {
			continue
		}
		if f.PascalName != want.pascal {
			t.Errorf("field %q: PascalName = %q, want %q", f.Name, f.PascalName, want.pascal)
		}
		if f.CamelName != want.camel {
			t.Errorf("field %q: CamelName = %q, want %q", f.Name, f.CamelName, want.camel)
		}
	}
}

func TestBuildDomainFile_EnumAsString(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})

	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}

		// Without EnumAsString.
		dfNo := BuildDomainFile(f, &Options{Domain: true})
		for _, m := range dfNo.Messages {
			if m.Name != "User" {
				continue
			}
			for _, fld := range m.Fields {
				if fld.Name == "status" {
					if fld.EnumAsString {
						t.Error("status.EnumAsString = true without option, want false")
					}
				}
			}
		}

		// With EnumAsString.
		dfYes := BuildDomainFile(f, &Options{Domain: true, EnumAsString: true})
		for _, m := range dfYes.Messages {
			if m.Name != "User" {
				continue
			}
			for _, fld := range m.Fields {
				if fld.Name == "status" {
					if !fld.EnumAsString {
						t.Error("status.EnumAsString = false with option, want true")
					}
				}
			}
		}
	}
}

func TestBuildDomainFile_OptionalTimestamp(t *testing.T) {
	df := buildIRForProto(t, "user.proto", &Options{Domain: true})
	user := irMustFindMessage(t, df.Messages, "User")
	f := irFindField(t, user, "deleted_at")

	if f.Kind != FieldKindTimestamp {
		t.Errorf("deleted_at.Kind = %v, want %v", f.Kind, FieldKindTimestamp)
	}
	if !f.Optional {
		t.Error("deleted_at.Optional = false, want true")
	}
}

func TestBuildDomainFile_OptionalEnum(t *testing.T) {
	df := buildIRForProto(t, "user.proto", &Options{Domain: true})
	user := irMustFindMessage(t, df.Messages, "User")
	f := irFindField(t, user, "previous_status")

	if f.Kind != FieldKindEnum {
		t.Errorf("previous_status.Kind = %v, want %v", f.Kind, FieldKindEnum)
	}
	if !f.Optional {
		t.Error("previous_status.Optional = false, want true")
	}
	if f.EnumTypeName != "UserStatus" {
		t.Errorf("previous_status.EnumTypeName = %q, want %q", f.EnumTypeName, "UserStatus")
	}
}
