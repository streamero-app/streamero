package net.scyde.streamero.ui

import android.Manifest
import android.app.Activity
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.view.WindowManager
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.annotation.OptIn
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.DownloadDone
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Download
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.WifiOff
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MimeTypes
import androidx.media3.common.Player
import androidx.media3.common.PlaybackException
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.PlayerView
import coil.compose.AsyncImage
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.DownloadRequest
import net.scyde.streamero.api.ItemDetail
import net.scyde.streamero.offline.EntryState
import net.scyde.streamero.offline.OfflineEntry
import net.scyde.streamero.offline.OfflineGroup
import net.scyde.streamero.offline.OfflineSync
import net.scyde.streamero.offline.Quality
import net.scyde.streamero.offline.Storage
import java.io.File

/** Whether the signed-in user may download (admins can do everything). */
fun canDownload(state: AppState): Boolean = state.me?.let { it.isAdmin || it.permissions.download } == true
private fun canTranscode(state: AppState): Boolean = state.me?.let { it.isAdmin || it.permissions.videoTranscoding } == true

@Composable
private fun qualityLabel(q: String): String = stringResource(when (q) {
    Quality.SOURCE -> R.string.quality_source
    Quality.P1080 -> R.string.quality_1080
    Quality.P720 -> R.string.quality_720
    else -> R.string.quality_original
})

