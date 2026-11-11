package net.scyde.streamero.ui

import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.clickable
import androidx.compose.foundation.selection.toggleable
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.*
import net.scyde.streamero.applyLanguage

private data class Entry(val page: String, val label: Int, val icon: ImageVector)

private val USER_PAGES = listOf(
    Entry("account", R.string.my_account, Icons.Outlined.Person),
    Entry("playback", R.string.playback, Icons.Outlined.Movie),
    Entry("subtitles", R.string.subtitles, Icons.Outlined.ClosedCaption),
    Entry("home", R.string.home_screen, Icons.Outlined.Home),
    Entry("quickconnect", R.string.quick_connect, Icons.Outlined.Tv),
)
/** Pages that work with a remote; administration needs typing and stays on phone and web. */
private val TV_PAGES = setOf("account", "playback", "subtitles", "home")
private val ADMIN_PAGES = listOf(
    Entry("dashboard", R.string.dashboard, Icons.Outlined.Dashboard),
    Entry("libraries", R.string.libraries, Icons.Outlined.FolderOpen),
    Entry("users", R.string.users, Icons.Outlined.Group),
    Entry("metadata", R.string.metadata, Icons.Outlined.AutoAwesome),
    Entry("transcoding", R.string.transcoding, Icons.Outlined.Memory),
    Entry("requests", R.string.requests, Icons.Outlined.Explore),
    Entry("enhancements", R.string.enhancements, Icons.Outlined.Extension),
    Entry("tasks", R.string.tasks, Icons.Outlined.Checklist),
    Entry("activity", R.string.activity, Icons.Outlined.History),
    Entry("logs", R.string.logs, Icons.Outlined.Article),
    Entry("devices", R.string.devices, Icons.Outlined.Devices),
    Entry("invites", R.string.invites, Icons.Outlined.Mail),
    Entry("apikeys", R.string.api_keys, Icons.Outlined.Key),
    Entry("server", R.string.server, Icons.Outlined.Dns),
    Entry("branding", R.string.branding, Icons.Outlined.Palette),
)

@Composable
fun MeScreen(state: AppState, nav: NavHostController, onSignedOut: () -> Unit, onSwitchProfile: () -> Unit) {
    val me = state.me ?: return
    val api = state.api ?: return
    val scope = rememberCoroutineScope()
    val tv = LocalTv.current
    val mem = rememberFocusMemory()
    LazyColumn(if (tv) Modifier.fillMaxHeight().widthIn(max = 760.dp).padding(horizontal = 20.dp) else Modifier.fillMaxSize(), contentPadding = PaddingValues(top = if (tv) 20.dp else 0.dp, bottom = 24.dp)) {
        item {
            Row(Modifier.statusBarsPadding().padding(20.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                val p = me.currentProfile
                Avatar(p?.name ?: me.displayName ?: me.username, p?.color, if (p?.hasAvatar == true) api.avatarUrl(p.id) else if (p == null && me.hasAvatar) api.avatarUrl(me.id) else null, 64.dp)
                Column {
                    Text((p?.name ?: me.displayName ?: me.username), style = Display.copy(fontSize = 26.sp), color = Kino.text)
                    Text("@${me.username}" + if (state.isAdmin) " · " + stringResource(R.string.administrator) else "", color = Kino.muted, fontSize = 13.sp)
                }
            }
        }
        if (!state.isKid) items(USER_PAGES.filter { !tv || it.page in TV_PAGES }) { e ->
            SettingsLink(e, Modifier.rememberFocus(mem, e.page, default = e.page == "account")) { nav.navigate("settings/${e.page}") }
        }
        if (!tv && (canDownload(state) || state.offline.store.data.value.entries.isNotEmpty())) item {
            SettingsLink(Entry("downloads", R.string.downloads, Icons.Outlined.Download)) { nav.navigate("downloads") }
        }
        if (canWatchTogether(state)) item {
            SettingsLink(Entry("together", R.string.watch_together, Icons.Outlined.Groups), Modifier.rememberFocus(mem, "together")) { nav.navigate("together") }
        }
        if (net.scyde.streamero.livetv.canWatchLiveTv(state.me)) item {
            SettingsLink(Entry("livetv", R.string.livetv, Icons.Outlined.LiveTv), Modifier.rememberFocus(mem, "livetv")) { nav.navigate("livetv") }
        }
        if (state.isAdmin && tv) item {
            Text(stringResource(R.string.tv_admin_elsewhere), color = Kino.faint, fontSize = 14.sp, modifier = Modifier.padding(start = 20.dp, end = 20.dp, top = 20.dp))
        }
        if (state.isAdmin && !tv) {
            item { Text(stringResource(R.string.administration).uppercase(), style = Label, color = Kino.faint, modifier = Modifier.padding(start = 20.dp, top = 20.dp, bottom = 6.dp)) }
            items(ADMIN_PAGES) { e -> SettingsLink(e) { nav.navigate("settings/${e.page}") } }
        }
        item {
            Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                GlassButton(stringResource(R.string.switch_profile), onSwitchProfile, Modifier.fillMaxWidth().rememberFocus(mem, "profile", default = state.isKid))
                GlassButton(stringResource(R.string.sign_out), { scope.launch { state.signOut(); onSignedOut() } }, Modifier.fillMaxWidth().rememberFocus(mem, "signout"))
            }
        }
    }
}

