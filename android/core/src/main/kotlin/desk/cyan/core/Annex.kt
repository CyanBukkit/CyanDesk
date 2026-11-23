package desk.cyan.core

import java.io.ByteArrayOutputStream

/**
 * MediaCodec AVC buffers are commonly length-prefixed. The desktop session
 * carries Annex-B, which is what the shared Go path already encodes and decodes.
 */
object Annex {
    fun avccToAnnexB(packet: ByteArray): ByteArray {
        if (packet.size >= 4 && packet[0] == 0.toByte() && packet[1] == 0.toByte() &&
            (packet[2] == 1.toByte() || (packet[2] == 0.toByte() && packet[3] == 1.toByte()))
        ) {
            return packet.copyOf()
        }
        val out = ByteArrayOutputStream()
        var i = 0
        var converted = false
        while (i + 4 <= packet.size) {
            val n = ((packet[i].toInt() and 0xff) shl 24) or
                ((packet[i + 1].toInt() and 0xff) shl 16) or
                ((packet[i + 2].toInt() and 0xff) shl 8) or
                (packet[i + 3].toInt() and 0xff)
            if (n <= 0 || i + 4 + n > packet.size) {
                if (!converted) return packet.copyOf()
                break
            }
            out.write(byteArrayOf(0, 0, 0, 1))
            out.write(packet, i + 4, n)
            i += 4 + n
            converted = true
        }
        if (!converted) return packet.copyOf()
        return out.toByteArray()
    }
}
