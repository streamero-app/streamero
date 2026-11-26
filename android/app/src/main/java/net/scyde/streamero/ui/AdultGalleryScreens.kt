package net.scyde.streamero.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.itemsIndexed
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.outlined.Collections
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
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
import net.scyde.streamero.adult.ADULT_GALLERY_SORTS
import net.scyde.streamero.adult.ADULT_NAMED_SORTS
import net.scyde.streamero.adult.AdultBlur
import net.scyde.streamero.adult.AdultGalleryFilter
import net.scyde.streamero.adult.AdultSceneFilter
import net.scyde.streamero.adult.SLIDESHOW_MS
import net.scyde.streamero.adult.nextPicture
import net.scyde.streamero.adult.toggleHiddenTag
import net.scyde.streamero.api.*

// Galleries, the start page, tags and search of the adult section (D-058). Like the rest of
// the section, pictures are loaded with the session into memory only (never the disk cache),
// and every screen keeps the window out of screenshots and the recent-apps preview.

/** A gallery in a grid or row: its cover (blurred until tapped), title, studio and date. */
@Composable
internal fun AdultGalleryCard(api: Api, g: AdultGallery, blur: AdultBlur, modifier: Modifier = Modifier, onOpen: () -> Unit) {
    val (src, focused) = rememberFocusSource()
    val key = "gallery:" + g.id
    Column(modifier.clickable(interactionSource = src, indication = tapIndication()) { if (g.coverImageId == null || !blur.reveal(key)) onOpen() }) {
        Box(Modifier.fillMaxWidth().focusRing(focused).aspectRatio(4f / 5f).clip(CardShape).background(Kino.surface2)) {
            g.coverImageId?.let { AdultImage(api.adultGalleryImageUrl(it, "thumb"), blur.blurred(key), Modifier.fillMaxSize()) }
            Row(Modifier.align(Alignment.BottomEnd).padding(6.dp).background(Color.Black.copy(alpha = 0.7f), androidx.compose.foundation.shape.RoundedCornerShape(4.dp))
                .padding(horizontal = 5.dp, vertical = 2.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(3.dp)) {
                Icon(Icons.Outlined.Collections, null, tint = Color.White, modifier = Modifier.size(12.dp))
                Text(g.imageCount.toString(), color = Color.White, fontSize = 11.sp, fontWeight = FontWeight.SemiBold)
            }
        }
        Spacer(Modifier.height(if (focused) 12.dp else 8.dp))
        Text(g.title, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(listOfNotNull(g.studio?.name, g.date ?: g.year?.toString()).joinToString(" · "), color = Kino.faint, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

private const val GALLERY_PAGE = 60

/** Galleries in a paged grid: all of them with search and sort, or one studio's, performer's or tag's. */
@Composable
internal fun AdultGalleryGrid(state: AppState, nav: NavHostController, session: AdultSession, base: AdultGalleryFilter, filters: Boolean, header: @Composable () -> Unit) {
    val api = state.api ?: return
    var q by rememberSaveable { mutableStateOf("") }
    var sort by rememberSaveable { mutableStateOf("date") }
    val filter = base.copy(q = q, sort = sort)
    var items by remember { mutableStateOf<List<AdultGallery>>(emptyList()) }
    var total by remember { mutableIntStateOf(-1) }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    suspend fun load(reset: Boolean) {
        val f = filter
        loading = true
        runCatching { api.adultGalleries(f.query(if (reset) 0 else items.size, GALLERY_PAGE)) }
            .onSuccess { p -> items = if (reset) p.items else items + p.items; total = p.total; error = null }
            .onFailure { e -> if (Api.isAdultLocked(e)) session.relock() else error = e }
        loading = false
    }
    LaunchedEffect(filter) {
        if (filter.q.isNotBlank()) delay(300)
        load(true)
    }
    LazyVerticalGrid(GridCells.Adaptive(Dims.gridCell + 12.dp), Modifier.fillMaxSize(), contentPadding = gridPadding(),
        horizontalArrangement = Arrangement.spacedBy(gridGap()), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                header()
                if (filters) {
                    OutlinedTextField(q, { q = it }, leadingIcon = { Icon(Icons.Outlined.Search, null) }, placeholder = { Text(stringResource(R.string.adult_search_galleries)) },
                        singleLine = true, modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
                    Picker(sort, ADULT_GALLERY_SORTS.map { it to stringResource(adultSortLabel(it)) }, { sort = it })
                }
                if (total >= 0) Text(LocalContext.current.resources.getQuantityString(R.plurals.adult_galleries_count, total, total), color = Kino.muted, fontSize = 13.sp)
                ErrorLine(error)
            }
        }
        items(items, key = { it.id }) { g ->
            AdultGalleryCard(api, g, session.blur, Modifier.initialFocus(!filters && g.id == items.firstOrNull()?.id)) { nav.navigate("adult/gallery/${g.id}") }
        }
        if (total > items.size) item(span = { GridItemSpan(maxLineSpan) }) {
            LaunchedEffect(items.size) { if (!loading) load(false) }
            Loading()
        }
        if (total == 0) item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(if (q.isBlank()) R.string.nothing_here else R.string.nothing_found), color = Kino.muted) }
    }
}

/** One gallery: its details and a grid of pictures; a picture opens the viewer. */
@Composable
fun AdultGalleryScreen(state: AppState, nav: NavHostController, id: String) {
    val api = state.api ?: return
    AdultFrame(state, nav) { session ->
        val (g, loading, error) = rememberLoad(id) { api.adultGallery(id) }
        LaunchedEffect(error) { if (Api.isAdultLocked(error)) session.relock() }
        if (loading && g == null) { Loading(Modifier.fillMaxSize()); return@AdultFrame }
        if (g == null) {
            Column(Modifier.statusBarsPadding().padding(20.dp)) { BackRow(nav); ErrorLine(error) }
            return@AdultFrame
        }
        LazyVerticalGrid(GridCells.Adaptive(if (LocalTv.current) 150.dp else 104.dp), Modifier.fillMaxSize(), contentPadding = gridPadding(),
            horizontalArrangement = Arrangement.spacedBy(4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            item(span = { GridItemSpan(maxLineSpan) }) {
                Column(Modifier.padding(bottom = 12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    BackRow(nav)
                    AdultTrail(listOfNotNull(stringResource(R.string.adult_section), stringResource(R.string.adult_photos), g.studio?.name, g.title))
                    g.studio?.let { s ->
                        Text(s.name.uppercase(), style = Head, color = Kino.muted, modifier = Modifier.focusClickable(scale = 1.02f) { nav.navigate("adult/studio/${s.id}") })
                    }
                    Text(g.title, style = Display.copy(fontSize = 32.sp), color = Kino.text, maxLines = 3, overflow = TextOverflow.Ellipsis)
                    Text(listOfNotNull(g.date, LocalContext.current.resources.getQuantityString(R.plurals.adult_pictures_count, g.imageCount, g.imageCount)).joinToString("  ·  "),
                        color = Kino.muted, fontSize = 14.sp)
                    PrimaryButton(stringResource(R.string.adult_view_gallery), { nav.navigate("adult/gallery/${g.id}/view/0") }, Modifier.initialFocus(),
                        icon = Icons.Default.PlayArrow, enabled = g.images.isNotEmpty())
                    g.scene?.let { s -> GlassButton(stringResource(R.string.adult_from_scene, s.name), { nav.navigate("adult/scene/${s.id}") }) }
                    if (g.performers.isNotEmpty()) LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        items(g.performers, key = { it.id }) { p -> GlassButton(p.name, { nav.navigate("adult/performer/${p.id}") }) }
                    }
                    if (g.tags.isNotEmpty()) LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        items(g.tags, key = { it }) { t -> GlassButton("#$t", { nav.navigate("adult/tag/" + android.net.Uri.encode(t)) }) }
                    }
                }
            }
            itemsIndexed(g.images, key = { _, im -> im.id }) { i, im ->
                val (src, focused) = rememberFocusSource()
                val key = "image:" + im.id
                Box(Modifier.aspectRatio(1f).focusRing(focused, scale = 1.04f).clip(androidx.compose.foundation.shape.RoundedCornerShape(2.dp)).background(Kino.surface2)
                    .clickable(interactionSource = src, indication = tapIndication()) { if (!session.blur.reveal(key)) nav.navigate("adult/gallery/${g.id}/view/$i") }) {
                    AdultImage(api.adultGalleryImageUrl(im.id, "thumb"), session.blur.blurred(key), Modifier.fillMaxSize())
                }
            }
        }
    }
}

/**
 * Full-screen pictures of a gallery: swipe (or D-pad left / right on a TV), pinch or double tap
 * to zoom, a slideshow. There is no download here: originals never leave memory on the phone.
 */
@Composable
fun AdultGalleryViewer(state: AppState, nav: NavHostController, id: String, start: Int) {
    val api = state.api ?: return
    AdultFrame(state, nav) { session ->
        val (g, _, error) = rememberLoad(id) { api.adultGallery(id) }
        LaunchedEffect(error) { if (Api.isAdultLocked(error)) session.relock() }
        Immersive()
        Box(Modifier.fillMaxSize().background(Color.Black)) {
            val images = g?.images.orEmpty()
            if (images.isEmpty()) { if (g == null) Loading(Modifier.fillMaxSize()) else ErrorLine(error); return@Box }
            val pager = rememberPagerState(initialPage = start.coerceIn(0, images.size - 1)) { images.size }
            val scope = rememberCoroutineScope()
            var chrome by remember { mutableStateOf(true) }
            var zoomed by remember { mutableStateOf(false) }
            var playing by remember { mutableStateOf(false) }
            val focus = remember { FocusRequester() }
            fun go(delta: Int) = scope.launch { pager.animateScrollToPage(nextPicture(pager.currentPage, delta, images.size)) }
            LaunchedEffect(playing, pager.currentPage) {
                if (!playing) return@LaunchedEffect
                delay(SLIDESHOW_MS)
                pager.animateScrollToPage(nextPicture(pager.currentPage, 1, images.size))
            }
            LaunchedEffect(Unit) { runCatching { focus.requestFocus() } }
            HorizontalPager(pager, Modifier.fillMaxSize().focusRequester(focus).focusable().onKeyEvent { e ->
                if (e.type != KeyEventType.KeyDown) return@onKeyEvent false
                when (e.key) {
                    Key.DirectionRight -> { go(1); true }
                    Key.DirectionLeft -> { go(-1); true }
                    Key.DirectionCenter, Key.Enter, Key.Spacebar, Key.MediaPlayPause -> { playing = !playing; chrome = true; true }
                    else -> false
                }
            }, userScrollEnabled = !zoomed, beyondViewportPageCount = 1, key = { images[it].id }) { page ->
                val im = images[page]
                Zoomable(active = page == pager.currentPage, onZoomed = { if (page == pager.currentPage) zoomed = it }, onTap = { _, _ -> chrome = !chrome }) {
                    // The thumbnail first, the full picture over it.
                    AdultImage(api.adultGalleryImageUrl(im.id, "thumb"), false, Modifier.fillMaxSize(), ContentScale.Fit)
                    AdultImage(api.adultGalleryImageUrl(im.id, "full"), false, Modifier.fillMaxSize(), ContentScale.Fit)
                }
            }
            AnimatedVisibility(chrome, enter = fadeIn(), exit = fadeOut()) {
                Row(Modifier.fillMaxWidth().background(Brush.verticalGradient(listOf(Color.Black.copy(alpha = 0.8f), Color.Transparent)))
                    .statusBarsPadding().padding(8.dp), verticalAlignment = Alignment.CenterVertically) {
                    IconButton(onClick = { nav.popBackStack() }) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Kino.text) }
                    Text(g?.title.orEmpty(), color = Kino.text, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                    Text("${pager.currentPage + 1} / ${images.size}", color = Kino.muted, fontSize = 13.sp, modifier = Modifier.padding(horizontal = 8.dp))
                    IconButton(onClick = { playing = !playing }) {
                        Icon(if (playing) Icons.Default.Pause else Icons.Default.PlayArrow,
                            stringResource(if (playing) R.string.adult_slideshow_stop else R.string.adult_slideshow), tint = Kino.text)
                    }
                }
            }
        }
    }
}

/** Rows of the section's start page. */
@Composable
internal fun AdultHomeList(state: AppState, nav: NavHostController, session: AdultSession, header: @Composable () -> Unit) {
    val api = state.api ?: return
    val (home, loading, error) = rememberLoad(Unit) { api.adultHome() }
    LaunchedEffect(error) { if (Api.isAdultLocked(error)) session.relock() }
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 30.dp), verticalArrangement = Arrangement.spacedBy(22.dp)) {
        item { Box(Modifier.padding(horizontal = Dims.edge)) { header() } }
        if (loading && home == null) item { Loading() }
        if (!Api.isAdultLocked(error)) item { Box(Modifier.padding(horizontal = Dims.edge)) { ErrorLine(error) } }
        home?.let { h ->
            val sceneRows = listOf(R.string.adult_continue to h.continueWatching, R.string.adult_recently_added to h.recentlyAdded,
                R.string.adult_favorite_scenes to h.favoritePerformers)
            sceneRows.filter { it.second.isNotEmpty() }.forEach { (title, list) ->
                item(key = "row-$title") {
                    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        SectionTitle(stringResource(title))
                        LazyRow(contentPadding = Dims.rowPad, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            items(list, key = { it.id }) { s -> AdultSceneCard(api, s, session.blur, false, Modifier.width(Dims.wide)) { nav.navigate("adult/scene/${s.id}") } }
                        }
                    }
                }
            }
            if (h.recentGalleries.isNotEmpty()) item(key = "galleries") {
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    SectionTitle(stringResource(R.string.adult_recent_galleries))
                    LazyRow(contentPadding = Dims.rowPad, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        items(h.recentGalleries, key = { it.id }) { g -> AdultGalleryCard(api, g, session.blur, Modifier.width(150.dp)) { nav.navigate("adult/gallery/${g.id}") } }
                    }
                }
            }
            if (h.recentlyReleased.isNotEmpty()) item(key = "released") {
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    SectionTitle(stringResource(R.string.adult_recently_released))
                    LazyRow(contentPadding = Dims.rowPad, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        items(h.recentlyReleased, key = { it.id }) { s -> AdultSceneCard(api, s, session.blur, false, Modifier.width(Dims.wide)) { nav.navigate("adult/scene/${s.id}") } }
                    }
                }
            }
            if (h.continueWatching.isEmpty() && h.recentlyAdded.isEmpty() && h.recentGalleries.isEmpty()) item {
                Text(stringResource(R.string.nothing_here), color = Kino.muted, modifier = Modifier.padding(horizontal = Dims.edge))
            }
        }
    }
}

