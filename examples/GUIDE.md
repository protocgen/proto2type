# Examples Guide

## TypeScript + Zod: Frontend Form Validation

This guide shows how to use `proto2type` to generate Zod schemas from your `.proto` files, giving you **type-safe runtime validation** that stays in sync with your API contract.

### 1. Define your proto with validation rules

```protobuf
// user.proto
syntax = "proto3";
package myapp.v1;

import "buf/validate/validate.proto";
import "google/protobuf/timestamp.proto";

message CreateUserRequest {
  // Email must be valid format
  string email = 1 [(buf.validate.field).string.email = true];

  // Display name: 1–100 characters
  string display_name = 2 [(buf.validate.field).string = {
    min_len: 1,
    max_len: 100
  }];

  // Age: 13–120 (e.g. minimum age requirement)
  int32 age = 3 [(buf.validate.field).int32 = {gte: 13, lte: 120}];

  // Bio is optional, max 500 chars when provided
  optional string bio = 4 [(buf.validate.field).string.max_len = 500];

  // At least one role required
  repeated string roles = 5 [(buf.validate.field).repeated = {
    min_items: 1,
    max_items: 5
  }];

  // Nested address with its own constraints
  Address address = 6;
}

message Address {
  string street = 1 [(buf.validate.field).string.min_len = 1];
  string city   = 2 [(buf.validate.field).string.min_len = 1];
  string state  = 3 [(buf.validate.field).string = {min_len: 2, max_len: 2}];
  string zip    = 4 [(buf.validate.field).string.pattern = "^[0-9]{5}(-[0-9]{4})?$"];
}
```

### 2. Configure buf to generate TypeScript + Zod

```yaml
# buf.gen.yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: src/gen
    opt:
      - lang=typescript
      - validate=true
```

Then run:

```bash
buf generate
```

### 3. Generated output

`proto2type` generates a single `.type.ts` file with Zod schemas that encode every validation rule:

```typescript
// src/gen/user.type.ts (generated — do not edit)
import { z } from "zod";

export const AddressSchema = z.object({
  street: z.string()
    .refine(v => [...v].length >= 1, { message: "must be at least 1 characters" })
    .default(""),
  city: z.string()
    .refine(v => [...v].length >= 1, { message: "must be at least 1 characters" })
    .default(""),
  state: z.string()
    .refine(v => [...v].length >= 2, { message: "must be at least 2 characters" })
    .refine(v => [...v].length <= 2, { message: "must be at most 2 characters" })
    .default(""),
  zip: z.string()
    .regex(new RegExp("^[0-9]{5}(-[0-9]{4})?$", "u"), { message: "must match pattern" })
    .default(""),
});
export type Address = z.infer<typeof AddressSchema>;

export const CreateUserRequestSchema = z.object({
  email: z.string().email().default(""),
  displayName: z.string()
    .refine(v => [...v].length >= 1, { message: "must be at least 1 characters" })
    .refine(v => [...v].length <= 100, { message: "must be at most 100 characters" })
    .default(""),
  age: z.number().int().gte(13).lte(120).default(0),
  bio: z.string()
    .refine(v => [...v].length <= 500, { message: "must be at most 500 characters" })
    .nullish(),
  roles: z.array(z.string()).min(1).max(5).default(() => []),
  address: AddressSchema.nullish(),
});
export type CreateUserRequest = z.infer<typeof CreateUserRequestSchema>;
```

**Key things to notice:**
- String lengths use `[...v].length` for correct Unicode counting (emoji = 1 character)
- Optional proto3 fields use `.nullish()` (accepts both `null` and `undefined`)
- Nested messages are composed (`AddressSchema.nullish()`)
- Proto3 defaults are populated (`.default("")`, `.default(0)`)
- Regex patterns get the `"u"` flag for Unicode support

### 4. Use in React with React Hook Form

```bash
npm install react-hook-form @hookform/resolvers zod
```

```tsx
// src/components/CreateUserForm.tsx
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import {
  CreateUserRequestSchema,
  type CreateUserRequest,
} from "../gen/user.type";
import { z } from "zod";

// z.input = what the form collects (defaults are optional)
// z.output = what you get after parsing (defaults filled in)
type FormInput = z.input<typeof CreateUserRequestSchema>;

export function CreateUserForm() {
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<FormInput, unknown, CreateUserRequest>({
    resolver: zodResolver(CreateUserRequestSchema),
    defaultValues: {
      roles: ["user"], // min 1 role required
    },
  });

  const onSubmit = (data: CreateUserRequest) => {
    // data is fully validated and typed — defaults populated by Zod
    fetch("/api/users", {
      method: "POST",
      body: JSON.stringify(data),
    });
  };

  return (
    <form onSubmit={handleSubmit(onSubmit)}>
      <input {...register("email")} placeholder="Email" />
      {errors.email && <span>{errors.email.message}</span>}

      <input {...register("displayName")} placeholder="Display Name" />
      {errors.displayName && <span>{errors.displayName.message}</span>}

      <input {...register("age", { valueAsNumber: true })} type="number" />
      {errors.age && <span>{errors.age.message}</span>}

      <textarea {...register("bio")} placeholder="Bio (optional)" />
      {errors.bio && <span>{errors.bio.message}</span>}

      {/* roles has min 1 — provide a default or a multi-select */}
      <input {...register("roles.0")} placeholder="Primary role" />
      {errors.roles && <span>{errors.roles.message}</span>}

      <button type="submit">Create User</button>
    </form>
  );
}
```

