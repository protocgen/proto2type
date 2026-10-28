package generator

import (
	"fmt"
	"strings"
)

// Options holds the plugin configuration parsed from command-line flags.
type Options struct {
	// Lang is the target language (go, python, kotlin, typescript).
	Lang string

	// Backend is the storage backend (firestore, mongo, dynamodb, datastore, spanner).
	// Empty string means no storage types are generated.
	Backend string

	// PostgresMode controls the Postgres backend behavior.
	// Supports single modes, shorthands, and '+'- or ','-separated composable tokens:
	//   "" or "types" | "tables" | "orm" | "go" | "rust" (default) — struct-per-message (<prefix>_postgres.type.go / .rs)
	//   "none"                     — disabled
	//   "ddl" | "sql"              — PostgreSQL SQL DDL (<prefix>_postgres.sql)
	//   "hcl" | "atlas"            — Atlas PostgreSQL HCL (<prefix>_postgres.hcl)
	//   "both"                     — language storage types + PostgreSQL SQL DDL
	//   "all"                      — language storage types + PostgreSQL SQL DDL + Atlas HCL
	//   "search_index"             — HAPI-style search index extraction functions (FHIR)
	//   "search_index_ddl"         — HAPI-style search index SQL DDL (<prefix>_search_index.sql)
	//   "search_index_hcl"         — HAPI-style search index Atlas HCL (<prefix>_search_index.hcl)
	//   composable examples: "ddl+hcl", "types+hcl", "search_index+ddl+hcl"
	PostgresMode string

	// PostgresSearchParams is the path to a FHIR SearchParameter JSON bundle.
	// Required when PostgresMode includes "search_index", "search_index_ddl", or "search_index_hcl".
	// e.g. "fhir/r4/search-parameters.json"
	PostgresSearchParams string

	// DorisMode controls the Doris/StarRocks/Iceberg lakehouse backend behavior.
	// Supports single modes, shorthands, and '+'- or ','-separated composable tokens:
	//   ""              (default) — Go storage structs + SQL row scanners
	//   "go" | "types"            — Go storage structs + SQL row scanners
	//   "ddl" | "sql"             — Doris & Iceberg SQL DDL (<prefix>_doris.sql, <prefix>_iceberg.sql)
	//   "hcl" | "atlas"           — Atlas Doris & Iceberg HCL (<prefix>_doris.hcl, <prefix>_iceberg.hcl)
	//   "both"                    — Go storage structs + SQL scanners and Doris & Iceberg SQL DDL
	//   "all"                     — all 3 dialects: Go storage structs + SQL scanners, Doris/StarRocks/Iceberg SQL DDL, and Atlas HCL
	//   "starrocks"               — StarRocks SQL DDL and Atlas HCL (<prefix>_starrocks.sql, <prefix>_starrocks.hcl)
	//   "starrocks_both"          — Go storage structs + SQL scanners, StarRocks SQL DDL, and StarRocks Atlas HCL
	//   "starrocks_ddl"           — StarRocks SQL DDL (<prefix>_starrocks.sql)
	//   "starrocks_hcl"           — StarRocks Atlas HCL (<prefix>_starrocks.hcl)
	//   composable examples: "ddl+hcl", "go+hcl", "starrocks_ddl+hcl"
	DorisMode string

	// DorisStorageVault sets the default Apache Doris storage_vault_name property
	// when not explicitly set on (proto2type.message).table.storage_vault_name.
	// When empty, "storage_vault_name" is omitted from Doris CREATE TABLE PROPERTIES.
	DorisStorageVault string

	// IcebergCatalog sets the default Apache Iceberg catalog prefix (<catalog>.<db>.<table>)
	// when not explicitly set on (proto2type.message).table.iceberg_catalog.
	// Defaults to "iceberg" in DDL generation when empty.
	IcebergCatalog string

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

	// Validate selects the validation strategy for code generation.
	// When non-empty, enables validation code generation from buf.validate constraints.
	// When empty (default), no validation code is generated.
	//
	// Supported values ("true" selects the default strategy per language):
	//
	//   Go:     "true" → protovalidate delegation via Validate() method.
	//           "native" → pure Go checks in Validate(), zero external deps.
	//   Rust:   "true" | "validator" → validator crate (#[derive(Validate)]).
	//   Kotlin: "true" | "native"   → hand-rolled validate(): List<String>.
	//   Python: constraints are always emitted via Pydantic Field() args.
	//
	// Oneof mutual-exclusion checks (Go) are always generated regardless of this flag.
	//
	// NOTE: length validation counts characters (Unicode scalar values), not bytes.
	// This differs from proto's min_len/max_len which count bytes, but matches
	// user expectations and is consistent with Python/Pydantic.
	Validate string

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

	// TSInt64Style controls int64/uint64 representation in TypeScript:
	// "string" (default, JSON-safe) or "bigint".
	TSInt64Style string

	// TSEnumStyle controls enum representation in TypeScript:
	// "enum" (default, z.enum) or "native" (z.nativeEnum with const object).
	TSEnumStyle string

	// TSExplicitTypes emits explicit export interface alongside z.infer
	// for improved IDE performance in large codebases.
	TSExplicitTypes bool

	// TSZodImport controls the zod import path (default "zod").
	TSZodImport string

	// TSTypesOnly emits plain TypeScript interfaces and type aliases without
	// Zod schemas or runtime dependencies. Incompatible with validate.
	TSTypesOnly bool

	// TSStrict appends .strict() to all generated Zod object schemas,
	// rejecting unknown fields per ProtoJSON spec. Default: false.
	TSStrict bool

	// TSPreset applies preset configurations for TypeScript.
	TSPreset string

	// TSOutputSuffix overrides the default TypeScript output file extension.
	TSOutputSuffix string

	// GoConstructor generates NewXxx() constructor functions for domain types
	// that have required fields (google.api.field_behavior=REQUIRED or
	// buf.validate.field.required=true). OUTPUT_ONLY fields are excluded.
	// Default: true.
	GoConstructor bool

	// Debug emits IR debug information to stderr when set.
	Debug bool
}

