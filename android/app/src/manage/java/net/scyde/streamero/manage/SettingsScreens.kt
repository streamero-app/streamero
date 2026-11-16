package net.scyde.streamero.manage

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import net.scyde.streamero.R
import net.scyde.streamero.ui.*

private data class SettingsLink(val label: Int, val page: String, val arg: String? = null)

private fun settingsLinks(s: ArrService): List<SettingsLink> {
    val common = listOf(
        SettingsLink(R.string.m_s_indexers, "providers", "indexer"),
        SettingsLink(R.string.m_s_download_clients, "providers", "downloadclient"),
        SettingsLink(R.string.m_s_connections, "providers", "notification"),
        SettingsLink(R.string.m_tags, "tags"),
        SettingsLink(R.string.m_s_general, "config", "config/host"),
        SettingsLink(R.string.m_s_ui, "config", "config/ui"),
    )
    return when (s) {
        ArrService.Prowlarr -> listOf(
            SettingsLink(R.string.m_s_indexers, "providers", "indexer"),
            SettingsLink(R.string.m_s_indexer_proxies, "providers", "indexerproxy"),
            SettingsLink(R.string.m_apps, "providers", "applications"),
            SettingsLink(R.string.m_s_app_profiles, "crud", "appprofile"),
            SettingsLink(R.string.m_s_download_clients, "providers", "downloadclient"),
            SettingsLink(R.string.m_s_connections, "providers", "notification"),
            SettingsLink(R.string.m_tags, "tags"),
            SettingsLink(R.string.m_s_general, "config", "config/host"),
            SettingsLink(R.string.m_s_ui, "config", "config/ui"),
        )
        else -> buildList {
            add(SettingsLink(R.string.m_s_media_management, "config", "config/mediamanagement"))
            add(SettingsLink(R.string.m_s_naming, "config", "config/naming"))
            add(SettingsLink(R.string.m_root_folders, "rootfolders"))
            add(SettingsLink(R.string.m_quality_profiles, "qualityprofiles"))
            add(SettingsLink(R.string.m_quality_definitions, "qualitydefinitions"))
            if (s != ArrService.Lidarr) add(SettingsLink(R.string.m_custom_formats, "crud", "customformat"))
            if (s == ArrService.Lidarr) add(SettingsLink(R.string.m_metadata_profiles, "crud", "metadataprofile"))
            if (s == ArrService.Sonarr) add(SettingsLink(R.string.m_language_profiles, "crud", "languageprofile"))
            add(SettingsLink(R.string.m_delay_profiles, "crud", "delayprofile"))
            add(SettingsLink(R.string.m_release_profiles, "crud", "releaseprofile"))
            add(SettingsLink(R.string.m_s_indexer_options, "config", "config/indexer"))
            add(SettingsLink(R.string.m_s_download_client_options, "config", "config/downloadclient"))
            add(SettingsLink(R.string.m_remote_path_mappings, "crud", "remotepathmapping"))
            add(SettingsLink(R.string.m_s_import_lists, "providers", "importlist"))
            add(SettingsLink(R.string.m_s_import_exclusions, "crud", if (s == ArrService.Radarr) "exclusions" else "importlistexclusion"))
            add(SettingsLink(R.string.m_s_metadata, "providers", "metadata"))
            addAll(common)
        }
    }
}

@Composable
fun SettingsIndex(nav: NavHostController, c: ArrClient) {
    ManagePage(nav, stringResource(R.string.settings), c.service.title) {
        items(settingsLinks(c.service)) { l ->
            ListRow(stringResource(l.label), chevron = true) { nav.manage(c.service, l.page, arg = l.arg) }
        }
    }
}

// ---- Provider resources (schema-driven) ----

/** Pending schema picked for a new provider (passed from the list to the editor). */
private object Pending { var schema: JsonObject? = null }

