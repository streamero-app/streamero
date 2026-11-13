package net.scyde.streamero.ui

import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.*
import java.util.UUID

private val NOTIFY_KINDS = listOf("discord", "ntfy", "gotify", "generic")
private val NOTIFY_EVENTS = listOf("item.added", "playback.started", "login.succeeded", "login.failed", "scan.failed")

private fun notifyKindRes(kind: String) = when (kind) {
    "discord" -> R.string.notify_kind_discord; "ntfy" -> R.string.notify_kind_ntfy
    "gotify" -> R.string.notify_kind_gotify; else -> R.string.notify_kind_generic
}
private fun notifyHelpRes(kind: String) = when (kind) {
    "discord" -> R.string.notify_help_discord; "ntfy" -> R.string.notify_help_ntfy
    "gotify" -> R.string.notify_help_gotify; else -> R.string.notify_help_generic
}
private fun notifyUrlHint(kind: String) = when (kind) {
    "discord" -> "https://discord.com/api/webhooks/…"; "ntfy" -> "https://ntfy.sh/my-streamero"
    "gotify" -> "https://gotify.example.com"; else -> "https://example.com/hook"
}
private fun notifyEventRes(e: String) = when (e) {
    "item.added" -> R.string.notify_event_item_added; "playback.started" -> R.string.notify_event_playback_started
    "login.succeeded" -> R.string.notify_event_login_succeeded; "login.failed" -> R.string.notify_event_login_failed
    else -> R.string.notify_event_scan_failed
}

