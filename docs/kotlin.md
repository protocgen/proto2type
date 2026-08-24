# Kotlin Support

`proto2type` provides Kotlin data class generation with zero-dependency validation. It converts your Protocol Buffer definitions into fully typed `kotlinx.serialization` data classes, giving you idiomatic Kotlin APIs while preserving exact proto constraints and behaviors.

## Quick Start

Add the plugin to your `buf.gen.yaml`:

```yaml
version: v2
plugins:
  - local: protoc-gen-proto2type
    out: gen/kotlin
    opt:
      - lang=kotlin
      - validate=true
```

Then run `buf generate`.

## Options Reference

Kotlin generation supports the following options via `buf.gen.yaml`:

| Option | Default | Description |
|---|---|---|
| `lang` | `go` | Must be set to `kotlin` to generate Kotlin code. |
| `validate` | `""` | Set to `true` (or `native`) to generate native Kotlin validation functions from `buf.validate` proto annotations. |
| `omitempty_default` | `true` | When true, optional, repeated, map, and message fields default to `null` or empty collections. |
| `enum_as_string` | `false` | Not supported for Kotlin (enums are always emitted as `@Serializable enum class`). |

> For a complete list of options, see the [Configuration Reference](../CONFIG.md).

## Dependencies Setup

Generated code relies on standard KotlinX libraries. Add the following to your `build.gradle.kts`:

```kotlin
plugins {
    kotlin("jvm") version "2.1.21"
    kotlin("plugin.serialization") version "2.1.21"
}

dependencies {
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.8.1")
    implementation("org.jetbrains.kotlinx:kotlinx-datetime:0.6.2")
}
```

## Generated Code Walkthrough

By default, the plugin generates `@Serializable` data classes and infers the correct property types, defaults, and serial names to match the protobuf JSON mapping spec.

### Messages and Types

Given a proto message, `proto2type` produces a data class. Proto fields with defaults (like proto3 zeros) are properly defaulted.

```kotlin
/** User represents a user account. */
@Serializable
data class User(
    val id: String = "",
    val email: String = "",
    @SerialName("display_name") val displayName: String = "",
    val active: Boolean = false,
    val age: Int = 0
)
```

### Enums

Enums are emitted as `@Serializable enum class` constructs. A companion object with a `fromValue(value: Int)` function is generated for robust conversion from the integer proto values.

```kotlin
/** UserStatus represents the user's account status. */
@Serializable
enum class UserStatus {
    @SerialName("USER_STATUS_UNSPECIFIED") UNSPECIFIED,
    @SerialName("USER_STATUS_ACTIVE") ACTIVE,
    @SerialName("USER_STATUS_SUSPENDED") SUSPENDED,
    @SerialName("USER_STATUS_DELETED") DELETED;

    companion object {
        fun fromValue(value: Int): UserStatus? = when(value) {
            0 -> UNSPECIFIED
            1 -> ACTIVE
            2 -> SUSPENDED
            3 -> DELETED
            else -> null
        }
    }
}
```

### Oneofs

Oneof fields are elegantly modeled using a `@Serializable sealed class` hierarchy. This approach embraces Kotlin's sealed classes to guarantee exhaustive `when` matching while remaining fully serializable with polymorphic JSON formatting.

```kotlin
/**
 * Oneof group: contact_method.
 * Serialized using kotlinx.serialization polymorphic format (not protobuf JSON).
 */
@Serializable
sealed class UserContactMethod {
    @Serializable
    @SerialName("contact_email")
    data class ContactEmail(val value: String) : UserContactMethod()
    @Serializable
    @SerialName("contact_phone")
    data class ContactPhone(val value: String) : UserContactMethod()
}
```

### Keyword Escaping

When proto fields conflict with Kotlin/Java keywords (like `super` or `class`), the generator safely escapes them using backticks so they remain valid Kotlin properties.

```kotlin
@Serializable
data class KeywordFields(
    @SerialName("super") val `super`: String = "",
    val cls: Boolean = false
)
```

## Validation Constraints

When `validate=true` is set, `proto2type` converts `buf.validate` proto annotations directly into pure, native Kotlin validation checks. 

This approach has **zero external dependencies** and emits two methods per class:
1. `validate(): List<String>` – returns a list of error strings (empty if valid).
2. `validateOrThrow()` – throws an `IllegalStateException` on the first error.

```kotlin
private val RE_ADDRESS_ZIP_PATTERN = Regex("^[0-9]{5}(-[0-9]{4})?$")

/** Validates constraints from buf.validate annotations. Returns a list of error messages (empty = valid). */
fun Address.validate(): List<String> {
    val errors = mutableListOf<String>()
    if (street.codePointCount(0, street.length) < 1) errors.add("street must be at least 1 characters")
    if (city.codePointCount(0, city.length) < 1) errors.add("city must be at least 1 characters")
    if (state.codePointCount(0, state.length) < 2) errors.add("state must be at least 2 characters")
    if (state.codePointCount(0, state.length) > 2) errors.add("state must be at most 2 characters")
    if (!zip.matches(RE_ADDRESS_ZIP_PATTERN)) errors.add("zip must match pattern: ^[0-9]{5}(-[0-9]{4})?$")
    return errors
}

/** Validates constraints and throws [IllegalStateException] if any fail. */
fun Address.validateOrThrow() {
    val errors = validate()
    if (errors.isNotEmpty()) {
        throw IllegalStateException("Address: validation failed: " + errors.joinToString("; "))
    }
}
```

