# TypeScript and Zod Support

`proto2type` provides robust TypeScript generation with Zod validation. It converts your Protocol Buffer definitions into fully typed TypeScript interfaces and Zod schemas, giving you strong typing at compile-time and strict schema validation at runtime.

## Quick Start

Add the plugin to your `buf.gen.yaml`:

```yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: gen/ts
    opt:
      - lang=typescript
      - validate=true
```

Then run `buf generate`.

## Options Reference

| Option | Default | Description |
|---|---|---|
| `lang` | `go` | Must be set to `typescript` to generate TS/Zod. |
| `validate` | `""` | Set to `true` to generate Zod constraints from `buf.validate` proto annotations. |
| `ts_types_only` | `false` | Emit plain TypeScript types without Zod (zero dependencies). Incompatible with `validate=true`. |
| `ts_int64` | `string` | Int64 representation: `string` (safe) or `bigint` (native). |
| `ts_enum_style` | `enum` | Enum style: `enum` (open `z.enum().or(...)`) or `native` (`z.nativeEnum()`). |
| `ts_explicit_types` | `true` | Emit explicit `interface` types alongside Zod schemas (improves IDE performance for large types). |
| `ts_strict` | `false` | Append `.strict()` to reject unknown fields per ProtoJSON spec. |
| `ts_zod_import` | `zod` | Zod import path (e.g. `zod/v4` or `@scope/zod`). |
| `ts_preset` | _none_ | Apply a preset configuration (e.g. `zod-strict` or `types-only`). |
| `ts_output_suffix` | `.type` | Output file suffix before `.ts` (e.g. `.schema` → `user.schema.ts`). |

## Generated Code Walkthrough

By default, the plugin generates Zod schemas and infers TypeScript types using `z.infer`.

### Messages and Types

Given a proto message, `proto2type` produces a Zod schema and its inferred type. Proto fields with defaults (like proto3 zeros) are properly defaulted via Zod's `.default()`.

```typescript
export const UserSchema = /* @__PURE__ */ z.object({
  id: z.string().default(""),
  email: z.string().default(""),
  active: z.boolean().default(false),
  age: z.number().int().default(0),
});
export type User = z.infer<typeof UserSchema>;
```

> **Note**: While the above shows `z.infer`, `proto2type` defaults to `ts_explicit_types=true`, which generates explicit `export interface User { ... }` types instead. This significantly improves IDE performance for large types.

### Enums

By default, enums are emitted as an open string literal union coupled with a Zod enum that falls back to `z.string()`. This allows safe parsing of string representations while preserving type hints.

```typescript
export type UserStatus = "Unspecified" | "Active" | "Suspended" | "Deleted" | (string & {});
export const UserStatusSchema = /* @__PURE__ */ z.enum(["Unspecified", "Active", "Suspended", "Deleted"]).or(z.string());
```

### Maps and Arrays

Maps and arrays are defaulted automatically to `{}` and `[]` respectively, matching the proto3 spec. Map keys are protected against prototype pollution attacks (`__proto__`, `constructor`, `prototype`).

## Framework Integration

Because the output uses standard Zod schemas, it integrates beautifully with the wider JS/TS ecosystem.

### React Hook Form

```typescript
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { UserSchema, type User } from "./gen/user.type";

// z.input = what the form collects (defaults are optional)
// z.output = what you get after parsing (defaults filled in)
type FormInput = z.input<typeof UserSchema>;

function UserProfile() {
  const { register, handleSubmit } = useForm<FormInput, unknown, User>({
    resolver: zodResolver(UserSchema)
  });
  
  // ...
}
```

> See [`examples/GUIDE.md`](../examples/GUIDE.md) for a complete walkthrough with React Hook Form, Next.js API routes, and tRPC.

### tRPC / API Routes

```typescript
import { UserSchema } from "./gen/user.type";

export const userRouter = router({
  createUser: publicProcedure
    .input(UserSchema)
    .mutation(async ({ input }) => {
      // input is fully typed as User
      // validation runs automatically
      return await db.users.create(input);
    }),
});
```

