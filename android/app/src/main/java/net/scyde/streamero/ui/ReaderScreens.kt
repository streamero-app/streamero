package net.scyde.streamero.ui

import android.app.Activity
import android.graphics.Bitmap
import android.graphics.pdf.PdfRenderer
import android.os.ParcelFileDescriptor
import android.view.WindowManager
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.calculatePan
import androidx.compose.foundation.gestures.calculateZoom
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.PagerState
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.SwapHoriz
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.positionChanged
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.navigation.NavHostController
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.Api
import net.scyde.streamero.api.ReadManifest
import net.scyde.streamero.api.ReadingProgressInput
import net.scyde.streamero.books.COMIC_FORMATS
import net.scyde.streamero.books.pageAfterTap
import net.scyde.streamero.books.progressionOf
import net.scyde.streamero.books.resumePage
import net.scyde.streamero.books.tapZone
import okhttp3.Request
import java.io.File

/** Hides the system bars and keeps the screen on while a reader or the photo viewer is open. */
@Composable
fun Immersive() {
    val ctx = LocalContext.current
    DisposableEffect(Unit) {
        val act = ctx as? Activity
        val ctl = act?.let { WindowCompat.getInsetsController(it.window, it.window.decorView) }
        act?.window?.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        ctl?.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        ctl?.hide(WindowInsetsCompat.Type.systemBars())
        onDispose {
            ctl?.show(WindowInsetsCompat.Type.systemBars())
            act?.window?.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        }
    }
}

/**
 * Pinch to zoom and pan a page or photo; one finger only pans once zoomed in, so swipes
 * still turn pages. A double tap zooms in or out; [onTap] gets the tap's x and the width.
 */
@Composable
fun Zoomable(active: Boolean, onZoomed: (Boolean) -> Unit, onTap: (Float, Float) -> Unit, modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    var scale by remember { mutableFloatStateOf(1f) }
    var offset by remember { mutableStateOf(Offset.Zero) }
    var size by remember { mutableStateOf(IntSize.Zero) }
    fun clamp(o: Offset, s: Float): Offset {
        val mx = size.width * (s - 1f) / 2f
        val my = size.height * (s - 1f) / 2f
        return Offset(o.x.coerceIn(-mx, mx), o.y.coerceIn(-my, my))
    }
    fun set(s: Float, o: Offset) {
        scale = s.coerceIn(1f, 5f)
        offset = if (scale <= 1f) Offset.Zero else clamp(o, scale)
        onZoomed(scale > 1.01f)
    }
    // A page scrolled away is shown at its full size again.
    LaunchedEffect(active) { if (!active && scale != 1f) set(1f, Offset.Zero) }
    Box(modifier.fillMaxSize().onSizeChanged { size = it }
        .pointerInput(Unit) {
            awaitEachGesture {
                awaitFirstDown(requireUnconsumed = false)
                do {
                    val event = awaitPointerEvent()
                    if (event.changes.size > 1 || scale > 1.01f) {
                        set(scale * event.calculateZoom(), offset + event.calculatePan())
                        event.changes.forEach { if (it.positionChanged()) it.consume() }
                    }
                } while (event.changes.any { it.pressed })
            }
        }
        .pointerInput(Unit) {
            detectTapGestures(
                onDoubleTap = { p -> if (scale > 1.01f) set(1f, Offset.Zero) else set(2.5f, (Offset(size.width / 2f, size.height / 2f) - p) * 1.5f) },
                onTap = { p -> onTap(p.x, size.width.toFloat()) },
            )
        }
        .graphicsLayer { scaleX = scale; scaleY = scale; translationX = offset.x; translationY = offset.y },
        contentAlignment = Alignment.Center) { content() }
}

/** Opens a book in the reader that fits its format (comics and PDF; EPUB and MOBI are read on the web). */
@Composable
fun ReaderScreen(state: AppState, nav: NavHostController, id: String) {
    val api = state.api ?: return
    val (data, loading, error) = rememberLoad(id) { api.openBook(id) to runCatching { api.item(id).item.title }.getOrDefault("") }
    Immersive()
    Box(Modifier.fillMaxSize().background(Color.Black)) {
        val (m, title) = data ?: run {
            if (loading) Loading(Modifier.fillMaxSize()) else Box(Modifier.padding(24.dp).statusBarsPadding()) { ErrorLine(error) }
            return@Box
        }
        val format = m.format.lowercase()
        when {
            format in COMIC_FORMATS || m.pages.isNotEmpty() -> ComicReader(state, nav, id, title, m)
            format == "pdf" -> PdfReader(state, nav, id, title, m)
            else -> Column(Modifier.fillMaxSize().padding(24.dp), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
                Text(stringResource(R.string.read_in_web), color = Kino.text, fontSize = 16.sp)
                Spacer(Modifier.height(16.dp))
                GlassButton(stringResource(R.string.back), { nav.popBackStack() })
            }
        }
    }
}

