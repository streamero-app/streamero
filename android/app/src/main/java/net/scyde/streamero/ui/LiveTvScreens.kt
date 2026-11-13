package net.scyde.streamero.ui

import android.app.Activity
import android.net.Uri
import android.view.WindowManager
import androidx.annotation.OptIn
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.FiberManualRecord
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.filled.StarBorder
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.Repeat
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.media3.common.MediaItem
import androidx.media3.common.MimeTypes
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import net.scyde.streamero.player.ErrorAction
import net.scyde.streamero.player.ErrorRecovery
import net.scyde.streamero.player.SAFE_PROFILE
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.PlayerView
import androidx.navigation.NavHostController
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.LiveTvChannel
import net.scyde.streamero.api.LiveTvPlayback
import net.scyde.streamero.api.LiveTvProgram
import net.scyde.streamero.livetv.*
import java.time.Instant

/** Ticks once a minute so now/next and progress bars move on. */
@Composable
private fun rememberNow(): Instant {
    var now by remember { mutableStateOf(Instant.now()) }
    LaunchedEffect(Unit) { while (true) { delay(30_000); now = Instant.now() } }
    return now
}

/** Channel list: logo, number and name, what is on now (with progress) and next; favourites and a favourites filter. */
@Composable
fun LiveTvScreen(state: AppState, nav: NavHostController) {
    val api = state.api ?: return
    if (!canWatchLiveTv(state.me)) { Text(stringResource(R.string.err_forbidden), color = Kino.danger, modifier = Modifier.statusBarsPadding().padding(20.dp)); return }
    val scope = rememberCoroutineScope()
    var onlyFavorites by remember { mutableStateOf(false) }
    var reload by remember { mutableIntStateOf(0) }
    var channels by remember { mutableStateOf<List<LiveTvChannel>?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    val now = rememberNow()
    LaunchedEffect(reload, now) {
        runCatching { api.liveChannels() }.onSuccess { channels = it; error = null }.onFailure { error = it }
    }
    val shown = channels?.filter { !onlyFavorites || it.favorite }
    LazyColumn(Modifier.fillMaxSize().statusBarsPadding(), contentPadding = PaddingValues(Dims.edge, 8.dp, Dims.edge, 30.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        item {
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (!LocalTv.current) IconButton(onClick = { nav.popBackStack() }) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Kino.text) }
                Text(stringResource(R.string.livetv), style = Display.copy(fontSize = 30.sp), color = Kino.text)
            }
        }
        item {
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                GlassButton(stringResource(R.string.livetv_all_channels), { onlyFavorites = false }, Modifier.initialFocus(), active = !onlyFavorites)
                GlassButton(stringResource(R.string.livetv_favorites), { onlyFavorites = true }, active = onlyFavorites, icon = Icons.Default.Star)
            }
        }
        when {
            shown == null && error == null -> item { Loading() }
            shown == null -> item { ErrorLine(error) }
            shown.isEmpty() -> item {
                Text(stringResource(if (onlyFavorites) R.string.livetv_no_favorites else R.string.livetv_no_channels), color = Kino.faint, fontSize = 15.sp)
            }
            else -> items(shown, key = { it.id }) { c ->
                ChannelRow(state, c, now,
                    onPlay = { nav.navigate("watchlive/${c.id}") },
                    onSchedule = { nav.navigate("livetv/channel/${c.id}") },
                    onFavorite = {
                        val fav = !c.favorite
                        channels = channels?.let { withFavorite(it, c.id, fav) }
                        scope.launch { runCatching { api.setLiveFavorite(c.id, fav) }.onFailure { e -> error = e; reload++ } }
                    })
            }
        }
        if (shown != null && error != null) item { ErrorLine(error) }
    }
}

