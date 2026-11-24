package net.scyde.streamero.ui

import android.view.WindowManager
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
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Replay
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.blur
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import coil.compose.AsyncImage
import coil.request.CachePolicy
import coil.request.ImageRequest
import coil.size.Precision
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.adult.ADULT_KINDS
import net.scyde.streamero.adult.ADULT_SORTS
import net.scyde.streamero.adult.ADULT_STATUSES
import net.scyde.streamero.adult.AdultBlur
import net.scyde.streamero.adult.AdultGate
import net.scyde.streamero.adult.AdultSceneFilter
import net.scyde.streamero.adult.adultGate
import net.scyde.streamero.adult.showsAdultSection
import net.scyde.streamero.adult.validAdultPin
import net.scyde.streamero.api.*

// The adult section (D-053). Its screens keep to themselves: no downloads, no watch together,
// nothing in the home rows; artwork is held in memory only, and the window is kept out of
// screenshots and the recent-apps preview while any of them is open.

/** How many open screens asked for FLAG_SECURE; the flag stays while any of them is shown. */
private var secureHolders = 0

/** Keeps the window out of screenshots, screen recording and the recent-apps preview while shown. */
@Composable
fun SecureWindow(enabled: Boolean = true) {
    val ctx = LocalContext.current
    DisposableEffect(enabled) {
        val window = ctx.findActivity()?.window
        if (!enabled || window == null) return@DisposableEffect onDispose { }
        if (secureHolders++ == 0) window.addFlags(WindowManager.LayoutParams.FLAG_SECURE)
        onDispose {
            secureHolders = (secureHolders - 1).coerceAtLeast(0)
            if (secureHolders == 0) window.clearFlags(WindowManager.LayoutParams.FLAG_SECURE)
        }
    }
}

fun NavHostController.watchAdult(id: String, start: Long? = null) {
    navigate(if (start == null) "watchadult/$id" else "watchadult/$id?start=$start")
}

/**
 * Artwork of the adult section: loaded with the session, never written to the disk cache. When
 * [blurred], a tiny copy is drawn enlarged (soft on every Android version) under a dim layer.
 */
@Composable
fun AdultImage(url: String?, blurred: Boolean, modifier: Modifier = Modifier, contentScale: ContentScale = ContentScale.Crop) {
    val loader = LocalImages.current ?: return
    if (url == null) return
    val request = ImageRequest.Builder(LocalContext.current).data(url)
        .diskCachePolicy(CachePolicy.DISABLED)
        .apply {
            // A separate memory key, so the sharp copy is never handed out for a blurred image.
            if (blurred) size(24).precision(Precision.EXACT).memoryCacheKey("$url#blurred") else crossfade(true)
        }
        .build()
    Box(modifier) {
        AsyncImage(model = request, imageLoader = loader, contentDescription = null, contentScale = contentScale,
            modifier = Modifier.fillMaxSize().then(if (blurred) Modifier.blur(16.dp) else Modifier))
        if (blurred) Box(Modifier.fillMaxSize().background(Color.Black.copy(alpha = 0.35f)), contentAlignment = Alignment.Center) {
            Icon(Icons.Outlined.VisibilityOff, stringResource(R.string.adult_tap_to_show), tint = Kino.text.copy(alpha = 0.8f), modifier = Modifier.size(22.dp))
        }
    }
}

/** What an adult screen gets once the section is open: its status, the blur of this visit, and a way to ask for the PIN again. */
class AdultSession(val status: AdultStatus, val blur: AdultBlur, val relock: () -> Unit, val update: (AdultStatus) -> Unit)

/**
 * The frame of every adult screen: checks the section is open to this session, asks for the
 * PIN when it is locked, and keeps the window private.
 */
