package desk.cyan.client

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import cyandesk.mobile.Mobile
import desk.cyan.core.Fingerprint

class LoginActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.login)
        findViewById<Button>(R.id.login).setOnClickListener {
            val server = findViewById<EditText>(R.id.server).text.toString().trim()
            val fingerprint = Fingerprint.normalize(findViewById<EditText>(R.id.fingerprint).text.toString())
            val user = findViewById<EditText>(R.id.user).text.toString()
            val password = findViewById<EditText>(R.id.password).text.toString()
            val status = findViewById<TextView>(R.id.status)
            if (!server.startsWith("https://") || fingerprint == null) {
                status.text = "需要 https 地址和 64 位证书指纹，不能跳过核对"
                return@setOnClickListener
            }
            findViewById<EditText>(R.id.password).setText("")
            status.text = "正在登录并登记这台 Android 设备"
            Thread {
                val result = runCatching {
                    Mobile.touch()
                    val agent = Mobile.connect(server, fingerprint, user, password)
                    val trust = agent.enroll("android")
                    if (trust != "trusted") {
                        agent.approve(agent.deviceID(), agent.fingerprint())
                    }
                    agent to agent.devices()
                }
                runOnUiThread {
                    result.onSuccess { (agent, devices) ->
                        Hub.agent = agent
                        val next = Intent(this, DeviceActivity::class.java)
                        next.putExtra("devices", devices)
                        startActivity(next)
                    }.onFailure {
                        val msg = it.message ?: "登录没有完成"
                        status.text = if (password.isNotEmpty() && msg.contains(password)) {
                            "登录没有完成。指纹、地址或账号没有被接受。"
                        } else {
                            msg
                        }
                    }
                }
            }.start()
        }
    }
}