/** Webhook, Discord, ntfy and Gotify targets for server events; saved together, like on the web. */
@Composable
fun NotificationsPage(state: AppState) {
    val api = state.api ?: return
    val scope = rememberCoroutineScope()
    var targets by remember { mutableStateOf<List<NotificationTarget>?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var saved by remember { mutableStateOf(false) }
    var removing by remember { mutableStateOf<NotificationTarget?>(null) }
    LaunchedEffect(Unit) { runCatching { api.notificationSettings() }.onSuccess { targets = it.targets }.onFailure { error = it } }
    Intro(stringResource(R.string.notify_intro))
    val list = targets ?: run { if (error == null) Loading() else ErrorLine(error); return }
    removing?.let { t ->
        ConfirmDialog(stringResource(R.string.notify_remove_confirm, t.name.ifBlank { stringResource(R.string.notifications) }), stringResource(R.string.remove), { removing = null }) {
            targets = list.filter { it.id != t.id }; saved = false
        }
    }
    if (list.isEmpty()) Text(stringResource(R.string.notify_none), color = Kino.faint, fontSize = 14.sp)
    list.forEach { t ->
        key(t.id) { TargetCard(api, t, { n -> targets = list.map { if (it.id == t.id) n else it }; saved = false }, { removing = t }) }
    }
    Actions {
        GlassButton(stringResource(R.string.notify_add), {
            targets = list + NotificationTarget(UUID.randomUUID().toString(), events = listOf("item.added")); saved = false
        }, enabled = list.size < 20)
        PrimaryButton(stringResource(R.string.save), enabled = list.all(::notificationTargetValid), onClick = {
            error = null
            scope.launch {
                runCatching { api.saveNotificationSettings(NotificationSettings(list)) }.onSuccess { targets = it.targets; saved = true }.onFailure { error = it }
            }
        })
    }
    if (saved) Text(stringResource(R.string.saved), color = Kino.success)
    ErrorLine(error)
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun TargetCard(api: Api, t: NotificationTarget, onChange: (NotificationTarget) -> Unit, onRemove: () -> Unit) {
    val scope = rememberCoroutineScope()
    var testing by remember { mutableStateOf(false) }
    var sent by remember { mutableStateOf(false) }
    var testError by remember { mutableStateOf<Throwable?>(null) }
    Card(t.name.ifBlank { stringResource(notifyKindRes(t.kind)) }) {
        Field(stringResource(R.string.name), t.name, { onChange(t.copy(name = it.take(80))) }, placeholder = stringResource(R.string.notify_name_placeholder))
        FieldRow(stringResource(R.string.notify_kind)) { Picker(t.kind, NOTIFY_KINDS.map { it to stringResource(notifyKindRes(it)) }, { onChange(t.copy(kind = it)) }) }
        Field(stringResource(R.string.notify_url), t.url, { onChange(t.copy(url = it.trim())) }, hint = stringResource(notifyHelpRes(t.kind)), placeholder = notifyUrlHint(t.kind))
        // Tokens are write-only: an empty field keeps the stored one.
        if (t.kind != "discord") Field(stringResource(if (t.kind == "gotify") R.string.notify_app_token else R.string.notify_token), t.token.orEmpty(),
            { onChange(t.copy(token = it.ifEmpty { null })) }, secret = true, placeholder = if (t.hasToken) stringResource(R.string.notify_token_stored) else null)
        Text(stringResource(R.string.notify_events), color = Kino.text, fontSize = 15.sp, fontWeight = FontWeight.Medium, modifier = Modifier.padding(top = 6.dp))
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            NOTIFY_EVENTS.forEach { e ->
                val on = e in t.events
                FilterChip(on, { onChange(t.copy(events = if (on) t.events - e else t.events + e)) }, label = { Text(stringResource(notifyEventRes(e))) },
                    colors = FilterChipDefaults.filterChipColors(selectedContainerColor = Kino.text, selectedLabelColor = Kino.bg, labelColor = Kino.muted))
            }
        }
        Toggle(stringResource(R.string.enabled), null, t.enabled) { onChange(t.copy(enabled = it)) }
        Actions {
            GlassButton(stringResource(R.string.notify_test), enabled = !testing && notificationTargetValid(t), onClick = {
                testing = true; sent = false; testError = null
                scope.launch { runCatching { api.testNotification(t) }.onSuccess { sent = true }.onFailure { testError = it }; testing = false }
            })
            DangerAction(stringResource(R.string.remove), onClick = onRemove)
        }
        if (sent) Text(stringResource(R.string.notify_test_sent), color = Kino.success, fontSize = 14.sp)
        ErrorLine(testError)
    }
}

private fun backupKindRes(kind: String) = when (kind) {
    "scheduled" -> R.string.backup_kind_scheduled; "pre_restore" -> R.string.backup_kind_pre_restore
    "uploaded" -> R.string.backup_kind_uploaded; else -> R.string.backup_kind_manual
}

/** Backups of the database and config: create, save to the phone, restore (typed confirm), delete, schedule. */
@Composable
fun BackupsPage(state: AppState) {
    val api = state.api ?: return
    val scope = rememberCoroutineScope()
    val ctx = LocalContext.current
    var tick by remember { mutableIntStateOf(0) }
    val (data, loadError) = rememberReload(tick) { api.backups() }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var scanWarning by remember { mutableStateOf(false) }
    var deleting by remember { mutableStateOf<Backup?>(null) }
    var restoring by remember { mutableStateOf<Backup?>(null) }
    var saving by remember { mutableStateOf<String?>(null) }
    var savedFile by remember { mutableStateOf(false) }
    // Poll while a backup is written or a restore runs (the server restarts after a restore).
    val active = data?.running == true || data?.lastRestore?.state == "running"
    LaunchedEffect(active, tick) { if (active) { delay(3000); tick++ } }
    val saveTo = rememberLauncherForActivityResult(ActivityResultContracts.CreateDocument("application/gzip")) { uri: Uri? ->
        val name = saving
        if (uri != null && name != null) scope.launch {
            savedFile = false
            runCatching { ctx.contentResolver.openOutputStream(uri)!!.use { api.downloadBackup(name, it) } }.onSuccess { savedFile = true }.onFailure { error = it }
        }
    }
    Intro(stringResource(R.string.backups_intro))
    ErrorLine(loadError ?: error)
    val d = data ?: run { if (loadError == null) Loading(); return }
    Text(stringResource(R.string.backups_folder, d.dir), color = Kino.faint, fontSize = 12.sp)
    PrimaryButton(stringResource(if (d.running) R.string.backups_running else R.string.backups_now), enabled = !active, modifier = Modifier.fillMaxWidth(), onClick = {
        error = null
        scope.launch { runCatching { api.createBackup() }.onSuccess { scanWarning = it.scanRunning; tick++ }.onFailure { error = it } }
    })
    if (d.scanRunning || scanWarning) Notice(Kino.accent) { Text(stringResource(R.string.backups_scan_warning), color = Kino.text, fontSize = 14.sp) }
    d.lastRestore?.let { r ->
        Notice(if (r.state == "failed") Kino.danger else Kino.muted) {
            val whenText = shortDateTime(r.finishedAt ?: r.startedAt)
            Text(when (r.state) {
                "running" -> stringResource(R.string.backups_restore_running, r.backup)
                "succeeded" -> stringResource(R.string.backups_restore_succeeded, r.backup, whenText)
                else -> stringResource(R.string.backups_restore_failed, r.backup, whenText)
            }, color = Kino.text, fontSize = 14.sp)
            r.error?.let { Text(it, color = Kino.danger, fontSize = 13.sp) }
            r.safetyBackup?.let { Text(stringResource(R.string.backups_safety, it), color = Kino.muted, fontSize = 13.sp) }
            r.warnings.forEach { Text(it, color = Kino.muted, fontSize = 13.sp) }
        }
    }
    if (savedFile) Text(stringResource(R.string.backups_saved_file), color = Kino.success, fontSize = 14.sp)
    deleting?.let { b ->
        ConfirmDialog(stringResource(R.string.backups_delete_confirm), stringResource(R.string.delete), { deleting = null }) {
            scope.launch { runCatching { api.deleteBackup(b.name) }.onFailure { error = it }; tick++ }
        }
    }
    restoring?.let { b -> RestoreDialog(b, onDismiss = { restoring = null }) { typed ->
        scope.launch { runCatching { api.restoreBackup(b.name, typed) }.onSuccess { restoring = null; tick++ }.onFailure { error = it; restoring = null } }
    } }
    if (d.backups.isEmpty()) Text(stringResource(R.string.backups_none), color = Kino.faint, fontSize = 14.sp)
    d.backups.forEach { b ->
        Card(null) {
            Text(shortDateTime(b.createdAt), color = Kino.text, fontSize = 16.sp, fontWeight = FontWeight.SemiBold)
            Actions {
                Pill(stringResource(backupKindRes(b.kind)))
                if (b.includesImages) Pill(stringResource(R.string.backups_with_artwork))
                b.problem?.let { Pill(stringResource(if (it == "newer_schema") R.string.backups_problem_newer else R.string.backups_problem_unreadable), Kino.danger) }
            }
            Text(listOfNotNull(formatBytes(b.sizeBytes), b.serverVersion.ifBlank { null }?.let { stringResource(R.string.version) + " " + it },
                stringResource(R.string.backups_during_scan).takeIf { b.scanRunning },
                b.skippedTables.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.backups_skipped, it.joinToString()) }).joinToString(" · "),
                color = Kino.muted, fontSize = 13.sp)
            Text(b.name, color = Kino.faint, fontSize = 12.sp, fontFamily = FontFamily.Monospace)
            Actions {
                GlassButton(stringResource(R.string.download), { saving = b.name; saveTo.launch(b.name) })
                GlassButton(stringResource(R.string.backups_restore), { restoring = b }, enabled = b.restorable && !active)
                DangerAction(stringResource(R.string.delete)) { deleting = b }
            }
        }
    }
    BackupSchedule(api, d.settings) { tick++ }
}

