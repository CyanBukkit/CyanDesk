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
            val status = findViewById<TextView>(R.id.status)
            if (!server.startsWith("https://") || fingerprint == null) {
                status.text = "需要 https 地址和 64 位证书指纹，不能跳过核对"
                return@setOnClickListener
            }
            val next = Intent(this, DeviceActivity::class.java)
            next.putExtra("server", server)
            next.putExtra("fingerprint", fingerprint)
            next.putExtra("user", findViewById<EditText>(R.id.user).text.toString())
            startActivity(next)
        }
    }
}