/** Tags with counts; each can be hidden (or shown again) by the viewer. */
@Composable
internal fun AdultTagList(state: AppState, nav: NavHostController, session: AdultSession, header: @Composable () -> Unit) {
    val api = state.api ?: return
    var q by rememberSaveable { mutableStateOf("") }
    var sort by rememberSaveable { mutableStateOf("count") }
    var showHidden by rememberSaveable { mutableStateOf(false) }
    var hidden by remember { mutableStateOf(session.status.hiddenTags) }
    var tick by remember { mutableIntStateOf(0) }
    var saveError by remember { mutableStateOf<Throwable?>(null) }
    val scope = rememberCoroutineScope()
    val (page, loading, error) = rememberLoad(listOf(q, sort, showHidden, tick)) {
        if (q.isNotBlank()) delay(300)
        api.adultTags(q, sort, includeHidden = showHidden)
    }
    LaunchedEffect(error) { if (Api.isAdultLocked(error)) session.relock() }
    fun toggle(tag: String) {
        val next = toggleHiddenTag(hidden, tag)
        scope.launch {
            runCatching { api.adultPreferences(AdultPreferencesInput(hiddenTags = next)) }
                .onSuccess { s -> hidden = s.hiddenTags; session.update(s); saveError = null; tick++ }
                .onFailure { saveError = it }
        }
    }
    LazyVerticalGrid(GridCells.Adaptive(220.dp), Modifier.fillMaxSize(), contentPadding = gridPadding(),
        horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                header()
                OutlinedTextField(q, { q = it }, leadingIcon = { Icon(Icons.Outlined.Search, null) }, placeholder = { Text(stringResource(R.string.adult_search_tags)) },
                    singleLine = true, modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                    Picker(sort, ADULT_NAMED_SORTS.map { it to stringResource(adultSortLabel(it)) }, { sort = it }, Modifier.weight(1f))
                    if (hidden.isNotEmpty()) GlassButton(stringResource(R.string.adult_show_hidden_tags, hidden.size), { showHidden = !showHidden }, active = showHidden)
                }
                Text(stringResource(R.string.adult_tags_hint), color = Kino.faint, fontSize = 13.sp)
                ErrorLine(saveError)
                if (!Api.isAdultLocked(error)) ErrorLine(error)
                if (loading && page == null) Loading()
            }
        }
        items(page?.items.orEmpty(), key = { it.name }) { t ->
            val isHidden = hidden.any { it.equals(t.name, ignoreCase = true) }
            Row(Modifier.clip(CardShape).background(if (isHidden) Color.Transparent else Kino.surface2), verticalAlignment = Alignment.CenterVertically) {
                Row(Modifier.weight(1f).focusClickable { nav.navigate("adult/tag/" + android.net.Uri.encode(t.name)) }.padding(horizontal = 12.dp, vertical = 12.dp),
                    verticalAlignment = Alignment.CenterVertically) {
                    Text(t.name, color = if (isHidden) Kino.faint else Kino.text, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                    Text((t.sceneCount + t.galleryCount).toString(), color = Kino.faint, fontSize = 12.sp)
                }
                IconButton(onClick = { toggle(t.name) }) {
                    Icon(if (isHidden) Icons.Outlined.Visibility else Icons.Outlined.VisibilityOff,
                        stringResource(if (isHidden) R.string.adult_show_tag else R.string.adult_hide_tag), tint = Kino.muted)
                }
            }
        }
        if (page != null && page.items.isEmpty()) item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(R.string.adult_no_tags), color = Kino.muted) }
    }
}

