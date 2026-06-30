# proto2type

[![CI](https://github.com/protocgen/proto2type/actions/workflows/ci.yml/badge.svg)](https://github.com/protocgen/proto2type/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/protocgen/proto2type)](https://goreportcard.com/report/github.com/protocgen/proto2type)

A `protoc`/`buf` plugin that generates native language types, storage structs, and bidirectional converters from Protocol Buffer definitions.

## Why this exists

Every service that uses Protocol Buffers hits the same 3-layer problem:

```
Proto messages  ←→  Domain types  ←→  Storage structs
(wire format)       (business logic)   (database layer)
```

You define your data once in `.proto` files, then maintain **parallel structs by hand** — domain types with `json:""` tags, Firestore types with `firestore:""` tags, MongoDB types with `bson:""` tags — plus the converter boilerplate between them. Fields drift. Tags get stale. A new field in the proto gets added to the domain struct but someone forgets the storage struct. Bugs compound silently.

**proto2type eliminates this.** Define your data once in proto. The plugin generates all three layers — domain types, storage structs, and converters — from a single source of truth.

## Features

- 🏗️ **Domain types** — clean native structs with `json:""` tags, `time.Time` instead of `timestamppb.Timestamp`
- 🔥 **Firestore backend** — `firestore:""` tags, `serverTimestamp` sentinel, document ID exclusion
- 🍃 **MongoDB backend** — `bson:""` tags, `_id` handling, `,inline` support
- 🔄 **Bidirectional converters** — `ToProto()` / `FromProto()`, `ToDomain()` / `FromDomain()` on every struct
- 🎯 **Field mask helpers** — `ApplyFieldMask()` for partial updates
- 📋 **Custom proto options** — `document_id`, `server_timestamp`, `skip`, `omitempty`, `inline`, `name`
- 🗄️ **SQLite backend (Rust)** — `Row` structs with `to_domain()` / `from_domain()`, JSON-serialised nested fields
- 🔌 **Works without a database** — generate domain types only, no backend required
- 🌐 **Multi-language** — Go, Rust, and Kotlin supported, Python / TypeScript planned

## Install

```bash
go install github.com/protocgen/proto2type@latest
```

This installs the `protoc-gen-proto2type` binary.

## Usage

### With buf

**Domain types only** (no backend):

```yaml
# buf.gen.yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: gen/go
    opt:
      - lang=go
```

**Domain + Firestore storage**:

```yaml
# buf.gen.yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: gen/go
    opt:
      - lang=go
      - backend=firestore
```

**Storage only** (skip domain types):

```yaml
# buf.gen.yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: gen/go
    opt:
      - lang=go
      - domain=false
      - backend=mongo
```

Then run:

```bash
buf generate
```

### Kotlin

**Domain types** (`@Serializable` data classes):

```yaml
# buf.gen.kotlin.yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: gen/kotlin
    opt:
      - lang=kotlin
```

### Rust

**Domain types** (serde-annotated structs):

```yaml
# buf.gen.rust.yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: gen/rust
    opt:
      - lang=rust
```

**Domain + SQLite storage**:

```yaml
# buf.gen.rust.yaml
version: v2
plugins:
  # Domain types
  - local: protoc-gen-proto2type
    out: gen/rust
    opt:
      - lang=rust

  # SQLite Row structs
  - local: protoc-gen-proto2type
    out: gen/rust
    opt:
      - lang=rust
      - backend=sqlite
      - domain=false
```

### With protoc

```bash
protoc --proto2type_out=./gen/go \
       --proto2type_opt=backend=firestore \
       your_service.proto
```

## Options

All options are passed via `--proto2type_opt=` (protoc) or `opt:` (buf).

See [CONFIG.md](CONFIG.md) for the full reference, including proto-level annotation options.

| Option | Default | Description |
|---|---|---|
| `lang` | `go` | Target language (`go`, `rust`, `kotlin` supported; `python`, `typescript` planned) |
| `backend` | _(none)_ | Storage backend (`firestore`, `mongo`, `sqlite`, `dynamodb`, `datastore`, `spanner`) |
| `domain` | `true` | Generate domain types + proto converters |
| `output_file` | _(auto)_ | Override output filename |
| `enum_as_string` | `false` | Store enums as string names instead of `int32` |
| `omitempty_default` | `true` | Default `omitempty` for optional / zero-value fields |

## Example

Given this proto:

```protobuf
// catalog.proto
syntax = "proto3";
package test.v1;

import "google/protobuf/timestamp.proto";

message ModelCatalogEntry {
  string model_id = 1;
  string provider = 2;
  string display_name = 3;
  double input_per_million = 4;
  double output_per_million = 5;
  bool enabled = 6;
  string category = 7;
  int64 context_window = 8;
  double discount_percent = 9;
  repeated string aliases = 12;
  string provider_model_id = 14;
  google.protobuf.Timestamp created_at = 13;
  google.protobuf.Timestamp updated_at = 15;
  string notes = 16;
  string region = 17;
}
```

### Generated domain struct (`catalog.type.go`)

```go
// Code generated by proto2type. DO NOT EDIT.
package catalog

import "time"

type ModelCatalogEntry struct {
	ModelID          string    `json:"model_id"`
	Provider         string    `json:"provider"`
	DisplayName      string    `json:"display_name"`
	InputPerMillion  float64   `json:"input_per_million"`
	OutputPerMillion float64   `json:"output_per_million"`
	Enabled          bool      `json:"enabled"`
	Category         string    `json:"category"`
	ContextWindow    int64     `json:"context_window"`
	DiscountPercent  float64   `json:"discount_percent"`
	Aliases          []string  `json:"aliases,omitempty"`
	ProviderModelID  string    `json:"provider_model_id"`
	CreatedAt        time.Time `json:"created_at,omitempty"`
	UpdatedAt        time.Time `json:"updated_at,omitempty"`
	Notes            string    `json:"notes"`
	Region           string    `json:"region"`
}

func (d *ModelCatalogEntry) ToProto() *catalogpb.ModelCatalogEntry { ... }
func (d *ModelCatalogEntry) FromProto(pb *catalogpb.ModelCatalogEntry) { ... }
```

### Generated Firestore struct (`catalog_firestore.type.go`)

```go
// Code generated by proto2type. DO NOT EDIT.
// backend: firestore
package catalog

import "time"

type ModelCatalogEntryFirestore struct {
	ModelID          string    `firestore:"model_id"`
	Provider         string    `firestore:"provider"`
	DisplayName      string    `firestore:"display_name"`
	InputPerMillion  float64   `firestore:"input_per_million"`
	OutputPerMillion float64   `firestore:"output_per_million"`
	Enabled          bool      `firestore:"enabled"`
	Category         string    `firestore:"category"`
	ContextWindow    int64     `firestore:"context_window"`
	DiscountPercent  float64   `firestore:"discount_percent"`
	Aliases          []string  `firestore:"aliases,omitempty"`
	ProviderModelID  string    `firestore:"provider_model_id"`
	CreatedAt        time.Time `firestore:"created_at,omitempty"`
	UpdatedAt        time.Time `firestore:"updated_at,omitempty"`
	Notes            string    `firestore:"notes"`
	Region           string    `firestore:"region"`
}

func (d *ModelCatalogEntryFirestore) ToProto() *catalogpb.ModelCatalogEntry { ... }
func (d *ModelCatalogEntryFirestore) FromProto(pb *catalogpb.ModelCatalogEntry) { ... }
```

## Proto Options

Annotate your `.proto` files with `proto2type` options to control generation per-field or per-message:

```protobuf
import "proto2type/options.proto";

message User {
  string id = 1 [(proto2type.field).document_id = true];
  string email = 2;
  google.protobuf.Timestamp created_at = 3 [(proto2type.field).server_timestamp = true];
  string internal_notes = 4 [(proto2type.field).skip = true];
  Address address = 5 [(proto2type.field).inline = true];
  string display_name = 6 [(proto2type.field).name = "name"];
}
```

| Option | Type | Description |
|---|---|---|
| `(proto2type.field).document_id` | `bool` | Mark as document ID — Firestore excludes it (ID is doc path), Mongo maps to `_id` |
| `(proto2type.field).server_timestamp` | `bool` | Server-managed timestamp — Firestore uses `serverTimestamp` sentinel |
| `(proto2type.field).skip` | `bool` | Exclude field from all generated types |
| `(proto2type.field).omitempty` | `OptionalBool` | Force `omitempty` on (`TRUE`) or off (`FALSE`) |
| `(proto2type.field).inline` | `bool` | Flatten nested message into parent — Mongo: `bson:",inline"` |
| `(proto2type.field).name` | `string` | Override the storage field name |
| `(proto2type.message).skip` | `bool` | Skip generating types for entire message |

## Type Mapping

| Proto Type | Go Domain Type |
|---|---|
| `string` | `string` |
| `int32`, `sint32`, `sfixed32` | `int32` |
| `int64`, `sint64`, `sfixed64` | `int64` |
| `uint32`, `fixed32` | `uint32` |
| `uint64`, `fixed64` | `uint64` |
| `float` | `float32` |
| `double` | `float64` |
| `bool` | `bool` |
| `bytes` | `[]byte` |
| `repeated T` | `[]T` |
| `map<K, V>` | `map[K]V` |
| `optional T` (scalar) | `*T` (pointer) |
| `google.protobuf.Timestamp` | `time.Time` |
| `optional google.protobuf.Timestamp` | `*time.Time` |
| `google.protobuf.Duration` | `time.Duration` |
| `optional google.protobuf.Duration` | `*time.Duration` |
| Nested message | `*MessageType` |
| Enum | `int32` (default) or `string` (`enum_as_string=true`) |

### Rust Type Mapping

| Proto Type | Rust Domain Type | SQLite Row Type |
|---|---|---|
| `string` | `String` | `String` |
| `int32`, `sint32`, `sfixed32` | `i32` | `i32` |
| `int64`, `sint64`, `sfixed64` | `i64` | `i64` |
| `uint32`, `fixed32` | `u32` | `u32` |
| `uint64`, `fixed64` | `u64` | `u64` |
| `float` | `f32` | `f32` |
| `double` | `f64` | `f64` |
| `bool` | `bool` | `bool` |
| `bytes` | `Vec<u8>` | `Vec<u8>` |
| `repeated T` | `Vec<T>` | `String` (JSON) |
| `map<K, V>` | `HashMap<K, V>` | `String` (JSON) |
| `optional T` | `Option<T>` | `Option<T>` |
| `google.protobuf.Timestamp` | `DateTime<Utc>` | `i64` (epoch ms) |
| `optional google.protobuf.Timestamp` | `Option<DateTime<Utc>>` | `Option<i64>` |
| `google.protobuf.Duration` | `chrono::Duration` | `i64` (milliseconds) |
| `optional google.protobuf.Duration` | `Option<chrono::Duration>` | `Option<i64>` |
| Nested message | `Option<Box<T>>` | `String` (JSON) |
| Enum | `i32` (default) or `String` (`enum_as_string=true`) | `i32` / `String` |

### Kotlin Type Mapping

| Proto Type | Kotlin Domain Type |
|---|---|
| `string` | `String` |
| `int32`, `sint32`, `sfixed32` | `Int` |
| `int64`, `sint64`, `sfixed64` | `Long` |
| `uint32`, `fixed32` | `Int` |
| `uint64`, `fixed64` | `Long` |
| `float` | `Float` |
| `double` | `Double` |
| `bool` | `Boolean` |
| `bytes` | `ByteArray` |
| `repeated T` | `List<T>` |
| `map<K, V>` | `Map<K, V>` |
| `optional T` | `T?` (nullable) |
| `google.protobuf.Timestamp` | `kotlinx.datetime.Instant` |
| `optional google.protobuf.Timestamp` | `Instant?` |
| `google.protobuf.Duration` | `kotlin.time.Duration` |
| `optional google.protobuf.Duration` | `Duration?` |
| Nested message | `MessageType?` |
| Enum | `@Serializable enum class` |

## Roadmap

| Phase | Scope | Status |
|---|---|---|
| **1** | Go + Firestore + MongoDB | ✅ Done |
| **1.5** | Rust + SQLite, Kotlin (domain-only) | ✅ Done |
| **2** | Python (absorbs [proto2pydantic](https://github.com/protocgen/proto2pydantic)) | Planned |
| **3** | DynamoDB + Datastore | Planned |
| **4** | Spanner + TypeScript + SQL ORMs | Planned |

## Development

This project uses [Nix](https://nixos.org) for reproducible development environments.

```bash
# Enter the dev shell (provides go, buf, protoc, pre-commit)
nix develop

# Run tests
nix develop -c go test ./...

# Regenerate golden files
nix develop -c go test ./... -update

# Build the plugin
nix develop -c go build -o protoc-gen-proto2type .

# Generate from test protos (Go)
cd testdata/proto && nix develop -c buf generate

# Generate from test protos (Rust)
cd testdata/proto && nix develop -c buf generate --template buf.gen.rust.yaml

# Generate from test protos (Kotlin)
cd testdata/proto && nix develop -c buf generate --template buf.gen.kotlin.yaml
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, PR process, and commit signing requirements.

## License

Apache-2.0
