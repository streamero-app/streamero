package net.scyde.streamero.manage

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import kotlinx.serialization.json.JsonObject
import net.scyde.streamero.R
import net.scyde.streamero.ui.*

private fun tmdbImage(path: String?, size: String = "w185"): String? = path?.takeIf { it.isNotBlank() }?.let { "https://image.tmdb.org/t/p/$size$it" }

/** Request status codes from the Seerr API (1 pending, 2 approved, 3 declined, 4 failed, 5 completed). */
@Composable
private fun requestStatus(code: Int?): Pair<String, androidx.compose.ui.graphics.Color> = when (code) {
    1 -> stringResource(R.string.m_pending) to Kino.accent
    2 -> stringResource(R.string.m_approved) to Kino.success
    3 -> stringResource(R.string.m_declined) to Kino.danger
    4 -> stringResource(R.string.m_failed) to Kino.danger
    5 -> stringResource(R.string.m_completed) to Kino.success
    else -> (code?.toString() ?: "?") to Kino.muted
}

/** Media status codes (1 unknown, 2 pending, 3 processing, 4 partially available, 5 available, 6 blocklisted, 7 deleted). */
@Composable
private fun mediaStatus(code: Int?): String? = when (code) {
    2 -> stringResource(R.string.m_pending)
    3 -> stringResource(R.string.m_processing)
    4 -> stringResource(R.string.m_partially_available)
    5 -> stringResource(R.string.m_available)
    else -> null
}

/** The title of a request's media (requests only carry the TMDb id). */
@Composable
private fun rememberTitle(c: ArrClient, type: String?, tmdbId: Int?): JsonObject? {
    if (tmdbId == null || type == null) return null
    return rememberLoaded("$type/$tmdbId") { runCatching { c.obj("$type/$tmdbId") }.getOrNull() }.value
}

@Composable
fun SeerrRequests(nav: NavHostController, c: ArrClient) {
    var filter by remember { mutableStateOf("pending") }
    var tick by remember { mutableIntStateOf(0) }
    var take by remember(filter) { mutableIntStateOf(20) }
    val page = rememberLoaded("$filter/$tick/$take") { c.obj("request", mapOf("take" to take, "skip" to 0, "filter" to filter, "sort" to "added")) }
    val (act, run) = rememberAction()
    var confirmDelete by remember { mutableStateOf<Int?>(null) }
    ManagePage(nav, stringResource(R.string.m_requests), c.service.title) {
        item {
            ChipRow(listOf("pending" to stringResource(R.string.m_pending), "approved" to stringResource(R.string.m_approved), "processing" to stringResource(R.string.m_processing),
                "available" to stringResource(R.string.m_available), "failed" to stringResource(R.string.m_failed), "all" to stringResource(R.string.m_all)), filter) { filter = it }
        }
        block { ActionOutcome(act) }
        loaded(page) { p ->
            val list = p.objs("results")
            if (list.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
            items(list, key = { it.i("id") ?: 0 }) { r ->
                val id = r.i("id") ?: 0
                val type = r.s("type") ?: r.o("media")?.s("mediaType")
                val t = rememberTitle(c, type, r.o("media")?.i("tmdbId"))
                val (label, color) = requestStatus(r.i("status"))
                Panel {
                    Row(Modifier.padding(12.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        Box(Modifier.width(56.dp).aspectRatio(2f / 3f).clip(RoundedCornerShape(4.dp)).background(Kino.surface2)) { ServerImage(tmdbImage(t?.s("posterPath")), Modifier.fillMaxSize()) }
                        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                            Text(t?.s("title") ?: t?.s("name") ?: "TMDb ${r.o("media")?.i("tmdbId")}", color = Kino.text, fontWeight = FontWeight.Medium)
                            Text(listOfNotNull(type?.uppercase(), r.objs("seasons").takeIf { it.isNotEmpty() }?.let { s -> stringResource(R.string.m_seasons) + " " + s.mapNotNull { it.i("seasonNumber") }.joinToString(", ") },
                                if (r.b("is4k") == true) "4K" else null, r.o("requestedBy")?.s("displayName"), when_(r.s("createdAt"))).joinToString(" · "), color = Kino.muted, fontSize = 12.sp)
                            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                                Pill(label, color)
                                mediaStatus(r.o("media")?.i("status"))?.let { Pill(it) }
                            }
                            Actions {
                                if (r.i("status") == 1) {
                                    TextButton(onClick = { run(null) { c.post("request/$id/approve"); tick++ } }) { Text(stringResource(R.string.m_approve), color = Kino.success) }
                                    TextButton(onClick = { run(null) { c.post("request/$id/decline"); tick++ } }) { Text(stringResource(R.string.m_decline), color = Kino.danger) }
                                }
                                if (r.i("status") == 4) TextButton(onClick = { run(null) { c.post("request/$id/retry"); tick++ } }) { Text(stringResource(R.string.retry)) }
                                TextButton(onClick = { confirmDelete = id }) { Text(stringResource(R.string.delete), color = Kino.faint) }
                            }
                        }
                    }
                }
            }
            if ((p.o("pageInfo")?.i("results") ?: 0) > list.size) block { GlassButton(stringResource(R.string.m_load_more), { take += 20 }, Modifier.fillMaxWidth()) }
        }
    }
    confirmDelete?.let { id -> ConfirmDialog(stringResource(R.string.m_delete_request), stringResource(R.string.delete), { confirmDelete = null }) { run(null) { c.delete("request/$id"); tick++ } } }
}

