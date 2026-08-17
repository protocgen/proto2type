package generator

import (
	"testing"
)

func TestTSFieldType_Integration(t *testing.T) {
	opts := &Options{
		Domain:       true,
		Lang:         "typescript",
		TSZodImport:  "zod",
		TSInt64Style: "string",
	}

	ir := buildIRForProto(t, "user.proto", opts)

	if len(ir.Messages) == 0 {
		t.Fatalf("expected messages")
	}
}

func TestTSEnumGeneration_Integration(t *testing.T) {
	opts := &Options{
		Domain:      true,
		Lang:        "typescript",
		TSEnumStyle: "native",
	}

	ir := buildIRForProto(t, "user.proto", opts)

	if len(ir.Enums) == 0 {
		t.Fatalf("expected enums")
	}
}

func TestTSOneofDetection_Integration(t *testing.T) {
	opts := &Options{
		Domain: true,
		Lang:   "typescript",
	}

	buildIRForProto(t, "user.proto", opts)
}

func TestTSValidateConstraints_Integration(t *testing.T) {
	opts := &Options{
		Domain:   true,
		Lang:     "typescript",
		Validate: "true",
	}

	buildIRForProto(t, "user.proto", opts)
}

func TestIsRecursive(t *testing.T) {
	tests := []struct {
		name string
		msg  *DomainMessage
		want bool
	}{
		{
			name: "false for a message with no message-typed fields",
			msg: &DomainMessage{
				Name: "Foo",
				Fields: []*DomainField{
					{Kind: FieldKindScalar},
				},
			},
			want: false,
		},
		{
			name: "false for a message referencing a different message",
			msg: &DomainMessage{
				Name: "Foo",
				Fields: []*DomainField{
					{Kind: FieldKindMessage, MessageTypeName: "Bar"},
				},
			},
			want: false,
		},
		{
			name: "true for a message with a field whose MessageTypeName == the message's Name",
			msg: &DomainMessage{
				Name: "Foo",
				Fields: []*DomainField{
					{Kind: FieldKindMessage, MessageTypeName: "Foo", NeedsBox: true},
				},
			},
			want: true,
		},
		{
			name: "true for a message with an oneof variant whose TypeName == the message's Name",
			msg: &DomainMessage{
				Name: "Foo",
				Oneofs: []*DomainOneof{
					{
						Variants: []*OneofVariant{
							{Kind: FieldKindMessage, TypeName: "Foo", NeedsBox: true},
						},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRecursive(tt.msg)
			if got != tt.want {
				t.Errorf("isRecursive() = %v, want %v", got, tt.want)
			}
		})
	}
}