@Composable
fun ProviderList(nav: NavHostController, c: ArrClient, resource: String) {
    val list = rememberLoaded("${c.service}/$resource") { c.list(resource) }
    var picking by remember { mutableStateOf(false) }
    ManagePage(nav, providerTitle(resource), c.service.title, actions = {
        IconButton(onClick = { picking = true }) { Icon(Icons.Outlined.Add, stringResource(R.string.add), tint = Kino.text) }
    }) {
        loaded(list) { l ->
            if (l.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
            items(l) { p ->
                val on = p.b("enable") ?: p.b("enableRss") ?: (p.s("syncLevel")?.let { it != "disabled" }) ?: true
                ListRow(p.s("name") ?: "?", listOfNotNull(p.s("implementationName") ?: p.s("implementation"), p.s("protocol")?.let(::humanize),
                    p.i("priority")?.let { "P$it" }).joinToString(" · "), leading = { Dot(if (on) Kino.success else Kino.faint) }, chevron = true) {
                    nav.manage(c.service, "provider", p.i("id").toString(), resource)
                }
            }
        }
    }
    if (picking) SchemaPicker(c, resource, onDismiss = { picking = false }) { schema ->
        picking = false
        Pending.schema = schema
        nav.manage(c.service, "provider", "new", resource)
    }
}

@Composable
fun providerTitle(resource: String): String = stringResource(when (resource) {
    "indexer" -> R.string.m_s_indexers
    "downloadclient" -> R.string.m_s_download_clients
    "notification" -> R.string.m_s_connections
    "importlist" -> R.string.m_s_import_lists
    "metadata" -> R.string.m_s_metadata
    "applications" -> R.string.m_apps
    "indexerproxy" -> R.string.m_s_indexer_proxies
    else -> R.string.settings
})

/** The implementations a provider resource offers (`/{resource}/schema`), searchable. */
@Composable
private fun SchemaPicker(c: ArrClient, resource: String, onDismiss: () -> Unit, onPick: (JsonObject) -> Unit) {
    val schemas = rememberLoaded("${c.service}/$resource/schema") { c.list("$resource/schema") }
    var q by remember { mutableStateOf("") }
    AlertDialog(onDismissRequest = onDismiss, containerColor = Kino.surface,
        title = { Text(stringResource(R.string.m_choose_type), color = Kino.text) },
        text = {
            Column(Modifier.heightIn(max = 480.dp)) {
                SearchField(q, { q = it }, stringResource(R.string.search))
                ArrError(schemas.error)
                if (schemas.value == null && schemas.error == null) Loading()
                // Presets (e.g. a specific Newznab indexer) come as nested schemas.
                val all = schemas.value.orEmpty().flatMap { s -> listOf(s) + s.objs("presets").map { p -> s.with("name", p.s("name")).with("fields", p.a("fields") ?: s.a("fields")) } }
                val shown = all.filter { q.isBlank() || (it.s("name") + " " + it.s("implementationName")).contains(q, true) }
                androidx.compose.foundation.lazy.LazyColumn {
                    items(shown) { s ->
                        ListRow(s.s("implementationName") ?: s.s("implementation") ?: "?", listOfNotNull(s.s("name")?.takeIf { n -> n != s.s("implementationName") }, s.s("protocol")?.let(::humanize), s.s("privacy")?.let(::humanize)).joinToString(" · ")) { onPick(s) }
                    }
                }
            }
        },
        confirmButton = {}, dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel), color = Kino.muted) } })
}

/** Keys of a provider shown as switches (what it is used for); capability flags stay hidden. */
private fun providerSwitches(p: JsonObject): List<String> = p.keys.filter { k ->
    val v = p[k] as? JsonPrimitive ?: return@filter false
    if (v.isString || v.content !in setOf("true", "false")) return@filter false
    if (k.startsWith("supports") || k == "isDefault" || k.startsWith("has")) return@filter false
    val cap = "supports" + k.replaceFirstChar { it.uppercase() }
    p.b(cap) != false && (k.startsWith("enable") || k.startsWith("on") || k in setOf("removeCompletedDownloads", "removeFailedDownloads", "includeHealthWarnings", "searchForMissingEpisodes", "shouldMonitorExisting", "enableAuto"))
}

/** Add or edit one provider (indexer, download client, list, connection, metadata, application). */
@Composable
fun ProviderEdit(nav: NavHostController, c: ArrClient, resource: String, id: String?) {
    val isNew = id == null || id == "new"
    val loaded = rememberLoaded("$resource/$id") {
        if (isNew) (Pending.schema ?: error("no schema")).without("id").with("name", Pending.schema?.s("name") ?: Pending.schema?.s("implementationName"))
        else c.obj("$resource/$id")
    }
    var p by remember(loaded.value) { mutableStateOf(loaded.value) }
    var form by remember(loaded.value) { mutableStateOf(loaded.value?.let { SchemaFormState.of(it) }) }
    var advanced by remember { mutableStateOf(false) }
    var askDelete by remember { mutableStateOf(false) }
    val (act, run) = rememberAction()
    val savedMsg = stringResource(R.string.saved)
    val testOk = stringResource(R.string.m_test_ok)
    ManagePage(nav, p?.s("name")?.ifBlank { null } ?: providerTitle(resource), p?.s("implementationName"), actions = {
        TextButton(onClick = { advanced = !advanced }) { Text(stringResource(if (advanced) R.string.m_hide_advanced else R.string.m_show_advanced), color = Kino.muted, fontSize = 13.sp) }
    }) {
        loaded(loaded) { _ ->
            val cur = p ?: return@loaded
            val f = form ?: return@loaded
            block {
                p?.s("infoLink")?.let { Text(it, color = Kino.faint, fontSize = 12.sp) }
                FormText(stringResource(R.string.m_name), cur.s("name").orEmpty(), { p = cur.with("name", it) }, error = (act.error as? ArrException)?.messageFor("name"))
                providerSwitches(cur).forEach { k -> Toggle(humanize(k), (act.error as? ArrException)?.messageFor(k), cur.b(k) == true) { p = cur.with(k, it) } }
                if (cur.containsKey("priority")) FormText(stringResource(R.string.m_priority), cur.i("priority")?.toString().orEmpty(), { t -> p = cur.with("priority", t.toIntOrNull() ?: 0) }, number = true)
                if (cur.containsKey("syncLevel")) FieldRow(humanize("syncLevel")) {
                    Picker(cur.s("syncLevel"), listOf("disabled", "addOnly", "fullSync").map { it to humanize(it) }, { p = cur.with("syncLevel", it) })
                }
                if (cur.containsKey("appProfileId")) AppProfilePicker(c, cur) { p = it }
                if (cur.containsKey("tags")) TagPicker(c, cur.ints("tags")) { p = cur.with("tags", it) }
                HorizontalDivider(color = Kino.border)
                SchemaForm(f, { form = it }, act.error, advanced)
                FormErrors(act.error, f.fields.map { it.name } + cur.keys)
                act.done?.let { Text(it, color = Kino.success, fontSize = 14.sp) }
                Actions {
                    PrimaryButton(stringResource(R.string.save), {
                        val body = f.applyTo(cur)
                        run(savedMsg) {
                            val r = if (isNew) c.post(resource, body) else c.put("$resource/${cur.i("id")}", body)
                            if (isNew) { (r as? JsonObject)?.i("id")?.let { nid -> nav.popBackStack(); nav.manage(c.service, "provider", nid.toString(), resource) } }
                        }
                    }, enabled = !act.busy)
                    GlassButton(stringResource(R.string.test), { run(testOk) { c.post("$resource/test", f.applyTo(cur)) } }, enabled = !act.busy)
                    if (!isNew) DangerAction(stringResource(R.string.delete)) { askDelete = true }
                }
            }
        }
    }
    if (askDelete) ConfirmDialog(stringResource(R.string.m_delete_confirm, p?.s("name").orEmpty()), stringResource(R.string.delete), { askDelete = false }) {
        run(null) { c.delete("$resource/$id"); nav.popBackStack() }
    }
}

