package net.scyde.streamero.ui

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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.Search
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
import androidx.navigation.NavHostController
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.*

/** Runs [load] once (and again when [key] changes), keeping the value, loading flag and error. */
@Composable
fun <T> rememberLoad(key: Any?, load: suspend () -> T): Triple<T?, Boolean, Throwable?> {
    var value by remember(key) { mutableStateOf<T?>(null) }
    var loading by remember(key) { mutableStateOf(true) }
    var error by remember(key) { mutableStateOf<Throwable?>(null) }
    LaunchedEffect(key) {
        loading = true
        runCatching { load() }.onSuccess { value = it; error = null }.onFailure { error = it }
        loading = false
    }
    return Triple(value, loading, error)
}

@Composable
fun HomeScreen(state: AppState, nav: NavHostController) {
    val api = state.api ?: return
    val (data, loading, error) = rememberLoad(state.me?.currentProfile?.id) {
        val prefs = runCatching { api.preferences() }.getOrNull()
        HomeData(api.latest(), runCatching { api.continueWatching() }.getOrDefault(emptyList()), runCatching { api.nextUp() }.getOrDefault(emptyList()),
            runCatching { api.favorites() }.getOrDefault(emptyList()), runCatching { api.collections() }.getOrDefault(emptyList()),
            runCatching { api.libraries() }.getOrDefault(emptyList()), prefs?.homeSections)
    }
    val img: (String, String) -> String = api::imageUrl
    LazyColumn(Modifier.fillMaxSize(), verticalArrangement = Arrangement.spacedBy(28.dp), contentPadding = PaddingValues(bottom = 24.dp)) {
        item { Stage(state, nav, data?.latest.orEmpty()) }
        if (loading) item { Loading() }
        if (error != null) item { Box(Modifier.padding(20.dp)) { ErrorLine(error) } }
        val d = data ?: return@LazyColumn
        if (d.latest.isEmpty() && d.cont.isEmpty() && d.nextUp.isEmpty() && d.favorites.isEmpty() && d.collections.isEmpty()) item {
            Column(Modifier.fillMaxWidth().statusBarsPadding().padding(horizontal = 20.dp, vertical = 48.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(stringResource(R.string.home_empty_title).uppercase(), style = Display.copy(fontSize = 34.sp), color = Kino.text)
                Text(stringResource(if (state.isAdmin) R.string.home_empty_admin else R.string.home_empty_user), color = Kino.muted, fontSize = 15.sp)
            }
        }
        val order = d.sections ?: listOf("continue", "nextUp", "latest", "favorites", "collections", "libraries")
        order.forEach { s ->
            when (s) {
                "continue" -> item { Shelf(stringResource(R.string.continue_watching), d.cont) { WideCard(it, img, 280.dp) { nav.watch(it.id) } } }
                "nextUp" -> item { Shelf(stringResource(R.string.next_up), d.nextUp) { WideCard(it, img, 280.dp) { nav.watch(it.id) } } }
                "latest" -> item { Shelf(stringResource(R.string.recently_added), d.latest) { PosterCard(it, img, 130.dp, state.enhancements.qualityTags) { nav.navigate("item/${it.id}") } } }
                "favorites" -> item { Shelf(stringResource(R.string.my_list), d.favorites) { PosterCard(it, img, 130.dp, state.enhancements.qualityTags) { nav.navigate("item/${it.id}") } } }
                "collections" -> item { Shelf(stringResource(R.string.collections), d.collections) { c -> CollectionTile(api, c, 130.dp) { nav.navigate("collection/${c.id}") } } }
                "libraries" -> item {
                    Shelf(stringResource(R.string.libraries), d.libraries) { l -> LibraryTile(l) { nav.navigate("library/${l.id}") } }
                }
            }
        }
    }
}

private data class HomeData(val latest: List<ItemSummary>, val cont: List<ItemSummary>, val nextUp: List<ItemSummary>, val favorites: List<ItemSummary>,
    val collections: List<MediaCollection>, val libraries: List<Library>, val sections: List<String>?)

/** The stage: one film edge to edge with its title set large, chosen from the newest titles. */
@Composable
fun Stage(state: AppState, nav: NavHostController, pool: List<ItemSummary>) {
    val api = state.api ?: return
    val choices = pool.filter { it.kind == "movie" || it.kind == "series" }.take(10)
    var pick by remember(choices.size) { mutableStateOf(choices.firstOrNull { it.hasBackdrop } ?: choices.firstOrNull()) }
    val p = pick ?: run { Spacer(Modifier.height(80.dp)); return }
    val accent = hexColor(p.colors?.firstOrNull())
    val scope = rememberCoroutineScope()
    Box(Modifier.fillMaxWidth().height(560.dp)) {
        ServerImage(if (p.hasBackdrop) api.imageUrl(p.id, "backdrop") else null, Modifier.fillMaxSize())
        Box(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(Kino.bg.copy(alpha = 0.6f), Color.Transparent, Kino.bg.copy(alpha = 0.7f), Kino.bg))))
        Box(Modifier.fillMaxSize().background(Brush.radialGradient(listOf(accent.copy(alpha = 0.28f), Color.Transparent), radius = 900f, center = androidx.compose.ui.geometry.Offset(0f, 1400f))))
        Column(Modifier.align(Alignment.BottomStart).padding(horizontal = 20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(p.title.uppercase(), style = Display.copy(fontSize = 44.sp), color = Kino.text, maxLines = 3)
            Text(listOfNotNull(p.rating?.let { "★ %.1f".format(it) }, p.year?.toString(), p.durationMs?.let { "${it / 60000} min" }).joinToString("  ·  "), color = Kino.muted)
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                if (p.kind == "movie") PrimaryButton(stringResource(R.string.play), { nav.watch(p.id) }, icon = Icons.Default.PlayArrow)
                GlassButton(stringResource(R.string.more_info), { nav.navigate("item/${p.id}") }, icon = Icons.Outlined.Info)
                if (state.enhancements.randomButton) GlassButton(null, {
                    scope.launch {
                        val libs = runCatching { api.libraries() }.getOrDefault(emptyList()).filter { it.kind != "music" }.shuffled()
                        for (l in libs) {
                            val it = runCatching { api.items(l.id, mapOf("sort" to "random", "unplayed" to true, "limit" to 1)).items.firstOrNull() }.getOrNull()
                            if (it != null) { nav.navigate("item/${it.id}"); break }
                        }
                    }
                }, Modifier.width(52.dp), icon = Icons.Default.Shuffle)
            }
            LazyRow(horizontalArrangement = Arrangement.spacedBy(10.dp), contentPadding = PaddingValues(vertical = 8.dp)) {
                items(choices) { c ->
                    Box(Modifier.width(78.dp).aspectRatio(2f / 3f).clip(CardShape).background(Kino.surface2)
                        .then(if (c.id == p.id) Modifier.background(Kino.text) else Modifier).clickable { pick = c }) {
                        ServerImage(if (c.hasPoster) api.imageUrl(c.id, "poster") else if (c.hasBackdrop) api.imageUrl(c.id, "backdrop") else null,
                            Modifier.fillMaxSize().padding(if (c.id == p.id) 2.dp else 0.dp).clip(CardShape))
                    }
                }
            }
        }
    }
}

