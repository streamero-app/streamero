package net.scyde.streamero.manage

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.navigation.NavHostController
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import net.scyde.streamero.R
import net.scyde.streamero.api.json
import net.scyde.streamero.ui.*

/** Bazarr's answers wrap lists in `data`. */
private suspend fun ArrClient.data(resource: String, query: Map<String, Any?> = emptyMap()): List<JsonObject> = list(resource, query)

/** A Bazarr missing-subtitle entry as a short language label ("en", "hr forced", "de HI"). */
private fun langLabel(l: JsonObject, forced: String): String = listOfNotNull(l.s("code2") ?: l.s("name"), if (l.b("forced") == true) forced else null, if (l.b("hi") == true) "HI" else null).joinToString(" ")

/** Movies or episodes with missing subtitles: search the best one per language, or pick manually. */
@Composable
fun BazarrWanted(nav: NavHostController, c: ArrClient, kind: String) {
    val movies = kind == "movies"
    var tick by remember { mutableIntStateOf(0) }
    var pages by remember { mutableIntStateOf(1) }
    val wanted = rememberLoaded("$kind/$tick/$pages") { c.obj("$kind/wanted", mapOf("start" to 0, "length" to 50 * pages)) }
    val (act, run) = rememberAction()
    val queued = stringResource(R.string.m_command_queued)
    ManagePage(nav, stringResource(if (movies) R.string.m_wanted_movies else R.string.m_wanted_episodes), wanted.value?.l("total")?.let { itemsCount(it.toInt()) }) {
        block {
            GlassButton(stringResource(R.string.m_search_all), { run(queued) { c.patch(if (movies) "movies" else "series", query = mapOf("action" to "search-wanted")) } }, Modifier.fillMaxWidth(), icon = Icons.Outlined.Search)
            ActionOutcome(act)
        }
        loaded(wanted) { w ->
            val list = w.objs("data")
            if (list.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
            items(list) { r ->
                val title = if (movies) r.s("title") ?: "?" else listOfNotNull(r.s("seriesTitle"), r.s("episode_number"), r.s("episodeTitle")).joinToString(" · ")
                val missing = r.objs("missing_subtitles")
                Panel {
                    Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text(title, color = Kino.text, fontSize = 15.sp)
                        r.s("sceneName")?.let { Text(it, color = Kino.faint, fontSize = 12.sp) }
                        Actions {
                            missing.forEach { l ->
                                AssistChip(onClick = {
                                    run(queued) {
                                        val q = mapOf("language" to l.s("code2"), "forced" to (l.b("forced") == true), "hi" to (l.b("hi") == true)) +
                                            if (movies) mapOf("radarrid" to r.i("radarrId")) else mapOf("seriesid" to r.i("sonarrSeriesId"), "episodeid" to r.i("sonarrEpisodeId"))
                                        c.patch(if (movies) "movies/subtitles" else "episodes/subtitles", query = q)
                                        tick++
                                    }
                                }, label = { Text(langLabel(l, stringResource(R.string.m_forced))) }, leadingIcon = { Icon(Icons.Outlined.Search, null, Modifier.size(16.dp)) })
                            }
                            val subsId = if (movies) r.i("radarrId") else r.i("sonarrEpisodeId")
                            if (subsId != null) TextButton(onClick = {
                                val arg = if (movies) "movies" else "episodes:${r.i("sonarrSeriesId") ?: ""}"
                                nav.manage(c.service, "bsubs", subsId.toString(), arg)
                            }) { Text(stringResource(R.string.m_manual_search)) }
                        }
                    }
                }
            }
            if (list.size < (w.l("total") ?: 0)) block { GlassButton(stringResource(R.string.m_load_more), { pages++ }, Modifier.fillMaxWidth()) }
        }
    }
}

