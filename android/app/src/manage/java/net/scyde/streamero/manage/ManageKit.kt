package net.scyde.streamero.manage

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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

@Composable
fun <T> rememberLoaded(key: Any?, pollMs: Long = 0, load: suspend () -> T): Loaded<T> {
    var value by remember(key) { mutableStateOf<T?>(null) }
    var error by remember(key) { mutableStateOf<Throwable?>(null) }
    var loading by remember(key) { mutableStateOf(true) }
    var tick by remember(key) { mutableIntStateOf(0) }
    val owner = LocalLifecycleOwner.current
    LaunchedEffect(key, tick) {
        suspend fun once() {
            loading = true
            try { value = load(); error = null } catch (e: CancellationException) { throw e } catch (e: Throwable) { error = e }
            loading = false
        }
        if (pollMs <= 0) once()
        else owner.lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) { while (true) { once(); delay(pollMs) } }
    }
    return Loaded(value, error, loading) { tick++ }
}

/** Runs one user action at a time and keeps its outcome (busy, error, a done message). */
class ActionState {
    var busy by mutableStateOf(false)
    var error by mutableStateOf<Throwable?>(null)
    var done by mutableStateOf<String?>(null)
}

@Composable
fun rememberAction(): Pair<ActionState, (String?, suspend () -> Unit) -> Unit> {
    val s = remember { ActionState() }
    val scope = rememberCoroutineScope()
    return s to { msg, block ->
        if (!s.busy) scope.launch {
            s.busy = true; s.error = null; s.done = null
            try { block(); s.done = msg } catch (e: CancellationException) { throw e } catch (e: Throwable) { s.error = e }
            s.busy = false
        }
    }
}

@Composable
fun ActionOutcome(s: ActionState) {
    s.done?.let { Text(it, color = Kino.success, fontSize = 14.sp) }
    ArrError(s.error)
}

/** A translated message for a failed call; the services' own messages are shown as they are. */
@Composable
fun arrErrorText(e: Throwable?): String? {
    if (e == null) return null
    if (e is ArrException) return when (e.code) {
        "arr_not_configured" -> stringResource(R.string.m_err_not_configured)
        "arr_unreachable" -> stringResource(R.string.m_err_unreachable)
        "forbidden" -> stringResource(R.string.err_forbidden)
        "unauthenticated" -> stringResource(R.string.m_err_signed_out)
        "invalid_path" -> stringResource(R.string.m_err_invalid_path)
        "too_large" -> stringResource(R.string.m_err_too_large)
        else -> stringResource(R.string.m_err_service, e.message.orEmpty())
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
    LazyColumn(Modifier.fillMaxSize().background(Kino.bg), contentPadding = PaddingValues(bottom = 32.dp)) {
        item {
            Row(Modifier.statusBarsPadding().padding(start = 4.dp, end = 12.dp, top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                IconButton(onClick = { nav.popBackStack() }) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Kino.text) }
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

/** A tappable row: optional poster, title, a muted second line and something on the right. */
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
    onClick: (() -> Unit)? = null,
) {
    Row(modifier.fillMaxWidth().then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier).padding(horizontal = 16.dp, vertical = 10.dp),
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
