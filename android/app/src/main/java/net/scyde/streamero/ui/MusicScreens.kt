package net.scyde.streamero.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.media3.common.Player
import androidx.navigation.NavHostController
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.*

@Composable
private fun SquareCover(api: Api, item: ItemSummary, round: Boolean = false, modifier: Modifier = Modifier) {
    Box(modifier.aspectRatio(1f).clip(if (round) CircleShape else CardShape).background(Kino.surface2), contentAlignment = Alignment.Center) {
        Icon(if (round) Icons.Default.Person else Icons.Default.Album, null, tint = Kino.faint, modifier = Modifier.size(36.dp))
        if (item.hasPoster) ServerImage(api.imageUrl(item.id, "poster"), Modifier.fillMaxSize())
    }
}

@Composable
fun MusicLibraryScreen(state: AppState, nav: NavHostController, lib: Library) {
    val api = state.api ?: return
    var tab by remember { mutableStateOf("album") }
    val (page, loading, error) = rememberLoad(tab) { api.items(lib.id, mapOf("kind" to tab, "sort" to "title", "limit" to 200)) }
    val scope = rememberCoroutineScope()
    LazyVerticalGrid(GridCells.Adaptive(if (tab == "track") 600.dp else 140.dp), Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(14.dp), verticalArrangement = Arrangement.spacedBy(if (tab == "track") 0.dp else 18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column(Modifier.statusBarsPadding().padding(top = 12.dp, bottom = 8.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(lib.name.uppercase(), style = Display.copy(fontSize = 34.sp), color = Kino.text)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    listOf("album" to R.string.albums, "artist" to R.string.artists, "track" to R.string.songs).forEach { (k, l) ->
                        GlassButton(stringResource(l), { tab = k }, active = tab == k)
                    }
                }
                PrimaryButton(stringResource(R.string.shuffle_all), {
                    scope.launch { runCatching { api.items(lib.id, mapOf("kind" to "track", "sort" to "random", "limit" to 200)) }.onSuccess { state.music.play(it.items) } }
                }, icon = Icons.Default.Shuffle)
                ErrorLine(error)
            }
        }
        if (loading) item(span = { GridItemSpan(maxLineSpan) }) { Loading() }
        val items = page?.items.orEmpty()
        if (tab == "track") {
            items(items.size, span = { GridItemSpan(maxLineSpan) }) { i -> TrackRow(state, items, i, showAlbum = true) }
        } else items(items, key = { it.id }) { it ->
            Column(Modifier.clickable { nav.navigate("item/${it.id}") }, horizontalAlignment = if (tab == "artist") Alignment.CenterHorizontally else Alignment.Start) {
                SquareCover(api, it, round = tab == "artist", modifier = Modifier.fillMaxWidth())
                Spacer(Modifier.height(8.dp))
                Text(it.title, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (tab == "album") Text(listOfNotNull(it.artist, it.year?.toString()).joinToString(" · "), color = Kino.faint, fontSize = 13.sp, maxLines = 1)
            }
        }
    }
}

@Composable
fun TrackRow(state: AppState, tracks: List<ItemSummary>, i: Int, showAlbum: Boolean = false) {
    val t = tracks[i]
    val active = state.music.current?.id == t.id
    Row(Modifier.fillMaxWidth().clickable { state.music.play(tracks, i) }.padding(vertical = 10.dp, horizontal = 4.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
        Text(if (active) "▶" else (t.episodeNumber ?: (i + 1)).toString(), color = if (active) LocalAccent.current else Kino.faint, fontSize = 14.sp, modifier = Modifier.width(26.dp))
        Column(Modifier.weight(1f)) {
            Text(t.title, color = if (active) LocalAccent.current else Kino.text, fontWeight = if (active) FontWeight.SemiBold else FontWeight.Normal, maxLines = 1, overflow = TextOverflow.Ellipsis)
            val sub = listOfNotNull(t.artist, if (showAlbum) t.album else null).joinToString(" · ")
            if (sub.isNotBlank()) Text(sub, color = Kino.faint, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        t.durationMs?.let { Text(clock(it), color = Kino.faint, fontSize = 13.sp) }
    }
}

@Composable
fun AlbumScreen(state: AppState, nav: NavHostController, d: ItemDetail) {
    val api = state.api ?: return
    val tracks = d.children.filter { it.kind == "track" }
    val accent = hexColor(d.item.colors?.firstOrNull())
    CompositionLocalProvider(LocalAccent provides accent) {
        LazyColumn(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(accent.copy(alpha = 0.25f), Kino.bg, Kino.bg))), contentPadding = PaddingValues(20.dp)) {
            item {
                Column(Modifier.statusBarsPadding(), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    SquareCover(api, d.item, modifier = Modifier.fillMaxWidth(0.75f).align(Alignment.CenterHorizontally))
                    Text(stringResource(R.string.album).uppercase(), style = Label, color = Kino.faint)
                    Text(d.item.title.uppercase(), style = Display.copy(fontSize = 32.sp), color = Kino.text)
                    Text(listOfNotNull(d.item.artist, d.item.year?.toString(), d.genres.firstOrNull(), stringResource(R.string.songs_count, tracks.size)).joinToString(" · "),
                        color = Kino.muted, modifier = Modifier.clickable { d.parentId?.let { nav.navigate("item/$it") } })
                    Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        PrimaryButton(stringResource(R.string.play), { state.music.play(tracks, 0) }, icon = Icons.Default.PlayArrow)
                        GlassButton(stringResource(R.string.shuffle), { state.music.shuffle = true; state.music.play(tracks, tracks.indices.random()) }, icon = Icons.Default.Shuffle)
                    }
                    Spacer(Modifier.height(8.dp))
                }
            }
            itemsIndexed(tracks) { i, _ -> TrackRow(state, tracks, i) }
        }
    }
}

@Composable
fun ArtistScreen(state: AppState, nav: NavHostController, d: ItemDetail) {
    val api = state.api ?: return
    val albums = d.children.filter { it.kind == "album" }.sortedByDescending { it.year ?: 0 }
    val scope = rememberCoroutineScope()
    LazyVerticalGrid(GridCells.Adaptive(140.dp), Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(14.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column(Modifier.statusBarsPadding().padding(top = 12.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(12.dp)) {
                SquareCover(api, d.item, round = true, modifier = Modifier.size(180.dp))
                Text(d.item.title.uppercase(), style = Display.copy(fontSize = 32.sp), color = Kino.text)
                Text(stringResource(R.string.albums_count, albums.size), color = Kino.muted)
                PrimaryButton(stringResource(R.string.play_all), {
                    scope.launch {
                        val all = albums.flatMap { a -> runCatching { api.item(a.id).children.filter { it.kind == "track" } }.getOrDefault(emptyList()) }
                        state.music.play(all)
                    }
                }, icon = Icons.Default.PlayArrow)
            }
        }
        items(albums, key = { it.id }) { a ->
            Column(Modifier.clickable { nav.navigate("item/${a.id}") }) {
                SquareCover(api, a, modifier = Modifier.fillMaxWidth())
                Text(a.title, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, maxLines = 1, modifier = Modifier.padding(top = 8.dp))
                a.year?.let { Text(it.toString(), color = Kino.faint, fontSize = 13.sp) }
            }
        }
    }
}

/** The music bar above the tabs; tap to open the queue. */
@Composable
fun MiniPlayer(state: AppState, nav: NavHostController) {
    val m = state.music
    val c = m.current ?: return
    val api = state.api ?: return
    var queueOpen by remember { mutableStateOf(false) }
    Column(Modifier.background(Kino.surface)) {
        val pct = if (m.duration > 0) (m.position.toFloat() / m.duration).coerceIn(0f, 1f) else 0f
        Box(Modifier.fillMaxWidth().height(2.dp).background(Color.White.copy(alpha = 0.12f))) { Box(Modifier.fillMaxHeight().fillMaxWidth(pct).background(LocalAccent.current)) }
        Row(Modifier.fillMaxWidth().clickable { queueOpen = true }.padding(horizontal = 12.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Box(Modifier.size(44.dp).clip(CardShape).background(Kino.surface2)) {
                ServerImage(api.imageUrl(if (c.hasPoster) c.id else c.parentId ?: c.id, "poster"), Modifier.fillMaxSize())
            }
            Column(Modifier.weight(1f)) {
                Text(c.title, color = Kino.text, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(listOfNotNull(c.artist, c.album).joinToString(" · "), color = Kino.faint, fontSize = 12.sp, maxLines = 1)
            }
            IconButton(onClick = { m.prev() }) { Icon(Icons.Default.SkipPrevious, stringResource(R.string.previous), tint = Kino.text) }
            IconButton(onClick = { m.toggle() }) { Icon(if (m.playing) Icons.Default.Pause else Icons.Default.PlayArrow, stringResource(R.string.play), tint = Kino.text) }
            IconButton(onClick = { m.next() }) { Icon(Icons.Default.SkipNext, stringResource(R.string.next), tint = Kino.text) }
        }
    }
    if (queueOpen) ModalBottomSheetQueue(state) { queueOpen = false }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ModalBottomSheetQueue(state: AppState, onClose: () -> Unit) {
    val m = state.music
    ModalBottomSheet(onDismissRequest = onClose, containerColor = Kino.surface) {
        Column(Modifier.padding(horizontal = 20.dp).padding(bottom = 30.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            m.current?.let { c ->
                Text(c.title.uppercase(), style = Display.copy(fontSize = 26.sp), color = Kino.text)
                Text(listOfNotNull(c.artist, c.album).joinToString(" · "), color = Kino.muted)
            }
            Slider(value = m.position.toFloat(), onValueChange = { m.seek(it.toLong()) }, valueRange = 0f..(m.duration.coerceAtLeast(1)).toFloat(),
                colors = SliderDefaults.colors(thumbColor = Kino.text, activeTrackColor = LocalAccent.current))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween, verticalAlignment = Alignment.CenterVertically) {
                Text(clock(m.position), color = Kino.faint, fontSize = 12.sp)
                IconButton(onClick = { m.toggleShuffle() }) { Icon(Icons.Default.Shuffle, stringResource(R.string.shuffle), tint = if (m.shuffle) LocalAccent.current else Kino.muted) }
                IconButton(onClick = { m.prev() }) { Icon(Icons.Default.SkipPrevious, null, tint = Kino.text, modifier = Modifier.size(32.dp)) }
                FilledIconButton(onClick = { m.toggle() }, colors = IconButtonDefaults.filledIconButtonColors(containerColor = Kino.text, contentColor = Kino.bg), modifier = Modifier.size(56.dp)) {
                    Icon(if (m.playing) Icons.Default.Pause else Icons.Default.PlayArrow, null, modifier = Modifier.size(30.dp))
                }
                IconButton(onClick = { m.next() }) { Icon(Icons.Default.SkipNext, null, tint = Kino.text, modifier = Modifier.size(32.dp)) }
                IconButton(onClick = { m.cycleRepeat() }) {
                    Icon(if (m.repeat == Player.REPEAT_MODE_ONE) Icons.Default.RepeatOne else Icons.Default.Repeat, stringResource(R.string.repeat), tint = if (m.repeat != Player.REPEAT_MODE_OFF) LocalAccent.current else Kino.muted)
                }
                Text(clock(m.duration), color = Kino.faint, fontSize = 12.sp)
            }
            Text(stringResource(R.string.queue).uppercase(), style = Head, color = Kino.text, modifier = Modifier.padding(top = 8.dp))
            LazyColumn(Modifier.heightIn(max = 360.dp)) {
                itemsIndexed(m.queue) { i, t ->
                    Row(Modifier.fillMaxWidth().clickable { m.jump(i) }.padding(vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        Text("${i + 1}", color = if (i == m.index) LocalAccent.current else Kino.faint, modifier = Modifier.width(24.dp))
                        Column { Text(t.title, color = if (i == m.index) LocalAccent.current else Kino.text, maxLines = 1); Text(t.artist.orEmpty(), color = Kino.faint, fontSize = 12.sp) }
                    }
                }
            }
            TextButton(onClick = { m.stop(); onClose() }) { Text(stringResource(R.string.stop), color = Kino.muted) }
        }
    }
}