@Composable
private fun BackupSchedule(api: Api, initial: BackupSettings, onSaved: () -> Unit) {
    var s by remember(initial) { mutableStateOf(initial) }
    var keep by remember(initial) { mutableStateOf(initial.keepLast.toString()) }
    var saved by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    val scope = rememberCoroutineScope()
    Card(stringResource(R.string.backups_schedule)) {
        Toggle(stringResource(R.string.backups_daily), stringResource(R.string.backups_daily_hint), s.enabled) { s = s.copy(enabled = it); saved = false }
        Field(stringResource(R.string.backups_keep), keep, { v -> keep = v.filter { it.isDigit() }.take(3); saved = false }, hint = stringResource(R.string.backups_keep_hint))
        Toggle(stringResource(R.string.backups_include_images), stringResource(R.string.backups_include_images_hint), s.includeImages) { s = s.copy(includeImages = it); saved = false }
        val keepLast = optionalInt(keep, 1..100)
        PrimaryButton(stringResource(R.string.save), enabled = keepLast != null, modifier = Modifier.fillMaxWidth(), onClick = {
            error = null
            scope.launch { runCatching { api.saveBackupSettings(s.copy(keepLast = keepLast!!)) }.onSuccess { saved = true; onSaved() }.onFailure { error = it } }
        })
        if (saved) Text(stringResource(R.string.saved), color = Kino.success)
        ErrorLine(error)
    }
}

/** Everything in the database is replaced: the backup's name must be typed to go ahead. */
@Composable
private fun RestoreDialog(b: Backup, onDismiss: () -> Unit, onRestore: (String) -> Unit) {
    var typed by remember { mutableStateOf("") }
    AlertDialog(onDismissRequest = onDismiss, containerColor = Kino.surface,
        title = { Text(stringResource(R.string.backups_restore), color = Kino.text) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(stringResource(R.string.backups_restore_warning), color = Kino.text, fontSize = 14.sp)
                Text(stringResource(R.string.backups_type_name, b.name), color = Kino.muted, fontSize = 13.sp)
                OutlinedTextField(typed, { typed = it }, singleLine = true, textStyle = LocalTextStyle.current.copy(fontFamily = FontFamily.Monospace, fontSize = 13.sp),
                    modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
            }
        },
        confirmButton = { TextButton(onClick = { onRestore(typed) }, enabled = restoreConfirmed(typed, b.name)) {
            Text(stringResource(R.string.backups_restore_confirm), color = if (restoreConfirmed(typed, b.name)) Kino.danger else Kino.faint)
        } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel), color = Kino.muted) } })
}