@Composable
private fun SettingsLink(e: Entry, modifier: Modifier = Modifier, onClick: () -> Unit) {
    Row(modifier.fillMaxWidth().focusClickable(scale = 1.02f, onClick = onClick).padding(horizontal = 20.dp, vertical = 14.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
        Icon(e.icon, null, tint = Kino.muted)
        Text(stringResource(e.label), color = Kino.text, fontSize = 16.sp, modifier = Modifier.weight(1f))
        Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = Kino.faint)
    }
}

@Composable
fun SettingsPage(state: AppState, nav: NavHostController, page: String) {
    val adminOnly = ADMIN_PAGES.any { it.page == page }
    if (adminOnly && !state.isAdmin) { Text(stringResource(R.string.err_forbidden), color = Kino.danger, modifier = Modifier.statusBarsPadding().padding(20.dp)); return }
    val title = (USER_PAGES + ADMIN_PAGES).firstOrNull { it.page == page }?.label ?: R.string.settings
    val tv = LocalTv.current
    Column((if (tv) Modifier.fillMaxHeight().widthIn(max = 860.dp) else Modifier.fillMaxSize()).verticalScroll(rememberScrollState()).statusBarsPadding()
        .then(if (tv) Modifier.padding(horizontal = 48.dp, vertical = 24.dp) else Modifier.padding(16.dp)), verticalArrangement = Arrangement.spacedBy(16.dp)) {
        Text(stringResource(title), style = Display.copy(fontSize = 30.sp), color = Kino.text, modifier = Modifier.padding(top = 8.dp))
        when (page) {
            "account" -> AccountPage(state)
            "playback" -> PlaybackPage(state)
            "subtitles" -> SubtitleLookPage(state)
            "home" -> HomeSectionsPage(state)
            "quickconnect" -> QuickConnectPage(state)
            "dashboard" -> DashboardPage(state, nav)
            "libraries" -> LibrariesAdminPage(state)
            "users" -> UsersPage(state)
            "metadata" -> MetadataPage(state)
            "transcoding" -> TranscodingPage(state)
            "requests" -> RequestsPage(state)
            "enhancements" -> EnhancementsPage(state)
            "tasks" -> TasksPage(state)
            "activity" -> ActivityPage(state)
            "logs" -> LogsPage(state)
            "devices" -> DevicesPage(state)
            "invites" -> InvitesPage(state)
            "apikeys" -> ApiKeysPage(state)
            "server" -> ServerPage(state)
            "branding" -> BrandingPage(state)
        }
    }
}

/** Load-edit-save for one settings document, with a status line. */
@Composable
private fun <T> SettingsDoc(load: suspend () -> T, save: suspend (T) -> T, content: @Composable (T, (T) -> Unit) -> Unit) {
    var value by remember { mutableStateOf<T?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var saved by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) { runCatching { load() }.onSuccess { value = it }.onFailure { error = it } }
    val v = value ?: run { if (error == null) Loading() else ErrorLine(error); return }
    content(v) { value = it; saved = false }
    PrimaryButton(stringResource(R.string.save), { scope.launch { runCatching { save(v) }.onSuccess { value = it; saved = true; error = null }.onFailure { error = it } } }, Modifier.fillMaxWidth())
    if (saved) Text(stringResource(R.string.saved), color = Kino.success)
    ErrorLine(error)
}

@Composable
private fun Toggle(label: String, hint: String?, checked: Boolean, onChange: (Boolean) -> Unit) {
    // The whole row toggles, so the remote has one target per setting.
    val (src, focused) = rememberFocusSource()
    Row(Modifier.fillMaxWidth().focusRing(focused, scale = 1.02f, width = 2.dp).clip(CardShape)
        .toggleable(checked, interactionSource = src, indication = tapIndication(), onValueChange = onChange).padding(vertical = 8.dp, horizontal = if (LocalTv.current) 8.dp else 0.dp),
        verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(label, color = Kino.text, fontSize = 15.sp)
            if (hint != null) Text(hint, color = Kino.faint, fontSize = 13.sp)
        }
        Switch(checked, null, colors = SwitchDefaults.colors(checkedThumbColor = Kino.bg, checkedTrackColor = Kino.text))
    }
}

