// proto2type generates native language types and storage structs from Protocol
// Buffer definitions, with support for multiple storage backends.
//
// Usage as a protoc plugin:
//
//	protoc --proto2type_out=. --proto2type_opt=backend=firestore your.proto
//
// Usage with buf:
//
//	# buf.gen.yaml
//	plugins:
//	  - local: protoc-gen-proto2type
//	    out: gen/go
//	    opt:
//	      - backend=firestore
package main

import (
	"flag"
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/protocgen/proto2type/generator"
)

func main() {
	var flags flag.FlagSet
	opts := &generator.Options{}

	flags.StringVar(&opts.Lang, "lang", "go", "target language: go, python, kotlin, typescript")
	flags.StringVar(&opts.Backend, "backend", "", "storage backend: firestore, mongo, dynamodb, datastore, spanner")
	flags.BoolVar(&opts.Domain, "domain", true, "generate domain types + proto converters")
	flags.StringVar(&opts.OutputFile, "output_file", "", "override output filename")
	flags.BoolVar(&opts.EnumAsString, "enum_as_string", false, "store enums as string names")
	flags.BoolVar(&opts.OmitemptyDefault, "omitempty_default", true, "default omitempty for optional fields")
	flags.StringVar(&opts.GoPackage, "go_package", "", "override Go package for generated types (import path;package_name)")
	flags.BoolVar(&opts.RustExhaustive, "rust_exhaustive", false, "generate exhaustive Rust structs (omit #[non_exhaustive])")

	protogen.Options{
		ParamFunc: flags.Set,
	}.Run(func(gen *protogen.Plugin) error {
		gen.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

		if !opts.Domain && opts.Backend == "" {
			return fmt.Errorf("proto2type: must specify at least one of domain=true or backend=<name>")
		}

		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			if err := generator.GenerateFile(gen, f, opts); err != nil {
				return err
			}
		}
		return nil
	})
}
