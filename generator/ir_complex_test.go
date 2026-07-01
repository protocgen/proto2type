package generator

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// ---------------------------------------------------------------------------
// IR tests for complex.proto – exercises advanced proto features.
// ---------------------------------------------------------------------------

func TestIR_NonContiguousEnum(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Domain: true})

	priority := irFindEnum(t, file, "Priority")

	if len(priority.Values) != 5 {
		t.Fatalf("Priority has %d values, want 5", len(priority.Values))
	}

	wantNumbers := []int32{0, 10, 20, 30, 100}
	for i, want := range wantNumbers {
		got := priority.Values[i]
		if got.Number != want {
			t.Errorf("value[%d].Number = %d, want %d", i, got.Number, want)
		}
	}

	// Only the first value (Number==0) should have IsDefault==true.
	for i, v := range priority.Values {
		wantDefault := i == 0
		if v.IsDefault != wantDefault {
			t.Errorf("value[%d] %q: IsDefault = %v, want %v", i, v.Name, v.IsDefault, wantDefault)
		}
	}
}

func TestIR_NestedEnum(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Domain: true})

	var settings *DomainMessage
	for _, m := range file.Messages {
		if m.Name == "Settings" {
			settings = m
			break
		}
	}
	if settings == nil {
		t.Fatal("Settings message not found")
	}

	if len(settings.NestedEnums) == 0 {
		t.Fatal("Settings has no nested enums")
	}

	var theme *DomainEnum
	for _, e := range settings.NestedEnums {
		if e.Name == "SettingsTheme" {
			theme = e
			break
		}
	}
	if theme == nil {
		t.Fatalf("nested enum SettingsTheme not found; got enums: %v",
			func() []string {
				var names []string
				for _, e := range settings.NestedEnums {
					names = append(names, e.Name)
				}
				return names
			}())
	}

	if len(theme.Values) != 3 {
		t.Errorf("SettingsTheme has %d values, want 3", len(theme.Values))
	}
}

func TestIR_DeeplyNestedMessages(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Domain: true})

	// Find Organization at top level.
	var org *DomainMessage
	for _, m := range file.Messages {
		if m.Name == "Organization" {
			org = m
			break
		}
	}
	if org == nil {
		t.Fatal("Organization message not found")
	}

	// Organization should have a nested Department.
	var dept *DomainMessage
	for _, m := range org.NestedMessages {
		if m.Name == "OrganizationDepartment" {
			dept = m
			break
		}
	}
	if dept == nil {
		t.Fatalf("OrganizationDepartment not found in NestedMessages; got: %v",
			func() []string {
				var names []string
				for _, m := range org.NestedMessages {
					names = append(names, m.Name)
				}
				return names
			}())
	}

	// Department should have a nested Team.
	var team *DomainMessage
	for _, m := range dept.NestedMessages {
		if m.Name == "OrganizationDepartmentTeam" {
			team = m
			break
		}
	}
	if team == nil {
		t.Fatalf("OrganizationDepartmentTeam not found in Department.NestedMessages; got: %v",
			func() []string {
				var names []string
				for _, m := range dept.NestedMessages {
					names = append(names, m.Name)
				}
				return names
			}())
	}
	_ = team // Verify it exists; name already checked.
}

