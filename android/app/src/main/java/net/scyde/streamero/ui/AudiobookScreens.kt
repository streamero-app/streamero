package net.scyde.streamero.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Bedtime
import androidx.compose.material.icons.filled.Forward30
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Replay
import androidx.compose.material.icons.filled.Replay30
import androidx.compose.material.icons.filled.Speed
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
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.books.SLEEP_CHOICES
import net.scyde.streamero.books.SLEEP_END_OF_CHAPTER
import net.scyde.streamero.books.SPEEDS
import net.scyde.streamero.books.formatIndex

private fun speedLabel(s: Float) = formatIndex(s.toDouble()) + "×"

/** The audiobook player: cover, chapter, position in the whole book, skips, speed and sleep timer. */
@Composable
fun AudiobookPlayerScreen(state: AppState, nav: NavHostController, id: String) {
    val api = state.api ?: return
    val p = state.book
    val b = p.book
    // Opened from a link or after the process restarted: start the book first.
    val (detail) = rememberLoad(id) { if (p.book?.id == id) null else api.item(id) }
    LaunchedEffect(detail) { detail?.let { if (p.book?.id != id) p.play(it.item) } }
    if (b == null || b.id != id || p.timeline == null) {
        Box(Modifier.fillMaxSize().statusBarsPadding().padding(20.dp)) { if (p.error != null) ErrorLine(p.error) else Loading(Modifier.fillMaxSize()) }
        return
    }
    val t = p.timeline!!
    val accent = hexColor(b.colors?.firstOrNull())
    var speedOpen by remember { mutableStateOf(false) }
    var sleepOpen by remember { mutableStateOf(false) }
    var chaptersOpen by remember { mutableStateOf(false) }
    // The sleep timer's countdown is shown by the clock, which ticks every second.
    var now by remember { mutableLongStateOf(0L) }
    LaunchedEffect(p.sleep) { while (p.sleep != null) { now++; delay(1000) } }
    val chapter = t.chapters.getOrNull(p.chapter)
    CompositionLocalProvider(LocalAccent provides accent) {
        Column(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(accent.copy(alpha = 0.3f), Kino.bg, Kino.bg)))
            .systemBarsPadding().padding(horizontal = 24.dp, vertical = 8.dp), horizontalAlignment = Alignment.CenterHorizontally) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                IconButton(onClick = { nav.popBackStack() }) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Kino.text) }
                Spacer(Modifier.weight(1f))
                TextButton(onClick = { nav.navigate("item/${b.id}") }) { Text(stringResource(R.string.details), color = Kino.muted) }
            }
            Spacer(Modifier.weight(0.4f))
            BookCover(api, b, Modifier.fillMaxWidth(0.78f))
            Spacer(Modifier.height(24.dp))
            Text(b.title, style = Display.copy(fontSize = 26.sp), color = Kino.text, maxLines = 2, overflow = TextOverflow.Ellipsis)
            b.authors?.joinToString(", ")?.let { Text(it, color = Kino.muted, maxLines = 1) }
            if (chapter != null) Text(chapter.title, color = LocalAccent.current, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(top = 8.dp).clickable { chaptersOpen = true })
            Spacer(Modifier.weight(0.3f))
            var drag by remember { mutableStateOf<Float?>(null) }
            Slider(value = drag ?: p.position.toFloat(), valueRange = 0f..t.durationMs.coerceAtLeast(1).toFloat(),
                onValueChange = { drag = it }, onValueChangeFinished = { drag?.let { v -> p.seekTo(v.toLong()) }; drag = null },
                colors = SliderDefaults.colors(thumbColor = Kino.text, activeTrackColor = LocalAccent.current))
            Row(Modifier.fillMaxWidth()) {
                Text(clock(drag?.toLong() ?: p.position), color = Kino.faint, fontSize = 12.sp)
                Spacer(Modifier.weight(1f))
                Text("-" + clock(((t.durationMs - p.position) / p.speed).toLong().coerceAtLeast(0)), color = Kino.faint, fontSize = 12.sp)
            }
            Row(Modifier.fillMaxWidth().padding(vertical = 12.dp), horizontalArrangement = Arrangement.SpaceEvenly, verticalAlignment = Alignment.CenterVertically) {
                IconButton(onClick = { p.skip(-30_000) }, Modifier.size(56.dp)) { Icon(Icons.Default.Replay30, stringResource(R.string.back_30), tint = Kino.text, modifier = Modifier.size(34.dp)) }
                IconButton(onClick = { p.skip(-15_000) }, Modifier.size(56.dp)) { Icon(Icons.Default.Replay, stringResource(R.string.back_15), tint = Kino.text, modifier = Modifier.size(30.dp)) }
                FilledIconButton(onClick = { p.toggle() }, colors = IconButtonDefaults.filledIconButtonColors(containerColor = Kino.text, contentColor = Kino.bg), modifier = Modifier.size(72.dp)) {
                    Icon(if (p.playing) Icons.Default.Pause else Icons.Default.PlayArrow, stringResource(if (p.playing) R.string.pause else R.string.play), modifier = Modifier.size(38.dp))
                }
                IconButton(onClick = { p.skip(30_000) }, Modifier.size(56.dp)) { Icon(Icons.Default.Forward30, stringResource(R.string.forward_30), tint = Kino.text, modifier = Modifier.size(34.dp)) }
            }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                GlassButton(speedLabel(p.speed), { speedOpen = true }, Modifier.weight(1f), icon = Icons.Default.Speed, active = p.speed != 1f)
                val left = now.let { p.sleepLeftMs() }
                GlassButton(left?.let { clock(it) } ?: stringResource(R.string.sleep_timer), { sleepOpen = true }, Modifier.weight(1f), icon = Icons.Default.Bedtime, active = p.sleep != null)
                if (t.chapters.size > 1) GlassButton(stringResource(R.string.chapters), { chaptersOpen = true }, Modifier.weight(1f))
            }
            Spacer(Modifier.weight(0.3f))
        }
    }
    if (speedOpen) ChoiceDialog(SPEEDS.map { it to speedLabel(it) }, p.speed, stringResource(R.string.speed), { speedOpen = false }) { p.changeSpeed(it); speedOpen = false }
    if (sleepOpen) {
        // -1 stands for "off" in the list.
        ChoiceDialog(listOf(-1 to stringResource(R.string.off)) + SLEEP_CHOICES.map { m ->
            m to if (m == SLEEP_END_OF_CHAPTER) stringResource(R.string.sleep_end_of_chapter) else stringResource(R.string.minutes_count, m)
        }, p.sleep ?: -1, stringResource(R.string.sleep_timer), { sleepOpen = false }) { m -> p.startSleepTimer(m.takeIf { it >= 0 }); sleepOpen = false }
    }
    if (chaptersOpen) ChaptersSheet(state) { chaptersOpen = false }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ChaptersSheet(state: AppState, onClose: () -> Unit) {
    val p = state.book
    val chapters = p.timeline?.chapters.orEmpty()
    val current = p.chapter
    val list = rememberLazyListState(initialFirstVisibleItemIndex = (current - 2).coerceAtLeast(0))
    ModalBottomSheet(onDismissRequest = onClose, containerColor = Kino.surface) {
        Text(stringResource(R.string.chapters).uppercase(), style = Head, color = Kino.text, modifier = Modifier.padding(horizontal = 20.dp, vertical = 6.dp))
        LazyColumn(Modifier.heightIn(max = 520.dp), state = list, contentPadding = PaddingValues(bottom = 30.dp)) {
            itemsIndexed(chapters) { i, c ->
                val active = i == current
                Row(Modifier.fillMaxWidth().clickable { p.jumpToChapter(i); onClose() }.padding(horizontal = 20.dp, vertical = 12.dp),
                    horizontalArrangement = Arrangement.spacedBy(14.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text("${i + 1}", color = if (active) LocalAccent.current else Kino.faint, fontSize = 14.sp, modifier = Modifier.width(28.dp))
                    Text(c.title, color = if (active) LocalAccent.current else Kino.text, fontWeight = if (active) FontWeight.SemiBold else FontWeight.Normal,
                        modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(clock((c.endMs - c.startMs).coerceAtLeast(0)), color = Kino.faint, fontSize = 13.sp)
                }
            }
        }
    }
}

/** The audiobook bar above the tabs while a book is open; tap for the player. */
@Composable
fun AudiobookMiniBar(state: AppState, nav: NavHostController) {
    val p = state.book
    val b = p.book ?: return
    val api = state.api ?: return
    Column(Modifier.background(Kino.surface)) {
        val pct = if (p.duration > 0) (p.position.toFloat() / p.duration).coerceIn(0f, 1f) else 0f
        Box(Modifier.fillMaxWidth().height(2.dp).background(Color.White.copy(alpha = 0.12f))) { Box(Modifier.fillMaxHeight().fillMaxWidth(pct).background(LocalAccent.current)) }
        Row(Modifier.fillMaxWidth().clickable { nav.navigate("listen/${b.id}") }.padding(horizontal = 12.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Box(Modifier.size(44.dp).clip(CardShape).background(Kino.surface2)) { if (b.hasPoster) ServerImage(api.imageUrl(b.id, "poster"), Modifier.fillMaxSize()) }
            Column(Modifier.weight(1f)) {
                Text(b.title, color = Kino.text, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(p.timeline?.chapters?.getOrNull(p.chapter)?.title ?: b.authors?.joinToString(", ").orEmpty(), color = Kino.faint, fontSize = 12.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            IconButton(onClick = { p.skip(-15_000) }) { Icon(Icons.Default.Replay, stringResource(R.string.back_15), tint = Kino.text) }
            IconButton(onClick = { p.toggle() }) { Icon(if (p.playing) Icons.Default.Pause else Icons.Default.PlayArrow, stringResource(R.string.play), tint = Kino.text) }
        }
    }
}
