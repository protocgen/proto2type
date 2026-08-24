# Rust Support

`proto2type` provides robust Rust generation, turning your Protocol Buffer definitions into fully typed Rust structs with Serde compatibility and validation. It converts domain types into idiomatic Rust and integrates with the `validator` crate for `buf.validate` constraint enforcement.

## Quick Start

Add the plugin to your `buf.gen.yaml`:

```yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: gen/rust
    opt:
      - lang=rust
      - validate=validator
```

Then run `buf generate`.

## Options Reference

| Option | Default | Description |
|---|---|---|
| `rust_exhaustive` | `false` | generate exhaustive Rust structs (omit `#[non_exhaustive]`) |
| `rust_buffa_module` | _(none)_ | Rust module path for buffa proto types (required for `backend=buffa`) |
| `rust_buffa_oneof_prefix` | _(none)_ | module prefix before oneof submodule (e.g. `__buffa` for connectrpc-build) |
| `rust_domain_module` | _(none)_ | Rust module path for domain type imports in buffa output (default: `use super::*`) |
| `validate` | `""` | `validator` (or `true`) to enable validation via the `validator` crate |

> **Note**: For a full list of general options and proto annotations, see [CONFIG.md](CONFIG.md).

## Dependencies

To use the generated Rust code, add the following to your `Cargo.toml`. When `validate=validator` is enabled, the `validator`, `lazy_static`, and `regex` crates are required.

```toml
[dependencies]
serde = { version = "1", features = ["derive"] }
serde_json = "1"
chrono = { version = "0.4.20", features = ["serde"] }
# Required for validate=validator
validator = { version = "0.20", features = ["derive"] }
lazy_static = "1"
regex = "1"
# Required if backend=buffa
buffa = "0.6"
buffa-types = "0.6"
# Required if backend=sqlite
rusqlite = { version = "0.35", features = ["bundled"] }
```

## Generated Code Walkthrough

By default, the plugin generates idiomatic Rust domain types with derivations for Serde, Debug, and Clone.

### Domain Structs

```rust
/// Domain representation of test.v1.User.
///
/// User represents a user account.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize, Validate)]
#[non_exhaustive]
pub struct User {
    pub id: String,
    #[validate(email)]
    pub email: String,
    #[validate(length(min = 1, max = 255))]
    pub display_name: String,
    pub active: bool,
    #[validate(range(min = 0, max = 150))]
    pub age: i32,
    #[serde(default)]
    #[validate(length(min = 1, max = 10))]
    pub roles: Vec<String>,
    #[serde(default)]
    pub metadata: HashMap<String, String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    #[validate(nested)]
    pub address: Option<Address>,
    pub created_at: DateTime<Utc>,
    /// Duration in milliseconds
    pub session_timeout: i64,
}
```

> **Note**: Structs are marked `#[non_exhaustive]` by default to allow backwards-compatible field additions. You can control this via the `rust_exhaustive` option.

### Enums

Enums are mapped to Rust enums with a `#[repr(i32)]` attribute and a `from_i32()` conversion method.

```rust
/// UserStatus represents the user's account status.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default, Serialize, Deserialize)]
#[repr(i32)]
pub enum UserStatus {
    #[default]
    Unspecified = 0,
    Active = 1,
    Suspended = 2,
    Deleted = 3,
}

impl UserStatus {
    pub fn from_i32(value: i32) -> Option<Self> {
        match value {
            0 => Some(Self::Unspecified),
            1 => Some(Self::Active),
            2 => Some(Self::Suspended),
            3 => Some(Self::Deleted),
            _ => None,
        }
    }
}
```

### Oneofs

Oneof fields are generated as a standard Rust enum and use Serde tagging attributes.

```rust
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "kind", content = "value")]
pub enum UserContactMethod {
    #[serde(rename = "contact_email")]
    ContactEmail(String),
    #[serde(rename = "contact_phone")]
    ContactPhone(String),
}
```

### Keyword Escaping

Fields named after Rust keywords use raw identifiers (`r#type`, `r#match`, etc.) with `#[serde(rename)]` for correct serialization. The `self` keyword is a special case — since `r#self` is not permitted in Rust, it uses a suffixed identifier `self_` instead.

```rust
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize, Validate)]
#[non_exhaustive]
pub struct KeywordFields {
    #[serde(rename = "type")]
    pub r#type: String,
    #[serde(rename = "self")]
    pub self_: i32,
    #[serde(rename = "match")]
    pub r#match: bool,
    #[serde(rename = "mod")]
    pub r#mod: String,
    #[serde(rename = "ref")]
    pub r#ref: i64,
}
```

## Validation