@Composable
private fun AdultFrame(state: AppState, nav: NavHostController, content: @Composable (AdultSession) -> Unit) {
    val api = state.api ?: return
    SecureWindow()
    var tick by remember { mutableIntStateOf(0) }
    var saved by remember { mutableStateOf<AdultStatus?>(null) }
    val (loaded, loading, error) = rememberLoad(tick) { api.adultStatus() }
    val status = saved ?: loaded
    val gate = if (!showsAdultSection(state.me)) AdultGate.Unavailable else adultGate(status, if (loading) null else error)
    when (gate) {
        AdultGate.Loading -> Loading(Modifier.fillMaxSize())
        AdultGate.Unavailable -> Column(Modifier.fillMaxSize().statusBarsPadding().padding(Dims.edge), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Text(stringResource(R.string.adult_unavailable), color = Kino.muted, fontSize = 15.sp)
            GlassButton(stringResource(R.string.back), { nav.popBackStack() }, Modifier.initialFocus())
        }
        AdultGate.Locked -> AdultPinGate(state, onBack = { nav.popBackStack() }) { saved = null; tick++ }
        AdultGate.Open -> {
            val s = status ?: return
            val blur = remember(s.blur) { AdultBlur(s.blur) }
            val session = remember(s, blur) { AdultSession(s, blur, relock = { saved = null; tick++ }, update = { saved = it }) }
            content(session)
        }
    }
}