@Composable
private fun AppProfilePicker(c: ArrClient, cur: JsonObject, onChange: (JsonObject) -> Unit) {
    val ps = rememberLoaded(c.service) { c.list("appprofile") }
    ps.value?.let { l -> FieldRow(stringResource(R.string.m_s_app_profiles)) { Picker(cur.i("appProfileId"), l.map { it.i("id") to (it.s("name") ?: "?") }, { onChange(cur.with("appProfileId", it)) }) } }
}

// ---- config/* documents ----

/** Dropdown values for config keys whose values the APIs document as enums. */
private val CONFIG_CHOICES: Map<String, List<Pair<JsonElement, String>>> = mapOf(
    "authenticationMethod" to listOf("none", "basic", "forms", "external"),
    "authenticationRequired" to listOf("enabled", "disabledForLocalAddresses"),
    "proxyType" to listOf("http", "socks4", "socks5"),
    "updateMechanism" to listOf("builtIn", "script", "external", "apt", "docker"),
    "certificateValidation" to listOf("enabled", "disabledForLocalAddresses", "disabled"),
    "logLevel" to listOf("info", "debug", "trace"),
    "consoleLogLevel" to listOf("", "info", "debug", "trace"),
    "rescanAfterRefresh" to listOf("always", "afterManual", "never"),
    "downloadPropersAndRepacks" to listOf("preferAndUpgrade", "doNotUpgrade", "doNotPrefer"),
    "fileDate" to listOf("none", "cinemas", "release", "localAirDate", "utcAirDate", "albumReleaseDate"),
    "recycleBinCleanupDays" to emptyList(),
    "episodeTitleRequired" to listOf("always", "bulkSeasonReleases", "never"),
    "extraFileExtensions" to emptyList(),
    "colonReplacementFormat" to listOf("delete", "dash", "spaceDash", "spaceDashSpace", "smart"),
    "theme" to listOf("auto", "dark", "light"),
    "chmodFolder" to emptyList(),
).filterValues { it.isNotEmpty() }.mapValues { (_, v) -> v.map { JsonPrimitive(it) as JsonElement to humanize(it).ifEmpty { "—" } } }

/** A plain settings document (`config/host`, `config/ui`, `config/naming`, …): edit and PUT back. */
@Composable
fun ConfigPage(nav: NavHostController, c: ArrClient, resource: String) {
    val doc = rememberLoaded("${c.service}/$resource") { c.obj(resource) }
    var edited by remember(doc.value) { mutableStateOf(doc.value) }
    val (act, run) = rememberAction()
    val savedMsg = stringResource(R.string.saved)
    ManagePage(nav, configTitle(resource), c.service.title) {
        loaded(doc) { original ->
            val cur = edited ?: return@loaded
            if (resource == "config/naming") item { NamingExamples(c, cur) }
            block {
                JsonForm(cur, { edited = it }, act.error, choices = CONFIG_CHOICES)
                FormErrors(act.error, cur.keys)
                PrimaryButton(stringResource(R.string.save), {
                    run(savedMsg) {
                        val body = normalizeGeneric(cur, original) as JsonObject
                        val r = c.put(resource + (original.i("id")?.let { "/$it" } ?: ""), body) as? JsonObject
                        if (r != null) edited = r
                    }
                }, Modifier.fillMaxWidth(), enabled = !act.busy && cur != original)
                act.done?.let { Text(it, color = Kino.success) }
            }
        }
    }
}

