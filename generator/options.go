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
}