@Composable
private fun Field(label: String, value: String, onChange: (String) -> Unit, secret: Boolean = false, hint: String? = null) {
    OutlinedTextField(value, onChange, label = { Text(label) }, supportingText = hint?.let { { Text(it) } }, singleLine = true,
        visualTransformation = if (secret) PasswordVisualTransformation() else androidx.compose.ui.text.input.VisualTransformation.None,
        modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
}

// ---- Personal ----

@Composable
private fun AccountPage(state: AppState) {
    val api = state.api ?: return
    val me = state.me ?: return
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var error by remember { mutableStateOf<Throwable?>(null) }
    val pick = rememberLauncherForActivityResult(ActivityResultContracts.GetContent()) { uri: Uri? ->
        if (uri != null) scope.launch {
            runCatching { api.uploadAvatar(ctx.contentResolver.openInputStream(uri)!!.use { it.readBytes() }); state.refreshMe() }.onFailure { error = it }
        }
    }
    // A TV has no photo picker: the picture is changed from a phone or the web.
    if (!LocalTv.current) Card(null) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
            Box(Modifier.clickable { pick.launch("image/*") }) { Avatar(me.displayName ?: me.username, "#8A5A3A", if (me.hasAvatar) api.avatarUrl(me.id) + "?v=" + System.identityHashCode(me) else null, 88.dp) }
            Column {
                TextButton(onClick = { pick.launch("image/*") }) { Text(stringResource(R.string.change_picture)) }
                if (me.hasAvatar) TextButton(onClick = { scope.launch { runCatching { api.removeAvatar(); state.refreshMe() } } }) { Text(stringResource(R.string.remove_picture), color = Kino.danger) }
            }
        }
        ErrorLine(error)
    }
    SettingsDoc(load = { UpdateMe(me.displayName ?: "", me.email ?: "", me.language ?: "en") },
        save = { u -> val r = api.updateMe(u); state.me = r; applyLanguage(u.language); u }) { v, set ->
        Field(stringResource(R.string.display_name), v.displayName.orEmpty(), { set(v.copy(displayName = it)) })
        Field(stringResource(R.string.email), v.email.orEmpty(), { set(v.copy(email = it)) })
        FieldRow(stringResource(R.string.language)) { Picker(v.language ?: "en", listOf("en" to "English", "hr" to "Hrvatski"), { set(v.copy(language = it)) }) }
    }
    var cur by remember { mutableStateOf("") }
    var next by remember { mutableStateOf("") }
    var pwMsg by remember { mutableStateOf<String?>(null) }
    Card(stringResource(R.string.change_password)) {
        Field(stringResource(R.string.current_password), cur, { cur = it }, secret = true)
        Field(stringResource(R.string.new_password), next, { next = it }, secret = true, hint = stringResource(R.string.password_hint))
        val ok = stringResource(R.string.password_changed)
        PrimaryButton(stringResource(R.string.change_password), { scope.launch { runCatching { api.changePassword(cur, next) }.onSuccess { pwMsg = ok; cur = ""; next = "" }.onFailure { error = it } } },
            Modifier.fillMaxWidth(), enabled = cur.isNotBlank() && next.length >= 8)
        pwMsg?.let { Text(it, color = Kino.success) }
    }
}

@Composable
private fun PlaybackPage(state: AppState) {
    val api = state.api ?: return
    SettingsDoc(load = { api.preferences() }, save = { api.savePreferences(it) }) { p, set ->
        val langs = listOf("" to stringResource(R.string.any)) + listOf("en", "hr", "de", "fr", "es", "it", "sr", "bs", "sl", "pl", "ru", "ja").map { it to java.util.Locale(it).displayLanguage }
        Card(stringResource(R.string.language)) {
            FieldRow(stringResource(R.string.audio_language)) { Picker(p.audioLanguage ?: "", langs, { set(p.copy(audioLanguage = it.ifBlank { null })) }) }
            FieldRow(stringResource(R.string.subtitle_language)) { Picker(p.subtitleLanguage ?: "", langs, { set(p.copy(subtitleLanguage = it.ifBlank { null })) }) }
            FieldRow(stringResource(R.string.subtitle_mode)) {
                Picker(p.subtitleMode, listOf("default" to stringResource(R.string.mode_default), "smart" to stringResource(R.string.mode_smart), "always" to stringResource(R.string.mode_always),
                    "forced" to stringResource(R.string.mode_forced), "none" to stringResource(R.string.mode_none)), { set(p.copy(subtitleMode = it)) })
            }
        }
        Card(stringResource(R.string.controls)) {
            FieldRow(stringResource(R.string.skip_back)) { Picker(p.skipBackSeconds ?: 10, listOf(5, 10, 15, 30, 60).map { it to "$it s" }, { set(p.copy(skipBackSeconds = it)) }) }
            FieldRow(stringResource(R.string.skip_forward)) { Picker(p.skipForwardSeconds ?: 10, listOf(5, 10, 15, 30, 60, 120).map { it to "$it s" }, { set(p.copy(skipForwardSeconds = it)) }) }
            FieldRow(stringResource(R.string.intros)) { Picker(p.skipIntro ?: "button", listOf("button" to stringResource(R.string.seg_button), "auto" to stringResource(R.string.seg_auto), "off" to stringResource(R.string.seg_off)), { set(p.copy(skipIntro = it)) }) }
            FieldRow(stringResource(R.string.credits)) { Picker(p.skipCredits ?: "button", listOf("button" to stringResource(R.string.credits_button), "auto" to stringResource(R.string.credits_auto), "off" to stringResource(R.string.seg_off)), { set(p.copy(skipCredits = it)) }) }
            Toggle(stringResource(R.string.autoplay), null, p.autoplayNext) { set(p.copy(autoplayNext = it)) }
        }
        Card(stringResource(R.string.quality)) {
            FieldRow(stringResource(R.string.max_resolution)) { Picker(p.maxHeight ?: 0, listOf(0 to stringResource(R.string.original), 2160 to "4K", 1080 to "1080p", 720 to "720p", 480 to "480p"), { set(p.copy(maxHeight = it.takeIf { x -> x > 0 })) }) }
            FieldRow(stringResource(R.string.max_bitrate)) { Picker(p.maxBitrateKbps ?: 0, listOf(0 to stringResource(R.string.no_limit), 40000 to "40 Mbit/s", 20000 to "20 Mbit/s", 10000 to "10 Mbit/s", 4000 to "4 Mbit/s", 2000 to "2 Mbit/s", 1000 to "1 Mbit/s"), { set(p.copy(maxBitrateKbps = it.takeIf { x -> x > 0 })) }) }
        }
    }
}

