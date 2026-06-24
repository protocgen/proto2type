package generator

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"

	proto2typepb "github.com/protocgen/proto2type/proto/proto2type"
)

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
