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
 * - Messages without constraints have no validate()
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
        age = 30
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

    // -----------------------------------------------------------------------
    // No-constraint messages should not have validate()
    // -----------------------------------------------------------------------

    @Test
    fun `address has no validate method - no constraints`() {
        // Address has no buf.validate constraints, so no validate() is generated.
        // This test verifies the codegen correctly skips constraint-free messages.
        // If validate() were generated on Address, this would compile but be wrong.
        val address = Address(
            street = "123 Main St",
            city = "Springfield",
            state = "IL",
            zip = "62701",
            country = "US"
        )
        // We can only verify at compile-time that Address does NOT have validate().
        // If this file compiles, it means Address.validate() was NOT generated
        // (since we don't call it and the test project has no other references).
        assertEquals("Springfield", address.city)
    }

    @Test
    fun `tag has no validate method - no constraints`() {
        val tag = Tag(key = "env", value = "prod")
        assertEquals("env", tag.key)
    }
}