@Composable
private fun SubtitleLookPage(state: AppState) {
    val api = state.api ?: return
    SettingsDoc(load = { api.preferences() }, save = { api.savePreferences(it) }) { p, set ->
        Card(null) {
            FieldRow(stringResource(R.string.size)) { Picker(p.subtitleSize ?: "normal", listOf("small" to stringResource(R.string.small), "normal" to stringResource(R.string.normal), "large" to stringResource(R.string.large), "xlarge" to stringResource(R.string.xlarge)), { set(p.copy(subtitleSize = it)) }) }
            FieldRow(stringResource(R.string.colour)) { Picker(p.subtitleColor ?: "white", listOf("white" to stringResource(R.string.white), "yellow" to stringResource(R.string.yellow), "cyan" to stringResource(R.string.cyan)), { set(p.copy(subtitleColor = it)) }) }
            FieldRow(stringResource(R.string.background)) { Picker(p.subtitleBackground ?: "shadow", listOf("none" to stringResource(R.string.none), "shadow" to stringResource(R.string.shadow), "box" to stringResource(R.string.box)), { set(p.copy(subtitleBackground = it)) }) }
        }
    }
}

@Composable
private fun HomeSectionsPage(state: AppState) {
    val api = state.api ?: return
    val all = listOf("continue", "nextUp", "latest", "favorites", "collections", "libraries")
    SettingsDoc(load = { api.preferences() }, save = { api.savePreferences(it) }) { p, set ->
        val chosen = p.homeSections ?: all
        Card(null) {
            (chosen + all.filter { it !in chosen }).forEach { s ->
                Toggle(stringResource(when (s) { "continue" -> R.string.continue_watching; "nextUp" -> R.string.next_up; "latest" -> R.string.recently_added
                    "favorites" -> R.string.my_list; "collections" -> R.string.collections; else -> R.string.libraries }), null, s in chosen) { on ->
                    set(p.copy(homeSections = if (on) chosen + s else chosen - s))
                }
            }
        }
    }
}

// ---- Administration ----

@Composable
private fun DashboardPage(state: AppState, nav: NavHostController) {
    val api = state.api ?: return
    var tick by remember { mutableIntStateOf(0) }
    val (d, _, error) = rememberLoad(tick) { api.dashboard() }
    val scope = rememberCoroutineScope()
    LaunchedEffect(Unit) { while (true) { kotlinx.coroutines.delay(10_000); tick++ } }
    ErrorLine(error)
    val dash = d ?: return Loading()
    Card(stringResource(R.string.now_playing)) {
        if (dash.nowPlaying.isEmpty()) Text(stringResource(R.string.nobody_watching), color = Kino.muted)
        dash.nowPlaying.forEach { s ->
            var msg by remember(s.key) { mutableStateOf("") }
            Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(s.title, color = Kino.text, fontWeight = FontWeight.SemiBold, modifier = Modifier.clickable { nav.navigate("item/${s.itemId}") })
                Text("${s.user}${s.profile?.let { " · $it" }.orEmpty()} · ${s.device} · ${s.method}${s.reason?.let { " ($it)" }.orEmpty()}", color = Kino.muted, fontSize = 13.sp)
                if (s.durationMs != null && s.durationMs > 0) ProgressLine((s.positionMs.toFloat() / s.durationMs).coerceIn(0f, 1f))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                    OutlinedTextField(msg, { msg = it }, placeholder = { Text(stringResource(R.string.message)) }, singleLine = true, modifier = Modifier.weight(1f), shape = CardShape)
                    GlassButton(stringResource(R.string.send), { scope.launch { runCatching { api.sendCommand(s.key, SessionCommand("message", msg)) }; msg = "" } }, enabled = msg.isNotBlank())
                }
                val stopped = stringResource(R.string.stopped_by_admin)
                TextButton(onClick = { scope.launch { runCatching { api.sendCommand(s.key, SessionCommand("stop", stopped)) }; tick++ } }) { Text(stringResource(R.string.stop_playback), color = Kino.danger) }
            }
        }
        Text(stringResource(R.string.transcodes_now, dash.activeTranscodes), color = Kino.faint, fontSize = 13.sp)
    }
    Card(stringResource(R.string.storage)) {
        dash.storage.forEach { s ->
            Text("${s.kind} · ${s.path}", color = Kino.text, fontSize = 14.sp)
            ProgressLine(1f - s.freeBytes.toFloat() / s.totalBytes.coerceAtLeast(1))
            Text(stringResource(R.string.free_of, s.freeBytes / 1_073_741_824, s.totalBytes / 1_073_741_824), color = Kino.faint, fontSize = 12.sp)
        }
    }
    Card(stringResource(R.string.server)) { Text("${dash.server.name} · ${dash.server.version} · ${dash.server.os}", color = Kino.muted) }
}

