# Go Support

`proto2type` provides robust native Go generation, turning your Protocol Buffer definitions into idiomatic Go structs. It generates deep copy methods, equality checks, field mask application, and full support for proto `buf.validate` annotations natively in Go without external dependencies (when using `validate=native`).

## Quick Start

Add the plugin to your `buf.gen.yaml`:

```yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: gen/go
    opt:
      - lang=go
      - go_package=github.com/my/project/gen/go;mygen
      - validate=native
```

Then run `buf generate`.

## Options Reference

| Option | Default | Description |
|---|---|---|
| `lang` | `go` | Target language. Must be set to `go`. |
| `domain` | `true` | Generate domain types (`User`) and proto converters (`ToProto`/`FromProto`). |
| `backend` | _none_ | Generate storage types. See [Storage Backends](#storage-backends) below. |
| `validate` | `""` | `true` (uses `protovalidate` delegation) or `native` (pure Go checks). |
| `enum_as_string` | `false` | If `true`, enums are mapped to `string`. If `false`, mapped to `int32`. |
| `omitempty_default`| `true` | If `true`, adds `omitempty` to optional/repeated/map/message json tags. |
| `go_package` | _none_ | Override Go package for generated types (e.g. `path/to/pkg;pkgname`). |

> **Note**: For a full list of general options and proto annotations, see [CONFIG.md](CONFIG.md).

## Generated Code Walkthrough

By default, the plugin generates idiomatic Go structs representing your domain types. These structs use standard Go types (like `time.Time` and `time.Duration`) rather than protobuf wrappers.

### Domain Structs

```go
// User is the domain representation of test.v1.User.
type User struct {
	ID             string            `json:"id,omitempty"`
	Email          string            `json:"email,omitempty"`
	Active         bool              `json:"active,omitempty"`
	CreatedAt      time.Time         `json:"created_at,omitempty"`
	SessionTimeout time.Duration     `json:"session_timeout,omitempty"`
}
```

### Enums

By default, enums are represented as `int32` fields to match standard Go proto bindings. You can opt to use strings by setting `enum_as_string=true`.

### Oneofs

Oneof fields are flattened into the parent struct as pointer fields. This makes working with them much more ergonomic in standard Go code. To ensure mutual exclusion and provide an easy way to see which variant is set, `proto2type` generates an `Active<Name>()` helper method.

```go
type User struct {
	// oneof: contact_method
	ContactEmail    *string `json:"contact_email,omitempty"`
	ContactPhone    *string `json:"contact_phone,omitempty"`
}

// ActiveContactMethod returns the proto field name of the set contact_method
// variant, or "" if none is set.
func (u *User) ActiveContactMethod() string {
	if u == nil {
		return ""
	}
	if u.ContactEmail != nil {
		return "contact_email"
	}
	if u.ContactPhone != nil {
		return "contact_phone"
	}
	return ""
}
```

### Proto Converters

`ToProto()` and `FromProto()` methods are generated to bridge the domain structs and the underlying protobuf message types, handling all Well-Known Type conversions securely.

```go
// ToProto converts to the protobuf message.
func (u *User) ToProto() *pb.User { ... }

// FromProto populates from a protobuf message.
func (u *User) FromProto(msg *pb.User) { ... }
```

### Deep Copy and Equality

`Clone()` and `Equal()` methods are automatically generated for all domain structs. These operations correctly recurse into nested messages, slices, and maps.

```go
// Clone returns a deep copy of User.
func (u *User) Clone() *User { ... }

// Equal reports whether u and other are equal.
func (u *User) Equal(other *User) bool { ... }
```

### Field Masks

To support partial updates, `proto2type` generates an `ApplyFieldMask<Name>` function for each message.

```go
// ApplyFieldMaskUser copies fields from src to dst based on the given paths.
func ApplyFieldMaskUser(dst, src *User, paths []string) { ... }
```

## Validation

Validation rules defined using `buf.validate` annotations can be enforced directly in Go. `proto2type` supports two validation modes for Go: `validate=true` and `validate=native`.

### `validate=true` (Protovalidate)

When using `validate=true`, the generated `Validate()` method delegates to the official `protovalidate` library. It converts the domain struct to a proto message and runs full validation, which includes executing CEL (Common Expression Language) expressions.

```go
import "buf.build/go/protovalidate"

func (u *User) Validate() error {
    if err := protovalidate.Validate(u.ToProto()); err != nil {
        return err
    }
    return nil
}
```

### `validate=native` (Zero Dependencies)

When using `validate=native`, `proto2type` emits pure Go constraint checks directly into the `Validate()` method, completely eliminating the dependency on `protovalidate` and the overhead of CEL evaluation. 

Validation constraints handle recursive checks, regular expressions (hoisted to package-level variables), string manipulation (evaluated using UTF-8 runes), and temporal ranges.

```go
var _reUserEmailEmail = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
var _reUserPhonePattern = regexp.MustCompile("^\\+?[0-9\\-\\s]+$")

// Validate checks domain invariants on User.
// It ensures at most one variant is set per oneof group.
// It also runs buf.validate constraints as native Go checks.
func (u *User) Validate() error {
	if u == nil {
		return nil
	}
	{
		_oneofCount := 0
		if u.ContactEmail != nil {
			_oneofCount++
		}
		if u.ContactPhone != nil {
			_oneofCount++
		}
		if _oneofCount > 1 {
			return fmt.Errorf("oneof contact_method: %d variants set, expected at most 1", _oneofCount)
		}
	}
	if u.Email != "" && !_reUserEmailEmail.MatchString(u.Email) {
		return fmt.Errorf("email: must be a valid email address")
	}
	if utf8.RuneCountInString(u.DisplayName) < 1 {
		return fmt.Errorf("display_name: must be at least %d characters", 1)
	}
	if utf8.RuneCountInString(u.DisplayName) > 255 {
		return fmt.Errorf("display_name: must be at most %d characters", 255)
	}
	if u.Age < 0 {
		return fmt.Errorf("age: must be >= 0")
	}
	if u.Age > 150 {
		return fmt.Errorf("age: must be <= 150")
	}
	if len(u.Roles) < 1 {
		return fmt.Errorf("roles: must have at least %d items", 1)
	}
	if len(u.Roles) > 10 {
		return fmt.Errorf("roles: must have at most %d items", 10)
	}
	if u.Phone != nil {
		if utf8.RuneCountInString((*u.Phone)) < 7 {
			return fmt.Errorf("phone: must be at least %d characters", 7)
		}
		if utf8.RuneCountInString((*u.Phone)) > 20 {
			return fmt.Errorf("phone: must be at most %d characters", 20)
		}
		if !_reUserPhonePattern.MatchString((*u.Phone)) {
			return fmt.Errorf("phone: must match pattern %s", "^\\+?[0-9\\-\\s]+$")
		}
	}
	if u.Address != nil {
		if err := u.Address.Validate(); err != nil {
			return fmt.Errorf("address: %w", err)
		}
	}
	for _i, _v := range u.Tags {
		if _v != nil {
			if err := _v.Validate(); err != nil {
				return fmt.Errorf("tags[%d]: %w", _i, err)
			}
		}
	}
	return nil
}
```

### Constraints Table

| buf.validate constraint | Native Go equivalent |
|---|---|
| `string.min_len` / `max_len` | `utf8.RuneCountInString(v)` bounds checking |
| `string.pattern` | `regexp.MustCompile(pattern).MatchString(v)` |
| `string.email` | Internal regex match (`^[^@\s]+@[^@\s]+\.[^@\s]+$`) |
| `string.uuid` | Internal regex match for UUID format |
| `string.in` / `not_in` | `switch v { case "a", "b": ... }` |
| `string.prefix` / `suffix` | `strings.HasPrefix(v, ...)` / `strings.HasSuffix(v, ...)` |
| `string.contains` | `strings.Contains(v, ...)` |
| `int32.gt` / `gte` | Direct numeric comparison `v > ...` |
| `timestamp.gt` | `time.Time.After(...)` and `time.Time.Before(...)` |
| `repeated.min_items` | `len(v) >= ...` |
| `repeated.unique` | Maps items into a `map[any]bool` tracker |

## Well-Known Types

Well-Known Types are natively mapped into idiomatic Go types for better ergonomics.

| WKT | Go equivalent |
|---|---|
| `google.protobuf.Timestamp` | `time.Time` |
| `google.protobuf.Duration` | `time.Duration` |
| `google.protobuf.Struct` | `map[string]any` |
| `google.protobuf.Value` | `any` |
| `google.protobuf.ListValue` | `[]any` |
| `google.protobuf.Any` | `any` |
| `google.protobuf.Empty` | `struct{}` |
| `google.protobuf.FieldMask` | `[]string` |
| `google.protobuf.StringValue` | `*string` |
| `google.protobuf.Int32Value` | `*int32` |
| `google.protobuf.BoolValue` | `*bool` |

## Storage Backends

Setting `backend=firestore` or `backend=mongo` generates a secondary struct (e.g. `UserFirestore`) that is fully customized for the selected datastore, including:
- Specific struct tags (e.g. `firestore:"email"` or `bson:"email"`).
- Flattened nested message structures (via `bson:",inline"`).
- Omitted Document ID fields (which are handled externally by firestore paths or MongoDB `_id`).
- Special timestamp annotations (`serverTimestamp`).

See [CONFIG.md](CONFIG.md) for more details on backend options.

## Known Limitations

- **No CEL support in native mode**: The `validate=native` option only translates standard `buf.validate` constraints (e.g. lengths, regex patterns, numeric ranges). Hand-written CEL expressions (`(buf.validate.field).cel`) are silently ignored in this mode. If you need full CEL evaluation, use `validate=true` to delegate to `protovalidate`.
- **UInt bounds constraints mapped correctly**: `uint32` and `uint64` are natively handled as unsigned integers in Go, bypassing potential sign issues that plague other language generation runtimes.
- **Regex performance**: In native mode, regular expressions are hoisted globally via `regexp.MustCompile` on package initialization. However, Go's `regexp` package does not use a backtracking engine, making it safe from catastrophic ReDoS but slightly less compatible with some PCRE advanced assertions like lookaheads.
