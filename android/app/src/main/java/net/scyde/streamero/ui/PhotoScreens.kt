package net.scyde.streamero.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.*
import net.scyde.streamero.books.exposureLine
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

/** "1 May 2024, 14:03" in the phone's language and time zone; the raw value when it does not parse. */
fun formatTaken(iso: String?): String? = iso?.let {
    runCatching { OffsetDateTime.parse(it).atZoneSameInstant(ZoneId.systemDefault()).format(DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT)) }
        .getOrDefault(it)
}

/** A photo library: its folders as albums, or every photo newest first. */
@Composable
fun PhotosLibraryScreen(state: AppState, nav: NavHostController, lib: Library) {
    var tab by androidx.compose.runtime.saveable.rememberSaveable { mutableStateOf("albums") }
    val query = if (tab == "albums") mapOf("sort" to "title") else mapOf("kind" to "photo", "sort" to "taken")
    PhotoGrid(state, nav, lib.id, query) {
        Column(Modifier.statusBarsPadding().padding(top = 12.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(lib.name, style = Display.copy(fontSize = 34.sp), color = Kino.text)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                listOf("albums" to R.string.photo_albums, "all" to R.string.photos_all).forEach { (k, l) -> GlassButton(stringResource(l), { tab = k }, active = tab == k) }
            }
        }
    }
}

/** An album: its sub-albums first, then the photos and videos in it. */
@Composable
fun PhotoAlbumScreen(state: AppState, nav: NavHostController, d: ItemDetail) {
    PhotoGrid(state, nav, d.libraryId, mapOf("parentId" to d.item.id, "sort" to "taken")) {
        Column(Modifier.statusBarsPadding().padding(top = 12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(stringResource(R.string.photo_album).uppercase(), style = Label, color = Kino.faint)
            Text(d.item.title, style = Display.copy(fontSize = 32.sp), color = Kino.text)
        }
    }
}

/** A paged grid of albums, photos and videos (square thumbnails). */
@Composable
private fun PhotoGrid(state: AppState, nav: NavHostController, libraryId: String, query: Map<String, Any?>, header: @Composable () -> Unit) {
    val api = state.api ?: return
    var items by remember(query) { mutableStateOf<List<ItemSummary>>(emptyList()) }
    var total by remember(query) { mutableIntStateOf(-1) }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    suspend fun load(reset: Boolean) {
        loading = true
        runCatching { api.items(libraryId, query + mapOf("limit" to 100, "offset" to if (reset) 0 else items.size)) }
            .onSuccess { p -> items = if (reset) p.items else items + p.items; total = p.total; error = null }.onFailure { error = it }
        loading = false
    }
    LaunchedEffect(query) { load(true) }
    val albums = items.filter { it.kind == "photoalbum" }
    val media = items.filter { it.kind != "photoalbum" }
    fun open(it: ItemSummary) {
        when (it.kind) {
            "photoalbum" -> nav.navigate("item/${it.id}")
            "video" -> nav.watch(it.id)
            else -> { state.photoStrip = media.filter { m -> m.kind == "photo" }; nav.navigate("photo/${it.id}") }
        }
    }
    LazyVerticalGrid(GridCells.Adaptive(110.dp), Modifier.fillMaxSize(), contentPadding = gridPadding(),
        horizontalArrangement = Arrangement.spacedBy(4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) { Column(Modifier.padding(bottom = 10.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) { header(); ErrorLine(error) } }
        items(albums, key = { it.id }, span = { GridItemSpan(if (maxLineSpan >= 2) 2 else 1) }) { a -> AlbumTile(api, a) { open(a) } }
        items(media, key = { it.id }) { m -> PhotoThumb(api, m) { open(m) } }
        if (total > items.size) item(span = { GridItemSpan(maxLineSpan) }) {
            LaunchedEffect(items.size) { if (!loading) load(false) }
            Loading()
        }
        if (total == 0) item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(R.string.nothing_here), color = Kino.muted) }
    }
}

@Composable
private fun AlbumTile(api: Api, a: ItemSummary, onClick: () -> Unit) {
    Column(Modifier.padding(bottom = 10.dp, end = 4.dp).clickable(onClick = onClick)) {
        Box(Modifier.fillMaxWidth().aspectRatio(1f).clip(CardShape).background(Kino.surface2), contentAlignment = Alignment.Center) {
            Icon(Icons.Default.Folder, null, tint = Kino.faint, modifier = Modifier.size(36.dp))
            if (a.hasPoster) ServerImage(api.imageUrl(a.id, "poster"), Modifier.fillMaxSize())
        }
        Spacer(Modifier.height(6.dp))
        Text(a.title, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        a.childCount?.let { Text(itemsCount(it), color = Kino.faint, fontSize = 12.sp) }
    }
}

@Composable
private fun PhotoThumb(api: Api, m: ItemSummary, onClick: () -> Unit) {
    Box(Modifier.aspectRatio(1f).clip(androidx.compose.foundation.shape.RoundedCornerShape(2.dp)).background(Kino.surface2).clickable(onClick = onClick)) {
        if (m.hasPoster) ServerImage(api.imageUrl(m.id, "poster"), Modifier.fillMaxSize())
        if (m.kind == "video") Row(Modifier.align(Alignment.BottomStart).padding(5.dp), verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Default.PlayArrow, null, tint = Color.White, modifier = Modifier.size(16.dp))
            m.durationMs?.let { Text(clock(it), color = Color.White, fontSize = 11.sp, fontWeight = FontWeight.SemiBold) }
        }
    }
}