### Propagation and Null Safety

Validation calls automatically propagate through nested types using Kotlin's `?.let` null safety operators. This prevents `NullPointerException` crashes while ensuring complete deep validation. Regular expressions are also hoisted to file-level private constants (`RE_...`) so they compile only once per class rather than on every check.

| buf.validate constraint | Kotlin equivalent check |
|---|---|
| `string.min_len: 1` | `v.codePointCount(0, v.length) < 1` |
| `string.max_len: 255` | `v.codePointCount(0, v.length) > 255` |
| `string.pattern: "..."` | `!v.matches(RE_PATTERN)` |
| `string.email: true` | `!v.matches(RE_EMAIL)` |
| `int32.gt: 0` | `v <= 0` |
| `int32.gte: 0` | `v < 0` |
| `int32.lt: 100` | `v >= 100` |
| `int32.lte: 100` | `v > 100` |
| `repeated.min_items: 1` | `v.size < 1` |
| `repeated.max_items: 10` | `v.size > 10` |

## Well-Known Types

Well-Known Types (WKTs) are mapped directly to native Kotlin or `kotlinx.serialization` equivalents. 

| WKT | Kotlin equivalent | Default Value |
|---|---|---|
| `google.protobuf.Timestamp` | `kotlinx.datetime.Instant` | `Instant.fromEpochSeconds(0)` |
| `google.protobuf.Duration` | `kotlin.time.Duration` | `Duration.ZERO` |
| `google.protobuf.Struct` | `Map<String, JsonElement>` | `emptyMap()` |
| `google.protobuf.Value` | `JsonElement` | `JsonNull` |
| `google.protobuf.ListValue` | `JsonArray` | `JsonArray(emptyList())` |
| `google.protobuf.Empty` | _Omitted or Empty Class_ | |
| `google.protobuf.Any` | `JsonElement` | `JsonNull` |
| `google.protobuf.FieldMask` | `List<String>` | `emptyList()` |
| `google.protobuf.BoolValue` | `Boolean?` | `null` |
| `google.protobuf.Int32Value` | `Int?` | `null` |
| `google.protobuf.Int64Value` | `Long?` | `null` |
| `google.protobuf.StringValue` | `String?` | `null` |
| `google.protobuf.DoubleValue` | `Double?` | `null` |
| `google.protobuf.FloatValue` | `Float?` | `null` |
| `google.protobuf.BytesValue` | `ByteArray?` | `null` |

> **Note**: `Duration`, `FieldMask`, and `BytesValue` require custom `@Serializer` implementations to match ProtoJSON wire format (Duration as `"1.5s"`, FieldMask as `"field1,field2"`, BytesValue as base64). The default `kotlinx.serialization` encoding differs from ProtoJSON.

## Framework Integration

Generated models are standard Kotlin `data class` types decorated with `@Serializable`, making them exceptionally portable across JVM and multiplatform targets. 

### Spring Boot

Add the `kotlinx-serialization` HTTP message converter to your Spring Web config to seamlessly accept proto-defined requests directly in `@RestController` routes. Add a `@ControllerAdvice` to intercept `IllegalStateException` and return automatic 400 Bad Requests when `validateOrThrow()` fails.

### Ktor

Because Ktor is natively built around `kotlinx.serialization`, you can directly install `ContentNegotiation` with `json()`.

```kotlin
install(ContentNegotiation) {
    json()
}

routing {
    post("/users") {
        val req = call.receive<User>()
        req.validateOrThrow()
        // ... proceed with robust, validated data
    }
}
```

Make sure to include the required dependencies in your build configuration:
```kotlin
implementation("io.ktor:ktor-server-content-negotiation:$ktor_version")
implementation("io.ktor:ktor-serialization-kotlinx-json:$ktor_version")
```

## Known Limitations

- **ByteArray Equality**: In Kotlin, `data class` generation for `ByteArray` fields relies on referential equality (`==`). To compare the contents of a byte array, you must manually use `contentEquals()`. A comment is generated on `ByteArray` properties to remind developers of this behavior.
- **Unsigned Integers**: `uint32` maps to `Int` (max 2^31-1 instead of 2^32-1), `uint64` maps to `Long` (max 2^63-1 instead of 2^64-1). Values above these limits will overflow. Kotlin's inline unsigned classes (`UInt`, `ULong`) introduce reflection serialization edge cases when interacting with Java interop.
- **No CEL Constraints**: `buf.validate` custom CEL expressions are not supported in native Kotlin generation. Only standard rules (string lengths, regex patterns, numeric ranges) are hoisted into validation code.
- **Oneof JSON Formatting**: Oneof structures use `kotlinx.serialization` polymorphic structure (e.g. wrapper classes). This may not strictly map to idiomatic protobuf JSON encoding out of the box without a custom `Json` formatting configuration.