/** Manual subtitle search for one movie or episode across the providers, then download one. */
@Composable
fun BazarrSubtitles(nav: NavHostController, c: ArrClient, kindArg: String, id: String) {
    val movies = kindArg == "movies"
    val seriesId = kindArg.substringAfter(':', "")
    val offers = rememberLoaded("$kindArg/$id") { c.data(if (movies) "providers/movies" else "providers/episodes", if (movies) mapOf("radarrid" to id) else mapOf("episodeid" to id)) }
    val (act, run) = rememberAction()
    val done = stringResource(R.string.m_subtitle_downloaded)
    ManagePage(nav, stringResource(R.string.m_manual_search), stringResource(R.string.m_providers)) {
        block { ActionOutcome(act) }
        loaded(offers) { list ->
            if (list.isEmpty()) block { Text(stringResource(R.string.m_nothing_found), color = Kino.muted) }
            items(list.sortedByDescending { it.d("score") ?: 0.0 }) { o ->
                ListRow(o.strs("release_info").firstOrNull() ?: o.s("subtitle")?.take(60) ?: "?",
                    listOfNotNull(o.s("provider"), o.s("language"), o.d("score")?.let { "${it.toInt()}%" }, if (o.b("hearing_impaired") == true || o.s("hearing_impaired") == "True") "HI" else null,
                        if (o.b("forced") == true || o.s("forced") == "True") stringResource(R.string.m_forced) else null, o.s("uploader")).joinToString(" · "),
                    trailing = {
                        IconButton(onClick = {
                            run(done) {
                                val q = mapOf("hi" to (o.s("hearing_impaired") ?: "False"), "forced" to (o.s("forced") ?: "False"), "original_format" to (o.s("original_format") ?: "False"),
                                    "provider" to o.s("provider"), "subtitle" to o.s("subtitle"), "language" to o.s("language")) +
                                    if (movies) mapOf("radarrid" to id) else mapOf("seriesid" to seriesId, "episodeid" to id)
                                c.post(if (movies) "providers/movies" else "providers/episodes", query = q)
                            }
                        }) { Icon(Icons.Outlined.Download, stringResource(R.string.m_download), tint = Kino.text) }
                    })
            }
        }
    }
}

@Composable
fun BazarrHistory(nav: NavHostController, c: ArrClient, kind: String) {
    var pages by remember { mutableIntStateOf(1) }
    val h = rememberLoaded("$kind/$pages") { c.obj("$kind/history", mapOf("start" to 0, "length" to 50 * pages)) }
    ManagePage(nav, stringResource(if (kind == "movies") R.string.m_history_movies else R.string.m_history_episodes), c.service.title) {
        loaded(h) { page ->
            val list = page.objs("data")
            if (list.isEmpty()) block { Text(stringResource(R.string.m_nothing_here), color = Kino.muted) }
            items(list) { r ->
                val title = r.s("title") ?: listOfNotNull(r.s("seriesTitle"), r.s("episode_number"), r.s("episodeTitle")).joinToString(" · ")
                ListRow(title, listOfNotNull(r.s("description"), r.o("language")?.s("name"), r.s("provider"), r.s("score"), r.s("timestamp")).joinToString(" · "))
            }
            if (list.size < (page.l("total") ?: 0)) block { GlassButton(stringResource(R.string.m_load_more), { pages++ }, Modifier.fillMaxWidth()) }
        }
    }
}

@Composable
fun BazarrProviders(nav: NavHostController, c: ArrClient) {
    var tick by remember { mutableIntStateOf(0) }
    val p = rememberLoaded(tick) { c.data("providers") }
    val (act, run) = rememberAction()
    ManagePage(nav, stringResource(R.string.m_providers), c.service.title) {
        block {
            GlassButton(stringResource(R.string.m_reset_providers), { run(null) { c.post("providers", query = mapOf("action" to "reset")); tick++ } }, Modifier.fillMaxWidth(), icon = Icons.Outlined.RestartAlt)
            ActionOutcome(act)
        }
        loaded(p) { list ->
            if (list.isEmpty()) block { Text(stringResource(R.string.m_providers_all_ok), color = Kino.success) }
            items(list) { r -> ListRow(r.s("name") ?: "?", listOfNotNull(r.s("status"), r.s("retry")).joinToString(" · "), leading = { Dot(if (r.s("status").equals("good", true)) Kino.success else Kino.accent) }) }
        }
    }
}

private data class BazarrSys(val status: JsonObject?, val health: List<JsonObject>, val tasks: List<JsonObject>, val logs: List<JsonObject>, val backups: List<JsonObject>)

