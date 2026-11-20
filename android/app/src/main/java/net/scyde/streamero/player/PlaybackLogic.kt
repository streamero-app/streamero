package net.scyde.streamero.player

import net.scyde.streamero.api.DeviceProfile
import net.scyde.streamero.api.MediaSegment
import net.scyde.streamero.api.PlaybackInfo

// Playback rules without Android types, so they are unit-tested on the JVM.

/**
 * A stream every Android device decodes (the same profile the web player falls back to):
 * after the player fails on what the device said it could play, playback restarts with this.
 */
val SAFE_PROFILE = DeviceProfile(containers = listOf("mp4"), videoCodecs = listOf("h264"), audioCodecs = listOf("aac"), hls = true)

/** What is shown as subtitles. */
sealed interface SubtitleChoice {
    data object Off : SubtitleChoice
    /** A text track drawn by the player: a position in [PlaybackInfo.subtitles]. */
    data class Text(val position: Int) : SubtitleChoice
    /** A bitmap track (PGS, VobSub) burned into the picture by the server: an index into the file's subtitle tracks. */
    data class Burned(val index: Int) : SubtitleChoice
}

object Subtitles {
    /**
     * The choice once a stream starts. [requested] is the file subtitle index picked on the title
     * page (null = automatic from the viewer's preferences, -1 = off); the player's list only holds
     * text tracks, so it is matched by the track's index, never by position.
     */
    fun initial(pi: PlaybackInfo, requested: Int?): SubtitleChoice {
        pi.burnedSubtitle?.let { return SubtitleChoice.Burned(it) }
        if (requested == null) return pi.subtitleDefault?.takeIf { it in pi.subtitles.indices }?.let { SubtitleChoice.Text(it) } ?: SubtitleChoice.Off
        if (requested < 0) return SubtitleChoice.Off
        val at = pi.subtitles.indexOfFirst { it.index == requested }
        return if (at >= 0) SubtitleChoice.Text(at) else SubtitleChoice.Off
    }

    /** After a restart (seek, audio switch, fallback): keep the text choice, or the burned track the stream carries. */
    fun afterReload(pi: PlaybackInfo, previous: SubtitleChoice): SubtitleChoice = when {
        pi.burnedSubtitle != null -> SubtitleChoice.Burned(pi.burnedSubtitle)
        previous is SubtitleChoice.Text && previous.position in pi.subtitles.indices -> previous
        else -> SubtitleChoice.Off
    }

    /** Whether switching from [from] to [to] needs a new stream (burning a track in, or taking one out). */
    fun needsReload(from: SubtitleChoice, to: SubtitleChoice): Boolean =
        (to is SubtitleChoice.Burned && to != from) || (from is SubtitleChoice.Burned && to !is SubtitleChoice.Burned)

    /** The subtitleIndex to ask for when the stream restarts: the burned track stays, otherwise none (text tracks are side-loaded). */
    fun requestIndex(choice: SubtitleChoice): Int = (choice as? SubtitleChoice.Burned)?.index ?: -1

    /**
     * The WebVTT URL for one text track of [pi]. An HLS stream (remux, transcode) that starts at
     * startMs has its time 0 there, so the cues are asked for that much earlier (`offsetMs`, not
     * part of the signature; older servers ignore it). Direct play keeps the file's own times.
     */
    fun url(trackUrl: String, pi: PlaybackInfo): String {
        if (pi.method == "direct" || pi.startMs <= 0) return trackUrl
        val base = trackUrl.substringBefore('#')
        return base + (if ('?' in base) "&" else "?") + "offsetMs=" + pi.startMs
    }

    /** The id given to a side-loaded text track, so it can be selected without a reload. */
    fun trackId(index: Int) = "streamero-sub-$index"
}

/** What went wrong, as far as recovery cares. */
enum class ErrorKind {
    /** The stream could not be read: the network dropped, or the server ended or refused it. */
    Source,
    /** A live stream was paused for so long that its position left the playlist. */
    BehindLive,
    /** The device could not decode or render what it was given. */
    Playback,
}

/**
 * Sorts a Media3 `PlaybackException.errorCode`: 1002 is "behind live window", the 2xxx range is
 * I/O; everything else (container, decoder, audio sink) is the device failing to play.
 */
fun errorKind(code: Int): ErrorKind = when (code) {
    1002 -> ErrorKind.BehindLive
    in 2000..2999 -> ErrorKind.Source
    else -> ErrorKind.Playback
}

/** What to do when the player reports an error. */
enum class ErrorAction {
    /** Ask for the same stream again from the current position. */
    Retry,
    /** Jump back to the live edge and carry on. */
    Resync,
    /** Restart with [SAFE_PROFILE]. */
    Fallback,
    Fail,
}

/**
 * A failure of the device restarts once with [SAFE_PROFILE] from the same position; a second one
 * is shown. A stream that could not be read is first asked for again as it was: a network blip
 * must not leave the rest of the film converted to a lower quality.
 */
class ErrorRecovery {
    companion object {
        /** A stream that fails again sooner than this after being asked for again is not retried. */
        const val RETRY_GAP_MS = 60_000L
    }

    var safe = false
        private set
    private var lastRetryAt: Long? = null

