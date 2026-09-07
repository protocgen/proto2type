package generator

import (
	"reflect"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

func FuzzBuildDomainFile(f *testing.F) {
	// Seed 1: Empty file
	req1 := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"test.proto"},
		ProtoFile: []*descriptorpb.FileDescriptorProto{
			{
				Name:    proto.String("test.proto"),
				Syntax:  proto.String("proto3"),
				Package: proto.String("test"),
			},
		},
	}
	b1, _ := proto.Marshal(req1)
	f.Add(b1)

	// Seed 2: Simple message with scalars
	req2 := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"test.proto"},
		ProtoFile: []*descriptorpb.FileDescriptorProto{
			{
				Name:    proto.String("test.proto"),
				Syntax:  proto.String("proto3"),
				Package: proto.String("test"),
				MessageType: []*descriptorpb.DescriptorProto{
					{
						Name: proto.String("SimpleMessage"),
						Field: []*descriptorpb.FieldDescriptorProto{
							{
								Name:   proto.String("id"),
								Number: proto.Int32(1),
								Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							},
						},
					},
				},
			},
		},
	}
	b2, _ := proto.Marshal(req2)
	f.Add(b2)

	// Seed 3: Message with nested message, enums, maps, oneofs, repeated fields
	req3 := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"test.proto"},
		ProtoFile: []*descriptorpb.FileDescriptorProto{
			{
				Name:    proto.String("test.proto"),
				Syntax:  proto.String("proto3"),
				Package: proto.String("test"),
				MessageType: []*descriptorpb.DescriptorProto{
					{
						Name: proto.String("ComplexMessage"),
						Field: []*descriptorpb.FieldDescriptorProto{
							{
								Name:   proto.String("repeated_field"),
								Number: proto.Int32(1),
								Label:  descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
								Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							},
						},
						NestedType: []*descriptorpb.DescriptorProto{
							{
								Name: proto.String("NestedMessage"),
							},
						},
						EnumType: []*descriptorpb.EnumDescriptorProto{
							{
								Name: proto.String("NestedEnum"),
								Value: []*descriptorpb.EnumValueDescriptorProto{
									{
										Name:   proto.String("UNSPECIFIED"),
										Number: proto.Int32(0),
									},
								},
							},
						},
						OneofDecl: []*descriptorpb.OneofDescriptorProto{
							{
								Name: proto.String("my_oneof"),
							},
						},
					},
				},
			},
		},
	}
	b3, _ := proto.Marshal(req3)
	f.Add(b3)

	f.Fuzz(func(t *testing.T, data []byte) {
		req := &pluginpb.CodeGeneratorRequest{}
		if err := proto.Unmarshal(data, req); err != nil {
			return
		}

		plugin, err := protogen.Options{}.New(req)
		if err != nil {
			return
		}

		opts := &Options{} // Default options

		for _, file := range plugin.Files {
			if !file.Generate {
				continue
			}

			// Build IR
			df, err := BuildDomainFile(file, opts)
			if err != nil {
				continue // errors are acceptable (e.g. invalid configurations)
			}

			if df == nil {
				continue
			}

			// Assert determinism: calling BuildDomainFile twice with same input produces equal results
			df2, err2 := BuildDomainFile(file, opts)
			if err2 != nil {
				t.Fatalf("BuildDomainFile returned error on second call but not first: %v", err2)
			}

			if !reflect.DeepEqual(df, df2) {
				t.Errorf("BuildDomainFile is not deterministic")
			}

			// Assert structural invariants
			for _, msg := range df.Messages {
				checkMessageInvariants(t, msg)
			}
			for _, enum := range df.Enums {
				if enum.Name == "" {
					t.Errorf("Empty enum name found")
				}
			}
		}
	})
}

func checkMessageInvariants(t *testing.T, msg *DomainMessage) {
	if msg.Name == "" {
		t.Errorf("Empty message name found: %s", msg.FullName)
	}

	oneofs := make(map[string]bool)
	for _, o := range msg.Oneofs {
		oneofs[o.Name] = true
	}

	for _, f := range msg.Fields {
		if (f.Repeated || f.IsMap) && f.NeedsBox {
			t.Errorf("NeedsBox set on repeated or map field %s", f.Name)
		}
		if f.IsOneof {
			if !oneofs[f.OneofTypeName] {
				t.Errorf("Oneof placeholder %s references non-existent oneof %s", f.Name, f.OneofTypeName)
			}
		}
	}

	for _, e := range msg.NestedEnums {
		if e.Name == "" {
			t.Errorf("Empty nested enum name found in %s", msg.Name)
		}
	}

	for _, child := range msg.NestedMessages {
		checkMessageInvariants(t, child)
	}
}