func TestIR_MultipleOneofs(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Domain: true})

	notif := irMustFindMessage(t, file.Messages, "Notification")

	if len(notif.Oneofs) != 2 {
		t.Fatalf("Notification has %d oneofs, want 2", len(notif.Oneofs))
	}

	// First oneof: channel.
	channel := notif.Oneofs[0]
	if channel.Name != "NotificationChannel" {
		t.Errorf("oneof[0].Name = %q, want %q", channel.Name, "NotificationChannel")
	}
	if len(channel.Variants) != 3 {
		t.Fatalf("channel oneof has %d variants, want 3", len(channel.Variants))
	}
	channelWant := []string{"email", "sms", "push_token"}
	for i, want := range channelWant {
		if channel.Variants[i].ProtoName != want {
			t.Errorf("channel.Variants[%d].ProtoName = %q, want %q", i, channel.Variants[i].ProtoName, want)
		}
		if channel.Variants[i].Kind != FieldKindScalar {
			t.Errorf("channel.Variants[%d].Kind = %v, want FieldKindScalar", i, channel.Variants[i].Kind)
		}
	}

	// Second oneof: content.
	content := notif.Oneofs[1]
	if content.Name != "NotificationContent" {
		t.Errorf("oneof[1].Name = %q, want %q", content.Name, "NotificationContent")
	}
	if len(content.Variants) != 2 {
		t.Fatalf("content oneof has %d variants, want 2", len(content.Variants))
	}
	contentWant := []string{"plain_text", "html"}
	for i, want := range contentWant {
		if content.Variants[i].ProtoName != want {
			t.Errorf("content.Variants[%d].ProtoName = %q, want %q", i, content.Variants[i].ProtoName, want)
		}
		if content.Variants[i].Kind != FieldKindScalar {
			t.Errorf("content.Variants[%d].Kind = %v, want FieldKindScalar", i, content.Variants[i].Kind)
		}
	}
}

func TestIR_MapWithMessageValues(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Domain: true})

	doc := irMustFindMessage(t, file.Messages, "Document")

	// settings_map: map<string, Settings>
	settingsMap := irFindField(t, doc, "settings_map")
	if !settingsMap.IsMap {
		t.Error("settings_map.IsMap = false, want true")
	}
	if settingsMap.MapValue == nil {
		t.Fatal("settings_map.MapValue is nil")
	}
	if settingsMap.MapValue.Kind != FieldKindMessage {
		t.Errorf("settings_map.MapValue.Kind = %v, want FieldKindMessage", settingsMap.MapValue.Kind)
	}
	if settingsMap.MapValue.MessageTypeName != "Settings" {
		t.Errorf("settings_map.MapValue.MessageTypeName = %q, want %q", settingsMap.MapValue.MessageTypeName, "Settings")
	}

	// code_names: map<int32, string>
	codeNames := irFindField(t, doc, "code_names")
	if !codeNames.IsMap {
		t.Error("code_names.IsMap = false, want true")
	}
	if codeNames.MapKey == nil {
		t.Fatal("code_names.MapKey is nil")
	}
	if codeNames.MapKey.ScalarKind != protoreflect.Int32Kind {
		t.Errorf("code_names.MapKey.ScalarKind = %v, want %v", codeNames.MapKey.ScalarKind, protoreflect.Int32Kind)
	}
	if codeNames.MapValue == nil {
		t.Fatal("code_names.MapValue is nil")
	}
	if codeNames.MapValue.ScalarKind != protoreflect.StringKind {
		t.Errorf("code_names.MapValue.ScalarKind = %v, want %v", codeNames.MapValue.ScalarKind, protoreflect.StringKind)
	}
}

func TestIR_WKTFields(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Domain: true})

	doc := irMustFindMessage(t, file.Messages, "Document")

	tests := []struct {
		fieldName string
		wantKind  FieldKind
		repeated  bool
	}{
		{"metadata", FieldKindStruct, false},
		{"extension", FieldKindAny, false},
		{"update_mask", FieldKindFieldMask, false},
		{"archived", FieldKindWrapperBool, false},
		{"view_count", FieldKindWrapperInt64, false},
		{"placeholders", FieldKindEmpty, true},
	}

	for _, tt := range tests {
		t.Run(tt.fieldName, func(t *testing.T) {
			f := irFindField(t, doc, tt.fieldName)
			if f.Kind != tt.wantKind {
				t.Errorf("%s.Kind = %v, want %v", tt.fieldName, f.Kind, tt.wantKind)
			}
			if f.Repeated != tt.repeated {
				t.Errorf("%s.Repeated = %v, want %v", tt.fieldName, f.Repeated, tt.repeated)
			}
		})
	}
}

