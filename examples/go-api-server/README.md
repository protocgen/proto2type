# Go API Server Example

**What if you never had to write validation code again?**

### Before proto2type

Every Go API handler looks like this — manual, repetitive, out of sync with your API spec:

```go
func handleCreateUser(w http.ResponseWriter, r *http.Request) {
    var req CreateUserRequest
    json.NewDecoder(r.Body).Decode(&req)

    // 20+ lines of validation you have to write and maintain:
    if req.Email == "" {
        http.Error(w, "email is required", 400); return
    }
    if !strings.Contains(req.Email, "@") {
        http.Error(w, "invalid email", 400); return
    }
    if len(req.DisplayName) == 0 {
        http.Error(w, "display_name is required", 400); return
    }
    if len(req.DisplayName) > 100 {
        http.Error(w, "display_name too long", 400); return
    }
    if req.Age < 13 || req.Age > 120 {
        http.Error(w, "age must be 13-120", 400); return
    }
    if req.Role < 0 || req.Role > 2 {
        http.Error(w, "invalid role", 400); return
    }
    // ... and you'll forget to update this when the proto changes
}
```

### After proto2type

Define constraints once in your `.proto` file:

```protobuf
message User {
  string email = 1        [(buf.validate.field).string.email = true];
  string display_name = 2 [(buf.validate.field).string = {min_len: 1, max_len: 100}];
  int32 age = 3            [(buf.validate.field).int32 = {gte: 13, lte: 120}];
  UserRole role = 4        [(buf.validate.field).enum.defined_only = true];
  optional string bio = 5  [(buf.validate.field).string.max_len = 500];
}
```

Run `buf generate`. Your handler becomes:

```go
func handleCreateUser(w http.ResponseWriter, r *http.Request) {
    var user domain.User
    json.NewDecoder(r.Body).Decode(&user)

    if err := user.Validate(); err != nil {
        http.Error(w, err.Error(), 400); return
    }

    db.Exec("INSERT INTO users ...", user.Email, user.DisplayName, user.Age, user.Role, user.Bio)
}
```

**That's it.** No validation library. No runtime reflection. No keeping code in sync with your API spec. The generated `Validate()` method checks email format, string length, numeric bounds, and enum validity — all from your proto annotations.

---

## Quick Start

```bash
# Run the server (uses in-memory SQLite, no setup needed)
go run .
```

Try it:

```bash
# ❌ Validation catches bad input
curl -s localhost:8080/users \
  -d '{"email":"not-an-email","display_name":"","age":5}' | jq
# → {"error":"email: must be a valid email address"}

# ✅ Valid input succeeds
curl -s localhost:8080/users \
  -d '{"email":"alice@example.com","display_name":"Alice","age":25,"role":1}' | jq
# → {"id":1,"user":{"email":"alice@example.com","display_name":"Alice","age":25,"role":1}}

# 📖 Read it back
curl -s localhost:8080/users/1 | jq
# → {"email":"alice@example.com","display_name":"Alice","age":25,"role":1}

# 📋 List all users
curl -s localhost:8080/users | jq
```

## How It Works

```
user.proto          →  buf generate  →  domain.User struct
(constraints)                            + Validate() method
                                         + ToProto() / FromProto()
                                         + JSON tags
                                         + Clone(), Equal()
```

1. **[`proto/user/v1/user.proto`](proto/user/v1/user.proto)** — define your messages with `buf.validate` annotations
2. **`buf generate`** — runs `protoc-gen-proto2type` with `validate=native`
3. **[`gen/user/v1/domain/user.type.go`](gen/user/v1/domain/user.type.go)** — generated domain types (committed so you can browse them)
4. **[`main.go`](main.go)** — HTTP server using domain types directly with `encoding/json` and `database/sql`

## What to Look At

| File | Lines | What it shows |
|------|-------|--------------|
| [`user.proto`](proto/user/v1/user.proto) | 35 | Proto definition with 5 validation constraints |
| [`user.type.go`](gen/user/v1/domain/user.type.go) | 163 | Generated: struct, Validate(), ToProto/FromProto, Clone, Equal |
| [`main.go`](main.go) | 160 | HTTP server — the entire application |

## Regenerating

If you modify the proto and want to regenerate:

```bash
# Install proto2type
go install github.com/protocgen/proto2type@latest

# Regenerate
cd proto && buf generate
```

## Zero Dependencies

The generated `Validate()` method uses only the Go standard library (`fmt`, `regexp`, `unicode/utf8`). No runtime frameworks, no reflection, no external validation libraries.
