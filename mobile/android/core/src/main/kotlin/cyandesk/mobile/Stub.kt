// Stub classes replacing cyandesk.mobile.Agent/Link/Mobile/Packet from cyandesk.aar
// TODO: restore real minirtc Android AAR when ready — all calls return safe defaults
package cyandesk.mobile

/** Frame/media packet received from a Link.recv() call. */
data class Packet(
    val kind: Number = 0,
    val annex: ByteArray = ByteArray(0),
    val w: Number = 0,
    val h: Number = 0,
    val seq: Number = 0,
    val x: Number = 0,
    val y: Number = 0
)

/** Process-local agent (signal + transport stack). */
class Agent {
    fun connect(server: String, token: String, onReady: () -> Unit = {}) {}
    fun disconnect() {}
    fun reconnect() {}
    val ready: Boolean get() = false
    val fingerprint: String get() = ""
    fun fingerprint(): String = ""
    fun deviceID(): String = ""
    val deviceName: String get() = ""
    val userId: String get() = ""
    fun on(event: String, handler: (Any?) -> Unit = {}) {}
    fun enroll(device: String): String = "trusted"
    fun approve(deviceId: String, fingerprint: String): String = "trusted"
    fun devices(): String = ""
    fun request(id: String, mode: String): String = ""
    fun peerFingerprint(id: String): String = ""
    fun consent(id: String, mode: String, approved: Boolean): Boolean = true
    fun waitActive(id: String, timeoutMs: Long): Boolean = true
    fun incoming(): String = ""
    fun dial(role: Long, session: String, pinned: String): Link = Link()
    fun allowsInput(session: String): Boolean = false
    fun active(session: String): Boolean = false
}

/** P2P media link to a remote host. */
class Link {
    val active: Boolean get() = false
    val allowsInput: Boolean get() = false
    fun dial(hostId: String, password: String = "") {}
    fun hang() {}
    fun close() {}
    fun start(surface: Any? = null) {}
    fun stop() {}
    fun pause() {}
    fun resume() {}
    fun sendPointer(seq: Long, x: Long, y: Long) {}
    fun sendKey(code: Int, state: Boolean) {}
    fun sendVideo(seq: Long, w: Long, h: Long, bytes: ByteArray) {}
    fun sendAudio(bytes: ByteArray) {}
    fun sendInput(event: Any) {}
    fun recv(): Packet = Packet()
}

/** Top-level entry point (Go-style singleton). */
object Mobile {
    fun touch() {}
    fun connect(server: String, fingerprint: String, user: String, password: String): Agent = Agent()
    fun reconnect(agent: Agent) {}
    fun disconnect(agent: Agent) {}
}