/** Discover/search titles through Seerr and open one to request it. */
@Composable
fun SeerrDiscover(nav: NavHostController, c: ArrClient) {
    var q by remember { mutableStateOf("") }
    var submitted by remember { mutableStateOf("") }
    val results = rememberLoaded(submitted) {
        if (submitted.isBlank()) c.list("discover/trending", mapOf("page" to 1)) else c.list("search", mapOf("query" to submitted, "page" to 1))
    }
    ManagePage(nav, stringResource(R.string.nav_discover), c.service.title) {
        item { SearchField(q, { q = it }, stringResource(R.string.m_search_titles), Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) { submitted = q.trim() } }
        if (submitted.isBlank()) item { Heading(stringResource(R.string.m_trending)) }
        loaded(results) { list ->
            items(list.filter { it.s("mediaType") == "movie" || it.s("mediaType") == "tv" }) { r ->
                ListRow(r.s("title") ?: r.s("name") ?: "?", listOfNotNull((r.s("releaseDate") ?: r.s("firstAirDate"))?.take(4), r.s("mediaType")?.uppercase(),
                    mediaStatus(r.o("mediaInfo")?.i("status"))).joinToString(" · "), image = tmdbImage(r.s("posterPath")), chevron = true) {
                    nav.manage(c.service, "media", r.i("id").toString(), r.s("mediaType"))
                }
            }
        }
    }
}

@Composable
fun SeerrMedia(nav: NavHostController, c: ArrClient, type: String, tmdbId: Int) {
    var tick by remember { mutableIntStateOf(0) }
    val m = rememberLoaded("$type/$tmdbId/$tick") { c.obj("$type/$tmdbId") }
    var seasons by remember { mutableStateOf(setOf<Int>()) }
    var is4k by remember { mutableStateOf(false) }
    val (act, run) = rememberAction()
    val requested = stringResource(R.string.m_requested)
    ManagePage(nav, m.value?.let { it.s("title") ?: it.s("name") } ?: "…", type.uppercase()) {
        loaded(m) { o ->
            item {
                Row(Modifier.padding(16.dp), horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                    Box(Modifier.width(110.dp).aspectRatio(2f / 3f).clip(RoundedCornerShape(6.dp)).background(Kino.surface2)) { ServerImage(tmdbImage(o.s("posterPath"), "w342"), Modifier.fillMaxSize()) }
                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text(listOfNotNull((o.s("releaseDate") ?: o.s("firstAirDate"))?.take(4), mediaStatus(o.o("mediaInfo")?.i("status"))).joinToString(" · "), color = Kino.muted, fontSize = 13.sp)
                        Text(o.s("overview").orEmpty(), color = Kino.muted, fontSize = 13.sp, maxLines = 9)
                    }
                }
            }
            if (type == "tv") {
                item { Heading(stringResource(R.string.m_choose_seasons)) }
                items(o.objs("seasons").filter { (it.i("seasonNumber") ?: 0) > 0 }) { s ->
                    val n = s.i("seasonNumber") ?: 0
                    ListRow(s.s("name") ?: stringResource(R.string.m_season_n, n), itemsCount(s.i("episodeCount") ?: 0), trailing = {
                        Checkbox(n in seasons, { on -> seasons = if (on) seasons + n else seasons - n }, colors = CheckboxDefaults.colors(checkedColor = Kino.text, checkmarkColor = Kino.bg))
                    })
                }
            }
            block {
                Toggle("4K", null, is4k) { is4k = it }
                PrimaryButton(stringResource(R.string.m_request), {
                    run(requested) {
                        val body = jsonOf("mediaType" to type, "mediaId" to tmdbId, "is4k" to is4k) + if (type == "tv") mapOf("seasons" to toJson(seasons.sorted())) else emptyMap()
                        c.post("request", JsonObject(body)); tick++
                    }
                }, Modifier.fillMaxWidth(), enabled = !act.busy && (type != "tv" || seasons.isNotEmpty()), icon = Icons.Outlined.AddCircleOutline)
                ActionOutcome(act)
            }
            val requests = o.o("mediaInfo")?.objs("requests").orEmpty()
            if (requests.isNotEmpty()) {
                item { Heading(stringResource(R.string.m_requests)) }
                items(requests) { r -> val (l, _) = requestStatus(r.i("status")); ListRow(r.o("requestedBy")?.s("displayName") ?: "?", "$l · ${when_(r.s("createdAt"))}") }
            }
        }
    }
}

