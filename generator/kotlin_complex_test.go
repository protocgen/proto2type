package generator

import (
	"testing"
)

// --- Kotlin Complex Proto Integration Tests ---

func TestKotlin_NonContiguousEnumFromValue(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Lang: "kotlin", Domain: true})

	priority := irFindEnum(t, file, "Priority")

	wantValues := []struct {
		name       string
		kotlinName string
		number     int32
	}{
		{"Unspecified", "UNSPECIFIED", 0},
		{"Low", "LOW", 10},
		{"Medium", "MEDIUM", 20},
		{"High", "HIGH", 30},
		{"Critical", "CRITICAL", 100},
	}

	if len(priority.Values) != len(wantValues) {
		t.Fatalf("Priority has %d values, want %d", len(priority.Values), len(wantValues))
	}

	for i, want := range wantValues {
		got := priority.Values[i]
		if got.Name != want.name {
			t.Errorf("value[%d].Name = %q, want %q", i, got.Name, want.name)
		}
		kotlinName := pascalToUpperSnake(got.Name)
		if kotlinName != want.kotlinName {
			t.Errorf("pascalToUpperSnake(%q) = %q, want %q", got.Name, kotlinName, want.kotlinName)
		}
		if got.Number != want.number {
			t.Errorf("value[%d].Number = %d, want %d", i, got.Number, want.number)
		}
	}
}

func TestKotlin_NestedEnumTypeName(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Lang: "kotlin", Domain: true})

	theme := irFindEnum(t, file, "SettingsTheme")

	if len(theme.Values) != 3 {
		t.Errorf("SettingsTheme has %d values, want 3", len(theme.Values))
	}
}

func TestKotlin_DeeplyNestedMessageName(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Lang: "kotlin", Domain: true})

	// Should not panic / fatal — the message exists in the IR.
	team := irMustFindMessage(t, file.Messages, "OrganizationDepartmentTeam")
	if team.Name != "OrganizationDepartmentTeam" {
		t.Errorf("got Name = %q, want %q", team.Name, "OrganizationDepartmentTeam")
	}
}

func TestKotlin_MultipleOneofsGenerateSealedClasses(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Lang: "kotlin", Domain: true})

	notification := irMustFindMessage(t, file.Messages, "Notification")

	if len(notification.Oneofs) != 2 {
		t.Fatalf("Notification has %d oneofs, want 2", len(notification.Oneofs))
	}

	first := notification.Oneofs[0]
	if first.Name != "NotificationChannel" {
		t.Errorf("first oneof Name = %q, want %q", first.Name, "NotificationChannel")
	}

	second := notification.Oneofs[1]
	if second.Name != "NotificationContent" {
		t.Errorf("second oneof Name = %q, want %q", second.Name, "NotificationContent")
	}

	// Verify kotlinOneofFieldType returns nullable sealed class types.
	gotFirst := kotlinOneofFieldType(first.Name)
	if gotFirst != "NotificationChannel?" {
		t.Errorf("kotlinOneofFieldType(%q) = %q, want %q", first.Name, gotFirst, "NotificationChannel?")
	}

	gotSecond := kotlinOneofFieldType(second.Name)
	if gotSecond != "NotificationContent?" {
		t.Errorf("kotlinOneofFieldType(%q) = %q, want %q", second.Name, gotSecond, "NotificationContent?")
	}
}

func TestKotlin_MapTypes(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Lang: "kotlin", Domain: true})

	doc := irMustFindMessage(t, file.Messages, "Document")

	tests := []struct {
		field string
		want  string
	}{
		{"settings_map", "Map<String, Settings?>"},
		{"code_names", "Map<Int, String>"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			df := irFindField(t, doc, tt.field)
			got := kotlinFieldType(*df)
			if got != tt.want {
				t.Errorf("kotlinFieldType(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

func TestKotlin_WKTFieldTypes(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Lang: "kotlin", Domain: true})

	doc := irMustFindMessage(t, file.Messages, "Document")

	tests := []struct {
		field string
		want  string
	}{
		{"metadata", "Map<String, Any?>"},
		{"extension", "Any?"},
		{"update_mask", "List<String>"},
		{"archived", "Boolean?"},
		{"view_count", "Long?"},
		{"placeholders", "List<Unit>"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			df := irFindField(t, doc, tt.field)
			got := kotlinFieldType(*df)
			if got != tt.want {
				t.Errorf("kotlinFieldType(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}

func TestKotlin_RecursiveMessageFields(t *testing.T) {
	file := buildIRForProto(t, "complex.proto", &Options{Lang: "kotlin", Domain: true})

	tree := irMustFindMessage(t, file.Messages, "TreeNode")

	tests := []struct {
		field string
		want  string
	}{
		{"parent", "TreeNode?"},
		{"children", "List<TreeNode>"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			df := irFindField(t, tree, tt.field)
			got := kotlinFieldType(*df)
			if got != tt.want {
				t.Errorf("kotlinFieldType(%q) = %q, want %q", tt.field, got, tt.want)
			}
		})
	}
}