    /**
     * [nowMs] is any monotonic clock. A read failure is retried once per [RETRY_GAP_MS], so a
     * blip an hour into a film is retried too, but a stream that keeps failing is not retried
     * forever.
     */
    fun onError(kind: ErrorKind = ErrorKind.Playback, nowMs: Long = 0L): ErrorAction {
        if (kind == ErrorKind.BehindLive) return ErrorAction.Resync
        val last = lastRetryAt
        if (kind == ErrorKind.Source && (last == null || nowMs - last >= RETRY_GAP_MS)) {
            lastRetryAt = nowMs
            return ErrorAction.Retry
        }
        if (safe) return ErrorAction.Fail
        safe = true
        return ErrorAction.Fallback
    }
}

/**
 * The progress to send when the player closes, or null when there is nothing worth saving:
 * nothing was loaded, it never got past the start, or the end was already reported as watched.
 */
fun closingProgress(loaded: Boolean, positionMs: Long, durationMs: Long?, ended: Boolean): Pair<Long, Long?>? =
    if (!loaded || ended || positionMs <= 0) null else positionMs to durationMs

/**
 * Whether the "next episode" button shows: at the end, in the credits, or in the last 20
 * seconds. A position of 0 (not started or not known yet) never counts as the end.
 */
fun showNextUp(hasNext: Boolean, ended: Boolean, inCredits: Boolean, durationMs: Long, positionMs: Long): Boolean =
    hasNext && (ended || inCredits || (durationMs > 0 && positionMs > 0 && durationMs - positionMs < 20_000))

/**
 * The audio formats to report. AC-3 and E-AC-3 are reported when the device decodes them or
 * can pass them through to the receiver or TV it is connected to; anything else is converted.
 */
fun audioCodecs(ac3Decoder: Boolean, eac3Decoder: Boolean, ac3Passthrough: Boolean, eac3Passthrough: Boolean): List<String> = buildList {
    addAll(listOf("aac", "mp3", "opus", "flac", "vorbis"))
    if (ac3Decoder || ac3Passthrough) add("ac3")
    if (eac3Decoder || eac3Passthrough) add("eac3")
}

/**
 * The screen is kept on while a film runs or is about to (buffering). A paused or finished one
 * lets the device go to sleep like any other screen.
 */
fun keepsScreenOn(playWhenReady: Boolean, buffering: Boolean, ready: Boolean): Boolean = playWhenReady && (buffering || ready)

/** The intro or recap running at [positionMs] (not in its last second, where skipping is pointless). */
fun introAt(segments: List<MediaSegment>, positionMs: Long): MediaSegment? =
    segments.firstOrNull { (it.kind == "intro" || it.kind == "recap") && positionMs >= it.startMs && positionMs < it.endMs - 1000 }

/** Whether the credits have started at [positionMs]. */
fun inCredits(segments: List<MediaSegment>, positionMs: Long): Boolean = segments.any { it.kind == "credits" && positionMs >= it.startMs }

/**
 * Where a player that is opened again starts: the place it had reached before the system
 * closed the app in the background, otherwise where the title page asked for.
 */
fun startPosition(savedMs: Long, requestedMs: Long?): Long? = savedMs.takeIf { it > 0 } ?: requestedMs

/**
 * Whether a player that reported the end is still at it after [playbackState] (Media3's
 * `Player.STATE_*`: 3 ready, 2 buffering, 4 ended): seeking back from the end starts the film
 * again, so the end is reported again when it comes.
 */
fun stillEnded(ended: Boolean, playbackState: Int): Boolean = when (playbackState) {
    4 -> true
    2, 3 -> false
    else -> ended
}

/** Whether the next episode starts on its own when this one ends. */
fun autoplayNextOnEnd(autoplay: Boolean, hasNext: Boolean, grouped: Boolean): Boolean = autoplay && hasNext && !grouped

/** One video decoder as far as the device profile cares. */
data class VideoDecoder(val mime: String, val hardware: Boolean, val main10: Boolean = false)

/**
 * The video codecs to report. HEVC only with a hardware decoder that does Main 10: most HEVC
 * files (and all HDR ones) are 10-bit, and reporting HEVC without it plays a black screen
 * instead of letting the server convert. 8-bit-only devices get HEVC transcoded to H.264.
 * AV1 likewise only with a hardware decoder.
 */
fun videoCodecs(decoders: List<VideoDecoder>): List<String> = buildList {
    add("h264")
    if (decoders.any { it.mime == "video/hevc" && it.hardware && it.main10 }) add("hevc")
    if (decoders.any { it.mime == "video/x-vnd.on2.vp9" }) add("vp9")
    // The software AV1 decoder every recent Android ships cannot keep up with full HD.
    if (decoders.any { it.mime == "video/av01" && it.hardware }) add("av1")
}

/** Keeps the earlier playWhenReady across a reload (a paused viewer stays paused). */
fun playWhenReadyAfterReload(wasPlaying: Boolean?, grouped: Boolean): Boolean = if (grouped) false else wasPlaying ?: true

/** How subtitles look (Settings → Subtitles), the same sizes and colours as the web player. */
data class CaptionLook(val foreground: Int, val background: Int, val shadow: Boolean, val scale: Float)

fun captionLook(size: String?, color: String?, background: String?): CaptionLook {
    val bg = background ?: "shadow"
    return CaptionLook(
        foreground = when (color) { "yellow" -> 0xFFFFE14D.toInt(); "cyan" -> 0xFF7FF3FF.toInt(); else -> 0xFFFFFFFF.toInt() },
        background = if (bg == "box") 0xBF000000.toInt() else 0,
        shadow = bg == "shadow",
        scale = when (size) { "small" -> 0.76f; "large" -> 1.29f; "xlarge" -> 1.65f; else -> 1f },
    )
}