@Composable
fun LibraryTile(l: Library, onClick: () -> Unit) {
    Box(Modifier.width(220.dp).height(110.dp).clip(RoundedCornerShape(6.dp)).background(Kino.surface2).clickable(onClick = onClick).padding(16.dp)) {
        Column(Modifier.align(Alignment.BottomStart)) {
            Text(l.name.uppercase(), style = Display.copy(fontSize = 24.sp), color = Kino.text)
            Text(itemsCount(l.itemCount), color = Kino.muted, fontSize = 13.sp)
        }
    }
}

@Composable
fun CollectionTile(api: Api, c: MediaCollection, width: androidx.compose.ui.unit.Dp, onClick: () -> Unit) {
    Column(Modifier.width(width).clickable(onClick = onClick)) {
        Box(Modifier.fillMaxWidth().aspectRatio(2f / 3f).clip(CardShape).background(Kino.surface2)) {
            if (c.hasPoster) ServerImage(api.collectionImageUrl(c.id, "poster"), Modifier.fillMaxSize())
            else Text(c.name.uppercase(), style = Display.copy(fontSize = 18.sp), color = Kino.text, modifier = Modifier.align(Alignment.BottomStart).padding(10.dp))
        }
        Spacer(Modifier.height(8.dp))
        Text(c.name, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(itemsCount(c.itemCount), color = Kino.faint, fontSize = 13.sp)
    }
}

@Composable
fun PageTitle(text: String, modifier: Modifier = Modifier) {
    Text(text.uppercase(), style = Display.copy(fontSize = 34.sp), color = Kino.text, modifier = modifier.statusBarsPadding().padding(start = 20.dp, end = 20.dp, top = 20.dp, bottom = 8.dp))
}

@Composable
fun LibrariesScreen(state: AppState, nav: NavHostController) {
    val api = state.api ?: return
    val (libs, loading, error) = rememberLoad(Unit) { api.libraries() }
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 20.dp)) {
        item { PageTitle(stringResource(R.string.libraries)) }
        if (loading) item { Loading() }
        item { Box(Modifier.padding(horizontal = 20.dp)) { ErrorLine(error) } }
        items(libs.orEmpty()) { l ->
            Box(Modifier.padding(horizontal = 20.dp, vertical = 6.dp)) {
                Box(Modifier.fillMaxWidth().height(110.dp).clip(RoundedCornerShape(6.dp)).background(Kino.surface2).clickable { nav.navigate("library/${l.id}") }.padding(18.dp)) {
                    Column(Modifier.align(Alignment.BottomStart)) {
                        Text(l.name.uppercase(), style = Display.copy(fontSize = 28.sp), color = Kino.text)
                        Text(itemsCount(l.itemCount), color = Kino.muted, fontSize = 13.sp)
                    }
                }
            }
        }
        item {
            Row(Modifier.padding(20.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                GlassButton(stringResource(R.string.collections), { nav.navigate("collections") })
                GlassButton(stringResource(R.string.my_list), { nav.navigate("library/favorites") })
            }
        }
    }
}

