package net.scyde.streamero.music

import android.app.Application
import android.content.ComponentName
import android.content.Context
import android.net.Uri
import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.Player
import androidx.media3.session.MediaController
import androidx.media3.session.SessionToken
import com.google.common.util.concurrent.MoreExecutors
import java.util.concurrent.ExecutionException
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException
import kotlinx.coroutines.CoroutineExceptionHandler
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.suspendCancellableCoroutine
import net.scyde.streamero.api.Api
import net.scyde.streamero.api.DeviceProfile
import net.scyde.streamero.api.ItemSummary
import net.scyde.streamero.api.PlaybackRequest

/** What the phone plays without help (the server converts anything else). */
val AUDIO_PROFILE = DeviceProfile(
    containers = listOf("mp3", "flac", "mp4", "aac", "ogg", "wav", "webm"),
    videoCodecs = emptyList(), audioCodecs = listOf("mp3", "flac", "aac", "alac", "opus", "vorbis", "pcm_s16le"), hls = true,
)

/**
 * Connects to [MusicService] without blocking or polling. [onGone] is told when the
 * service goes away (stopped, or the system closed it), after which the controller is useless.
 */
internal suspend fun connectToMusicService(app: Context, onGone: (MediaController) -> Unit): MediaController {
    val token = SessionToken(app, ComponentName(app, MusicService::class.java))
    val future = MediaController.Builder(app, token)
        .setListener(object : MediaController.Listener {
            override fun onDisconnected(controller: MediaController) = onGone(controller)
        })
        .buildAsync()
    return suspendCancellableCoroutine { cont ->
        future.addListener({
            try {
                cont.resume(future.get())
            } catch (e: ExecutionException) {
                cont.resumeWithException(e.cause ?: e)
            } catch (e: Exception) {
                cont.resumeWithException(e)
            }
        }, MoreExecutors.directExecutor())
        // A caller that gave up does not leave a connected controller behind.
        cont.invokeOnCancellation { MediaController.releaseFuture(future) }
    }
}

/**
 * A scope for player commands from taps: a command that fails (the service could not be
 * reached) is dropped instead of closing the app. Only the error type is logged, never a URL.
 */
internal fun playerScope(tag: String) = CoroutineScope(SupervisorJob() + Dispatchers.Main + CoroutineExceptionHandler { _, e ->
    Log.w(tag, "player command failed: ${e.javaClass.simpleName}")
})

/** The app's side of music playback: a queue of tracks and the MediaController of MusicService. */
class MusicController(private val app: Application) {
    var api: Api? = null
    /** Called before music starts, so an audiobook lets go of the player. */
    var onStart: () -> Unit = {}
    var queue by mutableStateOf<List<ItemSummary>>(emptyList())
        private set
    var index by mutableIntStateOf(-1)
        private set
    var playing by mutableStateOf(false)
        private set
    var position by mutableLongStateOf(0L)
        private set
    var duration by mutableLongStateOf(0L)
        private set
    var shuffle by mutableStateOf(false)
    var repeat by mutableIntStateOf(Player.REPEAT_MODE_OFF)
        private set
    val current: ItemSummary? get() = queue.getOrNull(index)

    private val scope = playerScope("MusicController")
    private var ticker: Job? = null

    // The service's player is shared with audiobooks: events while no music is queued are not ours.
    private val listener = object : Player.Listener {
        override fun onIsPlayingChanged(isPlaying: Boolean) {
            if (queue.isEmpty()) return
            playing = isPlaying
            if (isPlaying) startTicker() else { ticker?.cancel(); shared.current?.let { position = it.currentPosition } }
        }
        override fun onMediaItemTransition(mediaItem: MediaItem?, reason: Int) {
            if (queue.isEmpty()) return
            val c = shared.current ?: return
            // By id: while the queue is still being resolved the player holds fewer items than the list shown.
            index = queue.indexOfFirst { it.id == mediaItem?.mediaId }.takeIf { it >= 0 } ?: c.currentMediaItemIndex
            duration = queue.getOrNull(index)?.durationMs ?: 0L
            position = c.currentPosition
        }
    }

    private val shared: Shared<MediaController> = Shared(valid = { it.isConnected }) {
        connectToMusicService(app) { c -> shared.forget(c); ticker?.cancel(); playing = false }.also { it.addListener(listener) }
    }

