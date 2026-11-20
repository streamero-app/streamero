package net.scyde.streamero.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyGridScope
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
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
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
        attempt { load() }.onSuccess { value = it; error = null }.onFailure { error = it }
        loading = false
    }
    return Triple(value, loading, error)
}

@Composable
fun HomeScreen(state: AppState, nav: NavHostController) {
    val api = state.api ?: return
    val (data, loading, error) = rememberLoad(state.me?.currentProfile?.id) {
        // All rows at once instead of one after another; only "latest" is required, the rest fall back to empty.
        coroutineScope {
            val latest = async { api.latest() }
            val prefs = async { attempt { api.preferences() }.getOrNull() }
            val cont = async { attempt { api.continueWatching() }.getOrDefault(emptyList()) }
            val nextUp = async { attempt { api.nextUp() }.getOrDefault(emptyList()) }
            val favorites = async { attempt { api.favorites() }.getOrDefault(emptyList()) }
            val collections = async { attempt { api.collections() }.getOrDefault(emptyList()) }
            val libraries = async { attempt { api.libraries() }.getOrDefault(emptyList()) }
            val reading = async { attempt { api.continueReading() }.getOrDefault(emptyList()) }
            val onNow = async { if (net.scyde.streamero.livetv.canWatchLiveTv(state.me)) attempt { api.liveOnNow() }.getOrDefault(emptyList()) else emptyList() }
            HomeData(latest.await(), cont.await(), nextUp.await(), favorites.await(), collections.await(), libraries.await(), prefs.await()?.homeSections,
                reading.await(), onNow.await())
        }
    }
    val img: (String, String) -> String = api::imageUrl
    val tv = LocalTv.current
    val mem = rememberFocusMemory()
    val scope = rememberCoroutineScope()
    // "Remove from Continue watching" (an enhancement): a long press forgets the position.
    var removed by remember { mutableStateOf(setOf<String>()) }
    var removing by remember { mutableStateOf<ItemSummary?>(null) }
    var removeError by remember { mutableStateOf<Throwable?>(null) }
    removing?.let { r ->
        ConfirmDialog(stringResource(R.string.continue_remove_confirm, r.seriesTitle ?: r.title), stringResource(R.string.remove), { removing = null }) {
            scope.launch { attempt { api.progress(r.id, ProgressReport(0)) }.onSuccess { removed = removed + r.id }.onFailure { removeError = it } }
        }
    }
    LazyColumn(Modifier.fillMaxSize(), verticalArrangement = Arrangement.spacedBy(if (tv) 18.dp else 28.dp), contentPadding = PaddingValues(bottom = 24.dp)) {
        item { Stage(state, nav, data?.latest.orEmpty(), mem) }
        if (loading) item { Loading() }
        if (error != null || removeError != null) item { Box(Modifier.padding(20.dp)) { ErrorLine(error ?: removeError) } }
        val d = data ?: return@LazyColumn
        if (d.latest.isEmpty() && d.cont.isEmpty() && d.reading.isEmpty() && d.nextUp.isEmpty() && d.favorites.isEmpty() && d.collections.isEmpty()) item {
            Column(Modifier.fillMaxWidth().statusBarsPadding().padding(horizontal = 20.dp, vertical = 48.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(stringResource(R.string.home_empty_title), style = Display.copy(fontSize = 34.sp), color = Kino.text)
                Text(stringResource(if (state.isAdmin) R.string.home_empty_admin else R.string.home_empty_user), color = Kino.muted, fontSize = 15.sp)
            }
        }
        if (d.onNow.isNotEmpty()) item { OnNowShelf(state, nav, d.onNow) }
        val order = homeOrder(d.sections)
        order.forEach { s ->
            when (s) {
                "reading" -> item {
                    Shelf(stringResource(R.string.continue_reading), d.reading, key = { it.id }) { b -> BookCard(api, b, if (b.kind == "audiobook") 150.dp else Dims.poster, Modifier.rememberFocus(mem, "r" + b.id)) { nav.open("item/${b.id}") } }
                }
                "continue" -> item {
                    Shelf(stringResource(R.string.continue_watching), d.cont.filter { it.id !in removed }, key = { it.id }) {
                        WideCard(it, img, Dims.wide, Modifier.rememberFocus(mem, "c" + it.id), onLongClick = if (state.enhancements.removeFromContinue) ({ removing = it }) else null) { nav.play(it.id) }
                    }
                }
                "nextUp" -> item { Shelf(stringResource(R.string.next_up), d.nextUp, key = { it.id }) { WideCard(it, img, Dims.wide, Modifier.rememberFocus(mem, "n" + it.id)) { nav.play(it.id) } } }
                "latest" -> item { Shelf(stringResource(R.string.recently_added), d.latest, key = { it.id }) { PosterCard(it, img, Dims.poster, state.enhancements.qualityTags, Modifier.rememberFocus(mem, "l" + it.id)) { nav.open("item/${it.id}") } } }
                "favorites" -> item { Shelf(stringResource(R.string.my_list), d.favorites, key = { it.id }) { PosterCard(it, img, Dims.poster, state.enhancements.qualityTags, Modifier.rememberFocus(mem, "f" + it.id)) { nav.open("item/${it.id}") } } }
                "collections" -> item { Shelf(stringResource(R.string.collections), d.collections, key = { it.id }) { c -> CollectionTile(api, c, Dims.poster, Modifier.rememberFocus(mem, "k" + c.id)) { nav.open("collection/${c.id}") } } }
                "libraries" -> item {
                    Shelf(stringResource(R.string.libraries), d.libraries, key = { it.id }) { l -> LibraryTile(l, Modifier.rememberFocus(mem, "b" + l.id)) { nav.open("library/${l.id}") } }
                }
            }
        }
    }
}

private data class HomeData(val latest: List<ItemSummary>, val cont: List<ItemSummary>, val nextUp: List<ItemSummary>, val favorites: List<ItemSummary>,
    val collections: List<MediaCollection>, val libraries: List<Library>, val sections: List<String>?, val reading: List<ItemSummary> = emptyList(),
    val onNow: List<LiveTvChannel> = emptyList())

/** The home sections in order; books in progress come right after the films in progress. */
fun homeOrder(sections: List<String>?): List<String> {
    val order = sections ?: listOf("continue", "nextUp", "latest", "favorites", "collections", "libraries")
    if ("reading" in order) return order
    val at = order.indexOf("continue")
    // A home page set up without "continue" keeps its choice; the reading row then leads.
    return if (at >= 0) order.take(at + 1) + "reading" + order.drop(at + 1) else listOf("reading") + order
}

/** The stage: one film edge to edge with its title set large, chosen from the newest titles. */
@Composable
fun Stage(state: AppState, nav: NavHostController, pool: List<ItemSummary>, mem: FocusMemory) {
    val api = state.api ?: return
    val choices = pool.filter { it.kind == "movie" || it.kind == "series" }.distinctBy { it.id }.take(10)
    var pick by remember(choices.size) { mutableStateOf(choices.firstOrNull { it.hasBackdrop } ?: choices.firstOrNull()) }
    val p = pick ?: run { Spacer(Modifier.height(80.dp)); return }
    val accent = hexColor(p.colors?.firstOrNull())
    val scope = rememberCoroutineScope()
    val tv = LocalTv.current
    Box(Modifier.fillMaxWidth().height(if (tv) 500.dp else 560.dp)) {
        ServerImage(if (p.hasBackdrop) api.imageUrl(p.id, "backdrop") else null, Modifier.fillMaxSize())
        Box(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(Kino.bg.copy(alpha = 0.6f), Color.Transparent, Kino.bg.copy(alpha = 0.7f), Kino.bg))))
        Box(Modifier.fillMaxSize().background(Brush.radialGradient(listOf(accent.copy(alpha = 0.28f), Color.Transparent), radius = 900f, center = androidx.compose.ui.geometry.Offset(0f, 1400f))))
        if (tv) Box(Modifier.fillMaxSize().background(Brush.horizontalGradient(listOf(Kino.bg.copy(alpha = 0.85f), Color.Transparent), endX = 1400f)))
        Column(Modifier.align(Alignment.BottomStart).padding(horizontal = Dims.edge).then(if (tv) Modifier.fillMaxWidth(0.62f) else Modifier), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(p.title, style = Display.copy(fontSize = if (tv) 52.sp else 44.sp), color = Kino.text, maxLines = if (tv) 2 else 3, overflow = TextOverflow.Ellipsis)
            Text(listOfNotNull(p.rating?.let { stringResource(R.string.rating_value, it) }, p.year?.toString(), p.durationMs?.let { stringResource(R.string.minutes_short, it / 60000) }).joinToString("  ·  "), color = Kino.muted)
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                if (p.kind == "movie") PrimaryButton(stringResource(R.string.play), { nav.play(p.id) }, Modifier.rememberFocus(mem, "stage-play", default = true), icon = Icons.Default.PlayArrow)
                GlassButton(stringResource(R.string.more_info), { nav.open("item/${p.id}") }, Modifier.rememberFocus(mem, "stage-info", default = p.kind != "movie"), icon = Icons.Outlined.Info)
                if (state.enhancements.randomButton) GlassButton(null, {
                    scope.launch {
                        val libs = attempt { api.libraries() }.getOrDefault(emptyList()).filter { it.kind in listOf("movies", "shows", "mixed") }.shuffled()
                        for (l in libs) {
                            val it = attempt { api.items(l.id, mapOf("sort" to "random", "unplayed" to true, "limit" to 1)).items.firstOrNull() }.getOrNull()
                            if (it != null) { nav.open("item/${it.id}"); break }
                        }
                    }
                }, Modifier.width(52.dp), icon = Icons.Default.Shuffle, description = stringResource(R.string.random_title))
            }
            LazyRow(horizontalArrangement = Arrangement.spacedBy(10.dp), contentPadding = PaddingValues(vertical = if (tv) 12.dp else 8.dp, horizontal = if (tv) 6.dp else 0.dp)) {
                items(choices, key = { it.id }) { c ->
                    // On a TV, moving the focus over the small posters changes the film on stage.
                    Box(Modifier.width(if (tv) 64.dp else 78.dp).onFocusChanged { f -> if (f.isFocused) pick = c }.focusClickable(scale = 1.1f) { if (tv) nav.open("item/${c.id}") else pick = c }
                        .aspectRatio(2f / 3f).clip(CardShape).background(Kino.surface2)
                        .then(if (c.id == p.id) Modifier.background(Kino.text) else Modifier)) {
                        ServerImage(if (c.hasPoster) api.imageUrl(c.id, "poster") else if (c.hasBackdrop) api.imageUrl(c.id, "backdrop") else null,
                            Modifier.fillMaxSize().padding(if (c.id == p.id) 2.dp else 0.dp).clip(CardShape))
                    }
                }
            }
        }
    }
}