When using `validate=validator` (or `validate=true`), `proto2type` converts `buf.validate` proto annotations directly into attributes for the [validator](https://crates.io/crates/validator) crate.

- **Derive-compatible Constraints**: Constraints like lengths, numeric ranges, and email are converted into direct `#[validate(...)]` derivations (e.g., `#[validate(length(min = 1))]`).
- **Custom Functions**: For non-derive constraints (e.g., prefix, suffix, contains, const, in, not_in, and timestamp ranges), `proto2type` generates standalone validation functions and references them via `#[validate(custom(function = "validate_user_email"))]`.
- **Regex Patterns**: Regular expressions are hoisted out as package-level constants using `lazy_static!`.

```rust
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize, Validate)]
#[non_exhaustive]
pub struct Address {
    #[validate(length(min = 1))]
    pub street: String,
    #[validate(length(min = 1))]
    pub city: String,
    #[validate(length(min = 2, max = 2))]
    pub state: String,
    #[validate(regex(path = *RE_ZIP_PATTERN))]
    pub zip: String,
    pub country: String,
}

lazy_static! {
    static ref RE_ZIP_PATTERN: Regex = Regex::new(r#"^[0-9]{5}(-[0-9]{4})?$"#).unwrap();
}
```

### Constraints Table

| buf.validate constraint | Rust equivalent |
|---|---|
| `string.min_len` / `max_len` | `#[validate(length(min = ..., max = ...))]` |
| `string.email` | `#[validate(email)]` |
| `string.pattern` | `#[validate(regex(path = ...))]` using `lazy_static` `Regex` |
| `string.uri` | `#[validate(url)]` |
| `string.uuid` | `#[validate(regex(path = *RE_..._UUID))]` |
| `string.prefix` / `suffix` / `contains` | `#[validate(custom(function = "..."))]` |
| `string.const` / `in` / `not_in` | `#[validate(custom(function = "..."))]` |
| `int32.gte` / `lte` / `gt` / `lt` | `#[validate(range(min = ..., max = ...))]` |
| `required` | `#[validate(required)]` on `Option<T>` |
| `repeated.min_items` / `max_items` | `#[validate(length(min = ..., max = ...))]` |
| `message` | `#[validate(nested)]` on `Option<T>` or `Vec<T>` |
| `timestamp.gte` / `lte` | `#[validate(custom(function = "..."))]` |

## Well-Known Types

Well-Known Types (WKTs) natively map to standard Rust types.

| WKT | Rust equivalent |
|---|---|
| `google.protobuf.Timestamp` | `chrono::DateTime<Utc>` |
| `google.protobuf.Duration` | `i64` (duration in milliseconds) |
| `google.protobuf.Struct` | `serde_json::Map<String, serde_json::Value>` |
| `google.protobuf.Value` | `serde_json::Value` |
| `google.protobuf.ListValue` | `Vec<serde_json::Value>` |
| `google.protobuf.FieldMask` | `Vec<String>` |
| `google.protobuf.Any` | `serde_json::Value` |
| `google.protobuf.Empty` | `()` |
| `google.protobuf.BoolValue` | `Option<bool>` |
| `google.protobuf.StringValue` | `Option<String>` |
| `google.protobuf.Int32Value` | `Option<i32>` |
| `google.protobuf.Int64Value` | `Option<i64>` |
| `google.protobuf.FloatValue` | `Option<f32>` |
| `google.protobuf.DoubleValue` | `Option<f64>` |
| `google.protobuf.BytesValue` | `Option<Vec<u8>>` |
| `map<string, T>` | `std::collections::HashMap<String, T>` |

## Storage Backends

`proto2type` generates backend-specific storage types and converters through the `backend` option. Rust currently supports `buffa` (proto converters) and `sqlite`.

### `backend=buffa`

Generates converters between the `proto2type` domain structs and `buffa` Protobuf types, implementing `TryFrom` traits.

```rust
impl TryFrom<&User> for __buffa_mod::User {
    type Error = ConversionError;
    fn try_from(d: &User) -> Result<Self, Self::Error> {
        // Safe conversions via into()
    }
}

impl TryFrom<&__buffa_mod::User> for User {
    type Error = ConversionError;
    fn try_from(b: &__buffa_mod::User) -> Result<Self, Self::Error> {
        // Safe extraction and validation from proto
    }
}
```

### `backend=sqlite`

Generates intermediate structs (`UserRow`) tailored for interaction with `rusqlite`, which flattens out nested structures via JSON serialization. `to_domain` and `into_domain` methods are provided.

```rust
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct UserRow {
    pub id: String,
    pub created_at: i64,
    pub roles: String, // stored as JSON
    // ...
}

impl UserRow {
    pub fn from_row(row: &rusqlite::Row) -> rusqlite::Result<Self> {
        Ok(Self {
            id: row.get::<_, String>("id")?,
            created_at: row.get::<_, i64>("created_at")?,
            roles: row.get::<_, String>("roles")?,
        })
    }
    
    pub fn to_domain(&self) -> Result<User, ConversionError> {
        // Deserializes JSON fields and converts Unix ms to chrono::DateTime
    }
}
```

## Known Limitations

- **No CEL support**: The generated Rust validation only supports standard `buf.validate` assertions (e.g. lengths, regex patterns, numeric ranges). Hand-written Common Expression Language (CEL) expressions (`(buf.validate.field).cel`) are ignored.
- **Validator Crate**: You must use a compatible version of the `validator` crate (`0.18` or `0.20+`) that matches the generated `#[validate]` derive macros.
