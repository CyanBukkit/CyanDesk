package desk.cyan.client

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
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
            status.text = "正在登录"
            Thread {
                val result = runCatching {
                    val api = ServerApi(server, fingerprint)
                    val token = api.login(user, password)
                    api.devices(token)
                }
                runOnUiThread {
                    result.onSuccess { devices ->
                        val next = Intent(this, DeviceActivity::class.java)
                        next.putExtra("labels", devices.map { "${it.trust}  ${it.name}  ${it.id}" }.toTypedArray())
                        startActivity(next)
                    }.onFailure {
                        status.text = "登录没有完成。指纹、地址或账号没有被接受。"
                    }
                }
            }.start()
        }
    }
}