### Next.js (getServerSideProps)

Validate external API data against your proto contracts:

```typescript
export async function getServerSideProps() {
  const res = await fetch("https://api.example.com/users/1");
  const data = await res.json();
  
  // throws if invalid, matching strict proto definitions
  const user = UserSchema.parse(data);
  
  return { props: { user } };
}
```

## BigInt vs String Mode

By default, `int64`, `uint64`, `sint64`, etc. are emitted as `string` in TypeScript because JavaScript's `Number` loses precision for integers over `2^53 - 1`.

```typescript
// Default (string mode)
export const UserSchema = z.object({
  bigNumber: z.union([z.string().max(100).regex(/^-?\d+$/), z.number().refine(Number.isSafeInteger, { message: "integer out of safe range" })]).pipe(z.coerce.string()).default("0"),
});
```

You can enable native `bigint` mode using `ts_int64=bigint`:

```yaml
opt:
  - lang=typescript
  - ts_int64=bigint
```

```typescript
// BigInt mode
export const UserSchema = z.object({
  bigNumber: z.union([z.string().max(100).regex(/^-?\d+$/), z.number(), z.bigint()]).pipe(z.coerce.bigint()).default(0n),
});
```
> **Note**: Native `BigInt` values cannot be serialized with `JSON.stringify()` without providing a custom replacer. Consider your serialization layer before enabling this mode.

## Strict Mode

If you want your schemas to reject unknown fields (e.g. for strict ProtoJSON compliance or API gateways), set `ts_strict=true`:

```yaml
opt:
  - lang=typescript
  - ts_strict=true
```

This appends `.strict()` to all generated object schemas, failing validation if extra keys are present in the payload.

## Recursive Types

Recursive types are supported via `z.lazy()`. When a recursive type is detected, `proto2type` generates a manual type alias before the schema to help TypeScript infer it correctly without hitting circular reference errors.

```typescript
export type Category = {
  name?: string | null;
  parent?: Category | null;
  children?: Category[] | null;
};
export const CategorySchema: z.ZodType<Category> = /* @__PURE__ */ z.lazy(() => z.object({
  name: z.string().default(""),
  parent: CategorySchema.nullish(),
  children: z.array(CategorySchema).default(() => []),
}));
```

## Validation Constraints

When `validate=true` is set, `proto2type` converts `buf.validate` proto annotations directly into chained Zod constraints.

| buf.validate constraint | Zod equivalent |
|---|---|
| `string.len: 5` | `.refine(v => [...v].length === 5, ...)` |
| `string.min_len: 1` | `.refine(v => [...v].length >= 1, ...)` |
| `string.max_len: 255` | `.refine(v => [...v].length <= 255, ...)` |
| `string.pattern: "^[0-9]{5}$"` | `.regex(new RegExp("^[0-9]{5}$", "u"))` |
| `string.email: true` | `.email()` |
| `string.hostname: true` | `.refine(v => re.test(v), { message: "must be a valid hostname" })` |
| `string.ip: true` | `.refine(v => v === "" || z.string().ip().safeParse(v).success, ...)` |
| `string.prefix: "usr_"` | `.startsWith("usr_")` |
| `string.suffix: ".com"` | `.endsWith(".com")` |
| `string.contains: "@"` | `.includes("@")` |
| `string.const: "active"` | `.refine(v => v === "active", ...)` |
| `string.in: ["a", "b"]` | `.refine(v => ["a", "b"].includes(v), ...)` |
| `string.not_in: ["x"]` | `.refine(v => !["x"].includes(v), ...)` |
| `bytes.min_len: 2` | `.refine(v => ...test(v) && len >= 2, ...)` |
| `bytes.max_len: 255` | `.refine(v => ...test(v) && len <= 255, ...)` |
| `int32.const: 1` | `.refine(v => v === 1, ...)` |
| `int32.gt: 0` | `.gt(0)` |
| `int32.gte: 0` | `.gte(0)` |
| `int32.lt: 100` | `.lt(100)` |
| `int32.lte: 100` | `.lte(100)` |
| `int32.in: [1, 2]` | `.refine(v => [1, 2].includes(v), ...)` |
| `int32.not_in: [3]` | `.refine(v => ![3].includes(v), ...)` |
| `timestamp.gte: {...}` | `.refine(v => new Date(v).getTime() >= ...)` |
| `duration.gte: {...}` | `.refine(v => ... parseFloat(...) >= ...)` |
| `repeated.min_items: 1` | `.min(1)` |
| `repeated.max_items: 10` | `.max(10)` |
| `repeated.unique: true` | `.refine(v => new Set(v).size === v.length, ...)` |
| `map.min_pairs: 1` | `.refine(v => Object.keys(v).length >= 1, ...)` |
| `map.max_pairs: 10` | `.refine(v => Object.keys(v).length <= 10, ...)` |

