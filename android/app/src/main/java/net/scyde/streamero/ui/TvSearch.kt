package net.scyde.streamero.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Backspace
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.SpaceBar
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import kotlinx.coroutines.delay
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.ItemSummary

const val KEY_SPACE = "␣"
const val KEY_DELETE = "⌫"
const val KEY_CLEAR = "✕"

/** What the query becomes after one key of the on-screen keyboard. */
fun typeKey(query: String, key: String): String = when (key) {
    KEY_DELETE -> query.dropLast(1)
    KEY_CLEAR -> ""
    KEY_SPACE -> if (query.isEmpty() || query.endsWith(" ")) query else "$query "
    else -> query + key
}

private val KEYS = ("abcdefghijklmnopqrstuvwxyz1234567890".map { it.toString() } + listOf("č", "ć", "đ", "š", "ž", "-")).chunked(7)

/**
 * Search on a TV: an on-screen keyboard driven by the remote (no system keyboard popping up
 * over the results), the results grid beside it.
 */
@Composable
fun TvSearchScreen(state: AppState, nav: NavHostController) {
    val api = state.api ?: return
    var q by rememberSaveable { mutableStateOf("") }
    var results by remember { mutableStateOf<List<ItemSummary>?>(null) }
    LaunchedEffect(q) {
        if (q.isBlank()) { results = null; return@LaunchedEffect }
        delay(350)
        results = runCatching { api.search(q.trim()) }.getOrDefault(emptyList())
    }
    val mem = rememberFocusMemory()
    Row(Modifier.fillMaxSize().padding(start = 28.dp, top = 28.dp)) {
        Column(Modifier.width(330.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Row(Modifier.fillMaxWidth().clip(CardShape).background(Kino.surface2).padding(horizontal = 16.dp, vertical = 14.dp),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Icon(Icons.Outlined.Search, null, tint = Kino.muted)
                if (q.isEmpty()) Text(stringResource(R.string.search_hint), color = Kino.faint, fontSize = 17.sp, maxLines = 1)
                else Text(q, color = Kino.text, fontSize = 19.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Clip)
            }
            Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                KEYS.forEach { row ->
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        row.forEach { k -> Key(k, Modifier.weight(1f).rememberFocus(mem, "k$k", default = k == "a")) { q = typeKey(q, k) } }
                        repeat(7 - row.size) { Spacer(Modifier.weight(1f)) }
                    }
                }
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    KeyIcon(Icons.Outlined.SpaceBar, stringResource(R.string.key_space), Modifier.weight(3f)) { q = typeKey(q, KEY_SPACE) }
                    KeyIcon(Icons.AutoMirrored.Outlined.Backspace, stringResource(R.string.key_delete), Modifier.weight(2f)) { q = typeKey(q, KEY_DELETE) }
                    Key(stringResource(R.string.key_clear), Modifier.weight(2f)) { q = typeKey(q, KEY_CLEAR) }
                }
            }
        }
        LazyVerticalGrid(GridCells.Adaptive(Dims.gridCell), Modifier.fillMaxSize(), contentPadding = PaddingValues(horizontal = 32.dp, vertical = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(gridGap()), verticalArrangement = Arrangement.spacedBy(18.dp)) {
            val r = results
            when {
                r == null -> item(span = { GridItemSpan(maxLineSpan) }) {
                    Text(stringResource(R.string.tv_search_intro), color = Kino.muted, fontSize = 16.sp, modifier = Modifier.padding(top = 12.dp))
                }
                r.isEmpty() -> item(span = { GridItemSpan(maxLineSpan) }) { Text(stringResource(R.string.nothing_found), color = Kino.muted, modifier = Modifier.padding(top = 12.dp)) }
                else -> items(r, key = { it.id }) { PosterCard(it, api::imageUrl, Dims.gridCell, state.enhancements.qualityTags, Modifier.rememberFocus(mem, it.id)) { nav.navigate("item/${it.id}") } }
            }
        }
    }
}

@Composable
private fun Key(label: String, modifier: Modifier, onClick: () -> Unit) {
    val (src, focused) = rememberFocusSource()
    Box(modifier.height(42.dp).focusRing(focused, scale = 1.12f, width = 0.dp).clip(CardShape).background(if (focused) Kino.text else Kino.surface)
        .clickable(interactionSource = src, indication = null, onClick = onClick), contentAlignment = Alignment.Center) {
        Text(label.uppercase(), color = if (focused) Kino.bg else Kino.text, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, textAlign = TextAlign.Center, maxLines = 1)
    }
}

@Composable
private fun KeyIcon(icon: androidx.compose.ui.graphics.vector.ImageVector, label: String, modifier: Modifier, onClick: () -> Unit) {
    val (src, focused) = rememberFocusSource()
    Box(modifier.height(42.dp).focusRing(focused, scale = 1.06f, width = 0.dp).clip(CardShape).background(if (focused) Kino.text else Kino.surface)
        .clickable(interactionSource = src, indication = null, onClick = onClick), contentAlignment = Alignment.Center) {
        Icon(icon, label, tint = if (focused) Kino.bg else Kino.text)
    }
}

