package desk.cyan.client

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.GestureDescription
import android.graphics.Path
import desk.cyan.core.mayInjectGranted

class InputAccessibilityService : AccessibilityService() {
    override fun onServiceConnected() {
        enabled = true
        instance = this
    }

    override fun onDestroy() {
        enabled = false
        if (instance === this) instance = null
        super.onDestroy()
    }

    override fun onAccessibilityEvent(event: android.view.accessibility.AccessibilityEvent?) {}

    override fun onInterrupt() {}

    fun injectTap(x: Float, y: Float, ticketAllowsInput: Boolean): Boolean {
        if (!mayInjectGranted(ticketAllowsInput, enabled)) return false
        val path = Path().apply { moveTo(x, y) }
        val gesture = GestureDescription.Builder()
            .addStroke(GestureDescription.StrokeDescription(path, 0, 40))
            .build()
        return dispatchGesture(gesture, null, null)
    }

    companion object {
        @Volatile var enabled: Boolean = false
        @Volatile var instance: InputAccessibilityService? = null
    }
}