// ValidateEnabled returns true if any validation strategy is configured.
func (o *Options) ValidateEnabled() bool {
	return o.Validate != ""
}

// ValidateNative returns true if the native (zero-dependency) validation strategy is selected.
func (o *Options) ValidateNative() bool {
	return o.Validate == "native"
}

// PostgresModeTargets describes the resolved output targets for postgres_mode.
type PostgresModeTargets struct {
	EmitTypes          bool
	EmitDDL            bool
	EmitHCL            bool
	EmitSearchIndexGo  bool
	EmitSearchIndexDDL bool
	EmitSearchIndexHCL bool
}

// NeedsSearchParams reports whether the resolved postgres_mode targets require
// postgres_search_params to be configured.
func (t PostgresModeTargets) NeedsSearchParams() bool {
	return t.EmitSearchIndexGo || t.EmitSearchIndexDDL || t.EmitSearchIndexHCL
}

// ParsePostgresMode parses o.PostgresMode into its composable output targets.
// Empty string defaults to EmitTypes: true. Multiple tokens may be combined
// using '+' or ',' (e.g. "ddl+hcl", "types+hcl", "search_index+ddl+hcl").
func (o *Options) ParsePostgresMode() (PostgresModeTargets, error) {
	raw := strings.TrimSpace(o.PostgresMode)
	if raw == "" {
		return PostgresModeTargets{EmitTypes: true}, nil
	}
	if raw == "none" {
		return PostgresModeTargets{}, nil
	}
	tokens := splitModeTokens(raw)
	if len(tokens) == 0 {
		return PostgresModeTargets{}, fmt.Errorf("unknown postgres_mode: %q (must be '', 'none', 'orm', 'tables', 'types', 'go', 'rust', 'ddl', 'hcl', 'both', 'all', 'search_index', 'search_index_ddl', 'search_index_hcl', or a '+'-separated combination)", o.PostgresMode)
	}
	var targets PostgresModeTargets
	var hasSearchIndex bool
	for _, tok := range tokens {
		switch tok {
		case "none":
			return PostgresModeTargets{}, fmt.Errorf("unknown postgres_mode: %q ('none' cannot be combined with other mode tokens)", o.PostgresMode)
		case "tables", "types", "orm", "go", "rust":
			targets.EmitTypes = true
		case "ddl", "sql":
			targets.EmitDDL = true
		case "hcl", "atlas":
			targets.EmitHCL = true
		case "both":
			targets.EmitTypes = true
			targets.EmitDDL = true
		case "all":
			targets.EmitTypes = true
			targets.EmitDDL = true
			targets.EmitHCL = true
		case "search_index":
			targets.EmitSearchIndexGo = true
			hasSearchIndex = true
		case "search_index_ddl":
			targets.EmitSearchIndexDDL = true
		case "search_index_hcl":
			targets.EmitSearchIndexHCL = true
		default:
			return PostgresModeTargets{}, fmt.Errorf("unknown postgres_mode: %q (unknown token %q; valid tokens: 'none', 'orm', 'tables', 'types', 'go', 'rust', 'ddl', 'hcl', 'both', 'all', 'search_index', 'search_index_ddl', 'search_index_hcl')", o.PostgresMode, tok)
		}
	}
	// When "search_index" is combined with "ddl" or "hcl" (e.g., "search_index+ddl",
	// "search_index+hcl", "types+search_index+hcl", "all+search_index"), also emit
	// the corresponding search_index schema files; when table types were not requested,
	// route DDL/HCL exclusively to the search_index schema files.
	if hasSearchIndex {
		if targets.EmitDDL {
			targets.EmitSearchIndexDDL = true
			if !targets.EmitTypes {
				targets.EmitDDL = false
			}
		}
		if targets.EmitHCL {
			targets.EmitSearchIndexHCL = true
			if !targets.EmitTypes {
				targets.EmitHCL = false
			}
		}
	}
	return targets, nil
}

