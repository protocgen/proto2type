package test.v1

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue
import kotlin.test.assertFailsWith

/**
 * Integration tests for proto2type-generated Kotlin validation.
 *
 * Validates:
 * - Valid user passes validation (empty errors list)
 * - Email format checking
 * - String length constraints (min_len / max_len)
 * - Numeric range constraints (gte / lte)
 * - Boundary values (min/max edges)
 * - Multiple errors collected at once
 * - validateOrThrow() convenience method
 * - Optional field null safety (?.let)
 * - Pattern regex validation
 * - Repeated field min_items / max_items
 * - Nested message validation propagation
 * - Address constraints (street, city, state, zip pattern)
 * - Tag empty validate (no constraints)
 */
class ValidationTest {

    // -----------------------------------------------------------------------
    // Helpers
    // -----------------------------------------------------------------------

    /** Creates a valid User with all constraints satisfied. */
    private fun validUser() = User(
        id = "user-1",
        email = "alice@example.com",
        displayName = "Alice",
        active = true,
        age = 30,
        roles = listOf("admin"),
        address = validAddress()
    )

    /** Creates a valid Address with all constraints satisfied. */
    private fun validAddress() = Address(
        street = "123 Main St",
        city = "Springfield",
        state = "IL",
        zip = "62701",
        country = "US"
    )

    // -----------------------------------------------------------------------
    // Happy path
    // -----------------------------------------------------------------------

    @Test
    fun `valid user passes validation`() {
        val errors = validUser().validate()
        assertTrue(errors.isEmpty(), "valid user should have no errors, got: $errors")
    }

    @Test
    fun `default user fails validation - empty display name`() {
        val errors = User().validate()
        assertTrue(errors.isNotEmpty(), "default user should fail validation")
        assertTrue(
            errors.any { "display_name" in it },
            "should report display_name error for empty string"
        )
    }

    // -----------------------------------------------------------------------
    // Email validation
    // -----------------------------------------------------------------------

    @Test
    fun `invalid email format fails validation`() {
        val user = validUser().copy(email = "not-an-email")
        val errors = user.validate()
        assertTrue(errors.any { "email" in it }, "should report email error, got: $errors")
    }

    @Test
    fun `email missing domain fails validation`() {
        val user = validUser().copy(email = "alice@")
        val errors = user.validate()
        assertTrue(errors.any { "email" in it }, "missing domain should fail")
    }

    @Test
    fun `email missing @ fails validation`() {
        val user = validUser().copy(email = "aliceexample.com")
        val errors = user.validate()
        assertTrue(errors.any { "email" in it }, "missing @ should fail")
    }

    @Test
    fun `empty email passes validation - not required`() {
        // email is not marked as required, so empty string is valid
        val user = validUser().copy(email = "")
        val errors = user.validate()
        assertTrue(
            errors.none { "email" in it },
            "empty email should pass (not required), got: $errors"
        )
    }

    @Test
    fun `valid email formats pass validation`() {
        val validEmails = listOf(
            "user@example.com",
            "user+tag@example.com",
            "user@sub.domain.com",
            "a@b.co"
        )
        for (email in validEmails) {
            val errors = validUser().copy(email = email).validate()
            assertTrue(
                errors.none { "email" in it },
                "email '$email' should be valid, got: $errors"
            )
        }
    }

    // -----------------------------------------------------------------------
    // String length constraints (display_name: min=1, max=255)
    // -----------------------------------------------------------------------

    @Test
    fun `empty display name fails min length`() {
        val user = validUser().copy(displayName = "")
        val errors = user.validate()
        assertTrue(errors.any { "display_name" in it }, "empty name should fail min_len=1")
    }

    @Test
    fun `display name exceeding max length fails`() {
        val user = validUser().copy(displayName = "x".repeat(256))
        val errors = user.validate()
        assertTrue(errors.any { "display_name" in it }, "256 chars should fail max_len=255")
    }

    @Test
    fun `display name at min boundary passes`() {
        val user = validUser().copy(displayName = "x") // exactly 1 char
        val errors = user.validate()
        assertTrue(
            errors.none { "display_name" in it },
            "1 char should pass min_len=1"
        )
    }

    @Test
    fun `display name at max boundary passes`() {
        val user = validUser().copy(displayName = "x".repeat(255)) // exactly 255 chars
        val errors = user.validate()
        assertTrue(
            errors.none { "display_name" in it },
            "255 chars should pass max_len=255"
        )
    }

    @Test
    fun `unicode display name counts characters not bytes`() {
        // 3 emoji = 3 chars (each emoji is 4 bytes in UTF-8)
        val user = validUser().copy(displayName = "🎉🎊🎈")
        val errors = user.validate()
        assertTrue(
            errors.none { "display_name" in it },
            "3 unicode chars should pass min_len=1, got: $errors"
        )
    }

