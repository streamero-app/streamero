package net.scyde.streamero.ui

import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.MenuBook
import androidx.compose.material.icons.filled.Headphones
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.*
import net.scyde.streamero.books.COMIC_FORMATS
import net.scyde.streamero.books.formatIndex
import net.scyde.streamero.books.readableInApp

/** Opens the titles of one author or series of a library. */
fun NavHostController.shelf(libraryId: String, author: String? = null, series: String? = null) {
    val q = listOfNotNull(author?.let { "author=" + Uri.encode(it) }, series?.let { "series=" + Uri.encode(it) }).joinToString("&")
    navigate("shelf/$libraryId?$q")
}

private fun seriesLine(name: String?, index: Double?): String? = name?.let { n -> if (index != null) "$n #${formatIndex(index)}" else n }

/** A book (2:3) or audiobook (square) cover, the title set on it when there is no image. */
@Composable
fun BookCover(api: Api, item: ItemSummary, modifier: Modifier = Modifier, coverId: String? = null) {
    val square = item.kind == "audiobook"
    Box(modifier.aspectRatio(if (square) 1f else 2f / 3f).clip(CardShape)
        .background(Brush.verticalGradient(listOf(Kino.surface3, Kino.surface2)))) {
        Column(Modifier.align(Alignment.BottomStart).padding(10.dp)) {
            Icon(if (square) Icons.Default.Headphones else Icons.AutoMirrored.Filled.MenuBook, null, tint = Kino.faint, modifier = Modifier.size(20.dp))
            Text(item.title, style = Display.copy(fontSize = 16.sp), color = Kino.text, maxLines = 4, overflow = TextOverflow.Ellipsis)
        }
        if (item.hasPoster || coverId != null) ServerImage(api.imageUrl(coverId ?: item.id, "poster"), Modifier.fillMaxSize())
        ProgressLine(item.progress, Modifier.align(Alignment.BottomCenter))
    }
}

@Composable
fun BookCard(api: Api, item: ItemSummary, width: Dp? = null, modifier: Modifier = Modifier, onClick: () -> Unit) {
    val (src, focused) = rememberFocusSource()
    Column((if (width != null) modifier.width(width) else modifier).clickable(interactionSource = src, indication = tapIndication(), onClick = onClick)) {
        BookCover(api, item, Modifier.fillMaxWidth().focusRing(focused))
        Spacer(Modifier.height(8.dp))
        Text(item.title, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        val sub = item.authors?.joinToString(", ")?.ifBlank { null } ?: seriesLine(item.seriesName, item.seriesIndex)
        if (sub != null) Text(sub, color = Kino.faint, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** Books or audiobooks: every title, or the authors and series to choose from. */
@Composable
fun BooksLibraryScreen(state: AppState, nav: NavHostController, lib: Library) {
    var tab by rememberSaveableString("all")
    when (tab) {
        "all" -> BookGrid(state, nav, lib, null, null) { BooksHeader(lib.name, tab) { tab = it } }
        else -> NameGrid(state, nav, lib, tab) { BooksHeader(lib.name, tab) { tab = it } }
    }
}

@Composable
private fun rememberSaveableString(initial: String) = androidx.compose.runtime.saveable.rememberSaveable { mutableStateOf(initial) }

@Composable
private fun BooksHeader(title: String, tab: String, onTab: (String) -> Unit) {
    Column(Modifier.statusBarsPadding().padding(top = 12.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Text(title, style = Display.copy(fontSize = 34.sp), color = Kino.text)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            listOf("all" to R.string.books_all, "authors" to R.string.books_authors, "series" to R.string.books_series).forEach { (k, l) ->
                GlassButton(stringResource(l), { onTab(k) }, active = tab == k)
            }
        }
    }
}

/** A paged grid of a library's books or audiobooks, optionally of one author or series. */
@Composable
fun BookGrid(state: AppState, nav: NavHostController, lib: Library, author: String?, series: String?, header: @Composable () -> Unit) {
    val api = state.api ?: return
    val kind = if (lib.kind == "audiobooks") "audiobook" else "book"
    var sort by remember { mutableStateOf(if (series != null) "series" else "title") }
    var items by remember { mutableStateOf<List<ItemSummary>>(emptyList()) }
    var total by remember { mutableIntStateOf(-1) }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    suspend fun load(reset: Boolean) {
        loading = true
        runCatching {
            api.items(lib.id, mapOf("kind" to kind, "sort" to sort, "author" to author, "series" to series, "limit" to 60, "offset" to if (reset) 0 else items.size))
        }.onSuccess { p -> items = if (reset) p.items else items + p.items; total = p.total; error = null }.onFailure { error = it }
        loading = false
    }
    LaunchedEffect(sort) { load(true) }
    val cell = if (kind == "audiobook") 140.dp else Dims.gridCell
    LazyVerticalGrid(GridCells.Adaptive(cell), Modifier.fillMaxSize(), contentPadding = gridPadding(),
        horizontalArrangement = Arrangement.spacedBy(gridGap()), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                header()
                if (total >= 0) Text(itemsCount(total), color = Kino.muted, fontSize = 13.sp)
                Picker(sort, listOf("title" to stringResource(R.string.sort_title), "added" to stringResource(R.string.sort_added),
                    "series" to stringResource(R.string.sort_series)), { sort = it })
                ErrorLine(error)
            }
        }
        items(items, key = { it.id }) { BookCard(api, it) { nav.navigate("item/${it.id}") } }
        if (total > items.size) item(span = { GridItemSpan(maxLineSpan) }) {
            LaunchedEffect(items.size) { if (!loading) load(false) }
            Loading()
        }
        if (total == 0) item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(R.string.nothing_here), color = Kino.muted) }
    }
}

