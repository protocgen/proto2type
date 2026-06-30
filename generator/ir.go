package generator

import "google.golang.org/protobuf/reflect/protoreflect"

// FieldKind classifies a proto field into a language-agnostic category.
// Backends use this to select the correct type mapping and serialisation logic.
type FieldKind int

const (
	// FieldKindScalar is a primitive type (bool, int32, string, bytes, etc.).
	FieldKindScalar FieldKind = iota
	// FieldKindEnum is a protobuf enum.
	FieldKindEnum
	// FieldKindMessage is a user-defined message (not a WKT).
	FieldKindMessage
	// FieldKindTimestamp is google.protobuf.Timestamp.
	FieldKindTimestamp
	// FieldKindDuration is google.protobuf.Duration.
	FieldKindDuration
	// FieldKindWrapperBool is google.protobuf.BoolValue.
	FieldKindWrapperBool
	// FieldKindWrapperInt32 is google.protobuf.Int32Value.
	FieldKindWrapperInt32
	// FieldKindWrapperInt64 is google.protobuf.Int64Value.
	FieldKindWrapperInt64
	// FieldKindWrapperUInt32 is google.protobuf.UInt32Value.
	FieldKindWrapperUInt32
	// FieldKindWrapperUInt64 is google.protobuf.UInt64Value.
	FieldKindWrapperUInt64
	// FieldKindWrapperFloat is google.protobuf.FloatValue.
	FieldKindWrapperFloat
	// FieldKindWrapperDouble is google.protobuf.DoubleValue.
	FieldKindWrapperDouble
	// FieldKindWrapperString is google.protobuf.StringValue.
	FieldKindWrapperString
	// FieldKindWrapperBytes is google.protobuf.BytesValue.
	FieldKindWrapperBytes
	// FieldKindStruct is google.protobuf.Struct.
	FieldKindStruct
	// FieldKindValue is google.protobuf.Value.
	FieldKindValue
	// FieldKindListValue is google.protobuf.ListValue.
	FieldKindListValue
	// FieldKindFieldMask is google.protobuf.FieldMask.
	FieldKindFieldMask
	// FieldKindEmpty is google.protobuf.Empty.
	FieldKindEmpty
	// FieldKindAny is google.protobuf.Any.
	FieldKindAny
)

// String returns the human-readable name of a FieldKind.
func (k FieldKind) String() string {
	switch k {
	case FieldKindScalar:
		return "Scalar"
	case FieldKindEnum:
		return "Enum"
	case FieldKindMessage:
		return "Message"
	case FieldKindTimestamp:
		return "Timestamp"
	case FieldKindDuration:
		return "Duration"
	case FieldKindWrapperBool:
		return "WrapperBool"
	case FieldKindWrapperInt32:
		return "WrapperInt32"
	case FieldKindWrapperInt64:
		return "WrapperInt64"
	case FieldKindWrapperUInt32:
		return "WrapperUInt32"
	case FieldKindWrapperUInt64:
		return "WrapperUInt64"
	case FieldKindWrapperFloat:
		return "WrapperFloat"
	case FieldKindWrapperDouble:
		return "WrapperDouble"
	case FieldKindWrapperString:
		return "WrapperString"
	case FieldKindWrapperBytes:
		return "WrapperBytes"
	case FieldKindStruct:
		return "Struct"
	case FieldKindValue:
		return "Value"
	case FieldKindListValue:
		return "ListValue"
	case FieldKindFieldMask:
		return "FieldMask"
	case FieldKindEmpty:
		return "Empty"
	case FieldKindAny:
		return "Any"
	default:
		return "Unknown"
	}
}

