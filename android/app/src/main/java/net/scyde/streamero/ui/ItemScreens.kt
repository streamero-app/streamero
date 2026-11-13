package net.scyde.streamero.ui

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Groups
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Replay
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.*

fun clock(ms: Long): String {
    val s = ms / 1000
    return if (s >= 3600) "%d:%02d:%02d".format(s / 3600, (s % 3600) / 60, s % 60) else "%d:%02d".format(s / 60, s % 60)
}

@Composable
fun ItemScreen(state: AppState, nav: NavHostController, id: String) {
    val api = state.api ?: return
    var reload by remember { mutableIntStateOf(0) }
    val (d, loading, error) = rememberLoad(id to reload) { api.item(id) }
    if (loading && d == null) { Loading(Modifier.fillMaxSize()); return }
    if (d == null) { Box(Modifier.padding(20.dp).statusBarsPadding()) { ErrorLine(error) }; return }
    when (d.item.kind) {
        "album" -> { AlbumScreen(state, nav, d); return }
        "artist" -> { ArtistScreen(state, nav, d); return }
        "track" -> { LaunchedEffect(Unit) { d.parentId?.let { nav.navigate("item/$it") { popUpTo("item/$id") { inclusive = true } } } }; return }
    }
    val it = d.item
    val accent = hexColor(it.colors?.firstOrNull())
    val scope = rememberCoroutineScope()
    val ctx = LocalContext.current
    var file by remember { mutableIntStateOf(0) }
    var audio by remember { mutableStateOf<Int?>(null) }
    var sub by remember { mutableStateOf<Int?>(null) }
    val seasons = d.children.filter { c -> c.kind == "season" }
    var season by remember(seasons.size) { mutableStateOf(seasons.firstOrNull { s -> s.seasonNumber == 1 }?.id ?: seasons.firstOrNull()?.id) }
    val episodes = d.children.filter { c -> c.kind == "episode" }
    val tv = LocalTv.current
    val mem = rememberFocusMemory()
    val f = d.files.getOrNull(file)?.id?.takeIf { d.files.size > 1 }
    var invite by remember { mutableStateOf<String?>(null) }
    var togetherError by remember { mutableStateOf<Throwable?>(null) }
    var actionError by remember { mutableStateOf<Throwable?>(null) }
    fun userData(u: UserDataUpdate) {
        actionError = null
        scope.launch { runCatching { api.setUserData(it.id, u) }.onSuccess { reload++ }.onFailure { e -> actionError = e } }
    }
    // Play, resume, my list, watched: under the hero on a phone, inside it on a TV.
    val actions: @Composable () -> Unit = {
        if (d.files.isNotEmpty()) {
            PrimaryButton(if (it.positionMs > 0) stringResource(R.string.resume_from, clock(it.positionMs)) else stringResource(R.string.play),
                { nav.watch(it.id, file = f, audio = audio, sub = sub) }, if (tv) Modifier.rememberFocus(mem, "play", default = true) else Modifier.fillMaxWidth(), icon = Icons.Default.PlayArrow)
            if (it.positionMs > 0) GlassButton(stringResource(R.string.from_start), { nav.watch(it.id, 0, f, audio, sub) }, if (tv) Modifier.rememberFocus(mem, "start") else Modifier.fillMaxWidth(), icon = Icons.Default.Replay)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            GlassButton(null, { userData(UserDataUpdate(favorite = !it.favorite)) }, Modifier.rememberFocus(mem, "fav", default = d.files.isEmpty()).width(52.dp),
                icon = if (it.favorite) Icons.Default.Check else Icons.Default.Add, active = it.favorite)
            GlassButton(stringResource(if (it.played) R.string.watched else R.string.mark_watched), { userData(UserDataUpdate(played = !it.played)) },
                Modifier.rememberFocus(mem, "played"), icon = Icons.Default.Check, active = it.played)
        }
        ErrorLine(actionError)
        if (d.files.isNotEmpty() && canWatchTogether(state)) {
            GlassButton(stringResource(R.string.watch_together), {
                scope.launch { runCatching { state.sync.create(it.id, it.positionMs) }.onSuccess { g -> invite = g.id }.onFailure { e -> togetherError = e } }
            }, if (tv) Modifier.rememberFocus(mem, "together") else Modifier.fillMaxWidth(), icon = Icons.Default.Groups)
            if (!tv) ErrorLine(togetherError)
        }
        // Offline downloads are a phone feature (a TV stays connected).
        if (!tv) ItemDownloads(state, nav, d, season, seasons.firstOrNull { s -> s.id == season }?.seasonNumber, d.files.getOrNull(file)?.id)
    }
    invite?.let { gid ->
        InviteDialog(api.inviteLink(gid), it.title, onDismiss = { invite = null }, onStart = { invite = null; nav.watch(it.id) })
    }
    CompositionLocalProvider(LocalAccent provides accent) {
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 30.dp), verticalArrangement = Arrangement.spacedBy(22.dp)) {
            item {
                Box(Modifier.fillMaxWidth().height(if (tv) 500.dp else 520.dp)) {
                    ServerImage(when { d.hasBackdrop -> api.imageUrl(it.id, "backdrop"); it.kind == "episode" && it.hasPoster -> api.imageUrl(it.id, "still"); else -> null }, Modifier.fillMaxSize())
                    Box(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(Kino.bg.copy(alpha = 0.5f), Color.Transparent, Kino.bg.copy(alpha = 0.8f), Kino.bg))))
                    if (tv) Box(Modifier.fillMaxSize().background(Brush.horizontalGradient(listOf(Kino.bg.copy(alpha = 0.9f), Color.Transparent), endX = 1500f)))
                    Column(Modifier.align(Alignment.BottomStart).padding(horizontal = Dims.edge).then(if (tv) Modifier.fillMaxWidth(0.62f) else Modifier), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        it.seriesTitle?.let { s -> Text(s.uppercase(), style = Head, color = Kino.muted, modifier = Modifier.focusClickable(scale = 1.02f) { it.seriesId?.let { sid -> nav.navigate("item/$sid") } }.padding(horizontal = if (tv) 4.dp else 0.dp)) }
                        val title = if (it.kind == "episode") stringResource(R.string.episode_title, it.episodeNumber ?: 0, it.title) else it.title
                        Text(title, style = Display.copy(fontSize = 40.sp), color = Kino.text)
                        Text(listOfNotNull(it.rating?.let { r -> stringResource(R.string.rating_value, r) }, it.year?.toString(), it.durationMs?.let { ms -> stringResource(R.string.minutes_short, ms / 60000) },
                            d.genres.take(2).joinToString(", ").ifBlank { null }, d.officialRating, qualityTags(it.videoHeight, it.hdr).joinToString(" ").ifBlank { null }).joinToString("  ·  "), color = Kino.muted, fontSize = 14.sp)
                        d.tagline?.let { t -> Text(t, color = Kino.text.copy(alpha = 0.8f), fontSize = 16.sp) }
                        if (tv) {
                            d.overview?.let { o -> Text(o, color = Kino.text.copy(alpha = 0.85f), fontSize = 15.sp, lineHeight = 22.sp, maxLines = 4, overflow = TextOverflow.Ellipsis) }
                            ProgressLine(it.progress, Modifier.padding(top = 4.dp).fillMaxWidth(0.5f))
                            Row(Modifier.padding(top = 8.dp, bottom = 12.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) { actions() }
                        }
                    }
                }
            }
            item {
                Column(Modifier.padding(horizontal = Dims.edge).then(if (tv) Modifier.fillMaxWidth(0.5f) else Modifier), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    if (!tv) {
                        actions()
                        ProgressLine(it.progress)
                    }
                    PlayOptions(d, file, audio, sub, { f, a, s -> file = f; audio = a; sub = s })
                    if (d.files.isNotEmpty()) SubtitleFinder(state, it.id)
                }
            }
            if (!tv) d.overview?.let { o -> item { Text(o, color = Kino.text.copy(alpha = 0.85f), fontSize = 16.sp, lineHeight = 24.sp, modifier = Modifier.padding(horizontal = 20.dp)) } }
            item {
                Column(Modifier.padding(horizontal = Dims.edge), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    val directors = d.crew.filter { p -> p.role == "Director" }.joinToString { p -> p.name }
                    val writers = d.crew.filter { p -> p.role != "Director" }.joinToString { p -> p.name }
                    Fact(stringResource(R.string.directed_by), directors)
                    Fact(stringResource(R.string.starring), d.cast.take(5).joinToString { p -> p.name })
                    Fact(stringResource(R.string.written_by), writers)
                    Fact(stringResource(R.string.studio), d.studios.joinToString())
                    Fact(stringResource(R.string.released), d.premiereDate.orEmpty())
                    if (d.collections.isNotEmpty()) Fact(stringResource(R.string.collection), d.collections.joinToString { c -> c.name })
                    // A TV has no browser for these links.
                    if (!tv) Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        d.providerIds["imdb"]?.let { imdb -> TextButton(onClick = { ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse("https://www.imdb.com/title/$imdb/"))) }) { Text("IMDb", color = Kino.muted) } }
                        d.providerIds["tmdb"]?.let { tmdb -> TextButton(onClick = { ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse("https://www.themoviedb.org/${if (it.kind == "series") "tv" else "movie"}/$tmdb"))) }) { Text("TMDb", color = Kino.muted) } }
                    }
                }
            }
            if (seasons.isNotEmpty()) item {
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    SectionTitle(stringResource(R.string.episodes))
                    LazyRow(contentPadding = Dims.rowPad, horizontalArrangement = Arrangement.spacedBy(if (tv) 12.dp else 8.dp)) {
                        items(seasons) { s -> GlassButton(if (s.seasonNumber == 0) stringResource(R.string.specials) else stringResource(R.string.season_n, s.seasonNumber ?: 0), { season = s.id },
                            Modifier.rememberFocus(mem, "s" + s.id), active = s.id == season) }
                    }
                    season?.let { sid -> SeasonEpisodes(state, nav, sid, mem) }
                }
            }
            if (episodes.isNotEmpty()) item { Column { episodes.forEach { e -> EpisodeRow(state, nav, e, mem) } } }
            if (d.chapters.size > 1) item {
                Shelf(stringResource(R.string.chapters), d.chapters) { c ->
                    Column(Modifier.width(200.dp).focusClickable { nav.watch(it.id, c.startMs) }) {
                        Box(Modifier.fillMaxWidth().aspectRatio(16f / 9f).clip(CardShape).background(Kino.surface2)) {
                            d.trickplay?.let { tp -> TrickFrame(api, tp, c.startMs + 20_000, Modifier.fillMaxSize()) }
                            Box(Modifier.align(Alignment.BottomStart).padding(6.dp)) { Tag(clock(c.startMs)) }
                        }
                        Text(c.title, color = Kino.text, fontSize = 13.sp, maxLines = 1, modifier = Modifier.padding(top = 6.dp))
                    }
                }
            }
            if (d.cast.isNotEmpty()) item {
                Shelf(stringResource(R.string.cast), d.cast) { p ->
                    Column(Modifier.width(96.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                        Box(Modifier.size(80.dp).clip(CircleShape).background(Kino.surface2), contentAlignment = Alignment.Center) {
                            Text(p.name.split(" ").mapNotNull { n -> n.firstOrNull()?.toString() }.take(2).joinToString(""), color = Kino.muted, fontWeight = FontWeight.Bold)
                            p.imageUrl?.let { u -> ServerImage(api.resolve(u), Modifier.fillMaxSize()) }
                        }
                        Text(p.name, color = Kino.text, fontSize = 13.sp, maxLines = 2, fontWeight = FontWeight.SemiBold)
                        p.role?.let { r -> Text(r, color = Kino.faint, fontSize = 12.sp, maxLines = 1) }
                    }
                }
            }
            if (d.files.isNotEmpty()) item {
                Column(Modifier.padding(horizontal = 20.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(stringResource(R.string.media_info).uppercase(), style = Head, color = Kino.text)
                    d.files.forEach { f ->
                        Column(Modifier.fillMaxWidth().clip(CardShape).background(Kino.surface).padding(14.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Text(listOfNotNull(f.label, f.height?.let { h -> "${h}p" }, f.hdr, f.videoCodec?.uppercase(), f.container.uppercase()).joinToString(" · "), color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 14.sp)
                            if (f.audio.isNotEmpty()) Text(stringResource(R.string.audio) + ": " + f.audio.map { a -> listOfNotNull(a.language?.uppercase(), a.codec.uppercase(), a.channels?.let { c -> stringResource(R.string.channels_n, c) }).joinToString(" ") }.joinToString(" · "), color = Kino.muted, fontSize = 12.sp)
                            if (f.subtitles.isNotEmpty()) Text(stringResource(R.string.subtitles) + ": " + f.subtitles.joinToString(" · ") { s -> s.language?.uppercase() ?: "?" }, color = Kino.muted, fontSize = 12.sp)
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun Fact(label: String, value: String) {
    if (value.isBlank()) return
    Column {
        Text(label.uppercase(), style = Label, color = Kino.faint)
        Text(value, color = Kino.text, fontSize = 15.sp)
    }
}

@Composable
private fun PlayOptions(d: ItemDetail, file: Int, audio: Int?, sub: Int?, onChange: (Int, Int?, Int?) -> Unit) {
    val f = d.files.getOrNull(file) ?: return
    if (d.files.size < 2 && f.audio.size < 2 && f.subtitles.isEmpty()) return
    if (d.files.size > 1) FieldRow(stringResource(R.string.version)) {
        Picker(file, d.files.mapIndexed { i, x -> i to (x.label ?: listOfNotNull(x.height?.let { h -> "${h}p" }, x.hdr, x.videoCodec?.uppercase()).joinToString(" ")) }, { onChange(it, null, null) })
    }
    if (f.audio.size > 1) FieldRow(stringResource(R.string.audio)) {
        Picker(audio ?: -1, listOf(-1 to stringResource(R.string.automatic)) + f.audio.mapIndexed { i, a -> i to audioLabel(a) },
            { onChange(file, it.takeIf { x -> x >= 0 }, sub) })
    }
    if (f.subtitles.isNotEmpty()) FieldRow(stringResource(R.string.subtitles)) {
        Picker(sub ?: -2, listOf(-2 to stringResource(R.string.automatic), -1 to stringResource(R.string.off)) + f.subtitles.mapIndexed { i, s -> i to listOfNotNull(s.language?.uppercase() ?: "?", if (s.forced) stringResource(R.string.forced) else null).joinToString(" · ") },
            { onChange(file, audio, it.takeIf { x -> x >= -1 }) })
    }
}

@Composable
private fun SeasonEpisodes(state: AppState, nav: NavHostController, seasonId: String, mem: FocusMemory) {
    val api = state.api ?: return
    val (season, loading) = rememberLoad(seasonId) { api.item(seasonId) }
    if (loading) { Loading(); return }
    Column(Modifier.padding(vertical = if (LocalTv.current) 6.dp else 0.dp), verticalArrangement = Arrangement.spacedBy(if (LocalTv.current) 6.dp else 0.dp)) {
        season?.children?.filter { it.kind == "episode" }?.forEach { e -> EpisodeRow(state, nav, e, mem) }
    }
}

@Composable
private fun EpisodeRow(state: AppState, nav: NavHostController, e: ItemSummary, mem: FocusMemory) {
    val api = state.api ?: return
    val tv = LocalTv.current
    val (src, focused) = rememberFocusSource()
    Row(Modifier.fillMaxWidth().padding(horizontal = if (tv) 28.dp else 0.dp).rememberFocus(mem, "e" + e.id)
        .clip(CardShape).background(if (focused) Kino.surface3 else Color.Transparent)
        .clickable(interactionSource = src, indication = tapIndication()) { nav.watch(e.id) }.padding(horizontal = if (tv) 12.dp else 20.dp, vertical = 10.dp),
        horizontalArrangement = Arrangement.spacedBy(if (tv) 18.dp else 12.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.width(if (tv) 176.dp else 140.dp).focusRing(focused, scale = 1.04f).aspectRatio(16f / 9f).clip(CardShape).background(Kino.surface2)) {
            if (e.hasPoster) ServerImage(api.imageUrl(e.id, "still"), Modifier.fillMaxSize())
            ProgressLine(e.progress, Modifier.align(Alignment.BottomCenter))
        }
        Column(Modifier.weight(1f)) {
            Text("${e.episodeNumber ?: ""}. ${e.title}", color = Kino.text, fontWeight = FontWeight.SemiBold, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Text(listOfNotNull(e.durationMs?.let { stringResource(R.string.minutes_short, it / 60000) }, if (e.played) stringResource(R.string.watched) else null).joinToString(" · "), color = Kino.faint, fontSize = 13.sp)
        }
        DownloadedMark(state, e.id)
    }
}

/** One frame from the trickplay sheets (seek-bar thumbnails) as a chapter picture. */
@Composable
fun TrickFrame(api: Api, tp: TrickplayInfo, ms: Long, modifier: Modifier) {
    val idx = (ms / tp.intervalMs).toInt().coerceIn(0, tp.count - 1)
    val per = tp.columns * tp.rows
    val sheet = tp.sheetUrls.getOrNull(idx / per) ?: return
    val local = idx % per
    val col = local % tp.columns
    val row = local / tp.columns
    BoxWithConstraints(modifier.clip(CardShape)) {
        val w = maxWidth * tp.columns
        val h = maxHeight * tp.rows
        Box(Modifier.requiredSize(w, h).offset(x = -(maxWidth * col) + (w - maxWidth) / 2, y = -(maxHeight * row) + (h - maxHeight) / 2)) {
            ServerImage(api.resolve(sheet), Modifier.fillMaxSize(), androidx.compose.ui.layout.ContentScale.FillBounds)
        }
    }
}

@Composable
fun CollectionsScreen(state: AppState, nav: NavHostController) {
    val api = state.api ?: return
    val (cols, loading, error) = rememberLoad(Unit) { api.collections() }
    val (pls) = rememberLoad(Unit) { runCatching { api.playlists() }.getOrDefault(emptyList()) }
    LazyVerticalGrid(GridCells.Adaptive(Dims.gridCell), Modifier.fillMaxSize(), contentPadding = gridPadding(),
        horizontalArrangement = Arrangement.spacedBy(gridGap()), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(R.string.collections), style = Display.copy(fontSize = 34.sp), color = Kino.text, modifier = Modifier.statusBarsPadding().padding(top = 12.dp)) }
        if (loading) item(span = { GridItemSpan(maxLineSpan) }) { Loading() }
        item(span = { GridItemSpan(maxLineSpan) }) { ErrorLine(error) }
        items(cols.orEmpty(), key = { it.id }) { c -> CollectionTile(api, c, Dims.gridCell, Modifier.initialFocus(c.id == cols?.firstOrNull()?.id)) { nav.navigate("collection/${c.id}") } }
        if (!pls.isNullOrEmpty()) {
            item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(R.string.playlists).uppercase(), style = Head, color = Kino.text, modifier = Modifier.padding(top = 16.dp)) }
            items(pls!!, key = { "p" + it.id }) { p ->
                Column(Modifier.focusClickable { nav.navigate("playlist/${p.id}") }) {
                    Box(Modifier.fillMaxWidth().aspectRatio(1f).clip(CardShape).background(Kino.surface2)) {
                        p.coverItemIds.firstOrNull()?.let { cid -> ServerImage(api.imageUrl(cid, "poster"), Modifier.fillMaxSize()) }
                    }
                    Text(p.name, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, maxLines = 1)
                    Text(itemsCount(p.itemCount), color = Kino.faint, fontSize = 13.sp)
                }
            }
        }
    }
}

@Composable
fun CollectionScreen(state: AppState, nav: NavHostController, id: String) {
    val api = state.api ?: return
    val (d, loading, error) = rememberLoad(id) { api.collection(id) }
    ItemGridPage(state, nav, d?.collection?.name.orEmpty(), d?.collection?.overview, d?.items.orEmpty(), loading, error)
}

@Composable
fun PlaylistScreen(state: AppState, nav: NavHostController, id: String) {
    val api = state.api ?: return
    val (d, loading, error) = rememberLoad(id) { api.playlist(id) }
    ItemGridPage(state, nav, d?.playlist?.name.orEmpty(), null, d?.items.orEmpty(), loading, error, playAll = d?.items?.firstOrNull()?.let { first -> { nav.watch(first.id) } })
}

@Composable
private fun ItemGridPage(state: AppState, nav: NavHostController, title: String, overview: String?, items: List<ItemSummary>, loading: Boolean, error: Throwable?, playAll: (() -> Unit)? = null) {
    val api = state.api ?: return
    LazyVerticalGrid(GridCells.Adaptive(Dims.gridCell), Modifier.fillMaxSize(), contentPadding = gridPadding(),
        horizontalArrangement = Arrangement.spacedBy(gridGap()), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column(Modifier.statusBarsPadding().padding(top = 12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(title, style = Display.copy(fontSize = 34.sp), color = Kino.text)
                overview?.let { Text(it, color = Kino.muted) }
                playAll?.let { PrimaryButton(stringResource(R.string.play), it, icon = Icons.Default.PlayArrow) }
                ErrorLine(error)
            }
        }
        if (loading) item(span = { GridItemSpan(maxLineSpan) }) { Loading() }
        items(items, key = { it.id }) { PosterCard(it, api::imageUrl, Dims.gridCell, state.enhancements.qualityTags, Modifier.initialFocus(it.id == items.firstOrNull()?.id)) { nav.navigate("item/${it.id}") } }
    }
}