Example generated code with constraints:

```typescript
export const AddressSchema = /* @__PURE__ */ z.object({
  street: z.string().refine(v => [...v].length >= 1, { message: "must be at least 1 characters" }).default(""),
  city: z.string().refine(v => [...v].length >= 1, { message: "must be at least 1 characters" }).default(""),
  state: z.string().refine(v => [...v].length >= 2, { message: "must be at least 2 characters" }).refine(v => [...v].length <= 2, { message: "must be at most 2 characters" }).default(""),
  zip: z.string().regex(new RegExp("^[0-9]{5}(-[0-9]{4})?$", "u"), { message: "must match pattern" }).default(""),
  country: z.string().default(""),
});
```

## Well-Known Types

Well-Known Types (WKTs) are natively mapped to idiomatic Zod constructs with appropriate constraints.

| WKT | Zod equivalent |
|---|---|
| `google.protobuf.Timestamp` | `z.string().datetime({ offset: true })` |
| `google.protobuf.Duration` | `z.string().regex(/^-?[0-9]+(\.[0-9]+)?s$/)` |
| `google.protobuf.Struct` | `z.record(_safeKey, z.unknown())` |
| `google.protobuf.Value` | `z.unknown()` |
| `google.protobuf.ListValue` | `z.array(z.unknown())` |
| `google.protobuf.Empty` | `z.record(z.string(), z.never())` |
| `google.protobuf.Any` | `z.object({ "@type": z.string() }).passthrough()` |
| `google.protobuf.FieldMask` | `z.string().regex(...)` (comma-separated field paths) |
| `google.protobuf.BoolValue` | `z.boolean().nullable()` |
| `google.protobuf.Int32Value` | `z.number().int().nullable()` |
| `google.protobuf.Int64Value` | `z.string().nullable()` (or `z.bigint()` with `ts_int64=bigint`) |
| `google.protobuf.StringValue` | `z.string().nullable()` |
| `google.protobuf.DoubleValue` | `z.number().nullable()` |
| `google.protobuf.FloatValue` | `z.number().nullable()` |
| `google.protobuf.BytesValue` | `z.string().nullable()` |

## Known Limitations

- **ReDoS (Regular Expression Denial of Service)**: The JavaScript `RegExp` engine uses backtracking. `proto2type` detects and rejects common ReDoS patterns (adjacent unbounded quantifiers, overlapping character classes), but exotic patterns may still slip through. Review your proto regex patterns for ReDoS vulnerabilities.
- **NullValue**: The `google.protobuf.NullValue` enum is mapped directly to `z.null()`.
- **Proto2**: `proto2` syntax is not fully supported (no explicit required/optional distinction beyond standard proto3 rules).
- **Enum Type Widening**: By design, enums accept `string` (or `number` in some configurations) to be robust against unknown future enum values. They may accept values at runtime that are not explicit members of the string literal union.
- **`google.protobuf.Any`**: Mapped to `z.object({ "@type": z.string() }).passthrough()` — requires the `@type` URL but passes through all other fields unvalidated.
