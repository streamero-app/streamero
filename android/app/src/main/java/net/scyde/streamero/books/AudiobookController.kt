package net.scyde.streamero.books

import android.app.Application
import android.content.ComponentName
import android.net.Uri
import android.os.SystemClock
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Player
import androidx.media3.session.MediaController
import androidx.media3.session.SessionToken
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import net.scyde.streamero.api.Api
import net.scyde.streamero.api.ItemSummary
import net.scyde.streamero.api.PlaybackRequest
import net.scyde.streamero.api.ProgressReport
import net.scyde.streamero.music.AUDIO_PROFILE
import net.scyde.streamero.music.MusicService

private const val REPORT_EVERY_MS = 15_000L

/**
 * Audiobook playback. Uses the same background service as music (lock screen, headphones,
 * screen off), with the book's files as the queue and positions in the whole book.
 */
class AudiobookController(private val app: Application) {
    var api: Api? = null
    /** Called before a book starts, so music lets go of the player. */
    var onStart: () -> Unit = {}

    var book by mutableStateOf<ItemSummary?>(null)
        private set
    var timeline by mutableStateOf<AudiobookTimeline?>(null)
        private set
    var playing by mutableStateOf(false)
        private set
    var loading by mutableStateOf(false)
        private set
    var error by mutableStateOf<Throwable?>(null)
        private set
    /** Position in the whole book. */
    var position by mutableLongStateOf(0L)
        private set
    var speed by mutableFloatStateOf(1f)
        private set
    /** Sleep timer: the chosen minutes (0 = end of chapter), null when off. */
    var sleep by mutableStateOf<Int?>(null)
        private set
    private var sleepDeadline = 0L // elapsedRealtime for a timer in minutes
    private var sleepAt = 0L // book position for the end of a chapter

    val duration: Long get() = timeline?.durationMs ?: 0L
    val chapter: Int get() = timeline?.chapterAt(position) ?: -1

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    private var controller: MediaController? = null
    private var ticker: Job? = null
    private var lastReport = 0L

    private suspend fun controller(): MediaController {
        controller?.let { return it }
        val token = SessionToken(app, ComponentName(app, MusicService::class.java))
        val future = MediaController.Builder(app, token).buildAsync()
        while (!future.isDone) delay(20)
        return future.get().also { c ->
            controller = c
            c.addListener(object : Player.Listener {
                override fun onIsPlayingChanged(isPlaying: Boolean) {
                    if (book == null) return
                    playing = isPlaying
                    if (!isPlaying) report()
                }
            })
        }
    }

    private fun cover(b: ItemSummary): Uri? = api?.takeIf { b.hasPoster }?.let { Uri.parse(it.imageUrl(b.id, "poster")) }

    /** Opens [b] and starts it at the saved position (or at [startMs]). */
    fun play(b: ItemSummary, startMs: Long? = null) {
        val a = api ?: return
        if (book?.id == b.id && timeline != null) {
            if (startMs != null) seekTo(startMs)
            scope.launch { controller().play() }
            return
        }
        report()
        onStart()
        book = b
        timeline = null
        loading = true
        error = null
        speed = 1f
        cancelSleep()
        scope.launch {
            runCatching {
                val m = a.listen(b.id)
                val t = AudiobookTimeline(m.parts, m.chapters, m.durationMs)
                // Each file has its own signed stream URL; the player moves through them as a queue.
                val items = m.parts.map { p ->
                    val info = a.playback(b.id, PlaybackRequest(AUDIO_PROFILE, fileId = p.fileId, startMs = 0))
                    MediaItem.Builder().setMediaId(p.fileId).setUri(a.resolve(info.url))
                        .setMediaMetadata(MediaMetadata.Builder().setTitle(b.title).setArtist(b.authors?.joinToString(", "))
                            .setAlbumTitle(p.title).setArtworkUri(cover(b)).build())
                        .build()
                }
                val start = startMs ?: m.positionMs.takeIf { it < t.durationMs - 5_000 } ?: 0L
                val (i, local) = t.locate(start)
                val c = controller()
                timeline = t
                position = start
                c.setMediaItems(items, i, local)
                c.setPlaybackSpeed(1f)
                c.prepare()
                c.play()
                startTicker()
            }.onFailure { error = it; book = null }
            loading = false
        }
    }

    private fun startTicker() {
        ticker?.cancel()
        ticker = scope.launch {
            while (true) {
                val c = controller
                val t = timeline
                if (c != null && t != null && book != null) {
                    position = t.global(c.currentMediaItemIndex, c.currentPosition)
                    val now = SystemClock.elapsedRealtime()
                    when {
                        sleep == SLEEP_END_OF_CHAPTER && position >= sleepAt -> { c.pause(); cancelSleep() }
                        sleep != null && sleep != SLEEP_END_OF_CHAPTER && now >= sleepDeadline -> { c.pause(); cancelSleep() }
                    }
                    if (c.isPlaying && now - lastReport >= REPORT_EVERY_MS) report()
                }
                delay(500)
            }
        }
    }

    /** Tells the server where the listener is (every 15 s, on pause and when leaving the book). */
    fun report() {
        val a = api ?: return
        val b = book ?: return
        val d = duration.takeIf { it > 0 } ?: return
        lastReport = SystemClock.elapsedRealtime()
        val pos = position
        scope.launch { runCatching { a.progress(b.id, ProgressReport(pos, d)) } }
    }

    fun toggle() = scope.launch { val c = controller(); if (c.isPlaying) c.pause() else c.play() }
    fun pause() = scope.launch { controller?.pause() }

    fun seekTo(ms: Long) {
        val t = timeline ?: return
        val target = ms.coerceIn(0, t.durationMs)
        val (i, local) = t.locate(target)
        position = target
        scope.launch { controller().seekTo(i, local) }
    }

    fun skip(deltaMs: Long) = seekTo(position + deltaMs)

    fun jumpToChapter(i: Int) {
        val c = timeline?.chapters?.getOrNull(i) ?: return
        seekTo(c.startMs)
    }

    fun changeSpeed(s: Float) {
        speed = s
        scope.launch { controller().setPlaybackSpeed(s) }
    }

    /** Pauses after [minutes] (or at the end of the chapter for [SLEEP_END_OF_CHAPTER]); null turns it off. */
    fun startSleepTimer(minutes: Int?) {
        if (minutes == null) { cancelSleep(); return }
        sleep = minutes
        if (minutes == SLEEP_END_OF_CHAPTER) sleepAt = timeline?.chapterEnd(position) ?: duration
        else sleepDeadline = SystemClock.elapsedRealtime() + minutes * 60_000L
    }

    /** What is left of the sleep timer, in real time. */
    fun sleepLeftMs(): Long? = when (sleep) {
        null -> null
        SLEEP_END_OF_CHAPTER -> ((sleepAt - position).coerceAtLeast(0) / speed).toLong()
        else -> (sleepDeadline - SystemClock.elapsedRealtime()).coerceAtLeast(0)
    }

    private fun cancelSleep() { sleep = null; sleepDeadline = 0; sleepAt = 0 }

    /** Saves the position and lets go of the player (music took over or the user stopped). */
    fun release(stopPlayer: Boolean) {
        if (book == null) return
        report()
        ticker?.cancel()
        if (stopPlayer) controller?.run { stop(); clearMediaItems() }
        controller?.setPlaybackSpeed(1f)
        book = null; timeline = null; playing = false; position = 0; cancelSleep()
    }
}
