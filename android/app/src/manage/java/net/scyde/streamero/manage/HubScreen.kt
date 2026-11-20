package net.scyde.streamero.manage

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.TrendingUp
import androidx.compose.material.icons.automirrored.outlined.List
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.ui.*

/** What the hub shows under a reachable service: health warnings and work in progress. */
data class HubStats(val warnings: List<String> = emptyList(), val counts: List<Pair<Int, Long>> = emptyList())

private suspend fun stats(c: ArrClient): HubStats = attempt {
    when (c.service) {
        ArrService.Radarr, ArrService.Sonarr, ArrService.Lidarr -> coroutineScope {
            val health = async { attempt { c.list("health") }.getOrDefault(emptyList()) }
            val queue = async { attempt { c.obj("queue/status") }.getOrNull() }
            val missing = async { attempt { c.obj("wanted/missing", mapOf("page" to 1, "pageSize" to 1, "monitored" to true)).l("totalRecords") }.getOrNull() }
            HubStats(health.await().mapNotNull { it.s("message") },
                listOfNotNull(queue.await()?.l("totalCount")?.let { R.string.m_queue to it }, missing.await()?.let { R.string.m_missing to it }))
        }
        ArrService.Prowlarr -> coroutineScope {
            val health = async { attempt { c.list("health") }.getOrDefault(emptyList()) }
            val idx = async { attempt { c.list("indexer") }.getOrDefault(emptyList()) }
            HubStats(health.await().mapNotNull { it.s("message") }, listOf(R.string.m_indexers to idx.await().size.toLong()))
        }
        ArrService.Bazarr -> {
            val b = c.obj("badges")
            HubStats(emptyList(), listOfNotNull(
                b.l("movies")?.let { R.string.m_missing_movie_subs to it }, b.l("episodes")?.let { R.string.m_missing_episode_subs to it },
                b.l("providers")?.let { R.string.m_throttled_providers to it }))
        }
        ArrService.Seerr -> {
            val r = c.obj("request/count")
            HubStats(emptyList(), listOfNotNull(r.l("pending")?.let { R.string.m_pending to it }, r.l("processing")?.let { R.string.m_processing to it }))
        }
    }
}.getOrDefault(HubStats())

@Composable
fun HubScreen(state: AppState, nav: NavHostController) {
    val api = state.api ?: return
    if (LocalTv.current) {
        Column(Modifier.fillMaxSize().padding(48.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(stringResource(R.string.nav_manage), style = Display.copy(fontSize = 34.sp), color = Kino.text)
            Text(stringResource(R.string.m_tv_note), color = Kino.muted, fontSize = 18.sp)
        }
        return
    }
    val services = rememberLoaded(api, pollMs = 10_000) {
        val list = ArrClient.services(api)
        ManageCache.services = list
        coroutineScope {
            list.map { s -> async { s to (ArrService.of(s.id)?.takeIf { s.configured && s.reachable }?.let { stats(ArrClient(api, it, s.apiBase)) } ?: HubStats()) } }.awaitAll()
        }
    }
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 32.dp)) {
        item {
            Column(Modifier.statusBarsPadding().padding(start = 20.dp, end = 20.dp, top = 20.dp, bottom = 8.dp)) {
                Text(stringResource(R.string.nav_manage), style = Display.copy(fontSize = 34.sp), color = Kino.text)
                Text(stringResource(R.string.m_hub_intro), color = Kino.muted, fontSize = 14.sp)
            }
        }
        loaded(services) { list ->
            if (list.none { it.first.configured }) block {
                Notice(Kino.accent) {
                    Text(stringResource(R.string.m_none_configured), color = Kino.text, fontSize = 14.sp)
                    GlassButton(stringResource(R.string.m_connection_settings), { nav.navigate("settings/requests") })
                }
            }
            items(list, key = { it.first.id }) { (s, st) -> ServiceCard(nav, s, st) }
        }
    }
}

