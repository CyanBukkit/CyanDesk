package desk.cyan.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PolicyTest {
    @Test
    fun fingerprintMustBe64Hex() {
        assertNull(Fingerprint.normalize("abc"))
        assertNull(Fingerprint.normalize("zz" + "ab".repeat(31)))
        val ok = "ab".repeat(32)
        assertEquals(ok, Fingerprint.normalize("  $ok  "))
        assertTrue(Fingerprint.matches(ok.uppercase(), ok))
        assertFalse(Fingerprint.matches(ok, "cd".repeat(32)))
    }

    @Test
    fun controlRequestsInputAndViewDoesNot() {
        assertEquals(listOf("view"), scopesFor(SessionMode.VIEW))
        assertEquals(listOf("view", "input"), scopesFor(SessionMode.CONTROL))
    }

    @Test
    fun captureAndInputNeedExplicitConsent() {
        assertFalse(mayCapture(false))
        assertTrue(mayCapture(true))
        assertFalse(mayInject(SessionMode.CONTROL, accessibilityEnabledByUser = false))
        assertFalse(mayInject(SessionMode.VIEW, accessibilityEnabledByUser = true))
        assertTrue(mayInject(SessionMode.CONTROL, accessibilityEnabledByUser = true))
    }

    @Test
    fun frameMatchesCyandeskHeader() {
        val encoded = encodeFrame(1, byteArrayOf(0x61, 0x62))
        assertEquals('C'.code.toByte(), encoded[0])
        assertEquals('D'.code.toByte(), encoded[1])
        assertEquals(1.toByte(), encoded[2])
        assertEquals(1.toByte(), encoded[3])
        assertEquals(2, encoded[9].toInt())
        val (typ, payload) = decodeFrame(encoded)
        assertEquals(1, typ)
        assertEquals(2, payload.size)
    }

    @Test
    fun picoIsNotClaimed() {
        assertFalse(picoRuntimeImplemented())
    }
}