@Composable
private fun AdultPinGate(state: AppState, onBack: () -> Unit, onUnlocked: () -> Unit) {
    val api = state.api ?: return
    var pin by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    val scope = rememberCoroutineScope()
    fun unlock() {
        if (!validAdultPin(pin) || busy) return
        busy = true
        error = null
        scope.launch {
            runCatching { api.adultUnlock(pin) }.onSuccess { onUnlocked() }.onFailure { error = it; pin = "" }
            busy = false
        }
    }
    Column(Modifier.fillMaxSize().statusBarsPadding().padding(Dims.edge), verticalArrangement = Arrangement.spacedBy(14.dp, Alignment.CenterVertically),
        horizontalAlignment = Alignment.CenterHorizontally) {
        Icon(Icons.Outlined.Lock, null, tint = Kino.muted, modifier = Modifier.size(40.dp))
        Text(stringResource(R.string.adult_locked_title), style = Display.copy(fontSize = 28.sp), color = Kino.text)
        Text(stringResource(R.string.adult_locked_hint), color = Kino.muted, fontSize = 14.sp)
        OutlinedTextField(pin, { v -> pin = v.filter { it in '0'..'9' }.take(12) }, label = { Text(stringResource(R.string.pin)) }, singleLine = true,
            visualTransformation = PasswordVisualTransformation(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
            modifier = Modifier.widthIn(max = 360.dp).fillMaxWidth().dpadExit().initialFocus(), shape = CardShape)
        PrimaryButton(stringResource(R.string.adult_unlock), { unlock() }, Modifier.widthIn(max = 360.dp).fillMaxWidth(), icon = Icons.Outlined.Lock, enabled = validAdultPin(pin) && !busy)
        ErrorLine(error)
        TextButton(onClick = onBack) { Text(stringResource(R.string.back), color = Kino.muted) }
    }
}

/** The entry on the Libraries page; the caller shows it only when the section is open to this session. */
@Composable
fun AdultLibraryTile(modifier: Modifier = Modifier, onClick: () -> Unit) {
    Box(modifier.fillMaxWidth().height(110.dp).focusClickable(scale = 1.02f, onClick = onClick).clip(RoundedCornerShape6).background(Kino.surface2).padding(18.dp)) {
        Icon(Icons.Outlined.Lock, null, tint = Kino.faint, modifier = Modifier.align(Alignment.TopEnd).size(20.dp))
        Column(Modifier.align(Alignment.BottomStart)) {
            Text(stringResource(R.string.adult_section), style = Display.copy(fontSize = 28.sp), color = Kino.text)
            Text(stringResource(R.string.adult_section_hint), color = Kino.muted, fontSize = 13.sp)
        }
    }
}

private val RoundedCornerShape6 = androidx.compose.foundation.shape.RoundedCornerShape(6.dp)

private enum class AdultTab { Scenes, Studios, Performers }

/** The section: scenes, studios and performers of the adult libraries. */
@Composable
fun AdultScreen(state: AppState, nav: NavHostController) {
    AdultFrame(state, nav) { session ->
        var tab by rememberSaveable { mutableStateOf(AdultTab.Scenes) }
        var settings by remember { mutableStateOf(false) }
        val header: @Composable () -> Unit = {
            Column(Modifier.statusBarsPadding().padding(top = 12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(stringResource(R.string.adult_section), style = Display.copy(fontSize = 34.sp), color = Kino.text, modifier = Modifier.weight(1f))
                    GlassButton(null, { settings = true }, Modifier.width(52.dp), icon = Icons.Outlined.Settings)
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    AdultTab.entries.forEach { t ->
                        GlassButton(stringResource(when (t) { AdultTab.Scenes -> R.string.adult_scenes; AdultTab.Studios -> R.string.adult_studios; AdultTab.Performers -> R.string.adult_performers }),
                            { tab = t }, Modifier.initialFocus(t == tab), active = t == tab)
                    }
                }
            }
        }
        when (tab) {
            AdultTab.Scenes -> AdultSceneGrid(state, nav, session, AdultSceneFilter(), filters = true, header = header)
            AdultTab.Studios -> AdultStudioGrid(state, nav, session, header)
            AdultTab.Performers -> AdultPerformerGrid(state, nav, session, header)
        }
        if (settings) AdultSettingsSheet(state, session, onLocked = { settings = false; nav.popBackStack() }) { settings = false }
    }
}

/** Scenes in a paged grid: the whole section with search and filters, or one studio's or performer's. */
@Composable
private fun AdultSceneGrid(state: AppState, nav: NavHostController, session: AdultSession, base: AdultSceneFilter, filters: Boolean, header: @Composable () -> Unit) {
    val api = state.api ?: return
    var q by rememberSaveable { mutableStateOf("") }
    var kind by rememberSaveable { mutableStateOf("") }
    var status by rememberSaveable { mutableStateOf("all") }
    var sort by rememberSaveable { mutableStateOf("date") }
    val filter = base.copy(q = q, kind = kind, status = status, sort = sort)
    var items by remember { mutableStateOf<List<AdultScene>>(emptyList()) }
    var total by remember { mutableIntStateOf(-1) }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    suspend fun load(reset: Boolean) {
        val f = filter
        loading = true
        runCatching { api.adultScenes(f.query(if (reset) 0 else items.size, PAGE)) }
            .onSuccess { p -> items = if (reset) p.items else items + p.items; total = p.total; error = null }
            .onFailure { e -> if (Api.isAdultLocked(e)) session.relock() else error = e }
        loading = false
    }
    LaunchedEffect(filter) {
        if (filter.q.isNotBlank()) delay(300)
        load(true)
    }
    val portrait = kind == "movie"
    val cell: Dp = if (portrait) Dims.gridCell else 170.dp
    LazyVerticalGrid(GridCells.Adaptive(cell), Modifier.fillMaxSize(), contentPadding = gridPadding(),
        horizontalArrangement = Arrangement.spacedBy(gridGap()), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                header()
                if (filters) {
                    OutlinedTextField(q, { q = it }, leadingIcon = { Icon(Icons.Outlined.Search, null) }, placeholder = { Text(stringResource(R.string.adult_search_hint)) },
                        singleLine = true, modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Picker(kind, ADULT_KINDS.map { it to stringResource(adultKindLabel(it)) }, { kind = it }, Modifier.weight(1f))
                        Picker(status, ADULT_STATUSES.map { it to stringResource(adultStatusLabel(it)) }, { status = it }, Modifier.weight(1f))
                    }
                    Picker(sort, ADULT_SORTS.map { it to stringResource(adultSortLabel(it)) }, { sort = it })
                }
                if (total >= 0) Text(LocalContext.current.resources.getQuantityString(R.plurals.adult_scenes_count, total, total), color = Kino.muted, fontSize = 13.sp)
                ErrorLine(error)
            }
        }
        items(items, key = { it.id }) { s ->
            AdultSceneCard(api, s, session.blur, portrait, Modifier.initialFocus(!filters && s.id == items.firstOrNull()?.id)) { nav.navigate("adult/scene/${s.id}") }
        }
        if (total > items.size) item(span = { GridItemSpan(maxLineSpan) }) {
            LaunchedEffect(items.size) { if (!loading) load(false) }
            Loading()
        }
        if (total == 0) item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(if (q.isBlank()) R.string.nothing_here else R.string.nothing_found), color = Kino.muted) }
    }
}

private const val PAGE = 60

private fun adultKindLabel(k: String) = when (k) { "scene" -> R.string.adult_kind_scenes; "movie" -> R.string.adult_kind_movies; else -> R.string.adult_kind_all }
private fun adultStatusLabel(s: String) = when (s) {
    "unwatched" -> R.string.status_unplayed
    "inProgress" -> R.string.adult_status_in_progress
    "watched" -> R.string.watched
    else -> R.string.adult_status_all
}
private fun adultSortLabel(s: String) = when (s) {
    "added" -> R.string.sort_added
    "title" -> R.string.sort_title
    "played" -> R.string.adult_sort_played
    else -> R.string.adult_sort_date
}