/**
 * Saves the reading position a moment after the page changes and again when the reader
 * closes; nothing is saved until the reader moves from the page it opened at.
 */
@Composable
private fun SaveProgress(state: AppState, api: Api, id: String, page: Int, count: Int) {
    val opened = remember { page }
    val moved = remember { booleanArrayOf(false) }
    val latest by rememberUpdatedState(page)
    val save = remember { { p: Int -> state.scope.launch { runCatching { api.saveReadingProgress(id, ReadingProgressInput(progressionOf(p, count), page = p)) } } } }
    LaunchedEffect(page) {
        if (page != opened) moved[0] = true
        if (moved[0]) { delay(1500); save(page) }
    }
    DisposableEffect(Unit) { onDispose { if (moved[0]) save(latest) } }
}

/** The bars over a reader: back and title at the top, page number and a page slider below. */
@Composable
private fun ReaderChrome(visible: Boolean, title: String, pager: PagerState, rtl: Boolean, onBack: () -> Unit, actions: @Composable RowScope.() -> Unit = {}) {
    val scope = rememberCoroutineScope()
    val count = pager.pageCount
    AnimatedVisibility(visible, enter = fadeIn(), exit = fadeOut()) {
        Box(Modifier.fillMaxSize()) {
            Row(Modifier.fillMaxWidth().background(Brush.verticalGradient(listOf(Color.Black.copy(alpha = 0.85f), Color.Transparent)))
                .statusBarsPadding().padding(8.dp), verticalAlignment = Alignment.CenterVertically) {
                IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Kino.text) }
                Text(title, color = Kino.text, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                actions()
            }
            Column(Modifier.align(Alignment.BottomCenter).fillMaxWidth().background(Brush.verticalGradient(listOf(Color.Transparent, Color.Black.copy(alpha = 0.85f))))
                .navigationBarsPadding().padding(horizontal = 20.dp, vertical = 12.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                if (count > 1) {
                    var drag by remember { mutableStateOf<Float?>(null) }
                    // The slider runs the way the pages turn.
                    Slider(value = drag ?: pager.currentPage.toFloat(), valueRange = 0f..(count - 1).toFloat(),
                        onValueChange = { drag = it }, onValueChangeFinished = { drag?.let { v -> scope.launch { pager.scrollToPage(v.toInt()) } }; drag = null },
                        modifier = Modifier.graphicsLayer { scaleX = if (rtl) -1f else 1f },
                        colors = SliderDefaults.colors(thumbColor = Kino.text, activeTrackColor = LocalAccent.current))
                }
                Text(stringResource(R.string.page_of, ((pager.currentPage + 1)).coerceAtMost(count), count), color = Kino.muted, fontSize = 13.sp)
            }
        }
    }
}

@Composable
private fun ComicReader(state: AppState, nav: NavHostController, id: String, title: String, m: ReadManifest) {
    val api = state.api ?: return
    val count = m.pages.size
    var rtl by remember { mutableStateOf(m.rightToLeft) }
    var chrome by remember { mutableStateOf(false) }
    var zoomed by remember { mutableStateOf(false) }
    val pager = rememberPagerState(initialPage = resumePage(m.progress.page, count)) { count }
    val scope = rememberCoroutineScope()
    SaveProgress(state, api, id, pager.currentPage, count)
    Box(Modifier.fillMaxSize()) {
        // Right to left the pager is laid out mirrored: the next page comes in from the left.
        HorizontalPager(pager, Modifier.fillMaxSize(), reverseLayout = rtl, userScrollEnabled = !zoomed, beyondViewportPageCount = 1, key = { it }) { page ->
            Zoomable(active = page == pager.currentPage, onZoomed = { if (page == pager.currentPage) zoomed = it }, onTap = { x, w ->
                val target = pageAfterTap(tapZone(x, w), rtl, pager.currentPage, count)
                if (target == null) chrome = !chrome else scope.launch { pager.animateScrollToPage(target) }
            }) {
                ServerImage(api.resolve(m.pages[page]), Modifier.fillMaxSize(), contentScale = ContentScale.Fit)
            }
        }
        if (count == 0) Text(stringResource(R.string.nothing_here), color = Kino.muted, modifier = Modifier.align(Alignment.Center))
        ReaderChrome(chrome, title, pager, rtl, { nav.popBackStack() }) {
            TextButton(onClick = { rtl = !rtl }) {
                Icon(Icons.Default.SwapHoriz, null, tint = Kino.text)
                Spacer(Modifier.width(6.dp))
                Text(stringResource(if (rtl) R.string.right_to_left else R.string.left_to_right), color = Kino.text, fontSize = 13.sp)
            }
        }
    }
}