@Composable
private fun LibrariesAdminPage(state: AppState) {
    val api = state.api ?: return
    var tick by remember { mutableIntStateOf(0) }
    val (libs, _, error) = rememberLoad(tick) { api.libraries() }
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf("") }
    var kind by remember { mutableStateOf("movies") }
    var path by remember { mutableStateOf("/media/") }
    var err by remember { mutableStateOf<Throwable?>(null) }
    ErrorLine(error ?: err)
    libs?.forEach { l ->
        Card(l.name) {
            Text("${l.kind} · ${l.paths.joinToString()} · ${l.itemCount}", color = Kino.muted, fontSize = 13.sp)
            Text(l.scan.state + (l.scan.error?.let { " · $it" }.orEmpty()), color = Kino.faint, fontSize = 12.sp)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                GlassButton(stringResource(R.string.scan), { scope.launch { runCatching { api.scanLibrary(l.id) }.onFailure { err = it }; tick++ } })
                GlassButton(stringResource(R.string.refresh_metadata), { scope.launch { runCatching { api.refreshLibraryMetadata(l.id) }.onFailure { err = it } } })
            }
            TextButton(onClick = { scope.launch { runCatching { api.deleteLibrary(l.id) }.onFailure { err = it }; tick++ } }) { Text(stringResource(R.string.delete), color = Kino.danger) }
        }
    }
    Card(stringResource(R.string.add_library)) {
        Field(stringResource(R.string.name), name, { name = it })
        FieldRow(stringResource(R.string.kind)) { Picker(kind, listOf("movies" to stringResource(R.string.movies), "shows" to stringResource(R.string.shows), "music" to stringResource(R.string.music), "mixed" to stringResource(R.string.mixed)), { kind = it }) }
        Field(stringResource(R.string.folder), path, { path = it })
        PrimaryButton(stringResource(R.string.add_library), { scope.launch { runCatching { api.createLibrary(LibraryInput(name, kind, listOf(path))) }.onSuccess { name = ""; tick++ }.onFailure { err = it } } },
            Modifier.fillMaxWidth(), enabled = name.isNotBlank() && path.length > 1)
    }
}

@Composable
private fun UsersPage(state: AppState) {
    val api = state.api ?: return
    var tick by remember { mutableIntStateOf(0) }
    val (users, _, error) = rememberLoad(tick) { api.users() }
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf("") }
    var pass by remember { mutableStateOf("") }
    var admin by remember { mutableStateOf(false) }
    var err by remember { mutableStateOf<Throwable?>(null) }
    ErrorLine(error ?: err)
    users?.forEach { u ->
        Card(u.displayName ?: u.username) {
            Text("@${u.username}" + (if (u.isAdmin) " · " + stringResource(R.string.administrator) else "") + (if (u.disabled) " · " + stringResource(R.string.disabled) else ""), color = Kino.muted, fontSize = 13.sp)
            if (u.id != state.me?.id) {
                Toggle(stringResource(R.string.administrator), null, u.isAdmin) { on -> scope.launch { runCatching { api.updateUser(u.id, UpdateUser(isAdmin = on)) }.onFailure { err = it }; tick++ } }
                Toggle(stringResource(R.string.disabled), null, u.disabled) { on -> scope.launch { runCatching { api.updateUser(u.id, UpdateUser(disabled = on)) }.onFailure { err = it }; tick++ } }
                TextButton(onClick = { scope.launch { runCatching { api.deleteUser(u.id) }.onFailure { err = it }; tick++ } }) { Text(stringResource(R.string.delete), color = Kino.danger) }
            }
        }
    }
    Card(stringResource(R.string.add_user)) {
        Field(stringResource(R.string.username), name, { name = it })
        Field(stringResource(R.string.password), pass, { pass = it }, secret = true, hint = stringResource(R.string.password_hint))
        Toggle(stringResource(R.string.administrator), null, admin) { admin = it }
        PrimaryButton(stringResource(R.string.add_user), { scope.launch { runCatching { api.createUser(CreateUser(name, pass, admin)) }.onSuccess { name = ""; pass = ""; tick++ }.onFailure { err = it } } },
            Modifier.fillMaxWidth(), enabled = name.isNotBlank() && pass.length >= 8)
    }
}

@Composable
private fun MetadataPage(state: AppState) {
    val api = state.api ?: return
    var key by remember { mutableStateOf("") }
    SettingsDoc(load = { api.metadataSettings() }, save = { s -> api.saveMetadataSettings(MetadataSettingsInput(s.language, s.country, key.ifBlank { null })).also { key = "" } }) { s, set ->
        Card(null) {
            Text(if (s.tmdbConfigured) stringResource(R.string.tmdb_connected) else stringResource(R.string.tmdb_steps), color = if (s.tmdbConfigured) Kino.success else Kino.muted, fontSize = 14.sp)
            Field(stringResource(R.string.tmdb_key), key, { key = it }, secret = true)
            FieldRow(stringResource(R.string.metadata_language)) { Picker(s.language, listOf("en-US", "en-GB", "hr-HR", "de-DE", "fr-FR", "es-ES", "it-IT", "sr-RS", "bs-BA", "sl-SI").map { it to it }, { set(s.copy(language = it)) }) }
            FieldRow(stringResource(R.string.country)) { Picker(s.country, listOf("US", "GB", "HR", "DE", "AT", "FR", "ES", "IT", "RS", "BA", "SI").map { it to it }, { set(s.copy(country = it)) }) }
        }
    }
}

