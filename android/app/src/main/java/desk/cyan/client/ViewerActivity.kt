package desk.cyan.client

import android.app.Activity
import android.media.MediaCodec
import android.media.MediaFormat
import android.os.Bundle
import android.view.MotionEvent
import android.view.Surface
import android.view.SurfaceHolder
import android.view.SurfaceView
import android.widget.TextView
import java.util.concurrent.atomic.AtomicInteger

class ViewerActivity : Activity() {
    private var running = true
    private val pointerSeq = AtomicInteger(1)
    @Volatile private var frameW = 1
    @Volatile private var frameH = 1
    @Volatile private var allowInput = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.viewer)
        val screen = findViewById<SurfaceView>(R.id.screen)
        screen.holder.addCallback(object : SurfaceHolder.Callback {
            override fun surfaceCreated(holder: SurfaceHolder) {
                start(holder.surface, screen)
            }
            override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) {}
            override fun surfaceDestroyed(holder: SurfaceHolder) {
                running = false
            }
        })
    }

    override fun onDestroy() {
        running = false
        super.onDestroy()
    }

    private fun start(surface: Surface, screen: SurfaceView) {
        val agent = Hub.agent
        val session = Hub.sessionId
        val pinned = Hub.pinned
        if (agent == null || session.isEmpty() || pinned.length != 64) {
            status("没有已确认的会话")
            finish()
            return
        }
        Thread {
            var link: cyandesk.mobile.Link? = null
            var codec: MediaCodec? = null
            try {
                link = agent.dial(Hub.ROLE_CONTROLLER.toLong(), session, pinned)
                allowInput = agent.allowsInput(session)
                runOnUiThread {
                    status(if (allowInput) "加密会话已建立，可以点击画面" else "加密会话已建立，这张票据只有观看")
                    screen.setOnTouchListener { _, event ->
                        if (event.action == MotionEvent.ACTION_UP && allowInput && running) {
                            val x = (event.x / screen.width.coerceAtLeast(1) * frameW).toInt()
                            val y = (event.y / screen.height.coerceAtLeast(1) * frameH).toInt()
                            val current = link
                            if (current != null) {
                                runCatching { current.sendPointer(pointerSeq.getAndIncrement().toLong(), x.toLong(), y.toLong()) }
                            }
                        }
                        true
                    }
                }
                while (running) {
                    if (!agent.active(session)) break
                    val pkt = link.recv()
                    if (pkt.kind.toInt() != 3 || pkt.annex == null || pkt.annex.isEmpty()) continue
                    frameW = pkt.w.toInt().coerceAtLeast(2)
                    frameH = pkt.h.toInt().coerceAtLeast(2)
                    if (codec == null) {
                        codec = MediaCodec.createDecoderByType(MediaFormat.MIMETYPE_VIDEO_AVC)
                        val format = MediaFormat.createVideoFormat(MediaFormat.MIMETYPE_VIDEO_AVC, frameW, frameH)
                        codec.configure(format, surface, null, 0)
                        codec.start()
                    }
                    val decoder = codec ?: break
                    val index = decoder.dequeueInputBuffer(10_000)
                    if (index >= 0) {
                        val buf = decoder.getInputBuffer(index) ?: continue
                        buf.clear()
                        val annex = pkt.annex
                        if (annex.size > buf.capacity()) continue
                        buf.put(annex)
                        decoder.queueInputBuffer(index, 0, annex.size, pkt.seq.toLong() * 66_000L, 0)
                    }
                    val info = MediaCodec.BufferInfo()
                    val out = decoder.dequeueOutputBuffer(info, 0)
                    if (out >= 0) decoder.releaseOutputBuffer(out, true)
                }
            } catch (err: Exception) {
                runOnUiThread { status(err.message ?: "会话结束") }
            } finally {
                runCatching { link?.close() }
                runCatching { codec?.stop() }
                runCatching { codec?.release() }
                runOnUiThread {
                    status("会话已停止。撤销或关闭后不再解码。")
                }
            }
        }.start()
    }

    private fun status(text: String) {
        findViewById<TextView>(R.id.status).text = text
    }
}