@Composable
fun BazarrSystem(nav: NavHostController, c: ArrClient) {
    var tick by remember { mutableIntStateOf(0) }
    val sys = rememberLoaded(tick, pollMs = 15_000) {
        coroutineScope {
            val st = async { attempt { c.obj("system/status").o("data") }.getOrNull() }
            val h = async { attempt { c.data("system/health") }.getOrDefault(emptyList()) }
            val t = async { attempt { c.data("system/tasks") }.getOrDefault(emptyList()) }
            val l = async { attempt { c.data("system/logs") }.getOrDefault(emptyList()) }
            val b = async { attempt { c.data("system/backups") }.getOrDefault(emptyList()) }
            BazarrSys(st.await(), h.await(), t.await(), l.await(), b.await())
        }
    }
    val (act, run) = rememberAction()
    val queued = stringResource(R.string.m_command_queued)
    var restore by remember { mutableStateOf<String?>(null) }
    var deleting by remember { mutableStateOf<String?>(null) }
    val restoringMsg = stringResource(R.string.m_restore_started)
    ManagePage(nav, stringResource(R.string.m_system), c.service.title) {
        block { ActionOutcome(act) }
        loaded(sys) { s ->
            s.status?.let { st -> block { Card(stringResource(R.string.m_status)) { st.forEach { (k, v) -> (v as? JsonPrimitive)?.contentOrNull?.takeIf { it.isNotBlank() }?.let { KeyValue(humanize(k), it) } } } } }
            block {
                Card(stringResource(R.string.m_health)) {
                    if (s.health.isEmpty()) Text(stringResource(R.string.m_health_ok), color = Kino.success, fontSize = 14.sp)
                    s.health.forEach { Text("${it.s("object").orEmpty()}: ${it.s("issue").orEmpty()}", color = Kino.accent, fontSize = 14.sp) }
                }
            }
            item { Heading(stringResource(R.string.tasks)) }
            items(s.tasks) { t ->
                ListRow(t.s("name") ?: "?", listOfNotNull(t.s("interval"), t.s("next_run_in")?.let { stringResource(R.string.m_next_run, it) }).joinToString(" · "), trailing = {
                    if (t.b("job_running") == true) CircularProgressIndicator(Modifier.size(20.dp), color = Kino.text, strokeWidth = 2.dp)
                    else IconButton(onClick = { run(queued) { c.post("system/tasks", query = mapOf("taskid" to t.s("job_id"))); tick++ } }) { Icon(Icons.Outlined.PlayArrow, stringResource(R.string.run_now), tint = Kino.text) }
                })
            }
            item { Heading(stringResource(R.string.backups)) }
            block { GlassButton(stringResource(R.string.m_backup_now), { run(queued) { c.post("system/backups"); tick++ } }, Modifier.fillMaxWidth(), icon = Icons.Outlined.Backup) }
            items(s.backups) { b ->
                ListRow(b.s("filename") ?: "?", listOfNotNull(b.s("size"), b.s("date")).joinToString(" · "), trailing = {
                    Row {
                        IconButton(onClick = { restore = b.s("filename") }) { Icon(Icons.Outlined.Restore, stringResource(R.string.m_restore), tint = Kino.muted) }
                        IconButton(onClick = { deleting = b.s("filename") }, enabled = !act.busy) { Icon(Icons.Outlined.DeleteOutline, stringResource(R.string.delete), tint = Kino.danger) }
                    }
                })
            }
            item { Heading(stringResource(R.string.logs)) }
            items(s.logs.take(200)) { l ->
                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
                    Text("${l.s("timestamp").orEmpty()} · ${l.s("type").orEmpty()}", color = if (l.s("type") == "ERROR") Kino.danger else Kino.faint, fontSize = 11.sp)
                    Text(l.s("message").orEmpty(), color = Kino.text, fontSize = 12.sp, fontFamily = FontFamily.Monospace)
                }
            }
        }
    }
    deleting?.let { f ->
        ConfirmDialog(stringResource(R.string.m_delete_confirm, f), stringResource(R.string.delete), { deleting = null }) {
            run(null) { c.delete("system/backups", mapOf("filename" to f)); tick++ }
        }
    }
    restore?.let { f -> ConfirmDialog(stringResource(R.string.m_restore_confirm, f), stringResource(R.string.m_restore), { restore = null }) { run(restoringMsg) { c.patch("system/backups", query = mapOf("filename" to f)) } } }
}

/**
 * Bazarr saves settings as form fields named `settings-{section}-{key}` (lists repeat the
 * field). Only keys that changed are sent; an untouched secret is never sent.
 */
