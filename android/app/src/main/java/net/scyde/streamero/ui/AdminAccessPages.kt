package net.scyde.streamero.ui

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import net.scyde.streamero.AppState
import net.scyde.streamero.R
import net.scyde.streamero.api.*

// ---- Live TV sources (tuners and XMLTV guides) ----

/** Tuners and guide sources; channel mapping and DVR settings stay on the web (linked). */
@Composable
fun LiveTvAdminPage(state: AppState) {
    val api = state.api ?: return
    val ctx = LocalContext.current
    var tick by remember { mutableIntStateOf(0) }
    TunersCard(api, tick) { tick++ }
    GuidesCard(api, tick) { tick++ }
    Card(stringResource(R.string.livetv_mapping)) {
        Intro(stringResource(R.string.livetv_mapping_on_web))
        GlassButton(stringResource(R.string.open_on_web), {
            runCatching { ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(api.base.resolve("/settings/livetv").toString()))) }
        }, Modifier.fillMaxWidth())
    }
}

@Composable
private fun TestOutcome(r: LiveTvTestResult?) {
    if (r != null) Text(r.message, color = if (r.ok) Kino.success else Kino.danger, fontSize = 14.sp)
}

@Composable
private fun TunersCard(api: Api, tick: Int, onChanged: () -> Unit) {
    val (list, loadError) = rememberReload(tick) { api.tuners() }
    val scope = rememberCoroutineScope()
    var editing by remember { mutableStateOf<LiveTvTuner?>(null) }
    var adding by remember { mutableStateOf<LiveTvTunerInput?>(null) }
    var deleting by remember { mutableStateOf<LiveTvTuner?>(null) }
    var found by remember { mutableStateOf<List<LiveTvDiscoveredTuner>?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    deleting?.let { t ->
        ConfirmDialog(stringResource(R.string.livetv_delete_tuner_confirm, t.name), stringResource(R.string.delete), { deleting = null }) {
            scope.launch { runCatching { api.deleteTuner(t.id) }.onFailure { error = it }; onChanged() }
        }
    }
    Card(stringResource(R.string.livetv_tuners)) {
        Intro(stringResource(R.string.livetv_tuners_intro))
        if (editing == null && adding == null) Actions {
            PrimaryButton(stringResource(R.string.livetv_add_tuner), { adding = LiveTvTunerInput("m3u", "") })
            GlassButton(stringResource(R.string.livetv_discover), { error = null; scope.launch { runCatching { api.discoverTuners() }.onSuccess { found = it }.onFailure { error = it } } })
        }
        found?.let { f ->
            Notice(Kino.muted) {
                if (f.isEmpty()) Text(stringResource(R.string.livetv_discover_none), color = Kino.muted, fontSize = 14.sp)
                f.forEach { d ->
                    Text(d.name, color = Kino.text, fontWeight = FontWeight.SemiBold)
                    Text(listOfNotNull(d.model, d.url).joinToString(" · "), color = Kino.muted, fontSize = 13.sp)
                    if (d.added) Pill(stringResource(R.string.livetv_already_added))
                    else GlassButton(stringResource(R.string.add), { adding = LiveTvTunerInput("hdhomerun", d.name, url = d.url, tunerCount = d.tunerCount); found = null })
                }
            }
        }
        adding?.let { a -> TunerForm(api, null, a, onDone = { adding = null; onChanged() }, onCancel = { adding = null }) }
        if (list == null && loadError == null) Loading()
        if (list?.isEmpty() == true) Text(stringResource(R.string.livetv_no_tuners), color = Kino.faint, fontSize = 14.sp)
        list?.forEach { t ->
            HorizontalDivider(color = Kino.border, modifier = Modifier.padding(vertical = 6.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(t.name, color = Kino.text, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f, fill = false))
                Pill(stringResource(if (t.kind == "hdhomerun") R.string.livetv_kind_hdhomerun else R.string.livetv_kind_m3u))
                if (!t.enabled) Pill(stringResource(R.string.off))
            }
            Text(listOfNotNull(t.model, t.url ?: t.filePath, pluralStringResource(R.plurals.livetv_channels, t.channelCount, t.channelCount),
                if (t.tunerCount > 0) pluralStringResource(R.plurals.livetv_tuners_n, t.tunerCount, t.tunerCount) else stringResource(R.string.livetv_unlimited),
                t.lastRefreshAt?.let { stringResource(R.string.livetv_refreshed_at, shortDateTime(it)) }).joinToString(" · "), color = Kino.muted, fontSize = 13.sp)
            t.error?.let { Text(it, color = Kino.danger, fontSize = 13.sp) }
            if (editing?.id == t.id) TunerForm(api, t, LiveTvTunerInput(t.kind, t.name, t.url, t.filePath, t.tunerCount, t.userAgent, t.enabled),
                onDone = { editing = null; onChanged() }, onCancel = { editing = null })
            else Actions {
                GlassButton(stringResource(R.string.livetv_refresh_channels), { scope.launch { runCatching { api.refreshTuner(t.id) }.onFailure { error = it }; onChanged() } })
                GlassButton(stringResource(R.string.edit), { editing = t })
                DangerAction(stringResource(R.string.delete)) { deleting = t }
            }
        }
        ErrorLine(loadError ?: error)
    }
}

@Composable
private fun TunerForm(api: Api, tuner: LiveTvTuner?, start: LiveTvTunerInput, onDone: () -> Unit, onCancel: () -> Unit) {
    var kind by remember { mutableStateOf(start.kind) }
    var name by remember { mutableStateOf(start.name) }
    var fromFile by remember { mutableStateOf(start.filePath != null) }
    var url by remember { mutableStateOf(start.url.orEmpty()) }
    var filePath by remember { mutableStateOf(start.filePath.orEmpty()) }
    var count by remember { mutableStateOf(start.tunerCount?.toString().orEmpty()) }
    var userAgent by remember { mutableStateOf(start.userAgent.orEmpty()) }
    var enabled by remember { mutableStateOf(start.enabled) }
    var test by remember { mutableStateOf<LiveTvTestResult?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val file = kind == "m3u" && fromFile
    fun body() = LiveTvTunerInput(kind, name.trim(), url = if (!file) url.trim().ifEmpty { null } else null,
        filePath = if (file) filePath.trim().ifEmpty { null } else null, tunerCount = optionalInt(count, 0..64),
        userAgent = if (kind == "m3u") userAgent.trim().ifEmpty { null } else null, enabled = enabled)
    val valid = sourceValid(name, file, url, filePath)
    Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        FieldRow(stringResource(R.string.kind)) { Picker(kind, listOf("m3u" to stringResource(R.string.livetv_kind_m3u), "hdhomerun" to stringResource(R.string.livetv_kind_hdhomerun)), { kind = it; test = null }) }
        Field(stringResource(R.string.name), name, { name = it.take(100) })
        if (kind == "m3u") FieldRow(stringResource(R.string.livetv_source)) {
            Picker(fromFile, listOf(false to stringResource(R.string.livetv_from_url), true to stringResource(R.string.livetv_from_file)), { fromFile = it })
        }
        if (file) Field(stringResource(R.string.livetv_playlist_file), filePath, { filePath = it }, hint = stringResource(R.string.livetv_file_hint), placeholder = "/config/livetv/channels.m3u")
        else Field(stringResource(if (kind == "m3u") R.string.livetv_playlist_url else R.string.livetv_device_address), url, { url = it.trim() },
            hint = if (kind == "hdhomerun") stringResource(R.string.livetv_device_address_hint) else null,
            placeholder = if (kind == "m3u") "https://example.com/playlist.m3u" else "http://192.168.1.50")
        Field(stringResource(R.string.livetv_tuner_count), count, { v -> count = v.filter { it.isDigit() }.take(2) },
            hint = stringResource(if (kind == "m3u") R.string.livetv_tuner_count_m3u else R.string.livetv_tuner_count_hdhr))
        if (kind == "m3u") Field(stringResource(R.string.livetv_user_agent), userAgent, { userAgent = it.take(300) }, hint = stringResource(R.string.livetv_user_agent_hint))
        Toggle(stringResource(R.string.enabled), null, enabled) { enabled = it }
        Actions {
            GlassButton(stringResource(R.string.test), enabled = valid && !busy, onClick = {
                busy = true; error = null; test = null
                scope.launch { runCatching { api.testTuner(body()) }.onSuccess { test = it }.onFailure { error = it }; busy = false }
            })
            PrimaryButton(stringResource(if (tuner == null) R.string.livetv_add_tuner else R.string.save), enabled = valid && !busy, onClick = {
                busy = true; error = null
                scope.launch {
                    runCatching { if (tuner == null) api.createTuner(body()) else api.updateTuner(tuner.id, body()) }.onSuccess { onDone() }.onFailure { error = it }
                    busy = false
                }
            })
            GlassButton(stringResource(R.string.cancel), onCancel)
        }
        TestOutcome(test)
        ErrorLine(error)
    }
}

@Composable
private fun GuidesCard(api: Api, tick: Int, onChanged: () -> Unit) {
    val (list, loadError) = rememberReload(tick) { api.guideSources() }
    val scope = rememberCoroutineScope()
    var editing by remember { mutableStateOf<LiveTvGuideSource?>(null) }
    var adding by remember { mutableStateOf(false) }
    var deleting by remember { mutableStateOf<LiveTvGuideSource?>(null) }
    var queued by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    deleting?.let { g ->
        ConfirmDialog(stringResource(R.string.livetv_delete_guide_confirm, g.name), stringResource(R.string.delete), { deleting = null }) {
            scope.launch { runCatching { api.deleteGuideSource(g.id) }.onFailure { error = it }; onChanged() }
        }
    }
    Card(stringResource(R.string.livetv_guides)) {
        Intro(stringResource(R.string.livetv_guides_intro))
        if (adding) GuideForm(api, null, onDone = { adding = false; onChanged() }, onCancel = { adding = false })
        else if (editing == null) PrimaryButton(stringResource(R.string.livetv_add_guide), { adding = true })
        if (list == null && loadError == null) Loading()
        if (list?.isEmpty() == true) Text(stringResource(R.string.livetv_no_guides), color = Kino.faint, fontSize = 14.sp)
        list?.forEach { g ->
            HorizontalDivider(color = Kino.border, modifier = Modifier.padding(vertical = 6.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(g.name, color = Kino.text, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f, fill = false))
                if (!g.enabled) Pill(stringResource(R.string.off))
            }
            Text(listOfNotNull(g.url ?: g.filePath, pluralStringResource(R.plurals.livetv_channels, g.channelCount, g.channelCount),
                pluralStringResource(R.plurals.livetv_programs, g.programCount, g.programCount),
                pluralStringResource(R.plurals.livetv_every_hours, g.refreshHours, g.refreshHours),
                g.lastRefreshAt?.let { stringResource(R.string.livetv_refreshed_at, shortDateTime(it)) } ?: stringResource(R.string.livetv_not_loaded)).joinToString(" · "),
                color = Kino.muted, fontSize = 13.sp)
            g.lastResult?.let { Text(it, color = Kino.faint, fontSize = 13.sp) }
            g.error?.let { Text(it, color = Kino.danger, fontSize = 13.sp) }
            if (editing?.id == g.id) GuideForm(api, g, onDone = { editing = null; onChanged() }, onCancel = { editing = null })
            else Actions {
                GlassButton(stringResource(R.string.livetv_refresh_now), { scope.launch { runCatching { api.refreshGuideSource(g.id) }.onSuccess { queued = true }.onFailure { error = it } } })
                GlassButton(stringResource(R.string.edit), { editing = g })
                DangerAction(stringResource(R.string.delete)) { deleting = g }
            }
        }
        if (queued) Text(stringResource(R.string.livetv_refresh_queued), color = Kino.success, fontSize = 14.sp)
        ErrorLine(loadError ?: error)
    }
}

@Composable
private fun GuideForm(api: Api, guide: LiveTvGuideSource?, onDone: () -> Unit, onCancel: () -> Unit) {
    var name by remember { mutableStateOf(guide?.name.orEmpty()) }
    var fromFile by remember { mutableStateOf(guide?.filePath != null) }
    var url by remember { mutableStateOf(guide?.url.orEmpty()) }
    var filePath by remember { mutableStateOf(guide?.filePath.orEmpty()) }
    var timezone by remember { mutableStateOf(guide?.timezone.orEmpty()) }
    var hours by remember { mutableStateOf((guide?.refreshHours ?: 12).toString()) }
    var enabled by remember { mutableStateOf(guide?.enabled ?: true) }
    var test by remember { mutableStateOf<LiveTvTestResult?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    fun body() = LiveTvGuideSourceInput(name.trim(), url = if (!fromFile) url.trim().ifEmpty { null } else null,
        filePath = if (fromFile) filePath.trim().ifEmpty { null } else null, timezone = timezone.trim().ifEmpty { null },
        refreshHours = optionalInt(hours, 1..168) ?: 12, enabled = enabled)
    val valid = sourceValid(name, fromFile, url, filePath)
    Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Field(stringResource(R.string.name), name, { name = it.take(100) })
        FieldRow(stringResource(R.string.livetv_source)) {
            Picker(fromFile, listOf(false to stringResource(R.string.livetv_from_url), true to stringResource(R.string.livetv_from_file)), { fromFile = it })
        }
        if (fromFile) Field(stringResource(R.string.livetv_guide_file), filePath, { filePath = it }, hint = stringResource(R.string.livetv_file_hint), placeholder = "/config/livetv/guide.xml")
        else Field(stringResource(R.string.livetv_guide_url), url, { url = it.trim() }, hint = stringResource(R.string.livetv_guide_url_hint), placeholder = "https://example.com/guide.xml.gz")
        Field(stringResource(R.string.livetv_timezone), timezone, { timezone = it.take(64) }, hint = stringResource(R.string.livetv_timezone_hint), placeholder = "Europe/Zagreb")
        Field(stringResource(R.string.livetv_refresh_hours), hours, { v -> hours = v.filter { it.isDigit() }.take(3) })
        Toggle(stringResource(R.string.enabled), null, enabled) { enabled = it }
        Actions {
            GlassButton(stringResource(R.string.test), enabled = valid && !busy, onClick = {
                busy = true; error = null; test = null
                scope.launch { runCatching { api.testGuideSource(body()) }.onSuccess { test = it }.onFailure { error = it }; busy = false }
            })
            PrimaryButton(stringResource(if (guide == null) R.string.livetv_add_guide else R.string.save), enabled = valid && !busy, onClick = {
                busy = true; error = null
                scope.launch {
                    runCatching { if (guide == null) api.createGuideSource(body()) else api.updateGuideSource(guide.id, body()) }.onSuccess { onDone() }.onFailure { error = it }
                    busy = false
                }
            })
            GlassButton(stringResource(R.string.cancel), onCancel)
        }
        TestOutcome(test)
        ErrorLine(error)
    }
}

// ---- Sign-in: public address, OpenID Connect providers, LDAP ----

@Composable
fun SsoPage(state: AppState) {
    val api = state.api ?: return
    Intro(stringResource(R.string.sso_intro))
    var tick by remember { mutableIntStateOf(0) }
    AuthGeneralCard(api) { tick++ }
    OidcCard(api, tick)
    LdapCard(api)
}

@Composable
private fun AuthGeneralCard(api: Api, onSaved: () -> Unit) {
    val scope = rememberCoroutineScope()
    var saved by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var loaded by remember { mutableStateOf<AuthSettings?>(null) }
    var form by remember { mutableStateOf<AuthSettingsInput?>(null) }
    LaunchedEffect(Unit) { runCatching { api.authSettings() }.onSuccess { loaded = it; form = AuthSettingsInput(it.publicUrl, it.disablePasswordLogin) }.onFailure { error = it } }
    Card(stringResource(R.string.sso_title)) {
        val f = form ?: run { if (error == null) Loading() else ErrorLine(error); return@Card }
        Field(stringResource(R.string.sso_public_url), f.publicUrl, { form = f.copy(publicUrl = it.trim()); saved = false }, hint = stringResource(R.string.sso_public_url_hint),
            placeholder = api.base.toString().trimEnd('/'))
        Toggle(stringResource(R.string.sso_disable_password), stringResource(R.string.sso_disable_password_hint), f.disablePasswordLogin) { form = f.copy(disablePasswordLogin = it); saved = false }
        if (loaded?.recoveryActive == true) Text(stringResource(R.string.sso_recovery_active), color = Kino.danger, fontSize = 13.sp)
        PrimaryButton(stringResource(R.string.save), modifier = Modifier.fillMaxWidth(), onClick = {
            error = null
            scope.launch { runCatching { api.saveAuthSettings(f) }.onSuccess { loaded = it; saved = true; onSaved() }.onFailure { error = it } }
        })
        if (saved) Text(stringResource(R.string.saved), color = Kino.success)
        ErrorLine(error)
    }
}

/** Starting points for well-known providers (as on the web); the admin fills in their own address. */
private val PRESETS = listOf(
    OidcProviderInput("authentik", "Authentik", "https://auth.example.com/application/o/streamero/"),
    OidcProviderInput("keycloak", "Keycloak", "https://auth.example.com/realms/home"),
    OidcProviderInput("authelia", "Authelia", "https://auth.example.com", scopes = listOf("openid", "profile", "email", "groups")),
    OidcProviderInput("entra", "Microsoft", "https://login.microsoftonline.com/<tenant-id>/v2.0"),
    OidcProviderInput("google", "Google", "https://accounts.google.com", usernameClaim = "email"),
)

@Composable
private fun OidcCard(api: Api, outerTick: Int) {
    var tick by remember { mutableIntStateOf(0) }
    val (list, loadError) = rememberReload(outerTick to tick) { api.oidcProviders() }
    val scope = rememberCoroutineScope()
    var editing by remember { mutableStateOf<OidcProvider?>(null) }
    var adding by remember { mutableStateOf(false) }
    var deleting by remember { mutableStateOf<OidcProvider?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    deleting?.let { p ->
        ConfirmDialog(stringResource(R.string.sso_delete_confirm, p.label), stringResource(R.string.delete), { deleting = null }) {
            scope.launch { runCatching { api.deleteOidcProvider(p.id) }.onFailure { error = it }; tick++ }
        }
    }
    Card(stringResource(R.string.sso_oidc_title)) {
        Intro(stringResource(R.string.sso_oidc_intro))
        if (list?.isEmpty() == true && !adding) Text(stringResource(R.string.sso_no_providers), color = Kino.faint, fontSize = 14.sp)
        list?.forEach { p ->
            HorizontalDivider(color = Kino.border, modifier = Modifier.padding(vertical = 6.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(p.label, color = Kino.text, fontSize = 16.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f, fill = false))
                Pill(stringResource(if (p.enabled) R.string.on else R.string.off), if (p.enabled) Kino.success else Kino.muted)
            }
            Text(p.issuer, color = Kino.muted, fontSize = 13.sp)
            if (p.redirectUri.isNotBlank()) {
                Text(stringResource(R.string.sso_redirect_uri), color = Kino.faint, fontSize = 12.sp)
                CopyableText(p.redirectUri)
            } else Text(stringResource(R.string.sso_needs_public_url), color = Kino.danger, fontSize = 13.sp)
            if (editing?.id == p.id) OidcForm(api, p, onDone = { editing = null; tick++ }, onCancel = { editing = null })
            else Actions {
                GlassButton(stringResource(R.string.edit), { editing = p; adding = false })
                DangerAction(stringResource(R.string.delete)) { deleting = p }
            }
        }
        if (adding) OidcForm(api, null, onDone = { adding = false; tick++ }, onCancel = { adding = false })
        else if (editing == null) PrimaryButton(stringResource(R.string.sso_add_provider), { adding = true }, Modifier.fillMaxWidth())
        ErrorLine(loadError ?: error)
    }
}

@Composable
private fun OidcForm(api: Api, existing: OidcProvider?, onDone: () -> Unit, onCancel: () -> Unit) {
    var f by remember { mutableStateOf(existing?.toInput() ?: OidcProviderInput()) }
    var scopes by remember { mutableStateOf(f.scopes.joinToString(" ")) }
    var secret by remember { mutableStateOf("") }
    var preset by remember { mutableStateOf("") }
    var test by remember { mutableStateOf<OidcTestResult?>(null) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val valid = isValidOidcId(f.id) && f.label.isNotBlank() && isHttpUrl(f.issuer) && f.clientId.isNotBlank()
    Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (existing == null) FieldRow(stringResource(R.string.sso_preset)) {
            Picker(preset, listOf("" to stringResource(R.string.sso_preset_generic)) + PRESETS.map { it.id to it.label }, { id ->
                preset = id
                PRESETS.firstOrNull { it.id == id }?.let { p -> f = f.copy(id = p.id, label = p.label, issuer = p.issuer, usernameClaim = p.usernameClaim, scopes = p.scopes); scopes = p.scopes.joinToString(" ") }
            })
        }
        Field(stringResource(R.string.sso_label), f.label, { f = f.copy(label = it.take(60)) }, hint = stringResource(R.string.sso_label_hint))
        Field(stringResource(R.string.sso_id), f.id, { f = f.copy(id = it.lowercase().take(32)) }, hint = stringResource(R.string.sso_id_hint), enabled = existing == null)
        Field(stringResource(R.string.sso_issuer), f.issuer, { f = f.copy(issuer = it.trim()) }, hint = stringResource(R.string.sso_issuer_hint))
        Field(stringResource(R.string.sso_client_id), f.clientId, { f = f.copy(clientId = it.trim()) })
        // Write-only: the server keeps the stored secret when this stays empty.
        Field(stringResource(if (existing?.clientSecretSet == true) R.string.sso_replace_secret else R.string.sso_client_secret), secret, { secret = it },
            secret = true, hint = stringResource(R.string.sso_secret_hint))
        Field(stringResource(R.string.sso_scopes), scopes, { scopes = it }, hint = stringResource(R.string.sso_scopes_hint))
        Field(stringResource(R.string.sso_username_claim), f.usernameClaim, { f = f.copy(usernameClaim = it.trim()) })
        Field(stringResource(R.string.sso_email_claim), f.emailClaim, { f = f.copy(emailClaim = it.trim()) })
        Field(stringResource(R.string.sso_name_claim), f.nameClaim, { f = f.copy(nameClaim = it.trim()) })
        Field(stringResource(R.string.sso_groups_claim), f.groupsClaim, { f = f.copy(groupsClaim = it.trim()) }, hint = stringResource(R.string.sso_groups_claim_hint))
        Field(stringResource(R.string.sso_admin_group), f.adminGroup, { f = f.copy(adminGroup = it) }, hint = stringResource(R.string.sso_admin_group_hint))
        Field(stringResource(R.string.sso_allowed_group), f.allowedGroup, { f = f.copy(allowedGroup = it) }, hint = stringResource(R.string.sso_allowed_group_hint))
        Toggle(stringResource(R.string.sso_auto_create), stringResource(R.string.sso_auto_create_hint), f.autoCreate) { f = f.copy(autoCreate = it) }
        Toggle(stringResource(R.string.sso_link_by_email), stringResource(R.string.sso_link_by_email_hint), f.linkByEmail) { f = f.copy(linkByEmail = it) }
        Toggle(stringResource(R.string.sso_trust_email), stringResource(R.string.sso_trust_email_hint), f.trustEmail) { f = f.copy(trustEmail = it) }
        Toggle(stringResource(R.string.enabled), stringResource(R.string.sso_enabled_hint), f.enabled) { f = f.copy(enabled = it) }
        test?.let { t ->
            Text(stringResource(R.string.sso_test_ok, t.signingKeys) + if (!t.pkceS256) " " + stringResource(R.string.sso_test_no_pkce) else "", color = Kino.success, fontSize = 14.sp)
        }
        ErrorLine(error)
        Actions {
            PrimaryButton(stringResource(R.string.save), enabled = valid && !busy, onClick = {
                busy = true; error = null
                val body = f.copy(clientSecret = secret.ifEmpty { null }, scopes = parseScopes(scopes))
                scope.launch {
                    runCatching { if (existing == null) api.createOidcProvider(body) else api.updateOidcProvider(existing.id, body) }.onSuccess { onDone() }.onFailure { error = it }
                    busy = false
                }
            })
            GlassButton(stringResource(R.string.sso_test_connection), enabled = isHttpUrl(f.issuer) && !busy, onClick = {
                busy = true; error = null; test = null
                scope.launch { runCatching { api.testOidcProvider(f.issuer) }.onSuccess { test = it }.onFailure { error = it }; busy = false }
            })
            GlassButton(stringResource(R.string.cancel), onCancel)
        }
    }
}

@Composable
private fun LdapCard(api: Api) {
    val scope = rememberCoroutineScope()
    var loaded by remember { mutableStateOf<LdapSettings?>(null) }
    var f by remember { mutableStateOf<LdapSettingsInput?>(null) }
    var testUser by remember { mutableStateOf("") }
    var test by remember { mutableStateOf<LdapTestResult?>(null) }
    var saved by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<Throwable?>(null) }
    var busy by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { runCatching { api.ldapSettings() }.onSuccess { loaded = it; f = it.toInput() }.onFailure { error = it } }
    Card(stringResource(R.string.sso_ldap_title)) {
        val s = f ?: run { if (error == null) Loading() else ErrorLine(error); return@Card }
        fun set(n: LdapSettingsInput) { f = n; saved = false }
        Pill(stringResource(if (loaded?.enabled == true) R.string.on else R.string.off), if (loaded?.enabled == true) Kino.success else Kino.muted)
        Intro(stringResource(R.string.sso_ldap_intro))
        Toggle(stringResource(R.string.sso_ldap_enabled), stringResource(R.string.sso_ldap_enabled_hint), s.enabled) { set(s.copy(enabled = it)) }
        Field(stringResource(R.string.server), s.url, { set(s.copy(url = it.trim())) }, hint = "ldaps://host:636 · ldap://host:389", placeholder = "ldaps://ldap.example.com:636")
        Toggle("StartTLS", stringResource(R.string.sso_start_tls_hint), s.startTls) { set(s.copy(startTls = it)) }
        Toggle(stringResource(R.string.sso_skip_verify), stringResource(R.string.sso_skip_verify_hint), s.skipVerify) { set(s.copy(skipVerify = it)) }
        if (s.url.startsWith("ldap://") && !s.startTls) Text(stringResource(R.string.sso_plain_warning), color = Kino.danger, fontSize = 13.sp)
        OutlinedTextField(s.caCert, { set(s.copy(caCert = it)) }, label = { Text(stringResource(R.string.sso_ca_cert)) }, minLines = 3,
            placeholder = { Text("-----BEGIN CERTIFICATE-----", color = Kino.faint) }, modifier = Modifier.fillMaxWidth().dpadExit(), shape = CardShape)
        Field(stringResource(R.string.sso_bind_dn), s.bindDn, { set(s.copy(bindDn = it)) }, placeholder = "cn=streamero,ou=services,dc=example,dc=org")
        Field(stringResource(if (loaded?.bindPasswordSet == true) R.string.sso_replace_secret else R.string.sso_bind_password), s.bindPassword.orEmpty(),
            { set(s.copy(bindPassword = it.ifEmpty { null })) }, secret = true, hint = stringResource(R.string.sso_secret_hint))
        Field(stringResource(R.string.sso_base_dn), s.baseDn, { set(s.copy(baseDn = it)) }, placeholder = "dc=example,dc=org")
        Field(stringResource(R.string.sso_user_filter), s.userFilter, { set(s.copy(userFilter = it)) }, hint = stringResource(R.string.sso_user_filter_hint))
        Field(stringResource(R.string.sso_username_attr), s.usernameAttribute, { set(s.copy(usernameAttribute = it.trim())) }, hint = stringResource(R.string.sso_username_attr_hint))
        Field(stringResource(R.string.sso_email_attr), s.emailAttribute, { set(s.copy(emailAttribute = it.trim())) })
        Field(stringResource(R.string.sso_name_attr), s.nameAttribute, { set(s.copy(nameAttribute = it.trim())) })
        Field(stringResource(R.string.sso_group_attr), s.groupAttribute, { set(s.copy(groupAttribute = it.trim())) })
        Field(stringResource(R.string.sso_group_filter), s.groupFilter, { set(s.copy(groupFilter = it)) }, hint = stringResource(R.string.sso_group_filter_hint))
        Field(stringResource(R.string.sso_admin_group), s.adminGroup, { set(s.copy(adminGroup = it)) }, hint = stringResource(R.string.sso_ldap_admin_group_hint))
        Field(stringResource(R.string.sso_allowed_group), s.allowedGroup, { set(s.copy(allowedGroup = it)) }, hint = stringResource(R.string.sso_ldap_allowed_group_hint))
        Field(stringResource(R.string.sso_test_user), testUser, { testUser = it.take(64) }, hint = stringResource(R.string.sso_test_user_hint))
        test?.let { t ->
            Notice(if (t.ok) Kino.success else Kino.danger) {
                Text(if (t.ok) stringResource(R.string.sso_ldap_test_ok) else stringResource(R.string.sso_ldap_test_failed, t.message), color = Kino.text, fontSize = 14.sp)
                t.userDn?.let { dn ->
                    val yes = stringResource(R.string.yes); val no = stringResource(R.string.no)
                    listOf("DN" to dn, stringResource(R.string.sso_username_attr) to t.username.orEmpty(), stringResource(R.string.sso_email_attr) to (t.email ?: "—"),
                        stringResource(R.string.sso_groups) to (t.groups.joinToString("; ").ifBlank { "—" }),
                        stringResource(R.string.administrator) to (if (t.isAdmin) yes else no), stringResource(R.string.sso_allowed) to (if (t.allowed) yes else no))
                        .forEach { (k, v) -> Text("$k: $v", color = Kino.muted, fontSize = 13.sp) }
                }
            }
        }
        ErrorLine(error)
        Actions {
            PrimaryButton(stringResource(R.string.save), enabled = !busy, onClick = {
                busy = true; error = null
                scope.launch { runCatching { api.saveLdapSettings(s) }.onSuccess { loaded = it; f = it.toInput(); saved = true }.onFailure { error = it }; busy = false }
            })
            GlassButton(stringResource(R.string.sso_test_connection), enabled = s.url.isNotBlank() && !busy, onClick = {
                busy = true; error = null; test = null
                scope.launch { runCatching { api.testLdapSettings(LdapTestRequest(s, testUser.trim().ifEmpty { null })) }.onSuccess { test = it }.onFailure { error = it }; busy = false }
            })
        }
        if (saved) Text(stringResource(R.string.saved), color = Kino.success)
    }
}
