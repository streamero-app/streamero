package net.scyde.streamero.books

import android.app.Application
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
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import net.scyde.streamero.api.Api
import net.scyde.streamero.api.ItemSummary
import net.scyde.streamero.api.PlaybackRequest
import net.scyde.streamero.api.ProgressReport
import net.scyde.streamero.music.AUDIO_PROFILE
import net.scyde.streamero.music.Shared
import net.scyde.streamero.music.connectToMusicService
import net.scyde.streamero.music.playerScope

private const val REPORT_EVERY_MS = 15_000L

/**
 * Whether the sleep timer is due: [sleep] is the chosen minutes ([SLEEP_END_OF_CHAPTER] for the
 * end of the chapter at [sleepAtMs]), null when off; [deadline] is the real time a timer in
 * minutes runs out.
 */
fun sleepDue(sleep: Int?, positionMs: Long, sleepAtMs: Long, now: Long, deadline: Long): Boolean = when (sleep) {
    null -> false
    SLEEP_END_OF_CHAPTER -> positionMs >= sleepAtMs
    else -> now >= deadline
}

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

    private val scope = playerScope("AudiobookController")
    private var ticker: Job? = null
    private var lastReport = 0L

    // The service's player is shared with music: events while no book is open are not ours.
    private val listener = object : Player.Listener {
        override fun onIsPlayingChanged(isPlaying: Boolean) {
            if (book == null) return
            playing = isPlaying
            refreshPosition()
            if (isPlaying) {
                // A timer that ran out while the book was paused is over; it does not stop the listener at once.
                if (sleep != null && sleep != SLEEP_END_OF_CHAPTER && sleepDue(sleep, position, sleepAt, SystemClock.elapsedRealtime(), sleepDeadline)) cancelSleep()
                startTicker()
            } else {
                ticker?.cancel()
                report()
            }
        }
    }

    private val shared: Shared<MediaController> = Shared(valid = { it.isConnected }) {
        connectToMusicService(app) { c -> shared.forget(c); ticker?.cancel(); playing = false }.also { it.addListener(listener) }
    }

    private suspend fun controller(): MediaController = shared.get()

    private fun refreshPosition() {
        val c = shared.current ?: return
        val t = timeline ?: return
        if (book != null && c.mediaItemCount > 0) position = t.global(c.currentMediaItemIndex, c.currentPosition)
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
            try {
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
                // Another book (or music) was started while this one loaded.
                if (book?.id != b.id) return@launch
                timeline = t
                position = start
                c.setMediaItems(items, i, local)
                c.setPlaybackSpeed(1f)
                c.prepare()
                c.play()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                if (book?.id == b.id) { error = e; book = null }
            } finally {
                loading = false
            }
        }
    }

    /** While the book plays: the position, the sleep timer and a report every 15 s. Stops on pause. */
    private fun startTicker() {
        ticker?.cancel()
        ticker = scope.launch {
            while (isActive) {
                val c = shared.current ?: break
                if (book == null || timeline == null) break
                refreshPosition()
                val now = SystemClock.elapsedRealtime()
                if (sleepDue(sleep, position, sleepAt, now, sleepDeadline)) { c.pause(); cancelSleep() }
                if (c.isPlaying && now - lastReport >= REPORT_EVERY_MS) report()
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
    fun pause() = scope.launch { if (book != null) shared.current?.pause() }

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
        refreshPosition()
        report()
        ticker?.cancel()
        val c = shared.current
        if (stopPlayer) c?.run { stop(); clearMediaItems() }
        c?.setPlaybackSpeed(1f)
        book = null; timeline = null; playing = false; position = 0; cancelSleep()
    }
}
