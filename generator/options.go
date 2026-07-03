package generator

// Options holds the plugin configuration parsed from command-line flags.
type Options struct {
	// Lang is the target language (go, python, kotlin, typescript).
	Lang string

	// Backend is the storage backend (firestore, mongo, dynamodb, datastore, spanner).
	// Empty string means no storage types are generated.
	Backend string

	// Domain controls whether domain types and proto converters are generated.
	Domain bool

	// OutputFile overrides the default output filename.
	OutputFile string

	// EnumAsString stores enums as string names instead of int32.
	EnumAsString bool

	// OmitemptyDefault controls whether optional/zero-value fields get omitempty by default.
	OmitemptyDefault bool

	// GoPackage overrides the Go package name for generated types.
	// When set, generated types use this as their Go import path and the converters
	// import the proto types from the original go_package in the .proto file.
	GoPackage string

	// RustExhaustive controls whether Rust structs are generated as exhaustive (omitting #[non_exhaustive]).
	// Default: false. Set to true for vendored codegen where the consumer owns the types.
	RustExhaustive bool

	// BufModule is the Rust module path where buffa-generated proto types live.
	// Required for backend=buffa (e.g. "crate::proto::candela::harness::v1").
	BufModule string

	// Validate enables Validate() method generation using protovalidate.
	// When true, the generated Validate() delegates to protovalidate.Validate(d.ToProto())
	// for buf.validate constraint checking. Oneof mutual-exclusion checks are always
	// generated regardless of this flag.
	Validate bool

	// BufOneofPrefix is an optional module prefix inserted between the buffa module
	// and the "oneof" submodule in generated Rust code.
	// When empty (default), oneof paths are: __buffa_mod::oneof::<msg>::<Variant>
	// When set (e.g. "__buffa"), paths become: __buffa_mod::__buffa::oneof::<msg>::<Variant>
	// connectrpc-build uses "__buffa" as its prefix.
	BufOneofPrefix string

	// DomainModule is an optional Rust module path for domain type imports in buffa output.
	// When empty (default), generates: use super::*;
	// When set (e.g. "candela_core::harness"), generates: use candela_core::harness::*;
	// This allows generated buffa converters to be placed in any module, not just
	// as a sibling of the domain type definitions.
	DomainModule string

	// PythonBaseClass overrides the Pydantic base class (default: "BaseModel").
	PythonBaseClass string

	// PythonAliasGenerator adds model_config with alias generation ("camel" for to_camel).
	PythonAliasGenerator string

	// PythonEnumStyle controls enum generation: "" (default, prefix-stripped lowercase)
	// or "raw" (original proto names with UNSPECIFIED).
	PythonEnumStyle string

	// PythonPreset applies preset configurations ("a2a" sets alias_generator=camel + enum_style=raw).
	PythonPreset string

	// PythonDescription overrides module-level docstring.
	PythonDescription string

	// PythonStripProtoSuffix uses base.py instead of base_pb2_pydantic.py (Python only).
	PythonStripProtoSuffix bool
}