func TestIR_RecursiveMessage(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Domain: true})

	tree := irMustFindMessage(t, file.Messages, "TreeNode")

	// children: repeated TreeNode
	children := irFindField(t, tree, "children")
	if !children.Repeated {
		t.Error("children.Repeated = false, want true")
	}
	if children.Kind != FieldKindMessage {
		t.Errorf("children.Kind = %v, want FieldKindMessage", children.Kind)
	}
	if children.MessageTypeName != "TreeNode" {
		t.Errorf("children.MessageTypeName = %q, want %q", children.MessageTypeName, "TreeNode")
	}

	// parent: TreeNode
	parent := irFindField(t, tree, "parent")
	if parent.Kind != FieldKindMessage {
		t.Errorf("parent.Kind = %v, want FieldKindMessage", parent.Kind)
	}
	if parent.MessageTypeName != "TreeNode" {
		t.Errorf("parent.MessageTypeName = %q, want %q", parent.MessageTypeName, "TreeNode")
	}

	// NeedsBox: parent should need Box (recursive self-reference),
	// children should NOT (repeated fields use Vec which provides indirection).
	if !parent.NeedsBox {
		t.Error("parent.NeedsBox = false, want true (recursive self-reference)")
	}
	if children.NeedsBox {
		t.Error("children.NeedsBox = true, want false (repeated field, Vec provides indirection)")
	}
}

func TestIR_NonRecursiveMessage_NoBox(t *testing.T) {
	file := buildIRForProto(t, "user.proto", &Options{Domain: true})

	user := irMustFindMessage(t, file.Messages, "User")
	address := irFindField(t, user, "address")

	if address.Kind != FieldKindMessage {
		t.Errorf("address.Kind = %v, want FieldKindMessage", address.Kind)
	}
	// Address does not reference User, so NeedsBox should be false.
	if address.NeedsBox {
		t.Error("address.NeedsBox = true, want false (Address is not recursive)")
	}
}

func TestIR_SkipAnnotation(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Domain: true})

	audit := irMustFindMessage(t, file.Messages, "AuditLog")

	// internal_notes should NOT appear in Fields (it was skipped).
	for _, f := range audit.Fields {
		if f.Name == "internal_notes" {
			t.Error("internal_notes should be skipped but was found in Fields")
		}
	}

	// Count non-oneof-placeholder fields — should be exactly 3 (id, action, user_id).
	var regularFields int
	for _, f := range audit.Fields {
		if !f.IsOneof {
			regularFields++
		}
	}
	if regularFields != 3 {
		t.Errorf("AuditLog has %d regular fields, want 3", regularFields)
	}

	// Verify the expected field names are present.
	wantFields := map[string]bool{"id": false, "action": false, "user_id": false}
	for _, f := range audit.Fields {
		if f.IsOneof {
			continue
		}
		if _, ok := wantFields[f.Name]; ok {
			wantFields[f.Name] = true
		}
	}
	for name, found := range wantFields {
		if !found {
			t.Errorf("expected field %q not found in AuditLog", name)
		}
	}
}

func TestIR_NameOverride(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Domain: true})

	audit := irMustFindMessage(t, file.Messages, "AuditLog")

	userID := irFindField(t, audit, "user_id")
	if userID.NameOverride != "actor" {
		t.Errorf("user_id.NameOverride = %q, want %q", userID.NameOverride, "actor")
	}
}

func TestIR_DocumentID(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Domain: true})

	audit := irMustFindMessage(t, file.Messages, "AuditLog")

	if !audit.HasDocID {
		t.Error("AuditLog.HasDocID = false, want true")
	}

	id := irFindField(t, audit, "id")
	if !id.DocID {
		t.Error("id.DocID = false, want true")
	}
}