    // -----------------------------------------------------------------------
    // Numeric range constraints (age: gte=0, lte=150)
    // -----------------------------------------------------------------------

    @Test
    fun `negative age fails validation`() {
        val user = validUser().copy(age = -1)
        val errors = user.validate()
        assertTrue(errors.any { "age" in it }, "age=-1 should fail gte=0")
    }

    @Test
    fun `age over 150 fails validation`() {
        val user = validUser().copy(age = 151)
        val errors = user.validate()
        assertTrue(errors.any { "age" in it }, "age=151 should fail lte=150")
    }

    @Test
    fun `age at lower boundary passes`() {
        val user = validUser().copy(age = 0)
        val errors = user.validate()
        assertTrue(
            errors.none { "age" in it },
            "age=0 should pass gte=0"
        )
    }

    @Test
    fun `age at upper boundary passes`() {
        val user = validUser().copy(age = 150)
        val errors = user.validate()
        assertTrue(
            errors.none { "age" in it },
            "age=150 should pass lte=150"
        )
    }

    @Test
    fun `large negative age fails`() {
        val user = validUser().copy(age = Int.MIN_VALUE)
        val errors = user.validate()
        assertTrue(errors.any { "age" in it }, "MIN_VALUE should fail gte=0")
    }

    // -----------------------------------------------------------------------
    // Repeated field constraints (roles: min_items=1, max_items=10)
    // -----------------------------------------------------------------------

    @Test
    fun `empty roles fails min_items`() {
        val user = validUser().copy(roles = emptyList())
        val errors = user.validate()
        assertTrue(errors.any { "roles" in it }, "empty roles should fail min_items=1")
    }

    @Test
    fun `single role passes`() {
        val user = validUser().copy(roles = listOf("admin"))
        val errors = user.validate()
        assertTrue(errors.none { "roles" in it }, "1 role should pass, got: $errors")
    }

    @Test
    fun `10 roles passes max boundary`() {
        val user = validUser().copy(roles = (1..10).map { "role$it" })
        val errors = user.validate()
        assertTrue(errors.none { "roles" in it }, "10 roles should pass max_items=10")
    }

    @Test
    fun `11 roles fails max_items`() {
        val user = validUser().copy(roles = (1..11).map { "role$it" })
        val errors = user.validate()
        assertTrue(errors.any { "roles" in it }, "11 roles should fail max_items=10")
    }

    // -----------------------------------------------------------------------
    // Optional field null safety — phone (?.let path)
    // -----------------------------------------------------------------------

    @Test
    fun `null phone passes validation - optional field`() {
        val user = validUser().copy(phone = null)
        val errors = user.validate()
        assertTrue(
            errors.none { "phone" in it },
            "null phone should pass (optional), got: $errors"
        )
    }

    @Test
    fun `valid phone passes validation`() {
        val user = validUser().copy(phone = "+1-555-123-4567")
        val errors = user.validate()
        assertTrue(
            errors.none { "phone" in it },
            "valid phone should pass, got: $errors"
        )
    }

    @Test
    fun `phone too short fails min_len`() {
        val user = validUser().copy(phone = "12345") // 5 chars < min_len=7
        val errors = user.validate()
        assertTrue(errors.any { "phone" in it }, "5-char phone should fail min_len=7")
    }

    @Test
    fun `phone too long fails max_len`() {
        val user = validUser().copy(phone = "+1-555-123-456-7890-999") // > 20 chars
        val errors = user.validate()
        assertTrue(errors.any { "phone" in it }, "phone > 20 chars should fail max_len=20")
    }

    @Test
    fun `phone with letters fails pattern`() {
        val user = validUser().copy(phone = "555-CALL-NOW") // letters don't match pattern
        val errors = user.validate()
        assertTrue(errors.any { "phone" in it && "pattern" in it }, "letters in phone should fail pattern")
    }

    @Test
    fun `phone at min boundary passes`() {
        val user = validUser().copy(phone = "1234567") // exactly 7 chars
        val errors = user.validate()
        assertTrue(
            errors.none { "phone" in it },
            "7-char phone should pass min_len=7, got: $errors"
        )
    }

    // -----------------------------------------------------------------------
    // Nested message validation — Address
    // -----------------------------------------------------------------------

    @Test
    fun `valid address passes validation`() {
        val errors = validAddress().validate()
        assertTrue(errors.isEmpty(), "valid address should have no errors, got: $errors")
    }

    @Test
    fun `empty street fails address validation`() {
        val addr = validAddress().copy(street = "")
        val errors = addr.validate()
        assertTrue(errors.any { "street" in it }, "empty street should fail min_len=1")
    }