    private suspend fun controller(): MediaController = shared.get()

    /** The position line moves only while music plays; a paused player costs nothing. */
    private fun startTicker() {
        ticker?.cancel()
        ticker = scope.launch {
            while (isActive) {
                val c = shared.current ?: break
                position = c.currentPosition
                if (c.duration > 0) duration = c.duration
                delay(500)
            }
        }
    }

    private fun cover(t: ItemSummary): Uri? {
        val a = api ?: return null
        val id = if (t.hasPoster) t.id else t.parentId ?: return null
        return Uri.parse(a.imageUrl(id, "poster"))
    }

    private var generation = 0
    private var resolving: Job? = null

    private fun mediaItem(a: Api, t: ItemSummary, url: String): MediaItem =
        MediaItem.Builder().setMediaId(t.id).setUri(a.resolve(url))
            .setMediaMetadata(MediaMetadata.Builder().setTitle(t.title).setArtist(t.artist).setAlbumTitle(t.album).setArtworkUri(cover(t)).build())
            .build()

    /**
     * Plays [tracks] from [start]; each track's stream URL comes from the server (signed). The
     * chosen track starts as soon as its own URL is known; the rest of the queue is resolved in
     * parallel meanwhile and tracks the server cannot play are left out.
     */
    fun play(tracks: List<ItemSummary>, start: Int = 0) {
        val a = api ?: return
        if (tracks.isEmpty()) return
        onStart()
        val from = start.coerceIn(tracks.indices)
        val list = if (shuffle) listOf(tracks[from]) + tracks.filterIndexed { i, _ -> i != from }.shuffled() else tracks
        val first = if (shuffle) 0 else from
        val gen = ++generation
        resolving?.cancel()
        queue = list
        index = first
        resolving = scope.launch {
            val resolve: suspend (ItemSummary) -> String? = { t -> a.playback(t.id, PlaybackRequest(AUDIO_PROFILE)).url }
            val (at, head) = MusicQueue.first(list, first, resolve) ?: run { if (gen == generation) { queue = emptyList(); index = -1 }; return@launch }
            val c = controller()
            if (gen != generation) return@launch
            index = at
            // The list is already in shuffled order; the player shuffling it again would play
            // something other than what the queue shows.
            c.shuffleModeEnabled = false
            c.setMediaItems(listOf(mediaItem(a, head.track, head.stream)), 0, 0)
            c.prepare()
            c.play()
            val (before, after) = MusicQueue.rest(list, at, resolve = resolve)
            if (gen != generation) return@launch
            c.addMediaItems(0, before.map { mediaItem(a, it.track, it.stream) })
            c.addMediaItems(after.map { mediaItem(a, it.track, it.stream) })
            queue = before.map { it.track } + head.track + after.map { it.track }
            index = c.currentMediaItemIndex
        }
    }

    fun toggle() = scope.launch { val c = controller(); if (c.isPlaying) c.pause() else c.play() }
    fun next() = scope.launch { controller().seekToNextMediaItem() }
    fun prev() = scope.launch { controller().seekToPrevious() }
    fun seek(ms: Long) = scope.launch { controller().seekTo(ms); position = ms }
    fun jump(i: Int) = scope.launch {
        val c = controller()
        val id = queue.getOrNull(i)?.id ?: return@launch
        val at = (0 until c.mediaItemCount).firstOrNull { c.getMediaItemAt(it).mediaId == id } ?: return@launch
        c.seekTo(at, 0)
    }
    fun pause() = scope.launch { shared.current?.pause() }
    fun cycleRepeat() = scope.launch {
        repeat = when (repeat) { Player.REPEAT_MODE_OFF -> Player.REPEAT_MODE_ALL; Player.REPEAT_MODE_ALL -> Player.REPEAT_MODE_ONE; else -> Player.REPEAT_MODE_OFF }
        controller().repeatMode = repeat
    }
    fun toggleShuffle() = scope.launch { shuffle = !shuffle; controller().shuffleModeEnabled = shuffle }
    fun stop() {
        generation++
        resolving?.cancel()
        ticker?.cancel()
        shared.current?.run { stop(); clearMediaItems() }
        queue = emptyList(); index = -1; playing = false; position = 0; duration = 0
    }
}