/** Asks for the notification permission (Android 13+) so download progress shows. */
@Composable
private fun rememberNotificationAsk(): () -> Unit {
    val ctx = LocalContext.current
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {}
    return {
        if (Build.VERSION.SDK_INT >= 33 && ContextCompat.checkSelfPermission(ctx, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            runCatching { launcher.launch(Manifest.permission.POST_NOTIFICATIONS) }
        }
    }
}

/** A line saying where a download is (waiting, preparing 40 %, downloading 70 %, downloaded). */
@Composable
fun downloadStatus(e: OfflineEntry): String {
    val pct = (e.phaseProgress * 100).toInt()
    return when (e.state) {
        EntryState.TRANSCODING -> stringResource(R.string.download_transcoding, pct)
        EntryState.DOWNLOADING -> stringResource(R.string.download_downloading, pct)
        EntryState.DONE -> stringResource(R.string.download_done, Storage.format(Storage.entryBytes(e)))
        EntryState.FAILED -> stringResource(R.string.download_failed)
        else -> stringResource(R.string.download_queued)
    }
}

/**
 * The download controls on an item page: a film or an episode gets Download (with a quality
 * choice) and its state; a series gets "download this season" and automatic next episodes;
 * a season gets "download season".
 */
@Composable
fun ItemDownloads(state: AppState, nav: androidx.navigation.NavHostController, d: ItemDetail, seasonId: String?, seasonNumber: Int?, fileId: String?) {
    if (!canDownload(state)) return
    val api = state.api ?: return
    val offline = state.offline
    val data by offline.store.data.collectAsState()
    val scope = rememberCoroutineScope()
    val ask = rememberNotificationAsk()
    var error by remember { mutableStateOf<Throwable?>(null) }
    var pickFor by remember { mutableStateOf<String?>(null) }
    var auto by remember { mutableStateOf(false) }
    val kind = d.item.kind
    val qualities = if (canTranscode(state)) Quality.all else listOf(Quality.ORIGINAL)

    fun start(itemId: String, q: String) {
        ask()
        error = null
        scope.launch { runCatching { offline.request(api, DownloadRequest(itemId, q, fileId = fileId.takeIf { itemId == d.item.id })) }.onFailure { error = it } }
    }
    fun choose(itemId: String) { if (qualities.size == 1) start(itemId, qualities[0]) else pickFor = itemId }

    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        when {
            (kind == "movie" || kind == "episode") && d.files.isNotEmpty() -> {
                val e = data.entry(d.item.id)
                if (e == null) GlassButton(stringResource(R.string.download), { choose(d.item.id) }, Modifier.fillMaxWidth(), icon = Icons.Outlined.Download)
                else DownloadStateCard(e, onPlay = { nav.navigate("offline/${e.itemId}") }, onRetry = { scope.launch { offline.retry(e.itemId) } },
                    onDelete = { scope.launch { offline.delete(listOf(e.itemId)) } })
            }
            kind == "season" -> GlassButton(stringResource(R.string.download_this_season), { choose(d.item.id) }, Modifier.fillMaxWidth(), icon = Icons.Outlined.Download)
            kind == "series" -> {
                if (seasonId != null) GlassButton(if (seasonNumber != null && seasonNumber > 0) stringResource(R.string.download_season, seasonNumber) else stringResource(R.string.download_this_season),
                    { choose(seasonId) }, Modifier.fillMaxWidth(), icon = Icons.Outlined.Download)
                val sub = data.subscription(d.item.id)
                if (sub == null) GlassButton(stringResource(R.string.download_auto), { auto = true }, Modifier.fillMaxWidth(), icon = Icons.Outlined.Refresh)
                else Row(Modifier.fillMaxWidth().clip(CardShape).background(Kino.surface).padding(horizontal = 14.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(pluralStringResource(R.plurals.download_auto_active, sub.count, sub.count), color = Kino.text, fontSize = 14.sp, modifier = Modifier.weight(1f))
                    TextButton(onClick = { scope.launch { offline.unsubscribe(sub.seriesId) } }) { Text(stringResource(R.string.download_auto_stop), color = Kino.muted) }
                }
            }
        }
        ErrorLine(error)
    }

    pickFor?.let { target ->
        ChoiceDialog(qualities.map { it to qualityLabel(it) }, Quality.ORIGINAL, stringResource(R.string.download_quality), onDismiss = { pickFor = null }) { q ->
            pickFor = null; start(target, q)
        }
    }
    if (auto) AutoDownloadDialog(qualities, onDismiss = { auto = false }) { n, q ->
        auto = false
        ask()
        error = null
        scope.launch { runCatching { offline.subscribe(api, d.item.id, d.item.title, n, q) }.onFailure { error = it } }
    }
}

@Composable
private fun AutoDownloadDialog(qualities: List<String>, onDismiss: () -> Unit, onConfirm: (Int, String) -> Unit) {
    var count by remember { mutableIntStateOf(3) }
    var quality by remember { mutableStateOf(qualities.firstOrNull { it == Quality.P720 } ?: qualities.first()) }
    AlertDialog(onDismissRequest = onDismiss, containerColor = Kino.surface,
        title = { Text(stringResource(R.string.download_auto)) },
        text = {
            Column {
                Text(stringResource(R.string.download_auto_hint), color = Kino.muted, fontSize = 14.sp)
                FieldRow(stringResource(R.string.download_auto_count)) {
                    Picker(count, listOf(1, 2, 3, 5, 10).map { it to pluralStringResource(R.plurals.download_next_episodes, it, it) }, { count = it })
                }
                if (qualities.size > 1) FieldRow(stringResource(R.string.download_quality)) {
                    Picker(quality, qualities.map { it to qualityLabel(it) }, { quality = it })
                }
            }
        },
        confirmButton = { TextButton(onClick = { onConfirm(count, quality) }) { Text(stringResource(R.string.download), color = Kino.text) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel), color = Kino.muted) } })
}

@Composable
private fun DownloadStateCard(e: OfflineEntry, onPlay: () -> Unit, onRetry: () -> Unit, onDelete: () -> Unit) {
    Column(Modifier.fillMaxWidth().clip(CardShape).background(Kino.surface).padding(horizontal = 14.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(if (e.done) Icons.Default.DownloadDone else Icons.Outlined.Download, null, tint = if (e.state == EntryState.FAILED) Kino.danger else Kino.muted, modifier = Modifier.size(20.dp))
            Spacer(Modifier.width(10.dp))
            Text(downloadStatus(e), color = Kino.text, fontSize = 14.sp, modifier = Modifier.weight(1f))
            IconButton(onClick = onDelete) { Icon(if (e.done) Icons.Outlined.Delete else Icons.Default.Close, stringResource(R.string.download_delete), tint = Kino.muted) }
        }
        if (!e.done && e.state != EntryState.FAILED) LinearProgressIndicator(progress = { e.phaseProgress }, Modifier.fillMaxWidth(), color = Kino.text, trackColor = Kino.surface3)
        if (e.done) GlassButton(stringResource(R.string.play_offline), onPlay, Modifier.fillMaxWidth(), icon = Icons.Default.PlayArrow)
        if (e.state == EntryState.FAILED) GlassButton(stringResource(R.string.download_retry), onRetry, Modifier.fillMaxWidth(), icon = Icons.Outlined.Refresh)
    }
}

