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
- 🐍 **Python/Pydantic backend** — Pydantic `BaseModel` classes with `Field()` validation, `google.api.field_behavior` and `buf/validate` support
- ✅ **Validation** — `buf.validate` constraint checking: Rust via `validator` crate, Kotlin via native `validate()`, Python via Pydantic `Field()`, TypeScript via Zod chains
- 🌐 **Multi-language** — Go, Rust, Python, Kotlin, and TypeScript supported

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

### Python

**Pydantic models** (absorbs [proto2pydantic](https://github.com/protocgen/proto2pydantic)):

```yaml
# buf.gen.yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: gen/python
    opt:
      - lang=python
```

**A2A preset** (camelCase aliases + raw enum names for A2A/ProtoJSON compatibility):

```yaml
# buf.gen.yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: gen/python
    opt:
      - lang=python
      - preset=a2a
```

> **Note:** The standalone `proto2pydantic` tool has been absorbed into `proto2type`. Use `lang=python` going forward.

### Kotlin

**Serializable data classes** (kotlinx.serialization + kotlinx.datetime):

```yaml
# buf.gen.kotlin.yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: gen/kotlin
    opt:
      - lang=kotlin
      - validate=true
```

### TypeScript

**Zod schemas + inferred types** (runtime validation out of the box):

```yaml
# buf.gen.yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: gen/ts
    opt:
      - lang=typescript
      - validate=true
```

**BigInt mode** (native `bigint` for int64 instead of string):

```yaml
plugins:
  - local: protoc-gen-proto2type
    out: gen/ts
    opt:
      - lang=typescript
      - ts_int64=bigint
```

**Explicit TypeScript interfaces** (emitted by default alongside Zod schemas):

```yaml
plugins:
  - local: protoc-gen-proto2type
    out: gen/ts
    opt:
      - lang=typescript
      - ts_explicit_types=true
      - ts_enum_style=native
```

Generated output looks like:

```typescript
import { z } from "zod";

export interface User {
  id: string;
  email: string;
  displayName: string;
  active: boolean;
  address?: Address;
  createdAt?: string;
}

export const UserSchema: z.ZodType<User> = z.object({
  id: z.string().default(""),
  email: z.string().default(""),
  displayName: z.string().default(""),
  active: z.boolean().default(false),
  address: AddressSchema.optional(),
  createdAt: z.string().datetime({ offset: true }).optional(),
});
```

#### TypeScript Options

| Option | Default | Description |
|---|---|---|
| `ts_types_only` | `false` | Emit plain TypeScript types without Zod (zero dependencies) |
| `ts_int64` | `string` | Int64 representation: `string` (safe) or `bigint` (native) |
| `ts_enum_style` | `enum` | Enum style: `enum` (open `z.enum().or(z.string())`) or `native` (`z.nativeEnum()`) |
| `ts_explicit_types` | `true` | Emit explicit `interface` types alongside Zod schemas |
| `ts_zod_import` | `zod` | Zod import path (e.g. `zod/v4` or `@scope/zod`) |

#### Choose Your TS Mode

| | Types Only | Full Zod |
|---|---|---|
| **Dependencies** | 📦 Zero | 🛡️ `zod` peer dep |
| **Bundle impact** | ⚡ 0 KB | ~14 KB min+gzip |
| **Use case** | UI components, SDKs, shared packages | API routes, form validation, ingestion |
| **Config** | `ts_types_only=true` | _(default)_ or `validate=true` |

> **Migration**: switching from types-only to full Zod is a zero-diff upgrade — all `interface` and `type` definitions are structurally identical to `z.infer<typeof Schema>`.

Generates `@Serializable` data classes with proper WKT mappings, sealed class oneofs, and — when `validate=true` — native `validate()` / `validateOrThrow()` extension functions from `buf.validate` constraints.

### Validation

When `validate=true` (or a language-specific strategy), `proto2type` reads `buf.validate` constraints from your protos and generates native validation code.

Given this proto:

```protobuf
syntax = "proto3";
package test.v1;

import "buf/validate/validate.proto";

message User {
  string email = 1 [(buf.validate.field).string.email = true];
  string display_name = 2 [(buf.validate.field).string.min_len = 1];
  int32 age = 3 [(buf.validate.field).int32 = { gte: 0, lte: 150 }];
}
```

**Kotlin** (`validate=true`) generates:

```kotlin
fun User.validate(): List<String> {
    val errors = mutableListOf<String>()
    if (email.isNotEmpty() && !email.matches(Regex(...))) errors.add("email must be a valid email")
    if (displayName.length < 1) errors.add("display_name must be at least 1 characters")
    if (age < 0) errors.add("age must be >= 0")
    if (age > 150) errors.add("age must be <= 150")
    return errors
}

fun User.validateOrThrow() {
    val errors = validate()
    if (errors.isNotEmpty()) throw IllegalArgumentException(errors.joinToString("; "))
}
```

**Rust** (`validate=true`) generates:

```rust
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize, Validate)]
pub struct User {
    #[validate(email)]
    pub email: String,
    #[validate(length(min = 1))]
    pub display_name: String,
    #[validate(range(min = 0, max = 150))]
    pub age: i32,
}
```

**Python** maps constraints automatically to Pydantic `Field()` kwargs (no `validate` flag needed):

```python
class User(BaseModel):
    email: str = Field(..., pattern=r"^[^@]+@[^@]+$")
    display_name: str = Field(..., min_length=1)
    age: int = Field(..., ge=0, le=150)
```

#### Field Behavior & Validation

`proto2type` reads `google.api.field_behavior` annotations and `buf/validate` constraints from your protos and maps them to Pydantic `Field()` kwargs:

| Proto Annotation | Pydantic Effect |
|---|---|
| `REQUIRED` (`google.api.field_behavior`) | `Field(...)` — no default, field is mandatory |
| `OUTPUT_ONLY` (`google.api.field_behavior`) | `Field(exclude=True)` — excluded from serialization |
| `buf/validate` string rules | `Field(min_length=..., max_length=..., pattern=...)` |
| `buf/validate` numeric rules | `Field(ge=..., le=..., gt=..., lt=...)` |

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
| `lang` | `go` | Target language (`go`, `rust`, `python`, `kotlin`, `typescript`) |
| `backend` | _(none)_ | Storage backend (`firestore`, `mongo`, `sqlite`, `dynamodb`, `datastore`, `spanner`) |
| `domain` | `true` | Generate domain types + proto converters |
| `output_file` | _(auto)_ | Override output filename |
| `enum_as_string` | `false` | Store enums as string names instead of `int32` |
| `omitempty_default` | `true` | Default `omitempty` for optional / zero-value fields |
| `validate` | `""` | Validation strategy from `buf.validate` constraints (`true`, `validator`, `native`) |

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
| `optional T` | `T` (with `omitempty`) |
| `google.protobuf.Timestamp` | `time.Time` |
| `google.protobuf.Duration` | `time.Duration` |
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
| `google.protobuf.Duration` | `chrono::Duration` | `i64` (milliseconds) |
| Nested message | `Option<Box<T>>` | `String` (JSON) |
| Enum | `i32` (default) or `String` (`enum_as_string=true`) | `i32` / `String` |

### Kotlin Type Mapping

| Proto Type | Kotlin Domain Type |
|---|---|
| `string` | `String` |
| `int32`, `sint32`, `sfixed32` | `Int` |
| `int64`, `sint64`, `sfixed64` | `Long` |
| `uint32`, `fixed32` | `UInt` |
| `uint64`, `fixed64` | `ULong` |
| `float` | `Float` |
| `double` | `Double` |
| `bool` | `Boolean` |
| `bytes` | `ByteArray` |
| `repeated T` | `List<T>` (default `emptyList()`) |
| `map<K, V>` | `Map<K, V>` (default `emptyMap()`) |
| `optional T` | `T?` (default `null`) |
| `google.protobuf.Timestamp` | `kotlinx.datetime.Instant` |
| `google.protobuf.Duration` | `kotlin.time.Duration` |
| Nested message | `T?` (nullable) |
| Enum | `@Serializable enum class` with `@SerialName` and `fromValue(Int)` companion |
| Oneof | `@Serializable sealed class` with data class variants |

### TypeScript Type Mapping

| Proto Type | Zod Schema | TypeScript Type |
|---|---|---|
| `bool` | `z.boolean()` | `boolean` |
| `int32`, `sint32`, `sfixed32` | `z.number().int()` | `number` |
| `uint32`, `fixed32` | `z.number().int().nonnegative()` | `number` |
| `int64`, `sint64` (string mode) | `z.string()` | `string` |
| `int64`, `sint64` (bigint mode) | `z.union([z.string(), z.number(), z.bigint()]).pipe(z.coerce.bigint())` | `bigint` |
| `float`, `double` | `z.number()` | `number` |
| `string` | `z.string()` | `string` |
| `bytes` | `z.string()` | `string` (base64) |
| `repeated T` | `z.array(T)` | `T[]` |
| `map<K, V>` | `z.record(z.string(), V)` | `Record<string, V>` |
| `google.protobuf.Timestamp` | `z.string().datetime({ offset: true })` | `string` (ISO 8601) |
| `google.protobuf.Duration` | `z.string()` | `string` |
| `google.protobuf.FieldMask` | `z.string()` | `string` |
| `google.protobuf.*Value` | `z.T().nullable()` | `T \| null` |
| Nested message | `T.optional()` | `T \| undefined` |
| Enum | `z.enum([...]).or(z.string())` | `string` (open) |
| Oneof | `z.object({}).superRefine()` | mutual exclusion via refinement |

## Roadmap

| Phase | Scope | Status |
|---|---|---|
| **1** | Go + Firestore + MongoDB | ✅ Done |
| **1.5** | Rust + SQLite | ✅ Done |
| **2** | Python (absorbs [proto2pydantic](https://github.com/protocgen/proto2pydantic)) | ✅ Done |
| **3** | Kotlin + Validation | ✅ Done |
| **4** | DynamoDB + Datastore + Spanner | Planned |
| **5** | TypeScript + Zod | ✅ Done |

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
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, PR process, and commit signing requirements.

## License

Apache-2.0
