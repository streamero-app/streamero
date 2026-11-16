package net.scyde.streamero.manage

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import kotlinx.serialization.json.JsonObject
import net.scyde.streamero.R
import net.scyde.streamero.ui.*
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

/** Upcoming (and the last week's) releases of the monitored library, by day. */
@Composable
fun CalendarPage(nav: NavHostController, c: ArrClient) {
    val s = c.service
    val today = remember { LocalDate.now() }
    val cal = rememberLoaded(s) {
        c.list("calendar", mapOf("start" to today.minusDays(7).toString(), "end" to today.plusDays(42).toString(), "unmonitored" to false) + Lib.include(s))
    }
    ManagePage(nav, stringResource(R.string.m_calendar), s.title) {
        loaded(cal) { list ->
            val dated = list.mapNotNull { o -> calendarDate(s, o, today)?.let { it to o } }.sortedBy { it.first }
            if (dated.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
            dated.groupBy { it.first }.forEach { (day, rows) ->
                item { Heading(day.format(DateTimeFormatter.ofLocalizedDate(FormatStyle.FULL)) + if (day == today) " · " + stringResource(R.string.m_today) else "") }
                items(rows) { (_, o) ->
                    val mainId = (o.o("series") ?: o.o("artist"))?.i("id") ?: o.i("seriesId") ?: o.i("artistId") ?: o.i("id")
                    ListRow(Lib.recordTitle(s, o), listOfNotNull(
                        if (o.b("hasFile") == true || (o.o("statistics")?.d("percentOfTracks") ?: 0.0) >= 100) stringResource(R.string.m_downloaded) else null,
                        o.s("network") ?: o.o("series")?.s("network")).joinToString(" · "), image = Lib.poster(c, o.o("series") ?: o.o("artist") ?: o)) {
                        mainId?.let { nav.manage(s, "item", it.toString()) }
                    }
                }
            }
        }
    }
}

private fun calendarDate(s: ArrService, o: JsonObject, today: LocalDate): LocalDate? {
    fun parse(v: String?): LocalDate? = v?.let {
        runCatching { OffsetDateTime.parse(it).atZoneSameInstant(ZoneId.systemDefault()).toLocalDate() }.getOrNull() ?: runCatching { LocalDate.parse(it.take(10)) }.getOrNull()
    }
    return when (s) {
        ArrService.Sonarr -> parse(o.s("airDateUtc")) ?: parse(o.s("airDate"))
        ArrService.Lidarr -> parse(o.s("releaseDate"))
        // A movie has several dates; the one nearest to today in the window.
        else -> listOfNotNull(parse(o.s("inCinemas")), parse(o.s("digitalRelease")), parse(o.s("physicalRelease")))
            .filter { !it.isBefore(today.minusDays(7)) }.minOrNull()
    }
}

/** Downloads in progress (polled every 5 s while shown): progress, state, remove/blocklist. */
@Composable
fun QueuePage(nav: NavHostController, c: ArrClient) {
    val s = c.service
    val q = rememberLoaded(s, pollMs = 5_000) { c.obj("queue", mapOf("page" to 1, "pageSize" to 200) + Lib.include(s)) }
    var removing by remember { mutableStateOf<JsonObject?>(null) }
    val (act, run) = rememberAction()
    ManagePage(nav, stringResource(R.string.m_queue), q.value?.l("totalRecords")?.let { itemsCount(it.toInt()) }) {
        block { ActionOutcome(act) }
        loaded(q) { page ->
            val list = page.objs("records")
            if (list.isEmpty()) block { Text(stringResource(R.string.m_queue_empty), color = Kino.muted) }
            items(list, key = { it.i("id") ?: it.hashCode() }) { r ->
                val size = r.d("size") ?: 0.0
                val left = r.d("sizeleft") ?: 0.0
                Panel {
                    Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text(Lib.recordTitle(s, r), color = Kino.text, fontSize = 15.sp)
                        Text(r.s("title").orEmpty(), color = Kino.faint, fontSize = 12.sp, maxLines = 2)
                        if (size > 0) Progress(((size - left) / size).toFloat())
                        Text(listOfNotNull(r.s("trackedDownloadState")?.let(::humanize) ?: r.s("status")?.let(::humanize), r.o("quality")?.o("quality")?.s("name"),
                            if (size > 0) "${bytes((size - left).toLong())} / ${bytes(size.toLong())}" else null, r.s("timeleft"), r.s("downloadClient"), r.s("indexer")).joinToString(" · "),
                            color = Kino.muted, fontSize = 12.sp)
                        r.objs("statusMessages").flatMap { it.strs("messages") }.take(3).forEach { Text("• $it", color = Kino.accent, fontSize = 12.sp) }
                        r.s("errorMessage")?.let { Text(it, color = Kino.danger, fontSize = 12.sp) }
                        Row(Modifier.fillMaxWidth(), horizontalArrangement = androidx.compose.foundation.layout.Arrangement.End) {
                            TextButton(onClick = { removing = r }) { Text(stringResource(R.string.m_remove), color = Kino.danger) }
                        }
                    }
                }
            }
        }
    }
    removing?.let { r ->
        var fromClient by remember { mutableStateOf(true) }
        var blocklist by remember { mutableStateOf(false) }
        AlertDialog(onDismissRequest = { removing = null }, containerColor = Kino.surface,
            title = { Text(stringResource(R.string.m_remove_from_queue), color = Kino.text) },
            text = {
                Column {
                    Text(r.s("title").orEmpty(), color = Kino.muted, fontSize = 13.sp)
                    Toggle(stringResource(R.string.m_remove_from_client), null, fromClient) { fromClient = it }
                    Toggle(stringResource(R.string.m_add_to_blocklist), stringResource(R.string.m_blocklist_hint), blocklist) { blocklist = it }
                }
            },
            confirmButton = {
                TextButton(onClick = {
                    removing = null
                    run(null) { c.delete("queue/${r.i("id")}", mapOf("removeFromClient" to fromClient, "blocklist" to blocklist)); q.reload() }
                }) { Text(stringResource(R.string.m_remove), color = Kino.danger) }
            },
            dismissButton = { TextButton(onClick = { removing = null }) { Text(stringResource(R.string.cancel), color = Kino.muted) } })
    }
}

