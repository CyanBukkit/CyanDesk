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

    /** NAL units without start codes. Types 7 and 8 are SPS and PPS. */
    fun nalUnits(annex: ByteArray): List<ByteArray> {
        val starts = ArrayList<Int>()
        var i = 0
        while (i + 3 < annex.size) {
            if (annex[i] == 0.toByte() && annex[i + 1] == 0.toByte()) {
                if (annex[i + 2] == 1.toByte()) {
                    starts.add(i + 3)
                    i += 3
                    continue
                }
                if (i + 3 < annex.size && annex[i + 2] == 0.toByte() && annex[i + 3] == 1.toByte()) {
                    starts.add(i + 4)
                    i += 4
                    continue
                }
            }
            i++
        }
        return List(starts.size) { n ->
            val from = starts[n]
            val to = if (n + 1 < starts.size) {
                var end = starts[n + 1]
                while (end > from && annex[end - 1] == 0.toByte()) end--
                end
            } else annex.size
            annex.copyOfRange(from, to.coerceAtLeast(from))
        }
    }
}
