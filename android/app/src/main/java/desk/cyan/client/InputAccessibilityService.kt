package desk.cyan.client

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.GestureDescription
import android.graphics.Path
import desk.cyan.core.SessionMode
import desk.cyan.core.mayInject

class InputAccessibilityService : AccessibilityService() {
    override fun onServiceConnected() {
        enabled = true
    }

    override fun onDestroy() {
        enabled = false
        super.onDestroy()
    }

    override fun onAccessibilityEvent(event: android.view.accessibility.AccessibilityEvent?) {}

    override fun onInterrupt() {}

    fun injectTap(x: Float, y: Float, mode: SessionMode): Boolean {
        if (!mayInject(mode, enabled)) return false
        val path = Path().apply { moveTo(x, y) }
        val gesture = GestureDescription.Builder()
            .addStroke(GestureDescription.StrokeDescription(path, 0, 40))
            .build()
        return dispatchGesture(gesture, null, null)
    }

    companion object {
        var enabled: Boolean = false
    }
}
