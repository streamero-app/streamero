package net.scyde.streamero.ui

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.ClosedCaption
import androidx.compose.material.icons.filled.Download
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import java.util.Locale
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.SubtitleOffer
import net.scyde.streamero.api.SubtitleSearch

/** Whether this account may fetch subtitles (same rule as the web client). */
fun canFindSubtitles(state: AppState): Boolean {
    val me = state.me ?: return false
    return (me.isAdmin || me.permissions.editSubtitles) && me.currentProfile?.isKid != true
}

private fun languageName(code: String): String =
    Locale.forLanguageTag(code).getDisplayLanguage(Locale.getDefault()).ifBlank { code }

/** Search Bazarr's providers and download one offer, or let Bazarr fetch what is missing. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SubtitleFinder(state: AppState, itemId: String) {
    if (!canFindSubtitles(state)) return
    val api = state.api ?: return
    var open by remember { mutableStateOf(false) }
    GlassButton(stringResource(R.string.subs_find), { open = true }, Modifier.fillMaxWidth(), icon = Icons.Default.ClosedCaption)
    if (!open) return

    val scope = rememberCoroutineScope()
    var result by remember { mutableStateOf<SubtitleSearch?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var notice by remember { mutableStateOf<Int?>(null) }
    var busy by remember { mutableStateOf<SubtitleOffer?>(null) }
    var lang by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(itemId) { runCatching { api.searchSubtitles(itemId) }.onSuccess { result = it }.onFailure { error = it } }

    ModalBottomSheet(onDismissRequest = { open = false }, containerColor = Kino.bg) {
        Column(Modifier.fillMaxWidth().padding(horizontal = 20.dp).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(stringResource(R.string.subs_find).uppercase(), style = Head, color = Kino.text)
            error?.let { ErrorLine(it) }
            val r = result
            when {
                r == null && error == null -> Column(Modifier.fillMaxWidth().padding(vertical = 32.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    CircularProgressIndicator(color = Kino.text)
                    Text(stringResource(R.string.subs_searching), color = Kino.muted, fontSize = 14.sp)
                }
                r != null && !r.configured -> Text(stringResource(R.string.subs_not_configured), color = Kino.muted, fontSize = 15.sp)
                r != null -> {
                    GlassButton(stringResource(R.string.subs_auto), {
                        scope.launch { runCatching { api.searchMissingSubtitles(itemId) }.onSuccess { notice = R.string.subs_auto_started }.onFailure { error = it } }
                    }, Modifier.fillMaxWidth(), icon = Icons.Default.AutoAwesome)
                    notice?.let { Text(stringResource(it), color = Kino.success, fontSize = 14.sp) }
                    val langs = r.offers.map { it.language }.distinct()
                    if (langs.size > 1) Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        FilterChip(lang == null, { lang = null }, { Text(stringResource(R.string.subs_all)) })
                        langs.take(3).forEach { l -> FilterChip(lang == l, { lang = l }, { Text(languageName(l), maxLines = 1) }) }
                    }
                    val shown = r.offers.filter { lang == null || it.language == lang }
                    if (shown.isEmpty()) Text(stringResource(R.string.subs_none), color = Kino.muted, fontSize = 15.sp)
                    LazyColumn(Modifier.heightIn(max = 420.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        items(shown) { o ->
                            Row(Modifier.fillMaxWidth().padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                                Text("${o.score.toInt()}%", color = Kino.muted, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.width(44.dp))
                                Column(Modifier.weight(1f)) {
                                    val forced = if (o.forced) " · " + stringResource(R.string.subs_forced) else ""
                                    val hi = if (o.hearingImpaired) " · " + stringResource(R.string.subs_hi) else ""
                                    Text(languageName(o.language) + forced + hi, color = Kino.text, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                    Text(listOfNotNull(o.provider, o.uploader, o.release?.firstOrNull()).joinToString(" · "), color = Kino.muted, fontSize = 13.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                }
                                IconButton(enabled = busy == null, onClick = {
                                    busy = o
                                    scope.launch {
                                        runCatching { api.downloadSubtitle(itemId, o) }.onSuccess { notice = R.string.subs_downloaded }.onFailure { error = it }
                                        busy = null
                                    }
                                }) {
                                    if (busy === o) CircularProgressIndicator(Modifier.size(20.dp), color = Kino.text, strokeWidth = 2.dp)
                                    else Icon(Icons.Default.Download, stringResource(R.string.subs_download), tint = Kino.text)
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}
