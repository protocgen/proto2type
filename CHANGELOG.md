# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.0] - 2025-06-28

### Added

- **Rust language backend** with SQLite storage — generate domain structs and SQLite `Row` types from proto definitions
- **Rust enum generation** — `#[repr(i32)]` enums with `from_i32()`, `Display`, `Default`, `Serialize/Deserialize`
- **Oneof support** — tagged Rust enums (`#[serde(tag, content)]`), stored as JSON in SQLite
- **Well-Known Type mappings** — `Timestamp→DateTime<Utc>`, `Duration→i64` (ms), `Struct→Map`, `Value→serde_json::Value`, `ListValue→Vec<Value>`, `FieldMask→Vec<String>`, `Empty→()`
- **`TryFrom` impls** — `TryFrom<&Domain> for Row` (always), `TryFrom<Row> for Domain` (when no `document_id`)
- **`Default` derive** on all domain structs
- **`#[non_exhaustive]`** on all domain structs for forward compatibility
- **`PartialEq`** on all Row structs for test assertions
- **Version stamp** — `// proto2type v0.3.0` in all generated file headers
- **`ConversionError`** custom error type with `Json` and `InvalidTimestamp` variants
- **Keyword escaping** — `r#` prefix for most Rust keywords; `_` suffix for `self/super/crate/true/false`
- **`#[serde(default)]`** on `Vec` and `HashMap` fields
- **`into_domain(self)`** consuming conversion alongside borrowing `to_domain(&self)`
- **Security documentation** — `SECURITY.md` with trust model, code injection analysis, supply chain risks
- **`cargo audit`** in CI pipeline
- **`chrono`** pinned to `>=0.4.20` (RUSTSEC-2020-0159 mitigation)
- **Lefthook** hooks — pre-commit (gofmt, go-vet, golangci-lint) and pre-push (go test, golden file drift check)
- **Go integration tests** — 14 protoc-based tests exercising type resolution, enum gen, oneof detection, WKTs, import scanning, SQLite conversions via `buf build`
- **Rust integration tests** — 27 SQLite round-trip tests covering domain↔row, TryFrom, enum, oneof, nested `Option<Box<T>>`, unicode, binary blobs
- **CI jobs** — `golden-test-rust`, `rust-compile-check`, `rust-integration-test`

### Changed

- SQLite unsigned types (`u32/u64`) stored as `i64` with cast conversions
- `Duration` stored as `i64` milliseconds (not `chrono::Duration`) for serde compatibility
- `epoch_ms_to_datetime` returns `Result` instead of panicking
- Nested messages stored as `Option<String>` (JSON) in SQLite
- Error propagation uses `?` operator with `ConversionError` throughout

## [0.2.0] - 2025-06-27

### Added

- `enum_as_string` field option for per-field enum string serialization
- Repeated message fields use element-wise conversion in converters

## [0.1.0] - Initial Release

### Added

- Go language backend with Firestore and MongoDB storage
- Domain type generation from protobuf definitions
- Clone/Equal method generation
- FieldMask support
- Proto↔Domain converters
- Custom field options (`document_id`, `server_timestamp`, `skip`, `inline`, `name`)
