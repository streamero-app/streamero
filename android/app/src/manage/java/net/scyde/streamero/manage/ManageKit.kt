package net.scyde.streamero.manage

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import androidx.navigation.NavHostController
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.ui.*
import java.net.URLEncoder

/** The services' status as last loaded by the hub (their API bases come from it). */
object ManageCache {
    var services by mutableStateOf<List<ArrServiceStatus>>(emptyList())
}

@Composable
fun rememberArr(state: AppState, service: ArrService): ArrClient? {
    val api = state.api ?: return null
    val base = ManageCache.services.firstOrNull { it.id == service.id }?.apiBase
    return remember(api, service, base) { ArrClient(api, service, base) }
}

fun NavHostController.manage(service: ArrService, page: String, id: String? = null, arg: String? = null) {
    val q = buildList {
        if (id != null) add("id=" + URLEncoder.encode(id, "UTF-8"))
        if (arg != null) add("arg=" + URLEncoder.encode(arg, "UTF-8"))
    }.joinToString("&")
    navigate("manage/${service.id}/$page" + if (q.isEmpty()) "" else "?$q")
}

/** A loaded value that can be reloaded, optionally polled while the screen is shown. */
class Loaded<T>(val value: T?, val error: Throwable?, val loading: Boolean, val reload: () -> Unit)

/**
 * Loads a value for [key]. With [pollMs] it is loaded again after each answer, but only while
 * the screen is visible (not in the background, not behind another screen); calls never
 * overlap, and after failures the wait grows ([pollDelay]).
 */
@Composable
fun <T> rememberLoaded(key: Any?, pollMs: Long = 0, load: suspend () -> T): Loaded<T> {
    var value by remember(key) { mutableStateOf<T?>(null) }
    var error by remember(key) { mutableStateOf<Throwable?>(null) }
    var loading by remember(key) { mutableStateOf(true) }
    var tick by remember(key) { mutableIntStateOf(0) }
    val owner = LocalLifecycleOwner.current
    val currentLoad by rememberUpdatedState(load)
    LaunchedEffect(key, tick) {
        var failures = 0
        suspend fun once() {
            loading = true
            try {
                value = currentLoad(); error = null; failures = 0
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                error = e; failures++
            } finally {
                loading = false
            }
        }
        if (pollMs <= 0) once()
        else owner.lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { while (true) { once(); delay(pollDelay(pollMs, failures)) } }
    }
    return Loaded(value, error, loading) { tick++ }
}

/** Runs one user action at a time and keeps its outcome (busy, error, a done message). */
class ActionState {
    var busy by mutableStateOf(false)
    var error by mutableStateOf<Throwable?>(null)
    var done by mutableStateOf<String?>(null)

    /**
     * Claims the action for one run; false while another one runs. Called on the main thread
     * by the click itself, so a double tap cannot start the same call twice.
     */
    fun tryStart(): Boolean {
        if (busy) return false
        busy = true; error = null; done = null
        return true
    }
}

@Composable
fun rememberAction(): Pair<ActionState, (String?, suspend () -> Unit) -> Unit> {
    val s = remember { ActionState() }
    val scope = rememberCoroutineScope()
    return s to { msg, block ->
        if (s.tryStart()) scope.launch {
            try { block(); s.done = msg } catch (e: CancellationException) { throw e } catch (e: Throwable) { s.error = e } finally { s.busy = false }
        }
    }
}

@Composable
fun ActionOutcome(s: ActionState) {
    s.done?.let { Text(it, color = Kino.success, fontSize = 14.sp) }
    ArrError(s.error)
}

/** A count in a sentence, with the plural forms of the current language ([args] default to the count). */
@Composable
fun plural(id: Int, n: Int, vararg args: Any): String {
    val res = LocalContext.current.resources
    return if (args.isEmpty()) res.getQuantityString(id, n, n) else res.getQuantityString(id, n, *args)
}

/**
 * A translated, actionable message for a failed call: the proxy's codes (D-049, D-052) each
 * have their own; the services' own messages are shown as they are.
 */