/** The picture of a scene card: its poster, else its backdrop. */
private fun sceneCardImage(hasPoster: Boolean, hasBackdrop: Boolean): String? = when {
    hasPoster -> "poster"
    hasBackdrop -> "backdrop"
    else -> null
}

@Composable
private fun AdultSceneCard(api: Api, s: AdultScene, blur: AdultBlur, portrait: Boolean, modifier: Modifier = Modifier, onOpen: () -> Unit) {
    val (src, focused) = rememberFocusSource()
    val key = "scene:" + s.id
    val image = sceneCardImage(s.hasPoster, s.hasBackdrop)
    // A blurred picture is revealed by the first tap; the next one opens the scene.
    Column(modifier.clickable(interactionSource = src, indication = tapIndication()) { if (image == null || !blur.reveal(key)) onOpen() }) {
        Box(Modifier.fillMaxWidth().focusRing(focused).aspectRatio(if (portrait) 2f / 3f else 16f / 9f).clip(CardShape).background(Kino.surface2)) {
            if (image != null) AdultImage(api.adultImageUrl("scene", s.id, image), blur.blurred(key), Modifier.fillMaxSize())
            else Text(s.title, style = Display.copy(fontSize = 18.sp), color = Kino.text, maxLines = 3, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.align(Alignment.BottomStart).padding(10.dp))
            if (s.played) Icon(Icons.Default.Check, null, tint = Kino.text, modifier = Modifier.align(Alignment.TopEnd).padding(6.dp)
                .background(Color.Black.copy(alpha = 0.7f), androidx.compose.foundation.shape.RoundedCornerShape(4.dp)).padding(3.dp).size(14.dp))
            s.durationMs?.let { d -> Box(Modifier.align(Alignment.BottomEnd).padding(6.dp)) { Tag(clock(d)) } }
            ProgressLine(s.progress, Modifier.align(Alignment.BottomCenter))
        }
        Spacer(Modifier.height(if (focused) 12.dp else 8.dp))
        Text(s.title, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(listOfNotNull(s.studio?.name, s.date ?: s.year?.toString()).joinToString(" · "), color = Kino.faint, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** A studio or performer: picture (or initials) with the name and scene count. */
@Composable
private fun AdultPersonCard(imageUrl: String?, name: String, sceneCount: Int, blur: AdultBlur, key: String, portrait: Boolean, modifier: Modifier = Modifier, onOpen: () -> Unit) {
    val (src, focused) = rememberFocusSource()
    Column(modifier.clickable(interactionSource = src, indication = tapIndication()) { if (imageUrl == null || !blur.reveal(key)) onOpen() }) {
        Box(Modifier.fillMaxWidth().focusRing(focused).aspectRatio(if (portrait) 2f / 3f else 16f / 9f).clip(CardShape).background(Kino.surface2)) {
            if (imageUrl != null) AdultImage(imageUrl, blur.blurred(key), Modifier.fillMaxSize().then(if (portrait) Modifier else Modifier.padding(12.dp)),
                if (portrait) ContentScale.Crop else ContentScale.Fit)
            else Text(name, style = Display.copy(fontSize = 18.sp), color = Kino.text, maxLines = 3, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.align(Alignment.BottomStart).padding(10.dp))
        }
        Spacer(Modifier.height(if (focused) 12.dp else 8.dp))
        Text(name, color = Kino.text, fontWeight = FontWeight.SemiBold, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(LocalContext.current.resources.getQuantityString(R.plurals.adult_scenes_count, sceneCount, sceneCount), color = Kino.faint, fontSize = 13.sp, maxLines = 1)
    }
}

/** A paged, searchable grid of studios or performers. */
@Composable
private fun <T> AdultPagedGrid(
    session: AdultSession, header: @Composable () -> Unit, cell: Dp, searchHint: String,
    load: suspend (q: String, offset: Int) -> Pair<List<T>, Int>, key: (T) -> String, card: @Composable (T) -> Unit,
) {
    var q by rememberSaveable { mutableStateOf("") }
    var items by remember { mutableStateOf<List<T>>(emptyList()) }
    var total by remember { mutableIntStateOf(-1) }
    var loading by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    suspend fun fetch(reset: Boolean) {
        loading = true
        runCatching { load(q, if (reset) 0 else items.size) }
            .onSuccess { (page, t) -> items = if (reset) page else items + page; total = t; error = null }
            .onFailure { e -> if (Api.isAdultLocked(e)) session.relock() else error = e }
        loading = false
    }
    LaunchedEffect(q) {
        if (q.isNotBlank()) delay(300)
        fetch(true)
    }
    LazyVerticalGrid(GridCells.Adaptive(cell), Modifier.fillMaxSize(), contentPadding = gridPadding(),
        horizontalArrangement = Arrangement.spacedBy(gridGap()), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        item(span = { GridItemSpan(maxLineSpan) }) {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                header()
                OutlinedTextField(q, { q = it }, leadingIcon = { Icon(Icons.Outlined.Search, null) }, placeholder = { Text(searchHint) },
                    singleLine = true, modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
                ErrorLine(error)
            }
        }
        items(items, key = key) { card(it) }
        if (total > items.size) item(span = { GridItemSpan(maxLineSpan) }) {
            LaunchedEffect(items.size) { if (!loading) fetch(false) }
            Loading()
        }
        if (total == 0) item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(if (q.isBlank()) R.string.nothing_here else R.string.nothing_found), color = Kino.muted) }
    }
}

@Composable
private fun AdultStudioGrid(state: AppState, nav: NavHostController, session: AdultSession, header: @Composable () -> Unit) {
    val api = state.api ?: return
    AdultPagedGrid<AdultStudio>(session, header, 170.dp, stringResource(R.string.adult_search_studios),
        load = { q, offset -> api.adultStudios(q, offset).let { it.items to it.total } }, key = { it.id }) { s ->
        AdultPersonCard(if (s.hasImage) api.adultImageUrl("studio", s.id, "image") else null, s.name, s.sceneCount, session.blur, "studio:" + s.id, portrait = false) {
            nav.navigate("adult/studio/${s.id}")
        }
    }
}

@Composable
private fun AdultPerformerGrid(state: AppState, nav: NavHostController, session: AdultSession, header: @Composable () -> Unit) {
    val api = state.api ?: return
    AdultPagedGrid<AdultPerformer>(session, header, Dims.gridCell, stringResource(R.string.adult_search_performers),
        load = { q, offset -> api.adultPerformers(q, offset).let { it.items to it.total } }, key = { it.id }) { p ->
        AdultPersonCard(if (p.hasImage) api.adultImageUrl("performer", p.id, "image") else null, p.name, p.sceneCount, session.blur, "performer:" + p.id, portrait = true) {
            nav.navigate("adult/performer/${p.id}")
        }
    }
}

@Composable
private fun BackRow(nav: NavHostController) {
    IconButton(onClick = { nav.popBackStack() }, modifier = Modifier.statusBarsPadding()) {
        Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Kino.text)
    }
}

/** One studio and its scenes. */
@Composable
fun AdultStudioScreen(state: AppState, nav: NavHostController, id: String) {
    val api = state.api ?: return
    AdultFrame(state, nav) { session ->
        val (studio, _, error) = rememberLoad(id) { api.adultStudio(id) }
        LaunchedEffect(error) { if (Api.isAdultLocked(error)) session.relock() }
        AdultSceneGrid(state, nav, session, AdultSceneFilter(studioId = id), filters = false) {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                BackRow(nav)
                val key = "studio:$id"
                if (studio?.hasImage == true) Box(Modifier.height(90.dp).widthIn(max = 240.dp).fillMaxWidth().clip(CardShape).background(Kino.surface2)
                    .clickable { session.blur.reveal(key) }.padding(10.dp)) {
                    AdultImage(api.adultImageUrl("studio", id, "image"), session.blur.blurred(key), Modifier.fillMaxSize(), ContentScale.Fit)
                }
                Text(studio?.name.orEmpty(), style = Display.copy(fontSize = 34.sp), color = Kino.text)
                studio?.parent?.let { Text(it, color = Kino.muted, fontSize = 14.sp) }
                if (!Api.isAdultLocked(error)) ErrorLine(error)
            }
        }
    }
}

/** One performer and their scenes. */
@Composable
fun AdultPerformerScreen(state: AppState, nav: NavHostController, id: String) {
    val api = state.api ?: return
    AdultFrame(state, nav) { session ->
        val (p, _, error) = rememberLoad(id) { api.adultPerformer(id) }
        LaunchedEffect(error) { if (Api.isAdultLocked(error)) session.relock() }
        AdultSceneGrid(state, nav, session, AdultSceneFilter(performerId = id), filters = false) {
            Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                BackRow(nav)
                Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                    val key = "performer:$id"
                    if (p?.hasImage == true) Box(Modifier.width(120.dp).aspectRatio(2f / 3f).clip(CardShape).background(Kino.surface2).clickable { session.blur.reveal(key) }) {
                        AdultImage(api.adultImageUrl("performer", id, "image"), session.blur.blurred(key), Modifier.fillMaxSize())
                    }
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text(p?.name.orEmpty(), style = Display.copy(fontSize = 30.sp), color = Kino.text)
                        p?.let { x ->
                            Fact(stringResource(R.string.adult_aliases), x.aliases.joinToString())
                            Fact(stringResource(R.string.adult_born), x.birthdate.orEmpty())
                            Fact(stringResource(R.string.adult_country), x.country.orEmpty())
                        }
                    }
                }
                p?.description?.let { Text(it, color = Kino.text.copy(alpha = 0.85f), fontSize = 15.sp, lineHeight = 22.sp, maxLines = 8, overflow = TextOverflow.Ellipsis) }
                if (!Api.isAdultLocked(error)) ErrorLine(error)
            }
        }
    }
}