/** Authors or series of a library, each with a cover borrowed from one of its titles. */
@Composable
private fun NameGrid(state: AppState, nav: NavHostController, lib: Library, which: String, header: @Composable () -> Unit) {
    val api = state.api ?: return
    val (names, loading, error) = rememberLoad(lib.id + which) { if (which == "authors") api.authors(lib.id) else api.series(lib.id) }
    val square = lib.kind == "audiobooks"
    LazyVerticalGrid(GridCells.Adaptive(if (square) 140.dp else Dims.gridCell), Modifier.fillMaxSize(), contentPadding = gridPadding(),
        horizontalArrangement = Arrangement.spacedBy(gridGap()), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) { Column(verticalArrangement = Arrangement.spacedBy(10.dp)) { header(); ErrorLine(error) } }
        if (loading) item(span = { GridItemSpan(maxLineSpan) }) { Loading() }
        items(names.orEmpty(), key = { it.name }) { n ->
            val stub = ItemSummary(id = n.itemId ?: n.name, kind = if (square) "audiobook" else "book", title = n.name, hasPoster = n.itemId != null)
            Column(Modifier.clickable { if (which == "authors") nav.shelf(lib.id, author = n.name) else nav.shelf(lib.id, series = n.name) }) {
                BookCover(api, stub, Modifier.fillMaxWidth())
                Spacer(Modifier.height(8.dp))
                Text(n.name, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, maxLines = 2, overflow = TextOverflow.Ellipsis)
                Text(itemsCount(n.count), color = Kino.faint, fontSize = 13.sp)
            }
        }
        if (names?.isEmpty() == true) item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(R.string.nothing_here), color = Kino.muted) }
    }
}

