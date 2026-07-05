package test.v1

import kotlin.test.*
import kotlin.time.Duration.Companion.hours
import kotlinx.datetime.*
import kotlinx.serialization.*
import kotlinx.serialization.json.*

/**
 * Integration tests for proto2type-generated Kotlin WKT serialization.
 *
 * Validates roundtrip (encode → decode → assertEquals) for every well-known
 * type mapping on [User], including:
 * - Scalar fields (basic roundtrip & default zero values)
 * - google.protobuf.Value  → JsonElement  (singleValue, values, valueMap)
 * - google.protobuf.Struct → Map<String, JsonElement> (extraMetadata, structs, configs)
 * - google.protobuf.ListValue → JsonArray (preferences, lists)
 * - google.protobuf.FieldMask → List<String> (updateMask, fieldMasks)
 * - Wrapper map values (labels: String?, scores: Long?)
 * - google.protobuf.Timestamp map → Map<String, Instant> (eventTimes)
 * - google.protobuf.Duration → Duration (sessionTimeout)
 * - Oneof sealed class (UserContactMethod)
 * - Nested message (Address)
 */
class WktSerializationTest {

    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
    }

    // -----------------------------------------------------------------------
    // Helpers
    // -----------------------------------------------------------------------

    /** Encodes [value] to JSON and decodes it back, returning the decoded instance. */
    private inline fun <reified T> roundtrip(value: T): T =
        json.decodeFromString(json.encodeToString(value))

    /** Creates a minimal valid User for scalar-field tests. */
    private fun scalarUser() = User(
        id = "u-42",
        email = "test@example.com",
        displayName = "Test User",
        active = true,
        age = 28,
        roles = listOf("viewer"),
        metadata = mapOf("tier" to "free"),
    )

    // -----------------------------------------------------------------------
    // 1. Basic scalar roundtrip
    // -----------------------------------------------------------------------

    @Test
    fun `basic scalar fields survive roundtrip`() {
        val user = scalarUser()
        val decoded = roundtrip(user)

        assertEquals(user.id, decoded.id)
        assertEquals(user.email, decoded.email)
        assertEquals(user.displayName, decoded.displayName)
        assertEquals(user.active, decoded.active)
        assertEquals(user.age, decoded.age)
        assertEquals(user.roles, decoded.roles)
        assertEquals(user.metadata, decoded.metadata)
    }

    // -----------------------------------------------------------------------
    // 2. Default zero values
    // -----------------------------------------------------------------------

    @Test
    fun `default User has correct zero values`() {
        val user = User()

        assertEquals("", user.id)
        assertEquals("", user.email)
        assertEquals("", user.displayName)
        assertFalse(user.active)
        assertEquals(0, user.age)
        assertEquals(emptyList(), user.roles)
        assertEquals(emptyMap(), user.metadata)
        assertNull(user.address)
        assertEquals(Instant.fromEpochSeconds(0), user.createdAt)
        assertEquals(kotlin.time.Duration.ZERO, user.sessionTimeout)
        assertNull(user.phone)
        assertContentEquals(byteArrayOf(), user.avatar)
        assertNull(user.nickname)
        assertEquals(UserStatus.UNSPECIFIED, user.status)
        assertNull(user.contactMethod)
        assertEquals(emptyList(), user.tags)
        assertNull(user.deletedAt)
        assertNull(user.previousStatus)
        assertEquals(emptyList(), user.updateMask)
        assertEquals(emptyMap(), user.extraMetadata)
        assertEquals(JsonArray(emptyList()), user.preferences)
        assertNull(user.avatarThumbnail)
        assertEquals(emptyList(), user.fieldMasks)
        assertEquals(emptyList(), user.structs)
        assertEquals(emptyList(), user.lists)
        assertEquals(emptyMap(), user.eventTimes)
        assertEquals(emptyMap(), user.configs)
        assertEquals(JsonNull, user.singleValue)
        assertEquals(emptyList(), user.values)
        assertEquals(emptyMap(), user.valueMap)
        assertEquals(emptyMap(), user.labels)
        assertEquals(emptyMap(), user.scores)
    }

    @Test
    fun `default User roundtrips correctly`() {
        // A default-constructed User should survive encode→decode unchanged
        // (ByteArray fields compared separately via contentEquals because
        //  data class equals uses referential equality for arrays).
        val user = User()
        val decoded = roundtrip(user)
        val emptyBytes = byteArrayOf()
        assertEquals(
            user.copy(avatar = emptyBytes, avatarThumbnail = null),
            decoded.copy(avatar = emptyBytes, avatarThumbnail = null),
        )
        assertContentEquals(user.avatar, decoded.avatar)
    }

    // -----------------------------------------------------------------------
    // 3. Value fields (JsonElement)
    // -----------------------------------------------------------------------

    @Test
    fun `singleValue defaults to JsonNull`() {
        assertEquals(JsonNull, User().singleValue)
        val decoded = roundtrip(User())
        assertEquals(JsonNull, decoded.singleValue)
    }

    @Test
    fun `singleValue handles JsonPrimitive`() {
        val user = User(singleValue = JsonPrimitive("hello"))
        val decoded = roundtrip(user)
        assertEquals(JsonPrimitive("hello"), decoded.singleValue)
    }

    @Test
    fun `singleValue handles JsonPrimitive number`() {
        val user = User(singleValue = JsonPrimitive(42))
        val decoded = roundtrip(user)
        assertEquals(JsonPrimitive(42), decoded.singleValue)
    }

    @Test
    fun `singleValue handles JsonPrimitive boolean`() {
        val user = User(singleValue = JsonPrimitive(true))
        val decoded = roundtrip(user)
        assertEquals(JsonPrimitive(true), decoded.singleValue)
    }

    @Test
    fun `singleValue handles JsonObject`() {
        val obj = buildJsonObject {
            put("nested", JsonPrimitive("value"))
            put("count", JsonPrimitive(7))
        }
        val user = User(singleValue = obj)
        val decoded = roundtrip(user)
        assertEquals(obj, decoded.singleValue)
    }

    @Test
    fun `singleValue handles JsonArray`() {
        val arr = buildJsonArray {
            add(JsonPrimitive(1))
            add(JsonPrimitive("two"))
            add(JsonNull)
        }
        val user = User(singleValue = arr)
        val decoded = roundtrip(user)
        assertEquals(arr, decoded.singleValue)
    }

    @Test
    fun `values list roundtrips mixed JsonElements`() {
        val elements = listOf(
            JsonPrimitive("str"),
            JsonPrimitive(123),
            JsonPrimitive(false),
            JsonNull,
            buildJsonObject { put("k", JsonPrimitive("v")) },
            buildJsonArray { add(JsonPrimitive(1)) },
        )
        val user = User(values = elements)
        val decoded = roundtrip(user)
        assertEquals(elements, decoded.values)
    }

    @Test
    fun `valueMap roundtrips heterogeneous values`() {
        val map = mapOf(
            "str" to JsonPrimitive("hello") as JsonElement,
            "num" to JsonPrimitive(99) as JsonElement,
            "bool" to JsonPrimitive(true) as JsonElement,
            "nil" to JsonNull as JsonElement,
            "obj" to buildJsonObject { put("a", JsonPrimitive(1)) } as JsonElement,
            "arr" to buildJsonArray { add(JsonPrimitive(2)) } as JsonElement,
        )
        val user = User(valueMap = map)
        val decoded = roundtrip(user)
        assertEquals(map, decoded.valueMap)
    }

    // -----------------------------------------------------------------------
    // 4. Struct fields (Map<String, JsonElement>)
    // -----------------------------------------------------------------------

    @Test
    fun `extraMetadata roundtrips`() {
        val meta = mapOf(
            "source" to JsonPrimitive("api") as JsonElement,
            "version" to JsonPrimitive(3) as JsonElement,
            "nested" to buildJsonObject {
                put("deep", JsonPrimitive(true))
            } as JsonElement,
        )
        val user = User(extraMetadata = meta)
        val decoded = roundtrip(user)
        assertEquals(meta, decoded.extraMetadata)
    }

    @Test
    fun `structs list roundtrips`() {
        val s1 = mapOf("x" to JsonPrimitive(1) as JsonElement)
        val s2 = mapOf(
            "y" to JsonPrimitive("two") as JsonElement,
            "z" to JsonNull as JsonElement,
        )
        val user = User(structs = listOf(s1, s2))
        val decoded = roundtrip(user)
        assertEquals(listOf(s1, s2), decoded.structs)
    }

    @Test
    fun `configs map of structs roundtrips`() {
        val cfgs = mapOf(
            "db" to mapOf("host" to JsonPrimitive("localhost") as JsonElement, "port" to JsonPrimitive(5432) as JsonElement),
            "cache" to mapOf("ttl" to JsonPrimitive(300) as JsonElement),
        )
        val user = User(configs = cfgs)
        val decoded = roundtrip(user)
        assertEquals(cfgs, decoded.configs)
    }

    // -----------------------------------------------------------------------
    // 5. ListValue fields (JsonArray)
    // -----------------------------------------------------------------------

    @Test
    fun `preferences roundtrips`() {
        val prefs = buildJsonArray {
            add(JsonPrimitive("dark_mode"))
            add(JsonPrimitive(true))
            add(JsonPrimitive(42))
        }
        val user = User(preferences = prefs)
        val decoded = roundtrip(user)
        assertEquals(prefs, decoded.preferences)
    }

    @Test
    fun `lists field roundtrips multiple JsonArrays`() {
        val l1 = buildJsonArray { add(JsonPrimitive("a")); add(JsonPrimitive("b")) }
        val l2 = buildJsonArray { add(JsonPrimitive(1)); add(JsonNull) }
        val user = User(lists = listOf(l1, l2))
        val decoded = roundtrip(user)
        assertEquals(listOf(l1, l2), decoded.lists)
    }

    // -----------------------------------------------------------------------
    // 6. FieldMask fields
    // -----------------------------------------------------------------------

    @Test
    fun `updateMask roundtrips`() {
        val mask = listOf("display_name", "email", "address.city")
        val user = User(updateMask = mask)
        val decoded = roundtrip(user)
        assertEquals(mask, decoded.updateMask)
    }

    @Test
    fun `fieldMasks list roundtrips`() {
        val masks = listOf(
            listOf("email", "phone"),
            listOf("address.street", "address.zip"),
        )
        val user = User(fieldMasks = masks)
        val decoded = roundtrip(user)
        assertEquals(masks, decoded.fieldMasks)
    }

    // -----------------------------------------------------------------------
    // 7. Wrapper map values (nullable map values)
    // -----------------------------------------------------------------------

    @Test
    fun `labels map with non-null values roundtrips`() {
        val user = User(labels = mapOf("env" to "prod", "region" to "us-east"))
        val decoded = roundtrip(user)
        assertEquals(mapOf("env" to "prod", "region" to "us-east"), decoded.labels)
    }

    @Test
    fun `labels map with null values roundtrips`() {
        val user = User(labels = mapOf("env" to "prod", "cleared" to null))
        val decoded = roundtrip(user)
        assertEquals("prod", decoded.labels["env"])
        assertTrue(decoded.labels.containsKey("cleared"))
        assertNull(decoded.labels["cleared"])
    }

    @Test
    fun `scores map with non-null values roundtrips`() {
        val user = User(scores = mapOf("math" to 95L, "english" to 88L))
        val decoded = roundtrip(user)
        assertEquals(mapOf("math" to 95L, "english" to 88L), decoded.scores)
    }

    @Test
    fun `scores map with null values roundtrips`() {
        val user = User(scores = mapOf("math" to 100L, "pending" to null))
        val decoded = roundtrip(user)
        assertEquals(100L, decoded.scores["math"])
        assertTrue(decoded.scores.containsKey("pending"))
        assertNull(decoded.scores["pending"])
    }

    // -----------------------------------------------------------------------
    // 8. Timestamp map
    // -----------------------------------------------------------------------

    @Test
    fun `eventTimes map roundtrips`() {
        val times = mapOf(
            "login" to Instant.parse("2024-01-15T10:30:00Z"),
            "logout" to Instant.parse("2024-01-15T18:00:00Z"),
        )
        val user = User(eventTimes = times)
        val decoded = roundtrip(user)
        assertEquals(times, decoded.eventTimes)
    }

    // -----------------------------------------------------------------------
    // 9. Duration
    // -----------------------------------------------------------------------

    @Test
    fun `sessionTimeout roundtrips`() {
        val user = User(sessionTimeout = 2.hours)
        val decoded = roundtrip(user)
        assertEquals(2.hours, decoded.sessionTimeout)
    }

    @Test
    fun `sessionTimeout zero roundtrips`() {
        val user = User(sessionTimeout = kotlin.time.Duration.ZERO)
        val decoded = roundtrip(user)
        assertEquals(kotlin.time.Duration.ZERO, decoded.sessionTimeout)
    }

    // -----------------------------------------------------------------------
    // 10. Oneof sealed class
    // -----------------------------------------------------------------------

    @Test
    fun `contactMethod email roundtrips`() {
        val user = User(contactMethod = UserContactMethod.ContactEmail("a@b.com"))
        val decoded = roundtrip(user)
        assertEquals(UserContactMethod.ContactEmail("a@b.com"), decoded.contactMethod)
    }

    @Test
    fun `contactMethod phone roundtrips`() {
        val user = User(contactMethod = UserContactMethod.ContactPhone("+1-555-0100"))
        val decoded = roundtrip(user)
        assertEquals(UserContactMethod.ContactPhone("+1-555-0100"), decoded.contactMethod)
    }

    @Test
    fun `contactMethod null by default`() {
        assertNull(User().contactMethod)
        val decoded = roundtrip(User())
        assertNull(decoded.contactMethod)
    }

    // -----------------------------------------------------------------------
    // 11. Nested message
    // -----------------------------------------------------------------------

    @Test
    fun `address roundtrips`() {
        val addr = Address(
            street = "123 Main St",
            city = "Springfield",
            state = "IL",
            zip = "62704",
            country = "US",
        )
        val user = User(address = addr)
        val decoded = roundtrip(user)
        assertEquals(addr, decoded.address)
    }

    @Test
    fun `address null by default`() {
        assertNull(User().address)
        val decoded = roundtrip(User())
        assertNull(decoded.address)
    }

    // -----------------------------------------------------------------------
    // 11b. Bytes fields
    // -----------------------------------------------------------------------

    @Test
    fun `avatar bytes roundtrip with content`() {
        val bytes = byteArrayOf(1, 2, 3, 4)
        val user = User(avatar = bytes)
        val decoded = roundtrip(user)
        assertContentEquals(bytes, decoded.avatar)
    }

    @Test
    fun `avatarThumbnail roundtrips with content`() {
        val thumb = byteArrayOf(9, 8, 7)
        val user = User(avatarThumbnail = thumb)
        val decoded = roundtrip(user)
        assertContentEquals(thumb, decoded.avatarThumbnail)
    }

    // -----------------------------------------------------------------------
    // 12. Full roundtrip — every WKT field populated
    // -----------------------------------------------------------------------

    @Test
    fun `full roundtrip with all WKT fields populated`() {
        val user = User(
            id = "full-user",
            email = "full@example.com",
            displayName = "Full User",
            active = true,
            age = 35,
            roles = listOf("admin", "editor"),
            metadata = mapOf("plan" to "enterprise"),
            address = Address(
                street = "456 Oak Ave",
                city = "Portland",
                state = "OR",
                zip = "97201",
                country = "US",
            ),
            createdAt = Instant.parse("2024-06-01T12:00:00Z"),
            sessionTimeout = 2.hours,
            phone = "+1-503-555-0199",
            avatar = byteArrayOf(0xDE.toByte(), 0xAD.toByte(), 0xBE.toByte(), 0xEF.toByte()),
            avatarThumbnail = byteArrayOf(1, 2, 3),
            nickname = "fullie",
            status = UserStatus.ACTIVE,
            contactMethod = UserContactMethod.ContactEmail("full@example.com"),
            tags = listOf(Tag("role", "admin"), Tag("tier", "gold")),
            deletedAt = Instant.parse("2025-01-01T00:00:00Z"),
            previousStatus = UserStatus.SUSPENDED,

            // FieldMask
            updateMask = listOf("display_name", "email"),
            fieldMasks = listOf(
                listOf("email", "phone"),
                listOf("address.street"),
            ),

            // Struct
            extraMetadata = mapOf(
                "source" to JsonPrimitive("import"),
                "flags" to buildJsonObject { put("beta", JsonPrimitive(true)) },
            ),
            structs = listOf(
                mapOf("a" to JsonPrimitive(1) as JsonElement),
                mapOf("b" to JsonPrimitive("two") as JsonElement),
            ),
            configs = mapOf(
                "db" to mapOf("host" to JsonPrimitive("db.local") as JsonElement),
            ),

            // ListValue
            preferences = buildJsonArray {
                add(JsonPrimitive("dark_mode"))
                add(JsonPrimitive(true))
            },
            lists = listOf(
                buildJsonArray { add(JsonPrimitive("x")) },
                buildJsonArray { add(JsonPrimitive(1)) },
            ),

            // Value
            singleValue = JsonPrimitive("single"),
            values = listOf(JsonPrimitive(1), JsonNull, JsonPrimitive("v")),
            valueMap = mapOf(
                "k1" to JsonPrimitive(true) as JsonElement,
                "k2" to JsonNull as JsonElement,
            ),

            // Wrapper maps
            labels = mapOf("env" to "prod", "cleared" to null),
            scores = mapOf("math" to 100L, "pending" to null),

            // Timestamp map
            eventTimes = mapOf(
                "signup" to Instant.parse("2024-06-01T12:00:00Z"),
                "verify" to Instant.parse("2024-06-01T12:05:00Z"),
            ),
        )

        val encoded = json.encodeToString(user)
        val decoded = json.decodeFromString<User>(encoded)

        // Compare all fields except ByteArray (which uses referential equality).
        val emptyBytes = byteArrayOf()
        assertEquals(
            user.copy(avatar = emptyBytes, avatarThumbnail = null),
            decoded.copy(avatar = emptyBytes, avatarThumbnail = null),
        )
        assertContentEquals(user.avatar, decoded.avatar)
        assertContentEquals(user.avatarThumbnail, decoded.avatarThumbnail)

        // Spot-check a few WKT fields for extra confidence.
        assertEquals(2.hours, decoded.sessionTimeout)
        assertEquals(JsonPrimitive("single"), decoded.singleValue)
        assertEquals(listOf("display_name", "email"), decoded.updateMask)
        assertEquals("prod", decoded.labels["env"])
        assertNull(decoded.labels["cleared"])
        assertEquals(100L, decoded.scores["math"])
        assertNull(decoded.scores["pending"])
        assertEquals(Instant.parse("2024-06-01T12:00:00Z"), decoded.eventTimes["signup"])
        assertEquals(UserContactMethod.ContactEmail("full@example.com"), decoded.contactMethod)
    }
}