@Composable
fun LibraryTile(l: Library, modifier: Modifier = Modifier, onClick: () -> Unit) {
    Box(modifier.width(220.dp).heightIn(min = 110.dp).focusClickable(onClick = onClick).clip(RoundedCornerShape(6.dp)).background(Kino.surface2).padding(16.dp)) {
        Column(Modifier.align(Alignment.BottomStart)) {
            Text(l.name, style = Display.copy(fontSize = 24.sp), color = Kino.text)
            Text(itemsCount(l.itemCount), color = Kino.muted, fontSize = 13.sp)
        }
    }
}

@Composable
fun CollectionTile(api: Api, c: MediaCollection, width: androidx.compose.ui.unit.Dp, modifier: Modifier = Modifier, onClick: () -> Unit) {
    val (src, focused) = rememberFocusSource()
    Column(modifier.width(width).clickable(interactionSource = src, indication = tapIndication(), onClick = onClick)) {
        Box(Modifier.fillMaxWidth().focusRing(focused).aspectRatio(2f / 3f).clip(CardShape).background(Kino.surface2)) {
            if (c.hasPoster) ServerImage(api.collectionImageUrl(c.id, "poster"), Modifier.fillMaxSize())
            else Text(c.name, style = Display.copy(fontSize = 18.sp), color = Kino.text, modifier = Modifier.align(Alignment.BottomStart).padding(10.dp))
        }
        Spacer(Modifier.height(8.dp))
        Text(c.name, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(itemsCount(c.itemCount), color = Kino.faint, fontSize = 13.sp)
    }
}

@Composable
fun PageTitle(text: String, modifier: Modifier = Modifier) {
    Text(text, style = Display.copy(fontSize = 34.sp), color = Kino.text, modifier = modifier.statusBarsPadding().padding(start = Dims.edge, end = Dims.edge, top = if (LocalTv.current) 28.dp else 20.dp, bottom = 8.dp))
}

@Composable
fun LibrariesScreen(state: AppState, nav: NavHostController) {
    val api = state.api ?: return
    val (libs, loading, error) = rememberLoad(Unit) { api.libraries() }
    val mem = rememberFocusMemory()
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 20.dp)) {
        item { PageTitle(stringResource(R.string.libraries)) }
        if (loading) item { Loading() }
        item { Box(Modifier.padding(horizontal = 20.dp)) { ErrorLine(error) } }
        items(libs.orEmpty(), key = { it.id }) { l ->
            Box(Modifier.padding(horizontal = Dims.edge, vertical = 6.dp)) {
                Box(Modifier.fillMaxWidth().heightIn(min = 110.dp).rememberFocus(mem, l.id, default = l.id == libs?.firstOrNull()?.id)
                    .focusClickable(scale = 1.02f) { nav.open("library/${l.id}") }.clip(RoundedCornerShape(6.dp)).background(Kino.surface2).padding(18.dp)) {
                    Column(Modifier.align(Alignment.BottomStart)) {
                        Text(l.name, style = Display.copy(fontSize = 28.sp), color = Kino.text)
                        Text(itemsCount(l.itemCount), color = Kino.muted, fontSize = 13.sp)
                    }
                }
            }
        }
        item {
            Row(Modifier.padding(Dims.edge, 20.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                GlassButton(stringResource(R.string.collections), { nav.open("collections") })
                GlassButton(stringResource(R.string.my_list), { nav.open("library/favorites") })
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
    when (lib?.kind) {
        "music" -> { MusicLibraryScreen(state, nav, lib); return }
        "books", "audiobooks" -> { BooksLibraryScreen(state, nav, lib); return }
        "photos" -> { PhotosLibraryScreen(state, nav, lib); return }
    }
    var sort by rememberSaveable { mutableStateOf("added") }
    var genre by rememberSaveable { mutableStateOf("") }
    var status by rememberSaveable { mutableStateOf("") }
    val (genres) = rememberLoad(libraryId) { api.genres(libraryId) }
    val paged = rememberPaged(listOf(libraryId, sort, genre, status)) { offset ->
        api.items(libraryId, mapOf("sort" to sort, "genre" to genre.ifBlank { null }, "limit" to PAGE_SIZE, "offset" to offset,
            "unplayed" to (status == "unplayed").takeIf { it }, "resumable" to (status == "resumable").takeIf { it }, "favorite" to (status == "favorite").takeIf { it }))
    }
    val items = paged.list.items
    val total = paged.list.total
    val mem = rememberFocusMemory()
    LazyVerticalGrid(GridCells.Adaptive(Dims.gridCell), Modifier.fillMaxSize(), contentPadding = gridPadding(),
        horizontalArrangement = Arrangement.spacedBy(gridGap()), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text((lib?.name ?: ""), style = Display.copy(fontSize = 34.sp), color = Kino.text, modifier = Modifier.statusBarsPadding().padding(top = 12.dp))
                if (total >= 0) Text(itemsCount(total), color = Kino.muted, fontSize = 13.sp)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Picker(sort, listOf("added" to stringResource(R.string.sort_added), "title" to stringResource(R.string.sort_title),
                        "year" to stringResource(R.string.sort_year), "rating" to stringResource(R.string.sort_rating), "random" to stringResource(R.string.sort_random)), { sort = it }, Modifier.weight(1f))
                    Picker(status, STATUSES.map { it to stringResource(when (it) { "unplayed" -> R.string.status_unplayed; "resumable" -> R.string.status_resumable; "favorite" -> R.string.my_list; else -> R.string.status_all }) },
                        { status = it }, Modifier.weight(1f))
                }
                if (!genres.isNullOrEmpty()) Picker(genre, listOf("" to stringResource(R.string.all_genres)) + genres.map { it to it }, { genre = it })
                if (items.isEmpty()) ErrorLine(paged.error)
            }
        }
        items(items, key = { it.id }, contentType = { "poster" }) { PosterCard(it, api::imageUrl, Dims.gridCell, state.enhancements.qualityTags, Modifier.rememberFocus(mem, it.id, default = it.id == items.firstOrNull()?.id)) { nav.open("item/${it.id}") } }
        pagingFooter(paged)
    }
}

