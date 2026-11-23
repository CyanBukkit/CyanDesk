package desk.cyan.client

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.media.projection.MediaProjectionManager
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import android.widget.ArrayAdapter
import android.widget.Button
import android.widget.ListView
import android.widget.TextView

class DeviceActivity : Activity() {
    private var rows: List<Row> = emptyList()
    private var selected = -1
    private var pendingConsent = ""

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.devices)
        show(intent.getStringExtra("devices").orEmpty())
        findViewById<ListView>(R.id.devices).setOnItemClickListener { _, _, position, _ ->
            selected = position
            val row = rows[position]
            status("已选择 ${row.name}。指纹 ${row.fingerprint}")
        }
        findViewById<Button>(R.id.view).setOnClickListener { request("view") }
        findViewById<Button>(R.id.control).setOnClickListener { request("control") }
        findViewById<Button>(R.id.host).setOnClickListener { lookForIncoming() }
        findViewById<Button>(R.id.agreeView).setOnClickListener { agree("view") }
        findViewById<Button>(R.id.agreeControl).setOnClickListener { agree("control") }
    }

    private fun show(text: String) {
        rows = text.lineSequence().map { it.trim() }.filter { it.isNotEmpty() }.mapNotNull { line ->
            val p = line.split('\t')
            if (p.size < 4) null else Row(p[0], p[1], p[2], p[3])
        }.toList()
        val labels = if (rows.isEmpty()) listOf("没有设备") else rows.map { "${it.trust}  ${it.name}" }
        findViewById<ListView>(R.id.devices).adapter =
            ArrayAdapter(this, android.R.layout.simple_list_item_1, labels)
    }

    private fun request(mode: String) {
        val row = rows.getOrNull(selected)
        val agent = Hub.agent
        if (row == null || agent == null) {
            status("先点一台设备")
            return
        }
        if (row.fingerprint.length != 64) {
            status("这台设备没有可核对的指纹")
            return
        }
        status("正在向服务器申请会话")
        Thread {
            val result = runCatching {
                val id = agent.request(row.id, mode)
                agent.waitActive(id, 20000L)
                id
            }
            runOnUiThread {
                result.onSuccess { id ->
                    Hub.sessionId = id
                    Hub.pinned = row.fingerprint
                    Hub.mode = mode
                    startActivity(Intent(this, ViewerActivity::class.java))
                }.onFailure { status(it.message ?: "会话没有开始") }
            }
        }.start()
    }

    private fun lookForIncoming() {
        val agent = Hub.agent ?: return
        Thread {
            val result = runCatching {
                val id = agent.incoming()
                if (id.isEmpty()) "" to "" else id to agent.peerFingerprint(id)
            }
            runOnUiThread {
                result.onSuccess { (id, fp) ->
                    pendingConsent = id
                    if (id.isEmpty()) {
                        status("现在没有等待你同意的连接")
                    } else {
                        status("有连接在等你同意。对方指纹 $fp。核对后再点同意。系统会再弹出录屏框，应用不能跳过它。")
                    }
                }.onFailure { status(it.message ?: "读取会话失败") }
            }
        }.start()
    }

    private fun agree(mode: String) {
        val agent = Hub.agent
        val id = pendingConsent
        if (agent == null || id.isEmpty()) {
            status("还没有等待同意的连接")
            return
        }
        if (mode == "control" && !InputAccessibilityService.enabled) {
            status("控制需要你在系统设置里打开 CyanDesk 输入服务。这次不会授予输入，也不会绕过那个开关。")
            startActivity(Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS))
            return
        }
        Hub.mode = mode
        Hub.sessionId = id
        Thread {
            val fp = runCatching { agent.peerFingerprint(id) }.getOrDefault("")
            val consented = runCatching {
                agent.consent(id, mode, true)
                agent.waitActive(id, 5000L)
                fp
            }
            runOnUiThread {
                consented.onSuccess {
                    Hub.pinned = it
                    if (Build.VERSION.SDK_INT >= 33 &&
                        checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
                    ) {
                        requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 9)
                    } else {
                        askProjection()
                    }
                }.onFailure { status(it.message ?: "同意没有完成") }
            }
        }.start()
    }

    private fun askProjection() {
        val manager = getSystemService(MEDIA_PROJECTION_SERVICE) as MediaProjectionManager
        startActivityForResult(manager.createScreenCaptureIntent(), 7)
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode != 9) return
        if (grantResults.isNotEmpty() && grantResults[0] == PackageManager.PERMISSION_GRANTED) {
            askProjection()
        } else {
            status("Android 13 及更新版本需要通知权限，前台采集服务才能保持。没有同意就不会采集。")
        }
    }

    @Deprecated("platform callback")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != 7) return
        if (resultCode != RESULT_OK || data == null) {
            status("没有系统录屏同意，采集不会开始")
            return
        }
        val start = Intent(this, CaptureService::class.java)
        start.putExtra(CaptureService.RESULT_CODE, resultCode)
        start.putExtra(CaptureService.RESULT_DATA, data)
        if (Build.VERSION.SDK_INT >= 26) {
            startForegroundService(start)
        } else {
            startService(start)
        }
        status("已把系统返回的录屏结果交给采集服务。")
    }

    private fun status(text: String) {
        findViewById<TextView>(R.id.status).text = text
    }

    private data class Row(val id: String, val name: String, val trust: String, val fingerprint: String)
}
