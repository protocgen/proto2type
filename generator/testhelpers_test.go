package generator

import (
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
)

// ---------------------------------------------------------------------------
// Shared test helpers for IR-based tests.
// ---------------------------------------------------------------------------

// buildIRForProto compiles the given proto file and returns the DomainFile IR.
func buildIRForProto(t *testing.T, protoFile string, opts *Options) *DomainFile {
	t.Helper()
	fds := buildFileDescriptorSet(t)
	gen := newPlugin(t, fds, []string{protoFile})

	for _, f := range gen.Files {
		if f.Generate {
			return BuildDomainFile(f, opts)
		}
	}
	t.Fatalf("no generated file found for %s", protoFile)
	return nil
}

// irFindMessage searches top-level and nested messages recursively by name.
func irFindMessage(t *testing.T, msgs []*DomainMessage, name string) *DomainMessage {
	t.Helper()
	for _, m := range msgs {
		if m.Name == name {
			return m
		}
		if found := irFindMessage(t, m.NestedMessages, name); found != nil {
			return found
		}
	}
	return nil
}

// irMustFindMessage is like irFindMessage but fatals if not found.
func irMustFindMessage(t *testing.T, msgs []*DomainMessage, name string) *DomainMessage {
	t.Helper()
	m := irFindMessage(t, msgs, name)
	if m == nil {
		t.Fatalf("DomainMessage %q not found in IR", name)
	}
	return m
}

// irFindField finds a DomainField by proto name within a DomainMessage.
func irFindField(t *testing.T, dm *DomainMessage, fieldName string) *DomainField {
	t.Helper()
	for _, f := range dm.Fields {
		if f.Name == fieldName {
			return f
		}
	}
	t.Fatalf("DomainField %q not found in DomainMessage %q", fieldName, dm.Name)
	return nil
}

// irFindEnum searches top-level and nested enums recursively by name.
func irFindEnum(t *testing.T, file *DomainFile, name string) *DomainEnum {
	t.Helper()
	for _, e := range file.Enums {
		if e.Name == name {
			return e
		}
	}
	var searchMsg func([]*DomainMessage) *DomainEnum
	searchMsg = func(msgs []*DomainMessage) *DomainEnum {
		for _, m := range msgs {
			for _, e := range m.NestedEnums {
				if e.Name == name {
					return e
				}
			}
			if found := searchMsg(m.NestedMessages); found != nil {
				return found
			}
		}
		return nil
	}
	if found := searchMsg(file.Messages); found != nil {
		return found
	}
	t.Fatalf("DomainEnum %q not found", name)
	return nil
}

// irFindDomainMessageInPlugin builds the IR for each generated file and returns
// the DomainMessage with the given name. Useful when you have a protogen.Plugin.
func irFindDomainMessageInPlugin(t *testing.T, gen *protogen.Plugin, opts *Options, msgName string) *DomainMessage {
	t.Helper()
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}
		df := BuildDomainFile(f, opts)
		for _, m := range df.Messages {
			if m.Name == msgName {
				return m
			}
		}
	}
	t.Fatalf("DomainMessage %q not found in IR", msgName)
	return nil
}
