package net.scyde.streamero.player

import net.scyde.streamero.api.DeviceProfile
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

    /** The id given to a side-loaded text track, so it can be selected without a reload. */
    fun trackId(index: Int) = "streamero-sub-$index"
}

/** What to do when the player reports an error. */
enum class ErrorAction { Fallback, Fail }

/** First failure: restart once with [SAFE_PROFILE] from the same position; a second one is shown. */
class ErrorRecovery {
    var safe = false
        private set

    fun onError(): ErrorAction = if (safe) ErrorAction.Fail else { safe = true; ErrorAction.Fallback }
}

/** Whether the next episode starts on its own when this one ends. */
fun autoplayNextOnEnd(autoplay: Boolean, hasNext: Boolean, grouped: Boolean): Boolean = autoplay && hasNext && !grouped

/** One video decoder as far as the device profile cares. */
data class VideoDecoder(val mime: String, val hardware: Boolean, val main10: Boolean = false)

/**
 * The video codecs to report. HEVC only with a hardware decoder that does Main 10: most HEVC
 * files (and all HDR ones) are 10-bit, and reporting HEVC without it plays a black screen
 * instead of letting the server convert. 8-bit-only devices get HEVC transcoded to H.264.
 */
fun videoCodecs(decoders: List<VideoDecoder>): List<String> = buildList {
    add("h264")
    if (decoders.any { it.mime == "video/hevc" && it.hardware && it.main10 }) add("hevc")
    if (decoders.any { it.mime == "video/x-vnd.on2.vp9" }) add("vp9")
    if (decoders.any { it.mime == "video/av01" }) add("av1")
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