/** Everything with one tag: scenes and movies, then galleries. */
@Composable
fun AdultTagScreen(state: AppState, nav: NavHostController, tag: String) {
    AdultFrame(state, nav) { session ->
        var galleries by rememberSaveable { mutableStateOf(false) }
        val header: @Composable () -> Unit = {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                BackRow(nav)
                AdultTrail(listOf(stringResource(R.string.adult_section), stringResource(R.string.adult_tags), tag))
                Text("#$tag", style = Display.copy(fontSize = 32.sp), color = Kino.text)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    GlassButton(stringResource(R.string.adult_scenes_and_movies), { galleries = false }, active = !galleries)
                    GlassButton(stringResource(R.string.adult_photos), { galleries = true }, active = galleries)
                }
            }
        }
        if (galleries) AdultGalleryGrid(state, nav, session, AdultGalleryFilter(tag = tag), filters = false, header = header)
        else AdultSceneGrid(state, nav, session, AdultSceneFilter(tag = tag), filters = false, header = header)
    }
}

/** Search inside the section (the regular search never returns adult content). */
@Composable
fun AdultSearchScreen(state: AppState, nav: NavHostController, query: String) {
    val api = state.api ?: return
    AdultFrame(state, nav) { session ->
        val (r, loading, error) = rememberLoad(query) { api.adultSearch(query) }
        LaunchedEffect(error) { if (Api.isAdultLocked(error)) session.relock() }
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 30.dp), verticalArrangement = Arrangement.spacedBy(20.dp)) {
            item {
                Column(Modifier.padding(horizontal = Dims.edge), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    BackRow(nav)
                    AdultTrail(listOf(stringResource(R.string.adult_section), stringResource(R.string.search)))
                    Text(query, style = Display.copy(fontSize = 30.sp), color = Kino.text, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    if (loading && r == null) Loading()
                    if (!Api.isAdultLocked(error)) ErrorLine(error)
                }
            }
            r?.let { res ->
                if (res.performers.isNotEmpty()) item {
                    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        SectionTitle(stringResource(R.string.adult_performers))
                        LazyRow(contentPadding = Dims.rowPad, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            items(res.performers, key = { it.id }) { p ->
                                AdultPersonCard(if (p.hasImage) api.adultImageUrl("performer", p.id, "image") else null, p.name, p.sceneCount, session.blur,
                                    "performer:" + p.id, portrait = true, modifier = Modifier.width(Dims.gridCell), galleryCount = p.galleryCount, favorite = p.favorite) {
                                    nav.navigate("adult/performer/${p.id}")
                                }
                            }
                        }
                    }
                }
                if (res.studios.isNotEmpty()) item {
                    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        SectionTitle(stringResource(R.string.adult_studios))
                        LazyRow(contentPadding = Dims.rowPad, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            items(res.studios, key = { it.id }) { s -> GlassButton(s.name, { nav.navigate("adult/studio/${s.id}") }) }
                        }
                    }
                }
                if (res.scenes.isNotEmpty()) item {
                    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        SectionTitle(stringResource(R.string.adult_scenes_and_movies))
                        LazyRow(contentPadding = Dims.rowPad, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            items(res.scenes, key = { it.id }) { s -> AdultSceneCard(api, s, session.blur, false, Modifier.width(Dims.wide)) { nav.navigate("adult/scene/${s.id}") } }
                        }
                    }
                }
                if (res.galleries.isNotEmpty()) item {
                    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        SectionTitle(stringResource(R.string.adult_photos))
                        LazyRow(contentPadding = Dims.rowPad, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            items(res.galleries, key = { it.id }) { g -> AdultGalleryCard(api, g, session.blur, Modifier.width(150.dp)) { nav.navigate("adult/gallery/${g.id}") } }
                        }
                    }
                }
                if (res.tags.isNotEmpty()) item {
                    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        SectionTitle(stringResource(R.string.adult_tags))
                        LazyRow(contentPadding = Dims.rowPad, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            items(res.tags, key = { it.name }) { t -> GlassButton("#${t.name}", { nav.navigate("adult/tag/" + android.net.Uri.encode(t.name)) }) }
                        }
                    }
                }
                if (res.scenes.isEmpty() && res.galleries.isEmpty() && res.performers.isEmpty() && res.studios.isEmpty() && res.tags.isEmpty()) item {
                    Text(stringResource(R.string.nothing_found), color = Kino.muted, modifier = Modifier.padding(horizontal = Dims.edge))
                }
            }
        }
    }
}