// DorisModeTargets describes the resolved output targets for doris_mode.
type DorisModeTargets struct {
	EmitGo           bool
	EmitDorisDDL     bool
	EmitDorisHCL     bool
	EmitStarRocksDDL bool
	EmitStarRocksHCL bool
}

// ParseDorisMode parses o.DorisMode (and o.Backend) into its composable output targets.
// Multiple tokens may be combined using '+' or ',' (e.g. "ddl+hcl", "go+hcl", "starrocks_ddl+hcl").
func (o *Options) ParseDorisMode() (DorisModeTargets, error) {
	raw := strings.TrimSpace(o.DorisMode)
	if raw == "" {
		if o.Backend == "starrocks" {
			return DorisModeTargets{
				EmitGo:           true,
				EmitStarRocksDDL: true,
				EmitStarRocksHCL: true,
			}, nil
		}
		return DorisModeTargets{EmitGo: true}, nil
	}
	tokens := splitModeTokens(raw)
	if len(tokens) == 0 {
		return DorisModeTargets{}, fmt.Errorf("unknown doris_mode: %q (must be '', 'go', 'types', 'ddl', 'hcl', 'both', 'all', 'starrocks', 'starrocks_both', 'starrocks_ddl', 'starrocks_hcl', or a '+'-separated combination)", o.DorisMode)
	}
	var targets DorisModeTargets
	for _, tok := range tokens {
		switch tok {
		case "go", "types":
			targets.EmitGo = true
		case "ddl", "sql":
			targets.EmitDorisDDL = true
		case "hcl", "atlas":
			targets.EmitDorisHCL = true
		case "both":
			targets.EmitGo = true
			targets.EmitDorisDDL = true
		case "all":
			targets.EmitGo = true
			targets.EmitDorisDDL = true
			targets.EmitDorisHCL = true
			targets.EmitStarRocksDDL = true
			targets.EmitStarRocksHCL = true
		case "starrocks":
			targets.EmitStarRocksDDL = true
			targets.EmitStarRocksHCL = true
		case "starrocks_both":
			targets.EmitGo = true
			targets.EmitStarRocksDDL = true
			targets.EmitStarRocksHCL = true
		case "starrocks_ddl":
			targets.EmitStarRocksDDL = true
		case "starrocks_hcl":
			targets.EmitStarRocksHCL = true
		default:
			return DorisModeTargets{}, fmt.Errorf("unknown doris_mode: %q (unknown token %q; valid tokens: 'go', 'types', 'ddl', 'hcl', 'both', 'all', 'starrocks', 'starrocks_both', 'starrocks_ddl', 'starrocks_hcl')", o.DorisMode, tok)
		}
	}
	return targets, nil
}

func splitModeTokens(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '+' || r == ','
	})
	var out []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// TableMetaConfig is an alias for DorisTableMetaIR, representing table-level metadata
// extracted from (proto2type.message).table (extension 1307) or legacy table_meta (extension 50201).
type TableMetaConfig = DorisTableMetaIR

// ColumnConfig is an alias for DorisColumnMetaIR, representing column-level metadata
// extracted from (proto2type.field).column (extension 1307) or legacy column_meta (extension 50200).
type ColumnConfig = DorisColumnMetaIR