@Composable
private fun configTitle(resource: String): String = stringResource(when (resource) {
    "config/host" -> R.string.m_s_general
    "config/ui" -> R.string.m_s_ui
    "config/naming" -> R.string.m_s_naming
    "config/mediamanagement" -> R.string.m_s_media_management
    "config/indexer" -> R.string.m_s_indexer_options
    "config/downloadclient" -> R.string.m_s_download_client_options
    else -> R.string.settings
})

/** Live examples of the naming formats being edited (`config/naming/examples`). */
@Composable
private fun NamingExamples(c: ArrClient, naming: JsonObject) {
    val query = naming.filterValues { it is JsonPrimitive }.mapValues { (it.value as JsonPrimitive).content }
    val ex = rememberLoaded(query.hashCode()) { kotlinx.coroutines.delay(400); c.obj("config/naming/examples", query) }
    Panel {
        Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(stringResource(R.string.m_naming_examples).uppercase(), style = Label, color = Kino.faint)
            ex.value?.forEach { (k, v) ->
                val text = (v as? JsonPrimitive)?.content ?: return@forEach
                if (text.isNotBlank() && text != "null") {
                    Text(humanize(k), color = Kino.muted, fontSize = 12.sp)
                    Text(text, color = Kino.text, fontSize = 13.sp)
                }
            }
            ArrError(ex.error)
        }
    }
}

// ---- Quality ----

@Composable
fun QualityProfileList(nav: NavHostController, c: ArrClient) {
    val list = rememberLoaded(c.service) { c.list("qualityprofile") }
    ManagePage(nav, stringResource(R.string.m_quality_profiles), c.service.title, actions = {
        IconButton(onClick = { nav.manage(c.service, "qualityprofile", "new") }) { Icon(Icons.Outlined.Add, stringResource(R.string.add), tint = Kino.text) }
    }) {
        loaded(list) { l ->
            items(l) { p ->
                val cutoff = QualityProfiles.items(p).firstOrNull { QualityProfiles.itemId(it) == p.i("cutoff") }?.let(QualityProfiles::itemName)
                ListRow(p.s("name") ?: "?", listOfNotNull(cutoff?.let { stringResource(R.string.m_cutoff_is, it) },
                    if (p.b("upgradeAllowed") == true) stringResource(R.string.m_upgrades_on) else null).joinToString(" · "), chevron = true) {
                    nav.manage(c.service, "qualityprofile", p.i("id").toString())
                }
            }
        }
    }
}

