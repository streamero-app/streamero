package net.scyde.streamero.ui

import android.app.Activity
import android.app.PictureInPictureParams
import android.content.Context
import android.content.ContextWrapper
import android.content.pm.PackageManager
import android.media.MediaCodecInfo.CodecProfileLevel
import android.media.MediaCodecList
import android.os.Build
import android.util.Rational
import androidx.activity.ComponentActivity
import androidx.annotation.OptIn
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.app.PictureInPictureModeChangedInfo
import androidx.core.util.Consumer
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.Player
import androidx.media3.common.VideoSize
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.CaptionStyleCompat
import androidx.media3.ui.PlayerView
import androidx.media3.ui.SubtitleView
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import net.scyde.streamero.R
import net.scyde.streamero.api.DeviceProfile
import net.scyde.streamero.api.Preferences
import net.scyde.streamero.player.VideoDecoder
import net.scyde.streamero.player.captionLook
import net.scyde.streamero.player.videoCodecs

/**
 * A video player that behaves like one: takes audio focus (a call or another app pauses it,
 * focus loss ducks or pauses) and pauses when headphones are unplugged.
 */
@OptIn(UnstableApi::class)
fun videoPlayer(ctx: Context): ExoPlayer = ExoPlayer.Builder(ctx)
    .setAudioAttributes(AudioAttributes.Builder().setUsage(C.USAGE_MEDIA).setContentType(C.AUDIO_CONTENT_TYPE_MOVIE).build(), true)
    .setHandleAudioBecomingNoisy(true)
    .build()

private class Codecs(val video: List<String>, val ac3: Boolean, val eac3: Boolean)
@Volatile private var codecs: Codecs? = null

/** Reads the decoders once (it takes a while on some devices), off the main thread. */
private suspend fun codecs(): Codecs = codecs ?: withContext(Dispatchers.Default) {
    val infos = MediaCodecList(MediaCodecList.REGULAR_CODECS).codecInfos.filter { !it.isEncoder }
    val decoders = infos.flatMap { info ->
        val hardware = if (Build.VERSION.SDK_INT >= 29) info.isHardwareAccelerated
            else !info.name.lowercase().let { it.startsWith("omx.google.") || it.startsWith("c2.android.") }
        info.supportedTypes.map { type ->
            val main10 = type.equals("video/hevc", true) && runCatching {
                info.getCapabilitiesForType(type).profileLevels.any {
                    it.profile == CodecProfileLevel.HEVCProfileMain10 || it.profile == CodecProfileLevel.HEVCProfileMain10HDR10 ||
                        (Build.VERSION.SDK_INT >= 29 && it.profile == CodecProfileLevel.HEVCProfileMain10HDR10Plus)
                }
            }.getOrDefault(false)
            VideoDecoder(type.lowercase(), hardware, main10)
        }
    }
    val types = decoders.map { it.mime }.toSet()
    Codecs(videoCodecs(decoders), "audio/ac3" in types, "audio/eac3" in types).also { codecs = it }
}

/** What this device decodes (the server converts the rest). */
suspend fun androidDeviceProfile(prefs: Preferences?): DeviceProfile {
    val c = codecs()
    val audio = buildList {
        addAll(listOf("aac", "mp3", "opus", "flac", "vorbis"))
        if (c.ac3) add("ac3")
        if (c.eac3) add("eac3")
    }
    return DeviceProfile(listOf("mp4", "m4v", "mov", "mkv", "webm", "ts"), c.video, audio, hls = true,
        maxHeight = prefs?.maxHeight, maxBitrate = prefs?.maxBitrateKbps?.let { it * 1000 })
}

/** The viewer's subtitle size, colour and background (Settings → Subtitles). */
@OptIn(UnstableApi::class)
fun PlayerView.applyCaptionLook(prefs: Preferences?) {
    val look = captionLook(prefs?.subtitleSize, prefs?.subtitleColor, prefs?.subtitleBackground)
    subtitleView?.apply {
        setApplyEmbeddedStyles(false)
        setStyle(CaptionStyleCompat(look.foreground, look.background, android.graphics.Color.TRANSPARENT,
            if (look.shadow) CaptionStyleCompat.EDGE_TYPE_DROP_SHADOW else CaptionStyleCompat.EDGE_TYPE_NONE, android.graphics.Color.BLACK, null))
        setFractionalTextSize(SubtitleView.DEFAULT_TEXT_SIZE_FRACTION * look.scale)
    }
}

fun Context.findActivity(): Activity? {
    var c: Context? = this
    while (c is ContextWrapper) { if (c is Activity) return c; c = c.baseContext }
    return null
}

private fun pipParams(size: VideoSize, autoEnter: Boolean): PictureInPictureParams {
    // Android refuses ratios outside 1:2.39 … 2.39:1.
    val ratio = if (size.width > 0 && size.height > 0) {
        val r = size.width * size.pixelWidthHeightRatio / size.height
        when { r > 2.39f -> Rational(239, 100); r < 1 / 2.39f -> Rational(100, 239); else -> Rational((size.width * size.pixelWidthHeightRatio).toInt(), size.height) }
    } else Rational(16, 9)
    return PictureInPictureParams.Builder().setAspectRatio(ratio).apply { if (Build.VERSION.SDK_INT >= 31) setAutoEnterEnabled(autoEnter) }.build()
}

