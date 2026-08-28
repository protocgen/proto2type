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
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/protocgen/proto2type/generator"
)

type presetFlag struct {
	opts *generator.Options
}

func (p presetFlag) String() string { return p.opts.TSPreset }
func (p presetFlag) Set(val string) error {
	p.opts.TSPreset = val
	switch val {
	case "zod-strict":
		p.opts.TSStrict = true
		p.opts.TSExplicitTypes = true
		p.opts.Validate = "true"
	case "types-only":
		p.opts.TSTypesOnly = true
	}
	return nil
}

func main() {
	var flags flag.FlagSet
	opts := &generator.Options{}

	flags.StringVar(&opts.Lang, "lang", "go", "target language: go, python, kotlin, typescript")
	flags.StringVar(&opts.Backend, "backend", "", "storage backend: firestore, mongo, dynamodb, datastore, spanner, sqlite, buffa, jsonrpc")
	flags.BoolVar(&opts.Domain, "domain", true, "generate domain types + proto converters")
	flags.StringVar(&opts.OutputFile, "output_file", "", "override output filename")
	flags.BoolVar(&opts.EnumAsString, "enum_as_string", false, "store enums as string names")
	flags.BoolVar(&opts.OmitemptyDefault, "omitempty_default", true, "default omitempty for optional fields")
	flags.StringVar(&opts.GoPackage, "go_package", "", "override Go package for generated types (import path;package_name)")
	flags.BoolVar(&opts.RustExhaustive, "rust_exhaustive", false, "generate exhaustive Rust structs (omit #[non_exhaustive])")
	flags.StringVar(&opts.BufModule, "rust_buffa_module", "", "Rust module path for buffa proto types (required for backend=buffa)")
	flags.StringVar(&opts.Validate, "validate", "", "validation strategy: true (default per lang), validator (Rust), native (Kotlin)")
	flags.StringVar(&opts.BufOneofPrefix, "rust_buffa_oneof_prefix", "", "module prefix before oneof submodule (e.g. __buffa for connectrpc-build)")
	flags.StringVar(&opts.DomainModule, "rust_domain_module", "", "Rust module path for domain type imports in buffa output (default: use super::*)")
	flags.StringVar(&opts.PythonBaseClass, "python_base_class", "BaseModel", "Python: custom Pydantic base class")
	flags.StringVar(&opts.PythonAliasGenerator, "python_alias_generator", "", "Python: alias generator (camel)")
	flags.StringVar(&opts.PythonEnumStyle, "python_enum_style", "", "Python: enum style (raw)")
	flags.StringVar(&opts.PythonPreset, "python_preset", "", "Python: preset (a2a)")
	flags.StringVar(&opts.PythonDescription, "python_description", "", "Python: module-level docstring")
	flags.BoolVar(&opts.PythonStripProtoSuffix, "python_strip_proto_suffix", false, "Python: use base.py instead of base_pb2_pydantic.py")
	flags.StringVar(&opts.TSInt64Style, "ts_int64", "string", "TypeScript: int64 representation (string, bigint)")
	flags.StringVar(&opts.TSEnumStyle, "ts_enum_style", "enum", "TypeScript: enum style (enum, native)")
	flags.BoolVar(&opts.TSExplicitTypes, "ts_explicit_types", true, "TypeScript: emit explicit interface types")
	flags.StringVar(&opts.TSZodImport, "ts_zod_import", "zod", "TypeScript: Zod import path")
	flags.BoolVar(&opts.TSTypesOnly, "ts_types_only", false, "TypeScript: emit plain types without Zod")
	flags.BoolVar(&opts.TSStrict, "ts_strict", false, "TypeScript: append .strict() to reject unknown fields")
	flags.StringVar(&opts.TSOutputSuffix, "ts_output_suffix", ".type", "TypeScript: output file suffix (default .type)")
	flags.Var(presetFlag{opts: opts}, "ts_preset", "TypeScript: preset (zod-strict, types-only)")
	flags.BoolVar(&opts.GoConstructor, "go_constructor", true, "generate NewXxx() constructors for types with required fields")
	flags.BoolVar(&opts.Debug, "debug", false, "emit IR debug information to stderr")

	protogen.Options{
		ParamFunc: flags.Set,
	}.Run(func(gen *protogen.Plugin) error {
		gen.SupportedFeatures = uint64(
			pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL,
		)

		if strings.Contains(opts.TSZodImport, "..") || strings.HasPrefix(opts.TSZodImport, "/") {
			return fmt.Errorf("ts_zod_import must not contain path traversal")
		}
		if strings.Contains(opts.OutputFile, "..") || strings.HasPrefix(opts.OutputFile, "/") ||
			(len(opts.OutputFile) >= 2 && opts.OutputFile[1] == ':') || strings.HasPrefix(opts.OutputFile, `\\`) {
			return fmt.Errorf("output_file must not contain path traversal")
		}

		var generatedCount int
		for _, f := range gen.Files {
			if f.Generate {
				generatedCount++
			}
		}
		if opts.OutputFile != "" && generatedCount > 1 {
			return fmt.Errorf("output_file cannot be used with multiple proto files")
		}

		if !opts.Domain && opts.Backend == "" {
			return fmt.Errorf("proto2type: must specify at least one of domain=true or backend=<name>")
		}

		// Instantiate a new runner for this invocation.
		runner := generator.NewRunner()

		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			if err := runner.GenerateFile(gen, f, opts); err != nil {
				return err
			}
		}
		return nil
	})
}