/** The titles of one author or one series. */
@Composable
fun ShelfScreen(state: AppState, nav: NavHostController, libraryId: String, author: String?, series: String?) {
    val api = state.api ?: return
    val (libs) = rememberLoad(Unit) { api.libraries() }
    val lib = libs?.firstOrNull { it.id == libraryId } ?: run { Loading(Modifier.fillMaxSize()); return }
    BookGrid(state, nav, lib, author, series) {
        Column(Modifier.statusBarsPadding().padding(top = 12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(stringResource(if (author != null) R.string.books_author else R.string.books_series_one).uppercase(), style = Label, color = Kino.faint)
            Text(author ?: series.orEmpty(), style = Display.copy(fontSize = 34.sp), color = Kino.text)
        }
    }
}

/** A book, comic or audiobook: cover, who wrote it, where it is in its series, and read or listen. */
@Composable
fun BookScreen(state: AppState, nav: NavHostController, d: ItemDetail) {
    val api = state.api ?: return
    val it = d.item
    val b = d.book
    val audio = it.kind == "audiobook"
    val format = (b?.format ?: it.bookFormat)?.lowercase()
    val authors = b?.authors?.takeIf { a -> a.isNotEmpty() } ?: it.authors.orEmpty()
    val seriesName = b?.seriesName ?: it.seriesName
    val seriesIndex = b?.seriesIndex ?: it.seriesIndex
    val listen = if (audio) rememberLoad(it.id) { api.listen(it.id) }.first else null
    val accent = hexColor(it.colors?.firstOrNull())
    val pct = (it.progress * 100).toInt()
    CompositionLocalProvider(LocalAccent provides accent) {
        LazyColumn(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(accent.copy(alpha = 0.22f), Kino.bg, Kino.bg))),
            contentPadding = PaddingValues(start = Dims.edge, end = Dims.edge, bottom = 30.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            item {
                Column(Modifier.fillMaxWidth().statusBarsPadding().padding(top = 16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    BookCover(api, it, Modifier.fillMaxWidth(if (audio) 0.7f else 0.55f).align(Alignment.CenterHorizontally))
                    Spacer(Modifier.height(6.dp))
                    Text(stringResource(when { audio -> R.string.audiobook; format in COMIC_FORMATS -> R.string.comic; else -> R.string.book }).uppercase(), style = Label, color = Kino.faint)
                    Text(it.title, style = Display.copy(fontSize = 32.sp), color = Kino.text)
                    if (authors.isNotEmpty()) Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        authors.forEach { a -> Text(a, color = Kino.text, fontWeight = FontWeight.SemiBold, modifier = Modifier.clickable { nav.shelf(d.libraryId, author = a) }) }
                    }
                    seriesLine(seriesName, seriesIndex)?.let { s ->
                        Text(s, color = LocalAccent.current, fontWeight = FontWeight.SemiBold, modifier = Modifier.clickable { nav.shelf(d.libraryId, series = seriesName) })
                    }
                    val facts = listOfNotNull(format?.uppercase(), b?.pageCount?.let { n -> stringResource(R.string.pages_count, n) },
                        it.durationMs?.takeIf { audio }?.let { ms -> clock(ms) }, b?.published?.take(4), it.year?.toString()?.takeIf { b?.published == null })
                    if (facts.isNotEmpty()) Text(facts.joinToString("  ·  "), color = Kino.muted, fontSize = 14.sp)
                }
            }
            item {
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    if (pct > 0) {
                        ProgressLine(it.progress)
                        Text(stringResource(if (audio) R.string.listened_percent else R.string.read_percent, pct), color = Kino.muted, fontSize = 13.sp)
                    }
                    when {
                        audio -> {
                            val label = if (it.positionMs > 0) stringResource(R.string.resume_from, clock(it.positionMs)) else stringResource(R.string.listen)
                            PrimaryButton(label, { state.book.play(it); nav.navigate("listen/${it.id}") }, Modifier.fillMaxWidth(), icon = Icons.Default.Headphones)
                        }
                        readableInApp(format) -> {
                            val label = if (pct in 1..99) stringResource(R.string.continue_reading_at, pct) else stringResource(R.string.read)
                            PrimaryButton(label, { nav.navigate("read/${it.id}") }, Modifier.fillMaxWidth(), icon = Icons.AutoMirrored.Filled.MenuBook)
                        }
                        else -> Text(stringResource(R.string.read_in_web), color = Kino.muted, fontSize = 14.sp,
                            modifier = Modifier.fillMaxWidth().clip(CardShape).background(Kino.surface2).padding(14.dp))
                    }
                }
            }
            (b?.description ?: d.overview)?.takeIf { s -> s.isNotBlank() }?.let { desc ->
                item { Text(desc, color = Kino.text.copy(alpha = 0.85f), fontSize = 16.sp, lineHeight = 24.sp) }
            }
            item {
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Fact(stringResource(R.string.narrated_by), b?.narrators.orEmpty().joinToString())
                    Fact(stringResource(R.string.publisher), b?.publisher.orEmpty())
                    Fact(stringResource(R.string.released), b?.published.orEmpty())
                    Fact(stringResource(R.string.language), b?.language.orEmpty())
                    Fact("ISBN", b?.isbn.orEmpty())
                    Fact(stringResource(R.string.genres), d.genres.joinToString())
                }
            }
            val chapters = listen?.chapters.orEmpty()
            if (chapters.size > 1) {
                item { Text(stringResource(R.string.chapters).uppercase(), style = Head, color = Kino.text, modifier = Modifier.padding(top = 6.dp)) }
                itemsIndexed(chapters) { i, c ->
                    Row(Modifier.fillMaxWidth().clickable { state.book.play(it, c.startMs); nav.navigate("listen/${it.id}") }.padding(vertical = 10.dp),
                        horizontalArrangement = Arrangement.spacedBy(14.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text("${i + 1}", color = Kino.faint, fontSize = 14.sp, modifier = Modifier.width(26.dp))
                        Text(c.title, color = Kino.text, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(clock(c.startMs), color = Kino.faint, fontSize = 13.sp)
                        Icon(Icons.Default.PlayArrow, null, tint = Kino.faint, modifier = Modifier.size(18.dp))
                    }
                }
            }
        }
    }
}
