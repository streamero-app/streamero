package net.scyde.streamero.ui

import android.app.Activity
import android.net.Uri
import android.view.WindowManager
import androidx.annotation.OptIn
import androidx.activity.compose.BackHandler
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.focusable
import androidx.compose.material.icons.filled.Forward10
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Replay10
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
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
import androidx.media3.common.ForwardingPlayer
import androidx.media3.common.MimeTypes
import androidx.media3.common.PlaybackParameters
import androidx.media3.common.Player
import androidx.media3.common.PlaybackException
import androidx.media3.common.TrackSelectionOverride
import androidx.media3.common.Tracks
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.PlayerView
import androidx.navigation.NavHostController
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.*
import net.scyde.streamero.player.ErrorAction
import net.scyde.streamero.player.ErrorRecovery
import net.scyde.streamero.player.SAFE_PROFILE
import net.scyde.streamero.player.SubtitleChoice
import net.scyde.streamero.player.Subtitles
import net.scyde.streamero.player.autoplayNextOnEnd
import net.scyde.streamero.player.playWhenReadyAfterReload
import net.scyde.streamero.sync.PlayerAction
import net.scyde.streamero.sync.PlayerSnapshot
import net.scyde.streamero.sync.SyncSession

/** Short names of bitmap subtitle formats (ffprobe codec names). */
fun bitmapName(codec: String): String {
    val c = codec.lowercase()
    return when {
        "pgs" in c || c == "sup" -> "PGS"
        "dvb" in c -> "DVB"
        "dvd" in c || c == "vobsub" -> "VobSub"
        else -> c.uppercase()
    }
}

/** "EN · AC3 · 6 ch · Commentary". */
@Composable
fun audioLabel(a: AudioStream): String =
    listOfNotNull(a.language?.uppercase(), a.codec.uppercase(), a.channels?.let { stringResource(R.string.channels_n, it) }, a.title).joinToString(" · ")

/** Off, the text tracks, then the bitmap tracks (those are burned into the picture). */
@Composable
fun subtitleOptions(pi: PlaybackInfo): List<Pair<SubtitleChoice, String>> {
    val forced = stringResource(R.string.forced)
    return listOf<Pair<SubtitleChoice, String>>(SubtitleChoice.Off to stringResource(R.string.off)) +
        pi.subtitles.mapIndexed { i, s -> SubtitleChoice.Text(i) to listOfNotNull(s.language?.uppercase() ?: "?", if (s.forced) forced else null).joinToString(" · ") } +
        pi.bitmapSubtitles.map { b -> SubtitleChoice.Burned(b.index) to listOfNotNull(b.language?.uppercase() ?: "?", bitmapName(b.codec), if (b.forced) forced else null).joinToString(" · ") }
}

/** Shows the chosen side-loaded text track (or none) without reloading the stream. */
@OptIn(UnstableApi::class)
private fun applySubtitle(player: ExoPlayer, pi: PlaybackInfo?, choice: SubtitleChoice) {
    val b = player.trackSelectionParameters.buildUpon().clearOverridesOfType(C.TRACK_TYPE_TEXT)
    val text = (choice as? SubtitleChoice.Text)?.let { pi?.subtitles?.getOrNull(it.position) }
    if (text == null) {
        b.setTrackTypeDisabled(C.TRACK_TYPE_TEXT, true)
    } else {
        b.setTrackTypeDisabled(C.TRACK_TYPE_TEXT, false)
        val key = Subtitles.trackId(text.index)
        var found = false
        for (g in player.currentTracks.groups) {
            if (g.type != C.TRACK_TYPE_TEXT) continue
            val i = (0 until g.length).firstOrNull { val f = g.getTrackFormat(it); f.id?.endsWith(key) == true || f.label == key } ?: continue
            b.setOverrideForType(TrackSelectionOverride(g.mediaTrackGroup, i))
            found = true
            break
        }
        // Tracks not known yet: the language picks it now, the override follows in onTracksChanged.
        if (!found) b.setPreferredTextLanguage(text.language)
    }
    player.trackSelectionParameters = b.build()
}

