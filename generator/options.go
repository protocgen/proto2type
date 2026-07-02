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
}
