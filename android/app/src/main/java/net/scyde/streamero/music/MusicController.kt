package net.scyde.streamero.music

import android.app.Application
import android.content.ComponentName
import android.net.Uri
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
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import net.scyde.streamero.api.Api
import net.scyde.streamero.api.DeviceProfile
import net.scyde.streamero.api.ItemSummary
import net.scyde.streamero.api.PlaybackRequest

/** What the phone plays without help (the server converts anything else). */
val AUDIO_PROFILE = DeviceProfile(
    containers = listOf("mp3", "flac", "mp4", "aac", "ogg", "wav", "webm"),
    videoCodecs = emptyList(), audioCodecs = listOf("mp3", "flac", "aac", "alac", "opus", "vorbis", "pcm_s16le"), hls = true,
)

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

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    private var controller: MediaController? = null

    private suspend fun controller(): MediaController {
        controller?.let { return it }
        val token = SessionToken(app, ComponentName(app, MusicService::class.java))
        val future = MediaController.Builder(app, token).buildAsync()
        while (!future.isDone) delay(20)
        return future.get().also { c ->
            controller = c
            c.addListener(object : Player.Listener {
                override fun onIsPlayingChanged(isPlaying: Boolean) { playing = isPlaying }
                override fun onMediaItemTransition(mediaItem: MediaItem?, reason: Int) {
                    index = c.currentMediaItemIndex
                    duration = queue.getOrNull(index)?.durationMs ?: 0L
                }
            })
            scope.launch { while (true) { position = c.currentPosition; if (c.duration > 0) duration = c.duration; delay(500) } }
        }
    }

    private fun cover(t: ItemSummary): Uri? {
        val a = api ?: return null
        val id = if (t.hasPoster) t.id else t.parentId ?: return null
        return Uri.parse(a.imageUrl(id, "poster"))
    }

    /** Plays [tracks] from [start]; each track's stream URL comes from the server (signed). */
    fun play(tracks: List<ItemSummary>, start: Int = 0) {
        val a = api ?: return
        if (tracks.isEmpty()) return
        onStart()
        val list = if (shuffle) listOf(tracks[start]) + tracks.filterIndexed { i, _ -> i != start }.shuffled() else tracks
        val first = if (shuffle) 0 else start
        queue = list
        index = first
        scope.launch {
            val c = controller()
            val items = list.map { t ->
                val info = runCatching { a.playback(t.id, PlaybackRequest(AUDIO_PROFILE)) }.getOrNull()
                MediaItem.Builder().setMediaId(t.id).setUri(info?.let { a.resolve(it.url) } ?: "")
                    .setMediaMetadata(MediaMetadata.Builder().setTitle(t.title).setArtist(t.artist).setAlbumTitle(t.album).setArtworkUri(cover(t)).build())
                    .build()
            }
            c.setMediaItems(items, first, 0)
            c.prepare()
            c.play()
        }
    }

    fun toggle() = scope.launch { val c = controller(); if (c.isPlaying) c.pause() else c.play() }
    fun next() = scope.launch { controller().seekToNextMediaItem() }
    fun prev() = scope.launch { controller().seekToPrevious() }
    fun seek(ms: Long) = scope.launch { controller().seekTo(ms) }
    fun jump(i: Int) = scope.launch { controller().seekTo(i, 0) }
    fun pause() = scope.launch { controller?.pause() }
    fun cycleRepeat() = scope.launch {
        repeat = when (repeat) { Player.REPEAT_MODE_OFF -> Player.REPEAT_MODE_ALL; Player.REPEAT_MODE_ALL -> Player.REPEAT_MODE_ONE; else -> Player.REPEAT_MODE_OFF }
        controller().repeatMode = repeat
    }
    fun toggleShuffle() = scope.launch { shuffle = !shuffle; controller().shuffleModeEnabled = shuffle }
    fun stop() {
        controller?.stop(); controller?.clearMediaItems()
        queue = emptyList(); index = -1; playing = false
    }

    init { MoreExecutors.directExecutor() }
}