/** Issue types (1 video, 2 audio, 3 subtitles, 4 other). */
@Composable
private fun issueType(code: Int?): String = stringResource(when (code) { 1 -> R.string.m_issue_video; 2 -> R.string.m_issue_audio; 3 -> R.string.subtitles; else -> R.string.m_issue_other })

@Composable
fun SeerrIssues(nav: NavHostController, c: ArrClient) {
    var filter by remember { mutableStateOf("open") }
    var tick by remember { mutableIntStateOf(0) }
    val page = rememberLoaded("$filter/$tick") { c.obj("issue", mapOf("take" to 50, "skip" to 0, "filter" to filter, "sort" to "added")) }
    val (act, run) = rememberAction()
    ManagePage(nav, stringResource(R.string.m_issues), c.service.title) {
        item { ChipRow(listOf("open" to stringResource(R.string.m_open), "resolved" to stringResource(R.string.m_resolved), "all" to stringResource(R.string.m_all)), filter) { filter = it } }
        block { ActionOutcome(act) }
        loaded(page) { p ->
            val list = p.objs("results")
            if (list.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
            items(list) { r ->
                val id = r.i("id") ?: 0
                val t = rememberTitle(c, r.o("media")?.s("mediaType"), r.o("media")?.i("tmdbId"))
                Panel {
                    Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Text(t?.s("title") ?: t?.s("name") ?: "#$id", color = Kino.text, fontWeight = FontWeight.Medium)
                        Text(listOfNotNull(issueType(r.i("issueType")), r.o("createdBy")?.s("displayName"), when_(r.s("createdAt"))).joinToString(" · "), color = Kino.muted, fontSize = 12.sp)
                        r.objs("comments").firstOrNull()?.s("message")?.let { Text(it, color = Kino.text, fontSize = 13.sp) }
                        Actions {
                            if (r.i("status") == 1) TextButton(onClick = { run(null) { c.post("issue/$id/resolved"); tick++ } }) { Text(stringResource(R.string.m_resolve), color = Kino.success) }
                            else TextButton(onClick = { run(null) { c.post("issue/$id/open"); tick++ } }) { Text(stringResource(R.string.m_reopen)) }
                            TextButton(onClick = { run(null) { c.delete("issue/$id"); tick++ } }) { Text(stringResource(R.string.delete), color = Kino.faint) }
                        }
                    }
                }
            }
        }
    }
}

/** Seerr's permission bits as its public API documents them. */
val SEERR_PERMISSIONS: List<Pair<Long, String>> = listOf(
    2L to "ADMIN", 4L to "MANAGE_SETTINGS", 8L to "MANAGE_USERS", 16L to "MANAGE_REQUESTS", 32L to "REQUEST", 64L to "VOTE",
    128L to "AUTO_APPROVE", 256L to "AUTO_APPROVE_MOVIE", 512L to "AUTO_APPROVE_TV", 1024L to "REQUEST_4K", 2048L to "REQUEST_4K_MOVIE",
    4096L to "REQUEST_4K_TV", 8192L to "REQUEST_ADVANCED", 16384L to "REQUEST_VIEW", 32768L to "AUTO_APPROVE_4K", 65536L to "AUTO_APPROVE_4K_MOVIE",
    131072L to "AUTO_APPROVE_4K_TV", 262144L to "REQUEST_MOVIE", 524288L to "REQUEST_TV", 1048576L to "MANAGE_ISSUES", 2097152L to "VIEW_ISSUES",
    4194304L to "CREATE_ISSUES", 8388608L to "AUTO_REQUEST", 16777216L to "AUTO_REQUEST_MOVIE", 33554432L to "AUTO_REQUEST_TV",
    67108864L to "RECENT_VIEW", 134217728L to "WATCHLIST_VIEW",
)