/** How many titles a paged grid asks for at a time. */
const val PAGE_SIZE = 60

/** A grid that loads its titles page by page; [error] is the last failure (first page or later). */
@Stable
class PagedItems {
    var list by mutableStateOf(Paging())
    var loading by mutableStateOf(true)
    var error by mutableStateOf<Throwable?>(null)
    internal var more: () -> Unit = {}

    /** Asks for the next page, unless one is on its way or there is none. */
    fun loadMore() = more()
}

/**
 * Loads the first page again whenever [key] changes and later pages on [PagedItems.loadMore].
 * A page that arrives after the key changed belongs to the old list and is dropped with it.
 */
@Composable
fun rememberPaged(key: Any?, fetch: suspend (offset: Int) -> ItemPage): PagedItems {
    val paged = remember(key) { PagedItems() }
    val currentFetch by rememberUpdatedState(fetch)
    val scope = rememberCoroutineScope()
    LaunchedEffect(paged) {
        paged.loading = true
        attempt { currentFetch(0) }.onSuccess { paged.list = paged.list.next(it, reset = true); paged.error = null }.onFailure { paged.error = it }
        paged.loading = false
    }
    paged.more = {
        if (!paged.loading && paged.list.hasMore) {
            paged.loading = true
            scope.launch {
                try {
                    attempt { currentFetch(paged.list.offset) }.onSuccess { paged.list = paged.list.next(it, reset = false); paged.error = null }.onFailure { paged.error = it }
                } finally {
                    paged.loading = false
                }
            }
        }
    }
    return paged
}

