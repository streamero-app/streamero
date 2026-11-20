package net.scyde.streamero.manage

import android.app.DownloadManager
import android.content.Context
import android.net.Uri
import android.os.Environment
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Article
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.serialization.json.JsonObject
import net.scyde.streamero.R
import net.scyde.streamero.ui.*

private data class SystemInfo(
    val status: JsonObject?, val health: List<JsonObject>, val disks: List<JsonObject>, val tasks: List<JsonObject>, val updates: List<JsonObject>,
)

/** Status, health, disk space, scheduled tasks (run now) and updates of an *arr service. */
@Composable
fun SystemPage(nav: NavHostController, c: ArrClient) {
    val s = c.service
    val info = rememberLoaded(s, pollMs = 15_000) {
        coroutineScope {
            val st = async { attempt { c.obj("system/status") }.getOrNull() }
            val h = async { attempt { c.list("health") }.getOrDefault(emptyList()) }
            val d = async { if (s == ArrService.Prowlarr) emptyList() else attempt { c.list("diskspace") }.getOrDefault(emptyList()) }
            val t = async { attempt { c.list("system/task") }.getOrDefault(emptyList()) }
            val u = async { attempt { c.list("update") }.getOrDefault(emptyList()) }
            SystemInfo(st.await(), h.await(), d.await(), t.await(), u.await())
        }
    }
    val (act, run) = rememberAction()
    val queued = stringResource(R.string.m_command_queued)
    val restarting = stringResource(R.string.m_restarting)
    var askRestart by remember { mutableStateOf(false) }
    var askUpdate by remember { mutableStateOf<String?>(null) }
    if (askRestart) ConfirmDialog(stringResource(R.string.m_restart_confirm, s.title), stringResource(R.string.m_restart), { askRestart = false }) {
        run(restarting) { c.post("system/restart") }
    }
    askUpdate?.let { v ->
        ConfirmDialog(stringResource(R.string.m_install_update_confirm, s.title, v), stringResource(R.string.m_install), { askUpdate = null }) { run(queued) { c.command("ApplicationUpdate") } }
    }
    ManagePage(nav, stringResource(R.string.m_system), s.title) {
        item {
            Row(Modifier.padding(horizontal = 16.dp, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                GlassButton(stringResource(R.string.logs), { nav.manage(s, "logs") }, icon = Icons.AutoMirrored.Outlined.Article)
                GlassButton(stringResource(R.string.backups), { nav.manage(s, "backups") }, icon = Icons.Outlined.Backup)
                GlassButton(stringResource(R.string.m_restart), { askRestart = true }, icon = Icons.Outlined.RestartAlt)
            }
        }
        block { ActionOutcome(act) }
        loaded(info) { i ->
            i.status?.let { st ->
                block {
                    Card(stringResource(R.string.m_status)) {
                        listOf("version", "startTime", "osName", "osVersion", "runtimeVersion", "databaseType", "databaseVersion", "appData", "startupPath", "branch", "authentication", "urlBase", "packageVersion")
                            .forEach { k -> st.s(k)?.takeIf { it.isNotBlank() }?.let { v -> KeyValue(humanize(k), if (k == "startTime") when_(v) else v) } }
                    }
                }
            }
            block {
                Card(stringResource(R.string.m_health)) {
                    if (i.health.isEmpty()) Text(stringResource(R.string.m_health_ok), color = Kino.success, fontSize = 14.sp)
                    i.health.forEach { h ->
                        Text(h.s("message").orEmpty(), color = if (h.s("type") == "error") Kino.danger else Kino.accent, fontSize = 14.sp)
                        h.s("source")?.let { Text(it, color = Kino.faint, fontSize = 12.sp) }
                    }
                }
            }
            if (i.disks.isNotEmpty()) block {
                Card(stringResource(R.string.m_disk_space)) {
                    i.disks.forEach { d ->
                        val total = d.l("totalSpace") ?: 0
                        val free = d.l("freeSpace") ?: 0
                        Text(d.s("path") ?: d.s("label") ?: "?", color = Kino.text, fontSize = 14.sp)
                        if (total > 0) Progress(((total - free).toDouble() / total).toFloat())
                        Text(stringResource(R.string.m_free_of, bytes(free), bytes(total)), color = Kino.muted, fontSize = 12.sp)
                        Spacer(Modifier.height(6.dp))
                    }
                }
            }
            if (i.tasks.isNotEmpty()) {
                item { Heading(stringResource(R.string.tasks)) }
                items(i.tasks) { t ->
                    ListRow(t.s("name") ?: t.s("taskName") ?: "?", listOfNotNull(t.i("interval")?.let { plural(R.plurals.m_every_minutes, it) },
                        t.s("lastExecution")?.let { stringResource(R.string.m_last_run, when_(it)) }, t.s("nextExecution")?.let { stringResource(R.string.m_next_run, when_(it)) }).joinToString(" · "),
                        trailing = { IconButton(onClick = { t.s("taskName")?.let { n -> run(queued) { c.command(n) } } }) { Icon(Icons.Outlined.PlayArrow, stringResource(R.string.run_now), tint = Kino.text) } })
                }
            }
            if (i.updates.isNotEmpty()) {
                item { Heading(stringResource(R.string.m_updates)) }
                i.updates.firstOrNull { it.b("installable") == true && it.b("installed") != true }?.let { u ->
                    block { PrimaryButton(stringResource(R.string.m_install_update, u.s("version").orEmpty()), { askUpdate = u.s("version").orEmpty() }, Modifier.fillMaxWidth(), enabled = !act.busy, icon = Icons.Outlined.SystemUpdate) }
                }
                items(i.updates.take(5)) { u ->
                    val changes = u.o("changes")
                    ListRow(u.s("version") ?: "?", listOfNotNull(
                        when { u.b("installed") == true -> stringResource(R.string.m_installed); u.b("installable") == true -> stringResource(R.string.m_available); else -> null },
                        u.s("releaseDate")?.take(10),
                        changes?.let { ch -> (ch.strs("new") + ch.strs("fixed")).take(4).joinToString("; ") }).joinToString(" · "))
                }
            }
        }
    }
}

@Composable
fun KeyValue(k: String, v: String) {
    Row(Modifier.fillMaxWidth().padding(vertical = 2.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Text(k, color = Kino.faint, fontSize = 13.sp, modifier = Modifier.weight(0.4f))
        Text(v, color = Kino.text, fontSize = 13.sp, modifier = Modifier.weight(0.6f))
    }
}

@Composable
fun LogsPage(nav: NavHostController, c: ArrClient) {
    var level by remember { mutableStateOf("all") }
    val (logs, more, next) = rememberPaged("${c.service}/$level", 100) { p -> c.obj("log", mapOf("page" to p, "pageSize" to 100, "sortKey" to "time", "sortDirection" to "descending", "level" to level.takeIf { it != "all" })) }
    ManagePage(nav, stringResource(R.string.logs), c.service.title) {
        item { ChipRow(listOf("all" to stringResource(R.string.m_all), "info" to stringResource(R.string.m_log_info), "warn" to stringResource(R.string.m_log_warn), "error" to stringResource(R.string.m_log_error)), level) { level = it } }
        loaded(logs) { list ->
            val shown = list.filter { r ->
                val l = r.s("level")?.lowercase().orEmpty()
                when (level) { "error" -> l == "error" || l == "fatal"; "warn" -> l == "warn" || l == "error" || l == "fatal"; "info" -> l != "debug" && l != "trace"; else -> true }
            }
            keyedItems(shown, { it.i("id") }, "log") { r ->
                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
                    Text("${when_(r.s("time"))} · ${r.s("level").orEmpty()} · ${r.s("logger").orEmpty()}", color = when (r.s("level")?.lowercase()) { "error", "fatal" -> Kino.danger; "warn" -> Kino.accent; else -> Kino.faint }, fontSize = 11.sp)
                    Text(r.s("message").orEmpty(), color = Kino.text, fontSize = 13.sp, fontFamily = FontFamily.Monospace)
                    r.s("exception")?.let { Text(it.take(600), color = Kino.muted, fontSize = 11.sp, fontFamily = FontFamily.Monospace) }
                }
            }
            if (more) block { GlassButton(stringResource(R.string.m_load_more), next, Modifier.fillMaxWidth()) }
        }
    }
}

/** Backups: list, create, download (through the proxy), restore and delete. */
@Composable
fun BackupsPage(nav: NavHostController, c: ArrClient) {
    var tick by remember { mutableIntStateOf(0) }
    val backups = rememberLoaded("${c.service}/$tick") { c.list("system/backup") }
    val (act, run) = rememberAction()
    val ctx = LocalContext.current
    var restore by remember { mutableStateOf<JsonObject?>(null) }
    var delete by remember { mutableStateOf<JsonObject?>(null) }
    val queued = stringResource(R.string.m_command_queued)
    val restoring = stringResource(R.string.m_restore_started)
    val downloading = stringResource(R.string.m_download_started)
    ManagePage(nav, stringResource(R.string.backups), c.service.title) {
        block {
            GlassButton(stringResource(R.string.m_backup_now), { run(queued) { c.command("Backup"); kotlinx.coroutines.delay(3000); tick++ } }, Modifier.fillMaxWidth(), icon = Icons.Outlined.Backup)
            ActionOutcome(act)
        }
        loaded(backups) { list ->
            if (list.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
            items(list.sortedByDescending { it.s("time").orEmpty() }) { b ->
                ListRow(b.s("name") ?: "?", listOfNotNull(b.s("type")?.let(::humanize), bytes(b.l("size")), when_(b.s("time"))).joinToString(" · "), trailing = {
                    Row {
                        IconButton(onClick = { run(downloading) { downloadBackup(ctx, c, b) } }) { Icon(Icons.Outlined.Download, stringResource(R.string.m_download), tint = Kino.muted) }
                        IconButton(onClick = { restore = b }) { Icon(Icons.Outlined.Restore, stringResource(R.string.m_restore), tint = Kino.muted) }
                        IconButton(onClick = { delete = b }) { Icon(Icons.Outlined.DeleteOutline, stringResource(R.string.delete), tint = Kino.danger) }
                    }
                })
            }
        }
    }
    restore?.let { b ->
        ConfirmDialog(stringResource(R.string.m_restore_confirm, b.s("name").orEmpty()), stringResource(R.string.m_restore), { restore = null }) {
            run(restoring) { c.post("system/backup/restore/${b.i("id")}") }
        }
    }
    delete?.let { b ->
        ConfirmDialog(stringResource(R.string.m_delete_confirm, b.s("name").orEmpty()), stringResource(R.string.delete), { delete = null }) {
            run(null) { c.delete("system/backup/${b.i("id")}"); tick++ }
        }
    }
}

/**
 * Backups live outside the service's API base (`/backup/...`); the download goes through the
 * proxy's raw path with the session token, saved by the system download manager.
 */
private fun downloadBackup(ctx: Context, c: ArrClient, b: JsonObject) {
    val path = b.s("path")?.trimStart('/') ?: return
    val url = c.api.base.toString().trimEnd('/') + "/api/v1/arr/${c.service.id}/proxy/" + path
    // The name comes from the service: only its last segment, so it cannot point outside Downloads.
    val name = backupFileName(b.s("name")) ?: "backup.zip"
    val req = DownloadManager.Request(Uri.parse(url))
        .setTitle(name)
        .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
        .setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS, name)
    c.api.token?.let { req.addRequestHeader("Authorization", "Bearer $it") }
    val dm = ctx.getSystemService(Context.DOWNLOAD_SERVICE) as? DownloadManager ?: return
    dm.enqueue(req)
}