/** Quality profile: name, upgrades, cutoff, allowed qualities in order, custom format scores. */
@Composable
fun QualityProfileEdit(nav: NavHostController, c: ArrClient, id: String?) {
    val isNew = id == null || id == "new"
    val doc = rememberLoaded("qp/$id") { if (isNew) c.obj("qualityprofile/schema").without("id").with("name", "") else c.obj("qualityprofile/$id") }
    var p by remember(doc.value) { mutableStateOf(doc.value) }
    var askDelete by remember { mutableStateOf(false) }
    val (act, run) = rememberAction()
    val savedMsg = stringResource(R.string.saved)
    val err = act.error as? ArrException
    ManagePage(nav, p?.s("name")?.ifBlank { null } ?: stringResource(R.string.m_quality_profiles), c.service.title) {
        loaded(doc) { _ ->
            val cur = p ?: return@loaded
            block {
                FormText(stringResource(R.string.m_name), cur.s("name").orEmpty(), { p = cur.with("name", it) }, error = err?.messageFor("name"))
                Toggle(stringResource(R.string.m_upgrades_allowed), null, cur.b("upgradeAllowed") == true) { p = cur.with("upgradeAllowed", it) }
                FieldRow(stringResource(R.string.m_cutoff), err?.messageFor("cutoff")) {
                    Picker(cur.i("cutoff"), QualityProfiles.cutoffChoices(cur), { p = cur.with("cutoff", it) })
                }
                listOf("minFormatScore", "cutoffFormatScore", "minUpgradeFormatScore").filter { cur.containsKey(it) }.forEach { k ->
                    FormText(humanize(k), cur.i(k)?.toString().orEmpty(), { t -> p = cur.with(k, t.toIntOrNull() ?: 0) }, number = true, error = err?.messageFor(k))
                }
            }
            item { Heading(stringResource(R.string.m_qualities_order)) }
            val shown = QualityProfiles.display(cur)
            items(shown.size) { i ->
                val it = shown[i]
                Row(Modifier.fillMaxWidth().padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(it.b("allowed") == true, { a -> p = QualityProfiles.setAllowed(cur, i, a) }, colors = CheckboxDefaults.colors(checkedColor = Kino.text, checkmarkColor = Kino.bg))
                    Column(Modifier.weight(1f)) {
                        Text(QualityProfiles.itemName(it), color = if (it.b("allowed") == true) Kino.text else Kino.muted, fontSize = 15.sp,
                            fontWeight = if (QualityProfiles.itemId(it) == cur.i("cutoff")) FontWeight.Bold else FontWeight.Normal)
                        if (QualityProfiles.isGroup(it)) Text(it.objs("items").joinToString(", ") { q -> QualityProfiles.itemName(q) }, color = Kino.faint, fontSize = 12.sp)
                    }
                    IconButton(onClick = { p = QualityProfiles.move(cur, i, i - 1) }, enabled = i > 0) { Icon(Icons.Outlined.KeyboardArrowUp, stringResource(R.string.m_move_up), tint = Kino.muted) }
                    IconButton(onClick = { p = QualityProfiles.move(cur, i, i + 1) }, enabled = i < shown.size - 1) { Icon(Icons.Outlined.KeyboardArrowDown, stringResource(R.string.m_move_down), tint = Kino.muted) }
                }
            }
            val formats = cur.objs("formatItems")
            if (formats.isNotEmpty()) {
                item { Heading(stringResource(R.string.m_custom_format_scores)) }
                items(formats) { f ->
                    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 2.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text(f.s("name") ?: "?", color = Kino.text, fontSize = 14.sp, modifier = Modifier.weight(1f))
                        var t by remember(f.i("format")) { mutableStateOf(f.i("score")?.toString() ?: "0") }
                        OutlinedTextField(t, { v -> t = v; v.toIntOrNull()?.let { sc -> p = QualityProfiles.setScore(cur, f.i("format") ?: -1, sc) } }, singleLine = true,
                            modifier = Modifier.width(110.dp), shape = CardShape, keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.Number))
                    }
                }
            }
            block {
                FormErrors(act.error, listOf("name", "cutoff", "minFormatScore", "cutoffFormatScore"))
                Actions {
                    PrimaryButton(stringResource(R.string.save), { run(savedMsg) { if (isNew) { c.post("qualityprofile", cur); nav.popBackStack() } else c.put("qualityprofile/$id", cur) } }, enabled = !act.busy)
                    if (!isNew) DangerAction(stringResource(R.string.delete)) { askDelete = true }
                }
                act.done?.let { Text(it, color = Kino.success) }
            }
        }
    }
    if (askDelete) ConfirmDialog(stringResource(R.string.m_delete_confirm, p?.s("name").orEmpty()), stringResource(R.string.delete), { askDelete = false }) {
        run(null) { c.delete("qualityprofile/$id"); nav.popBackStack() }
    }
}

/** Quality definitions: the size limits per quality (MB per minute), saved per changed row. */
@Composable
fun QualityDefinitionsPage(nav: NavHostController, c: ArrClient) {
    val defs = rememberLoaded(c.service) { c.list("qualitydefinition") }
    var edited by remember(defs.value) { mutableStateOf(defs.value.orEmpty().associateBy { it.i("id") ?: 0 }) }
    val (act, run) = rememberAction()
    val savedMsg = stringResource(R.string.saved)
    val limit = if (c.service == ArrService.Lidarr) 1500f else 1000f
    ManagePage(nav, stringResource(R.string.m_quality_definitions), stringResource(R.string.m_mb_per_minute)) {
        block {
            PrimaryButton(stringResource(R.string.save), {
                run(savedMsg) {
                    val original = defs.value.orEmpty().associateBy { it.i("id") ?: 0 }
                    edited.values.filter { it != original[it.i("id") ?: 0] }.forEach { d -> c.put("qualitydefinition/${d.i("id")}", d) }
                    defs.reload()
                }
            }, Modifier.fillMaxWidth(), enabled = !act.busy)
            ActionOutcome(act)
        }
        loaded(defs) { list ->
            items(list.sortedBy { it.i("weight") ?: 0 }) { d0 ->
                val id = d0.i("id") ?: 0
                val d = edited[id] ?: d0
                fun set(k: String, v: Double?) { edited = edited + (id to d.with(k, v)) }
                Panel {
                    Column(Modifier.padding(14.dp)) {
                        Text(d.s("title") ?: d.o("quality")?.s("name") ?: "?", color = Kino.text, fontWeight = FontWeight.SemiBold)
                        SizeSlider(stringResource(R.string.m_min_size), d.d("minSize"), limit) { set("minSize", it) }
                        if (d.containsKey("preferredSize")) SizeSlider(stringResource(R.string.m_preferred_size), d.d("preferredSize"), limit) { set("preferredSize", it) }
                        SizeSlider(stringResource(R.string.m_max_size), d.d("maxSize"), limit) { set("maxSize", it) }
                    }
                }
            }
        }
    }
}