@Composable
private fun TranscodingPage(state: AppState) {
    val api = state.api ?: return
    SettingsDoc(load = { api.encoding() }, save = { api.saveEncoding(it) }) { e, set ->
        Card(stringResource(R.string.transcoding)) {
            FieldRow(stringResource(R.string.hw_accel)) { Picker(e.hardwareAcceleration, listOf("none" to stringResource(R.string.hw_none), "vaapi" to "VAAPI (Intel, AMD)", "qsv" to "Intel Quick Sync", "nvenc" to "NVIDIA NVENC"), { set(e.copy(hardwareAcceleration = it)) }) }
            if (e.hardwareAcceleration == "vaapi") Field("VAAPI", e.vaapiDevice ?: "/dev/dri/renderD128", { set(e.copy(vaapiDevice = it)) })
            FieldRow(stringResource(R.string.encoder_speed)) { Picker(e.preset, listOf("ultrafast", "superfast", "veryfast", "faster", "fast", "medium", "slow").map { it to it }, { set(e.copy(preset = it)) }) }
            FieldRow(stringResource(R.string.quality_value, e.quality)) { Slider(e.quality.toFloat(), { set(e.copy(quality = it.toInt())) }, valueRange = 14f..35f) }
            FieldRow(stringResource(R.string.audio_bitrate)) { Picker(e.audioBitrateKbps, listOf(96, 128, 160, 192, 256, 320).map { it to "$it kbit/s" }, { set(e.copy(audioBitrateKbps = it)) }) }
            FieldRow(stringResource(R.string.segment_length)) { Picker(e.segmentSeconds, listOf(2, 3, 4, 6, 8, 10).map { it to "$it s" }, { set(e.copy(segmentSeconds = it)) }) }
            FieldRow(stringResource(R.string.max_transcodes)) { Picker(e.maxSessions, listOf(0, 1, 2, 3, 4, 6, 8, 12, 16).map { it to if (it == 0) stringResource(R.string.no_limit) else it.toString() }, { set(e.copy(maxSessions = it)) }) }
            Toggle(stringResource(R.string.tone_mapping), null, e.toneMapping) { set(e.copy(toneMapping = it)) }
        }
    }
    SettingsDoc(load = { api.resume() }, save = { api.saveResume(it) }) { r, set ->
        Card(stringResource(R.string.resume)) {
            Field(stringResource(R.string.resume_min), r.minResumePercent.toString(), { set(r.copy(minResumePercent = it.toIntOrNull() ?: 0)) })
            Field(stringResource(R.string.resume_max), r.maxResumePercent.toString(), { set(r.copy(maxResumePercent = it.toIntOrNull() ?: 90)) })
            Field(stringResource(R.string.resume_duration), r.minResumeDurationSeconds.toString(), { set(r.copy(minResumeDurationSeconds = it.toIntOrNull() ?: 0)) })
        }
    }
}

@Composable
private fun RequestsPage(state: AppState) {
    val api = state.api ?: return
    var keys by remember { mutableStateOf(mapOf<String, String>()) }
    var checks by remember { mutableStateOf<List<ConnectionCheck>>(emptyList()) }
    val scope = rememberCoroutineScope()
    SettingsDoc(load = { api.requestSettings() }, save = { s ->
        api.saveRequestSettings(RequestSettingsInput(s.usersCanRequest, s.seerrUrl, keys["seerr"], s.radarrUrl, keys["radarr"], s.sonarrUrl, keys["sonarr"], s.bazarrUrl, keys["bazarr"])).also { keys = emptyMap() }
    }) { s, set ->
        Card("Seerr / Jellyseerr") {
            Field(stringResource(R.string.address), s.seerrUrl.orEmpty(), { set(s.copy(seerrUrl = it)) })
            Field(stringResource(R.string.api_key) + if (s.seerrConfigured) " ✓" else "", keys["seerr"].orEmpty(), { keys = keys + ("seerr" to it) }, secret = true)
            Toggle(stringResource(R.string.everyone_can_request), null, s.usersCanRequest) { set(s.copy(usersCanRequest = it)) }
        }
        Card("Radarr / Sonarr") {
            Field("Radarr", s.radarrUrl.orEmpty(), { set(s.copy(radarrUrl = it)) })
            Field("Radarr " + stringResource(R.string.api_key) + if (s.radarrConfigured) " ✓" else "", keys["radarr"].orEmpty(), { keys = keys + ("radarr" to it) }, secret = true)
            Field("Sonarr", s.sonarrUrl.orEmpty(), { set(s.copy(sonarrUrl = it)) })
            Field("Sonarr " + stringResource(R.string.api_key) + if (s.sonarrConfigured) " ✓" else "", keys["sonarr"].orEmpty(), { keys = keys + ("sonarr" to it) }, secret = true)
        }
        Card("Bazarr") {
            Field(stringResource(R.string.address), s.bazarrUrl.orEmpty(), { set(s.copy(bazarrUrl = it)) })
            Field(stringResource(R.string.api_key) + if (s.bazarrConfigured) " ✓" else "", keys["bazarr"].orEmpty(), { keys = keys + ("bazarr" to it) }, secret = true)
        }
        GlassButton(stringResource(R.string.test_connections), { scope.launch { checks = runCatching { api.testRequestSettings() }.getOrDefault(emptyList()) } }, Modifier.fillMaxWidth())
        checks.forEach { c -> Text("${c.app}: " + if (c.ok) "✓ ${c.version.orEmpty()}" else c.error.orEmpty(), color = if (c.ok) Kino.success else Kino.danger) }
    }
}

