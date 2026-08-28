package generator

import (
	"fmt"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"

	proto2typepb "github.com/protocgen/proto2type/proto/proto2type"
)

// getFieldBehaviors extracts google.api.field_behavior annotations from a field.
// Returns nil if no annotation is present.
func getFieldBehaviors(field *protogen.Field) []annotations.FieldBehavior {
	opts, ok := field.Desc.Options().(*descriptorpb.FieldOptions)
	if !ok || opts == nil {
		return nil
	}
	if !proto.HasExtension(opts, annotations.E_FieldBehavior) {
		return nil
	}
	behaviors, ok := proto.GetExtension(opts, annotations.E_FieldBehavior).([]annotations.FieldBehavior)
	if !ok || len(behaviors) == 0 {
		return nil
	}
	return behaviors
}

// getFieldOptions returns the proto2type field options for a field, or nil if none are set.
func getFieldOptions(field *protogen.Field) *proto2typepb.FieldOptions {
	opts, ok := field.Desc.Options().(*descriptorpb.FieldOptions)
	if !ok || opts == nil {
		return nil
	}
	if !proto.HasExtension(opts, proto2typepb.E_Field) {
		return nil
	}
	ext, ok := proto.GetExtension(opts, proto2typepb.E_Field).(*proto2typepb.FieldOptions)
	if !ok {
		return nil
	}
	return ext
}

// getMessageOptions returns the proto2type message options, or nil if none are set.
func getMessageOptions(msg *protogen.Message) *proto2typepb.MessageOptions {
	opts, ok := msg.Desc.Options().(*descriptorpb.MessageOptions)
	if !ok || opts == nil {
		return nil
	}
	if !proto.HasExtension(opts, proto2typepb.E_Message) {
		return nil
	}
	ext, ok := proto.GetExtension(opts, proto2typepb.E_Message).(*proto2typepb.MessageOptions)
	if !ok {
		return nil
	}
	return ext
}

// isDocumentID returns true if the field is marked as a document ID.
func isDocumentID(field *protogen.Field) bool {
	fo := getFieldOptions(field)
	return fo != nil && fo.DocumentId
}

// isServerTimestamp returns true if the field is marked as a server timestamp.
func isServerTimestamp(field *protogen.Field) bool {
	fo := getFieldOptions(field)
	return fo != nil && fo.ServerTimestamp
}

// isFieldSkipped returns true if the field should be excluded from generated types.
func isFieldSkipped(field *protogen.Field) bool {
	fo := getFieldOptions(field)
	return fo != nil && fo.Skip
}

// isMessageSkipped returns true if the message should be excluded from generation.
func isMessageSkipped(msg *protogen.Message) bool {
	mo := getMessageOptions(msg)
	return mo != nil && mo.Skip
}

// fieldOmitempty returns the explicit omitempty setting, or UNSPECIFIED if not set.
func fieldOmitempty(field *protogen.Field) proto2typepb.OptionalBool {
	fo := getFieldOptions(field)
	if fo == nil {
		return proto2typepb.OptionalBool_OPTIONAL_BOOL_UNSPECIFIED
	}
	return fo.Omitempty
}

// isInline returns true if the field should be flattened (Mongo bson:",inline").
func isInline(field *protogen.Field) bool {
	fo := getFieldOptions(field)
	return fo != nil && fo.Inline
}

// fieldNameOverride returns the storage name override, or empty string if not set.
func fieldNameOverride(field *protogen.Field) string {
	fo := getFieldOptions(field)
	if fo == nil {
		return ""
	}
	return fo.Name
}

// validateFieldNameOverride checks that a field name override does not contain
// dangerous characters that could cause injection or path traversal issues in
// storage backends. Returns an error message if invalid, or empty string if valid.
func validateFieldNameOverride(name string) string {
	for _, c := range name {
		switch c {
		case '.', '/', '$', '[', ']', '\x00', '"', '`':
			return fmt.Sprintf("field name override %q contains invalid character %q", name, string(c))
		}
	}
	return ""
}

// isEnumAsString returns true if this enum field should use string representation.
// Per-field annotation takes priority; global option is the fallback.
func isEnumAsString(field *protogen.Field, opts *Options) bool {
	fo := getFieldOptions(field)
	if fo != nil {
		switch fo.EnumAsString {
		case proto2typepb.OptionalBool_OPTIONAL_BOOL_TRUE:
			return true
		case proto2typepb.OptionalBool_OPTIONAL_BOOL_FALSE:
			return false
		}
	}
	// Fallback to global option.
	return opts != nil && opts.EnumAsString
}

// isFieldEncrypted returns true if the field is marked for application-level encryption.
func isFieldEncrypted(field *protogen.Field) bool {
	fo := getFieldOptions(field)
	return fo != nil && fo.Encrypt
}

// validateEncrypt returns true only if the field has encrypt=true AND is a valid
// candidate for encryption (non-repeated, non-map, string scalar, not a DocID).
// Invalid combinations are silently ignored — the annotation has no effect.
func validateEncrypt(field *protogen.Field) bool {
	if !isFieldEncrypted(field) {
		return false
	}
	// Only string fields are supported for v1 encryption.
	if field.Desc.Kind() != protoreflect.StringKind {
		return false
	}
	// Repeated and map fields are not supported.
	if field.Desc.IsList() || field.Desc.IsMap() {
		return false
	}
	// Message fields (e.g. wrappers) are not supported.
	if field.Desc.Message() != nil {
		return false
	}
	// DocID fields are excluded from the Firestore struct — can't encrypt what's not there.
	if isDocumentID(field) {
		return false
	}
	return true
}

// getComputedField returns the computed field config, or nil if not set.
func getComputedField(field *protogen.Field) *proto2typepb.ComputedField {
	fo := getFieldOptions(field)
	if fo == nil {
		return nil
	}
	return fo.Computed
}