/** A size slider; the top of the scale means "no limit" (null). */
@Composable
private fun SizeSlider(label: String, value: Double?, limit: Float, onChange: (Double?) -> Unit) {
    val v = (value ?: limit.toDouble()).toFloat().coerceIn(0f, limit)
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(label, color = Kino.muted, fontSize = 12.sp, modifier = Modifier.width(84.dp))
        Slider(v, { x -> onChange(if (x >= limit) null else (Math.round(x * 10) / 10.0)) }, valueRange = 0f..limit, modifier = Modifier.weight(1f),
            colors = SliderDefaults.colors(thumbColor = Kino.text, activeTrackColor = LocalAccent.current, inactiveTrackColor = Kino.surface3))
        Text(if (value == null) "∞" else String.format(java.util.Locale.ROOT, "%.1f", value), color = Kino.text, fontSize = 12.sp, modifier = Modifier.width(52.dp))
    }
}

// ---- Custom formats ----

@Composable
fun CustomFormatEdit(nav: NavHostController, c: ArrClient, id: String?) {
    val isNew = id == null || id == "new"
    val doc = rememberLoaded("cf/$id") { if (isNew) jsonOf("name" to "", "includeCustomFormatWhenRenaming" to false, "specifications" to emptyList<Any>()) else c.obj("customformat/$id") }
    var cf by remember(doc.value) { mutableStateOf(doc.value) }
    var forms by remember(doc.value) { mutableStateOf<List<SchemaFormState>>(doc.value?.objs("specifications")?.map { SchemaFormState.of(it) } ?: emptyList()) }
    var adding by remember { mutableStateOf(false) }
    var askDelete by remember { mutableStateOf(false) }
    val (act, run) = rememberAction()
    val savedMsg = stringResource(R.string.saved)
    fun body(cur: JsonObject) = cur.with("specifications", JsonArray(cur.objs("specifications").mapIndexed { i, s -> forms.getOrNull(i)?.applyTo(s) ?: s }))
    ManagePage(nav, cf?.s("name")?.ifBlank { null } ?: stringResource(R.string.m_custom_formats), c.service.title) {
        loaded(doc) { _ ->
            val cur = cf ?: return@loaded
            block {
                FormText(stringResource(R.string.m_name), cur.s("name").orEmpty(), { cf = cur.with("name", it) }, error = (act.error as? ArrException)?.messageFor("name"))
                Toggle(humanize("includeCustomFormatWhenRenaming"), null, cur.b("includeCustomFormatWhenRenaming") == true) { cf = cur.with("includeCustomFormatWhenRenaming", it) }
            }
            item { Heading(stringResource(R.string.m_conditions)) }
            cur.objs("specifications").forEachIndexed { i, spec ->
                block {
                    Card(spec.s("implementationName") ?: spec.s("implementation")) {
                        FormText(stringResource(R.string.m_name), spec.s("name").orEmpty(), { v -> cf = cur.with("specifications", JsonArray(cur.objs("specifications").mapIndexed { j, s -> if (j == i) s.with("name", v) else s })) })
                        Toggle(humanize("negate"), null, spec.b("negate") == true) { v -> cf = cur.with("specifications", JsonArray(cur.objs("specifications").mapIndexed { j, s -> if (j == i) s.with("negate", v) else s })) }
                        Toggle(humanize("required"), null, spec.b("required") == true) { v -> cf = cur.with("specifications", JsonArray(cur.objs("specifications").mapIndexed { j, s -> if (j == i) s.with("required", v) else s })) }
                        forms.getOrNull(i)?.let { f -> SchemaForm(f, { nf -> forms = forms.toMutableList().also { it[i] = nf } }, act.error, advanced = true) }
                        DangerAction(stringResource(R.string.m_remove)) {
                            cf = cur.with("specifications", JsonArray(cur.objs("specifications").filterIndexed { j, _ -> j != i }))
                            forms = forms.filterIndexed { j, _ -> j != i }
                        }
                    }
                }
            }
            block {
                GlassButton(stringResource(R.string.m_add_condition), { adding = true }, Modifier.fillMaxWidth(), icon = Icons.Outlined.Add)
                FormErrors(act.error, listOf("name"))
                Actions {
                    PrimaryButton(stringResource(R.string.save), { run(savedMsg) { if (isNew) { c.post("customformat", body(cur)); nav.popBackStack() } else c.put("customformat/$id", body(cur)) } }, enabled = !act.busy)
                    if (!isNew) DangerAction(stringResource(R.string.delete)) { askDelete = true }
                }
                act.done?.let { Text(it, color = Kino.success) }
            }
        }
    }
    if (adding) SchemaPicker(c, "customformat", onDismiss = { adding = false }) { spec ->
        adding = false
        val cur = cf ?: return@SchemaPicker
        cf = cur.with("specifications", JsonArray(cur.objs("specifications") + listOf(spec)))
        forms = forms + SchemaFormState.of(spec)
    }
    if (askDelete) ConfirmDialog(stringResource(R.string.m_delete_confirm, cf?.s("name").orEmpty()), stringResource(R.string.delete), { askDelete = false }) {
        run(null) { c.delete("customformat/$id"); nav.popBackStack() }
    }
}

// ---- Root folders, tags ----