@Composable
private fun EnhancementsPage(state: AppState) {
    val api = state.api ?: return
    SettingsDoc(load = { api.enhancements() }, save = { api.saveEnhancements(it).also { r -> state.enhancements = r } }) { e, set ->
        Card(null) {
            Text(stringResource(R.string.enhancements_intro), color = Kino.muted, fontSize = 13.sp)
            Toggle(stringResource(R.string.enh_quality), null, e.qualityTags) { set(e.copy(qualityTags = it)) }
            Toggle(stringResource(R.string.enh_random), null, e.randomButton) { set(e.copy(randomButton = it)) }
            Toggle(stringResource(R.string.enh_pause), null, e.pauseScreen) { set(e.copy(pauseScreen = it)) }
            Toggle(stringResource(R.string.enh_continue), null, e.removeFromContinue) { set(e.copy(removeFromContinue = it)) }
            Toggle(stringResource(R.string.enh_search), null, e.discoverInSearch) { set(e.copy(discoverInSearch = it)) }
            Toggle(stringResource(R.string.enh_arr), null, e.arrLinks) { set(e.copy(arrLinks = it)) }
            Toggle(stringResource(R.string.enh_shortcuts), null, e.shortcutsHelp) { set(e.copy(shortcutsHelp = it)) }
            Toggle(stringResource(R.string.enh_pip), null, e.autoPip) { set(e.copy(autoPip = it)) }
        }
    }
}

@Composable
private fun TasksPage(state: AppState) {
    val api = state.api ?: return
    var tick by remember { mutableIntStateOf(0) }
    val (tasks, _, error) = rememberLoad(tick) { api.tasks() }
    val scope = rememberCoroutineScope()
    ErrorLine(error)
    tasks?.forEach { t ->
        Card(stringResource(when (t.key) { "scan_all" -> R.string.task_scan; "refresh_metadata" -> R.string.task_metadata; "cleanup_images" -> R.string.task_cleanup; else -> R.string.task_analyze })) {
            Text(listOfNotNull(t.lastRunAt?.take(16)?.replace('T', ' '), t.lastState, t.interval?.let { stringResource(R.string.every, it) }).joinToString(" · "), color = Kino.muted, fontSize = 13.sp)
            GlassButton(if (t.running) stringResource(R.string.running) else stringResource(R.string.run_now), { scope.launch { runCatching { api.runTask(t.key) }; tick++ } }, enabled = !t.running)
        }
    }
}

@Composable
private fun ActivityPage(state: AppState) {
    val api = state.api ?: return
    var offset by remember { mutableIntStateOf(0) }
    val (page, loading, error) = rememberLoad(offset) { api.activity(offset) }
    ErrorLine(error)
    if (loading) Loading()
    page?.entries?.forEach { e ->
        Column(Modifier.padding(vertical = 6.dp)) {
            Text(e.action + (e.user?.let { " · $it" }.orEmpty()), color = Kino.text, fontSize = 14.sp)
            Text(e.at.take(19).replace('T', ' '), color = Kino.faint, fontSize = 12.sp)
        }
    }
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        GlassButton(stringResource(R.string.newer), { offset = (offset - 50).coerceAtLeast(0) }, enabled = offset > 0)
        GlassButton(stringResource(R.string.older), { offset += 50 }, enabled = (page?.total ?: 0) > offset + 50)
    }
}

@Composable
private fun LogsPage(state: AppState) {
    val api = state.api ?: return
    var level by remember { mutableStateOf("info") }
    var q by remember { mutableStateOf("") }
    val (logs, loading, error) = rememberLoad(level to q) { api.logs(level, q) }
    FieldRow(stringResource(R.string.level)) { Picker(level, listOf("debug", "info", "warn", "error").map { it to it }, { level = it }) }
    OutlinedTextField(q, { q = it }, placeholder = { Text(stringResource(R.string.search)) }, singleLine = true, modifier = Modifier.fillMaxWidth(), shape = CardShape)
    ErrorLine(error)
    if (loading) Loading()
    logs?.forEach { l ->
        Column(Modifier.padding(vertical = 4.dp)) {
            Text("${l.time.take(19).replace('T', ' ')} ${l.level.uppercase()} ${l.source}", color = when (l.level) { "error" -> Kino.danger; "warn" -> Kino.accent; else -> Kino.faint }, fontSize = 11.sp)
            Text(l.message + (l.attrs?.let { "  $it" }.orEmpty()), color = Kino.text, fontSize = 12.sp, fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace)
        }
    }
}