@Composable
private fun ServiceCard(nav: NavHostController, s: ArrServiceStatus, st: HubStats) {
    val svc = ArrService.of(s.id) ?: return
    val ok = s.configured && s.reachable
    Surface(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp).clickable {
        if (ok) nav.manage(svc, "home") else nav.navigate("settings/requests")
    }, shape = RoundedCornerShape(10.dp), color = Kino.surface, border = BorderStroke(1.dp, Kino.border)) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Dot(when { ok -> Kino.success; s.configured -> Kino.danger; else -> Kino.faint }, 10.dp)
                Text(svc.title, style = Head, color = Kino.text, modifier = Modifier.weight(1f))
                Pill(when {
                    ok -> s.version?.let { "v$it" } ?: stringResource(R.string.m_online)
                    s.configured -> stringResource(R.string.m_unreachable)
                    else -> stringResource(R.string.m_not_configured)
                }, if (ok) Kino.muted else if (s.configured) Kino.danger else Kino.faint)
            }
            when {
                !s.configured -> Text(stringResource(R.string.m_not_configured_hint, svc.title), color = Kino.muted, fontSize = 13.sp)
                !s.reachable -> Text(s.error ?: stringResource(R.string.m_err_unreachable), color = Kino.danger, fontSize = 13.sp)
                else -> {
                    if (st.counts.isNotEmpty()) Row(horizontalArrangement = Arrangement.spacedBy(24.dp)) {
                        st.counts.forEach { (res, n) -> Stat(n.toString(), stringResource(res)) }
                    }
                    st.warnings.take(3).forEach { w ->
                        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                            Icon(Icons.Outlined.WarningAmber, null, tint = Kino.accent, modifier = Modifier.size(16.dp))
                            Text(w, color = Kino.muted, fontSize = 13.sp)
                        }
                    }
                    if (st.warnings.size > 3) Text(plural(R.plurals.m_more_warnings, st.warnings.size - 3), color = Kino.faint, fontSize = 12.sp)
                }
            }
        }
    }
}

private data class Entry(val page: String, val label: Int, val icon: ImageVector, val arg: String? = null)

private fun sections(svc: ArrService): List<Entry> = when (svc) {
    ArrService.Radarr, ArrService.Sonarr, ArrService.Lidarr -> listOf(
        Entry("library", R.string.m_library, Icons.Outlined.VideoLibrary),
        Entry("add", R.string.m_add_new, Icons.Outlined.AddCircleOutline),
        Entry("calendar", R.string.m_calendar, Icons.Outlined.CalendarMonth),
        Entry("queue", R.string.m_queue, Icons.Outlined.Downloading),
        Entry("history", R.string.m_history, Icons.Outlined.History),
        Entry("blocklist", R.string.m_blocklist, Icons.Outlined.Block),
        Entry("wanted", R.string.m_missing, Icons.Outlined.SearchOff),
        Entry("wanted", R.string.m_cutoff_unmet, Icons.AutoMirrored.Outlined.TrendingUp, "cutoff"),
        Entry("system", R.string.m_system, Icons.Outlined.MonitorHeart),
        Entry("settings", R.string.settings, Icons.Outlined.Settings),
    )
    ArrService.Prowlarr -> listOf(
        Entry("providers", R.string.m_indexers, Icons.AutoMirrored.Outlined.List, "indexer"),
        Entry("search", R.string.search, Icons.Outlined.Search),
        Entry("providers", R.string.m_apps, Icons.Outlined.Apps, "applications"),
        Entry("stats", R.string.m_stats, Icons.Outlined.BarChart),
        Entry("history", R.string.m_history, Icons.Outlined.History),
        Entry("system", R.string.m_system, Icons.Outlined.MonitorHeart),
        Entry("settings", R.string.settings, Icons.Outlined.Settings),
    )
    ArrService.Bazarr -> listOf(
        Entry("bwanted", R.string.m_wanted_movies, Icons.Outlined.Movie, "movies"),
        Entry("bwanted", R.string.m_wanted_episodes, Icons.Outlined.Tv, "episodes"),
        Entry("bhistory", R.string.m_history_movies, Icons.Outlined.History, "movies"),
        Entry("bhistory", R.string.m_history_episodes, Icons.Outlined.History, "episodes"),
        Entry("bproviders", R.string.m_providers, Icons.Outlined.Hub),
        Entry("bsystem", R.string.m_system, Icons.Outlined.MonitorHeart),
        Entry("bsettings", R.string.settings, Icons.Outlined.Settings),
    )
    ArrService.Seerr -> listOf(
        Entry("requests", R.string.m_requests, Icons.Outlined.Inbox),
        Entry("discover", R.string.nav_discover, Icons.Outlined.Explore),
        Entry("issues", R.string.m_issues, Icons.Outlined.ReportProblem),
        Entry("users", R.string.users, Icons.Outlined.Group),
        Entry("sjobs", R.string.m_jobs, Icons.Outlined.Schedule),
        Entry("ssettings", R.string.settings, Icons.Outlined.Settings),
    )
}

@Composable
fun ServiceHome(nav: NavHostController, c: ArrClient) {
    val status = ManageCache.services.firstOrNull { it.id == c.service.id }
    ManagePage(nav, c.service.title, status?.version?.let { "v$it" }) {
        items(sections(c.service)) { s ->
            ListRow(stringResource(s.label), leading = { Icon(s.icon, null, tint = Kino.muted) }, chevron = true) { nav.manage(c.service, s.page, arg = s.arg) }
        }
    }
}