@Composable
private fun ChannelLogo(state: AppState, c: LiveTvChannel, size: androidx.compose.ui.unit.Dp) {
    Box(Modifier.size(size).clip(CardShape).background(Kino.surface3), contentAlignment = Alignment.Center) {
        if (c.logoUrl != null) ServerImage(state.api?.resolve(c.logoUrl), Modifier.fillMaxSize().padding(4.dp), ContentScale.Fit)
        else Text(c.number, color = Kino.muted, fontWeight = FontWeight.Bold, fontSize = 14.sp)
    }
}

@Composable
private fun ChannelRow(state: AppState, c: LiveTvChannel, now: Instant, onPlay: () -> Unit, onSchedule: () -> Unit, onFavorite: () -> Unit) {
    val (current, next) = channelNowNext(c, emptyList(), now)
    Row(Modifier.fillMaxWidth().clip(CardShape).background(Kino.surface), verticalAlignment = Alignment.CenterVertically) {
        Row(Modifier.weight(1f).focusClickable(scale = 1.02f, onClick = onPlay).padding(12.dp),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            ChannelLogo(state, c, 56.dp)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                Text("${c.number}  ${c.name}", color = Kino.text, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (current != null) {
                    Text(current.title, color = Kino.muted, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    ProgressLine(programProgress(current, now).coerceAtLeast(0.01f), Modifier.padding(vertical = 2.dp))
                } else Text(stringResource(R.string.livetv_no_guide), color = Kino.faint, fontSize = 14.sp)
                if (next != null) Text(stringResource(R.string.livetv_next, startTime(next), next.title), color = Kino.faint, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        IconButton(onClick = onSchedule) { Icon(Icons.Outlined.CalendarMonth, stringResource(R.string.livetv_schedule), tint = Kino.muted) }
        IconButton(onClick = onFavorite) {
            Icon(if (c.favorite) Icons.Default.Star else Icons.Default.StarBorder,
                stringResource(if (c.favorite) R.string.livetv_unfavorite else R.string.livetv_favorite), tint = if (c.favorite) LocalAccent.current else Kino.muted)
        }
    }
}

/** A Home row of channels with what is on now; nothing when Live TV is not set up. */
@Composable
fun OnNowShelf(state: AppState, nav: NavHostController, channels: List<LiveTvChannel>) {
    if (channels.isEmpty()) return
    val now = rememberNow()
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        SectionTitle(stringResource(R.string.livetv_on_now))
        LazyRow(contentPadding = PaddingValues(horizontal = Dims.edge), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            items(channels, key = { it.id }) { c ->
                Column(Modifier.width(Dims.wide).clip(CardShape).background(Kino.surface).focusClickable { nav.navigate("watchlive/${c.id}") }.padding(12.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        ChannelLogo(state, c, 40.dp)
                        Text(c.name, color = Kino.muted, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                    Text(c.now?.title ?: "", color = Kino.text, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    ProgressLine(programProgress(c.now, now).coerceAtLeast(0.01f))
                }
            }
        }
    }
}

/** What a channel shows in the next hours, with recording for those who manage Live TV. */
@Composable
fun LiveChannelScreen(state: AppState, nav: NavHostController, channelId: String) {
    val api = state.api ?: return
    val scope = rememberCoroutineScope()
    val manage = canManageLiveTv(state.me)
    var reload by remember { mutableIntStateOf(0) }
    var busy by remember { mutableStateOf<String?>(null) }
    var actionError by remember { mutableStateOf<Throwable?>(null) }
    var notice by remember { mutableStateOf<String?>(null) }
    val ctx = LocalContext.current
    val now = rememberNow()
    val (guide, loading, error) = rememberLoad(reload) { api.liveGuide(12) }
    val row = guide?.rows?.firstOrNull { it.channel.id == channelId }
    val programs = channelSchedule(guide, channelId, now)

    fun act(p: LiveTvProgram, series: Boolean) {
        busy = p.id; actionError = null; notice = null
        scope.launch {
            runCatching { if (series) api.recordSeries(p.id, channelId) else api.recordProgram(p.id, channelId) }
                .onSuccess { notice = ctx.getString(if (series) R.string.livetv_series_scheduled else R.string.livetv_recording_scheduled); reload++ }
                .onFailure { actionError = it }
            busy = null
        }
    }

    LazyColumn(Modifier.fillMaxSize().statusBarsPadding(), contentPadding = PaddingValues(Dims.edge, 8.dp, Dims.edge, 30.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        item {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                if (!LocalTv.current) IconButton(onClick = { nav.popBackStack() }) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Kino.text) }
                row?.channel?.let { ChannelLogo(state, it, 44.dp) }
                Text(row?.channel?.let { "${it.number}  ${it.name}" } ?: stringResource(R.string.livetv), style = Display.copy(fontSize = 26.sp), color = Kino.text,
                    maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        item { PrimaryButton(stringResource(R.string.livetv_watch), { nav.navigate("watchlive/$channelId") }, Modifier.fillMaxWidth().initialFocus(), icon = Icons.Default.PlayArrow) }
        notice?.let { item { Text(it, color = Kino.muted, fontSize = 14.sp) } }
        if (actionError != null) item { ErrorLine(actionError) }
        when {
            loading && guide == null -> item { Loading() }
            guide == null -> item { ErrorLine(error) }
            programs.isEmpty() -> item { Text(stringResource(R.string.livetv_no_guide), color = Kino.faint, fontSize = 15.sp) }
            else -> items(programs, key = { it.id + it.start }) { p ->
                ProgramRow(p, now, manage, busy == p.id, onRecord = { act(p, false) }, onSeries = { act(p, true) })
            }
        }
    }
}

@Composable
private fun recordingLabel(status: String?): String? = when (status) {
    "scheduled" -> stringResource(R.string.livetv_status_scheduled)
    "conflict" -> stringResource(R.string.livetv_status_conflict)
    "recording" -> stringResource(R.string.livetv_status_recording)
    "completed" -> stringResource(R.string.livetv_status_completed)
    "failed" -> stringResource(R.string.livetv_status_failed)
    else -> null
}

@Composable
private fun ProgramRow(p: LiveTvProgram, now: Instant, manage: Boolean, busy: Boolean, onRecord: () -> Unit, onSeries: () -> Unit) {
    val airing = programProgress(p, now) > 0f
    Column(Modifier.fillMaxWidth().clip(CardShape).background(if (airing) Kino.surface3 else Kino.surface).padding(14.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(timeRange(p), color = Kino.muted, fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
            if (p.isNew) Tag(stringResource(R.string.livetv_new))
            recordingLabel(p.recordingStatus)?.let { label ->
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Default.FiberManualRecord, null, tint = if (p.recordingStatus == "conflict") Kino.muted else Kino.danger, modifier = Modifier.size(12.dp))
                    Spacer(Modifier.width(4.dp))
                    Text(label, color = Kino.text, fontSize = 13.sp)
                }
            }
        }
        Text(p.title, color = Kino.text, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, maxLines = 2, overflow = TextOverflow.Ellipsis)
        val episode = listOfNotNull(
            if (p.season != null && p.episode != null) stringResource(R.string.episode_code, p.season, p.episode) else null, p.subtitle).joinToString(" · ")
        if (episode.isNotEmpty()) Text(episode, color = Kino.muted, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        p.description?.let { Text(it, color = Kino.faint, fontSize = 13.sp, maxLines = 3, overflow = TextOverflow.Ellipsis) }
        if (airing) ProgressLine(programProgress(p, now))
        if (manage && canRecord(p, now)) Row(Modifier.padding(top = 6.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            GlassButton(stringResource(R.string.livetv_record), onRecord, icon = Icons.Default.FiberManualRecord, enabled = !busy)
            if (p.seriesRuleId == null) GlassButton(stringResource(R.string.livetv_record_series), onSeries, icon = Icons.Outlined.Repeat, enabled = !busy)
        }
    }
}

/**
 * Plays a channel. The server's HLS playlist is a sliding window, so the player's own
 * controls pause and rewind within it (time-shift); nothing is saved as resume progress.
 */
@OptIn(UnstableApi::class)
@Composable
fun LivePlayerScreen(state: AppState, nav: NavHostController, channelId: String) {
    val api = state.api ?: return
    val ctx = LocalContext.current
    val player = remember { videoPlayer(ctx) }
    var playback by remember { mutableStateOf<LiveTvPlayback?>(null) }
    var channel by remember { mutableStateOf<LiveTvChannel?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var notice by remember { mutableStateOf<String?>(null) }
    var attempt by remember { mutableIntStateOf(0) }
    val recovery = remember { ErrorRecovery() }

    DisposableEffect(Unit) {
        val act = ctx.findActivity()
        state.music.pause()
        act?.window?.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        val ctl = act?.let { WindowCompat.getInsetsController(it.window, it.window.decorView) }
        ctl?.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        ctl?.hide(WindowInsetsCompat.Type.systemBars())
        // A stream the device cannot play: once more as plain H.264/AAC, then say so.
        val l = object : Player.Listener {
            override fun onPlayerError(e: PlaybackException) {
                when (recovery.onError()) {
                    ErrorAction.Fallback -> { notice = ctx.getString(R.string.player_fallback); attempt++ }
                    ErrorAction.Fail -> error = e
                }
            }
        }
        player.addListener(l)
        onDispose {
            ctl?.show(WindowInsetsCompat.Type.systemBars())
            act?.window?.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
            player.removeListener(l)
            player.release()
        }
    }
    val inPip = videoLifecycle(player, state.enhancements.autoPip)
    LaunchedEffect(channelId, attempt) {
        if (channel == null) channel = runCatching { api.liveChannels() }.getOrNull()?.firstOrNull { it.id == channelId }
        val prefs = runCatching { api.preferences() }.getOrNull()
        try {
            val device = if (recovery.safe) SAFE_PROFILE.copy(maxHeight = prefs?.maxHeight, maxBitrate = prefs?.maxBitrateKbps?.let { it * 1000 }) else androidDeviceProfile(prefs)
            val pb = api.playChannel(channelId, device)
            playback = pb
            error = null
            player.setMediaItem(MediaItem.Builder().setUri(Uri.parse(api.resolve(pb.url))).setMimeType(MimeTypes.APPLICATION_M3U8).build())
            player.playWhenReady = true
            player.prepare()
        } catch (e: kotlinx.coroutines.CancellationException) {
            throw e
        } catch (e: Exception) {
            error = e
        }
    }
    LaunchedEffect(notice) { if (notice != null) { delay(8_000); notice = null } }

    Box(Modifier.fillMaxSize().background(Color.Black)) {
        AndroidView({ PlayerView(it).apply { this.player = player; setShowNextButton(false); setShowPreviousButton(false) } }, Modifier.fillMaxSize(),
            update = { v -> v.useController = !inPip; if (inPip) v.hideController() })
        if (inPip) return@Box
        Row(Modifier.align(Alignment.TopStart).padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
            if (!LocalTv.current) IconButton(onClick = { nav.popBackStack() }) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Color.White) }
            Column {
                Text(channel?.let { "${it.number}  ${it.name}" }.orEmpty(), color = Color.White, fontWeight = FontWeight.SemiBold)
                (playback?.now ?: channel?.now)?.let { Text(it.title, color = Color.White.copy(alpha = 0.75f), fontSize = 13.sp) }
            }
        }
        if (playback == null && error == null) CircularProgressIndicator(Modifier.align(Alignment.Center), color = Color.White)
        notice?.let { Text(it, color = Color.White, fontSize = 15.sp, modifier = Modifier.align(Alignment.TopCenter).padding(top = 64.dp).background(Color.Black.copy(alpha = 0.85f), CardShape).padding(14.dp)) }
        error?.let { PlayerErrorBox(it, { error = null; playback = null; attempt++ }, { nav.popBackStack() }) }
    }
}