@Composable
private fun DevicesPage(state: AppState) {
    val api = state.api ?: return
    var tick by remember { mutableIntStateOf(0) }
    val (sessions, _, error) = rememberLoad(tick) { api.sessions() }
    val scope = rememberCoroutineScope()
    ErrorLine(error)
    sessions?.forEach { s ->
        Card(s.deviceName) {
            Text("${s.username} · ${s.client} · ${s.lastSeenAt.take(16).replace('T', ' ')}", color = Kino.muted, fontSize = 13.sp)
            if (!s.current) TextButton(onClick = { scope.launch { runCatching { api.revokeSession(s.id) }; tick++ } }) { Text(stringResource(R.string.sign_out), color = Kino.danger) }
        }
    }
}

@Composable
private fun InvitesPage(state: AppState) {
    val api = state.api ?: return
    var tick by remember { mutableIntStateOf(0) }
    val (invites, _, error) = rememberLoad(tick) { api.invites() }
    val scope = rememberCoroutineScope()
    var link by remember { mutableStateOf<String?>(null) }
    ErrorLine(error)
    PrimaryButton(stringResource(R.string.create_invite), { scope.launch { runCatching { api.createInvite(CreateInvite(72, 1)) }.onSuccess { link = api.base.resolve("/invite/${it.token}").toString(); tick++ } } }, Modifier.fillMaxWidth())
    link?.let { androidx.compose.foundation.text.selection.SelectionContainer { Text(it, color = Kino.text) } }
    invites?.forEach { i ->
        Card(i.note.ifBlank { i.id.take(8) }) {
            Text("${i.uses}/${i.maxUses} · ${i.expiresAt.take(16).replace('T', ' ')}", color = Kino.muted, fontSize = 13.sp)
            TextButton(onClick = { scope.launch { runCatching { api.deleteInvite(i.id) }; tick++ } }) { Text(stringResource(R.string.delete), color = Kino.danger) }
        }
    }
}

@Composable
private fun ApiKeysPage(state: AppState) {
    val api = state.api ?: return
    var tick by remember { mutableIntStateOf(0) }
    val (keys, _, error) = rememberLoad(tick) { api.apiKeys() }
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf("") }
    var created by remember { mutableStateOf<String?>(null) }
    ErrorLine(error)
    Card(stringResource(R.string.create_api_key)) {
        Field(stringResource(R.string.name), name, { name = it })
        PrimaryButton(stringResource(R.string.create_api_key), { scope.launch { runCatching { api.createApiKey(name) }.onSuccess { created = it.key; name = ""; tick++ } } }, Modifier.fillMaxWidth(), enabled = name.isNotBlank())
        created?.let { androidx.compose.foundation.text.selection.SelectionContainer { Text(it, color = Kino.text, fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace) } }
    }
    keys?.forEach { k ->
        Card(k.name) {
            Text("${k.prefix}… · ${k.createdBy} · ${k.lastUsedAt?.take(10) ?: "—"}", color = Kino.muted, fontSize = 13.sp)
            TextButton(onClick = { scope.launch { runCatching { api.deleteApiKey(k.id) }; tick++ } }) { Text(stringResource(R.string.delete), color = Kino.danger) }
        }
    }
}

@Composable
private fun ServerPage(state: AppState) {
    val api = state.api ?: return
    SettingsDoc(load = { api.serverSettings() }, save = { api.saveServerSettings(it) }) { s, set ->
        Card(null) { Field(stringResource(R.string.server_name), s.serverName, { set(s.copy(serverName = it)) }) }
    }
}

@Composable
private fun BrandingPage(state: AppState) {
    val api = state.api ?: return
    SettingsDoc(load = { api.branding() }, save = { api.saveBranding(it) }) { b, set ->
        Card(null) {
            OutlinedTextField(b.loginDisclaimer, { set(b.copy(loginDisclaimer = it)) }, label = { Text(stringResource(R.string.login_message)) }, minLines = 3, modifier = Modifier.fillMaxWidth(), shape = CardShape)
            OutlinedTextField(b.customCss, { set(b.copy(customCss = it)) }, label = { Text(stringResource(R.string.custom_css)) }, minLines = 6, modifier = Modifier.fillMaxWidth(), shape = CardShape)
        }
    }
}

/** Approve a Quick Connect code shown on a TV or another new device. */
@Composable
private fun QuickConnectPage(state: AppState) {
    val api = state.api ?: return
    var code by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var approved by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    Text(stringResource(R.string.qc_approve_intro), color = Kino.muted, fontSize = 15.sp)
    OutlinedTextField(code, { v -> code = v.filter { it.isDigit() }.take(6); approved = null }, label = { Text(stringResource(R.string.qc_code)) }, singleLine = true,
        keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.NumberPassword),
        textStyle = androidx.compose.ui.text.TextStyle(fontSize = 28.sp, letterSpacing = 6.sp), modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
    PrimaryButton(stringResource(R.string.qc_approve), enabled = !busy && code.length == 6, modifier = Modifier.fillMaxWidth(), onClick = {
        busy = true; error = null
        scope.launch {
            runCatching { api.quickConnectAuthorize(code) }.onSuccess { approved = it.deviceName; code = "" }.onFailure { error = it }
            busy = false
        }
    })
    approved?.let { Text(stringResource(R.string.qc_approved_device, it), color = Kino.success) }
    if ((error as? ApiException)?.status == 404) Text(stringResource(R.string.qc_unknown_code), color = Kino.danger) else ErrorLine(error)
}