// IsWrapper returns true if the kind is one of the google.protobuf wrapper types.
func (k FieldKind) IsWrapper() bool {
	switch k {
	case FieldKindWrapperBool, FieldKindWrapperInt32, FieldKindWrapperInt64,
		FieldKindWrapperUInt32, FieldKindWrapperUInt64,
		FieldKindWrapperFloat, FieldKindWrapperDouble,
		FieldKindWrapperString, FieldKindWrapperBytes:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// IR types
// ---------------------------------------------------------------------------

// DomainFile is the top-level IR node for a single .proto source file.
type DomainFile struct {
	// SourcePath is the proto file path (e.g. "user.proto").
	SourcePath string
	// Enums are the top-level enum definitions.
	Enums []*DomainEnum
	// Messages are the top-level message definitions.
	Messages []*DomainMessage
}

// DomainMessage is the IR for a single proto message.
type DomainMessage struct {
	// Name is the PascalCase type name (e.g. "ModelCatalogEntry").
	// For nested messages this is flattened: Parent_Child.
	Name string
	// FullName is the fully-qualified proto name (e.g. "test.v1.User").
	FullName string
	// Comment is the leading proto comment, if any.
	Comment string
	// Fields in declaration order (excludes skipped fields and oneof members).
	Fields []*DomainField
	// Oneofs in declaration order.
	Oneofs []*DomainOneof
	// Enums defined inside this message.
	NestedEnums []*DomainEnum
	// Messages defined inside this message (excluding map-entry synthetics).
	NestedMessages []*DomainMessage
	// Skip is true when the message has (proto2type.message).skip = true.
	Skip bool
	// HasDocID is true when at least one field has (proto2type.field).document_id = true.
	HasDocID bool
}

// DomainField is the IR for a single message field.
type DomainField struct {
	// Name is the original proto field name (snake_case).
	Name string
	// PascalName is the PascalCase version of Name (e.g. "DisplayName").
	PascalName string
	// CamelName is the lowerCamelCase version of Name (e.g. "displayName").
	CamelName string

	// Kind classifies the field (scalar, message, timestamp, enum, etc.).
	Kind FieldKind
	// ScalarKind is the proto scalar kind (only meaningful when Kind == FieldKindScalar).
	ScalarKind protoreflect.Kind

	// MessageTypeName is the PascalCase IR type name for message-typed fields.
	// Empty for scalars and WKTs.
	MessageTypeName string
	// EnumTypeName is the PascalCase IR enum type name for enum-typed fields.
	// Empty for non-enum fields.
	EnumTypeName string

	// Optional is true for proto3 `optional` scalar fields.
	Optional bool
	// Repeated is true for `repeated` fields (but NOT maps).
	Repeated bool
	// IsMap is true for `map<K,V>` fields.
	IsMap bool

	// MapKey describes the key kind and scalar type (only set when IsMap is true).
	MapKey *MapTypeInfo
	// MapValue describes the value kind and type info (only set when IsMap is true).
	MapValue *MapTypeInfo

	// --- proto2type annotations ---

	// DocID is true when (proto2type.field).document_id = true.
	DocID bool
	// ServerTimestamp is true when (proto2type.field).server_timestamp = true.
	ServerTimestamp bool
	// FieldSkip is true when (proto2type.field).skip = true (this field was already
	// excluded from Fields, but we keep the flag for edge-case introspection).
	FieldSkip bool
	// NameOverride is the (proto2type.field).name value, or empty.
	NameOverride string
	// Inline is true when (proto2type.field).inline = true.
	Inline bool
	// EnumAsString is true when the enum should be serialised as its string name.
	EnumAsString bool
	// Omitempty is the resolved omitempty flag for this field.
	Omitempty bool

	// OneofName is the oneof group name when this field is a oneof variant.
	// Empty for non-oneof fields. (In the main Fields slice, oneof members
	// are NOT present; they appear only in DomainOneof.Variants.)
	OneofName string
}

// MapTypeInfo captures the kind and type name of a map key or value.
type MapTypeInfo struct {
	Kind            FieldKind
	ScalarKind      protoreflect.Kind
	MessageTypeName string
	EnumTypeName    string
}

// DomainEnum is the IR for a proto enum.
type DomainEnum struct {
	// Name is the PascalCase type name. For nested enums this is prefixed
	// with the parent message name (e.g. "UserSettings_Theme").
	Name string
	// Comment is the leading proto comment.
	Comment string
	// Values in declaration order.
	Values []*DomainEnumValue
}

// DomainEnumValue is a single enum value.
type DomainEnumValue struct {
	// Name is the PascalCase, prefix-stripped name (e.g. "Active").
	Name string
	// ProtoName is the original UPPER_SNAKE proto name (e.g. "USER_STATUS_ACTIVE").
	ProtoName string
	// Number is the proto enum numeric value.
	Number int32
	// IsDefault is true when Number == 0 and this is the first value.
	IsDefault bool
	// Comment is the leading proto comment.
	Comment string
}

// DomainOneof is the IR for a proto oneof group.
type DomainOneof struct {
	// Name is MessageName + PascalCase(oneof_name), e.g. "UserContactMethod".
	Name string
	// FieldName is the snake_case oneof field name (e.g. "contact_method").
	FieldName string
	// Variants in declaration order.
	Variants []*OneofVariant
}

// OneofVariant is a single variant inside a oneof group.
type OneofVariant struct {
	// Name is the PascalCase variant name (e.g. "ContactEmail").
	Name string
	// ProtoName is the original proto field name (e.g. "contact_email").
	ProtoName string
	// Kind classifies the variant (scalar, message, timestamp, enum, etc.).
	Kind FieldKind
	// ScalarKind is set when Kind == FieldKindScalar.
	ScalarKind protoreflect.Kind
	// TypeName is the resolved type name for message/enum variants.
	TypeName string
}
