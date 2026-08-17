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