/** A small mark on episode rows that are on the device. */
@Composable
fun DownloadedMark(state: AppState, itemId: String) {
    val data by state.offline.store.data.collectAsState()
    if (data.entry(itemId)?.done == true) Icon(Icons.Default.DownloadDone, stringResource(R.string.downloaded), tint = Kino.muted, modifier = Modifier.size(18.dp))
}

/** A picture kept with a download. */
@Composable
private fun LocalImage(file: File?, modifier: Modifier) {
    if (file == null || !file.exists()) return
    AsyncImage(model = file, contentDescription = null, contentScale = ContentScale.Crop, modifier = modifier)
}

/**
 * The offline library: everything on the device by film and series, the space it takes,
 * Wi-Fi only, automatic series and removal. Works without the server; `banner` is shown on
 * top when the app could not reach it.
 */
@Composable
fun DownloadsScreen(state: AppState, onPlay: (String) -> Unit, banner: (@Composable () -> Unit)? = null) {
    val offline = state.offline
    val store = offline.store
    val data by store.data.collectAsState()
    val scope = rememberCoroutineScope()
    var confirmAll by remember { mutableStateOf(false) }
    val groups = remember(data.entries) { Storage.groups(data.entries) }
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 30.dp)) {
        item {
            Column(Modifier.statusBarsPadding().padding(horizontal = 20.dp).padding(top = 12.dp, bottom = 8.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(stringResource(R.string.downloads), style = Display.copy(fontSize = 34.sp), color = Kino.text)
                banner?.invoke()
                Text(stringResource(R.string.downloads_total, Storage.format(Storage.totalBytes(data.entries))), color = Kino.muted, fontSize = 14.sp)
                Row(Modifier.fillMaxWidth().clickable { scope.launch { offline.setWifiOnly(!data.wifiOnly) } }.padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(stringResource(R.string.downloads_wifi_only), color = Kino.text, fontSize = 15.sp, modifier = Modifier.weight(1f))
                    Switch(data.wifiOnly, { on -> scope.launch { offline.setWifiOnly(on) } },
                        colors = SwitchDefaults.colors(checkedThumbColor = Kino.bg, checkedTrackColor = Kino.text))
                }
            }
        }
        if (data.subscriptions.isNotEmpty()) {
            item { Text(stringResource(R.string.downloads_auto).uppercase(), style = Label, color = Kino.faint, modifier = Modifier.padding(start = 20.dp, top = 12.dp, bottom = 4.dp)) }
            items(data.subscriptions, key = { "a" + it.seriesId }) { s ->
                Row(Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text(s.title, color = Kino.text, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(pluralStringResource(R.plurals.download_next_episodes, s.count, s.count) + " · " + qualityLabel(s.quality), color = Kino.faint, fontSize = 13.sp)
                    }
                    TextButton(onClick = { scope.launch { offline.unsubscribe(s.seriesId) } }) { Text(stringResource(R.string.remove), color = Kino.muted) }
                }
            }
        }
        if (groups.isEmpty()) item {
            Text(stringResource(R.string.downloads_empty), color = Kino.muted, fontSize = 15.sp, lineHeight = 22.sp, modifier = Modifier.padding(20.dp))
        }
        groups.forEach { g ->
            item(key = g.key) {
                if (g.series == null && g.entries.size == 1 && g.entries[0].item.kind != "episode") OfflineRow(store.file(g.entries[0].poster), g.entries[0], g.entries[0].item.title,
                    listOfNotNull(g.entries[0].item.year?.toString()), poster = true, onPlay = onPlay, onDelete = { scope.launch { offline.delete(listOf(it)) } })
                else SeriesGroup(state, g, onPlay)
            }
        }
        if (data.entries.isNotEmpty()) item {
            TextButton(onClick = { confirmAll = true }, Modifier.padding(horizontal = 12.dp, vertical = 12.dp)) { Text(stringResource(R.string.downloads_delete_all), color = Kino.danger) }
        }
    }
    if (confirmAll) AlertDialog(onDismissRequest = { confirmAll = false }, containerColor = Kino.surface,
        text = { Text(stringResource(R.string.downloads_delete_all_confirm), color = Kino.text) },
        confirmButton = { TextButton(onClick = { confirmAll = false; scope.launch { offline.deleteAll() } }) { Text(stringResource(R.string.remove), color = Kino.danger) } },
        dismissButton = { TextButton(onClick = { confirmAll = false }) { Text(stringResource(R.string.cancel), color = Kino.muted) } })
}