### 5. Use in a Next.js API route (server-side)

```typescript
// src/app/api/users/route.ts
import { CreateUserRequestSchema } from "@/gen/user.type";

export async function POST(request: Request) {
  let body: unknown;
  try {
    body = await request.json();
  } catch {
    return Response.json({ error: "invalid JSON" }, { status: 400 });
  }

  // Validate with the same schema used on the frontend
  const result = CreateUserRequestSchema.safeParse(body);

  if (!result.success) {
    return Response.json(
      { errors: result.error.flatten().fieldErrors },
      { status: 400 }
    );
  }

  // result.data is typed and validated
  const user = await db.users.create(result.data);
  return Response.json(user, { status: 201 });
}
```

### 6. Use with tRPC

```typescript
// src/server/trpc.ts
import { CreateUserRequestSchema } from "../gen/user.type";

export const userRouter = router({
  create: publicProcedure
    .input(CreateUserRequestSchema)
    .mutation(async ({ input }) => {
      // input is fully typed as CreateUserRequest
      return db.users.create(input);
    }),
});
```

---

## Validation Rules Reference

| Proto rule | Zod output | Example |
|---|---|---|
| `string.email = true` | `.email()` | Valid email format |
| `string.min_len = N` | `.refine(v => [...v].length >= N, ...)` | Unicode-aware min length |
| `string.max_len = N` | `.refine(v => [...v].length <= N, ...)` | Unicode-aware max length |
| `string.pattern = "..."` | `.regex(new RegExp("...", "u"), ...)` | Regex with Unicode flag |
| `string.uuid = true` | `.uuid()` | UUID format |
| `string.uri = true` | `.url().refine(...)` | URL with scheme check |
| `int32.gte = N` | `.gte(N)` | Greater than or equal |
| `int32.lte = N` | `.lte(N)` | Less than or equal |
| `int32.gt = N` | `.gt(N)` | Strictly greater than |
| `int32.lt = N` | `.lt(N)` | Strictly less than |
| `repeated.min_items = N` | `.min(N)` | Minimum array length |
| `repeated.max_items = N` | `.max(N)` | Maximum array length |
| `optional` field | `.nullish()` | Accepts `null` \| `undefined` |
| `oneof` | `.superRefine(...)` | Mutual exclusion check |
| Nested `message` | `NestedSchema.nullish()` | Recursive Zod composition |

---

## Multi-Language Comparison

The same `CreateUserRequest` proto generates validation in every language:

### TypeScript (Zod)

```typescript
email: z.string().email().default("")
age: z.number().int().gte(13).lte(120).default(0)
```

### Kotlin (native)

```kotlin
fun CreateUserRequest.validate(): List<String> {
    val errors = mutableListOf<String>()
    if (email.isNotEmpty() && !email.matches(RE_CREATE_USER_REQUEST_EMAIL_EMAIL))
        errors.add("email must be a valid email")
    if (age < 13) errors.add("age must be >= 13")
    if (age > 120) errors.add("age must be <= 120")
    return errors
}
```

### Rust (validator crate)

```rust
#[derive(Validate)]
pub struct CreateUserRequest {
    #[validate(email)]
    pub email: String,
    #[validate(range(min = 13, max = 120))]
    pub age: i32,
}
```

### Python (Pydantic)

```python
class CreateUserRequest(BaseModel):
    model_config = ConfigDict(validate_default=True)

    email: str = Field(pattern=r"^[^@]+@[^@]+\.[^@]+$")
    age: int = Field(ge=13, le=120)
```

---

## Configuration Quick Reference

```yaml
# Minimal: types only (no Zod, no validation)
- lang=typescript
- ts_types_only=true

# Full: Zod schemas + validation
- lang=typescript
- validate=true

# Strict mode: reject unknown fields
- lang=typescript
- validate=true
- ts_strict=true

# Preset shorthand
- lang=typescript
- ts_preset=zod-strict    # = validate + strict + explicit types
```

See [README.md](../README.md) for the complete options reference.

