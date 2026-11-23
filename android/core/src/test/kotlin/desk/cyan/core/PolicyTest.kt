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
        assertFalse(mayStartCapture(28, projectionAccepted = false, notificationGranted = true))
        assertTrue(mayStartCapture(28, projectionAccepted = true, notificationGranted = false))
        assertTrue(mayStartCapture(29, projectionAccepted = true, notificationGranted = false))
        assertFalse(mayStartCapture(33, projectionAccepted = true, notificationGranted = false))
        assertTrue(mayStartCapture(33, projectionAccepted = true, notificationGranted = true))
        assertFalse(mayStartCapture(34, projectionAccepted = true, notificationGranted = false))
        assertTrue(mayStartCapture(34, projectionAccepted = true, notificationGranted = true))
        assertFalse(captureUsesProjectionServiceType(28))
        assertTrue(captureUsesProjectionServiceType(29))
        assertTrue(captureUsesProjectionServiceType(34))
        assertTrue(captureUsesProjectionServiceType(35))
        assertFalse(mayInject(SessionMode.CONTROL, accessibilityEnabledByUser = false))
        assertFalse(mayInject(SessionMode.VIEW, accessibilityEnabledByUser = true))
        assertTrue(mayInject(SessionMode.CONTROL, accessibilityEnabledByUser = true))
        assertFalse(mayInjectGranted(ticketAllowsInput = false, accessibilityEnabledByUser = true))
        assertFalse(mayInjectGranted(ticketAllowsInput = true, accessibilityEnabledByUser = false))
        assertTrue(mayInjectGranted(ticketAllowsInput = true, accessibilityEnabledByUser = true))
    }

    @Test
    fun avccBecomesAnnexB() {
        val avcc = byteArrayOf(0, 0, 0, 2, 0x65, 0x88.toByte())
        val annex = Annex.avccToAnnexB(avcc)
        assertEquals(byteArrayOf(0, 0, 0, 1, 0x65, 0x88.toByte()).toList(), annex.toList())
        val already = byteArrayOf(0, 0, 0, 1, 0x67)
        assertEquals(already.toList(), Annex.avccToAnnexB(already).toList())
        val units = Annex.nalUnits(byteArrayOf(0, 0, 0, 1, 0x67, 0x42, 0, 0, 0, 1, 0x68, 0x11))
        assertEquals(2, units.size)
        assertEquals(7, units[0][0].toInt() and 0x1f)
        assertEquals(8, units[1][0].toInt() and 0x1f)
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