@OptIn(UnstableApi::class)
@Composable
fun PlayerScreen(state: AppState, nav: NavHostController, itemId: String, start: Long?, file: String?, audioChoice: Int?, subChoice: Int?) {
    val api = state.api ?: return
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val player = remember { videoPlayer(ctx) }
    var info by remember { mutableStateOf<PlaybackInfo?>(null) }
    var detail by remember { mutableStateOf<ItemDetail?>(null) }
    var prefs by remember { mutableStateOf<Preferences?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var subtitle by remember { mutableStateOf<SubtitleChoice>(SubtitleChoice.Off) }
    var audio by remember { mutableStateOf(audioChoice) }
    var position by remember { mutableLongStateOf(0L) }
    var notice by remember { mutableStateOf<String?>(null) }
    var next by remember { mutableStateOf<ItemSummary?>(null) }
    var panel by remember { mutableStateOf(false) }
    var skipped by remember { mutableStateOf<Long?>(null) }
    var ended by remember { mutableStateOf(false) }
    var firstStart by remember { mutableStateOf(true) }
    var recovery by remember { mutableStateOf(ErrorRecovery()) }
    var loadJob by remember { mutableStateOf<Job?>(null) }
    val offset = remember { mutableLongStateOf(0L) } // HLS starts at info.startMs
    // Watch together: the group decides what plays (it may switch to another item), the
    // local controls ask the group instead of acting on the player.
    val sync = state.sync
    val group = sync.group
    val grouped = group != null
    var current by remember { mutableStateOf(itemId) }
    var syncEntry by remember { mutableStateOf<String?>(null) }
    var prepared by remember { mutableStateOf(false) }
    val controls = remember { GroupControlledPlayer(player) { rel -> offset.longValue + rel } }
    SideEffect { controls.group = if (grouped) sync else null }

    fun abs(): Long = offset.longValue + player.currentPosition

    /**
     * (Re)starts the stream at [startMs]. [subRequest] is the subtitleIndex for the server: the
     * title page's choice on the first start, afterwards the burned-in track (or -1). A reload
     * keeps the player paused when it was paused.
     */
    fun load(startMs: Long?, audioIndex: Int?, subRequest: Int?,
             autoplay: Boolean = playWhenReadyAfterReload(if (info == null) null else player.playWhenReady, sync.group != null)) {
        prepared = false
        val id = current
        val first = firstStart
        val safe = recovery.safe
        loadJob?.cancel()
        loadJob = scope.launch {
            try {
                val device = if (safe) SAFE_PROFILE.copy(maxHeight = prefs?.maxHeight, maxBitrate = prefs?.maxBitrateKbps?.let { it * 1000 }) else androidDeviceProfile(prefs)
                val pi = api.playback(id, PlaybackRequest(device, if (id == itemId) file else null, audioIndex, startMs?.coerceAtLeast(0), subtitleIndex = subRequest))
                info = pi
                error = null
                subtitle = if (first) Subtitles.initial(pi, subRequest) else Subtitles.afterReload(pi, subtitle)
                firstStart = false
                // Every text track is side-loaded, so switching between them needs no reload. The
                // URLs are used as the server gives them (already aligned with where the stream starts).
                val subs = pi.subtitles.map { s ->
                    val key = Subtitles.trackId(s.index)
                    MediaItem.SubtitleConfiguration.Builder(Uri.parse(api.resolve(s.url))).setId(key).setLabel(key).setMimeType(MimeTypes.TEXT_VTT)
                        .setLanguage(s.language).setSelectionFlags(if (s.forced) C.SELECTION_FLAG_FORCED else 0).build()
                }
                val item = MediaItem.Builder().setUri(api.resolve(pi.url)).setSubtitleConfigurations(subs)
                    .apply { if (pi.method != "direct") setMimeType(MimeTypes.APPLICATION_M3U8) }.build()
                offset.longValue = if (pi.method == "direct") 0 else pi.startMs
                player.setMediaItem(item, if (pi.method == "direct") pi.startMs else 0)
                applySubtitle(player, pi, subtitle)
                player.playWhenReady = autoplay
                player.prepare()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                error = e
            }
        }
    }

    fun reload(startMs: Long? = abs(), autoplay: Boolean? = null) {
        if (autoplay == null) load(startMs, audio, Subtitles.requestIndex(subtitle)) else load(startMs, audio, Subtitles.requestIndex(subtitle), autoplay)
    }

    // Full screen, landscape-friendly, screen kept on.
    DisposableEffect(Unit) {
        val act = ctx.findActivity()
        state.music.pause()
        act?.window?.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        val ctl = act?.let { WindowCompat.getInsetsController(it.window, it.window.decorView) }
        ctl?.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        ctl?.hide(WindowInsetsCompat.Type.systemBars())
        val listener = object : Player.Listener {
            override fun onPlaybackStateChanged(s: Int) {
                if (s == Player.STATE_READY) prepared = true
                if (s == Player.STATE_ENDED) ended = true
            }
            override fun onTracksChanged(tracks: Tracks) { if (subtitle is SubtitleChoice.Text) applySubtitle(player, info, subtitle) }
            // The device could not play what it said it could (10-bit video, an audio format...):
            // once, restart from here with a stream every device plays; after that, say so.
            override fun onPlayerError(e: PlaybackException) {
                when (recovery.onError()) {
                    ErrorAction.Fallback -> { notice = ctx.getString(R.string.player_fallback); reload(autoplay = true) }
                    ErrorAction.Fail -> error = e
                }
            }
        }
        player.addListener(listener)
        onDispose {
            // The last position is read before the player goes and sent on the app's scope,
            // so it arrives even though this screen is already gone.
            val id = current
            val pos = abs()
            val dur = info?.durationMs
            if (info != null && pos > 0 && !ended) state.appScope.launch { runCatching { api.progress(id, ProgressReport(pos, dur)) } }
            ctl?.show(WindowInsetsCompat.Type.systemBars())
            act?.window?.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
            player.removeListener(listener)
            player.release()
            // Closing the player leaves the group, so nobody waits for a screen that is gone.
            if (sync.group != null) sync.leaveInBackground()
        }
    }
    val inPip = videoLifecycle(player, state.enhancements.autoPip)

    LaunchedEffect(current) {
        if (prefs == null) prefs = runCatching { api.preferences() }.getOrNull()
        detail = runCatching { api.item(current) }.getOrNull()
        next = api.nextEpisode(current)
        if (sync.group == null) load(start, audioChoice, subChoice, autoplay = true)
    }
    // In a group, the engine drives the player a few times a second and reports back.
    LaunchedEffect(grouped) {
        if (!grouped) return@LaunchedEffect
        while (isActive) {
            val snap = PlayerSnapshot(syncEntry, prepared && info != null, abs(), player.playWhenReady,
                player.playbackState == Player.STATE_BUFFERING, player.playbackState == Player.STATE_ENDED, player.playbackParameters.speed.toDouble())
            sync.step(snap).forEach { a ->
                when (a) {
                    is PlayerAction.Load -> {
                        val same = a.entry.itemId == itemId
                        syncEntry = a.entry.id
                        if (current != a.entry.itemId) {
                            current = a.entry.itemId; info = null; error = null; ended = false; firstStart = true; recovery = ErrorRecovery()
                        }
                        load(a.startMs, if (same) audioChoice else null, if (same && firstStart) subChoice else Subtitles.requestIndex(subtitle), autoplay = false)
                    }
                    is PlayerAction.Seek -> seekTo(player, info, offset, a.positionMs) { s -> reload(s) }
                    PlayerAction.Play -> player.play()
                    PlayerAction.Pause -> player.pause()
                    is PlayerAction.Speed -> player.setPlaybackSpeed(a.speed.toFloat())
                }
            }
            delay(sync.nextWakeMs().coerceAtLeast(10))
        }
    }
    LaunchedEffect(sync.endedReason) {
        if (sync.endedReason != null) { notice = ctx.getString(R.string.together_left); sync.endedReason = null }
    }
    // Local seeks (skip intro, TV remote) go to the whole group while in one.
    fun seekAbs(ms: Long) {
        if (sync.group != null) sync.send(sync.engine.seek(ms)) else seekTo(player, info, offset, ms) { s -> reload(s) }
    }
    // Progress every 10 s; messages and stop requests from an administrator every 4 s.
    LaunchedEffect(info) {
        var tick = 0
        while (info != null) {
            delay(1000)
            position = abs()
            if (++tick % 10 == 0 && player.isPlaying) runCatching { api.progress(current, ProgressReport(position, info?.durationMs)) }
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
        // In a group every member would skip at once: there it is the button only.
        if (intro != null && introMode == "auto" && !grouped && skipped != intro.startMs) { skipped = intro.startMs; seekAbs(intro.endMs) }
    }
    val inCredits = info?.segments?.any { it.kind == "credits" && position >= it.startMs } == true
    // The group's queue decides what comes next, not this device.
    val nextUp = next.takeIf { !grouped }
    val showNext = nextUp != null && (ended || inCredits || (dur > 0 && dur - position < 20_000 && position > 0))
    val autoplayNext = prefs?.autoplayNext ?: true
    val tv = LocalTv.current
    val leave: () -> Unit = { nav.popBackStack() }
    val playNext: () -> Unit = { nextUp?.let { n -> nav.popBackStack(); nav.watch(n.id) } }
    // The end: marked watched, and with "play the next episode" on, the next one starts.
    LaunchedEffect(ended) {
        if (!ended) return@LaunchedEffect
        val id = current
        val total = info?.durationMs ?: dur
        state.appScope.launch { runCatching { api.progress(id, ProgressReport(if (total > 0) total else abs(), info?.durationMs, played = true)) } }
        if (autoplayNextOnEnd(autoplayNext, nextUp != null, grouped)) playNext()
    }
    // Credits on "automatic" go straight on to the next episode.
    LaunchedEffect(inCredits) {
        if (inCredits && prefs?.skipCredits == "auto" && autoplayNextOnEnd(autoplayNext, nextUp != null, grouped)) playNext()
    }
    fun chooseSubtitle(x: SubtitleChoice) {
        val from = subtitle
        subtitle = x
        panel = false
        if (Subtitles.needsReload(from, x)) reload() else applySubtitle(player, info, x)
    }
    fun chooseAudio(a: Int) { audio = a; panel = false; reload() }
    val retry: () -> Unit = { error = null; if (info == null) load(start, audioChoice, subChoice, autoplay = true) else reload(autoplay = true) }
    val it0 = detail?.item
    val pauseTitle = it0?.let { if (it.kind == "episode") stringResource(R.string.episode_title, it.episodeNumber ?: 0, it.title) else it.title }

    if (tv) {
        Box(Modifier.fillMaxSize()) {
            TvPlayerChrome(controls, info, detail, prefs, dur, position, intro, introMode, skipped, showNext, nextUp, panel, audio, subtitle, error, notice,
                abs = ::abs, setPanel = { panel = it }, onSkipIntro = { i -> skipped = i.startMs; seekAbs(i.endMs) },
                onSeek = ::seekAbs,
                onNext = playNext,
                onAudio = ::chooseAudio,
                onSubtitle = ::chooseSubtitle,
                onRetry = retry,
                onLeave = leave)
            if (!panel) PauseInfo(player, state.enhancements.pauseScreen, pauseTitle, it0?.seriesTitle, detail?.overview)
            // On a TV the group is shown, not operated: back leaves the player and the group.
            group?.let { SyncOverlay(it, sync.connected, null, Modifier.align(Alignment.TopEnd).padding(horizontal = 48.dp, vertical = 32.dp)) }
        }
        return
    }

    BackHandler { if (panel) panel = false else leave() }
    Box(Modifier.fillMaxSize().background(Color.Black)) {
        AndroidView({ PlayerView(it).apply { this.player = controls; setShowSubtitleButton(false); setShowNextButton(false); setShowPreviousButton(false) } }, Modifier.fillMaxSize(),
            update = { v -> v.useController = !inPip; if (inPip) v.hideController(); v.applyCaptionLook(prefs) })
        if (inPip) return@Box
        PauseInfo(player, state.enhancements.pauseScreen, pauseTitle, it0?.seriesTitle, detail?.overview)
        group?.let { SyncOverlay(it, sync.connected, { sync.leaveInBackground() }, Modifier.align(Alignment.TopEnd).padding(top = 64.dp, end = 12.dp)) }
        Row(Modifier.align(Alignment.TopStart).padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClick = leave) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Color.White)
            }
            Text(detail?.item?.let { it.seriesTitle?.let { s -> "$s · " }.orEmpty() + it.title }.orEmpty(), color = Color.White, fontWeight = FontWeight.SemiBold)
            Spacer(Modifier.weight(1f))
            info?.let { pi -> if (pi.subtitles.isNotEmpty() || pi.bitmapSubtitles.isNotEmpty() || pi.audio.size > 1) IconButton(onClick = { panel = true }) { Icon(Icons.Default.ClosedCaption, stringResource(R.string.subtitles), tint = Color.White) } }
        }
        if (intro != null && introMode == "button" && skipped != intro.startMs) Button(onClick = { skipped = intro.startMs; seekAbs(intro.endMs) },
            Modifier.align(Alignment.BottomEnd).padding(24.dp, 96.dp), shape = CardShape, colors = ButtonDefaults.buttonColors(containerColor = Color.Black.copy(alpha = 0.7f), contentColor = Color.White)) {
            Icon(Icons.Default.SkipNext, null); Spacer(Modifier.width(6.dp)); Text(stringResource(if (intro.kind == "recap") R.string.skip_recap else R.string.skip_intro))
        }
        if (showNext && (prefs?.skipCredits ?: "button") != "off") Button(onClick = playNext,
            Modifier.align(Alignment.BottomEnd).padding(24.dp, 96.dp), shape = CardShape, colors = ButtonDefaults.buttonColors(containerColor = Kino.text, contentColor = Kino.bg)) {
            Text(stringResource(R.string.next_episode) + ": " + nextUp!!.title, maxLines = 1)
        }
        notice?.let { Text(it, color = Color.White, fontSize = 16.sp, modifier = Modifier.align(Alignment.TopCenter).padding(top = 64.dp).background(Color.Black.copy(alpha = 0.85f), CardShape).padding(14.dp)) }
        error?.let { PlayerErrorBox(it, retry, leave) }
    }
    if (panel && !inPip) info?.let { pi ->
        AlertDialog(onDismissRequest = { panel = false }, confirmButton = { TextButton(onClick = { panel = false }) { Text(stringResource(R.string.close)) } },
            containerColor = Kino.surface, title = { Text(stringResource(R.string.audio_and_subtitles)) }, text = {
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    if (pi.audio.size > 1) FieldRow(stringResource(R.string.audio)) {
                        Picker(audio ?: pi.audioIndex ?: 0, pi.audio.mapIndexed { i, a -> i to audioLabel(a) }, ::chooseAudio)
                    }
                    if (pi.subtitles.isNotEmpty() || pi.bitmapSubtitles.isNotEmpty()) FieldRow(stringResource(R.string.subtitles)) {
                        Picker(subtitle, subtitleOptions(pi), ::chooseSubtitle)
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

/**
 * The player the on-screen controls (and the TV remote) talk to. Outside a group it passes
 * everything through; in a group, play, pause, seek and speed become group commands and the
 * engine moves the real player once the server answers with the new state.
 */
@OptIn(UnstableApi::class)
private class GroupControlledPlayer(private val inner: Player, private val absolute: (Long) -> Long) : ForwardingPlayer(inner) {
    var group: SyncSession? = null

    private inline fun command(local: () -> Unit, message: (SyncSession) -> SyncClientMessage) {
        val g = group
        if (g == null) local() else g.send(message(g))
    }

    override fun play() = command({ super.play() }) { it.engine.play() }
    override fun pause() = command({ super.pause() }) { it.engine.pause() }
    override fun setPlayWhenReady(playWhenReady: Boolean) =
        command({ super.setPlayWhenReady(playWhenReady) }) { if (playWhenReady) it.engine.play() else it.engine.pause() }
    override fun seekTo(positionMs: Long) = command({ super.seekTo(positionMs) }) { it.engine.seek(absolute(positionMs)) }
    override fun seekTo(mediaItemIndex: Int, positionMs: Long) =
        command({ super.seekTo(mediaItemIndex, positionMs) }) { it.engine.seek(absolute(positionMs)) }
    override fun seekForward() = command({ super.seekForward() }) { it.engine.seek(absolute(inner.currentPosition + inner.seekForwardIncrement)) }
    override fun seekBack() = command({ super.seekBack() }) { it.engine.seek(absolute(inner.currentPosition - inner.seekBackIncrement)) }
    override fun setPlaybackSpeed(speed: Float) = command({ super.setPlaybackSpeed(speed) }) { it.engine.speed(speed.toDouble()) }
    override fun setPlaybackParameters(playbackParameters: PlaybackParameters) =
        command({ super.setPlaybackParameters(playbackParameters) }) { it.engine.speed(playbackParameters.speed.toDouble()) }
}

/**
 * The player on a TV, driven by the remote: left/right seek (with a running "+30 s" note and
 * the seek applied once the presses stop), centre or up/down shows the controls, media keys
 * work at any time, back closes what is open before leaving.
 */
@OptIn(UnstableApi::class)
@Composable
private fun TvPlayerChrome(
    player: Player, info: PlaybackInfo?, detail: ItemDetail?, prefs: Preferences?, dur: Long, position: Long,
    intro: MediaSegment?, introMode: String, skipped: Long?, showNext: Boolean, next: ItemSummary?, panel: Boolean,
    audio: Int?, subtitle: SubtitleChoice, error: Throwable?, notice: String?,
    abs: () -> Long, setPanel: (Boolean) -> Unit, onSkipIntro: (MediaSegment) -> Unit, onSeek: (Long) -> Unit, onNext: () -> Unit,
    onAudio: (Int) -> Unit, onSubtitle: (SubtitleChoice) -> Unit, onRetry: () -> Unit, onLeave: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    var controls by remember { mutableStateOf(false) }
    var touched by remember { mutableIntStateOf(0) } // bumps on every key so the controls stay while in use
    var playing by remember { mutableStateOf(player.isPlaying) }
    var target by remember { mutableStateOf<Long?>(null) }
    var seekDelta by remember { mutableStateOf<Long?>(null) }
    var seekJob by remember { mutableStateOf<kotlinx.coroutines.Job?>(null) }
    val root = remember { FocusRequester() }
    val back = (prefs?.skipBackSeconds ?: 10) * 1000L
    val fwd = (prefs?.skipForwardSeconds ?: 10) * 1000L
    val skipIntro = intro != null && introMode == "button" && skipped != intro.startMs
    val nextButton = showNext && (prefs?.skipCredits ?: "button") != "off"

    DisposableEffect(player) {
        val l = object : androidx.media3.common.Player.Listener {
            override fun onIsPlayingChanged(isPlaying: Boolean) { playing = isPlaying }
        }
        player.addListener(l)
        onDispose { player.removeListener(l) }
    }
    // Hide the controls a few seconds after the last key while the film plays.
    LaunchedEffect(controls, touched, playing) { if (controls && playing) { delay(5000); controls = false } }
    // Nothing else holds the focus: the screen itself takes the keys.
    LaunchedEffect(controls, panel, skipIntro, nextButton) {
        if (!controls && !panel && !skipIntro && !nextButton) { delay(50); runCatching { root.requestFocus() } }
    }

    fun nudge(delta: Long) {
        val from = target ?: abs()
        target = seekTarget(from, delta, dur)
        seekDelta = (seekDelta ?: 0L) + delta
        seekJob?.cancel()
        seekJob = scope.launch {
            delay(800)
            target?.let(onSeek)
            delay(300)
            target = null; seekDelta = null
        }
    }
    fun toggle() { if (player.isPlaying) player.pause() else player.play() }

    BackHandler {
        when {
            panel -> setPanel(false)
            controls -> controls = false
            else -> onLeave()
        }
    }

    Box(Modifier.fillMaxSize().background(Color.Black)
        .onPreviewKeyEvent { e ->
            if (e.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
            touched++
            when (remoteAction(e.key)) {
                RemoteAction.Toggle -> { toggle(); return@onPreviewKeyEvent true }
                RemoteAction.Play -> { player.play(); return@onPreviewKeyEvent true }
                RemoteAction.Pause -> { player.pause(); return@onPreviewKeyEvent true }
                RemoteAction.Forward -> { nudge(fwd); return@onPreviewKeyEvent true }
                RemoteAction.Rewind -> { nudge(-back); return@onPreviewKeyEvent true }
                RemoteAction.Next -> { if (next != null) onNext(); return@onPreviewKeyEvent true }
                RemoteAction.Stop -> { onLeave(); return@onPreviewKeyEvent true }
                null -> {}
            }
            if (panel || controls) return@onPreviewKeyEvent false
            when (e.key) {
                Key.DirectionLeft -> { nudge(-back); true }
                Key.DirectionRight -> { nudge(fwd); true }
                Key.DirectionUp, Key.DirectionDown -> { controls = true; true }
                Key.DirectionCenter, Key.Enter, Key.NumPadEnter -> if (skipIntro || nextButton) false else { controls = true; true }
                else -> false
            }
        }
        .focusRequester(root).focusable()) {
        AndroidView({ PlayerView(it).apply { this.player = player; useController = false; isFocusable = false; descendantFocusability = android.view.ViewGroup.FOCUS_BLOCK_DESCENDANTS } }, Modifier.fillMaxSize(),
            update = { v -> v.applyCaptionLook(prefs) })
        SeekNote(seekDelta)

        // Seeking with the controls hidden still shows where the film will land.
        AnimatedVisibility(!controls && target != null, Modifier.align(Alignment.BottomCenter), enter = fadeIn(), exit = fadeOut()) {
            Box(Modifier.fillMaxWidth().background(Brush.verticalGradient(listOf(Color.Transparent, Color.Black.copy(alpha = 0.8f)))).padding(horizontal = 56.dp, vertical = 32.dp)) {
                TvTimeline(position, dur, target)
            }
        }

        AnimatedVisibility(controls, Modifier.fillMaxSize(), enter = fadeIn(), exit = fadeOut()) {
            Box(Modifier.fillMaxSize()) {
                Box(Modifier.fillMaxWidth().height(160.dp).background(Brush.verticalGradient(listOf(Color.Black.copy(alpha = 0.75f), Color.Transparent))))
                Column(Modifier.align(Alignment.TopStart).padding(horizontal = 56.dp, vertical = 32.dp)) {
                    detail?.item?.seriesTitle?.let { Text(it.uppercase(), style = Head, color = Kino.muted) }
                    Text(detail?.item?.let { if (it.kind == "episode") stringResource(R.string.episode_title, it.episodeNumber ?: 0, it.title) else it.title }.orEmpty(), style = Display.copy(fontSize = 34.sp), color = Kino.text, maxLines = 1)
                }
                Column(Modifier.align(Alignment.BottomCenter).fillMaxWidth().background(Brush.verticalGradient(listOf(Color.Transparent, Color.Black.copy(alpha = 0.85f))))
                    .padding(horizontal = 56.dp).padding(top = 60.dp, bottom = 32.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
                    val (src, barFocused) = rememberFocusSource()
                    TvTimeline(position, dur, target, Modifier.onKeyEvent { e ->
                        if (e.type != KeyEventType.KeyDown) return@onKeyEvent false
                        when (e.key) { Key.DirectionLeft -> { nudge(-back); true }; Key.DirectionRight -> { nudge(fwd); true }; else -> false }
                    }.focusable(interactionSource = src).padding(vertical = 4.dp), focused = barFocused)
                    Row(horizontalArrangement = Arrangement.spacedBy(16.dp), verticalAlignment = Alignment.CenterVertically) {
                        TvPlayerButton(Icons.Default.Replay10, stringResource(R.string.rewind)) { nudge(-back) }
                        TvPlayerButton(if (playing) Icons.Default.Pause else Icons.Default.PlayArrow, stringResource(if (playing) R.string.pause else R.string.play),
                            Modifier.initialFocus()) { toggle() }
                        TvPlayerButton(Icons.Default.Forward10, stringResource(R.string.fast_forward)) { nudge(fwd) }
                        Spacer(Modifier.weight(1f))
                        if (info != null && (info.subtitles.isNotEmpty() || info.bitmapSubtitles.isNotEmpty() || info.audio.size > 1))
                            TvPlayerButton(Icons.Default.ClosedCaption, stringResource(R.string.audio_and_subtitles), wide = true) { controls = false; setPanel(true) }
                        if (next != null) TvPlayerButton(Icons.Default.SkipNext, stringResource(R.string.next_episode), wide = true) { onNext() }
                    }
                }
            }
        }

        if (!controls && !panel) {
            if (skipIntro) TvPlayerButton(Icons.Default.SkipNext, stringResource(if (intro!!.kind == "recap") R.string.skip_recap else R.string.skip_intro),
                Modifier.align(Alignment.BottomEnd).padding(48.dp, 56.dp).initialFocus(), wide = true) { onSkipIntro(intro) }
            else if (nextButton) TvPlayerButton(Icons.Default.SkipNext, stringResource(R.string.next_episode) + ": " + next!!.title,
                Modifier.align(Alignment.BottomEnd).padding(48.dp, 56.dp).initialFocus(), wide = true) { onNext() }
        }
        if (panel && info != null) TvTracksPanel(info, audio, subtitle, onAudio, onSubtitle, Modifier.align(Alignment.CenterEnd))
        notice?.let { Text(it, color = Color.White, fontSize = 18.sp, modifier = Modifier.align(Alignment.TopCenter).padding(top = 64.dp).background(Color.Black.copy(alpha = 0.85f), CardShape).padding(16.dp)) }
        error?.let { PlayerErrorBox(it, onRetry, onLeave) }
        if (info == null && error == null) CircularProgressIndicator(Modifier.align(Alignment.Center), color = Kino.text, strokeWidth = 2.dp)
    }
}
