# Migration Guide

## v0.6.x → v0.7.0

v0.7.0 includes breaking changes to generated output for TypeScript, Kotlin, and Rust backends. Regenerate all output files after upgrading.

### Breaking Changes

#### TypeScript: `.optional()` → `.nullish()`

All optional proto3 fields now generate `.nullish()` instead of `.optional()`. This means fields accept both `undefined` **and** `null`, matching how JSON serializers (including `protobuf-ts`, `ts-proto`, `connect-es`) encode absent fields.

**Before (v0.6.x):**
```typescript
email: z.string().email().optional(),
```

**After (v0.7.0):**
```typescript
email: z.string().email().nullish(),
```

**Action required:** If your code relies on `typeof field === 'undefined'` to detect absent fields, switch to `field == null` (which catches both `null` and `undefined`).

---

#### TypeScript: Unicode-aware string length

String length validation now uses `[...v].length` (counts Unicode scalar values) instead of `.min(N)` / `.max(N)` (counts UTF-16 code units). Multi-byte characters like emoji are now counted correctly.

**Before:** `z.string().min(3).max(100)`
**After:** `z.string().refine(v => [...v].length >= 3, ...).refine(v => [...v].length <= 100, ...)`

**Action required:** If you have strings with emoji or CJK characters near the length boundary, validation results may change.

---

#### TypeScript: RegExp Unicode flag

All generated `new RegExp(...)` calls now include the `"u"` flag for proper Unicode support.

**Before:** `new RegExp("^[a-z]+$")`
**After:** `new RegExp("^[a-z]+$", "u")`

**Action required:** If your regex patterns use syntax incompatible with Unicode mode (e.g. bare `]` or `{`), update the patterns in your `.proto` files.

---

#### Kotlin: Unicode-aware string length

String length validation now uses `.codePointCount(0, s.length)` instead of `.length`. Consistent with TypeScript behavior.

---

#### Kotlin: Regex constants hoisted

Regex patterns are now compiled once as file-level `private val` constants instead of per-call `Regex("...")`. No API change, but generated output differs.

---

#### Rust: `ValidateEmail` trait import

Generated Rust files with email validation now import `validator::{Validate, ValidateEmail}` instead of just `validator::Validate`.

---

#### Option renames (Rust)

| Old name | New name |
|---|---|
| `buffa_module` | `rust_buffa_module` |
| `buffa_oneof_prefix` | `rust_buffa_oneof_prefix` |
| `domain_module` | `rust_domain_module` |

**Action required:** Update your `buf.gen.yaml` option names.

---

### Non-Breaking Improvements

- **Security:** Rust regex injection fix (raw string delimiters), expanded ReDoS detection, oneof scalar constraint validation
- **Performance:** Hoisted regex constants, precomputed cycle detection, deduplicated prototype pollution checks
- **Correctness:** WKT Duration bounds, Rust oneof validation, TS constraint ordering