    @Test
    fun `empty city fails address validation`() {
        val addr = validAddress().copy(city = "")
        val errors = addr.validate()
        assertTrue(errors.any { "city" in it }, "empty city should fail min_len=1")
    }

    @Test
    fun `state too short fails address validation`() {
        val addr = validAddress().copy(state = "I") // 1 char < min_len=2
        val errors = addr.validate()
        assertTrue(errors.any { "state" in it }, "1-char state should fail min_len=2")
    }

    @Test
    fun `state too long fails address validation`() {
        val addr = validAddress().copy(state = "ILL") // 3 chars > max_len=2
        val errors = addr.validate()
        assertTrue(errors.any { "state" in it }, "3-char state should fail max_len=2")
    }

    @Test
    fun `state exactly 2 chars passes`() {
        val errors = validAddress().copy(state = "CA").validate()
        assertTrue(errors.none { "state" in it }, "2-char state should pass")
    }

    @Test
    fun `valid zip passes pattern`() {
        for (zip in listOf("90210", "62701", "10001-1234")) {
            val errors = validAddress().copy(zip = zip).validate()
            assertTrue(errors.none { "zip" in it }, "zip '$zip' should pass pattern")
        }
    }

    @Test
    fun `invalid zip fails pattern`() {
        for (zip in listOf("1234", "ABCDE", "123456", "12345-")) {
            val errors = validAddress().copy(zip = zip).validate()
            assertTrue(errors.any { "zip" in it }, "zip '$zip' should fail pattern")
        }
    }

    // -----------------------------------------------------------------------
    // Nested message propagation — User.address errors prefixed
    // -----------------------------------------------------------------------

    @Test
    fun `user with invalid address gets nested errors`() {
        val user = validUser().copy(address = Address(zip = "bad"))
        val errors = user.validate()
        // Should have nested errors prefixed with "address."
        assertTrue(errors.any { it.startsWith("address.") }, "should prefix nested errors, got: $errors")
        assertTrue(errors.any { "address.street" in it }, "should report address.street error")
        assertTrue(errors.any { "address.city" in it }, "should report address.city error")
        assertTrue(errors.any { "address.zip" in it }, "should report address.zip error")
    }

    @Test
    fun `user with null address skips nested validation`() {
        val user = validUser().copy(address = null)
        val errors = user.validate()
        assertTrue(
            errors.none { it.startsWith("address.") },
            "null address should skip nested validation, got: $errors"
        )
    }

    // -----------------------------------------------------------------------
    // Multiple errors
    // -----------------------------------------------------------------------

    @Test
    fun `multiple violations collected at once`() {
        val user = User(
            email = "bad",
            displayName = "", // too short
            age = 200         // too high
        )
        val errors = user.validate()
        assertTrue(errors.size >= 3, "should have at least 3 errors, got: $errors")
        assertTrue(errors.any { "email" in it }, "should include email error")
        assertTrue(errors.any { "display_name" in it }, "should include display_name error")
        assertTrue(errors.any { "age" in it }, "should include age error")
    }

    // -----------------------------------------------------------------------
    // validateOrThrow()
    // -----------------------------------------------------------------------

    @Test
    fun `validateOrThrow passes for valid user`() {
        validUser().validateOrThrow() // should not throw
    }

    @Test
    fun `validateOrThrow throws for invalid user`() {
        val user = validUser().copy(age = 200)
        val ex = assertFailsWith<IllegalStateException> {
            user.validateOrThrow()
        }
        assertTrue(ex.message!!.contains("age"), "exception message should mention age")
        assertTrue(ex.message!!.contains("User"), "exception message should mention type name")
    }

    @Test
    fun `validateOrThrow message contains all errors`() {
        val user = User(email = "bad", displayName = "", age = -1)
        val ex = assertFailsWith<IllegalStateException> {
            user.validateOrThrow()
        }
        val msg = ex.message!!
        assertTrue(msg.contains("email"), "should mention email")
        assertTrue(msg.contains("display_name"), "should mention display_name")
        assertTrue(msg.contains("age"), "should mention age")
    }

    @Test
    fun `address validateOrThrow throws for invalid address`() {
        val addr = Address(zip = "bad")
        val ex = assertFailsWith<IllegalStateException> {
            addr.validateOrThrow()
        }
        assertTrue(ex.message!!.contains("Address"), "should mention Address type")
    }

    // -----------------------------------------------------------------------
    // Tag - empty validate (no constraints, but method exists)
    // -----------------------------------------------------------------------

    @Test
    fun `tag validate returns empty list - no constraints`() {
        val tag = Tag(key = "env", value = "prod")
        val errors = tag.validate()
        assertTrue(errors.isEmpty(), "tag with no constraints should always pass")
    }

    @Test
    fun `tag validateOrThrow passes - no constraints`() {
        Tag(key = "env", value = "prod").validateOrThrow() // should not throw
    }
}