/**
 * The player's life with the app: playback pauses when the app goes to the background, and with
 * the "picture in picture" enhancement on (phones only) leaving the app while a video plays keeps
 * it in a small window instead. Returns whether the video is in that window now (hide the controls).
 */
@Composable
fun videoLifecycle(player: Player, autoPip: Boolean): Boolean {
    val ctx = LocalContext.current
    val act = remember(ctx) { ctx.findActivity() as? ComponentActivity }
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val tv = LocalTv.current
    var inPip by remember { mutableStateOf(act?.isInPictureInPictureMode == true) }
    val canPip = autoPip && !tv && act != null && ctx.packageManager.hasSystemFeature(PackageManager.FEATURE_PICTURE_IN_PICTURE)

    DisposableEffect(lifecycle, player) {
        // Stopped: in the background, or the picture-in-picture window was closed.
        val obs = LifecycleEventObserver { _, e -> if (e == Lifecycle.Event.ON_STOP) player.pause() }
        lifecycle.addObserver(obs)
        onDispose { lifecycle.removeObserver(obs) }
    }
    DisposableEffect(act, player, canPip) {
        if (act == null) return@DisposableEffect onDispose { }
        val modeChanged = Consumer<PictureInPictureModeChangedInfo> { inPip = it.isInPictureInPictureMode }
        act.addOnPictureInPictureModeChangedListener(modeChanged)
        // Android 12+ enters on its own (smoother); before that, on the "leaving the app" hint.
        fun update() { if (canPip) runCatching { act.setPictureInPictureParams(pipParams(player.videoSize, player.isPlaying)) } }
        val leaving = Runnable {
            if (canPip && Build.VERSION.SDK_INT < 31 && player.isPlaying) runCatching { act.enterPictureInPictureMode(pipParams(player.videoSize, false)) }
        }
        act.addOnUserLeaveHintListener(leaving)
        val l = object : Player.Listener {
            override fun onIsPlayingChanged(isPlaying: Boolean) = update()
            override fun onVideoSizeChanged(videoSize: VideoSize) = update()
        }
        player.addListener(l)
        update()
        onDispose {
            act.removeOnPictureInPictureModeChangedListener(modeChanged)
            act.removeOnUserLeaveHintListener(leaving)
            player.removeListener(l)
            if (canPip) runCatching { act.setPictureInPictureParams(pipParams(VideoSize.UNKNOWN, false)) }
        }
    }
    return inPip
}

/**
 * The "pause screen" enhancement: a few seconds into a pause the film's title and synopsis
 * fade in over a dimmed picture, like a cinema between reels.
 */
@Composable
fun BoxScope.PauseInfo(player: Player, enabled: Boolean, title: String?, subtitle: String?, overview: String?) {
    var paused by remember { mutableStateOf(false) }
    DisposableEffect(player) {
        val l = object : Player.Listener {
            override fun onIsPlayingChanged(isPlaying: Boolean) { paused = !isPlaying && player.playbackState == Player.STATE_READY && !player.playWhenReady }
            override fun onPlayWhenReadyChanged(playWhenReady: Boolean, reason: Int) { paused = !playWhenReady && player.playbackState == Player.STATE_READY }
        }
        player.addListener(l)
        onDispose { player.removeListener(l) }
    }
    var show by remember { mutableStateOf(false) }
    LaunchedEffect(paused, enabled) { show = false; if (paused && enabled) { delay(5000); show = true } }
    androidx.compose.animation.AnimatedVisibility(show && title != null, Modifier.matchParentSize(),
        enter = androidx.compose.animation.fadeIn(androidx.compose.animation.core.tween(600)), exit = androidx.compose.animation.fadeOut()) {
        Box(Modifier.fillMaxSize().background(Brush.horizontalGradient(listOf(Color.Black.copy(alpha = 0.85f), Color.Black.copy(alpha = 0.2f))))) {
            Column(Modifier.align(Alignment.CenterStart).fillMaxWidth(0.55f).padding(horizontal = 48.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(stringResource(R.string.paused).uppercase(), style = Label, color = Kino.faint)
                subtitle?.let { Text(it.uppercase(), style = Head, color = Kino.muted) }
                Text(title.orEmpty(), style = Display.copy(fontSize = 34.sp), color = Kino.text, maxLines = 2, overflow = TextOverflow.Ellipsis)
                overview?.let { Text(it, color = Kino.text.copy(alpha = 0.8f), fontSize = 15.sp, lineHeight = 22.sp, maxLines = 5, overflow = TextOverflow.Ellipsis) }
            }
        }
    }
}

/** A playback error over the picture, with a way to try again. */
@Composable
fun BoxScope.PlayerErrorBox(error: Throwable, onRetry: (() -> Unit)?, onBack: (() -> Unit)? = null) {
    Column(Modifier.align(Alignment.Center).padding(24.dp).widthIn(max = 460.dp).background(Kino.surface, CardShape).padding(18.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp)) {
        ErrorLine(error)
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            onRetry?.let { PrimaryButton(stringResource(R.string.retry), it, Modifier.initialFocus()) }
            onBack?.let { GlassButton(stringResource(R.string.back), it, if (onRetry == null) Modifier.initialFocus() else Modifier) }
        }
    }
}