/** The end of a paged grid: loads the next page when it scrolls into view, a retry after a failure, or "nothing here". */
fun LazyGridScope.pagingFooter(paged: PagedItems) {
    val list = paged.list
    if (list.hasMore) item(key = "paging-footer", span = { GridItemSpan(maxLineSpan) }, contentType = "footer") {
        if (paged.error != null && !paged.loading) {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                ErrorLine(paged.error)
                GlassButton(stringResource(R.string.retry), { paged.loadMore() })
            }
        } else {
            LaunchedEffect(list.offset) { paged.loadMore() }
            Loading()
        }
    } else if (paged.loading && list.total < 0) item(key = "paging-loading", span = { GridItemSpan(maxLineSpan) }, contentType = "footer") { Loading() }
    if (list.total == 0) item(key = "paging-empty", span = { GridItemSpan(maxLineSpan) }, contentType = "footer") { Text(stringResource(R.string.nothing_here), color = Kino.muted) }
}

@Composable
fun FavoritesScreen(state: AppState, nav: NavHostController) {
    val api = state.api ?: return
    val (favs, loading, error) = rememberLoad(Unit) { api.favorites() }
    val mem = rememberFocusMemory()
    LazyVerticalGrid(GridCells.Adaptive(Dims.gridCell), Modifier.fillMaxSize(), contentPadding = gridPadding(),
        horizontalArrangement = Arrangement.spacedBy(gridGap()), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(R.string.my_list), style = Display.copy(fontSize = 34.sp), color = Kino.text, modifier = Modifier.statusBarsPadding().padding(top = 12.dp)) }
        if (loading) item(span = { GridItemSpan(maxLineSpan) }) { Loading() }
        item(span = { GridItemSpan(maxLineSpan) }) { ErrorLine(error) }
        items(favs.orEmpty().distinctBy { it.id }, key = { it.id }, contentType = { "poster" }) { PosterCard(it, api::imageUrl, Dims.gridCell, state.enhancements.qualityTags, Modifier.rememberFocus(mem, it.id, default = it.id == favs?.firstOrNull()?.id)) { nav.open("item/${it.id}") } }
    }
}

