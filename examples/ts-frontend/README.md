# TypeScript Frontend Example

**Same proto, same constraints, two languages.**

This example pairs with the [Go API Server](../go-api-server/) to show the cross-language story: one `.proto` file generates both a Go backend with `Validate()` and a TypeScript frontend with Zod schemas — sharing the same validation rules.

## The Cross-Language Pitch

```
                    user.proto
                   (one source of truth)
                        │
              ┌─────────┴─────────┐
              ▼                   ▼
      Go API Server          TS Frontend
   domain.User.Validate()   UserSchema (Zod)
   email, age, role checks   email, age, role checks
              │                   │
              └─────────┬─────────┘
                        │
              Constraints never drift.
```

**Before proto2type:** You write validation in Go, then rewrite it in TypeScript, then they drift apart, and one day a user submits `age: -5` because the frontend allows it but the backend doesn't.

**After proto2type:** Define once in proto. Generate both. They can't drift.

## Quick Start

```bash
# Terminal 1: Start the Go API server
cd ../go-api-server && go run .

# Terminal 2: Start the frontend dev server
npm install
npm run dev
```

Open http://localhost:5173 — try submitting invalid data. The Zod schema catches it client-side with the same rules the Go server enforces server-side.

## How It Works

The form uses [React Hook Form](https://react-hook-form.com/) with [`@hookform/resolvers`](https://github.com/react-hook-form/resolvers) to wire the generated Zod schema directly into the form:

```tsx
import { UserSchema } from "./gen/user/v1/user.type";  // ← generated

const { register, handleSubmit, formState: { errors } } = useForm({
  resolver: zodResolver(UserSchema),  // ← one line, all validation
});
```

That's it. Every field error message comes from the Zod schema, which was generated from the proto constraints. The form component doesn't know or care what the rules are.

## What to Look At

| File | What it shows |
|------|--------------|
| [`proto/user/v1/user.proto`](proto/user/v1/user.proto) | Same proto as the Go example — email, min/max length, age range, defined_only enum |
| [`src/gen/user/v1/user.type.ts`](src/gen/user/v1/user.type.ts) | Generated: `UserSchema` (Zod), `User` (TypeScript interface), `UserRole` enum |
| [`src/App.tsx`](src/App.tsx) | React form wired to generated schema, posts to Go API |
| [`vite.config.ts`](vite.config.ts) | Dev server proxies `/users` to Go server on :8080 |

## Shared Proto

Both examples use the **exact same** `user.proto`:

```protobuf
message User {
  string email = 1        [(buf.validate.field).string.email = true];
  string display_name = 2 [(buf.validate.field).string = {min_len: 1, max_len: 100}];
  int32 age = 3            [(buf.validate.field).int32 = {gte: 13, lte: 120}];
  UserRole role = 4        [(buf.validate.field).enum.defined_only = true];
  optional string bio = 5  [(buf.validate.field).string.max_len = 500];
}
```

Go generates `user.Validate()`. TypeScript generates `UserSchema.parse()`. Same rules. Zero drift.

## Known Limitations

The Go `validate=native` backend treats `string.email` with IgnoreEmpty semantics — an empty email passes validation. The TypeScript/Zod backend always runs `z.string().email()`, so an empty email is rejected. This edge case is tracked upstream and does not affect the form UX since the HTML `type="email"` attribute also requires a value.

## Regenerating

```bash
cd proto && buf generate
```

Requires `protoc-gen-proto2type` on your `$PATH`:

```bash
go install github.com/protocgen/proto2type@latest
# Ensure the Go bin directory is on your PATH:
export PATH="$(go env GOBIN 2>/dev/null | grep . || echo "$(go env GOPATH)/bin"):$PATH"
```
