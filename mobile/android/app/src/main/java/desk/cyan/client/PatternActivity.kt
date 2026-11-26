package desk.cyan.client

import android.app.Activity
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.View
import android.widget.TextView

/** Full-screen colors so a consented capture has more than one frame to encode. */
class PatternActivity : Activity() {
    private val colors = intArrayOf(0xFFFF0000.toInt(), 0xFF00CC00.toInt(), 0xFF0000FF.toInt())
    private var index = 0
    private val handler = Handler(Looper.getMainLooper())
    private val tick = object : Runnable {
        override fun run() {
            findViewById<View>(R.id.pattern).setBackgroundColor(colors[index % colors.size])
            index++
            handler.postDelayed(this, 700)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.pattern)
        findViewById<View>(R.id.tapped).setOnClickListener {
            findViewById<TextView>(R.id.tapped).text = "已点击"
        }
        handler.post(tick)
    }

    override fun onDestroy() {
        handler.removeCallbacks(tick)
        super.onDestroy()
    }
}