@Composable
fun arrErrorText(e: Throwable?): String? {
    if (e == null) return null
    if (e is PartialFailure) {
        val head = plural(R.plurals.m_partial_failure, e.failed.size, e.failed.size, e.total)
        val lines = e.failed.map { (name, err) -> name + ": " + (arrErrorText(err) ?: err.message.orEmpty()) }
        return (listOf(head) + lines).joinToString("\n")
    }
    if (e is InvalidNumber) return stringResource(R.string.m_err_not_a_number, e.fields.joinToString(", "))
    if (e is ArrException) {
        arrErrorRes(e.code)?.let { return stringResource(it) }
        val detail = e.detail ?: e.message?.takeIf { e.code == "validation" }
        return if (!detail.isNullOrBlank()) stringResource(R.string.m_err_service, detail) else stringResource(R.string.m_err_service_http, e.status)
    }
    return errorText(e)
}

@Composable
fun ArrError(e: Throwable?) {
    val t = arrErrorText(e) ?: return
    Text(t, color = Kino.danger, fontSize = 14.sp)
}

/** A Manage page: back arrow, title, and a lazy list of [content]. */
@Composable
fun ManagePage(
    nav: NavHostController,
    title: String,
    subtitle: String? = null,
    actions: @Composable RowScope.() -> Unit = {},
    content: LazyListScope.() -> Unit,
) {
    // The arrow goes through the system back dispatcher, so an UnsavedChangesGuard on the page can ask first.
    val dispatcher = androidx.activity.compose.LocalOnBackPressedDispatcherOwner.current?.onBackPressedDispatcher
    LazyColumn(Modifier.fillMaxSize().background(Kino.bg), contentPadding = PaddingValues(bottom = 32.dp)) {
        item {
            Row(Modifier.statusBarsPadding().padding(start = 4.dp, end = 12.dp, top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                IconButton(onClick = { if (dispatcher != null) dispatcher.onBackPressed() else nav.popBackStack() }) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Kino.text) }
                Column(Modifier.weight(1f)) {
                    Text(title, style = Display.copy(fontSize = 26.sp), color = Kino.text, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    if (subtitle != null) Text(subtitle, color = Kino.muted, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                actions()
            }
        }
        content()
    }
}

/** While [dirty], back (the arrow or the system gesture) asks before the edits are thrown away. */
@Composable
fun UnsavedChangesGuard(nav: NavHostController, dirty: Boolean) {
    var ask by remember { mutableStateOf(false) }
    androidx.activity.compose.BackHandler(enabled = dirty) { ask = true }
    if (ask) ConfirmDialog(stringResource(R.string.m_discard_changes), stringResource(R.string.m_discard), { ask = false }) { nav.popBackStack() }
}

/** Items with keys that never collide ([uniqueKeys]), so a repeated or missing id cannot crash the list. */
fun <T> LazyListScope.keyedItems(list: List<T>, id: (T) -> Any?, contentType: Any? = null, content: @Composable (T) -> Unit) {
    val keys = uniqueKeys(list.map(id))
    items(list.size, key = { keys[it] }, contentType = { contentType }) { i -> content(list[i]) }
}

/** A page link that lacks what the page needs (an old or broken link). */
@Composable
fun BadRoute(nav: NavHostController) {
    ManagePage(nav, stringResource(R.string.nav_manage)) {
        block { Text(stringResource(R.string.m_bad_link), color = Kino.danger, fontSize = 14.sp) }
    }
}

/** Padding for blocks inside a [ManagePage]. */
val PagePad = Modifier.padding(horizontal = 16.dp, vertical = 6.dp)

fun LazyListScope.block(content: @Composable ColumnScope.() -> Unit) {
    item { Column(PagePad.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp), content = content) }
}

fun <T> LazyListScope.loaded(l: Loaded<T>, content: LazyListScope.(T) -> Unit) {
    val v = l.value
    if (l.error != null) block { ArrError(l.error); GlassButton(stringResource(R.string.retry), l.reload) }
    if (v == null) { if (l.error == null) item { Loading() } } else content(v)
}

/** A tappable (optionally long-pressable) row: optional poster, title, a muted second line and something on the right. */
@OptIn(androidx.compose.foundation.ExperimentalFoundationApi::class)
@Composable
fun ListRow(
    title: String,
    subtitle: String? = null,
    modifier: Modifier = Modifier,
    image: String? = null,
    imageWidth: Dp = 44.dp,
    leading: (@Composable () -> Unit)? = null,
    trailing: (@Composable () -> Unit)? = null,
    chevron: Boolean = false,
    onLongClick: (() -> Unit)? = null,
    onClick: (() -> Unit)? = null,
) {
    val tap = when {
        onLongClick != null -> Modifier.combinedClickable(onClick = { onClick?.invoke() }, onLongClick = onLongClick)
        onClick != null -> Modifier.clickable(onClick = onClick)
        else -> Modifier
    }
    Row(modifier.fillMaxWidth().then(tap).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        if (image != null) Box(Modifier.width(imageWidth).aspectRatio(2f / 3f).clip(RoundedCornerShape(4.dp)).background(Kino.surface2)) { ServerImage(image, Modifier.fillMaxSize()) }
        leading?.invoke()
        Column(Modifier.weight(1f)) {
            Text(title, color = Kino.text, fontSize = 15.sp, fontWeight = FontWeight.Medium, maxLines = 2, overflow = TextOverflow.Ellipsis)
            if (!subtitle.isNullOrBlank()) Text(subtitle, color = Kino.muted, fontSize = 13.sp, maxLines = 3, overflow = TextOverflow.Ellipsis)
        }
        trailing?.invoke()
        if (chevron) Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = Kino.faint)
    }
}

