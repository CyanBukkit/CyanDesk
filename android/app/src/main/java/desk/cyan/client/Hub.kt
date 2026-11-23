package desk.cyan.client

import cyandesk.mobile.Agent

/** Process-local session. The Go agent holds keys; this object does not log them. */
object Hub {
    @Volatile var agent: Agent? = null
    @Volatile var sessionId: String = ""
    @Volatile var pinned: String = ""
    @Volatile var mode: String = "view"
    const val ROLE_HOST = 1
    const val ROLE_CONTROLLER = 2
}