@Composable
private fun SeriesGroup(state: AppState, g: OfflineGroup, onPlay: (String) -> Unit) {
    val offline = state.offline
    val scope = rememberCoroutineScope()
    Column(Modifier.padding(top = 10.dp)) {
        Row(Modifier.padding(horizontal = 20.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
            Box(Modifier.width(48.dp).aspectRatio(2f / 3f).clip(CardShape).background(Kino.surface2)) {
                LocalImage(g.entries.firstNotNullOfOrNull { offline.store.file(it.seriesPoster)?.takeIf { f -> f.exists() } }, Modifier.fillMaxSize())
            }
            Column(Modifier.weight(1f)) {
                Text(g.title, color = Kino.text, fontWeight = FontWeight.Bold, fontSize = 17.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(pluralStringResource(R.plurals.episodes_count, g.entries.size, g.entries.size) + " · " + Storage.format(g.bytes), color = Kino.faint, fontSize = 13.sp)
            }
        }
        g.entries.forEach { e ->
            val code = listOfNotNull(e.item.seasonNumber?.let { "S$it" }, e.item.episodeNumber?.let { "E$it" }).joinToString(":")
            OfflineRow(offline.store.file(e.poster), e, listOf(code, e.item.title).filter { it.isNotBlank() }.joinToString(" · "), emptyList(), poster = false,
                onPlay = onPlay, onDelete = { scope.launch { offline.delete(listOf(it)) } })
        }
    }
}

@Composable
private fun OfflineRow(image: File?, e: OfflineEntry, title: String, meta: List<String>, poster: Boolean, onPlay: (String) -> Unit, onDelete: (String) -> Unit) {
    Row(Modifier.fillMaxWidth().clickable(enabled = e.done) { onPlay(e.itemId) }.padding(horizontal = 20.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
        Box(Modifier.width(if (poster) 56.dp else 112.dp).aspectRatio(if (poster) 2f / 3f else 16f / 9f).clip(CardShape).background(Kino.surface2)) {
            LocalImage(image, Modifier.fillMaxSize())
            if (e.done) Icon(Icons.Default.PlayArrow, null, tint = Color.White, modifier = Modifier.align(Alignment.Center).background(Color.Black.copy(alpha = 0.5f), CardShape).padding(4.dp).size(18.dp))
            val dur = e.durationMs ?: e.item.durationMs
            if (dur != null && dur > 0 && e.playback.positionMs > 0) ProgressLine((e.playback.positionMs.toFloat() / dur).coerceIn(0f, 1f), Modifier.align(Alignment.BottomCenter))
        }
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            Text(title, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 15.sp, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Text((meta + qualityLabel(e.quality) + downloadStatus(e)).joinToString(" · "), color = if (e.state == EntryState.FAILED) Kino.danger else Kino.faint, fontSize = 13.sp, maxLines = 2)
            if (!e.done && e.state != EntryState.FAILED) LinearProgressIndicator(progress = { e.phaseProgress }, Modifier.fillMaxWidth().padding(top = 2.dp), color = Kino.text, trackColor = Kino.surface3)
        }
        IconButton(onClick = { onDelete(e.itemId) }) { Icon(Icons.Outlined.Delete, stringResource(R.string.download_delete), tint = Kino.muted) }
    }
}

/** Shown above the offline library when the server could not be reached at start. */
@Composable
fun OfflineBanner(retrying: Boolean, onRetry: () -> Unit) {
    Row(Modifier.fillMaxWidth().clip(CardShape).background(Kino.surface).padding(14.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Icon(Icons.Outlined.WifiOff, null, tint = Kino.muted)
        Column(Modifier.weight(1f)) {
            Text(stringResource(R.string.offline_title), color = Kino.text, fontWeight = FontWeight.SemiBold)
            Text(stringResource(R.string.offline_text), color = Kino.muted, fontSize = 13.sp)
        }
        if (retrying) CircularProgressIndicator(Modifier.size(20.dp), color = Kino.text, strokeWidth = 2.dp)
        else TextButton(onClick = onRetry) { Text(stringResource(R.string.offline_retry), color = Kino.text) }
    }
}

/**
 * Plays a downloaded file: local video, local .srt sidecars, resume from the local position.
 * Progress is recorded on the device (and sent to the server when it can be reached).
 */
@OptIn(UnstableApi::class)
@Composable
fun OfflinePlayerScreen(state: AppState, itemId: String, onClose: () -> Unit) {
    val ctx = LocalContext.current
    val offline = state.offline
    val entry = remember(itemId) { offline.store.entry(itemId) }
    val video = entry?.let { offline.store.file(it.file) }?.takeIf { it.exists() }
    if (entry == null || video == null) {
        Column(Modifier.fillMaxSize().statusBarsPadding().padding(20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(stringResource(R.string.download_missing), color = Kino.danger)
            GlassButton(stringResource(R.string.back), onClose)
        }
        return
    }
    val player = remember { videoPlayer(ctx) }
    val scope = rememberCoroutineScope()
    var error by remember { mutableStateOf<Throwable?>(null) }
    fun duration(): Long? = player.duration.takeIf { it != C.TIME_UNSET && it > 0 } ?: entry.durationMs

    DisposableEffect(itemId) {
        val act = ctx.findActivity()
        state.music.pause()
        act?.window?.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        val ctl = act?.let { WindowCompat.getInsetsController(it.window, it.window.decorView) }
        ctl?.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        ctl?.hide(WindowInsetsCompat.Type.systemBars())
        // A local file that does not play will not play better on a retry: say so instead of a black screen.
        val l = object : Player.Listener { override fun onPlayerError(e: PlaybackException) { error = e } }
        player.addListener(l)
        val subs = entry.subtitles.mapNotNull { s ->
            offline.store.file(s.file)?.takeIf { it.exists() }?.let { f ->
                MediaItem.SubtitleConfiguration.Builder(Uri.fromFile(f)).setMimeType(MimeTypes.APPLICATION_SUBRIP)
                    .setLanguage(s.language.ifBlank { null }).setLabel(s.language.uppercase().ifBlank { null })
                    .setSelectionFlags(if (s.forced) C.SELECTION_FLAG_FORCED else 0).build()
            }
        }
        player.setMediaItem(MediaItem.Builder().setUri(Uri.fromFile(video)).setSubtitleConfigurations(subs).build(), OfflineSync.startPosition(entry))
        player.playWhenReady = true
        player.prepare()
        onDispose {
            offline.finishPlayback(itemId, player.currentPosition, duration())
            ctl?.show(WindowInsetsCompat.Type.systemBars())
            act?.window?.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
            player.removeListener(l)
            player.release()
        }
    }
    // Saved on the device every few seconds while playing.
    LaunchedEffect(itemId) {
        var tick = 0
        while (true) {
            delay(5000)
            if (player.isPlaying) {
                tick++
                offline.recordPlayback(itemId, player.currentPosition, duration())
                if (tick % 3 == 0) scope.launch { offline.reportOnline(itemId, player.currentPosition, duration()) }
            }
        }
    }
    val inPip = videoLifecycle(player, state.enhancements.autoPip)
    BackHandler { onClose() }
    Box(Modifier.fillMaxSize().background(Color.Black)) {
        AndroidView({ PlayerView(it).apply { this.player = player; setShowSubtitleButton(true); setShowNextButton(false); setShowPreviousButton(false); applyCaptionLook(null) } }, Modifier.fillMaxSize(),
            update = { v -> v.useController = !inPip; if (inPip) v.hideController() })
        if (inPip) return@Box
        error?.let { PlayerErrorBox(it, null, onClose) }
        Row(Modifier.align(Alignment.TopStart).padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClick = onClose) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Color.White) }
            Text(entry.item.seriesTitle?.let { "$it · " }.orEmpty() + entry.item.title, color = Color.White, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}