private val STATUSES = listOf("", "unplayed", "resumable", "favorite")

@Composable
fun LibraryScreen(state: AppState, nav: NavHostController, libraryId: String) {
    val api = state.api ?: return
    if (libraryId == "favorites") { FavoritesScreen(state, nav); return }
    val (libs) = rememberLoad(Unit) { api.libraries() }
    val lib = libs?.firstOrNull { it.id == libraryId }
    if (lib?.kind == "music") { MusicLibraryScreen(state, nav, lib); return }
    var sort by remember { mutableStateOf("added") }
    var genre by remember { mutableStateOf("") }
    var status by remember { mutableStateOf("") }
    val (genres) = rememberLoad(libraryId) { api.genres(libraryId) }
    var items by remember { mutableStateOf<List<ItemSummary>>(emptyList()) }
    var total by remember { mutableIntStateOf(-1) }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    val scope = rememberCoroutineScope()
    suspend fun load(reset: Boolean) {
        loading = true
        runCatching {
            api.items(libraryId, mapOf("sort" to sort, "genre" to genre.ifBlank { null }, "limit" to 60, "offset" to if (reset) 0 else items.size,
                "unplayed" to (status == "unplayed").takeIf { it }, "resumable" to (status == "resumable").takeIf { it }, "favorite" to (status == "favorite").takeIf { it }))
        }.onSuccess { p -> items = if (reset) p.items else items + p.items; total = p.total; error = null }.onFailure { error = it }
        loading = false
    }
    LaunchedEffect(sort, genre, status) { load(true) }
    LazyVerticalGrid(GridCells.Adaptive(118.dp), Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text((lib?.name ?: "").uppercase(), style = Display.copy(fontSize = 34.sp), color = Kino.text, modifier = Modifier.statusBarsPadding().padding(top = 12.dp))
                if (total >= 0) Text(itemsCount(total), color = Kino.muted, fontSize = 13.sp)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Picker(sort, listOf("added" to stringResource(R.string.sort_added), "title" to stringResource(R.string.sort_title),
                        "year" to stringResource(R.string.sort_year), "rating" to stringResource(R.string.sort_rating), "random" to stringResource(R.string.sort_random)), { sort = it }, Modifier.weight(1f))
                    Picker(status, STATUSES.map { it to stringResource(when (it) { "unplayed" -> R.string.status_unplayed; "resumable" -> R.string.status_resumable; "favorite" -> R.string.my_list; else -> R.string.status_all }) },
                        { status = it }, Modifier.weight(1f))
                }
                if (!genres.isNullOrEmpty()) Picker(genre, listOf("" to stringResource(R.string.all_genres)) + genres.map { it to it }, { genre = it })
                ErrorLine(error)
            }
        }
        items(items, key = { it.id }) { PosterCard(it, api::imageUrl, 118.dp, state.enhancements.qualityTags) { nav.navigate("item/${it.id}") } }
        if (total > items.size) item(span = { GridItemSpan(maxLineSpan) }) {
            LaunchedEffect(items.size) { if (!loading) load(false) }
            Loading()
        }
        if (total == 0) item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(R.string.nothing_here), color = Kino.muted) }
    }
    @Suppress("UNUSED_EXPRESSION") scope
}

