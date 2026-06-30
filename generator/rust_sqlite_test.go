package generator

import (
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ---------------------------------------------------------------------------
// Test 1: rustSqliteFieldTypeFromIR – table-driven, no protoc required.
// ---------------------------------------------------------------------------

func TestRustSqliteFieldTypeFromIR(t *testing.T) {
	tests := []struct {
		name  string
		field *DomainField
		want  string
	}{
		// --- Repeated / Map always → "String" regardless of kind ---
		{"repeated scalar", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Int32Kind, Repeated: true}, "String"},
		{"repeated message", &DomainField{Kind: FieldKindMessage, Repeated: true}, "String"},
		{"map field", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, IsMap: true}, "String"},

		// --- Timestamp ---
		{"timestamp singular", &DomainField{Kind: FieldKindTimestamp}, "i64"},
		{"timestamp optional", &DomainField{Kind: FieldKindTimestamp, Optional: true}, "Option<i64>"},

		// --- Duration ---
		{"duration singular", &DomainField{Kind: FieldKindDuration}, "i64"},
		{"duration optional", &DomainField{Kind: FieldKindDuration, Optional: true}, "Option<i64>"},

		// --- WKT JSON types → "String" ---
		{"struct", &DomainField{Kind: FieldKindStruct}, "String"},
		{"value", &DomainField{Kind: FieldKindValue}, "String"},
		{"list_value", &DomainField{Kind: FieldKindListValue}, "String"},
		{"field_mask", &DomainField{Kind: FieldKindFieldMask}, "String"},
		{"empty", &DomainField{Kind: FieldKindEmpty}, "String"},
		{"any", &DomainField{Kind: FieldKindAny}, "String"},

		// --- Message (singular) → Option<String> ---
		{"message singular", &DomainField{Kind: FieldKindMessage}, "Option<String>"},

		// --- Enum ---
		{"enum singular (int)", &DomainField{Kind: FieldKindEnum}, "i32"},
		{"enum optional (int)", &DomainField{Kind: FieldKindEnum, Optional: true}, "Option<i32>"},
		{"enum singular (string)", &DomainField{Kind: FieldKindEnum, EnumAsString: true}, "String"},
		{"enum optional (string)", &DomainField{Kind: FieldKindEnum, EnumAsString: true, Optional: true}, "Option<String>"},

		// --- Scalars ---
		{"scalar string", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind}, "String"},
		{"scalar int32", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Int32Kind}, "i32"},
		{"scalar sint32", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Sint32Kind}, "i32"},
		{"scalar sfixed32", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Sfixed32Kind}, "i32"},
		{"scalar int64", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Int64Kind}, "i64"},
		{"scalar sint64", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Sint64Kind}, "i64"},
		{"scalar sfixed64", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Sfixed64Kind}, "i64"},
		{"scalar uint32", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Uint32Kind}, "i64"},
		{"scalar fixed32", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Fixed32Kind}, "i64"},
		{"scalar uint64", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Uint64Kind}, "i64"},
		{"scalar fixed64", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Fixed64Kind}, "i64"},
		{"scalar bool", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.BoolKind}, "bool"},
		{"scalar float", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.FloatKind}, "f32"},
		{"scalar double", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.DoubleKind}, "f64"},
		{"scalar bytes", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.BytesKind}, "Vec<u8>"},

		// --- Optional scalars ---
		{"optional string", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.StringKind, Optional: true}, "Option<String>"},
		{"optional int32", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Int32Kind, Optional: true}, "Option<i32>"},
		{"optional bool", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.BoolKind, Optional: true}, "Option<bool>"},
		{"optional float", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.FloatKind, Optional: true}, "Option<f32>"},
		{"optional double", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.DoubleKind, Optional: true}, "Option<f64>"},
		{"optional bytes", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.BytesKind, Optional: true}, "Option<Vec<u8>>"},
		{"optional int64", &DomainField{Kind: FieldKindScalar, ScalarKind: protoreflect.Int64Kind, Optional: true}, "Option<i64>"},

		// --- Wrapper kinds ---
		{"wrapper string", &DomainField{Kind: FieldKindWrapperString}, "Option<String>"},
		{"wrapper bool", &DomainField{Kind: FieldKindWrapperBool}, "Option<bool>"},
		{"wrapper int32", &DomainField{Kind: FieldKindWrapperInt32}, "Option<i32>"},
		{"wrapper int64", &DomainField{Kind: FieldKindWrapperInt64}, "Option<i64>"},
		{"wrapper uint32", &DomainField{Kind: FieldKindWrapperUInt32}, "Option<u32>"},
		{"wrapper uint64", &DomainField{Kind: FieldKindWrapperUInt64}, "Option<u64>"},
		{"wrapper float", &DomainField{Kind: FieldKindWrapperFloat}, "Option<f32>"},
		{"wrapper double", &DomainField{Kind: FieldKindWrapperDouble}, "Option<f64>"},
		{"wrapper bytes", &DomainField{Kind: FieldKindWrapperBytes}, "Option<Vec<u8>>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rustSqliteFieldTypeFromIR(tt.field)
			if got != tt.want {
				t.Errorf("rustSqliteFieldTypeFromIR(%s) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test 2: rustSqliteWrapperTypeFromIR – all 9 wrapper kinds.
// ---------------------------------------------------------------------------

func TestRustSqliteWrapperTypeFromIR(t *testing.T) {
	tests := []struct {
		kind FieldKind
		want string
	}{
		{FieldKindWrapperString, "Option<String>"},
		{FieldKindWrapperBool, "Option<bool>"},
		{FieldKindWrapperInt32, "Option<i32>"},
		{FieldKindWrapperInt64, "Option<i64>"},
		{FieldKindWrapperUInt32, "Option<u32>"},
		{FieldKindWrapperUInt64, "Option<u64>"},
		{FieldKindWrapperFloat, "Option<f32>"},
		{FieldKindWrapperDouble, "Option<f64>"},
		{FieldKindWrapperBytes, "Option<Vec<u8>>"},
	}

	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			got := rustSqliteWrapperTypeFromIR(tt.kind)
			if got != tt.want {
				t.Errorf("rustSqliteWrapperTypeFromIR(%v) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}

	// Non-wrapper kind should fall through to default.
	t.Run("non-wrapper defaults to Option<String>", func(t *testing.T) {
		got := rustSqliteWrapperTypeFromIR(FieldKindScalar)
		if got != "Option<String>" {
			t.Errorf("rustSqliteWrapperTypeFromIR(FieldKindScalar) = %q, want %q", got, "Option<String>")
		}
	})
}

// ---------------------------------------------------------------------------
// Test 3: buildProtoMessageMap – integration test using test protos.
// ---------------------------------------------------------------------------

func TestBuildProtoMessageMap(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"user.proto"})

	var msgs []*protogen.Message
	for _, f := range gen.Files {
		if f.Generate {
			msgs = f.Messages
			break
		}
	}
	if len(msgs) == 0 {
		t.Fatal("no messages found in user.proto")
	}

	m := buildProtoMessageMap(msgs)

	// user.proto should contain at least User, Address, Tag at the top level.
	topLevel := []string{
		"test.v1.User",
		"test.v1.Address",
		"test.v1.Tag",
	}
	for _, name := range topLevel {
		if _, ok := m[name]; !ok {
			t.Errorf("buildProtoMessageMap: missing top-level message %q", name)
		}
	}

	// The map should index by FullName, so the looked-up message should agree.
	for fullName, msg := range m {
		got := string(msg.Desc.FullName())
		if got != fullName {
			t.Errorf("buildProtoMessageMap key %q points to message with FullName %q", fullName, got)
		}
	}

	// Verify nested messages are also included (User has a map entry,
	// MetadataEntry, which protogen surfaces as a nested message).
	// The map should recursively contain any nested messages from the proto.
	for _, msg := range msgs {
		for _, nested := range msg.Messages {
			nestedFull := string(nested.Desc.FullName())
			if _, ok := m[nestedFull]; !ok {
				t.Errorf("buildProtoMessageMap: missing nested message %q", nestedFull)
			}
		}
	}
}

func TestBuildProtoMessageMap_Catalog(t *testing.T) {
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{"catalog.proto"})

	var msgs []*protogen.Message
	for _, f := range gen.Files {
		if f.Generate {
			msgs = f.Messages
			break
		}
	}
	if len(msgs) == 0 {
		t.Fatal("no messages found in catalog.proto")
	}

	m := buildProtoMessageMap(msgs)

	if _, ok := m["test.v1.ModelCatalogEntry"]; !ok {
		t.Errorf("buildProtoMessageMap: missing ModelCatalogEntry")
	}

	// Verify every message recursively appears.
	var checkAll func([]*protogen.Message)
	checkAll = func(list []*protogen.Message) {
		for _, msg := range list {
			fullName := string(msg.Desc.FullName())
			if _, ok := m[fullName]; !ok {
				t.Errorf("buildProtoMessageMap: missing %q", fullName)
			}
			checkAll(msg.Messages)
		}
	}
	checkAll(msgs)
}