/** One scene or movie: artwork, facts, and Play / Resume. */
@Composable
fun AdultSceneScreen(state: AppState, nav: NavHostController, id: String) {
    val api = state.api ?: return
    AdultFrame(state, nav) { session ->
        val (d, loading, error) = rememberLoad(id) { api.adultScene(id) }
        LaunchedEffect(error) { if (Api.isAdultLocked(error)) session.relock() }
        if (loading && d == null) { Loading(Modifier.fillMaxSize()); return@AdultFrame }
        if (d == null) {
            Column(Modifier.statusBarsPadding().padding(20.dp)) { BackRow(nav); ErrorLine(error) }
            return@AdultFrame
        }
        val tv = LocalTv.current
        val key = "scene:" + d.id
        val hero = when { d.hasBackdrop -> "backdrop"; d.hasPoster -> "poster"; else -> null }
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 30.dp), verticalArrangement = Arrangement.spacedBy(20.dp)) {
            item {
                Box(Modifier.fillMaxWidth().height(if (tv) 440.dp else 420.dp)) {
                    if (hero != null) AdultImage(api.adultImageUrl("scene", d.id, hero), session.blur.blurred(key),
                        Modifier.fillMaxSize().clickable { session.blur.reveal(key) })
                    Box(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(Kino.bg.copy(alpha = 0.5f), Color.Transparent, Kino.bg.copy(alpha = 0.8f), Kino.bg))))
                    BackRow(nav)
                    Column(Modifier.align(Alignment.BottomStart).padding(horizontal = Dims.edge).then(if (tv) Modifier.fillMaxWidth(0.62f) else Modifier),
                        verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        d.studio?.let { s ->
                            Text(s.name.uppercase(), style = Head, color = Kino.muted,
                                modifier = Modifier.focusClickable(scale = 1.02f) { nav.navigate("adult/studio/${s.id}") })
                        }
                        Text(d.title, style = Display.copy(fontSize = 36.sp), color = Kino.text, maxLines = 4, overflow = TextOverflow.Ellipsis)
                        Text(listOfNotNull(d.date ?: d.year?.toString(), d.durationMs?.let { ms -> stringResource(R.string.minutes_short, ms / 60000) },
                            qualityTags(d.height, null).joinToString(" ").ifBlank { null }).joinToString("  ·  "), color = Kino.muted, fontSize = 14.sp)
                    }
                }
            }
            item {
                Column(Modifier.padding(horizontal = Dims.edge).then(if (tv) Modifier.fillMaxWidth(0.5f) else Modifier), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    if (d.files.isNotEmpty()) {
                        PrimaryButton(if (d.positionMs > 0) stringResource(R.string.resume_from, clock(d.positionMs)) else stringResource(R.string.play),
                            { nav.watchAdult(d.id) }, Modifier.fillMaxWidth().initialFocus(), icon = Icons.Default.PlayArrow)
                        if (d.positionMs > 0) GlassButton(stringResource(R.string.from_start), { nav.watchAdult(d.id, 0) }, Modifier.fillMaxWidth(), icon = Icons.Default.Replay)
                        ProgressLine(d.progress)
                    } else Text(stringResource(R.string.adult_no_files), color = Kino.muted)
                }
            }
            if (d.performers.isNotEmpty()) item {
                Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    SectionTitle(stringResource(R.string.adult_performers))
                    LazyRow(contentPadding = Dims.rowPad, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        items(d.performers, key = { it.id }) { p -> GlassButton(p.name, { nav.navigate("adult/performer/${p.id}") }) }
                    }
                }
            }
            d.description?.takeIf { it.isNotBlank() }?.let { text ->
                item { Text(text, color = Kino.text.copy(alpha = 0.85f), fontSize = 16.sp, lineHeight = 24.sp, modifier = Modifier.padding(horizontal = Dims.edge)) }
            }
            item {
                Column(Modifier.padding(horizontal = Dims.edge), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                    Fact(stringResource(R.string.adult_tags), d.tags.joinToString())
                    Fact(stringResource(R.string.released), d.date.orEmpty())
                    d.files.forEach { f ->
                        Text(listOfNotNull(f.height?.let { h -> "${h}p" }, f.videoCodec?.uppercase(), f.container.uppercase().ifBlank { null }).joinToString(" · "),
                            color = Kino.faint, fontSize = 13.sp)
                    }
                }
            }
        }
    }
}