@Composable
fun SearchScreen(state: AppState, nav: NavHostController) {
    val api = state.api ?: return
    if (LocalTv.current) { TvSearchScreen(state, nav); return }
    var q by rememberSaveable { mutableStateOf("") }
    var results by remember { mutableStateOf<List<ItemSummary>?>(null) }
    var elsewhere by remember { mutableStateOf<DiscoverPage?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    LaunchedEffect(q) {
        if (q.isBlank()) { results = null; elsewhere = null; error = null; return@LaunchedEffect }
        delay(300)
        // Typing on cancels this; only a real failure is shown (as an error, not as "nothing found").
        attempt { api.search(q.trim()) }.onSuccess { results = it.distinctBy { r -> r.id }; error = null }.onFailure { results = null; error = it }
        elsewhere = if (state.enhancements.discoverInSearch) attempt { api.discover(q.trim()) }.getOrNull() else null
    }
    LazyVerticalGrid(GridCells.Adaptive(118.dp), Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            OutlinedTextField(q, { q = it }, leadingIcon = { Icon(Icons.Outlined.Search, null) }, placeholder = { Text(stringResource(R.string.search_hint)) },
                singleLine = true, modifier = Modifier.fillMaxWidth().statusBarsPadding().padding(top = 8.dp), shape = CardShape)
        }
        if (error != null) item(span = { GridItemSpan(maxLineSpan) }) { ErrorLine(error) }
        items(results.orEmpty(), key = { it.id }, contentType = { "poster" }) { PosterCard(it, api::imageUrl, 118.dp, state.enhancements.qualityTags) { nav.open("item/${it.id}") } }
        if (results?.isEmpty() == true) item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(R.string.nothing_found), color = Kino.muted) }
        val other = elsewhere?.takeIf { it.configured }
        val missing = other?.titles?.filter { it.itemId == null }?.distinctBy { "${it.mediaType}${it.tmdbId}" }.orEmpty()
        if (other != null && missing.isNotEmpty()) {
            item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(R.string.not_here_yet).uppercase(), style = Head, color = Kino.text, modifier = Modifier.padding(top = 12.dp)) }
            items(missing.take(12), key = { "d${it.mediaType}${it.tmdbId}" }) { DiscoverCard(state, it, other.canRequest, nav) }
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
    Column(Modifier.width(Dims.gridCell)) {
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
            t.itemId != null -> GlassButton(stringResource(R.string.watch), { nav.open("item/${t.itemId}") }, Modifier.fillMaxWidth().heightIn(max = 40.dp))
            status == "none" && canRequest -> PrimaryButton(stringResource(R.string.request), enabled = !busy, modifier = Modifier.fillMaxWidth().heightIn(max = 40.dp), onClick = {
                busy = true
                error = null
                scope.launch { attempt { api.request(t) }.onSuccess { status = it.status }.onFailure { error = it }; busy = false }
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
    var q by rememberSaveable { mutableStateOf("") }
    var page by remember { mutableStateOf<DiscoverPage?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    val (upcoming) = rememberLoad(Unit) { attempt { api.upcoming() }.getOrDefault(emptyList()) }
    LaunchedEffect(q) {
        delay(if (q.isBlank()) 0 else 350)
        attempt { api.discover(q.trim()) }.onSuccess { page = it; error = null }.onFailure { error = it }
    }
    LazyVerticalGrid(GridCells.Adaptive(Dims.gridCell), Modifier.fillMaxSize(), contentPadding = gridPadding(),
        horizontalArrangement = Arrangement.spacedBy(gridGap()), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column(Modifier.statusBarsPadding(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(stringResource(R.string.discover), style = Display.copy(fontSize = 34.sp), color = Kino.text, modifier = Modifier.padding(top = 12.dp))
                Text(stringResource(R.string.discover_intro), color = Kino.muted, fontSize = 14.sp)
                OutlinedTextField(q, { q = it }, leadingIcon = { Icon(Icons.Outlined.Search, null) }, placeholder = { Text(stringResource(R.string.discover_hint)) },
                    singleLine = true, modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
                ErrorLine(error)
                if (page?.configured == false) Text(stringResource(R.string.discover_not_configured), color = Kino.muted)
            }
        }
        if (q.isBlank() && !upcoming.isNullOrEmpty()) item(span = { GridItemSpan(maxLineSpan) }) {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Text(stringResource(R.string.coming_soon).uppercase(), style = Head, color = Kino.text)
                LazyRow(horizontalArrangement = Arrangement.spacedBy(10.dp), contentPadding = PaddingValues(vertical = if (LocalTv.current) 10.dp else 0.dp, horizontal = if (LocalTv.current) 6.dp else 0.dp)) {
                    items(upcoming.orEmpty().take(30), contentType = { "upcoming" }) { u ->
                        // Focusable on a TV only so the row can be scrolled with the remote.
                        Column(Modifier.width(240.dp).then(if (LocalTv.current) Modifier.focusClickable { } else Modifier).clip(CardShape).background(Kino.surface).padding(12.dp)) {
                            Text(u.date.take(10), color = LocalAccent.current, fontSize = 12.sp, fontWeight = FontWeight.Bold)
                            Text(u.seriesTitle ?: u.title, color = Kino.text, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text(if (u.kind == "episode") stringResource(R.string.episode_code_title, u.season ?: 0, u.episode ?: 0, u.title) else stringResource(R.string.movie), color = Kino.muted, fontSize = 13.sp, maxLines = 1)
                        }
                    }
                }
            }
        }
        page?.takeIf { it.configured }?.let { p ->
            item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(if (q.isBlank()) R.string.trending else R.string.results).uppercase(), style = Head, color = Kino.text) }
            items(p.titles.distinctBy { "${it.mediaType}${it.tmdbId}" }, key = { "${it.mediaType}${it.tmdbId}" }, contentType = { "discover" }) { DiscoverCard(state, it, p.canRequest, nav) }
        }
    }
}

/** Grid padding: room for overscan and for a focused card to grow on a TV. */
@Composable
fun gridPadding(): PaddingValues = if (LocalTv.current) PaddingValues(horizontal = 40.dp, vertical = 20.dp) else PaddingValues(16.dp)

@Composable
fun gridGap() = if (LocalTv.current) 20.dp else 12.dp
