package desk.cyan.client

import android.app.Activity
import android.content.Intent
import android.media.projection.MediaProjectionManager
import android.os.Bundle
import android.provider.Settings
import android.widget.ArrayAdapter
import android.widget.Button
import android.widget.ListView
import android.widget.TextView
import desk.cyan.core.SessionMode
import desk.cyan.core.mayCapture
import desk.cyan.core.mayInject
import desk.cyan.core.scopesFor

class DeviceActivity : Activity() {
    private var mode = SessionMode.VIEW
    private var projectionAccepted = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.devices)
        val names = intent.getStringArrayExtra("labels") ?: arrayOf("没有设备")
        findViewById<ListView>(R.id.devices).adapter =
            ArrayAdapter(this, android.R.layout.simple_list_item_1, names)
        findViewById<Button>(R.id.view).setOnClickListener { choose(SessionMode.VIEW) }
        findViewById<Button>(R.id.control).setOnClickListener { choose(SessionMode.CONTROL) }
        findViewById<Button>(R.id.capture).setOnClickListener { askProjection() }
    }

    private fun choose(next: SessionMode) {
        mode = next
        val enabled = InputAccessibilityService.enabled
        val status = findViewById<TextView>(R.id.status)
        status.text = "申请 " + scopesFor(mode).joinToString(",") +
            if (mayInject(mode, enabled)) "，输入服务已由用户打开" else "，控制还需要你在系统设置里打开 CyanDesk 输入服务"
        if (mode == SessionMode.CONTROL && !enabled) {
            startActivity(Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS))
        }
    }

    private fun askProjection() {
        val manager = getSystemService(MEDIA_PROJECTION_SERVICE) as MediaProjectionManager
        startActivityForResult(manager.createScreenCaptureIntent(), 7)
    }

    @Deprecated("platform callback")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        projectionAccepted = requestCode == 7 && resultCode == RESULT_OK && data != null
        if (!mayCapture(projectionAccepted) || data == null) {
            findViewById<TextView>(R.id.status).text = "没有系统录屏同意，采集不会开始"
            return
        }
        val start = Intent(this, CaptureService::class.java)
        start.putExtra(CaptureService.RESULT_CODE, resultCode)
        start.putExtra(CaptureService.RESULT_DATA, data)
        startForegroundService(start)
    }
}
