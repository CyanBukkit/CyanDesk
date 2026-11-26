package desk.cyan.client

import android.view.Surface
import cyandesk.mobile.Agent
import cyandesk.mobile.Link

/** Process-local session. The Go agent holds keys; this object does not log them. */
object Hub {
    @Volatile var agent: Agent? = null
    @Volatile var sessionId: String = ""
    @Volatile var pinned: String = ""
    @Volatile var mode: String = "view"
    @Volatile var link: Link? = null
    @Volatile var surface: Surface? = null
    @Volatile var viewer: ViewerActivity? = null
    @Volatile var frameW: Int = 2
    @Volatile var frameH: Int = 2
    @Volatile var allowInput: Boolean = false
    const val ROLE_HOST = 1
    const val ROLE_CONTROLLER = 2
}
