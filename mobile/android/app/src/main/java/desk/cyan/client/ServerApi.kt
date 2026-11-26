package desk.cyan.client

import org.json.JSONObject
import java.io.BufferedReader
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import javax.net.ssl.HostnameVerifier
import javax.net.ssl.HttpsURLConnection
import javax.net.ssl.SSLContext
import javax.net.ssl.TrustManager
import javax.net.ssl.X509TrustManager

class ServerApi(private val base: String, private val fingerprint: String) {
    data class Device(val id: String, val name: String, val trust: String, val fingerprint: String)

    fun login(user: String, password: String): String {
        val body = JSONObject().put("login_name", user).put("password", password).toString()
        val text = request("POST", "/api/v1/auth/login", body, null)
        return JSONObject(text).getString("access_token")
    }

    fun devices(access: String): List<Device> {
        val text = request("GET", "/api/v1/devices", null, access)
        val arr = JSONObject(text).optJSONArray("devices") ?: return emptyList()
        return List(arr.length()) { i ->
            val item = arr.getJSONObject(i)
            Device(
                item.optString("id"),
                item.optString("name"),
                item.optString("trust_state"),
                item.optString("fingerprint"),
            )
        }
    }

    private fun request(method: String, path: String, body: String?, bearer: String?): String {
        val conn = (URL(base.trimEnd('/') + path).openConnection() as HttpsURLConnection)
        conn.requestMethod = method
        conn.connectTimeout = 8000
        conn.readTimeout = 8000
        conn.hostnameVerifier = HostnameVerifier { hostname, session ->
            HttpsURLConnection.getDefaultHostnameVerifier().verify(hostname, session)
        }
        val context = SSLContext.getInstance("TLS")
        context.init(null, arrayOf<TrustManager>(pin(fingerprint)), null)
        conn.sslSocketFactory = context.socketFactory
        if (bearer != null) conn.setRequestProperty("Authorization", "Bearer $bearer")
        if (body != null) {
            conn.doOutput = true
            conn.setRequestProperty("Content-Type", "application/json")
            conn.outputStream.use { it.write(body.toByteArray()) }
        }
        val stream = if (conn.responseCode in 200..299) conn.inputStream else conn.errorStream
        val text = stream.bufferedReader().use(BufferedReader::readText)
        if (conn.responseCode !in 200..299) error("http ${conn.responseCode}")
        return text
    }

    private fun pin(expected: String) = object : X509TrustManager {
        override fun getAcceptedIssuers(): Array<X509Certificate> = emptyArray()
        override fun checkClientTrusted(chain: Array<X509Certificate>, authType: String) {}
        override fun checkServerTrusted(chain: Array<X509Certificate>, authType: String) {
            val got = MessageDigest.getInstance("SHA-256").digest(chain[0].encoded)
                .joinToString("") { "%02x".format(it) }
            if (got != expected) throw CertificateException("fingerprint mismatch")
        }
    }
}
