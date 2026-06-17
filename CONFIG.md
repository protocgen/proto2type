# Configuration Reference

`proto2type` accepts options via `buf.gen.yaml` (under `opt:`) or as `protoc` flags (`--proto2type_opt=`).

## Plugin Options

Command-line options passed to the plugin.

### `lang`

Target language for code generation.

| Default | `go` |
|---|---|
| Values | `go`, `python`, `kotlin`, `typescript` |

> **Note:** Only `go` is currently supported. Other languages are on the [roadmap](README.md#roadmap).

### `backend`

Storage backend to generate structs for.

| Default | _(none)_ — no storage types generated |
|---|---|
| Values | `firestore`, `mongo`, `dynamodb`, `datastore`, `spanner` |

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

| Default | `{proto_name}.type.go` (domain), `{proto_name}_{backend}.type.go` (storage) |
|---|---|
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

### Summary Table

| Option | Default | Description |
|---|---|---|
| `lang` | `go` | Target language |
| `backend` | _(none)_ | Storage backend |
| `domain` | `true` | Generate domain types + proto converters |
| `output_file` | _(auto)_ | Override output filename |
| `enum_as_string` | `false` | Enums as `string` instead of `int32` |
| `omitempty_default` | `true` | Default `omitempty` for optional/zero-value fields |

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