@Composable
fun Dot(color: Color, size: Dp = 8.dp) = Box(Modifier.size(size).clip(CircleShape).background(color))

@Composable
fun SearchField(value: String, onChange: (String) -> Unit, placeholder: String, modifier: Modifier = Modifier, onSubmit: (() -> Unit)? = null) {
    OutlinedTextField(value, onChange, modifier = modifier.fillMaxWidth(), singleLine = true, shape = CardShape,
        leadingIcon = { Icon(Icons.Outlined.Search, null, tint = Kino.muted) },
        placeholder = { Text(placeholder, color = Kino.faint) },
        keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(imeAction = androidx.compose.ui.text.input.ImeAction.Search),
        keyboardActions = androidx.compose.foundation.text.KeyboardActions(onSearch = { onSubmit?.invoke() }))
}

/** A horizontally scrolling row of single-choice chips (filters, sort keys). */
@Composable
fun <T> ChipRow(options: List<Pair<T, String>>, selected: T, modifier: Modifier = Modifier, onSelect: (T) -> Unit) {
    Row(modifier.fillMaxWidth().horizontalScroll(androidx.compose.foundation.rememberScrollState()).padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.forEach { (v, label) ->
            FilterChip(selected = v == selected, onClick = { onSelect(v) }, label = { Text(label) },
                colors = FilterChipDefaults.filterChipColors(selectedContainerColor = Kino.text, selectedLabelColor = Kino.bg, labelColor = Kino.muted, containerColor = Kino.surface),
                border = FilterChipDefaults.filterChipBorder(enabled = true, selected = v == selected, borderColor = Kino.border))
        }
    }
}

/** A small stat: a big number over a caption. */
@Composable
fun Stat(value: String, caption: String, color: Color = Kino.text) {
    Column {
        Text(value, color = color, fontSize = 20.sp, fontWeight = FontWeight.Bold)
        Text(caption, color = Kino.faint, fontSize = 12.sp)
    }
}

/** A section heading inside a page. */
@Composable
fun Heading(text: String, modifier: Modifier = Modifier) {
    Text(text.uppercase(), style = Label, color = Kino.faint, modifier = modifier.padding(start = 16.dp, end = 16.dp, top = 18.dp, bottom = 4.dp))
}

/** A thin progress bar (download progress in the queue). */
@Composable
fun Progress(fraction: Float, modifier: Modifier = Modifier) {
    Box(modifier.fillMaxWidth().height(4.dp).clip(RoundedCornerShape(2.dp)).background(Kino.surface3)) {
        Box(Modifier.fillMaxHeight().fillMaxWidth(fraction.coerceIn(0f, 1f)).background(LocalAccent.current))
    }
}

/** A bordered surface like [Card] but without the heading, for list groups. */
@Composable
fun Panel(modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    Surface(modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp), shape = RoundedCornerShape(8.dp), color = Kino.surface, border = BorderStroke(1.dp, Kino.border)) {
        Column(content = content)
    }
}

/** Local date-time for an ISO timestamp from a service. */
fun when_(iso: String?): String = shortDateTime(iso)
