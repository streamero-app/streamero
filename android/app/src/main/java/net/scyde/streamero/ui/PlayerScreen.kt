package net.scyde.streamero.ui

import android.app.Activity
import android.media.MediaCodecList
import android.net.Uri
import android.view.WindowManager
import androidx.annotation.OptIn
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.ClosedCaption
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MimeTypes
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.PlayerView
import androidx.navigation.NavHostController
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.*

/** What this phone decodes in hardware (the server converts the rest). */
fun androidDeviceProfile(prefs: Preferences?): DeviceProfile {
    val types = MediaCodecList(MediaCodecList.REGULAR_CODECS).codecInfos.filter { !it.isEncoder }.flatMap { it.supportedTypes.toList() }.map { it.lowercase() }.toSet()
    val video = buildList {
        add("h264")
        if ("video/hevc" in types) add("hevc")
        if ("video/x-vnd.on2.vp9" in types) add("vp9")
        if ("video/av01" in types) add("av1")
    }
    val audio = buildList {
        addAll(listOf("aac", "mp3", "opus", "flac", "vorbis"))
        if ("audio/ac3" in types) add("ac3")
        if ("audio/eac3" in types) add("eac3")
    }
    return DeviceProfile(listOf("mp4", "m4v", "mov", "mkv", "webm", "ts"), video, audio, hls = true,
        maxHeight = prefs?.maxHeight, maxBitrate = prefs?.maxBitrateKbps?.let { it * 1000 })
}