/** Downloads a book to the app cache once (the newest few are kept) and returns the file. */
private suspend fun cachedBook(api: Api, ctx: android.content.Context, id: String, url: String, ext: String): File = withContext(Dispatchers.IO) {
    val dir = File(ctx.cacheDir, "books").apply { mkdirs() }
    val file = File(dir, "$id.$ext")
    if (file.length() > 0) { file.setLastModified(System.currentTimeMillis()); return@withContext file }
    val tmp = File(dir, "$id.$ext.part")
    api.http.newCall(Request.Builder().url(api.resolve(url)).build()).execute().use { resp ->
        if (!resp.isSuccessful) throw net.scyde.streamero.api.ApiException(resp.code, "generic", "HTTP ${resp.code}")
        tmp.outputStream().use { out -> resp.body!!.byteStream().copyTo(out) }
    }
    tmp.renameTo(file)
    dir.listFiles()?.filter { it.name != file.name }?.sortedByDescending { it.lastModified() }?.drop(4)?.forEach { it.delete() }
    file
}

@Composable
private fun PdfReader(state: AppState, nav: NavHostController, id: String, title: String, m: ReadManifest) {
    val api = state.api ?: return
    val ctx = LocalContext.current
    val (renderer, loading, error) = rememberLoad(id) {
        val f = cachedBook(api, ctx, id, m.fileUrl, "pdf")
        withContext(Dispatchers.IO) { PdfRenderer(ParcelFileDescriptor.open(f, ParcelFileDescriptor.MODE_READ_ONLY)) }
    }
    DisposableEffect(renderer) { onDispose { runCatching { renderer?.close() } } }
    val r = renderer ?: run {
        if (loading) Loading(Modifier.fillMaxSize()) else Column(Modifier.padding(24.dp).statusBarsPadding()) {
            ErrorLine(error); GlassButton(stringResource(R.string.back), { nav.popBackStack() })
        }
        return
    }
    val count = r.pageCount
    val lock = remember { Mutex() } // PdfRenderer opens one page at a time
    val widthPx = with(LocalDensity.current) { LocalConfiguration.current.screenWidthDp.dp.toPx() }
    val renderWidth = (widthPx * 1.5f).toInt().coerceIn(600, 2400)
    var chrome by remember { mutableStateOf(false) }
    var zoomed by remember { mutableStateOf(false) }
    val pager = rememberPagerState(initialPage = resumePage(m.progress.page, count)) { count }
    val scope = rememberCoroutineScope()
    SaveProgress(state, api, id, pager.currentPage, count)
    Box(Modifier.fillMaxSize()) {
        HorizontalPager(pager, Modifier.fillMaxSize(), userScrollEnabled = !zoomed, beyondViewportPageCount = 1, key = { it }) { page ->
            var bmp by remember { mutableStateOf<Bitmap?>(null) }
            LaunchedEffect(page) {
                bmp = lock.withLock {
                    withContext(Dispatchers.IO) {
                        runCatching {
                            r.openPage(page).use { p ->
                                val h = (renderWidth.toLong() * p.height / p.width.coerceAtLeast(1)).toInt().coerceIn(1, 4000)
                                Bitmap.createBitmap(renderWidth, h, Bitmap.Config.ARGB_8888).also { b ->
                                    b.eraseColor(android.graphics.Color.WHITE)
                                    p.render(b, null, null, PdfRenderer.Page.RENDER_MODE_FOR_DISPLAY)
                                }
                            }
                        }.getOrNull()
                    }
                }
            }
            Zoomable(active = page == pager.currentPage, onZoomed = { if (page == pager.currentPage) zoomed = it }, onTap = { x, w ->
                val target = pageAfterTap(tapZone(x, w), false, pager.currentPage, count)
                if (target == null) chrome = !chrome else scope.launch { pager.animateScrollToPage(target) }
            }) {
                val b = bmp
                if (b == null) CircularProgressIndicator(color = Kino.faint, strokeWidth = 2.dp)
                else Image(b.asImageBitmap(), null, Modifier.fillMaxSize(), contentScale = ContentScale.Fit)
            }
        }
        ReaderChrome(chrome, title, pager, false, { nav.popBackStack() })
    }
}
