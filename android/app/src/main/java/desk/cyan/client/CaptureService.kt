package desk.cyan.client

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.hardware.display.DisplayManager
import android.hardware.display.VirtualDisplay
import android.media.MediaCodec
import android.media.MediaCodecInfo
import android.media.MediaFormat
import android.media.projection.MediaProjection
import android.media.projection.MediaProjectionManager
import android.os.Build
import android.os.IBinder
import android.util.DisplayMetrics
import android.view.WindowManager
import desk.cyan.core.Annex
import java.io.File
import java.nio.ByteBuffer

/**
 * Capture starts only from a MediaProjection result the system returned.
 * Android 10 adds the projection foreground-service type. Android 14 requires
 * that type on the manifest and on startForeground. This service does not
 * grant the projection or the accessibility service by itself.
 */
class CaptureService : Service() {
    private var running = true
    private var projection: MediaProjection? = null
    private var display: VirtualDisplay? = null
    private var codec: MediaCodec? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val code = intent?.getIntExtra(RESULT_CODE, 0) ?: 0
        @Suppress("DEPRECATION")
        val data = intent?.getParcelableExtra<Intent>(RESULT_DATA)
        if (code != android.app.Activity.RESULT_OK || data == null) {
            stopSelf()
            return START_NOT_STICKY
        }
        val channel = "capture"
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel(channel, "采集", NotificationManager.IMPORTANCE_LOW)
        )
        val note = Notification.Builder(this, channel)
            .setContentTitle("CyanDesk 正在采集你同意的屏幕")
            .setSmallIcon(android.R.drawable.ic_menu_camera)
            .build()
        if (Build.VERSION.SDK_INT >= 29) {
            startForeground(1, note, ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PROJECTION)
        } else {
            startForeground(1, note)
        }
        val projection = (getSystemService(MEDIA_PROJECTION_SERVICE) as MediaProjectionManager)
            .getMediaProjection(code, data)
        if (projection == null) {
            stopSelf()
            return START_NOT_STICKY
        }
        this.projection = projection
        projection.registerCallback(object : MediaProjection.Callback() {
            override fun onStop() {
                running = false
                stopSelf()
            }
        }, null)
        val agent = Hub.agent
        val session = Hub.sessionId
        val pinned = Hub.pinned
        if (agent == null || session.isEmpty() || pinned.length != 64) {
            projection.stop()
            stopSelf()
            return START_NOT_STICKY
        }
        Thread {
            var link: cyandesk.mobile.Link? = null
            try {
                link = agent.dial(Hub.ROLE_HOST.toLong(), session, pinned)
                mark("running")
                val metrics = DisplayMetrics()
                @Suppress("DEPRECATION")
                (getSystemService(WINDOW_SERVICE) as WindowManager).defaultDisplay.getMetrics(metrics)
                val size = evenSize(metrics.widthPixels, metrics.heightPixels)
                val encoder = openEncoder(size.first, size.second)
                codec = encoder
                val input = encoder.createInputSurface()
                encoder.start()
                display = projection.createVirtualDisplay(
                    "cyandesk",
                    size.first,
                    size.second,
                    metrics.densityDpi,
                    DisplayManager.VIRTUAL_DISPLAY_FLAG_AUTO_MIRROR,
                    input,
                    null,
                    null
                )
                Thread {
                    while (running) {
                        try {
                            if (!agent.active(session)) {
                                running = false
                                link?.close()
                                break
                            }
                        } catch (_: Exception) {
                            running = false
                            break
                        }
                        try {
                            Thread.sleep(400)
                        } catch (_: InterruptedException) {
                            break
                        }
                    }
                }.start()
                Thread {
                    try {
                        while (running) {
                            val pkt = link.recv()
                            if (pkt.kind.toInt() != 5) continue
                            val granted = agent.allowsInput(session)
                            val svc = InputAccessibilityService.instance
                            if (svc != null) {
                                val x = pkt.x.toFloat() * metrics.widthPixels / size.first
                                val y = pkt.y.toFloat() * metrics.heightPixels / size.second
                                svc.injectTap(x, y, granted && InputAccessibilityService.enabled)
                            }
                        }
                    } catch (_: Exception) {
                        running = false
                    }
                }.start()
                val info = MediaCodec.BufferInfo()
                var seq = 1
                var header = ByteArray(0)
                while (running) {
                    val index = encoder.dequeueOutputBuffer(info, 10_000)
                    if (index == MediaCodec.INFO_OUTPUT_FORMAT_CHANGED) {
                        header = csd(encoder.outputFormat)
                        continue
                    }
                    if (index < 0) continue
                    val buf = encoder.getOutputBuffer(index)
                    if (buf != null && info.size > 0) {
                        val raw = ByteArray(info.size)
                        buf.position(info.offset)
                        buf.get(raw)
                        var annex = Annex.avccToAnnexB(raw)
                        val key = info.flags and MediaCodec.BUFFER_FLAG_KEY_FRAME != 0
                        if (key && header.isNotEmpty()) annex = header + annex
                        if (info.flags and MediaCodec.BUFFER_FLAG_CODEC_CONFIG != 0) {
                            header = annex
                        } else if (agent.active(session)) {
                            link.sendVideo(seq.toLong(), size.first.toLong(), size.second.toLong(), annex)
                            seq++
                        }
                    }
                    encoder.releaseOutputBuffer(index, false)
                }
            } catch (err: Exception) {
                running = false
                mark("error: " + (err.message ?: err.toString()))
            } finally {
                running = false
                val current = runCatching { File(filesDir, "capture-state.txt").readText() }.getOrDefault("")
                if (!current.startsWith("error:")) {
                    mark("stopped")
                }
                runCatching { link?.close() }
                runCatching { display?.release() }
                runCatching { codec?.stop() }
                runCatching { codec?.release() }
                runCatching { projection.stop() }
                stopSelf()
            }
        }.start()
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        running = false
        super.onDestroy()
    }

    private fun openEncoder(width: Int, height: Int): MediaCodec {
        fun build(baseline: Boolean): MediaCodec {
            val format = MediaFormat.createVideoFormat(MediaFormat.MIMETYPE_VIDEO_AVC, width, height)
            format.setInteger(MediaFormat.KEY_COLOR_FORMAT, MediaCodecInfo.CodecCapabilities.COLOR_FormatSurface)
            format.setInteger(MediaFormat.KEY_BIT_RATE, 1_500_000)
            format.setInteger(MediaFormat.KEY_FRAME_RATE, 15)
            format.setInteger(MediaFormat.KEY_I_FRAME_INTERVAL, 1)
            if (baseline) {
                format.setInteger(MediaFormat.KEY_PROFILE, MediaCodecInfo.CodecProfileLevel.AVCProfileBaseline)
                format.setInteger(MediaFormat.KEY_LEVEL, MediaCodecInfo.CodecProfileLevel.AVCLevel31)
            }
            val encoder = MediaCodec.createEncoderByType(MediaFormat.MIMETYPE_VIDEO_AVC)
            try {
                encoder.configure(format, null, null, MediaCodec.CONFIGURE_FLAG_ENCODE)
            } catch (err: Exception) {
                encoder.release()
                throw err
            }
            return encoder
        }
        return try {
            build(true)
        } catch (_: Exception) {
            build(false)
        }
    }

    private fun mark(state: String) {
        runCatching { File(filesDir, "capture-state.txt").writeText(state) }
    }

    private fun evenSize(width: Int, height: Int): Pair<Int, Int> {
        var w = width.coerceAtLeast(2)
        var h = height.coerceAtLeast(2)
        val long = maxOf(w, h)
        if (long > 1280) {
            val scale = 1280f / long
            w = (w * scale).toInt().coerceAtLeast(2)
            h = (h * scale).toInt().coerceAtLeast(2)
        }
        if (w % 2 != 0) w--
        if (h % 2 != 0) h--
        return w to h
    }

    private fun csd(format: MediaFormat): ByteArray {
        val out = java.io.ByteArrayOutputStream()
        for (key in listOf("csd-0", "csd-1")) {
            val buf: ByteBuffer = format.getByteBuffer(key) ?: continue
            val bytes = ByteArray(buf.remaining())
            buf.get(bytes)
            out.write(Annex.avccToAnnexB(bytes))
        }
        return out.toByteArray()
    }

    companion object {
        const val RESULT_CODE = "resultCode"
        const val RESULT_DATA = "resultData"
    }
}
