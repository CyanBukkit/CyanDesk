package desk.cyan.client

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.media.projection.MediaProjectionManager
import android.os.IBinder
import desk.cyan.core.mayCapture

class CaptureService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val code = intent?.getIntExtra(RESULT_CODE, 0) ?: 0
        @Suppress("DEPRECATION")
        val data = intent?.getParcelableExtra<Intent>(RESULT_DATA)
        if (!mayCapture(code == android.app.Activity.RESULT_OK && data != null) || data == null) {
            stopSelf()
            return START_NOT_STICKY
        }
        val channel = "capture"
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel(channel, "采集", NotificationManager.IMPORTANCE_LOW)
        )
        startForeground(1, Notification.Builder(this, channel).setContentTitle("正在采集").setSmallIcon(android.R.drawable.ic_menu_camera).build())
        val projection = (getSystemService(MEDIA_PROJECTION_SERVICE) as MediaProjectionManager)
            .getMediaProjection(code, data)
        if (projection == null) {
            stopSelf()
            return START_NOT_STICKY
        }
        return START_NOT_STICKY
    }

    companion object {
        const val RESULT_CODE = "resultCode"
        const val RESULT_DATA = "resultData"
    }
}