@OptIn(UnstableApi::class)
@Composable
fun PlayerScreen(state: AppState, nav: NavHostController, itemId: String, start: Long?, file: String?, audioChoice: Int?, subChoice: Int?) {
    val api = state.api ?: return
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val player = remember { ExoPlayer.Builder(ctx).build() }
    var info by remember { mutableStateOf<PlaybackInfo?>(null) }
    var detail by remember { mutableStateOf<ItemDetail?>(null) }
    var prefs by remember { mutableStateOf<Preferences?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var subtitle by remember { mutableIntStateOf(-1) }
    var audio by remember { mutableStateOf(audioChoice) }
    var position by remember { mutableLongStateOf(0L) }
    var notice by remember { mutableStateOf<String?>(null) }
    var next by remember { mutableStateOf<ItemSummary?>(null) }
    var panel by remember { mutableStateOf(false) }
    var skipped by remember { mutableStateOf<Long?>(null) }
    val offset = remember { mutableLongStateOf(0L) } // HLS starts at info.startMs

    // Full screen, landscape-friendly, screen kept on.
    DisposableEffect(Unit) {
        val act = ctx as Activity
        state.music.pause()
        act.window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        val ctl = WindowCompat.getInsetsController(act.window, act.window.decorView)
        ctl.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        ctl.hide(WindowInsetsCompat.Type.systemBars())
        onDispose {
            ctl.show(WindowInsetsCompat.Type.systemBars())
            act.window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
            player.release()
        }
    }

    fun abs(): Long = offset.longValue + player.currentPosition

    fun load(startMs: Long?, audioIndex: Int?, subIndex: Int?) = scope.launch {
        runCatching { api.playback(itemId, PlaybackRequest(androidDeviceProfile(prefs), file, audioIndex, startMs?.coerceAtLeast(0))) }
            .onSuccess { pi ->
                info = pi
                val chosenSub = subIndex ?: pi.subtitleDefault ?: -1
                subtitle = chosenSub
                val subs = pi.subtitles.getOrNull(chosenSub)?.let { s ->
                    listOf(MediaItem.SubtitleConfiguration.Builder(Uri.parse(api.resolve(s.url))).setMimeType(MimeTypes.TEXT_VTT)
                        .setLanguage(s.language).setSelectionFlags(C.SELECTION_FLAG_DEFAULT).build())
                }.orEmpty()
                val item = MediaItem.Builder().setUri(api.resolve(pi.url)).setSubtitleConfigurations(subs)
                    .apply { if (pi.method != "direct") setMimeType(MimeTypes.APPLICATION_M3U8) }.build()
                offset.longValue = if (pi.method == "direct") 0 else pi.startMs
                player.setMediaItem(item, if (pi.method == "direct") pi.startMs else 0)
                player.prepare()
                player.play()
            }.onFailure { error = it }
    }

    LaunchedEffect(itemId) {
        prefs = runCatching { api.preferences() }.getOrNull()
        detail = runCatching { api.item(itemId) }.getOrNull()
        next = api.nextEpisode(itemId)
        load(start, audioChoice, subChoice)
    }
    // Progress every 10 s; messages and stop requests from an administrator every 4 s.
    LaunchedEffect(info) {
        var tick = 0
        while (info != null) {
            delay(1000)
            position = abs()
            if (++tick % 10 == 0 && player.isPlaying) runCatching { api.progress(itemId, ProgressReport(position, info?.durationMs)) }
            if (tick % 4 == 0) runCatching { api.commands() }.getOrNull()?.forEach { c ->
                if (c.kind == "stop") { player.pause(); notice = c.text ?: ctx.getString(R.string.stopped_by_admin); delay(4000); nav.popBackStack(); return@LaunchedEffect }
                notice = c.text
            }
        }
    }
    LaunchedEffect(notice) { if (notice != null) { delay(10_000); notice = null } }

    val dur = info?.durationMs ?: detail?.item?.durationMs ?: 0L
    val intro = info?.segments?.firstOrNull { (it.kind == "intro" || it.kind == "recap") && position in it.startMs until it.endMs - 1000 }
    val introMode = prefs?.skipIntro ?: "button"
    LaunchedEffect(intro?.startMs) {
        if (intro != null && introMode == "auto" && skipped != intro.startMs) { skipped = intro.startMs; seekTo(player, info, offset, intro.endMs) { s -> load(s, audio, subtitle) } }
    }
    val inCredits = info?.segments?.any { it.kind == "credits" && position >= it.startMs } == true
    val showNext = next != null && (inCredits || (dur > 0 && dur - position < 20_000 && position > 0))

    Box(Modifier.fillMaxSize().background(Color.Black)) {
        AndroidView({ PlayerView(it).apply { this.player = player; setShowSubtitleButton(false); setShowNextButton(false); setShowPreviousButton(false) } }, Modifier.fillMaxSize())
        Row(Modifier.align(Alignment.TopStart).padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClick = { runCatching { scope.launch { api.progress(itemId, ProgressReport(abs(), info?.durationMs)) } }; nav.popBackStack() }) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Color.White)
            }
            Text(detail?.item?.let { it.seriesTitle?.let { s -> "$s · " }.orEmpty() + it.title }.orEmpty(), color = Color.White, fontWeight = FontWeight.SemiBold)
            Spacer(Modifier.weight(1f))
            if (info != null && (info!!.subtitles.isNotEmpty() || info!!.audio.size > 1)) IconButton(onClick = { panel = true }) { Icon(Icons.Default.ClosedCaption, stringResource(R.string.subtitles), tint = Color.White) }
        }
        if (intro != null && introMode == "button" && skipped != intro.startMs) Button(onClick = { skipped = intro.startMs; seekTo(player, info, offset, intro.endMs) { s -> load(s, audio, subtitle) } },
            Modifier.align(Alignment.BottomEnd).padding(24.dp, 96.dp), shape = CardShape, colors = ButtonDefaults.buttonColors(containerColor = Color.Black.copy(alpha = 0.7f), contentColor = Color.White)) {
            Icon(Icons.Default.SkipNext, null); Spacer(Modifier.width(6.dp)); Text(stringResource(if (intro.kind == "recap") R.string.skip_recap else R.string.skip_intro))
        }
        if (showNext && (prefs?.skipCredits ?: "button") != "off") Button(onClick = { nav.popBackStack(); nav.watch(next!!.id) },
            Modifier.align(Alignment.BottomEnd).padding(24.dp, 96.dp), shape = CardShape, colors = ButtonDefaults.buttonColors(containerColor = Kino.text, contentColor = Kino.bg)) {
            Text(stringResource(R.string.next_episode) + ": " + next!!.title, maxLines = 1)
        }
        notice?.let { Text(it, color = Color.White, fontSize = 16.sp, modifier = Modifier.align(Alignment.TopCenter).padding(top = 64.dp).background(Color.Black.copy(alpha = 0.85f), CardShape).padding(14.dp)) }
        error?.let { Box(Modifier.align(Alignment.Center).background(Kino.surface, CardShape).padding(16.dp)) { ErrorLine(it) } }
    }
    if (panel) {
        val pi = info!!
        AlertDialog(onDismissRequest = { panel = false }, confirmButton = { TextButton(onClick = { panel = false }) { Text(stringResource(R.string.close)) } },
            containerColor = Kino.surface, title = { Text(stringResource(R.string.audio_and_subtitles)) }, text = {
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    if (pi.audio.size > 1) FieldRow(stringResource(R.string.audio)) {
                        Picker(audio ?: pi.audioIndex ?: 0, pi.audio.mapIndexed { i, a -> i to listOfNotNull(a.language?.uppercase(), a.codec.uppercase(), a.title).joinToString(" · ") },
                            { audio = it; panel = false; load(abs(), it, subtitle) })
                    }
                    if (pi.subtitles.isNotEmpty()) FieldRow(stringResource(R.string.subtitles)) {
                        Picker(subtitle, listOf(-1 to stringResource(R.string.off)) + pi.subtitles.mapIndexed { i, s -> i to listOfNotNull(s.language?.uppercase() ?: "?", if (s.forced) stringResource(R.string.forced) else null).joinToString(" · ") },
                            { subtitle = it; panel = false; load(abs(), audio, it) })
                    }
                }
            })
    }
}

/** Seeks inside the stream when possible; a transcoded stream past what is ready restarts there. */
private fun seekTo(player: ExoPlayer, info: PlaybackInfo?, offset: MutableLongState, ms: Long, restart: (Long) -> Unit) {
    if (info == null) return
    if (info.method == "direct") { player.seekTo(ms); return }
    val rel = ms - offset.longValue
    if (rel >= 0 && rel <= player.bufferedPosition + 30_000) player.seekTo(rel) else restart(ms)
}
