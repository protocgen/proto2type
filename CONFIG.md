# Configuration Reference

`proto2type` accepts options via `buf.gen.yaml` (under `opt:`) or as `protoc` flags (`--proto2type_opt=`).

## Plugin Options

Command-line options passed to the plugin.

### `lang`

Target language for code generation.

| Default | `go` |
|---|---|
| Values | `go`, `rust`, `python`, `kotlin`, `typescript` |

> **Note:** `go`, `rust`, `python`, `kotlin`, and `typescript` are currently supported. Other languages are on the [roadmap](README.md#roadmap).

#### Rust-specific behaviour

When `lang=rust`:

- **Domain types** are `#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]` structs
- `google.protobuf.Timestamp` → `chrono::DateTime<Utc>`
- `google.protobuf.Duration` → `chrono::Duration`
- Nested messages → `Option<Box<T>>`
- `repeated` / `map` → `Vec<T>` / `HashMap<K, V>` with `#[serde(default)]`
- Optional fields → `Option<T>` with `#[serde(skip_serializing_if = "Option::is_none")]`
- File suffix: `{proto_name}.type.rs` (domain), `{proto_name}_{backend}.type.rs` (storage)

##### Rust Plugin Options

| Option | Default | Description |
|---|---|---|
| `rust_exhaustive` | `false` | generate exhaustive Rust structs (omit #[non_exhaustive]) |
| `rust_buffa_module` | _(none)_ | Rust module path for buffa proto types (required for backend=buffa) |
| `rust_buffa_oneof_prefix` | _(none)_ | module prefix before oneof submodule (e.g. __buffa for connectrpc-build) |
| `rust_domain_module` | _(none)_ | Rust module path for domain type imports in buffa output (default: use super::*) |

#### Python-specific behaviour

When `lang=python`:

- **Models** extend `pydantic.BaseModel` (configurable via `base_class`)
- `google.protobuf.Timestamp` → `datetime.datetime`
- `google.protobuf.Duration` → `datetime.timedelta`
- `google.protobuf.Struct` → `dict[str, Any]`
- Nested messages → top-level module class
- `repeated` / `map` → `list[T]` / `dict[K, V]`
- `optional` → `T | None` with `Field(default=None)`
- Enums → `str` (Literal) or `IntEnum` (configurable via `enum_style`)
- `google.api.field_behavior` annotations → `Field(...)` (REQUIRED) / `Field(exclude=True)` (OUTPUT_ONLY)
- `buf/validate` constraints → `Field()` kwargs (`min_length`, `max_length`, `ge`, `le`, `gt`, `lt`, `pattern`)
- File suffix: `{proto_name}_pb2_pydantic.py`

#### Kotlin-specific behaviour

When `lang=kotlin`:

- **Domain types** are `@Serializable` data classes using `kotlinx.serialization`
- `google.protobuf.Timestamp` → `kotlinx.datetime.Instant`
- `google.protobuf.Duration` → `kotlin.time.Duration`
- Nested messages → nullable `T?`
- `repeated` / `map` → `List<T>` / `Map<K, V>` with defaults `emptyList()` / `emptyMap()`
- Optional fields → `T?` with default `null`
- Enums → `@Serializable enum class` with `@SerialName` and `fromValue(Int)` companion
- Oneofs → `@Serializable sealed class` with data class variants
- File suffix: `{proto_name}.type.kt`

#### TypeScript-specific behaviour

When `lang=typescript`:

- File suffix: `{proto_name}.type.ts` (configurable via `ts_output_suffix`)

##### TypeScript Plugin Options

| Option | Default | Description |
|---|---|---|
| `ts_int64` | `string` | TypeScript: int64 representation (string, bigint) |
| `ts_enum_style` | `enum` | TypeScript: enum style (enum, native) |
| `ts_explicit_types` | `true` | TypeScript: emit explicit interface types |
| `ts_zod_import` | `zod` | TypeScript: Zod import path |
| `ts_types_only` | `false` | TypeScript: emit plain types without Zod |
| `ts_strict` | `false` | TypeScript: append .strict() to reject unknown fields |
| `ts_output_suffix` | `.type` | TypeScript: output file suffix (default .type) |
| `ts_preset` | _(none)_ | TypeScript: preset (zod-strict, types-only) |

##### Python Plugin Options

| Option | Default | Description |
|---|---|---|
| `python_base_class` | `BaseModel` | Python: custom Pydantic base class |
| `python_alias_generator` | _(none)_ | Python: alias generator (camel) |
| `python_enum_style` | _(none)_ | Python: enum style (raw) |
| `python_preset` | _(none)_ | Python: preset (a2a) |
| `python_description` | _(none)_ | Python: module-level docstring |
| `python_strip_proto_suffix` | `false` | Python: use base.py instead of base_pb2_pydantic.py |

### `backend`

Storage backend to generate structs for.

| Default | _(none)_ — no storage types generated |
|---|---|
| Values | `firestore`, `mongo`, `sqlite`, `dynamodb`, `datastore`, `spanner`, `buffa`, `jsonrpc` |

When set, generates a `<Name><Backend>` struct (e.g. `UserFirestore`, `UserMongo`) with backend-specific struct tags and `ToProto()` / `FromProto()` converters.

Can be combined with `domain=true` (default) to generate both domain and storage types, or used with `domain=false` to generate storage types only.

### `domain`

Controls whether domain types and proto converters are generated.

| Default | `true` |
|---|---|
| Values | `true`, `false` |

When `true`, generates a clean native struct (e.g. `User`) with `json:""` tags, `time.Time` for timestamps, and `ToProto()` / `FromProto()` converters.

At least one of `domain=true` or `backend=<name>` must be specified.

### `output_file`

Override the default output filename.

| Default | `{proto_name}.type.go` (domain), `{proto_name}_{backend}.type.go` (storage) — Go |
|---|---|
| | `{proto_name}.type.rs` (domain), `{proto_name}_{backend}.type.rs` (storage) — Rust |
| Example | `models.go` |

### `enum_as_string`

Controls how proto enums are represented in generated types.

| Default | `false` |
|---|---|
| When `false` | Enums are `int32` |
| When `true` | Enums are `string` (using proto enum value names) |

### `omitempty_default`

Controls the default `omitempty` behavior for optional and zero-value fields.

| Default | `true` |
|---|---|
| When `true` | `optional`, `repeated`, `map`, and message fields get `omitempty` |
| When `false` | Only fields with explicit `(proto2type.field).omitempty = OPTIONAL_BOOL_TRUE` get `omitempty` |

### `go_package`

Override Go package for generated types (import path;package_name).

| Default | _(none)_ |
|---|---|

### `debug`

Emit IR debug information to stderr.

| Default | `false` |
|---|---|

### `validate`

Selects the validation strategy for code generation from `buf.validate` constraints.

| Default | `""` (empty — no validation) |
|---|---|
| Values | `true` (default per language), `validator` (Rust), `native` (Kotlin) |

Behavior per language:

| Language | Strategy | Generated Code |
|---|---|---|
| Go | `true` → protovalidate | `Validate()` method delegating to `protovalidate.Validate(d.ToProto())` |
| Rust | `true` or `validator` | `#[derive(Validate)]` + `#[validate(...)]` attributes (validator crate) |
| Kotlin | `true` or `native` | Extension functions `fun T.validate(): List<String>` + `fun T.validateOrThrow()` |
| Python | _(always)_ | Constraints mapped to Pydantic `Field()` kwargs (no `validate` flag needed) |
| TypeScript | `true` | Constraints mapped to Zod schemas |

> **Note:** Length validation counts characters, not bytes.

### Summary Table

| Option | Default | Description |
|---|---|---|
| `lang` | `go` | target language: go, python, kotlin, typescript |
| `backend` | _(none)_ | storage backend: firestore, mongo, dynamodb, datastore, spanner, sqlite, buffa, jsonrpc |
| `domain` | `true` | generate domain types + proto converters |
| `output_file` | _(none)_ | override output filename |
| `enum_as_string` | `false` | store enums as string names |
| `omitempty_default` | `true` | default omitempty for optional fields |
| `go_package` | _(none)_ | override Go package for generated types (import path;package_name) |
| `rust_exhaustive` | `false` | generate exhaustive Rust structs (omit #[non_exhaustive]) |
| `rust_buffa_module` | _(none)_ | Rust module path for buffa proto types (required for backend=buffa) |
| `validate` | _(none)_ | validation strategy: true (default per lang), validator (Rust), native (Kotlin) |
| `rust_buffa_oneof_prefix` | _(none)_ | module prefix before oneof submodule (e.g. __buffa for connectrpc-build) |
| `rust_domain_module` | _(none)_ | Rust module path for domain type imports in buffa output (default: use super::*) |
| `python_base_class` | `BaseModel` | Python: custom Pydantic base class |
| `python_alias_generator` | _(none)_ | Python: alias generator (camel) |
| `python_enum_style` | _(none)_ | Python: enum style (raw) |
| `python_preset` | _(none)_ | Python: preset (a2a) |
| `python_description` | _(none)_ | Python: module-level docstring |
| `python_strip_proto_suffix` | `false` | Python: use base.py instead of base_pb2_pydantic.py |
| `ts_int64` | `string` | TypeScript: int64 representation (string, bigint) |
| `ts_enum_style` | `enum` | TypeScript: enum style (enum, native) |
| `ts_explicit_types` | `true` | TypeScript: emit explicit interface types |
| `ts_zod_import` | `zod` | TypeScript: Zod import path |
| `ts_types_only` | `false` | TypeScript: emit plain types without Zod |
| `ts_strict` | `false` | TypeScript: append .strict() to reject unknown fields |
| `ts_output_suffix` | `.type` | TypeScript: output file suffix (default .type) |
| `ts_preset` | _(none)_ | TypeScript: preset (zod-strict, types-only) |
| `debug` | `false` | emit IR debug information to stderr |

## Proto Options

Annotate individual fields and messages in your `.proto` files to control how `proto2type` generates code for them.

First, import the options proto:

```protobuf
import "proto2type/options.proto";
```

### `(proto2type.field).document_id`

Marks a field as the document ID.

| Type | `bool` |
|---|---|
| Default | `false` |

**Behavior per backend:**

| Backend | Effect |
|---|---|
| Firestore | Field is **excluded** from the generated struct — Firestore uses the document path as the ID, not a struct field |
| MongoDB | Field tag becomes `bson:"_id"` — maps to MongoDB's `_id` document key |
| _(domain)_ | No effect — field is included normally with `json:""` tag |

```protobuf
message User {
  string id = 1 [(proto2type.field).document_id = true];
}
```

### `(proto2type.field).server_timestamp`

Marks a timestamp field as server-managed.

| Type | `bool` |
|---|---|
| Default | `false` |

**Behavior per backend:**

| Backend | Effect |
|---|---|
| Firestore | Tag becomes `firestore:"field_name,serverTimestamp"` — Firestore sets the timestamp on write |
| MongoDB | No special behavior (use MongoDB server-side `$currentDate` in your queries) |
| _(domain)_ | No effect — field is a normal `time.Time` |

```protobuf
message User {
  google.protobuf.Timestamp updated_at = 10 [(proto2type.field).server_timestamp = true];
}
```

### `(proto2type.field).skip`

Excludes the field from **all** generated types (domain, storage, and converters).

| Type | `bool` |
|---|---|
| Default | `false` |

Use this for internal-only fields that should not appear in application-layer code.

```protobuf
message User {
  string internal_trace_id = 99 [(proto2type.field).skip = true];
}
```

### `(proto2type.field).omitempty`

Force-override the `omitempty` behavior for a specific field.

| Type | `OptionalBool` |
|---|---|
| Default | `OPTIONAL_BOOL_UNSPECIFIED` (uses `omitempty_default` plugin option) |
| Values | `OPTIONAL_BOOL_TRUE`, `OPTIONAL_BOOL_FALSE` |

```protobuf
message User {
  // Always include category in JSON/storage even when empty
  string category = 7 [(proto2type.field).omitempty = OPTIONAL_BOOL_FALSE];
  // Always omit roles when empty
  repeated string roles = 8 [(proto2type.field).omitempty = OPTIONAL_BOOL_TRUE];
}
```

### `(proto2type.field).inline`

Flattens a nested message's fields into the parent struct.

| Type | `bool` |
|---|---|
| Default | `false` |

**Behavior per backend:**

| Backend | Effect |
|---|---|
| MongoDB | Adds `bson:",inline"` to the struct tag — embeds nested document fields at the parent level |
| Firestore | No direct equivalent — nested message is stored as a sub-map |
| _(domain)_ | No effect — field remains a pointer to the nested struct |

```protobuf
message User {
  Address address = 5 [(proto2type.field).inline = true];
}
```

Generated MongoDB struct:
```go
type UserMongo struct {
    // ...
    Address *AddressMongo `bson:",inline"`
}
```

### `(proto2type.field).name`

Overrides the storage field name used in struct tags.

| Type | `string` |
|---|---|
| Default | _(proto field name in snake_case)_ |

This affects `firestore:""`, `bson:""`, and `json:""` tag values.

```protobuf
message User {
  string display_name = 3 [(proto2type.field).name = "name"];
}
```

Generated:
```go
DisplayName string `json:"name"`          // domain
DisplayName string `firestore:"name"`     // firestore
DisplayName string `bson:"name"`          // mongo
```

### `(proto2type.message).skip`

Skips generating types for the entire message.

| Type | `bool` |
|---|---|
| Default | `false` |

Use this for messages that are only used as proto wire types and should not have generated domain or storage types.

```protobuf
message InternalRpcRequest {
  option (proto2type.message).skip = true;
  string trace_id = 1;
}
```

## Backend Reference

### Firestore

| Feature | Support |
|---|---|
| Struct tag | `firestore:""` |
| Document ID | `document_id=true` → field excluded from struct (Firestore uses doc path) |
| Server timestamps | `server_timestamp=true` → `firestore:"field,serverTimestamp"` |
| Omitempty | `firestore:"field,omitempty"` |
| Struct suffix | `Firestore` (e.g. `UserFirestore`) |
| File suffix | `_firestore.type.go` |

**Example generated output:**

```go
type UserFirestore struct {
    Email       string    `firestore:"email"`
    DisplayName string    `firestore:"display_name"`
    Active      bool      `firestore:"active"`
    CreatedAt   time.Time `firestore:"created_at,serverTimestamp"`
    UpdatedAt   time.Time `firestore:"updated_at,serverTimestamp"`
}

func (d *UserFirestore) ToProto() *userpb.User { ... }
func (d *UserFirestore) FromProto(pb *userpb.User) { ... }
```

### MongoDB

| Feature | Support |
|---|---|
| Struct tag | `bson:""` |
| Document ID | `document_id=true` → `bson:"_id"` |
| Inline embedding | `inline=true` → `bson:",inline"` |
| Omitempty | `bson:"field,omitempty"` |
| Struct suffix | `Mongo` (e.g. `UserMongo`) |
| File suffix | `_mongo.type.go` |

**Example generated output:**

```go
type UserMongo struct {
    ID          string        `bson:"_id"`
    Email       string        `bson:"email"`
    DisplayName string        `bson:"display_name"`
    Active      bool          `bson:"active"`
    Address     *AddressMongo `bson:",inline"`
    CreatedAt   time.Time     `bson:"created_at,omitempty"`
    UpdatedAt   time.Time     `bson:"updated_at,omitempty"`
}

func (d *UserMongo) ToProto() *userpb.User { ... }
func (d *UserMongo) FromProto(pb *userpb.User) { ... }
```

### SQLite (Rust)

| Feature | Support |
|---|---|
| Language | Rust (`lang=rust`) |
| Struct suffix | `Row` (e.g. `UserRow`) |
| File suffix | `_sqlite.type.rs` |
| Timestamps | `i64` epoch milliseconds |
| Durations | `i64` milliseconds |
| Nested messages | `String` (JSON-serialised via `serde_json`) |
| `repeated` / `map` | `String` (JSON-serialised via `serde_json`) |
| Row construction | `from_row(&rusqlite::Row)` for named column access |
| Domain converters | `to_domain()` / `from_domain()` methods |

**Example generated output:**

```rust
// Code generated by proto2type. DO NOT EDIT.
// backend: sqlite

use super::*;
use serde::{Deserialize, Serialize};
use chrono::{DateTime, Utc};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UserRow {
    pub id: String,
    pub email: String,
    pub display_name: String,
    pub active: bool,
    pub created_at: i64,        // epoch milliseconds
    pub session_timeout: i64,   // duration in milliseconds
    pub address: String,        // nested message as JSON
    pub roles: String,          // repeated field as JSON
}

impl UserRow {
    pub fn from_row(row: &rusqlite::Row) -> rusqlite::Result<Self> { ... }
    pub fn to_domain(&self) -> Result<User, serde_json::Error> { ... }
    pub fn from_domain(d: &User) -> Result<Self, serde_json::Error> { ... }
}
```

## Example: Full buf.gen.yaml

Generate domain types, Firestore storage, and MongoDB storage from the same proto:

```yaml
# buf.gen.yaml
version: v2
plugins:
  # Domain types (json tags, time.Time, converters)
  - local: protoc-gen-proto2type
    out: gen/go
    opt:
      - lang=go

  # Firestore storage types
  - local: protoc-gen-proto2type
    out: gen/go
    opt:
      - lang=go
      - domain=false
      - backend=firestore

  # MongoDB storage types
  - local: protoc-gen-proto2type
    out: gen/go
    opt:
      - lang=go
      - domain=false
      - backend=mongo
```

This produces three files per proto:
- `user.type.go` — domain types with `json:""` tags
- `user_firestore.type.go` — Firestore structs with `firestore:""` tags
- `user_mongo.type.go` — MongoDB structs with `bson:""` tags

All three include `ToProto()` and `FromProto()` converters.

### Security Considerations

#### JSON Deserialization Trust Model

The SQLite backend serializes nested messages, repeated fields, and maps as JSON strings in TEXT columns. The generated `to_domain()` and `into_domain()` methods deserialize these values using `serde_json::from_str()`.

**Trust Assumption:** The SQLite database is treated as a trusted data source. If the database is writable by untrusted parties, consider:

- Adding input size validation before deserialization
- Using `serde_json::from_reader` with a bounded reader
- Validating JSON depth before processing

`serde_json` applies a default recursion limit of 128 levels, which mitigates deep-nesting attacks.

#### Sensitive Data

Generated structs derive `Debug`, `Serialize`, and `Deserialize` for all fields. This means:
- `Debug` formatting will include all field values, including PII
- Serialization will include all fields

For fields containing sensitive data, consider:
- Adding `#[serde(skip)]` manually to generated files (not recommended — will be overwritten)
- Filing an issue to add a `(proto2type.field).sensitive` option
- Using a wrapper type that implements custom `Debug`

#### Supply Chain

Generated Rust code depends on these crates:
| Crate | Min Version | Notes |
|-------|------------|-------|
| `serde` | 1.0 | Widely audited |
| `serde_json` | 1.0 | Widely audited |
| `chrono` | 0.4.20+ | Pin to avoid RUSTSEC-2020-0159 |
| `rusqlite` | 0.35 | C FFI to SQLite |
| `validator` | 0.18 | Used when `validate=true` for `#[derive(Validate)]` |
| `lazy_static` | 1.4 | Used by generated pattern/UUID validation code |
| `regex` | 1.0 | Used by generated pattern validation code |

Run `cargo audit` regularly on projects using generated code.