@Composable
fun RootFoldersPage(nav: NavHostController, c: ArrClient) {
    var tick by remember { mutableIntStateOf(0) }
    val list = rememberLoaded("${c.service}/rf/$tick") { c.list("rootfolder") }
    var path by remember { mutableStateOf("") }
    var confirm by remember { mutableStateOf<JsonObject?>(null) }
    val (act, run) = rememberAction()
    ManagePage(nav, stringResource(R.string.m_root_folders), c.service.title) {
        block {
            FormText(stringResource(R.string.m_path), path, { path = it }, error = (act.error as? ArrException)?.messageFor("path"), placeholder = "/media/movies")
            GlassButton(stringResource(R.string.add), {
                run(null) {
                    var body = jsonOf("path" to path.trim())
                    if (c.service == ArrService.Lidarr) {
                        // Lidarr root folders carry defaults for new artists.
                        val qp = c.list("qualityprofile").firstOrNull()?.i("id")
                        val mp = c.list("metadataprofile").firstOrNull()?.i("id")
                        body = body.with("name", path.trim().trimEnd('/').substringAfterLast('/')).with("defaultQualityProfileId", qp).with("defaultMetadataProfileId", mp)
                            .with("defaultMonitorOption", "all").with("defaultTags", emptyList<Int>())
                    }
                    c.post("rootfolder", body); path = ""; tick++
                }
            }, Modifier.fillMaxWidth(), enabled = path.isNotBlank() && !act.busy, icon = Icons.Outlined.CreateNewFolder)
            FormErrors(act.error, listOf("path"))
        }
        loaded(list) { l ->
            items(l) { r ->
                ListRow(r.s("path") ?: "?", listOfNotNull(stringResource(R.string.m_free, bytes(r.l("freeSpace"))), r.objs("unmappedFolders").size.takeIf { it > 0 }?.let { stringResource(R.string.m_unmapped, it) },
                    if (r.b("accessible") == false) stringResource(R.string.m_inaccessible) else null).joinToString(" · "),
                    trailing = { IconButton(onClick = { confirm = r }) { Icon(Icons.Outlined.DeleteOutline, stringResource(R.string.delete), tint = Kino.danger) } })
            }
        }
    }
    confirm?.let { r -> ConfirmDialog(stringResource(R.string.m_delete_confirm, r.s("path").orEmpty()), stringResource(R.string.delete), { confirm = null }) { run(null) { c.delete("rootfolder/${r.i("id")}"); tick++ } } }
}

@Composable
fun TagsPage(nav: NavHostController, c: ArrClient) {
    var tick by remember { mutableIntStateOf(0) }
    val list = rememberLoaded("${c.service}/tags/$tick") { runCatching { c.list("tag/detail") }.getOrElse { c.list("tag") } }
    var label by remember { mutableStateOf("") }
    var renaming by remember { mutableStateOf<JsonObject?>(null) }
    val (act, run) = rememberAction()
    ManagePage(nav, stringResource(R.string.m_tags), c.service.title) {
        block {
            FormText(stringResource(R.string.m_new_tag), label, { label = it.lowercase() })
            GlassButton(stringResource(R.string.add), { run(null) { c.post("tag", jsonOf("label" to label.trim())); label = ""; tick++ } }, Modifier.fillMaxWidth(), enabled = label.isNotBlank())
            ActionOutcome(act)
        }
        loaded(list) { l ->
            items(l) { t ->
                val uses = listOf("movieIds", "seriesIds", "artistIds", "indexerIds", "downloadClientIds", "notificationIds", "importListIds", "delayProfileIds", "releaseProfileIds", "applicationIds")
                    .sumOf { t.ints(it).size }
                ListRow(t.s("label") ?: "?", if (t.containsKey("delayProfileIds") || t.containsKey("indexerIds")) stringResource(R.string.m_used_by, uses) else null, trailing = {
                    Row {
                        IconButton(onClick = { renaming = t }) { Icon(Icons.Outlined.Edit, stringResource(R.string.edit), tint = Kino.muted) }
                        IconButton(onClick = { run(null) { c.delete("tag/${t.i("id")}"); tick++ } }) { Icon(Icons.Outlined.DeleteOutline, stringResource(R.string.delete), tint = Kino.danger) }
                    }
                })
            }
        }
    }
    renaming?.let { t ->
        var v by remember { mutableStateOf(t.s("label").orEmpty()) }
        AlertDialog(onDismissRequest = { renaming = null }, containerColor = Kino.surface,
            text = { FormText(stringResource(R.string.m_name), v, { v = it.lowercase() }) },
            confirmButton = { TextButton(onClick = { renaming = null; run(null) { c.put("tag/${t.i("id")}", jsonOf("id" to t.i("id"), "label" to v.trim())); tick++ } }) { Text(stringResource(R.string.save), color = Kino.text) } },
            dismissButton = { TextButton(onClick = { renaming = null }) { Text(stringResource(R.string.cancel), color = Kino.muted) } })
    }
}

// ---- Generic list + JSON editor (delay/release/metadata/language/app profiles, path mappings, exclusions, custom formats) ----