@Composable
fun FavoritesScreen(state: AppState, nav: NavHostController) {
    val api = state.api ?: return
    val (favs, loading, error) = rememberLoad(Unit) { api.favorites() }
    LazyVerticalGrid(GridCells.Adaptive(118.dp), Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(R.string.my_list).uppercase(), style = Display.copy(fontSize = 34.sp), color = Kino.text, modifier = Modifier.statusBarsPadding().padding(top = 12.dp)) }
        if (loading) item(span = { GridItemSpan(maxLineSpan) }) { Loading() }
        item(span = { GridItemSpan(maxLineSpan) }) { ErrorLine(error) }
        items(favs.orEmpty(), key = { it.id }) { PosterCard(it, api::imageUrl, 118.dp, state.enhancements.qualityTags) { nav.navigate("item/${it.id}") } }
    }
}

@Composable
fun SearchScreen(state: AppState, nav: NavHostController) {
    val api = state.api ?: return
    var q by remember { mutableStateOf("") }
    var results by remember { mutableStateOf<List<ItemSummary>?>(null) }
    var elsewhere by remember { mutableStateOf<DiscoverPage?>(null) }
    LaunchedEffect(q) {
        if (q.isBlank()) { results = null; elsewhere = null; return@LaunchedEffect }
        delay(300)
        results = runCatching { api.search(q) }.getOrDefault(emptyList())
        elsewhere = if (state.enhancements.discoverInSearch) runCatching { api.discover(q) }.getOrNull() else null
    }
    LazyVerticalGrid(GridCells.Adaptive(118.dp), Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            OutlinedTextField(q, { q = it }, leadingIcon = { Icon(Icons.Outlined.Search, null) }, placeholder = { Text(stringResource(R.string.search_hint)) },
                singleLine = true, modifier = Modifier.fillMaxWidth().statusBarsPadding().padding(top = 8.dp), shape = CardShape)
        }
        items(results.orEmpty(), key = { it.id }) { PosterCard(it, api::imageUrl, 118.dp, state.enhancements.qualityTags) { nav.navigate("item/${it.id}") } }
        if (results?.isEmpty() == true) item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(R.string.nothing_found), color = Kino.muted) }
        val missing = elsewhere?.takeIf { it.configured }?.titles?.filter { it.itemId == null }.orEmpty()
        if (missing.isNotEmpty()) {
            item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(R.string.not_here_yet).uppercase(), style = Head, color = Kino.text, modifier = Modifier.padding(top = 12.dp)) }
            items(missing.take(12), key = { "d${it.mediaType}${it.tmdbId}" }) { DiscoverCard(state, it, elsewhere!!.canRequest, nav) }
        }
    }
}

