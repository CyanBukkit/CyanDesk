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
import desk.cyan.core.Annex
import java.util.concurrent.atomic.AtomicInteger

class ViewerActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.viewer)
        Hub.viewer = this
        val screen = findViewById<SurfaceView>(R.id.screen)
        screen.holder.addCallback(object : SurfaceHolder.Callback {
            override fun surfaceCreated(holder: SurfaceHolder) {
                Hub.surface = holder.surface
                Playback.ensure(screen)
            }
            override fun surfaceChanged(holder: SurfaceHolder, format: Int, width: Int, height: Int) {
                Hub.surface = holder.surface
            }
            override fun surfaceDestroyed(holder: SurfaceHolder) {
                if (Hub.surface == holder.surface) Hub.surface = null
            }
        })
    }

    override fun onDestroy() {
        if (Hub.viewer === this) Hub.viewer = null
        if (!isChangingConfigurations) Playback.stop()
        super.onDestroy()
    }

    fun showStatus(text: String) {
        findViewById<TextView>(R.id.status).text = text
    }
}

/** Keeps the decrypted decoder across rotation. A new surface reconfigures MediaCodec. */
object Playback {
    private val pointerSeq = AtomicInteger(1)
    @Volatile private var running = false
    private var thread: Thread? = null

    fun ensure(screen: SurfaceView) {
        val agent = Hub.agent
        val session = Hub.sessionId
        val pinned = Hub.pinned
        if (agent == null || session.isEmpty() || pinned.length != 64) {
            Hub.viewer?.showStatus("没有已确认的会话")
            return
        }
        screen.setOnTouchListener { _, event ->
            val current = Hub.link
            if (event.action == MotionEvent.ACTION_UP && Hub.allowInput && running && current != null) {
                val x = (event.x / screen.width.coerceAtLeast(1) * Hub.frameW.coerceAtLeast(2)).toInt()
                val y = (event.y / screen.height.coerceAtLeast(1) * Hub.frameH.coerceAtLeast(2)).toInt()
                runCatching { current.sendPointer(pointerSeq.getAndIncrement().toLong(), x.toLong(), y.toLong()) }
            }
            true
        }
        synchronized(this) {
            if (running) {
                Hub.viewer?.showStatus(if (Hub.allowInput) "加密会话已建立，可以点击画面" else "加密会话已建立，这张票据只有观看")
                return
            }
            running = true
        }
        thread = Thread {
            var link = Hub.link
            var codec: MediaCodec? = null
            var bound: Surface? = null
            try {
                if (link == null) {
                    playbackNote("dialing")
                    link = agent.dial(Hub.ROLE_CONTROLLER.toLong(), session, pinned)
                    Hub.link = link
                    playbackNote("dialed")
                }
                Hub.allowInput = agent.allowsInput(session)
                Hub.viewer?.runOnUiThread {
                    Hub.viewer?.showStatus(if (Hub.allowInput) "加密会话已建立，可以点击画面" else "加密会话已建立，这张票据只有观看")
                }
                Thread {
                    while (running) {
                        try {
                            if (!agent.active(session)) {
                                Hub.link?.close()
                                break
                            }
                        } catch (_: Exception) {
                            Hub.link?.close()
                            break
                        }
                        try {
                            Thread.sleep(400)
                        } catch (_: InterruptedException) {
                            break
                        }
                    }
                }.start()
                while (running) {
                    if (!agent.active(session)) break
                    val surface = Hub.surface
                    if (surface == null || !surface.isValid) {
                        Thread.sleep(40)
                        continue
                    }
                    if (bound !== surface) {
                        runCatching { codec?.stop() }
                        runCatching { codec?.release() }
                        codec = null
                        bound = surface
                    }
                    val pkt = (Hub.link ?: link)!!.recv()
                    if (pkt.kind.toInt() != 3 || pkt.annex == null || pkt.annex.isEmpty()) continue
                    Hub.frameW = pkt.w.toInt().coerceAtLeast(2)
                    Hub.frameH = pkt.h.toInt().coerceAtLeast(2)
                    val decoder = codec ?: MediaCodec.createDecoderByType(MediaFormat.MIMETYPE_VIDEO_AVC).also {
                        val format = MediaFormat.createVideoFormat(MediaFormat.MIMETYPE_VIDEO_AVC, Hub.frameW, Hub.frameH)
                        format.setInteger(MediaFormat.KEY_MAX_INPUT_SIZE, pkt.annex.size.coerceAtLeast(64 * 1024))
                        attachParameterSets(format, pkt.annex)
                        it.configure(format, surface, null, 0)
                        it.start()
                        codec = it
                    }
                    val index = decoder.dequeueInputBuffer(10_000)
                    if (index >= 0) {
                        val buf = decoder.getInputBuffer(index) ?: continue
                        buf.clear()
                        if (pkt.annex.size <= buf.capacity()) {
                            buf.put(pkt.annex)
                            decoder.queueInputBuffer(index, 0, pkt.annex.size, pkt.seq.toLong() * 66_000L, 0)
                        }
                    }
                    val info = MediaCodec.BufferInfo()
                    var out = decoder.dequeueOutputBuffer(info, 0)
                    while (out >= 0) {
                        decoder.releaseOutputBuffer(out, true)
                        out = decoder.dequeueOutputBuffer(info, 0)
                    }
                }
            } catch (err: Exception) {
                playbackNote(err.message ?: err.toString())
                Hub.viewer?.runOnUiThread { Hub.viewer?.showStatus(err.message ?: "会话结束") }
            } finally {
                running = false
                runCatching { link?.close() }
                Hub.link = null
                runCatching { codec?.stop() }
                runCatching { codec?.release() }
                Hub.viewer?.runOnUiThread { Hub.viewer?.showStatus("会话已停止。撤销或关闭后不再解码。") }
            }
        }.also { it.start() }
    }

    fun stop() {
        running = false
        Hub.link?.close()
    }

    private fun playbackNote(text: String) {
        val ctx = Hub.viewer ?: return
        runCatching { java.io.File(ctx.filesDir, "playback-state.txt").writeText(text) }
    }

    private fun attachParameterSets(format: MediaFormat, annex: ByteArray) {
        for (unit in Annex.nalUnits(annex)) {
            if (unit.isEmpty()) continue
            when (unit[0].toInt() and 0x1f) {
                7 -> format.setByteBuffer("csd-0", java.nio.ByteBuffer.wrap(byteArrayOf(0, 0, 0, 1) + unit))
                8 -> format.setByteBuffer("csd-1", java.nio.ByteBuffer.wrap(byteArrayOf(0, 0, 0, 1) + unit))
            }
        }
    }
}