/** Blurring and the PIN of the section. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun AdultSettingsSheet(state: AppState, session: AdultSession, onLocked: () -> Unit, onClose: () -> Unit) {
    val api = state.api ?: return
    val status = session.status
    var current by remember { mutableStateOf("") }
    var next by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var done by remember { mutableStateOf<Int?>(null) }
    val scope = rememberCoroutineScope()
    fun save(input: AdultPreferencesInput, message: Int?) {
        busy = true
        error = null
        done = null
        scope.launch {
            runCatching { api.adultPreferences(input) }
                .onSuccess { s -> session.update(s); current = ""; next = ""; done = message }
                .onFailure { error = it }
            busy = false
        }
    }
    val digits: (String) -> String = { v -> v.filter { it in '0'..'9' }.take(12) }
    ModalBottomSheet(onDismissRequest = onClose, containerColor = Kino.surface) {
        Column(Modifier.fillMaxWidth().padding(horizontal = 20.dp).padding(bottom = 30.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(stringResource(R.string.adult_settings).uppercase(), style = Head, color = Kino.text)
            Toggle(stringResource(R.string.adult_blur), stringResource(R.string.adult_blur_hint), status.blur) { on ->
                if (!busy) save(AdultPreferencesInput(blur = on), null)
            }
            HorizontalDivider(color = Kino.border)
            Text(stringResource(R.string.adult_pin), color = Kino.text, fontSize = 15.sp, fontWeight = FontWeight.Medium)
            Text(stringResource(if (status.pinSet) R.string.adult_pin_on_hint else R.string.adult_pin_off_hint), color = Kino.faint, fontSize = 13.sp)
            if (status.pinSet) OutlinedTextField(current, { current = digits(it) }, label = { Text(stringResource(R.string.adult_current_pin)) }, singleLine = true,
                visualTransformation = PasswordVisualTransformation(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
                modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
            OutlinedTextField(next, { next = digits(it) }, label = { Text(stringResource(R.string.adult_new_pin)) }, singleLine = true,
                supportingText = { Text(stringResource(R.string.adult_pin_rules)) },
                visualTransformation = PasswordVisualTransformation(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
                modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                PrimaryButton(stringResource(if (status.pinSet) R.string.adult_change_pin else R.string.adult_set_pin), {
                    save(AdultPreferencesInput(pin = next, currentPin = current.ifBlank { null }), R.string.adult_pin_saved)
                }, enabled = !busy && validAdultPin(next) && (!status.pinSet || validAdultPin(current)))
                if (status.pinSet) GlassButton(stringResource(R.string.adult_remove_pin), {
                    save(AdultPreferencesInput(pin = "", currentPin = current), R.string.adult_pin_removed)
                }, enabled = !busy && validAdultPin(current))
            }
            done?.let { Text(stringResource(it), color = Kino.success, fontSize = 14.sp) }
            ErrorLine(error)
            if (status.pinSet) {
                HorizontalDivider(color = Kino.border)
                GlassButton(stringResource(R.string.adult_lock_now), {
                    scope.launch { runCatching { api.adultLock() }.onSuccess { onLocked() }.onFailure { error = it } }
                }, Modifier.fillMaxWidth(), icon = Icons.Outlined.Lock)
            }
        }
    }
}
