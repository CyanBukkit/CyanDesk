package desk.cyan.core

object Fingerprint {
    fun normalize(raw: String): String? {
        val hex = raw.trim().lowercase()
        if (hex.length != 64 || hex.any { it !in '0'..'9' && it !in 'a'..'f' }) return null
        return hex
    }

    fun matches(pinned: String, presented: String): Boolean {
        val a = normalize(pinned) ?: return false
        val b = normalize(presented) ?: return false
        return a == b
    }
}

enum class SessionMode { VIEW, CONTROL }

fun scopesFor(mode: SessionMode): List<String> = when (mode) {
    SessionMode.VIEW -> listOf("view")
    SessionMode.CONTROL -> listOf("view", "input")
}

/** Capture starts only after the user accepts the system MediaProjection dialog. */
fun mayCapture(projectionAccepted: Boolean): Boolean = projectionAccepted

/**
 * Input injection requires both a control scope and the user-enabled accessibility service.
 * The app cannot turn that service on by itself.
 */
fun mayInject(mode: SessionMode, accessibilityEnabledByUser: Boolean): Boolean =
    mode == SessionMode.CONTROL && accessibilityEnabledByUser

/**
 * Host injection follows the ticket the server issued. A local control button
 * cannot widen a view-only ticket, and a revoked session is not granted input.
 */
fun mayInjectGranted(ticketAllowsInput: Boolean, accessibilityEnabledByUser: Boolean): Boolean =
    ticketAllowsInput && accessibilityEnabledByUser

fun encodeFrame(type: Int, payload: ByteArray): ByteArray {
    require(type in 1..255)
    val out = ByteArray(10 + payload.size)
    out[0] = 'C'.code.toByte()
    out[1] = 'D'.code.toByte()
    out[2] = 1
    out[3] = type.toByte()
    out[8] = ((payload.size ushr 8) and 0xff).toByte()
    out[9] = (payload.size and 0xff).toByte()
    payload.copyInto(out, 10)
    return out
}

fun decodeFrame(bytes: ByteArray): Pair<Int, ByteArray> {
    require(bytes.size >= 10)
    require(bytes[0] == 'C'.code.toByte() && bytes[1] == 'D'.code.toByte() && bytes[2] == 1.toByte())
    val n = ((bytes[6].toInt() and 0xff) shl 24) or
        ((bytes[7].toInt() and 0xff) shl 16) or
        ((bytes[8].toInt() and 0xff) shl 8) or
        (bytes[9].toInt() and 0xff)
    require(bytes.size == 10 + n)
    return bytes[3].toInt() and 0xff to bytes.copyOfRange(10, bytes.size)
}

/** Pico OS is Android. This build does not talk to OpenXR or a 6DoF runtime. */
fun picoRuntimeImplemented(): Boolean = false
