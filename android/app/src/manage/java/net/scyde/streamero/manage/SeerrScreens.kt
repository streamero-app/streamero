package net.scyde.streamero.manage

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.serialization.json.JsonArray
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
    return rememberLoaded("$type/$tmdbId") { attempt { c.obj("$type/$tmdbId") }.getOrNull() }.value
}

@Composable
fun SeerrRequests(nav: NavHostController, c: ArrClient) {
    var filter by remember { mutableStateOf("pending") }
    var tick by remember { mutableIntStateOf(0) }
    var take by remember(filter) { mutableIntStateOf(20) }
    val page = rememberLoaded("$filter/$tick/$take") { c.obj("request", mapOf("take" to take, "skip" to 0, "filter" to filter, "sort" to "added")) }
    val (act, run) = rememberAction()
    var confirmDelete by remember { mutableStateOf<Int?>(null) }
    var confirmDecline by remember { mutableStateOf<Int?>(null) }
    ManagePage(nav, stringResource(R.string.m_requests), c.service.title) {
        item {
            ChipRow(listOf("pending" to stringResource(R.string.m_pending), "approved" to stringResource(R.string.m_approved), "processing" to stringResource(R.string.m_processing),
                "available" to stringResource(R.string.m_available), "failed" to stringResource(R.string.m_failed), "all" to stringResource(R.string.m_all)), filter) { filter = it }
        }
        block { ActionOutcome(act) }
        loaded(page) { p ->
            val list = p.objs("results")
            if (list.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
            keyedItems(list, { it.i("id") }, "request") { r ->
                val id = r.i("id") ?: return@keyedItems
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
                                    TextButton(onClick = { confirmDecline = id }, enabled = !act.busy) { Text(stringResource(R.string.m_decline), color = Kino.danger) }
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
    confirmDecline?.let { id -> ConfirmDialog(stringResource(R.string.m_decline_confirm), stringResource(R.string.m_decline), { confirmDecline = null }) { run(null) { c.post("request/$id/decline"); tick++ } } }
}

/** Discover/search titles through Seerr and open one to request it. */
@Composable
fun SeerrDiscover(nav: NavHostController, c: ArrClient) {
    var q by rememberSaveable { mutableStateOf("") }
    var submitted by rememberSaveable { mutableStateOf("") }
    val results = rememberLoaded(submitted) {
        if (submitted.isBlank()) c.list("discover/trending", mapOf("page" to 1)) else c.list("search", mapOf("query" to submitted, "page" to 1))
    }
    ManagePage(nav, stringResource(R.string.nav_discover), c.service.title) {
        item { SearchField(q, { q = it }, stringResource(R.string.m_search_titles), Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) { submitted = q.trim() } }
        if (submitted.isBlank()) item { Heading(stringResource(R.string.m_trending)) }
        loaded(results) { list ->
            keyedItems(list.filter { (it.s("mediaType") == "movie" || it.s("mediaType") == "tv") && it.i("id") != null }, { it.s("mediaType") + "/" + it.i("id") }, "title") { r ->
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
    var confirmDelete by remember { mutableStateOf<Int?>(null) }
    confirmDelete?.let { id -> ConfirmDialog(stringResource(R.string.m_delete_issue_confirm), stringResource(R.string.delete), { confirmDelete = null }) { run(null) { c.delete("issue/$id"); tick++ } } }
    ManagePage(nav, stringResource(R.string.m_issues), c.service.title) {
        item { ChipRow(listOf("open" to stringResource(R.string.m_open), "resolved" to stringResource(R.string.m_resolved), "all" to stringResource(R.string.m_all)), filter) { filter = it } }
        block { ActionOutcome(act) }
        loaded(page) { p ->
            val list = p.objs("results")
            if (list.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
            keyedItems(list, { it.i("id") }, "issue") { r ->
                val id = r.i("id") ?: return@keyedItems
                val t = rememberTitle(c, r.o("media")?.s("mediaType"), r.o("media")?.i("tmdbId"))
                Panel {
                    Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Text(t?.s("title") ?: t?.s("name") ?: "#$id", color = Kino.text, fontWeight = FontWeight.Medium)
                        Text(listOfNotNull(issueType(r.i("issueType")), r.o("createdBy")?.s("displayName"), when_(r.s("createdAt"))).joinToString(" · "), color = Kino.muted, fontSize = 12.sp)
                        r.objs("comments").firstOrNull()?.s("message")?.let { Text(it, color = Kino.text, fontSize = 13.sp) }
                        Actions {
                            if (r.i("status") == 1) TextButton(onClick = { run(null) { c.post("issue/$id/resolved"); tick++ } }) { Text(stringResource(R.string.m_resolve), color = Kino.success) }
                            else TextButton(onClick = { run(null) { c.post("issue/$id/open"); tick++ } }) { Text(stringResource(R.string.m_reopen)) }
                            TextButton(onClick = { nav.manage(c.service, "sissue", id.toString()) }) { Text(stringResource(R.string.m_comments_n, r.objs("comments").size), color = Kino.text) }
                            TextButton(onClick = { confirmDelete = id }, enabled = !act.busy) { Text(stringResource(R.string.delete), color = Kino.faint) }
                        }
                    }
                }
            }
        }
    }
}

/** One Seerr issue: its comment thread, a reply box, resolve/reopen. */
@Composable
fun SeerrIssue(nav: NavHostController, c: ArrClient, id: Int) {
    var tick by remember { mutableIntStateOf(0) }
    val issue = rememberLoaded("issue/$id/$tick") { c.obj("issue/$id") }
    var reply by rememberSaveable { mutableStateOf("") }
    val (act, run) = rememberAction()
    val t = issue.value?.o("media")?.let { m -> rememberTitle(c, m.s("mediaType"), m.i("tmdbId")) }
    ManagePage(nav, t?.s("title") ?: t?.s("name") ?: "#$id", issue.value?.let { issueType(it.i("issueType")) }) {
        loaded(issue) { o ->
            block {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Pill(stringResource(if (o.i("status") == 1) R.string.m_open else R.string.m_resolved), if (o.i("status") == 1) Kino.accent else Kino.success)
                    o.i("problemSeason")?.takeIf { it > 0 }?.let { Pill(stringResource(R.string.m_season_n, it)) }
                }
                Actions {
                    if (o.i("status") == 1) TextButton(onClick = { run(null) { c.post("issue/$id/resolved"); tick++ } }) { Text(stringResource(R.string.m_resolve), color = Kino.success) }
                    else TextButton(onClick = { run(null) { c.post("issue/$id/open"); tick++ } }) { Text(stringResource(R.string.m_reopen)) }
                }
            }
            item { Heading(stringResource(R.string.m_comments)) }
            val comments = o.objs("comments")
            if (comments.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
            items(comments) { cm ->
                Panel {
                    Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Text(listOfNotNull(cm.o("user")?.let { it.s("displayName") ?: it.s("email") }, cm.s("createdAt")?.let(::when_)).joinToString(" · "), color = Kino.faint, fontSize = 12.sp)
                        Text(cm.s("message").orEmpty(), color = Kino.text, fontSize = 14.sp)
                    }
                }
            }
            block {
                FormText(stringResource(R.string.m_add_comment), reply, { reply = it }, multiline = true)
                PrimaryButton(stringResource(R.string.m_send), { run(null) { c.post("issue/$id/comment", jsonOf("message" to reply.trim())); reply = ""; tick++ } },
                    Modifier.fillMaxWidth(), enabled = reply.isNotBlank() && !act.busy, icon = Icons.AutoMirrored.Outlined.Send)
                ActionOutcome(act)
            }
        }
    }
}

fun seerrPermissionNames(bits: Long): List<String> = SeerrPermissions.names(bits)

@Composable
fun SeerrUsers(nav: NavHostController, c: ArrClient) {
    val users = rememberLoaded(c.service) { c.obj("user", mapOf("take" to 100, "skip" to 0, "sort" to "displayname")) }
    ManagePage(nav, stringResource(R.string.users), c.service.title) {
        loaded(users) { p ->
            items(p.objs("results")) { u ->
                val id = u.i("id") ?: 0
                val admin = SeerrPermissions.has(u.l("permissions") ?: 0, SeerrPermissions.ADMIN)
                ListRow(u.s("displayName") ?: u.s("email") ?: "?", listOfNotNull(u.s("email"), stringResource(R.string.m_requests) + ": " + (u.i("requestCount") ?: 0), when_(u.s("createdAt"))).joinToString(" · "),
                    trailing = { if (admin) Pill(stringResource(R.string.m_perm_admin), Kino.accent) }, chevron = true) {
                    nav.manage(c.service, "suser", id.toString())
                }
            }
        }
    }
}

private class SeerrUserData(val user: JsonObject, val permissions: Long, val meId: Int?, val quota: JsonObject?, val main: JsonObject?)

private val QUOTA_KEYS = listOf("movieQuotaLimit", "movieQuotaDays", "tvQuotaLimit", "tvQuotaDays")

/** One Seerr user: permission toggles per documented bit, request limits when the API offers them, save. */
@Composable
fun SeerrUser(nav: NavHostController, c: ArrClient, id: Int) {
    var tick by remember { mutableIntStateOf(0) }
    val data = rememberLoaded("suser/$id/$tick") {
        coroutineScope {
            val u = async { c.obj("user/$id") }
            val perms = async { attempt { c.obj("user/$id/settings/permissions").l("permissions") }.getOrNull() }
            val me = async { attempt { c.obj("auth/me").i("id") }.getOrNull() }
            val quota = async { attempt { c.obj("user/$id/quota") }.getOrNull() }
            val main = async { attempt { c.obj("user/$id/settings/main") }.getOrNull() }
            val user = u.await()
            SeerrUserData(user, perms.await() ?: user.l("permissions") ?: 0, me.await(), quota.await(), main.await())
        }
    }
    var mask by remember(data.value) { mutableLongStateOf(data.value?.permissions ?: 0L) }
    var main by remember(data.value) { mutableStateOf(data.value?.main) }
    var warnAdmin by remember { mutableStateOf(false) }
    val (act, run) = rememberAction()
    val savedMsg = stringResource(R.string.saved)
    val d = data.value
    UnsavedChangesGuard(nav, d != null && (mask != d.permissions || main != d.main))
    fun save() {
        val dd = d ?: return
        run(savedMsg) {
            if (mask != dd.permissions) c.post("user/$id/settings/permissions", jsonOf("permissions" to mask))
            val m = main
            if (m != null && dd.main != null && m != dd.main) c.post("user/$id/settings/main", normalizeForSave(m, dd.main))
            tick++
        }
    }
    ManagePage(nav, d?.user?.let { it.s("displayName") ?: it.s("email") } ?: stringResource(R.string.users), d?.user?.s("email")) {
        loaded(data) { dd ->
            val isSelfOrOwner = id == dd.meId || id == 1
            item { Heading(stringResource(R.string.m_permissions)) }
            if (SeerrPermissions.has(mask, SeerrPermissions.ADMIN)) block { Notice(Kino.muted) { Text(stringResource(R.string.m_perm_admin_note), color = Kino.muted, fontSize = 13.sp) } }
            block {
                SeerrPermissions.KNOWN.forEach { b ->
                    Toggle(stringResource(b.label), b.key, SeerrPermissions.has(mask, b.value)) { on -> mask = SeerrPermissions.set(mask, b.value, on) }
                }
                SeerrPermissions.otherBits(mask).forEach { bit ->
                    Toggle(stringResource(R.string.m_perm_other, bit), stringResource(R.string.m_perm_other_hint), true) { on -> mask = SeerrPermissions.set(mask, 1L shl bit, on) }
                }
            }
            val q = dd.quota
            if (q != null || (main?.keys?.any { it in QUOTA_KEYS } == true)) {
                item { Heading(stringResource(R.string.m_request_limits)) }
                block {
                    q?.let { qq ->
                        listOf("movie" to R.string.movies, "tv" to R.string.series).forEach { (k, label) ->
                            qq.o(k)?.let { o ->
                                KeyValue(stringResource(label), if ((o.i("limit") ?: 0) > 0) plural(R.plurals.m_quota_used, o.i("days") ?: 0, o.i("used") ?: 0, o.i("limit") ?: 0, o.i("days") ?: 0) else stringResource(R.string.m_unlimited))
                            }
                        }
                    }
                    val m = main
                    if (m != null) QUOTA_KEYS.filter { m.containsKey(it) }.forEach { k ->
                        FormText(stringResource(when (k) {
                            "movieQuotaLimit" -> R.string.m_quota_movie_limit; "movieQuotaDays" -> R.string.m_quota_movie_days
                            "tvQuotaLimit" -> R.string.m_quota_tv_limit; else -> R.string.m_quota_tv_days
                        }), m.l(k)?.toString().orEmpty(), { t -> main = m.with(k, t.trim().toLongOrNull()) }, help = stringResource(R.string.m_quota_blank_hint), number = true)
                    }
                }
            }
            block {
                PrimaryButton(stringResource(R.string.save), {
                    if (isSelfOrOwner && SeerrPermissions.losesAdmin(dd.permissions, mask)) warnAdmin = true else save()
                }, Modifier.fillMaxWidth(), enabled = !act.busy && (mask != dd.permissions || main != dd.main))
                FormErrors(act.error, QUOTA_KEYS)
                act.done?.let { Text(it, color = Kino.success) }
            }
        }
    }
    if (warnAdmin) ConfirmDialog(stringResource(R.string.m_perm_remove_own_admin), stringResource(R.string.m_save_anyway), { warnAdmin = false }) { save() }
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

private val SEERR_AGENTS = listOf("email", "discord", "slack", "telegram", "pushover", "pushbullet", "gotify", "ntfy", "webhook", "webpush", "lunasea")

/** Seerr settings: main, network, Radarr/Sonarr servers and notification agents. */
@Composable
fun SeerrSettings(nav: NavHostController, c: ArrClient, arg: String?) {
    when {
        arg == null -> ManagePage(nav, stringResource(R.string.settings), c.service.title) {
            item { ListRow(stringResource(R.string.m_s_general), chevron = true) { nav.manage(c.service, "ssettings", arg = "doc:settings/main") } }
            item { ListRow(stringResource(R.string.m_s_network), chevron = true) { nav.manage(c.service, "ssettings", arg = "doc:settings/network") } }
            item { ListRow(stringResource(R.string.m_media_server), stringResource(R.string.m_media_server_hint), chevron = true) { nav.manage(c.service, "ssettings", arg = "mediaserver") } }
            item { ListRow("Radarr", chevron = true) { nav.manage(c.service, "ssettings", arg = "servers:radarr") } }
            item { ListRow("Sonarr", chevron = true) { nav.manage(c.service, "ssettings", arg = "servers:sonarr") } }
            item { Heading(stringResource(R.string.notifications)) }
            items(SEERR_AGENTS) { a -> ListRow(humanize(a), chevron = true) { nav.manage(c.service, "ssettings", arg = "doc:settings/notifications/$a") } }
        }
        arg.startsWith("doc:") -> SeerrDoc(nav, c, arg.removePrefix("doc:"))
        arg.startsWith("servers:") -> SeerrServers(nav, c, arg.removePrefix("servers:"))
        arg == "mediaserver" -> SeerrMediaServer(nav, c)
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
    if (hasSecrets(doc.value)) SecureScreen()
    UnsavedChangesGuard(nav, cur != null && doc.value != null && cur != doc.value)
    ManagePage(nav, humanize(resource.substringAfterLast('/')), c.service.title) {
        loaded(doc) { original ->
            val o = cur ?: return@loaded
            block {
                JsonForm(o, { cur = it }, act.error, hide = emptySet())
                PrimaryButton(stringResource(R.string.save), { run(savedMsg) { c.post(resource, normalizeForSave(o, original)); doc.reload() } }, Modifier.fillMaxWidth(), enabled = !act.busy && o != original)
                if (resource.startsWith("settings/notifications/")) GlassButton(stringResource(R.string.test), { run(testOk) { c.post("$resource/test", normalizeForSave(o, original)) } }, Modifier.fillMaxWidth())
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
        else c.list("settings/$kind").firstOrNull { it.i("id")?.toString() == id } ?: throw ArrException(404, "not_found", "no $kind server $id")
    }
    var cur by remember(doc.value) { mutableStateOf(doc.value) }
    var askDelete by remember { mutableStateOf(false) }
    val (act, run) = rememberAction()
    val savedMsg = stringResource(R.string.saved)
    val testOk = stringResource(R.string.m_test_ok)
    if (hasSecrets(doc.value)) SecureScreen()
    UnsavedChangesGuard(nav, cur != null && doc.value != null && cur != doc.value)
    ManagePage(nav, cur?.s("name")?.ifBlank { null } ?: kind, c.service.title) {
        loaded(doc) { original ->
            val o = cur ?: return@loaded
            block {
                JsonForm(o, { cur = it }, act.error)
                Actions {
                    PrimaryButton(stringResource(R.string.save), {
                        run(savedMsg) {
                            val body = normalizeForSave(o, original)
                            if (isNew) { c.post("settings/$kind", body); nav.popBackStack() } else { c.put("settings/$kind/$id", body); doc.reload() }
                        }
                    }, enabled = !act.busy)
                    GlassButton(stringResource(R.string.test), { run(testOk) { c.post("settings/$kind/test", normalizeForSave(o, original)) } })
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


// ---- Media server link (Seerr's own media-server settings) ----

/**
 * Which media-server settings resource Seerr uses: `settings/plex`, or the alternative one
 * Seerr's API offers for its other supported servers. `mediaServerType` in `settings/main`
 * decides when present (1 = the first, 2 and 3 = the alternative); otherwise the alternative
 * is used when it answers with a configured host.
 */
internal object SeerrMediaServerKind {
    const val PLEX = "plex"
    const val ALT = "jellyfin"

    fun fromType(type: Int?): String? = when (type) { 1 -> PLEX; 2, 3 -> ALT; else -> null }

    fun fromAltDoc(doc: JsonObject?): String =
        if (doc != null && listOf("ip", "hostname", "name").any { !doc.s(it).isNullOrBlank() }) ALT else PLEX

    /** The `enable` query of `settings/{kind}/library`: the ids to keep enabled, comma-separated. */
    fun enableQuery(libraries: List<JsonObject>, id: String, on: Boolean): String =
        libraries.mapNotNull { l -> val lid = l.s("id") ?: return@mapNotNull null; if (lid == id) lid.takeIf { on } else lid.takeIf { l.b("enabled") == true } }.joinToString(",")
}

private val MS_HIDE = setOf("libraries", "machineId", "serverId", "name")

@Composable
private fun SeerrMediaServer(nav: NavHostController, c: ArrClient) {
    val kind = rememberLoaded("ms/kind") {
        SeerrMediaServerKind.fromType(attempt { c.obj("settings/main").i("mediaServerType") }.getOrNull())
            ?: SeerrMediaServerKind.fromAltDoc(attempt { c.obj("settings/${SeerrMediaServerKind.ALT}") }.getOrNull())
    }
    val k = kind.value
    var tick by remember { mutableIntStateOf(0) }
    val doc = rememberLoaded("ms/$k/$tick") { if (k == null) JsonObject(emptyMap()) else c.obj("settings/$k") }
    var cur by remember(doc.value) { mutableStateOf(doc.value) }
    var libs by remember(doc.value) { mutableStateOf(doc.value?.objs("libraries").orEmpty()) }
    val scan = rememberLoaded("ms/$k/scan/$tick", pollMs = 4_000) { if (k == null) null else attempt { c.obj("settings/$k/sync") }.getOrNull() }
    val (act, run) = rememberAction()
    val savedMsg = stringResource(R.string.saved)
    val synced = stringResource(R.string.m_libraries_synced)
    val labels = mapOf(
        "ip" to stringResource(R.string.m_ms_host), "hostname" to stringResource(R.string.m_ms_host), "port" to stringResource(R.string.m_ms_port),
        "useSsl" to stringResource(R.string.m_ms_ssl), "urlBase" to stringResource(R.string.m_ms_url_base),
        "externalHostname" to stringResource(R.string.m_ms_external_url), "webAppUrl" to stringResource(R.string.m_ms_web_app_url),
    )
    if (hasSecrets(doc.value)) SecureScreen()
    UnsavedChangesGuard(nav, cur != null && doc.value != null && cur != doc.value)
    ManagePage(nav, stringResource(R.string.m_media_server), doc.value?.s("name")?.ifBlank { null } ?: c.service.title) {
        if (kind.error != null) block { ArrError(kind.error) }
        loaded(doc) { original ->
            val o = cur ?: return@loaded
            item { Heading(stringResource(R.string.m_connection)) }
            block {
                JsonForm(o, { cur = it }, act.error, hide = MS_HIDE, labels = labels)
                PrimaryButton(stringResource(R.string.save), {
                    run(savedMsg) { c.post("settings/$k", normalizeForSave(o, original)); tick++ }
                }, Modifier.fillMaxWidth(), enabled = !act.busy && o != original)
                FormErrors(act.error, o.keys)
                act.done?.let { Text(it, color = Kino.success) }
            }
            item { Heading(stringResource(R.string.m_libraries)) }
            block {
                GlassButton(stringResource(R.string.m_sync_libraries), {
                    run(synced) { libs = (c.get("settings/$k/library", mapOf("sync" to true)) as? JsonArray)?.filterIsInstance<JsonObject>() ?: libs }
                }, Modifier.fillMaxWidth(), enabled = !act.busy, icon = Icons.Outlined.Sync)
                if (libs.isEmpty()) Text(stringResource(R.string.m_no_libraries), color = Kino.muted, fontSize = 13.sp)
            }
            keyedItems(libs, { it.s("id") }, "library") { l ->
                val lid = l.s("id") ?: return@keyedItems
                ListRow(l.s("name") ?: lid, listOfNotNull(l.s("type")?.let(::humanize), l.l("lastScan")?.takeIf { it > 0 }?.let { stringResource(R.string.m_last_run, when_(java.time.Instant.ofEpochMilli(it).toString())) }).joinToString(" · "),
                    trailing = {
                        Switch(l.b("enabled") == true, { on ->
                            run(null) {
                                val r = c.get("settings/$k/library", mapOf("enable" to SeerrMediaServerKind.enableQuery(libs, lid, on)))
                                libs = (r as? JsonArray)?.filterIsInstance<JsonObject>() ?: libs.map { if (it.s("id") == lid) it.with("enabled", on) else it }
                            }
                        }, enabled = !act.busy, colors = SwitchDefaults.colors(checkedThumbColor = Kino.bg, checkedTrackColor = Kino.text))
                    })
            }
            block {
                Card(stringResource(R.string.m_library_scan)) {
                    val sc = scan.value
                    val running = sc?.b("running") == true
                    if (running) {
                        val total = sc?.i("total") ?: 0
                        val progress = sc?.i("progress") ?: 0
                        Text(listOfNotNull(sc?.o("currentLibrary")?.s("name"), if (total > 0) "$progress / $total" else null).joinToString(" · "), color = Kino.muted, fontSize = 13.sp)
                        if (total > 0) Progress(progress.toFloat() / total)
                    } else Text(stringResource(R.string.m_scan_idle), color = Kino.muted, fontSize = 13.sp)
                    if (sc == null && scan.value == null && !scan.loading) Text(stringResource(R.string.m_scan_unavailable), color = Kino.faint, fontSize = 12.sp)
                    if (running) GlassButton(stringResource(R.string.m_cancel_scan), { run(null) { c.post("settings/$k/sync", jsonOf("cancel" to true)); scan.reload() } }, Modifier.fillMaxWidth())
                    else PrimaryButton(stringResource(R.string.m_start_scan), { run(null) { c.post("settings/$k/sync", jsonOf("start" to true)); scan.reload() } }, Modifier.fillMaxWidth(), enabled = !act.busy && libs.any { it.b("enabled") == true })
                }
            }
        }
    }
}