@Composable
fun DiscoverCard(state: AppState, t: DiscoverTitle, canRequest: Boolean, nav: NavHostController) {
    val api = state.api ?: return
    var status by remember(t.tmdbId) { mutableStateOf(t.status) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    val scope = rememberCoroutineScope()
    Column(Modifier.width(118.dp)) {
        Box(Modifier.fillMaxWidth().aspectRatio(2f / 3f).clip(CardShape).background(Kino.surface2)) {
            // TMDb poster URLs are public CDN images, loaded without the session.
            if (t.posterUrl != null) coil.compose.AsyncImage(t.posterUrl, null, Modifier.fillMaxSize(), contentScale = androidx.compose.ui.layout.ContentScale.Crop)
            if (status != "none") Box(Modifier.align(Alignment.TopStart).padding(6.dp)) { Tag(stringResource(statusLabel(status)).uppercase()) }
        }
        Spacer(Modifier.height(6.dp))
        Text(t.title, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(listOfNotNull(t.year?.toString(), stringResource(if (t.mediaType == "tv") R.string.series else R.string.movie)).joinToString(" · "), color = Kino.faint, fontSize = 12.sp)
        Spacer(Modifier.height(6.dp))
        when {
            t.itemId != null -> GlassButton(stringResource(R.string.watch), { nav.navigate("item/${t.itemId}") }, Modifier.fillMaxWidth().heightIn(max = 40.dp))
            status == "none" && canRequest -> PrimaryButton(stringResource(R.string.request), enabled = !busy, modifier = Modifier.fillMaxWidth().heightIn(max = 40.dp), onClick = {
                busy = true
                scope.launch { runCatching { api.request(t) }.onSuccess { status = it.status }.onFailure { error = it }; busy = false }
            })
        }
        ErrorLine(error)
    }
}

fun statusLabel(s: String) = when (s) {
    "pending" -> R.string.status_requested
    "processing" -> R.string.status_downloading
    "partial" -> R.string.status_partial
    "available" -> R.string.status_available
    else -> R.string.status_not_requested
}

@Composable
fun DiscoverScreen(state: AppState, nav: NavHostController) {
    val api = state.api ?: return
    var q by remember { mutableStateOf("") }
    var page by remember { mutableStateOf<DiscoverPage?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    val (upcoming) = rememberLoad(Unit) { runCatching { api.upcoming() }.getOrDefault(emptyList()) }
    LaunchedEffect(q) {
        delay(if (q.isBlank()) 0 else 350)
        runCatching { api.discover(q) }.onSuccess { page = it; error = null }.onFailure { error = it }
    }
    LazyVerticalGrid(GridCells.Adaptive(118.dp), Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column(Modifier.statusBarsPadding(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(stringResource(R.string.discover).uppercase(), style = Display.copy(fontSize = 34.sp), color = Kino.text, modifier = Modifier.padding(top = 12.dp))
                Text(stringResource(R.string.discover_intro), color = Kino.muted, fontSize = 14.sp)
                OutlinedTextField(q, { q = it }, leadingIcon = { Icon(Icons.Outlined.Search, null) }, placeholder = { Text(stringResource(R.string.discover_hint)) },
                    singleLine = true, modifier = Modifier.fillMaxWidth(), shape = CardShape)
                ErrorLine(error)
                if (page?.configured == false) Text(stringResource(R.string.discover_not_configured), color = Kino.muted)
            }
        }
        if (q.isBlank() && !upcoming.isNullOrEmpty()) item(span = { GridItemSpan(maxLineSpan) }) {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(stringResource(R.string.coming_soon).uppercase(), style = Head, color = Kino.text)
                LazyRow(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    items(upcoming!!.take(30)) { u ->
                        Column(Modifier.width(240.dp).clip(CardShape).background(Kino.surface).padding(12.dp)) {
                            Text(u.date.take(10), color = LocalAccent.current, fontSize = 12.sp, fontWeight = FontWeight.Bold)
                            Text(u.seriesTitle ?: u.title, color = Kino.text, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text(if (u.kind == "episode") "S${u.season}:E${u.episode} · ${u.title}" else stringResource(R.string.movie), color = Kino.muted, fontSize = 13.sp, maxLines = 1)
                        }
                    }
                }
            }
        }
        page?.takeIf { it.configured }?.let { p ->
            item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(if (q.isBlank()) R.string.trending else R.string.results).uppercase(), style = Head, color = Kino.text) }
            items(p.titles, key = { "${it.mediaType}${it.tmdbId}" }) { DiscoverCard(state, it, p.canRequest, nav) }
        }
    }
}