fun seerrPermissionNames(bits: Long): List<String> = SEERR_PERMISSIONS.filter { (bit, _) -> bits and bit != 0L }.map { it.second }

@Composable
fun SeerrUsers(nav: NavHostController, c: ArrClient) {
    val users = rememberLoaded(c.service) { c.obj("user", mapOf("take" to 100, "skip" to 0, "sort" to "displayname")) }
    var open by remember { mutableStateOf<Int?>(null) }
    ManagePage(nav, stringResource(R.string.users), c.service.title) {
        loaded(users) { p ->
            items(p.objs("results")) { u ->
                val id = u.i("id") ?: 0
                ListRow(u.s("displayName") ?: u.s("email") ?: "?", listOfNotNull(u.s("email"), stringResource(R.string.m_requests) + ": " + (u.i("requestCount") ?: 0), when_(u.s("createdAt"))).joinToString(" · ")) {
                    open = if (open == id) null else id
                }
                if (open == id) {
                    val perms = seerrPermissionNames(u.l("permissions") ?: 0)
                    Text(if (perms.isEmpty()) stringResource(R.string.none) else perms.joinToString(" · ") { humanize(it.lowercase()) }, color = Kino.muted, fontSize = 12.sp, modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
                }
            }
        }
    }
}

@Composable
fun SeerrJobs(nav: NavHostController, c: ArrClient) {
    var tick by remember { mutableIntStateOf(0) }
    val jobs = rememberLoaded(tick, pollMs = 10_000) { c.list("settings/jobs") }
    val status = rememberLoaded(c.service) { c.obj("status") }
    val (act, run) = rememberAction()
    val queued = stringResource(R.string.m_command_queued)
    ManagePage(nav, stringResource(R.string.m_jobs), status.value?.s("version")?.let { "v$it" + if (status.value?.b("updateAvailable") == true) " · " + stringResource(R.string.m_update_available) else "" }) {
        block { ActionOutcome(act) }
        loaded(jobs) { list ->
            items(list) { j ->
                val id = j.s("id") ?: return@items
                ListRow(j.s("name") ?: id, listOfNotNull(j.s("interval")?.let(::humanize), j.s("nextExecutionTime")?.let { stringResource(R.string.m_next_run, when_(it)) }).joinToString(" · "), trailing = {
                    if (j.b("running") == true) TextButton(onClick = { run(null) { c.post("settings/jobs/$id/cancel"); tick++ } }) { Text(stringResource(R.string.cancel), color = Kino.danger) }
                    else IconButton(onClick = { run(queued) { c.post("settings/jobs/$id/run"); tick++ } }) { Icon(Icons.Outlined.PlayArrow, stringResource(R.string.run_now), tint = Kino.text) }
                })
            }
        }
    }
}

private val SEERR_AGENTS = listOf("email", "discord", "slack", "telegram", "pushover", "pushbullet", "gotify", "ntfy", "webhook", "webpush")

/** Seerr settings: main, network, Radarr/Sonarr servers and notification agents. */
@Composable
fun SeerrSettings(nav: NavHostController, c: ArrClient, arg: String?) {
    when {
        arg == null -> ManagePage(nav, stringResource(R.string.settings), c.service.title) {
            item { ListRow(stringResource(R.string.m_s_general), chevron = true) { nav.manage(c.service, "ssettings", arg = "doc:settings/main") } }
            item { ListRow(stringResource(R.string.m_s_network), chevron = true) { nav.manage(c.service, "ssettings", arg = "doc:settings/network") } }
            item { ListRow("Radarr", chevron = true) { nav.manage(c.service, "ssettings", arg = "servers:radarr") } }
            item { ListRow("Sonarr", chevron = true) { nav.manage(c.service, "ssettings", arg = "servers:sonarr") } }
            item { Heading(stringResource(R.string.notifications)) }
            items(SEERR_AGENTS) { a -> ListRow(humanize(a), chevron = true) { nav.manage(c.service, "ssettings", arg = "doc:settings/notifications/$a") } }
        }
        arg.startsWith("doc:") -> SeerrDoc(nav, c, arg.removePrefix("doc:"))
        arg.startsWith("servers:") -> SeerrServers(nav, c, arg.removePrefix("servers:"))
        arg.startsWith("server:") -> SeerrServer(nav, c, arg.removePrefix("server:").substringBefore('/'), arg.substringAfter('/', "new"))
    }
}

/** One settings document, saved with POST (Seerr's settings endpoints). */
@Composable
private fun SeerrDoc(nav: NavHostController, c: ArrClient, resource: String) {
    val doc = rememberLoaded(resource) { c.obj(resource) }
    var cur by remember(doc.value) { mutableStateOf(doc.value) }
    val (act, run) = rememberAction()
    val savedMsg = stringResource(R.string.saved)
    val testOk = stringResource(R.string.m_test_ok)
    ManagePage(nav, humanize(resource.substringAfterLast('/')), c.service.title) {
        loaded(doc) { original ->
            val o = cur ?: return@loaded
            block {
                JsonForm(o, { cur = it }, act.error, hide = emptySet())
                PrimaryButton(stringResource(R.string.save), { run(savedMsg) { (c.post(resource, normalizeGeneric(o, original)) as? JsonObject)?.let { cur = it } } }, Modifier.fillMaxWidth(), enabled = !act.busy && o != original)
                if (resource.startsWith("settings/notifications/")) GlassButton(stringResource(R.string.test), { run(testOk) { c.post("$resource/test", normalizeGeneric(o, original)) } }, Modifier.fillMaxWidth())
                ActionOutcome(act)
            }
        }
    }
}

@Composable
private fun SeerrServers(nav: NavHostController, c: ArrClient, kind: String) {
    val list = rememberLoaded(kind) { c.list("settings/$kind") }
    ManagePage(nav, if (kind == "radarr") "Radarr" else "Sonarr", c.service.title, actions = {
        IconButton(onClick = { nav.manage(c.service, "ssettings", arg = "server:$kind/new") }) { Icon(Icons.Outlined.Add, stringResource(R.string.add), tint = Kino.text) }
    }) {
        loaded(list) { l ->
            if (l.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
            items(l) { s ->
                ListRow(s.s("name") ?: "?", listOfNotNull("${s.s("hostname")}:${s.i("port")}", if (s.b("is4k") == true) "4K" else null, if (s.b("isDefault") == true) stringResource(R.string.m_default) else null).joinToString(" · "), chevron = true) {
                    nav.manage(c.service, "ssettings", arg = "server:$kind/${s.i("id")}")
                }
            }
        }
    }
}

@Composable
private fun SeerrServer(nav: NavHostController, c: ArrClient, kind: String, id: String) {
    val isNew = id == "new"
    val doc = rememberLoaded("$kind/$id") {
        if (isNew) jsonOf("name" to "", "hostname" to "", "port" to if (kind == "radarr") 7878 else 8989, "apiKey" to "", "useSsl" to false, "baseUrl" to "",
            "activeProfileId" to 0, "activeProfileName" to "", "activeDirectory" to "", "is4k" to false, "isDefault" to false, "externalUrl" to "", "syncEnabled" to false, "preventSearch" to false)
        else c.list("settings/$kind").first { it.i("id")?.toString() == id }
    }
    var cur by remember(doc.value) { mutableStateOf(doc.value) }
    var askDelete by remember { mutableStateOf(false) }
    val (act, run) = rememberAction()
    val savedMsg = stringResource(R.string.saved)
    val testOk = stringResource(R.string.m_test_ok)
    ManagePage(nav, cur?.s("name")?.ifBlank { null } ?: kind, c.service.title) {
        loaded(doc) { original ->
            val o = cur ?: return@loaded
            block {
                JsonForm(o, { cur = it }, act.error)
                Actions {
                    PrimaryButton(stringResource(R.string.save), {
                        run(savedMsg) {
                            val body = normalizeGeneric(o, original)
                            if (isNew) { c.post("settings/$kind", body); nav.popBackStack() } else c.put("settings/$kind/$id", body)
                        }
                    }, enabled = !act.busy)
                    GlassButton(stringResource(R.string.test), { run(testOk) { c.post("settings/$kind/test", normalizeGeneric(o, original)) } })
                    if (!isNew) DangerAction(stringResource(R.string.delete)) { askDelete = true }
                }
                ActionOutcome(act)
            }
        }
    }
    if (askDelete) ConfirmDialog(stringResource(R.string.m_delete_confirm, cur?.s("name").orEmpty()), stringResource(R.string.delete), { askDelete = false }) {
        run(null) { c.delete("settings/$kind/$id"); nav.popBackStack() }
    }
}