fun bazarrSettingsForm(original: JsonObject, edited: JsonObject): List<Pair<String, String>> {
    val out = mutableListOf<Pair<String, String>>()
    edited.forEach { (section, sv) ->
        val so = sv as? JsonObject ?: return@forEach
        val oo = original[section] as? JsonObject
        so.forEach { (key, v0) ->
            val orig = oo?.get(key)
            val v = normalizeGeneric(v0, orig, key)
            if (v == orig) return@forEach
            val name = "settings-$section-$key"
            when (v) {
                is JsonArray -> if (v.isEmpty()) out += name to "" else v.forEach { e -> out += name to ((e as? JsonPrimitive)?.contentOrNull ?: e.toString()) }
                is JsonObject -> out += name to json.encodeToString(JsonElement.serializer(), v)
                is JsonNull -> out += name to ""
                is JsonPrimitive -> out += name to v.content
            }
        }
    }
    return out
}

/** Bazarr settings: every section of its settings object, plus enabled languages and language profiles. */
@Composable
fun BazarrSettings(nav: NavHostController, c: ArrClient) {
    var tick by remember { mutableIntStateOf(0) }
    val doc = rememberLoaded(tick) {
        coroutineScope {
            val s = async { c.obj("system/settings") }
            val langs = async { attempt { c.list("system/languages") }.getOrDefault(emptyList()) }
            val profiles = async { attempt { c.list("system/languages/profiles") }.getOrDefault(emptyList()) }
            Triple(s.await(), langs.await(), profiles.await())
        }
    }
    var edited by remember(doc.value) { mutableStateOf(doc.value?.first) }
    var enabled by remember(doc.value) { mutableStateOf<List<JsonElement>>(doc.value?.second?.filter { it.b("enabled") == true }?.map { toJson(it.s("code2")) } ?: emptyList()) }
    var profiles by remember(doc.value) { mutableStateOf(doc.value?.third?.let { JsonObject(mapOf("profiles" to JsonArray(it))) }) }
    var section by remember { mutableStateOf("languages") }
    val (act, run) = rememberAction()
    val savedMsg = stringResource(R.string.saved)
    val d = doc.value
    if (hasSecrets(d?.first)) SecureScreen()
    UnsavedChangesGuard(nav, d != null && (edited != d.first ||
        enabled.mapNotNull { (it as? JsonPrimitive)?.contentOrNull }.toSet() != d.second.filter { it.b("enabled") == true }.mapNotNull { it.s("code2") }.toSet() ||
        profiles?.a("profiles") != JsonArray(d.third)))
    ManagePage(nav, stringResource(R.string.settings), c.service.title) {
        loaded(doc) { (original, langs, origProfiles) ->
            val cur = edited ?: return@loaded
            item { ChipRow(listOf("languages" to stringResource(R.string.m_languages)) + cur.keys.filter { cur[it] is JsonObject }.map { it to humanize(it) }, section) { section = it } }
            block {
                if (section == "languages") {
                    MultiPick(stringResource(R.string.m_languages), langs.map { SelectOption(toJson(it.s("code2")), it.s("name") ?: "?") }, enabled) { enabled = it }
                    profiles?.let { p -> JsonForm(p, { profiles = it }, act.error) }
                } else (cur[section] as? JsonObject)?.let { sec ->
                    JsonForm(sec, { edited = cur.with(section, it) }, act.error, hide = emptySet())
                }
                PrimaryButton(stringResource(R.string.save), {
                    run(savedMsg) {
                        val badNumbers = (invalidNumbers(cur, original) + (profiles?.let { invalidNumbers(it, JsonObject(mapOf("profiles" to JsonArray(origProfiles)))) } ?: emptyList())).distinct()
                        if (badNumbers.isNotEmpty()) throw InvalidNumber(badNumbers)
                        val fields = bazarrSettingsForm(original, cur).toMutableList()
                        val origEnabled = langs.filter { it.b("enabled") == true }.mapNotNull { it.s("code2") }.toSet()
                        val nowEnabled = enabled.mapNotNull { (it as? JsonPrimitive)?.contentOrNull }
                        if (nowEnabled.toSet() != origEnabled) nowEnabled.forEach { fields += "languages-enabled" to it }
                        // Numbers typed into a profile become numbers again (as everywhere else in the editor).
                        val p = profiles?.a("profiles")?.let { normalizeGeneric(it, JsonArray(origProfiles), "profiles") }
                        if (p != null && p != JsonArray(origProfiles)) fields += "languages-profiles" to json.encodeToString(JsonElement.serializer(), p)
                        if (fields.isNotEmpty()) c.postForm("system/settings", fields)
                        tick++
                    }
                }, Modifier.fillMaxWidth(), enabled = !act.busy)
                ActionOutcome(act)
            }
        }
    }
}