private fun crudTitleRes(resource: String) = when (resource) {
    "delayprofile" -> R.string.m_delay_profiles
    "releaseprofile" -> R.string.m_release_profiles
    "metadataprofile" -> R.string.m_metadata_profiles
    "languageprofile" -> R.string.m_language_profiles
    "remotepathmapping" -> R.string.m_remote_path_mappings
    "appprofile" -> R.string.m_s_app_profiles
    "customformat" -> R.string.m_custom_formats
    "exclusions", "importlistexclusion" -> R.string.m_s_import_exclusions
    else -> R.string.settings
}

/** A template for a new resource that has no `/schema` endpoint (shapes from the public API docs). */
private fun crudTemplate(resource: String): JsonObject? = when (resource) {
    "delayprofile" -> jsonOf("enableUsenet" to true, "enableTorrent" to true, "preferredProtocol" to "usenet", "usenetDelay" to 0, "torrentDelay" to 0,
        "bypassIfHighestQuality" to true, "bypassIfAboveCustomFormatScore" to false, "minimumCustomFormatScore" to 0, "order" to 1, "tags" to emptyList<Int>())
    "releaseprofile" -> jsonOf("name" to "", "enabled" to true, "required" to emptyList<String>(), "ignored" to emptyList<String>(), "indexerId" to 0, "tags" to emptyList<Int>())
    "remotepathmapping" -> jsonOf("host" to "", "remotePath" to "", "localPath" to "")
    "exclusions" -> jsonOf("tmdbId" to 0, "movieTitle" to "", "movieYear" to 0)
    "importlistexclusion" -> jsonOf("tvdbId" to 0, "title" to "")
    else -> null
}

fun crudName(o: JsonObject): String = o.s("name") ?: o.s("label") ?: o.s("title") ?: o.s("movieTitle") ?: o.s("artistName")
    ?: o.s("remotePath")?.let { "${o.s("host").orEmpty()} ${it} → ${o.s("localPath").orEmpty()}" }
    ?: o.s("preferredProtocol")?.let { "${humanize(it)} · ${o.i("usenetDelay") ?: 0}/${o.i("torrentDelay") ?: 0} min" } ?: "#${o.i("id")}"

@Composable
fun CrudList(nav: NavHostController, c: ArrClient, resource: String) {
    val list = rememberLoaded("${c.service}/$resource") { c.list(resource) }
    val editPage = if (resource == "customformat") "customformat" else "crudedit"
    ManagePage(nav, stringResource(crudTitleRes(resource)), c.service.title, actions = {
        IconButton(onClick = { nav.manage(c.service, editPage, "new", resource) }) { Icon(Icons.Outlined.Add, stringResource(R.string.add), tint = Kino.text) }
    }) {
        loaded(list) { l ->
            if (l.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
            items(l) { o -> ListRow(crudName(o), o.ints("tags").takeIf { it.isNotEmpty() }?.let { stringResource(R.string.m_tags) + ": " + it.size }, chevron = true) { nav.manage(c.service, editPage, o.i("id").toString(), resource) } }
        }
    }
}

@Composable
fun CrudEdit(nav: NavHostController, c: ArrClient, resource: String, id: String?) {
    val isNew = id == null || id == "new"
    val doc = rememberLoaded("$resource/$id") {
        if (!isNew) c.obj("$resource/$id")
        else crudTemplate(resource) ?: c.obj("$resource/schema").without("id")
    }
    var cur by remember(doc.value) { mutableStateOf(doc.value) }
    var askDelete by remember { mutableStateOf(false) }
    val (act, run) = rememberAction()
    val savedMsg = stringResource(R.string.saved)
    ManagePage(nav, cur?.let(::crudName) ?: stringResource(crudTitleRes(resource)), stringResource(crudTitleRes(resource))) {
        loaded(doc) { original ->
            val o = cur ?: return@loaded
            block {
                JsonForm(o, { cur = it }, act.error, hide = setOf("id", "tags"), choices = mapOf("preferredProtocol" to listOf("usenet", "torrent").map { JsonPrimitive(it) as JsonElement to humanize(it) }))
                if (o.containsKey("tags")) TagPicker(c, o.ints("tags")) { cur = o.with("tags", it) }
                FormErrors(act.error, o.keys)
                Actions {
                    PrimaryButton(stringResource(R.string.save), {
                        run(savedMsg) {
                            val body = normalizeGeneric(o, original) as JsonObject
                            if (isNew) { c.post(resource, body); nav.popBackStack() } else c.put("$resource/$id", body)
                        }
                    }, enabled = !act.busy)
                    if (!isNew) DangerAction(stringResource(R.string.delete)) { askDelete = true }
                }
                act.done?.let { Text(it, color = Kino.success) }
            }
        }
    }
    if (askDelete) ConfirmDialog(stringResource(R.string.m_delete_confirm, cur?.let(::crudName).orEmpty()), stringResource(R.string.delete), { askDelete = false }) {
        run(null) { c.delete("$resource/$id"); nav.popBackStack() }
    }
}