/** A paged list of records (`page`, `pageSize`, `totalRecords`, `records`) with "load more". */
@Composable
fun rememberPaged(key: Any?, pageSize: Int = 50, load: suspend (page: Int) -> JsonObject): Triple<Loaded<List<JsonObject>>, Boolean, () -> Unit> {
    var pages by remember(key) { mutableIntStateOf(1) }
    var total by remember(key) { mutableLongStateOf(0) }
    val l = rememberLoaded("$key/$pages") {
        (1..pages).flatMap { p -> load(p).also { total = it.l("totalRecords") ?: 0 }.objs("records") }
    }
    val more = (l.value?.size ?: 0) < total
    return Triple(l, more) { pages++ }
}

@Composable
fun HistoryPage(nav: NavHostController, c: ArrClient) {
    val s = c.service
    val (h, more, next) = rememberPaged(s) { p ->
        c.obj("history", mapOf("page" to p, "pageSize" to 50, "sortKey" to "date", "sortDirection" to "descending") + if (s.isLibrary) Lib.include(s) else emptyMap())
    }
    ManagePage(nav, stringResource(R.string.m_history), s.title) {
        loaded(h) { list ->
            if (list.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
            items(list) { r ->
                val title = if (s == ArrService.Prowlarr) (r.o("data")?.s("query")?.ifBlank { null } ?: r.o("data")?.s("grabTitle") ?: r.s("eventType") ?: "?") else Lib.recordTitle(s, r)
                ListRow(title, listOfNotNull(r.s("eventType")?.let(::humanize),
                    if (s == ArrService.Prowlarr) r.o("data")?.s("source") else r.s("sourceTitle"),
                    r.o("quality")?.o("quality")?.s("name"), r.b("successful")?.let { if (it) "✓" else "✕" }, when_(r.s("date"))).joinToString(" · "))
            }
            if (more) block { GlassButton(stringResource(R.string.m_load_more), next, Modifier.fillMaxWidth()) }
        }
    }
}

@Composable
fun BlocklistPage(nav: NavHostController, c: ArrClient) {
    val s = c.service
    var tick by remember { mutableIntStateOf(0) }
    val (b, more, next) = rememberPaged("$s/$tick") { p -> c.obj("blocklist", mapOf("page" to p, "pageSize" to 50, "sortKey" to "date", "sortDirection" to "descending") + Lib.include(s)) }
    val (act, run) = rememberAction()
    ManagePage(nav, stringResource(R.string.m_blocklist), s.title) {
        block { ActionOutcome(act) }
        loaded(b) { list ->
            if (list.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
            items(list) { r ->
                ListRow(r.s("sourceTitle") ?: "?", listOfNotNull(Lib.recordTitle(s, r).takeIf { it != r.s("sourceTitle") }, r.s("indexer"), r.s("message"), when_(r.s("date"))).joinToString(" · "),
                    trailing = { IconButton(onClick = { run(null) { c.delete("blocklist/${r.i("id")}"); tick++ } }) { Icon(Icons.Outlined.DeleteOutline, stringResource(R.string.m_remove), tint = Kino.danger) } })
            }
            if (more) block { GlassButton(stringResource(R.string.m_load_more), next, Modifier.fillMaxWidth()) }
        }
    }
}

/** Wanted: monitored items missing a file, or below the profile's cutoff; search one or all. */
@Composable
fun WantedPage(nav: NavHostController, c: ArrClient, cutoff: Boolean) {
    val s = c.service
    val res = if (cutoff) "wanted/cutoff" else "wanted/missing"
    val (w, more, next) = rememberPaged("$s/$res") { p ->
        c.obj(res, mapOf("page" to p, "pageSize" to 50, "monitored" to true, "sortDirection" to "descending",
            "sortKey" to when (s) { ArrService.Sonarr -> "episodes.airDateUtc"; ArrService.Lidarr -> "albums.releaseDate"; else -> "movieMetadata.sortTitle" }) + Lib.include(s))
    }
    val (act, run) = rememberAction()
    val queued = stringResource(R.string.m_command_queued)
    ManagePage(nav, stringResource(if (cutoff) R.string.m_cutoff_unmet else R.string.m_missing), s.title) {
        block {
            GlassButton(stringResource(R.string.m_search_all), { run(queued) { c.command(Lib.searchAllWanted(s, cutoff)) } }, Modifier.fillMaxWidth(), icon = Icons.Outlined.Search)
            ActionOutcome(act)
        }
        loaded(w) { list ->
            if (list.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
            items(list) { r ->
                val id = r.i("id") ?: return@items
                val mainId = when (s) { ArrService.Radarr -> id; ArrService.Sonarr -> r.i("seriesId"); else -> r.i("artistId") }
                ListRow(Lib.recordTitle(s, r), listOfNotNull(r.s("airDate") ?: r.s("releaseDate")?.take(10) ?: r.i("year")?.toString(),
                    r.o("movieFile")?.o("quality")?.o("quality")?.s("name")).joinToString(" · "),
                    image = Lib.poster(c, r.o("series") ?: r.o("artist") ?: r),
                    trailing = { IconButton(onClick = { val (n, a) = Lib.searchWanted(s, listOf(id)); run(queued) { c.command(n, *a) } }) { Icon(Icons.Outlined.Search, stringResource(R.string.m_auto_search), tint = Kino.muted) } }) {
                    mainId?.let { nav.manage(s, "item", it.toString()) }
                }
            }
            if (more) block { GlassButton(stringResource(R.string.m_load_more), next, Modifier.fillMaxWidth()) }
        }
    }
}