/** Full-screen photos: swipe through the album, pinch to zoom, details in a sheet. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PhotoViewerScreen(state: AppState, nav: NavHostController, id: String) {
    val api = state.api ?: return
    val strip = remember { state.photoStrip.takeIf { s -> s.any { it.id == id } } }
    val (single) = rememberLoad(id) { if (strip == null) api.item(id).item else null }
    val photos = strip ?: listOfNotNull(single)
    Immersive()
    Box(Modifier.fillMaxSize().background(Color.Black)) {
        if (photos.isEmpty()) { Loading(Modifier.fillMaxSize()); return@Box }
        val pager = rememberPagerState(initialPage = photos.indexOfFirst { it.id == id }.coerceAtLeast(0)) { photos.size }
        var chrome by remember { mutableStateOf(true) }
        var zoomed by remember { mutableStateOf(false) }
        var info by remember { mutableStateOf(false) }
        HorizontalPager(pager, Modifier.fillMaxSize(), userScrollEnabled = !zoomed, beyondViewportPageCount = 1, key = { photos[it].id }) { page ->
            val p = photos[page]
            Zoomable(active = page == pager.currentPage, onZoomed = { if (page == pager.currentPage) zoomed = it }, onTap = { _, _ -> chrome = !chrome }) {
                // The thumbnail first, the full picture (HEIC converted by the server) over it.
                if (p.hasPoster) ServerImage(api.imageUrl(p.id, "poster"), Modifier.fillMaxSize(), contentScale = ContentScale.Fit)
                val (view) = rememberLoad(p.id) { if (p.kind == "photo") api.photo(p.id) else null }
                view?.let { v -> ServerImage(api.resolve(v.url), Modifier.fillMaxSize(), contentScale = ContentScale.Fit) }
                if (p.kind == "video") FilledIconButton(onClick = { nav.watch(p.id) }, Modifier.size(72.dp),
                    colors = IconButtonDefaults.filledIconButtonColors(containerColor = Kino.text, contentColor = Kino.bg)) {
                    Icon(Icons.Default.PlayArrow, stringResource(R.string.play), Modifier.size(38.dp))
                }
            }
        }
        val current = photos.getOrNull(pager.currentPage)
        AnimatedVisibility(chrome, enter = fadeIn(), exit = fadeOut()) {
            Row(Modifier.fillMaxWidth().background(Brush.verticalGradient(listOf(Color.Black.copy(alpha = 0.8f), Color.Transparent)))
                .statusBarsPadding().padding(8.dp), verticalAlignment = Alignment.CenterVertically) {
                IconButton(onClick = { nav.popBackStack() }) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Kino.text) }
                Column(Modifier.weight(1f)) {
                    Text(current?.title.orEmpty(), color = Kino.text, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    formatTaken(current?.takenAt)?.let { Text(it, color = Kino.muted, fontSize = 12.sp) }
                }
                if (photos.size > 1) Text("${pager.currentPage + 1} / ${photos.size}", color = Kino.muted, fontSize = 13.sp, modifier = Modifier.padding(horizontal = 8.dp))
                IconButton(onClick = { info = true }) { Icon(Icons.Outlined.Info, stringResource(R.string.details), tint = Kino.text) }
            }
        }
        if (info && current != null) ModalBottomSheet(onDismissRequest = { info = false }, containerColor = Kino.surface) {
            PhotoInfo(state, current)
        }
    }
}

@Composable
private fun PhotoInfo(state: AppState, p: ItemSummary) {
    val api = state.api ?: return
    val (d, loading) = rememberLoad(p.id) { api.item(p.id) }
    val ph = d?.photo
    Column(Modifier.fillMaxWidth().padding(horizontal = 20.dp).padding(bottom = 30.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text(p.title, style = Head, color = Kino.text)
        if (loading && d == null) Loading()
        Fact(stringResource(R.string.photo_taken), formatTaken(ph?.takenAt ?: p.takenAt).orEmpty())
        val w = ph?.width ?: p.width
        val h = ph?.height ?: p.height
        if (w != null && h != null) Fact(stringResource(R.string.photo_size), "$w × $h")
        Fact(stringResource(R.string.photo_camera), listOfNotNull(ph?.camera, ph?.lens).joinToString(" · "))
        Fact(stringResource(R.string.photo_exposure), exposureLine(ph?.fNumber, ph?.exposureTime, ph?.iso, ph?.focalLength))
        if (ph?.latitude != null && ph.longitude != null) Fact(stringResource(R.string.photo_location), "%.5f, %.5f".format(ph.latitude, ph.longitude))
    }
}
